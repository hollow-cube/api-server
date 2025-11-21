//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen -o server.gen.go -package intnl -generate types,strict-server,std-http-server openapi.yaml
//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen -o client.gen.go -package intnl -generate client openapi.yaml

package intnl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/hollow-cube/hc-services/libraries/common/pkg/metric"
	"github.com/hollow-cube/hc-services/services/player-service/config"
	"github.com/hollow-cube/hc-services/services/player-service/internal/pkg/authz"
	"github.com/hollow-cube/hc-services/services/player-service/internal/pkg/model"
	"github.com/hollow-cube/hc-services/services/player-service/internal/pkg/storage"
	"github.com/hollow-cube/hc-services/services/player-service/internal/pkg/util"
	"github.com/hollow-cube/hc-services/services/player-service/internal/pkg/wkafka"
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
	Producer   wkafka.SyncWriter
	TBHeadless *tebex.HeadlessClient

	Storage           storage.Client
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
		storageClient:     p.Storage,
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

	storageClient storage.Client
	authzClient   authz.Client
	producer      wkafka.Writer
	tbHeadless    *tebex.HeadlessClient

	punishmentLadders map[string]*model.PunishmentLadder
	punishmentAliases map[model.PunishmentType]map[string]*model.PunishmentLadder

	//todo names are never evicted from this, should use redis instead and perhaps LRU on server if needed.
	nameCache2 *lru.Cache[string, DisplayNameV2]
}

func (s *server) GetPlayerData(ctx context.Context, request GetPlayerDataRequestObject) (GetPlayerDataResponseObject, error) {
	p, err := s.storageClient.GetPlayerData(ctx, util.RemapUUID(request.PlayerId))
	apiPlayer, err := s.playerDataToAPIWithName(p, err, ctx)
	if err != nil {
		return nil, err
	} else if apiPlayer == nil {
		return PlayerNotFoundResponse{}, nil
	}
	return GetPlayerData200JSONResponse(*apiPlayer), nil
}

func (s *server) CreatePlayerData(ctx context.Context, request CreatePlayerDataRequestObject) (CreatePlayerDataResponseObject, error) {
	time_0 := time.UnixMilli(0)
	p := &model.PlayerData{
		Id:             request.Body.Id,
		Username:       request.Body.Username,
		FirstJoin:      time_0,
		LastOnline:     time_0,
		Settings:       make(model.PlayerSettings),
		LinkedAccounts: []model.LinkedAccount{},
	}

	err := s.storageClient.CreatePlayerData(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("failed to create player data: %w", err)
	}

	err = s.storageClient.AddPlayerIP(ctx, p.Id, request.Body.Ip)
	if err != nil {
		return nil, fmt.Errorf("failed to add player ip: %w", err)
	}

	displayName2, err := s.computeDisplayNameV2(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("failed to compute display name 2: %w", err)
	}

	go s.metrics.Write(&model.NewPlayer{PlayerId: p.Id})

	return CreatePlayerData201JSONResponse(*playerDataToAPI(p, displayName2, false, nil)), nil
}

func (s *server) UpdatePlayerData(ctx context.Context, request UpdatePlayerDataRequestObject) (UpdatePlayerDataResponseObject, error) {
	p, err := s.storageClient.GetPlayerData(ctx, request.PlayerId)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return PlayerNotFoundResponse{}, nil
		}

		return nil, fmt.Errorf("failed to get player data: %w", err)
	}

	var changed bool
	updates := request.Body
	if updates.Username != nil && *updates.Username != p.Username {
		p.Username = *updates.Username
		changed = true
	}
	if updates.LastOnline != nil {
		p.LastOnline = *updates.LastOnline

		// assign the join date here
		if p.FirstJoin.UnixMilli() == 0 {
			p.FirstJoin = *updates.LastOnline
		}
		changed = true
	}
	if updates.PlaytimeInc != nil {
		p.Playtime += *updates.PlaytimeInc
		changed = true
	}
	if updates.IpHistory != nil && len(*updates.IpHistory) > 0 {
		for _, ip := range *updates.IpHistory {
			if ip == "" {
				continue
			}

			if err = s.storageClient.AddPlayerIP(ctx, p.Id, ip); err != nil {
				return nil, fmt.Errorf("failed to record player ip: %w", err)
			}
		}
		changed = true
	}
	if updates.BetaEnabled != nil {
		p.BetaEnabled = *updates.BetaEnabled
		changed = true
	}
	if updates.SettingsUpdates != nil {
		for key, value := range *updates.SettingsUpdates {
			p.Settings[key] = value
			changed = true
		}
	}

	if !changed {
		return UpdatePlayerData200Response{}, nil
	}

	err = s.storageClient.UpdatePlayerData(ctx, p)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return PlayerNotFoundResponse{}, nil
		}

		return nil, fmt.Errorf("failed to update player data: %w", err)
	}

	return UpdatePlayerData200Response{}, nil
}

