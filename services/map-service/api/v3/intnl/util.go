package intnl

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hollow-cube/hc-services/libraries/common/pkg/kafkafx"
	"github.com/hollow-cube/hc-services/services/map-service/internal/db"
	"github.com/hollow-cube/hc-services/services/map-service/internal/pkg/authz"
	"github.com/hollow-cube/hc-services/services/map-service/internal/pkg/model"
	"github.com/segmentio/kafka-go"
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

func (s *server) getMapSlotIndex(ctx context.Context, pd *db.MapPlayerData, mapId string, slot *int) (int, error) {
	if slot == nil {
		// First available slot
		unlockedSlots, err := s.getUnlockedSlots(ctx, pd)
		if err != nil {
			return -1, err
		}

		for i := 0; i < unlockedSlots; i++ {
			if pd.Map[i] == "" {
				pd.Map[i] = mapId
				return i, nil
			}
		}

		return -1, fmt.Errorf("no available slots")
	} else if *slot == -1 {
		// New system, insert as -1 if they have space
		existing, err := s.store.GetMapSlots(ctx, pd.ID)
		if err != nil {
			return -1, err
		}

		_ = existing
		// TODO: reenable
		//if len(existing) >= pd.UnlockedSlots {
		//	return -1, fmt.Errorf("no free slots")
		//}

		// They have at least one available slot, ok to insert
		return -1, nil
	}

	// Try to insert into specific given slot (must be free)
	if pd.Map[*slot] != "" {
		return -1, fmt.Errorf("slot %d is already occupied", *slot)
	}

	return *slot, nil
}

func (s *server) revokeMapFromSlots(ctx context.Context, id string) error {
	updatedUsers, err := s.store.RemoveMapFromSlots(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to remove map from slots: %w", err)
	}

	for _, playerId := range updatedUsers {
		if err = s.writePlayerDataUpdateMessage(ctx, playerId); err != nil {
			return fmt.Errorf("failed to write player data update message: %w", err)
		}
	}

	return nil
}

func (s *server) getUnlockedSlots(ctx context.Context, pd *db.MapPlayerData) (int, error) {
	slots, err := s.getTotalSlotsFromPerm(ctx, pd)
	if err != nil {
		return 0, err
	}
	return slots, nil
}

func (s *server) getTotalSlotsFromPerm(ctx context.Context, pd *db.MapPlayerData) (int, error) {
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

func (s *server) writePlayerDataUpdateMessage(ctx context.Context, playerId string) error {
	// Read the current value always
	pd, err := s.GetMapPlayerDataWithIndexedSlots(ctx, playerId)
	if err != nil {
		return err
	}

	updateMessageData, err := json.Marshal(&model.PlayerDataUpdateMessage{
		Action: model.PlayerDataUpdate_Update,
		Data:   pd,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal player data update message: %w", err)
	}
	go s.producer.WriteMessages(context.Background(), kafka.Message{
		Topic: kafkafx.TopicMapPlayerDataUpdate,
		Key:   []byte(playerId),
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
		Topic: kafkafx.TopicMapUpdate,
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
