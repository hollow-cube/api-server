package intnl

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/hollow-cube/hc-services/libraries/common/pkg/common"
	"github.com/hollow-cube/hc-services/services/map-service/internal/pkg/model"
	"github.com/hollow-cube/hc-services/services/map-service/internal/pkg/storage"
	"github.com/redis/rueidis"
)

func (s *server) GetMapPlayerData(ctx context.Context, request GetMapPlayerDataRequestObject) (GetMapPlayerDataResponseObject, error) {
	pd, err := s.storageClient.GetPlayerData2(ctx, request.PlayerId)
	if errors.Is(err, storage.ErrNotFound) {
		// Always return empty player data even if not found
		pd = &model.PlayerData{
			Id:   request.PlayerId,
			Maps: make([]string, 2),
		}
	} else if err != nil {
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

	maps, hasNextPage, err := s.storageClient.GetRecentMaps(ctx, page, pageSize, request.PlayerId, model.SaveStateTypePlaying)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch recent maps: %w", err)
	}

	results := make([]MapHistoryEntry, len(maps))
	for i, m := range maps {
		results[i].MapId = m.Id
	}
	return GetMapHistory200JSONResponse{GetMapHistoryJSONResponse{
		NextPage: hasNextPage,
		Page:     page,
		Results:  results,
	}}, nil
}

func (s *server) DeleteMapPlayerStates(ctx context.Context, request DeleteMapPlayerStatesRequestObject) (DeleteMapPlayerStatesResponseObject, error) {
	completedMaps, err := s.storageClient.GetCompletedMaps(ctx, request.PlayerId)
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

func playerDataToAPI(pd *model.PlayerData) GetMapPlayerDataJSONResponse {
	return GetMapPlayerDataJSONResponse{
		Id:          pd.Id,
		MapSlots:    pd.Maps,
		ContestSlot: pd.ContestSlot,
	}
}
