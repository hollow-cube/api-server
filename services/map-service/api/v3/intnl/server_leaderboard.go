package intnl

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/hollow-cube/hc-services/libraries/common/pkg/common"
	"github.com/hollow-cube/hc-services/services/map-service/internal/pkg/model"
	"github.com/redis/rueidis"
)

func (s *server) GetGlobalLeaderboard(ctx context.Context, request GetGlobalLeaderboardRequestObject) (GetGlobalLeaderboardResponseObject, error) {
	playerId, leaderboardName := request.Params.PlayerId, request.LeaderboardName
	if playerId != nil && *playerId != "" {
		// Fetch the player score (if present)

		var score int
		var err error
		if leaderboardName == "top_times" {
			score, err = s.storageClient.GetTopTimesLeaderboardForPlayer(ctx, *playerId)
		} else if leaderboardName == "maps_beaten" {
			score, err = s.storageClient.GetMapsBeatenLeaderboardForPlayer(ctx, *playerId)
		} else {
			return GetGlobalLeaderboard404Response{}, nil
		}
		if err != nil {
			return nil, fmt.Errorf("failed to fetch player score: %w", err)
		}

		return &GetGlobalLeaderboard200JSONResponse{LeaderboardDataJSONResponse{
			Player: &LeaderboardEntry{
				Player: *playerId,
				Score:  score,
				Rank:   -1,
			},
		}}, nil
	}

	// Fetch top 10
	var entries []*model.LeaderboardEntry
	var err error
	if leaderboardName == "top_times" {
		entries, err = s.storageClient.GetTopTimesLeaderboard(ctx)
	} else if leaderboardName == "maps_beaten" {
		entries, err = s.storageClient.GetMapsBeatenLeaderboard(ctx)
	} else {
		return GetGlobalLeaderboard404Response{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to fetch leaderboard: %w", err)
	}

	var result LeaderboardDataJSONResponse
	result.Top = make([]LeaderboardEntry, len(entries))
	for i, entry := range entries {
		result.Top[i] = LeaderboardEntry{
			Player: entry.PlayerId,
			Score:  entry.Score,
			Rank:   i + 1,
		}
	}

	return &GetGlobalLeaderboard200JSONResponse{result}, nil
}

func (s *server) GetMapLeaderboard(ctx context.Context, request GetMapLeaderboardRequestObject) (GetMapLeaderboardResponseObject, error) {
	playerId := request.Params.PlayerId
	if request.LeaderboardName != "playtime" {
		return GetMapLeaderboard404Response{}, nil
	}
	leaderboardKey := mapLeaderboardKey(request.MapId, request.LeaderboardName)

	// Fetch top 10
	entries, err := s.redis.Do(ctx, s.redis.B().Zrange().Key(leaderboardKey).
		Min("0").Max("9").Withscores().Build()).AsZScores()
	if err != nil {
		if errors.Is(err, rueidis.Nil) {
			// Always return an empty leaderboard if it does not exist (yet)
			return GetMapLeaderboard200JSONResponse{LeaderboardDataJSONResponse{
				Top: []LeaderboardEntry{},
			}}, nil
		}

		return nil, fmt.Errorf("failed to fetch leaderboard: %w", err)
	}
	// Fetch the player score (if present)
	var playerScore []int64
	if playerId != nil && *playerId != "" {
		rankScore, err := s.redis.Do(ctx, s.redis.B().Zrank().Key(leaderboardKey).
			Member(string(common.UUIDToBin(*playerId))).Withscore().Build()).AsIntSlice()
		if errors.Is(err, rueidis.Nil) {
			// Player does not have a score on this map yet, ignore.
		} else if err != nil {
			return nil, fmt.Errorf("failed to fetch player score: %w", err)
		} else {
			playerScore = rankScore // Player has a score, set it.
		}
	}

	lb := cachedLBToAPI(entries, playerId, playerScore)

	// Normalize the tied times
	lastScore, lastRank := -1, 0
	for i, entry := range lb.Top {
		score := int(math.Round(float64(entry.Score)/50.0)) * 50 // round to 50ms
		if lastScore != score {
			lastRank = i + 1 // rank starts at 1
			lastScore = score
		}
		lb.Top[i] = LeaderboardEntry{
			Player: entry.Player,
			Score:  score,
			Rank:   lastRank,
		}
	}
	if lb.Player != nil {
		score := int(math.Round(float64(lb.Player.Score)/50.0)) * 50
		for _, entry := range lb.Top {
			if score == entry.Score {
				lb.Player = &LeaderboardEntry{
					Player: entry.Player,
					Score:  score,
					Rank:   lastRank,
				}
				break
			}
		}
	}

	return GetMapLeaderboard200JSONResponse{lb}, nil
}

func (s *server) DeleteMapLeaderboard(ctx context.Context, request DeleteMapLeaderboardRequestObject) (DeleteMapLeaderboardResponseObject, error) {
	if request.LeaderboardName != "playtime" {
		return DeleteMapLeaderboard404Response{}, nil
	}
	leaderboardKey := mapLeaderboardKey(request.MapId, request.LeaderboardName)

	playerId := request.Params.PlayerId
	if playerId == nil || *playerId == "" {
		// We cannot do the two deletions in a transaction, so do the savestate first because it is the more important one.
		err := s.storageClient.SoftDeleteMapSaveStates(ctx, request.MapId, false)
		if err != nil {
			return nil, fmt.Errorf("failed to mark save states for deletion: %w", err)
		}
		err = s.redis.Do(ctx, s.redis.B().Del().Key(leaderboardKey).Build()).Error()
		if err != nil && !errors.Is(err, rueidis.Nil) {
			return nil, fmt.Errorf("failed to delete player score: %w", err)
		}
	} else {
		// We cannot do the two deletions in a transaction, so do the savestate first because it is the more important one.
		err := s.storageClient.SoftDeleteMapPlayerSaveStates(ctx, request.MapId, *playerId)
		if err != nil {
			return nil, fmt.Errorf("failed to mark save states for deletion: %w", err)
		}
		err = s.redis.Do(ctx, s.redis.B().Zrem().Key(leaderboardKey).Member(string(common.UUIDToBin(*playerId))).Build()).Error()
		if err != nil && !errors.Is(err, rueidis.Nil) {
			return nil, fmt.Errorf("failed to delete player score: %w", err)
		}
	}

	return DeleteMapLeaderboard200Response{}, nil
}

func (s *server) RestoreMapLeaderboard(ctx context.Context, request RestoreMapLeaderboardRequestObject) (RestoreMapLeaderboardResponseObject, error) {
	if request.LeaderboardName != "playtime" {
		return RestoreMapLeaderboard404Response{}, nil
	}
	leaderboardKey := mapLeaderboardKey(request.MapId, request.LeaderboardName)

	// Confirm that the map is a parkour map, otherwise do nothing (its not currently an error)
	m, err := s.storageClient.GetMapById(ctx, request.MapId)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch map: %w", err)
	}
	if m.Settings.Variant != model.Parkour {
		return RestoreMapLeaderboard200Response{}, nil
	}

	// Fetch all save states for this map and rewrite them into redis
	// TODO: This should really be paged it could be a ton of entries.
	saveStates, err := s.storageClient.GetAllSaveStates(ctx, request.MapId)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch save states: %w", err)
	}

	cmds := make(rueidis.Commands, len(saveStates))
	for i, saveState := range saveStates {
		cmds[i] = s.redis.B().Zadd().Key(leaderboardKey).Lt().ScoreMember().
			ScoreMember(float64(saveState.PlayTime), string(common.UUIDToBin(saveState.PlayerId))).Build()
	}
	for _, resp := range s.redis.DoMulti(ctx, cmds...) {
		if err = resp.Error(); err != nil {
			return nil, fmt.Errorf("failed to write save states to redis: %w", err)
		}
	}

	return RestoreMapLeaderboard200Response{}, nil
}

func mapLeaderboardKey(mapId, lbType string) string {
	return fmt.Sprintf("map:%s:lb_%s", mapId, lbType)
}

func cachedLBToAPI(entries []rueidis.ZScore, playerId *string, playerScore []int64) LeaderboardDataJSONResponse {
	var result LeaderboardDataJSONResponse
	result.Top = make([]LeaderboardEntry, len(entries))
	for i, entry := range entries {
		result.Top[i] = LeaderboardEntry{
			Player: common.UUIDFromBin([]byte(entry.Member)),
			Score:  int(entry.Score),
			Rank:   i + 1,
		}
	}

	if playerId != nil && *playerId != "" && playerScore != nil {
		result.Player = &LeaderboardEntry{
			Player: *playerId,
			Score:  int(playerScore[1]),
			Rank:   int(playerScore[0]) + 1,
		}
	}

	return result
}