func (s *server) GetPlayerBackpack(_ context.Context, _ GetPlayerBackpackRequestObject) (GetPlayerBackpackResponseObject, error) {
	// todo: reenable these values when adding back experience and coins
	//bp, err := h.storageClient.GetPlayerBackpack(ctx, playerId)
	//if err != nil {
	//	if errors.Is(err, storage.ErrNotFound) {
	//		return nil, v1.ErrPlayerDataNotFound
	//	}
	//
	//	return nil, fmt.Errorf("failed to get player backpack: %w", err)
	//}

	return GetPlayerBackpack200JSONResponse{}, nil
}

func (s *server) GetPlayerCosmetics(ctx context.Context, request GetPlayerCosmeticsRequestObject) (GetPlayerCosmeticsResponseObject, error) {
	cosmetics, err := s.storageClient.GetUnlockedCosmetics(ctx, request.PlayerId)
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
	p, err := s.storageClient.GetPlayerData(ctx, request.PlayerId)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return GetPlayerDisplayNameV2404Response{}, nil
		}

		return nil, fmt.Errorf("failed to get player data: %w", err)
	}

	displayName, err := s.computeDisplayNameV2(ctx, p)
	if err != nil {
		return nil, err
	}
	return GetPlayerDisplayNameV2200JSONResponse(displayName), nil
}

func (s *server) GetPlayerAlts(ctx context.Context, request GetPlayerAltsRequestObject) (GetPlayerAltsResponseObject, error) {
	playerIPs, err := s.storageClient.GetPlayerIPs(ctx, request.PlayerId)
	if err != nil {
		return nil, err
	}

	sharedPlayers, err := s.storageClient.GetPlayersByIPs(ctx, playerIPs)
	if err != nil {
		return nil, err
	}

	results := make([]PlayerAltsItem, 0, 10)
	for _, player := range sharedPlayers {
		if player.Id == request.PlayerId {
			continue
		}

		results = append(results, PlayerAltsItem{
			Id:       player.Id,
			Username: player.Username,
		})
	}

	return GetPlayerAlts200JSONResponse{Results: results}, nil
}

func (s *server) GetPlayerId(ctx context.Context, request GetPlayerIdRequestObject) (GetPlayerIdResponseObject, error) {
	pid, err := s.storageClient.LookupPlayerByIdOrUsername(ctx, request.IdOrUsername)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return PlayerNotFoundResponse{}, nil
		}

		return nil, fmt.Errorf("failed to lookup player: %w", err)
	}

	return GetPlayerId200TextResponse(pid), nil
}

func (s *server) PerformTabComplete(ctx context.Context, request PerformTabCompleteRequestObject) (PerformTabCompleteResponseObject, error) {
	if request.Body.Query == "" {
		return PerformTabComplete200JSONResponse{Result: []TabCompleteEntry{}}, nil
	}

	entries, err := s.storageClient.SearchPlayersFuzzy(ctx, request.Body.Query)
	if err != nil {
		return nil, fmt.Errorf("failed to search players: %w", err)
	}

	result := make([]TabCompleteEntry, len(entries))
	for i, entry := range entries {
		result[i] = TabCompleteEntry{
			Id:       entry.Id,
			Username: entry.Username,
		}
	}
	return &PerformTabComplete200JSONResponse{Result: result}, nil
}

