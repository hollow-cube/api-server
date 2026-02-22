//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen -o server.gen.go -package intnl -generate types,strict-server,std-http-server openapi.yaml
//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen -o client.gen.go -package intnl -generate client openapi.yaml

package intnl

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hollow-cube/hc-services/libraries/common/pkg/metric"
	"github.com/hollow-cube/hc-services/libraries/common/pkg/natsutil"
	"github.com/hollow-cube/hc-services/services/player-service/api/auth"
	"github.com/hollow-cube/hc-services/services/player-service/config"
	"github.com/hollow-cube/hc-services/services/player-service/internal/db"
	"github.com/hollow-cube/hc-services/services/player-service/internal/pkg/model"
	"github.com/hollow-cube/hc-services/services/player-service/internal/pkg/util"
	"github.com/hollow-cube/tebex-go"
	"github.com/nats-io/nats.go/jetstream"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

var _ StrictServerInterface = (*server)(nil)

type ServerParams struct {
	fx.In

	Log        *zap.SugaredLogger
	Config     *config.Config
	Metrics    metric.Writer
	JetStream  *natsutil.JetStreamWrapper
	TBHeadless *tebex.HeadlessClient

	Store             *db.Store
	PunishmentLadders map[string]*model.PunishmentLadder
}

func NewServer(p ServerParams) (StrictServerInterface, error) {
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

	err := p.JetStream.UpsertStream(context.Background(), jetstream.StreamConfig{
		Name:       "PUNISHMENTS",
		Subjects:   []string{"punishment.>"},
		Retention:  jetstream.LimitsPolicy,
		Storage:    jetstream.FileStorage,
		MaxAge:     10 * time.Minute,
		Duplicates: 60 * time.Second,
	})
	if err != nil {
		return nil, err
	}

	err = p.JetStream.UpsertStream(context.Background(), jetstream.StreamConfig{
		Name:       "NOTIFICATIONS",
		Subjects:   []string{"notification.>"},
		Retention:  jetstream.LimitsPolicy,
		Storage:    jetstream.FileStorage,
		MaxAge:     10 * time.Minute,
		Duplicates: 60 * time.Second,
	})
	if err != nil {
		return nil, err
	}

	err = p.JetStream.UpsertStream(context.Background(), jetstream.StreamConfig{
		Name:       "PLAYER_DATA_MANAGEMENT",
		Subjects:   []string{"player-data.>"},
		Retention:  jetstream.LimitsPolicy,
		Storage:    jetstream.FileStorage,
		MaxAge:     10 * time.Minute,
		Duplicates: 60 * time.Second,
	})
	if err != nil {
		return nil, err
	}

	return &server{
		log:               p.Log.With("handler", "internal"),
		metrics:           p.Metrics,
		store:             p.Store,
		jetStream:         p.JetStream,
		tbHeadless:        p.TBHeadless,
		punishmentLadders: p.PunishmentLadders,
		punishmentAliases: punishmentAliases,
	}, nil
}

type server struct {
	log     *zap.SugaredLogger
	metrics metric.Writer

	store      *db.Store
	jetStream  *natsutil.JetStreamWrapper
	tbHeadless *tebex.HeadlessClient

	punishmentLadders map[string]*model.PunishmentLadder
	punishmentAliases map[model.PunishmentType]map[string]*model.PunishmentLadder
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

	pd, err := s.store.GetPlayerData(ctx, request.PlayerId)
	if err != nil {
		if errors.Is(err, db.ErrNoRows) {
			return GetPlayerDisplayNameV2404Response{}, nil
		}

		return nil, fmt.Errorf("failed to get player data: %w", err)
	}

	displayName := computeDisplayNameV2(pd)
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

func (s *server) sendPlayerDataUpdateMessage(ctx context.Context, msg *model.PlayerDataUpdateMessage) error {
	if err := s.jetStream.PublishJSONAsync(ctx, msg); err != nil {
		return fmt.Errorf("failed to publish player data update message: %w", err)
	}

	return nil
}

var (
	orgMapNames = map[string]DisplayNameV2{
		"b571aed9-19f4-4032-9c06-75a4b7cf6c00": {
			DisplayNamePart{Type: "username", Text: "Hollow Cube", Color: "#3895ff"},
		},
	}
)

func computeDisplayNameV2(pd db.PlayerData) DisplayNameV2 {
	var parts DisplayNameV2

	role := pd.EffectiveRole()
	if role.Badge() != "" {
		parts = append(parts, DisplayNamePart{
			Type: "badge",
			Text: role.Badge(),
		})
	}

	parts = append(parts, DisplayNamePart{
		Type:  "username",
		Text:  pd.Username,
		Color: role.Color(),
	})

	return parts
}
