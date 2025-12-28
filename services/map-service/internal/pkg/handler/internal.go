package handler

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/IBM/sarama"
	lru "github.com/hashicorp/golang-lru"
	"github.com/hollow-cube/hc-services/libraries/common/pkg/metric"
	v1 "github.com/hollow-cube/hc-services/services/map-service/api/v1"
	"github.com/hollow-cube/hc-services/services/map-service/internal/db"
	"github.com/hollow-cube/hc-services/services/map-service/internal/pkg/authz"
	"github.com/hollow-cube/hc-services/services/map-service/internal/pkg/model"
	"github.com/hollow-cube/hc-services/services/map-service/internal/pkg/object"
	"github.com/hollow-cube/hc-services/services/map-service/internal/pkg/storage"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/redis/go-redis/v9"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

var mapSearchTime = promauto.NewHistogram(prometheus.HistogramOpts{
	Name: "map_service_map_search_time_seconds",
	Help: "The time it takes to search for maps",
})

type InternalHandler struct {
	log     *zap.SugaredLogger
	metrics metric.Writer

	storageClient   storage.Client
	store           *db.Store
	authzClient     authz.Client
	objectClient    object.Client
	replayStorage   object.Client
	perfdumpStorage object.Client

	redis *redis.Client //todo redis should not be a hard dependency here, i would like to mock it for testing later

	producer sarama.AsyncProducer

	legacyData *legacyDataCache
}

type InternalHandlerParams struct {
	fx.In

	Log     *zap.SugaredLogger
	Metrics metric.Writer

	Storage          storage.Client
	Store            *db.Store
	Authz            authz.Client
	Object           object.Client `name:"object-mapmaker"`
	ReplayStorage    object.Client `name:"object-mapmaker-replays"`
	LegacyMapStorage object.Client `name:"object-mapmaker-legacy-maps"`
	PerfdumpStorage  object.Client `name:"object-mapmaker-perfdumps"`
	MetricWriter     metric.Writer

	Producer sarama.AsyncProducer

	Redis *redis.Client
}

func NewInternalHandler(p InternalHandlerParams) (v1.InternalServer, error) {
	legacyInfoCache, _ := lru.New(100)
	return &InternalHandler{
		log:     p.Log.With("handler", "internal"),
		metrics: p.Metrics,

		storageClient:   p.Storage,
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
	m *model.Map,
	optionalPlayerData *db.MapPlayerData,
) (err error) {
	return SafeWriteMapToDatabase(ctx, h.store, h.authzClient, h.producer, m, optionalPlayerData)
}

func SafeWriteMapToDatabase(
	ctx context.Context,
	store *db.Store,
	authzClient authz.Client,
	producer sarama.AsyncProducer,
	m *model.Map,
	optionalPlayerData *db.MapPlayerData,
) (err error) {

	// Write to DB and permission manager at the same time (2 phase commit)
	err = db.TxNoReturn(ctx, store, func(ctx context.Context, tx *db.Store) error {
		tok, err := authzClient.SetMapOwner(ctx, m.Id, m.Owner)
		if err != nil {
			return fmt.Errorf("authz write failed: %w", err)
		}

		m.AuthzKey = tok
		if err := storageClient.CreateMap(ctx, m); err != nil {
			return fmt.Errorf("db write failed: %w", err)
		}

		if optionalPlayerData != nil {
			err = tx.UpsertPlayerData(ctx, db.UpsertPlayerDataParams{
				ID:            optionalPlayerData.ID,
				UnlockedSlots: optionalPlayerData.UnlockedSlots,
				Maps:          optionalPlayerData.Maps,
				LastPlayedMap: optionalPlayerData.LastPlayedMap,
				LastEditedMap: optionalPlayerData.LastEditedMap,
				ContestSlot:   optionalPlayerData.ContestSlot,
			})
			if err != nil {
				return fmt.Errorf("failed to update player data: %w", err)
			}
		}

		return nil
	})
	if err != nil {
		// Rollback authz update
		if rbErr := authzClient.DeleteMap(ctx, m.Id); rbErr != nil {
			zap.S().Errorw("failed to rollback authz", "err", rbErr)
		}

		return fmt.Errorf("failed to create map: %w", err)
	}

	// Send update to kafka if we updated the player data
	if optionalPlayerData != nil {
		if err = writePlayerDataUpdateMessage(producer, *optionalPlayerData); err != nil {
			return fmt.Errorf("failed to send player data update message: %w", err)
		}
	}

	return
}

func writePlayerDataUpdateMessage(producer sarama.AsyncProducer, pd db.MapPlayerData) error {
	updateMessageData, err := json.Marshal(&model.PlayerDataUpdateMessage{
		Action: model.PlayerDataUpdate_Update,
		Data:   pd,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal player data update message: %w", err)
	}
	producer.Input() <- &sarama.ProducerMessage{
		Topic: model.PlayerDataUpdateTopic,
		Value: sarama.ByteEncoder(updateMessageData),
	}

	return nil
}

func (h *InternalHandler) revokeMapFromSlots(ctx context.Context, id string) error {
	updatedUsers, err := h.store.RemoveMapFromSlots(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to remove map from slots: %w", err)
	}

	for _, pd := range updatedUsers {
		if err = writePlayerDataUpdateMessage(h.producer, pd); err != nil {
			return fmt.Errorf("failed to write player data update message: %w", err)
		}
	}

	return nil
}

func (h *InternalHandler) writeMapUpdate(update *model.MapUpdateMessage) error {
	updateMessageData, err := json.Marshal(update)
	if err != nil {
		return fmt.Errorf("failed to marshal map update message: %w", err)
	}
	h.producer.Input() <- &sarama.ProducerMessage{
		Topic: model.MapUpdateTopic,
		Value: sarama.ByteEncoder(updateMessageData),
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
