//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen -o server.gen.go -package intnl -generate types,strict-server,std-http-server openapi.yaml
//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen -o client.gen.go -package intnl -generate client openapi.yaml

package intnl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/hollow-cube/hc-services/libraries/common/pkg/kafkafx"
	"github.com/hollow-cube/hc-services/libraries/common/pkg/metric"
	"github.com/hollow-cube/hc-services/services/player-service/api/auth"
	"github.com/hollow-cube/hc-services/services/player-service/config"
	"github.com/hollow-cube/hc-services/services/player-service/internal/db"
	"github.com/hollow-cube/hc-services/services/player-service/internal/pkg/authz"
	"github.com/hollow-cube/hc-services/services/player-service/internal/pkg/model"
	"github.com/hollow-cube/hc-services/services/player-service/internal/pkg/util"
	"github.com/hollow-cube/tebex-go"
	"github.com/segmentio/kafka-go"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

var _ StrictServerInterface = (*server)(nil)

type ServerParams struct {
	fx.In

	Log        *zap.SugaredLogger
	Config     *config.Config
	Metrics    metric.Writer
	Producer   kafkafx.SyncProducer
	TBHeadless *tebex.HeadlessClient

	Store             *db.Store
	Authz             authz.Client
	PunishmentLadders map[string]*model.PunishmentLadder
}

func NewServer(p ServerParams) (StrictServerInterface, error) {
	nameCache2, _ := lru.New[string, DisplayNameV2](1000)

	punishmentAliases := make(map[model.PunishmentType]map[string]*model.PunishmentLadder)
	for _, ladder := range p.PunishmentLadders {
		aliases, ok := punishmentAliases[ladder.Type]
		if !ok {
			aliases = make(map[string]*model.PunishmentLadder)
			punishmentAliases[ladder.Type] = aliases
		}

		aliases[ladder.Id] = ladder
		for _, reason := range ladder.Reasons {
			aliases[reason.Id] = ladder
			for _, alias := range reason.Aliases {
				aliases[alias] = ladder
			}
		}
	}

	return &server{
		log:               p.Log.With("handler", "internal"),
		metrics:           p.Metrics,
		store:             p.Store,
		authzClient:       p.Authz,
		producer:          p.Producer,
		tbHeadless:        p.TBHeadless,
		punishmentLadders: p.PunishmentLadders,
		punishmentAliases: punishmentAliases,
		nameCache2:        nameCache2,
	}, nil
}

type server struct {
	log     *zap.SugaredLogger
	metrics metric.Writer

	store       *db.Store
	authzClient authz.Client
	producer    kafkafx.SyncProducer
	tbHeadless  *tebex.HeadlessClient

	punishmentLadders map[string]*model.PunishmentLadder
	punishmentAliases map[model.PunishmentType]map[string]*model.PunishmentLadder

	//todo names are never evicted from this, should use redis instead and perhaps LRU on server if needed.
	nameCache2 *lru.Cache[string, DisplayNameV2]
}

func (s *server) GetPlayerData(ctx context.Context, request GetPlayerDataRequestObject) (GetPlayerDataResponseObject, error) {
	pd, err := s.store.GetPlayerData(ctx, util.RemapUUID(request.PlayerId))
	if errors.Is(err, db.ErrNoRows) {
		return GetPlayerData404Response{}, nil
	} else if err != nil {
		return nil, err
	}

	apiPlayer, err := s.hydratePlayerData(ctx, pd)
	if err != nil {
		return nil, err
	}
	return GetPlayerData200JSONResponse(*apiPlayer), nil
}

func (s *server) CreatePlayerData(ctx context.Context, request CreatePlayerDataRequestObject) (CreatePlayerDataResponseObject, error) {
	var skin *db.PlayerSkin
	if request.Body.Skin != nil {
		skin = &db.PlayerSkin{
			Texture:   request.Body.Skin.Texture,
			Signature: request.Body.Skin.Signature,
		}
	}

	pd, err := s.store.CreatePlayerData(ctx, request.Body.Id, request.Body.Username, skin)
	if err != nil {
		return nil, fmt.Errorf("failed to create player data: %w", err)
	}

	err = s.store.AddPlayerIP(ctx, pd.ID, request.Body.Ip)
	if err != nil {
		return nil, fmt.Errorf("failed to add player ip: %w", err)
	}

	go s.metrics.Write(&model.NewPlayer{PlayerId: pd.ID})

	apiPlayer, err := s.hydratePlayerData(ctx, pd)
	if err != nil {
		return nil, err
	}

	return CreatePlayerData201JSONResponse(*apiPlayer), nil
}

