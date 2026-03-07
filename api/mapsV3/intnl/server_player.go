package intnl

import (
	"context"
	"fmt"
	"strconv"

	"github.com/hollow-cube/api-server/internal/mapdb"
	"github.com/hollow-cube/api-server/internal/pkg/common"
	"github.com/redis/rueidis"
)

// v4 removed
func (s *server) GetMapPlayerDataWithIndexedSlots(ctx context.Context, playerId string) (mapdb.MapPlayerData, error) {
	pd, err := s.store.GetPlayerData(ctx, playerId)
	if err != nil {
		return pd, fmt.Errorf("failed to fetch player data: %w", err)
	}

	// Fill in old indexed map slots
	indexedSlots, err := s.store.GetIndexedMapSlots(ctx, playerId)
	if err != nil {
		return pd, fmt.Errorf("failed to fetch indexed map slots: %w", err)
	}
	pd.Map = make([]string, 5)
	for _, slot := range indexedSlots {
		pd.Map[slot.Index] = slot.MapID
	}

	return pd, err
}

// v4 removed
func (s *server) GetMapPlayerData(ctx context.Context, request GetMapPlayerDataRequestObject) (GetMapPlayerDataResponseObject, error) {
	pd, err := s.GetMapPlayerDataWithIndexedSlots(ctx, request.PlayerId)
	if err != nil {
		return nil, err
	}

	return GetMapPlayerData200JSONResponse{playerDataToAPI(pd)}, nil
}

// v4
func (s *server) GetMapPlayerSlots(ctx context.Context, request GetMapPlayerSlotsRequestObject) (GetMapPlayerSlotsResponseObject, error) {
	// Get maps from existing slots
	slots, err := s.store.GetMapSlots(ctx, request.PlayerId)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch map slots: %w", err)
	}
	mapIds := make([]string, len(slots))
	for i, slot := range slots {
		mapIds[i] = slot.MapID
	}
	maps, err := s.store.MultiGetMapWithTagsById(ctx, mapIds)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch maps: %w", err)
	}

	// Get all published maps
	published, err := s.store.SearchMaps(ctx, mapdb.SearchMapsParams{
		Variants:  []string{"parkour", "building"},
		Owner:     &request.PlayerId,
		Sort:      "published",
		SortOrder: "desc",
		Page:      0,
		PageSize:  1000000, // we have no limit at home
	})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch published maps: %w", err)
	}

	result := make(GetMapPlayerSlots200JSONResponse, len(slots)+len(published))
	for i, slot := range slots {
		for _, m := range maps {
			if m.Map.ID != slot.MapID {
				continue
			}

			result[i] = MapData(s.hydrateMap(m.Map, m.Tags))
		}
	}
	for i, m := range published {
		result[i+len(slots)] = MapData(s.hydratePublishedMap(m.PublishedMap))
	}
	return result, nil
}

func (s *server) GetMapHistory(ctx context.Context, request GetMapHistoryRequestObject) (GetMapHistoryResponseObject, error) {
	var err error
	page, pageSize := 0, 10
	params := request.Params.Params
	if params.Page != nil {
		page, err = strconv.Atoi(*params.Page)
	}
	if params.PageSize != nil {
		pageSize, err = strconv.Atoi(*params.PageSize)
	}
	if err != nil {
		return nil, err
	}

	maps, err := s.store.GetRecentMaps(ctx, mapdb.GetRecentMapsParams{
		PlayerID: request.PlayerId,
		Type:     mapdb.SaveStateTypePlaying,
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch recent maps: %w", err)
	}
	hasNextPage := len(maps) == pageSize+1
	maps = maps[0:min(pageSize, len(maps))]

	results := make([]MapHistoryEntry, len(maps))
	for i, m := range maps {
		results[i].MapId = m
	}
	return GetMapHistory200JSONResponse{GetMapHistoryJSONResponse{
		NextPage: hasNextPage,
		Page:     page,
		Results:  results,
	}}, nil
}

func (s *server) DeleteMapPlayerStates(ctx context.Context, request DeleteMapPlayerStatesRequestObject) (DeleteMapPlayerStatesResponseObject, error) {
	completedMaps, err := s.store.GetCompletedMaps(ctx, request.PlayerId)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch completed maps: %w", err)
	}
	if len(completedMaps) == 0 {
		return DeleteMapPlayerStates200Response{}, nil
	}

	playerUuid := string(common.UUIDToBin(request.PlayerId))
	cmds := make(rueidis.Commands, len(completedMaps))
	for i, mapId := range completedMaps {
		err = s.store.DeleteMapPlayerSaveStates(ctx, mapId, request.PlayerId)
		if err != nil {
			return nil, fmt.Errorf("failed to delete map player save states: %w", err)
		}

		leaderboardKey := mapLeaderboardKey(mapId, "playtime")
		cmds[i] = s.redis.B().Zrem().Key(leaderboardKey).Member(playerUuid).Build()
	}
	for _, resp := range s.redis.DoMulti(ctx, cmds...) {
		if err = resp.Error(); err != nil {
			return nil, fmt.Errorf("failed to delete player states: %w", err)
		}
	}

	return DeleteMapPlayerStates200Response{}, nil
}

// v4 removed
func playerDataToAPI(pd mapdb.MapPlayerData) GetMapPlayerDataJSONResponse {
	return GetMapPlayerDataJSONResponse{
		Id:          pd.ID,
		MapSlots:    pd.Map,
		ContestSlot: pd.ContestSlot,
	}
}
