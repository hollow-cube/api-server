package intnl

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hollow-cube/hc-services/services/map-service/internal/db"
	"github.com/hollow-cube/hc-services/services/map-service/internal/pkg/authz"
	"github.com/hollow-cube/hc-services/services/map-service/internal/pkg/model"
	"github.com/segmentio/kafka-go"
	"go.uber.org/zap"
)

func (s *server) ensurePermForMapSize(ctx context.Context, playerId string, size int) (allowed bool, err error) {
	var state authz.State
	switch size {
	case model.MapSizeNormal:
		return true, nil // Always allowed
	case model.MapSizeLarge:
		state, err = s.authzClient.CheckPlatformPermission(ctx, playerId, authz.NoKey, authz.UMapSize2)
	case model.MapSizeMassive:
		state, err = s.authzClient.CheckPlatformPermission(ctx, playerId, authz.NoKey, authz.UMapSize3)
	case model.MapSizeColossal:
		state, err = s.authzClient.CheckPlatformPermission(ctx, playerId, authz.NoKey, authz.UMapSize4)
	default: // Any invalid size, including unlimited which can not be set by users.
		return false, fmt.Errorf("invalid map size: %d", size)
	}
	if err != nil {
		return false, err
	}
	if state != authz.Allow {
		return false, nil
	}
	return true, nil
}

func (s *server) hasFreeMapSlot(ctx context.Context, pd db.MapPlayerData) (bool, error) {
	unlockedSlots, err := s.getUnlockedSlots(ctx, pd)
	if err != nil {
		return false, err
	}
	if len(pd.Maps) < unlockedSlots {
		// If the maps array is smaller than unlocked they always have one ready.
		// The loop below will also fail in this case, so it is double good :)
		return true, nil
	}

	for i := 0; i < unlockedSlots; i++ {
		if pd.Maps[i] == "" {
			return true, nil
		}
	}

	return false, nil
}

func (s *server) addMapToSlot(ctx context.Context, pd db.MapPlayerData, mapId string, slot int) (bool, error) {
	unlockedSlots, err := s.getUnlockedSlots(ctx, pd)
	if err != nil {
		return false, err
	}
	if slot < 0 || slot >= unlockedSlots {
		return false, nil
	}

	// Resize slice if necessary
	if len(pd.Maps) < unlockedSlots {
		pd.Maps = append(pd.Maps, make([]string, unlockedSlots-len(pd.Maps))...)
	}

	// Check if slot is free
	if pd.Maps[slot] != "" {
		return false, nil
	}

	pd.Maps[slot] = mapId
	return true, nil
}

func (s *server) addMapToFreeSlot(ctx context.Context, pd db.MapPlayerData, mapId string) (int, bool, error) {
	unlockedSlots, err := s.getUnlockedSlots(ctx, pd)
	if err != nil {
		return -1, false, err
	}

	// Resize slice if necessary
	if len(pd.Maps) < unlockedSlots {
		pd.Maps = append(pd.Maps, make([]string, unlockedSlots-len(pd.Maps))...)
	}

	for i := 0; i < unlockedSlots; i++ {
		if pd.Maps[i] == "" {
			pd.Maps[i] = mapId
			return i, true, nil
		}
	}

	return -1, false, nil
}

func (s *server) revokeMapFromSlots(ctx context.Context, id string) error {
	updatedUsers, err := s.store.RemoveMapFromSlots(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to remove map from slots: %w", err)
	}

	for _, pd := range updatedUsers {
		if err = s.writePlayerDataUpdateMessage(pd); err != nil {
			return fmt.Errorf("failed to write player data update message: %w", err)
		}
	}

	return nil
}

func (s *server) getUnlockedSlots(ctx context.Context, pd db.MapPlayerData) (int, error) {
	slots, err := s.getTotalSlotsFromPerm(ctx, pd)
	if err != nil {
		return 0, err
	}
	return slots, nil
}

func (s *server) getTotalSlotsFromPerm(ctx context.Context, pd db.MapPlayerData) (int, error) {
	// This is pretty dumb logic, but uh... oh well.
	state, err := s.authzClient.CheckPlatformPermission(ctx, pd.ID, authz.NoKey, authz.UMapSlot3)
	if err != nil {
		return 0, err
	}
	if state != authz.Allow {
		return 2, nil
	}

	state, err = s.authzClient.CheckPlatformPermission(ctx, pd.ID, authz.NoKey, authz.UMapSlot4)
	if err != nil {
		return 0, err
	}
	if state != authz.Allow {
		return 3, nil
	}

	state, err = s.authzClient.CheckPlatformPermission(ctx, pd.ID, authz.NoKey, authz.UMapSlot5)
	if err != nil {
		return 0, err
	}
	if state != authz.Allow {
		return 4, nil
	}

	return 5, nil
}

func (s *server) safeWriteMapToDatabase(ctx context.Context, mapParams db.CreateMapParams, optionalPlayerData *db.MapPlayerData) (*db.Map, error) {

	// Write to DB and permission manager at the same time (2 phase commit)
	m, err := db.Tx(ctx, s.store, func(ctx context.Context, tx *db.Store) (*db.Map, error) {
		_, err := s.authzClient.SetMapOwner(ctx, mapParams.ID, mapParams.Owner)
		if err != nil {
			return nil, fmt.Errorf("authz write failed: %w", err)
		}

		m, err := tx.CreateMap(ctx, mapParams)
		if err != nil {
			return nil, fmt.Errorf("db write failed: %w", err)
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
				return nil, fmt.Errorf("failed to update player data: %w", err)
			}
		}

		return m, nil
	})
	if err != nil {
		// Rollback authz update
		if rbErr := s.authzClient.DeleteMap(ctx, m.Id); rbErr != nil {
			zap.S().Errorw("failed to rollback authz", "err", rbErr)
		}

		return fmt.Errorf("failed to create map: %w", err)
	}

	// Send update to kafka if we updated the player data
	if optionalPlayerData != nil {
		if err = s.writePlayerDataUpdateMessage(*optionalPlayerData); err != nil {
			return fmt.Errorf("failed to send player data update message: %w", err)
		}
	}

	return
}

func (s *server) writePlayerDataUpdateMessage(pd db.MapPlayerData) error {
	updateMessageData, err := json.Marshal(&model.PlayerDataUpdateMessage{
		Action: model.PlayerDataUpdate_Update,
		Data:   pd,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal player data update message: %w", err)
	}
	go s.producer.WriteMessages(context.Background(), kafka.Message{
		Topic: model.PlayerDataUpdateTopic,
		Key:   []byte(pd.ID),
		Value: updateMessageData,
	})

	return nil
}

func (s *server) writeMapUpdate(update *model.MapUpdateMessage) error {
	updateMessageData, err := json.Marshal(update)
	if err != nil {
		return fmt.Errorf("failed to marshal map update message: %w", err)
	}
	go s.producer.WriteMessages(context.Background(), kafka.Message{
		Topic: model.MapUpdateTopic,
		Key:   []byte(update.ID),
		Value: updateMessageData,
	})
	return nil
}

func (s *server) clearCachedSearches(ctx context.Context) {
	cachedKeys, err := s.redis.Do(ctx, s.redis.B().Keys().Pattern("maps:search:*").Build()).AsStrSlice()
	if err != nil || len(cachedKeys) == 0 {
		return // DNC about error, we tried our best
	}
	err = s.redis.Do(ctx, s.redis.B().Del().Key(cachedKeys...).Build()).Error()
	if err != nil {
		s.log.Errorw("failed to clear cached map searches", "err", err)
	}
}
