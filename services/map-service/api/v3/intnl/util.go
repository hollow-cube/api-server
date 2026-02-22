package intnl

import (
	"context"
	"fmt"

	"github.com/hollow-cube/hc-services/services/map-service/internal/db"
	"github.com/hollow-cube/hc-services/services/map-service/internal/pkg/model"
)

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

		unlockedSlots, err := s.getUnlockedSlots(ctx, pd)
		if err != nil {
			return -1, err
		}
		if len(existing) >= unlockedSlots {
			return -1, fmt.Errorf("no free slots")
		}

		// They have at least one available slot, ok to insert
		return -1, nil
	}

	// Try to insert into specific given slot (must be free)
	if pd.Map[*slot] != "" {
		return -1, fmt.Errorf("slot %d is already occupied", *slot)
	}

	return *slot, nil
}

func (s *server) getUnlockedSlots(ctx context.Context, pd *db.MapPlayerData) (int, error) {
	slots, err := s.getTotalSlotsFromPerm(ctx, pd)
	if err != nil {
		return 0, err
	}
	return slots, nil
}

func (s *server) getTotalSlotsFromPerm(ctx context.Context, pd *db.MapPlayerData) (int, error) {
	resp, err := s.players.GetPlayerDataWithResponse(ctx, pd.ID)
	if err != nil {
		return 0, err
	}

	return resp.JSON200.MapSlots, nil
}

func (s *server) writeMapUpdate(ctx context.Context, update *model.MapUpdateMessage) error {
	if err := s.jetStream.PublishJSONAsync(ctx, update); err != nil {
		return fmt.Errorf("failed to publish map update message: %w", err)
	}

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