func (s *server) UpdatePlayerData(ctx context.Context, request UpdatePlayerDataRequestObject) (UpdatePlayerDataResponseObject, error) {
	p, err := s.store.GetPlayerData(ctx, request.PlayerId)
	if errors.Is(err, db.ErrNoRows) {
		return PlayerNotFoundResponse{}, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to get player data: %w", err)
	}

	var changed bool
	dbUpdates := db.UpdatePlayerDataParams{ID: p.ID, Skin: p.Skin}
	updates := request.Body
	if updates.Username != nil && *updates.Username != p.Username {
		dbUpdates.Username = updates.Username
		changed = true
	}
	if updates.LastOnline != nil {
		dbUpdates.LastOnline = updates.LastOnline
		changed = true
	}
	if updates.PlaytimeInc != nil {
		newPlaytime := p.Playtime + *updates.PlaytimeInc
		dbUpdates.Playtime = &newPlaytime
		changed = true
	}
	if updates.BetaEnabled != nil {
		dbUpdates.BetaEnabled = updates.BetaEnabled
		changed = true
	}
	if updates.SettingsUpdates != nil {
		for key, value := range *updates.SettingsUpdates {
			p.Settings[key] = value
			dbUpdates.Settings = p.Settings
			changed = true
		}
	}
	if updates.Skin != nil {
		dbUpdates.Skin = &db.PlayerSkin{
			Texture:   updates.Skin.Texture,
			Signature: updates.Skin.Signature,
		}
		changed = true
	}

	err = db.TxNoReturn(ctx, s.store, func(ctx context.Context, txStore *db.Store) error {
		if updates.IpHistory != nil && len(*updates.IpHistory) > 0 {
			for _, ip := range *updates.IpHistory {
				if ip == "" {
					continue
				}

				if err = txStore.AddPlayerIP(ctx, p.ID, ip); err != nil {
					return fmt.Errorf("failed to record player ip: %w", err)
				}
			}
			changed = true
		}

		if !changed {
			return nil
		}

		err = txStore.UpdatePlayerData(ctx, dbUpdates)
		if err != nil {
			return fmt.Errorf("failed to update player data: %w", err)
		}

		return nil
	})
	if errors.Is(err, db.ErrNoRows) {
		return PlayerNotFoundResponse{}, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to apply transaction: %w", err)
	}

	return UpdatePlayerData200Response{}, nil
}

func (s *server) GetPlayerBackpack(_ context.Context, _ GetPlayerBackpackRequestObject) (GetPlayerBackpackResponseObject, error) {
	return GetPlayerBackpack200JSONResponse{}, nil
}

func (s *server) GetPlayerCosmetics(ctx context.Context, request GetPlayerCosmeticsRequestObject) (GetPlayerCosmeticsResponseObject, error) {
	cosmetics, err := s.store.GetUnlockedCosmetics(ctx, request.PlayerId)
	if err != nil {
		return nil, fmt.Errorf("failed to get unlocked cosmetics: %w", err)
	}

	return GetPlayerCosmetics200JSONResponse(cosmetics), nil
}

