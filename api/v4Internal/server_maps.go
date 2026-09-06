package v4Internal

import (
	"context"
	"fmt"

	"github.com/gohugoio/hashstructure"
	"github.com/hollow-cube/api-server/internal/mapdb"
)

type MapRequest struct {
	MapID string `path:"mapId"`
}

type PaginatedMapHistoryList struct {
	Count   int      `json:"count"`
	Results []string `json:"results"`
}

// GET /players/{playerId}/map-history
func (s *Server) GetPlayerMapHistory(ctx context.Context, request PlayerPaginatedRequest) (*PaginatedMapHistoryList, error) {
	offset, limit := defaultPageParams(request.Page, request.PageSize)

	maps, err := s.mapStore.GetRecentMaps2(ctx, mapdb.GetRecentMaps2Params{
		PlayerID: request.PlayerID,
		Type:     mapdb.SaveStateTypePlaying,
		Offset:   offset,
		Limit:    limit,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch recent maps: %w", err)
	}

	result := PaginatedMapHistoryList{Results: make([]string, len(maps))}
	for i, m := range maps {
		result.Results[i] = m.MapID
		result.Count = int(m.TotalCount)
	}
	return &result, nil
}

// utils

func createMapSearchCacheKey(params *mapdb.SearchMapsParams) (string, bool) {
	if params.Name != nil && *params.Name != "" {
		return "", false // Never cache queries with search text
	}
	hash, err := hashstructure.Hash(params, nil)
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf("maps:search:%d", hash), true
}
