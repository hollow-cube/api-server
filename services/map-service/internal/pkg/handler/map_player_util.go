package handler

import (
	"context"

	"github.com/hollow-cube/hc-services/services/map-service/internal/db"
	"github.com/hollow-cube/hc-services/services/map-service/internal/pkg/authz"
)

func (h *InternalHandler) HasFreeMapSlot(ctx context.Context, pd db.MapPlayerData) (bool, error) {
	unlockedSlots, err := h.getUnlockedSlots(ctx, pd)
	if err != nil {
		return false, err
	}
	if len(pd.Map) < unlockedSlots {
		// If the maps array is smaller than unlocked they always have one ready.
		// The loop below will also fail in this case, so it is double good :)
		return true, nil
	}

	for i := 0; i < unlockedSlots; i++ {
		if pd.Map[i] == "" {
			return true, nil
		}
	}

	return false, nil
}

func (h *InternalHandler) AddMapToSlot(ctx context.Context, pd db.MapPlayerData, mapId string, slot int) (bool, error) {
	unlockedSlots, err := h.getUnlockedSlots(ctx, pd)
	if err != nil {
		return false, err
	}
	if slot < 0 || slot >= unlockedSlots {
		return false, nil
	}

	// Resize slice if necessary
	if len(pd.Map) < unlockedSlots {
		pd.Map = append(pd.Map, make([]string, unlockedSlots-len(pd.Map))...)
	}

	// Check if slot is free
	if pd.Map[slot] != "" {
		return false, nil
	}

	pd.Map[slot] = mapId
	return true, nil
}

func (h *InternalHandler) AddMapToFreeSlot(ctx context.Context, pd db.MapPlayerData, mapId string) (int, bool, error) {
	unlockedSlots, err := h.getUnlockedSlots(ctx, pd)
	if err != nil {
		return -1, false, err
	}

	// Resize slice if necessary
	if len(pd.Map) < unlockedSlots {
		pd.Map = append(pd.Map, make([]string, unlockedSlots-len(pd.Map))...)
	}

	for i := 0; i < unlockedSlots; i++ {
		if pd.Map[i] == "" {
			pd.Map[i] = mapId
			return i, true, nil
		}
	}

	return -1, false, nil
}

func (h *InternalHandler) getUnlockedSlots(ctx context.Context, pd db.MapPlayerData) (int, error) {
	slots, err := h.getTotalSlotsFromPerm(ctx, pd)
	if err != nil {
		return 0, err
	}
	return slots, nil
}

func (h *InternalHandler) getTotalSlotsFromPerm(ctx context.Context, pd db.MapPlayerData) (int, error) {
	// This is pretty dumb logic, but uh... oh well.
	state, err := h.authzClient.CheckPlatformPermission(ctx, pd.ID, authz.NoKey, authz.UMapSlot3)
	if err != nil {
		return 0, err
	}
	if state != authz.Allow {
		return 2, nil
	}

	state, err = h.authzClient.CheckPlatformPermission(ctx, pd.ID, authz.NoKey, authz.UMapSlot4)
	if err != nil {
		return 0, err
	}
	if state != authz.Allow {
		return 3, nil
	}

	state, err = h.authzClient.CheckPlatformPermission(ctx, pd.ID, authz.NoKey, authz.UMapSlot5)
	if err != nil {
		return 0, err
	}
	if state != authz.Allow {
		return 4, nil
	}

	return 5, nil
}