func (s *server) GetPlayerDisplayNameV2(ctx context.Context, request GetPlayerDisplayNameV2RequestObject) (GetPlayerDisplayNameV2ResponseObject, error) {
	if orgName, ok := orgMapNames[request.PlayerId]; ok {
		return GetPlayerDisplayNameV2200JSONResponse(orgName), nil
	}

	if cached, ok := s.nameCache2.Get(request.PlayerId); ok {
		return GetPlayerDisplayNameV2200JSONResponse(cached), nil
	}

	// Load it from storage
	pd, err := s.store.GetPlayerData(ctx, request.PlayerId)
	if err != nil {
		if errors.Is(err, db.ErrNoRows) {
			return GetPlayerDisplayNameV2404Response{}, nil
		}

		return nil, fmt.Errorf("failed to get player data: %w", err)
	}

	displayName, err := s.computeDisplayNameV2(ctx, pd.ID, pd.Username)
	if err != nil {
		return nil, err
	}

	return GetPlayerDisplayNameV2200JSONResponse(displayName), nil
}

func (s *server) GetPlayerAlts(ctx context.Context, request GetPlayerAltsRequestObject) (GetPlayerAltsResponseObject, error) {
	playerIPs, err := s.store.GetPlayerIPHistory(ctx, request.PlayerId)
	if err != nil {
		return nil, err
	}

	sharedPlayers, err := s.store.GetPlayersByIPs(ctx, playerIPs)
	if err != nil {
		return nil, err
	}

	results := make([]PlayerAltsItem, 0, 10)
	for _, row := range sharedPlayers {
		if row.ID == request.PlayerId {
			continue
		}

		results = append(results, PlayerAltsItem{
			Id:       row.ID,
			Username: row.Username,
		})
	}

	return GetPlayerAlts200JSONResponse{Results: results}, nil
}

func (s *server) CyclePlayerApiKey(ctx context.Context, request CyclePlayerApiKeyRequestObject) (CyclePlayerApiKeyResponseObject, error) {
	res, err := db.Tx(ctx, s.store, func(ctx context.Context, txStore *db.Store) (*CyclePlayerApiKey200JSONResponse, error) {
		_, err := txStore.GetPlayerData(ctx, request.PlayerId)
		if errors.Is(err, db.ErrNoRows) {
			return nil, nil
		} else if err != nil {
			return nil, fmt.Errorf("failed to get player data: %w", err)
		}

		err = txStore.DeleteAllApiKeys(ctx, request.PlayerId)
		if err != nil {
			return nil, fmt.Errorf("failed to delete existing api keys: %w", err)
		}

		key, hash, err := auth.GenerateAPIKey()
		if err != nil {
			return nil, fmt.Errorf("failed to generate api key: %w", err)
		}

		err = txStore.InsertApiKey(ctx, hash, request.PlayerId)
		return &CyclePlayerApiKey200JSONResponse{
			ApiKey: key,
		}, err
	})
	if err != nil {
		return nil, err
	} else if res == nil {
		return &CyclePlayerApiKey404Response{}, nil
	}
	return res, nil
}

func (s *server) GetPlayerId(ctx context.Context, request GetPlayerIdRequestObject) (GetPlayerIdResponseObject, error) {
	pid, err := s.store.SafeLookupPlayerIdByIdOrUsername(ctx, request.IdOrUsername)
	if errors.Is(err, db.ErrNoRows) {
		return PlayerNotFoundResponse{}, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to lookup player: %w", err)
	}

	return GetPlayerId200TextResponse(pid), nil
}

func (s *server) PerformTabComplete(ctx context.Context, request PerformTabCompleteRequestObject) (PerformTabCompleteResponseObject, error) {
	if request.Body.Query == "" {
		return PerformTabComplete200JSONResponse{Result: []TabCompleteEntry{}}, nil
	}

	entries, err := s.store.SearchPlayersFuzzy(ctx, request.Body.Query)
	if err != nil {
		return nil, fmt.Errorf("failed to search players: %w", err)
	}

	result := make([]TabCompleteEntry, len(entries))
	for i, entry := range entries {
		result[i] = TabCompleteEntry{
			Id:       entry.ID,
			Username: entry.Username,
		}
	}
	return &PerformTabComplete200JSONResponse{Result: result}, nil
}

func sendPlayerDataUpdateMessage(w kafkafx.SyncProducer, _ context.Context, msg *model.PlayerDataUpdateMessage) {
	log := zap.S()

	content, err := json.Marshal(msg)
	if err != nil {
		log.Errorw("failed to marshal player data update message", "error", err)
		return
	}

	kafkaRecord := kafka.Message{
		Topic: "player_data_updates",
		Key:   []byte(msg.Id),
		Value: content,
	}

	if err = w.WriteMessages(context.Background(), kafkaRecord); err != nil {
		log.Errorw("failed to write to kafka", "error", err)
	}
}