func sendPlayerDataUpdateMessage(w wkafka.Writer, _ context.Context, msg *model.PlayerDataUpdateMessage) {
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

func (s *server) playerDataToAPIWithName(p *model.PlayerData, err error, ctx context.Context) (*PlayerData, error) { // abstracted to reduce boilerplate
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, nil
		}

		return nil, fmt.Errorf("failed to get player data: %w", err)
	}

	var hypercubeTime *time.Time
	hasHypercube, err := s.authzClient.HasHypercube(ctx, p.Id, authz.NoKey)
	if err != nil {
		return nil, fmt.Errorf("failed to check hypercube status: %w", err)
	}
	if hasHypercube {
		// Try to get the time, though there may not be an entry because some people get it implicitly
		// from other relationships. In that case we should simply grant it for a year from now.
		hcStartTime, hcTerm, err := s.authzClient.GetHypercubeStats(ctx, p.Id, authz.NoKey)
		if errors.Is(err, authz.ErrNotFound) {
			// Implicit grant
			temp := time.Now().Add(365 * 24 * time.Hour)
			hypercubeTime = &temp
		} else if err == nil {
			temp := hcStartTime.Add(hcTerm)
			if time.Now().After(temp) {
				// This is also an implicit grant case, kinda gross but oh well
				temp = time.Now().Add(365 * 24 * time.Hour)
			}
			hypercubeTime = &temp
		} else {
			return nil, fmt.Errorf("failed to check hypercube time: %w", err)
		}
	}

	var ok bool
	var displayName2 DisplayNameV2
	if displayName2, ok = s.nameCache2.Get(p.Id); !ok || true { //todo temporarily disable display name v2 cache
		displayName2, err = s.computeDisplayNameV2(ctx, p)
		if err != nil {
			return nil, fmt.Errorf("failed to compute display name 2: %w", err)
		}
	}

	// Can test empty code to see if TOTP is disabled
	_, err = s.testTotpCode(ctx, p.Id, "", false)
	totpEnabled := true
	if errors.Is(err, errNotConfigured) {
		totpEnabled = false
	} else if err != nil {
		return nil, fmt.Errorf("failed to check totp status: %w", err)
	}

	// todo: reenable these values when adding back experience and coins
	return playerDataToAPI(p, displayName2, totpEnabled, hypercubeTime), nil
}

func playerDataToAPI(p *model.PlayerData, displayName2 DisplayNameV2, totpEnabled bool, hypercubeTime *time.Time) *PlayerData {
	var settings map[string]interface{}
	if p.Settings != nil {
		settings = p.Settings
	} else {
		settings = make(map[string]interface{})
	}

	return &PlayerData{
		Id:            p.Id,
		Username:      p.Username,
		DisplayNameV2: displayName2,
		FirstJoin:     p.FirstJoin,
		LastOnline:    p.LastOnline,
		Playtime:      p.Playtime,
		Settings:      settings,
		//Experience:    int64(p.Experience),
		Experience:  int64(0),
		BetaEnabled: p.BetaEnabled,

		Coins: 0,
		//Coins:  p.Coins,
		Cubits:         p.Cubits,
		HypercubeUntil: hypercubeTime,

		TotpEnabled: totpEnabled,
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
		/* xLaurenRose */ "090c4e2a-37ff-4fae-88bb-c4de51c37776": "mod_2",
		/* TuxedoLemon */ "e7a98851-920a-49cc-a644-a880f638d14e": "mod_2",
		/* Clypes */ "f6eee203-43cc-4d13-973c-f6e44c098d56": "ct_1",
		/* _BoXcat */ "62b4e630-3529-4675-afb0-06a6223d341c": "ct_1",
		/* Robeens */ "b4e0ff6e-c806-4598-a866-249e7ea40cee": "ct_1",

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

func (s *server) computeDisplayNameV2(ctx context.Context, p *model.PlayerData) (DisplayNameV2, error) {
	var parts DisplayNameV2
	var nameColor string

	if badge, ok := hardcodedBadges[p.Id]; ok {
		nameColor = hardcodedColors[badge]
		parts = append(parts, DisplayNamePart{
			Type: "badge",
			Text: badge,
		})
	} else {
		// try to check hypercube
		//todo this can use a lower spicedb req consistency. High consistency is only relevant for admin/other high ranks.
		hasHypercube, err := s.authzClient.HasHypercube(ctx, p.Id, authz.NoKey)
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
		Text:  p.Username,
		Color: nameColor,
	})
	if p.Username != "" {
		s.nameCache2.Add(p.Id, parts)
	}

	return parts, nil
}
