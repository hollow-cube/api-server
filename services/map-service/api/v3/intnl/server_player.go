package intnl

import (
	"context"
	"fmt"
	"strconv"

	"github.com/hollow-cube/hc-services/libraries/common/pkg/common"
	"github.com/hollow-cube/hc-services/services/map-service/internal/db"
	"github.com/redis/rueidis"
)

func (s *server) GetMapPlayerData(ctx context.Context, request GetMapPlayerDataRequestObject) (GetMapPlayerDataResponseObject, error) {
	pd, err := s.store.GetPlayerData(ctx, request.PlayerId)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch player data: %w", err)
	}

	return GetMapPlayerData200JSONResponse{playerDataToAPI(pd)}, nil
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

	maps, err := s.store.GetRecentMaps(ctx, db.GetRecentMapsParams{
		PlayerID: request.PlayerId,
		Type:     db.SaveStateTypePlaying,
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

func playerDataToAPI(pd db.MapPlayerData) GetMapPlayerDataJSONResponse {
	return GetMapPlayerDataJSONResponse{
		Id:          pd.ID,
		MapSlots:    pd.Maps,
		ContestSlot: pd.ContestSlot,
	}
}
