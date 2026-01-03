package handler

import (
	"context"
	"encoding/json"
	"fmt"

	lru "github.com/hashicorp/golang-lru"
	"github.com/hollow-cube/hc-services/libraries/common/pkg/kafkafx"
	"github.com/hollow-cube/hc-services/libraries/common/pkg/metric"
	v1 "github.com/hollow-cube/hc-services/services/map-service/api/v1"
	"github.com/hollow-cube/hc-services/services/map-service/internal/db"
	"github.com/hollow-cube/hc-services/services/map-service/internal/pkg/authz"
	"github.com/hollow-cube/hc-services/services/map-service/internal/pkg/model"
	"github.com/hollow-cube/hc-services/services/map-service/internal/pkg/object"
	"github.com/redis/go-redis/v9"
	"github.com/segmentio/kafka-go"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

type InternalHandler struct {
	log     *zap.SugaredLogger
	metrics metric.Writer

	store           *db.Store
	authzClient     authz.Client
	objectClient    object.Client
	replayStorage   object.Client
	perfdumpStorage object.Client

	redis *redis.Client //todo redis should not be a hard dependency here, i would like to mock it for testing later

	producer kafkafx.AsyncProducer

	legacyData *legacyDataCache
}

type InternalHandlerParams struct {
	fx.In

	Log     *zap.SugaredLogger
	Metrics metric.Writer

	Store            *db.Store
	Authz            authz.Client
	Object           object.Client `name:"object-mapmaker"`
	ReplayStorage    object.Client `name:"object-mapmaker-replays"`
	LegacyMapStorage object.Client `name:"object-mapmaker-legacy-maps"`
	PerfdumpStorage  object.Client `name:"object-mapmaker-perfdumps"`
	MetricWriter     metric.Writer

	Producer kafkafx.AsyncProducer

	Redis *redis.Client
}

func NewInternalHandler(p InternalHandlerParams) (v1.InternalServer, error) {
	legacyInfoCache, _ := lru.New(100)
	return &InternalHandler{
		log:     p.Log.With("handler", "internal"),
		metrics: p.Metrics,

		store:           p.Store,
		authzClient:     p.Authz,
		objectClient:    p.Object,
		replayStorage:   p.ReplayStorage,
		perfdumpStorage: p.PerfdumpStorage,

		producer: p.Producer,
		redis:    p.Redis,

		legacyData: &legacyDataCache{
			client: p.LegacyMapStorage,
			infos:  legacyInfoCache,
		},
	}, nil
}

func (h *InternalHandler) safeWriteMapToDatabase(
	ctx context.Context,
	mapParams db.CreateMapParams,
	optionalPlayerData *db.MapPlayerData,
) (db.Map, error) {
	return SafeWriteMapToDatabase(ctx, h.store, h.authzClient, h.producer, mapParams, optionalPlayerData)
}

func SafeWriteMapToDatabase(
	ctx context.Context,
	store *db.Store,
	authzClient authz.Client,
	producer kafkafx.AsyncProducer,
	mapParams db.CreateMapParams,
	optionalPlayerData *db.MapPlayerData,
) (db.Map, error) {
	// Write to DB and permission manager at the same time (2 phase commit)
	m, err := db.Tx(ctx, store, func(ctx context.Context, tx *db.Store) (db.Map, error) {
		_, err := authzClient.SetMapOwner(ctx, mapParams.ID, mapParams.Owner)
		if err != nil {
			return db.Map{}, fmt.Errorf("authz write failed: %w", err)
		}

		m, err := tx.CreateMap(ctx, mapParams)
		if err != nil {
			return db.Map{}, fmt.Errorf("db write failed: %w", err)
		}

		if optionalPlayerData != nil {
			err = tx.UpsertPlayerData(ctx, db.UpsertPlayerDataParams{
				ID:            optionalPlayerData.ID,
				UnlockedSlots: optionalPlayerData.UnlockedSlots,
				//Map:           optionalPlayerData.Map,
				LastPlayedMap: optionalPlayerData.LastPlayedMap,
				LastEditedMap: optionalPlayerData.LastEditedMap,
				ContestSlot:   optionalPlayerData.ContestSlot,
			})
			if err != nil {
				return db.Map{}, fmt.Errorf("failed to update player data: %w", err)
			}
		}

		return m, nil
	})
	if err != nil {
		// Rollback authz update
		if rbErr := authzClient.DeleteMap(ctx, mapParams.ID); rbErr != nil {
			zap.S().Errorw("failed to rollback authz", "err", rbErr)
		}

		return db.Map{}, fmt.Errorf("failed to create map: %w", err)
	}

	// Send update to kafka if we updated the player data
	if optionalPlayerData != nil {
		if err = writePlayerDataUpdateMessage(ctx, producer, *optionalPlayerData); err != nil {
			return db.Map{}, fmt.Errorf("failed to send player data update message: %w", err)
		}
	}

	return m, nil
}

func writePlayerDataUpdateMessage(ctx context.Context, producer kafkafx.AsyncProducer, pd db.MapPlayerData) error {
	updateMessageData, err := json.Marshal(&model.PlayerDataUpdateMessage{
		Action: model.PlayerDataUpdate_Update,
		Data:   pd,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal player data update message: %w", err)
	}
	if err := producer.WriteMessages(ctx, kafka.Message{
		Topic: model.PlayerDataUpdateTopic,
		Value: updateMessageData,
	}); err != nil {
		return fmt.Errorf("failed to write player data update message: %w", err)
	}

	return nil
}

func (h *InternalHandler) revokeMapFromSlots(ctx context.Context, id string) error {
	updatedUsers, err := h.store.RemoveMapFromSlots(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to remove map from slots: %w", err)
	}

	for _, playerId := range updatedUsers {
		_ = playerId
		//if err = writePlayerDataUpdateMessage(ctx, h.producer, playerId); err != nil {
		//	return fmt.Errorf("failed to write player data update message: %w", err)
		//}
	}

	return nil
}

func (h *InternalHandler) writeMapUpdate(ctx context.Context, update *model.MapUpdateMessage) error {
	updateMessageData, err := json.Marshal(update)
	if err != nil {
		return fmt.Errorf("failed to marshal map update message: %w", err)
	}

	if err := h.producer.WriteMessages(ctx, kafka.Message{
		Topic: model.MapUpdateTopic,
		Value: updateMessageData,
	}); err != nil {
		return fmt.Errorf("failed to write map update message: %w", err)
	}

	return nil
}

func (h *InternalHandler) clearCachedSearches(ctx context.Context) {
	cachedKeys := h.redis.Keys(ctx, "maps:search:*").Val()
	if len(cachedKeys) == 0 {
		return
	}
	err := h.redis.Del(ctx, cachedKeys...).Err()
	if err != nil {
		h.log.Errorw("failed to clear cached map searches", "err", err)
	}
}
