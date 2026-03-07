package v4Internal

import (
	"context"
	"fmt"
	"slices"

	"github.com/hollow-cube/api-server/internal/mapdb"
)

type GetMapSlotsResponse struct {
	Results []MapSlot `json:"results"`
}

// GET /players/{playerId}/map-slots
func (s *Server) GetMapPlayerSlots(ctx context.Context, request PlayerRequest) (*GetMapSlotsResponse, error) {
	// Get maps from existing slots
	slots, err := s.mapStore.GetMapSlots(ctx, request.PlayerId)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch map slots: %w", err)
	}
	mapIds := make([]string, len(slots))
	for i, slot := range slots {
		mapIds[i] = slot.MapID
	}
	maps, err := s.mapStore.MultiGetMapWithTagsById(ctx, mapIds)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch maps: %w", err)
	}

	// Get all published maps
	published, err := s.mapStore.SearchMaps(ctx, mapdb.SearchMapsParams{
		Variants:  []string{"parkour", "building"},
		Owner:     &request.PlayerId,
		Sort:      "published",
		SortOrder: "desc",
		Page:      0,
		PageSize:  1000000, // we have 'no limit' at home
	})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch published maps: %w", err)
	}

	results := make([]MapSlot, 0, len(slots)+len(published))
	for _, slot := range slots {
		for _, m := range maps {
			if m.Map.ID != slot.MapID {
				continue
			}

			// TODO: builder slots should have the created time set to the acceptance time probably
			results = append(results, MapSlot{
				Map:       hydrateMap(m.Map, m.Tags),
				CreatedAt: slot.CreatedAt,
			})
		}
	}
	for _, m := range published {
		results = append(results, MapSlot{
			Map:       hydratePublishedMap(m.PublishedMap),
			CreatedAt: *m.PublishedMap.PublishedAt,
		})
	}

	slices.SortFunc(results, func(a, b MapSlot) int {
		return a.CreatedAt.Compare(b.CreatedAt)
	})

	return &GetMapSlotsResponse{Results: results}, nil
}