var (
	orgMapNames = map[string]DisplayNameV2{
		"b571aed9-19f4-4032-9c06-75a4b7cf6c00": {
			DisplayNamePart{Type: "username", Text: "Hollow Cube", Color: "#3895ff"},
		},
	}
	hardcodedBadges = map[string]string{
		/* notmattw */ "aceb326f-da15-45bc-bf2f-11940c21780c": "dev_3",
		/* Ontal */ "ed017f08-fd89-46e2-bba0-495686319801": "mod_3",
		/* SethPRG */ "a3634428-40a0-45b3-8583-a3b5813d64c5": "ct_3",
		/* Tamto */ "b6496267-8dfe-485c-982f-85871ae4cbe4": "ct_3",

		/* ArcaneWarrior */ "3e66e238-ec72-49bb-b9dc-6a8a83d0aae6": "dev_2",
		/* cosrnic */ "702c7a80-bf6f-4fa8-b079-f7931b0ff2f6": "dev_2",
		/* Kha0x */ "f36ce582-48a9-42b8-9956-b883224f9dc0": "mod_2",
		/* YouAreRexist */ "b0045e0e-bb24-424b-b6d0-a64e1d7a73a1": "mod_2",
		/* dude_guy_boy */ "82452dc0-fa2b-42ad-acf3-5236c2afeba4": "mod_2",
		/* caseyclosed */ "d921706f-ba93-4ebb-a0a3-14fbbe6cbead": "mod_2",
		/* chilee */ "849f5535-5e30-4dce-bc2f-6dba16573eb9": "mod_2",
		/* Salad_Cadabra */ "695c36e2-d650-498e-961a-473794296e65": "ct_2",
		/* Ossipago1 */ "47cc8695-2681-4dcd-b772-7eeb8d69c09b": "ct_2",
		/* ThatGravyBoat */ "503450fc-72c2-4e87-8243-94e264977437": "dev_2",
		/* JakeMT04 */ "d9ac68bc-886a-4669-8c7b-d3dc5bf373df": "dev_2",
		/* M1lkys */ "3b5b3f6b-865b-4995-850e-68949836435f": "mod_2",
		/* Expectational */ "8d36737e-1c0a-4a71-87de-9906f577845e": "dev_2",
		/* ThatOneLance */ "212a971c-82fd-47e9-95e7-ddca39781b47": "ct_1",
		/* dgdteftw */ "6681431c-19b7-4e90-a5cb-57f9e724f2b0": "ct_1",
		/* Tado */ "9eeb4bdb-4bce-4cfb-b221-02f790d69600": "ct_2",
		/* FritzAngelos */ "4ea0db0e-626f-4030-ac40-05455ed06963": "ct_2",
		/* deopixel */ "4066a491-ee59-4561-bdef-efd15bf41eef": "ct_2",
		/* Knubby */ "1d8093b3-520c-4be9-8cc0-426e7e539f27": "mod_2",
		/* BeaverMon */ "dd95b882-1dcc-4b8a-b427-c7c080df2dd8": "ct_1",
		/* ashfromupthere */ "c77ae0a8-19fb-46d8-be79-e7cc3d1b0dcb": "ct_2",
		/* NautikSM */ "6863b7e9-6a65-4e2a-9142-23fa99504578": "mod_2",
		/* justcat_ */ "a894acf4-1080-4232-8375-b1e7d2d7fabb": "mod_2",
		/* meoworawr */ "e90ea9ec-080a-401b-8d10-6a53c407ac53": "dev_2",
		/* Devilsta */ "e7b2bef3-7c97-4446-9657-fdbf4f09a0fd": "mod_2",
		/* kimoi_ */ "8df33c65-09a3-4d89-8096-d220ec97a416": "mod_2",
		/* TuxedoLemon */ "e7a98851-920a-49cc-a644-a880f638d14e": "mod_2",
		/* ZuckerFreaks */ "b35a731e-c205-40da-833f-474cbb79d5b5": "mod_2",
		/* Clypes */ "f6eee203-43cc-4d13-973c-f6e44c098d56": "ct_1",
		/* _BoXcat */ "62b4e630-3529-4675-afb0-06a6223d341c": "ct_1",
		/* Robeens */ "b4e0ff6e-c806-4598-a866-249e7ea40cee": "ct_1",
		/* cudsys */ "5f827271-01f8-4591-b688-c478e89b870f": "ct_1",

		/* HammSamichz */ "932f4094-7189-45d9-bf58-70f972ec3e6d": "media",
		/* SandwichLord_ */ "1e2bf44f-122f-4960-a62d-7da9609f52e7": "media",
		/* fruitberries */ "4d2eb015-44af-45ae-9a2c-a6a7bcbae7a4": "media",
		/* iTMG */ "3e5ef799-73bc-44fb-a279-b015c4fb4c84": "media",
		/* Feinberg */ "9a8e24df-4c85-49d6-96a6-951da84fa5c4": "media",
		/* dasnerth */ "82c13d8a-6157-49ae-b66f-85deeaf2e54c": "media",
		/* Antfrost */ "0a28b182-9fb3-4d6e-bd96-fabd2fe6648a": "media",
		/* SlushieVRC */ "3f1561ed-1e33-4269-96e6-770efe13c150": "media",
		/* Kaelan_ */ "f0b2acfc-60ca-42e4-b7a4-640bf8ab2dd4": "media",
		/* AntVenom */ "0c063bfd-3521-413d-a766-50be1d71f00e": "media",
		/* HanabiYaki */ "7952495b-6a93-4e81-8dd0-2e282a68f732": "media",
		/* Evbo_ */ "c08ad74b-ad0b-44b5-8d1b-594d790edfb3": "media",
		/* Purpled */ "1218cdf3-52bd-4e18-ba24-d4b202ec85f3": "media",
		/* Infume */ "a54e3bc4-c635-4b07-a236-b81efbcfe791": "media",
		/* Loefars */ "dac076ab-0e11-436b-9aff-6f985e99df26": "media",
		/* Greninja */ "ac38802e-9eb0-4fb2-ad79-739796e8c5d6": "media",
		/* Picobit */ "e89da8ad-4211-4bc0-9a45-746fdb535309": "media",
	}
	hardcodedColors = map[string]string{
		"dev_3": "#fa4141",
		"dev_2": "#30FBFF",
		"dev_1": "#46FA32",
		"mod_3": "#fa4141",
		"mod_2": "#30FBFF",
		"mod_1": "#46FA32",
		"ct_3":  "#fa4141",
		"ct_2":  "#30FBFF",
		"ct_1":  "#46FA32",
		"media": "#cc39e9",
	}
)

func (s *server) computeDisplayNameV2(ctx context.Context, playerId, playerUsername string) (DisplayNameV2, error) {
	var parts DisplayNameV2
	var nameColor string

	if badge, ok := hardcodedBadges[playerId]; ok {
		nameColor = hardcodedColors[badge]
		parts = append(parts, DisplayNamePart{
			Type: "badge",
			Text: badge,
		})
	} else {
		// try to check hypercube
		//todo this can use a lower spicedb req consistency. High consistency is only relevant for admin/other high ranks.
		hasHypercube, err := s.authzClient.HasHypercube(ctx, playerId, authz.NoKey)
		if err != nil {
			return DisplayNameV2{}, fmt.Errorf("failed to check hypercube status: %w", err)
		}

		if hasHypercube {
			nameColor = "#ffb700"
			parts = append(parts, DisplayNamePart{
				Type: "badge",
				Text: "hypercube/gold",
			})
		}
	}

	// Username
	parts = append(parts, DisplayNamePart{
		Type:  "username",
		Text:  playerUsername,
		Color: nameColor,
	})
	if playerUsername != "" {
		s.nameCache2.Add(playerId, parts)
	}

	return parts, nil
}
