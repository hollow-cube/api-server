package v4Internal

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"

	"github.com/hollow-cube/api-server/internal/pkg/common"
	"github.com/hollow-cube/api-server/internal/pkg/model"
	"github.com/hollow-cube/api-server/internal/pkg/notification"
	"github.com/hollow-cube/api-server/pkg/ox"
	"github.com/redis/rueidis"
)

type (
	LeaderboardData struct {
		Player *LeaderboardEntry  `json:"player,omitempty"`
		Top    []LeaderboardEntry `json:"top"`
	}
	LeaderboardEntry struct {
		Player string `json:"player"`
		Rank   int    `json:"rank"`
		Score  int    `json:"score"`
	}
)

type (
	PaginatedPlayerTopTimeList struct {
		Count   int             `json:"count"`
		Results []PlayerTopTime `json:"results"`
	}
	PlayerTopTime struct {
		MapID          string `json:"mapId"`
		PublishedID    int    `json:"publishedId"`
		MapName        string `json:"mapName"`
		CompletionTime int    `json:"completionTime"`
		Rank           int    `json:"rank"`
	}
)

// GET /players/{playerId}/top-times
func (s *Server) GetPlayerTopTimes(ctx context.Context, request PlayerPaginatedRequest) (*PaginatedPlayerTopTimeList, error) {
	offset, limit := defaultPageParams(request.Page, request.PageSize)

	bestTimes, err := s.mapStore.GetPlayerBestTimes(ctx, request.PlayerID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch player best times: %w", err)
	}

	if len(bestTimes) == 0 {
		return &PaginatedPlayerTopTimeList{
			Count:   0,
			Results: []PlayerTopTime{},
		}, nil
	}

	// Use a redis pipeline to avoid network rtt
	// Use ZCOUNT to count players with strictly lower times (to handle ties).
	// E.g. if 6 players share the same time, ZCOUNT returns 0 for all of them, so they all get rank 1.
	cmds := make(rueidis.Commands, len(bestTimes))
	for i, bt := range bestTimes {
		leaderboardKey := mapLeaderboardKey(bt.MapID, "playtime")
		roundedPlaytime := int(math.Round(float64(bt.Playtime)/50.0)) * 50
		threshold := roundedPlaytime - 25
		cmds[i] = s.redis.B().Zcount().Key(leaderboardKey).
			Min("-inf").Max(fmt.Sprintf("(%d", threshold)).Build()
	}

	// Exec pipeline
	results := s.redis.DoMulti(ctx, cmds...)

	// Collect entries - ZCOUNT returns the count of players with better times
	type rankedEntry struct {
		MapID       string
		PublishedID int
		MapName     string
		Playtime    int
		Rank        int
	}
	var entries []rankedEntry
	for i, resp := range results {
		betterCount, err := resp.AsInt64()
		if err != nil {
			s.log.Warnw("failed to get rank for map", "mapId", bestTimes[i].MapID, "error", err)
			continue
		}

		mapName := ""
		if bestTimes[i].MapName != nil {
			mapName = *bestTimes[i].MapName
		}

		publishedID := 0
		if bestTimes[i].PublishedID != nil {
			publishedID = *bestTimes[i].PublishedID
		}

		// Round playtime to 50ms for display (consistent with rank calculation)
		roundedPlaytime := int(math.Round(float64(bestTimes[i].Playtime)/50.0)) * 50
		entries = append(entries, rankedEntry{
			MapID:       bestTimes[i].MapID,
			PublishedID: publishedID,
			MapName:     mapName,
			Playtime:    roundedPlaytime,
			Rank:        int(betterCount) + 1, // Rank = count of better times + 1
		})
	}

	// Sort by rank ascending (best placements first)
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Rank < entries[j].Rank
	})

	// Paginate
	totalItems := int64(len(entries))
	start := int(offset)
	end := int(offset) + int(limit)

	if start >= len(entries) {
		return &PaginatedPlayerTopTimeList{
			Count:   int(totalItems),
			Results: []PlayerTopTime{},
		}, nil
	}

	if end > len(entries) {
		end = len(entries)
	}

	items := make([]PlayerTopTime, 0, end-start)
	for _, e := range entries[start:end] {
		items = append(items, PlayerTopTime{
			MapID:          e.MapID,
			PublishedID:    e.PublishedID,
			MapName:        e.MapName,
			CompletionTime: e.Playtime,
			Rank:           e.Rank,
		})
	}

	return &PaginatedPlayerTopTimeList{
		Count:   int(totalItems),
		Results: items,
	}, nil
}

type GetGlobalLeaderboardRequest struct {
	LeaderboardID string `path:"leaderboardId"`
	PlayerID      string `query:"playerId"`
}

// GET /maps/hub/leaderboard/{leaderboardId}
func (s *Server) GetGlobalLeaderboard(ctx context.Context, request GetGlobalLeaderboardRequest) (*LeaderboardData, error) {
	if request.PlayerID != "" {
		// Fetch the player score (if present)

		var score int64
		var err error
		if request.LeaderboardID == "top_times" {
			score, err = s.mapStore.GetTopTimesLeaderboardForPlayer(ctx, request.PlayerID)
		} else if request.LeaderboardID == "maps_beaten" {
			score, err = s.mapStore.GetMapsBeatenLeaderboardForPlayer(ctx, request.PlayerID)
		} else {
			return nil, ox.NotFound{}
		}
		if err != nil {
			return nil, fmt.Errorf("failed to fetch player score: %w", err)
		}

		return &LeaderboardData{
			Player: &LeaderboardEntry{
				Player: request.PlayerID,
				Score:  int(score),
				Rank:   -1,
			},
		}, nil
	}

	// Fetch top 10
	if request.LeaderboardID == "top_times" {
		entries, err := s.mapStore.GetTopTimesLeaderboard(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch leaderboard: %w", err)
		}

		var result LeaderboardData
		result.Top = make([]LeaderboardEntry, len(entries))
		for i, entry := range entries {
			result.Top[i] = LeaderboardEntry{
				Player: entry.PlayerID,
				Score:  int(entry.TopTimes),
				Rank:   i + 1,
			}
		}

		return &result, nil
	} else if request.LeaderboardID == "maps_beaten" {
		entries, err := s.mapStore.GetMapsBeatenLeaderboard(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch leaderboard: %w", err)
		}

		var result LeaderboardData
		result.Top = make([]LeaderboardEntry, len(entries))
		for i, entry := range entries {
			result.Top[i] = LeaderboardEntry{
				Player: entry.PlayerID,
				Score:  int(entry.UniqueMapsBeaten),
				Rank:   i + 1,
			}
		}

		return &result, nil
	}

	return nil, ox.NotFound{}
}

type GetMapLeaderboardRequest struct {
	MapID    string `path:"mapId"`
	PlayerID string `query:"playerId"`
}

// GET /maps/{mapId}/leaderboard
func (s *Server) GetMapLeaderboard(ctx context.Context, request GetMapLeaderboardRequest) (*LeaderboardData, error) {
	m, err := s.mapStore.GetMapById(ctx, request.MapID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch map: %w", err)
	}
	isAsc := m.Leaderboard == nil || m.Leaderboard.Asc

	playerId := request.PlayerID
	leaderboardKey := mapLeaderboardKey(request.MapID, "playtime")

	// Fetch top 10
	var entries []rueidis.ZScore
	if isAsc {
		entries, err = s.redis.Do(ctx, s.redis.B().Zrange().Key(leaderboardKey).
			Min("0").Max("9").Withscores().Build()).AsZScores()
	} else {
		entries, err = s.redis.Do(ctx, s.redis.B().Zrevrange().Key(leaderboardKey).
			Start(0).Stop(9).Withscores().Build()).AsZScores()
	}
	if err != nil {
		if errors.Is(err, rueidis.Nil) {
			// Always return an empty leaderboard if it does not exist (yet)
			return &LeaderboardData{Top: []LeaderboardEntry{}}, nil
		}

		return nil, fmt.Errorf("failed to fetch leaderboard: %w", err)
	}
	// Fetch the player score (if present)
	var playerScore []int64
	if playerId != "" {
		var rankScore []int64
		if isAsc {
			rankScore, err = s.redis.Do(ctx, s.redis.B().Zrank().Key(leaderboardKey).
				Member(string(common.UUIDToBin(playerId))).Withscore().Build()).AsIntSlice()
		} else {
			rankScore, err = s.redis.Do(ctx, s.redis.B().Zrevrank().Key(leaderboardKey).
				Member(string(common.UUIDToBin(playerId))).Withscore().Build()).AsIntSlice()
		}
		if errors.Is(err, rueidis.Nil) {
			// Player does not have a score on this map yet, ignore.
		} else if err != nil {
			return nil, fmt.Errorf("failed to fetch player score: %w", err)
		} else {
			playerScore = rankScore // Player has a score, set it.
		}
	}

	lb := hydrateLeaderboard(entries, playerId, playerScore)

	lastScore, lastRank := -1, 0
	for i, entry := range lb.Top {
		score := entry.Score
		if m.Leaderboard == nil || m.Leaderboard.Format == "time" {
			score = int(math.Round(float64(score)/50.0)) * 50 // round to 50ms
		}
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
		score := lb.Player.Score
		if m.Leaderboard == nil || m.Leaderboard.Format == "time" {
			score = int(math.Round(float64(score)/50.0)) * 50 // round to 50ms
		}
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

	return &lb, nil
}

type DeleteMapLeaderboardRequest struct {
	MapID    string `path:"mapId"`
	PlayerID string `query:"playerId"`
	Notify   bool   `query:"notify"`
}

// DELETE /maps/{mapId}/leaderboard
func (s *Server) DeleteMapLeaderboard(ctx context.Context, request DeleteMapLeaderboardRequest) error {
	leaderboardKey := mapLeaderboardKey(request.MapID, "playtime")

	playerId := request.PlayerID
	if playerId == "" {
		// We cannot do the two deletions in a transaction, so do the savestate first because it is the more important one.
		err := s.mapStore.Unsafe_DeleteMapSaveStates(ctx, request.MapID)
		if err != nil {
			return fmt.Errorf("failed to mark save states for deletion: %w", err)
		}
		err = s.redis.Do(ctx, s.redis.B().Del().Key(leaderboardKey).Build()).Error()
		if err != nil && !errors.Is(err, rueidis.Nil) {
			return fmt.Errorf("failed to delete player score: %w", err)
		}
	} else {
		// We cannot do the two deletions in a transaction, so do the savestate first because it is the more important one.
		err := s.mapStore.DeleteMapPlayerSaveStates(ctx, request.MapID, playerId)
		if err != nil {
			return fmt.Errorf("failed to mark save states for deletion: %w", err)
		}
		err = s.redis.Do(ctx, s.redis.B().Zrem().Key(leaderboardKey).Member(string(common.UUIDToBin(playerId))).Build()).Error()
		if err != nil && !errors.Is(err, rueidis.Nil) {
			return fmt.Errorf("failed to delete player score: %w", err)
		}

		if request.Notify {
			err = s.notifications.Create(ctx, playerId, notification.CreateInput{
				Key:           request.MapID,
				Type:          "map_time_deleted",
				ReplaceUnread: true,
			})
			if err != nil {
				s.log.Warnw("failed to create notification", "playerId", playerId, "error", err)
			}
		}
	}

	return nil
}

// PUT /maps/{mapId}/leaderboard
func (s *Server) RestoreMapLeaderboard(ctx context.Context, request MapRequest) error {
	leaderboardKey := mapLeaderboardKey(request.MapID, "playtime")

	// Confirm that the map is a parkour map, otherwise do nothing (its not currently an error)
	m, err := s.map_(ctx, request.MapID)
	if err != nil {
		return err
	}
	if m.OptVariant != string(model.Parkour) {
		return nil
	}

	// Fetch all save states for this map and rewrite them into redis
	// TODO: This should really be paged it could be a ton of entries.
	saveStates, err := s.mapStore.GetAllBestCompletedSaveStatesForMap(ctx, request.MapID)
	if err != nil {
		return fmt.Errorf("failed to fetch save states: %w", err)
	}

	cmds := make(rueidis.Commands, len(saveStates)+1)
	cmds[0] = s.redis.B().Del().Key(leaderboardKey).Build()
	for i, saveState := range saveStates {
		var score float64
		if saveState.Score != nil {
			score = *saveState.Score
		} else {
			// Legacy behavior prior to custom leaderboards
			score = float64(max(saveState.Playtime, saveState.Ticks*50))
		}

		cmds[i+1] = s.redis.B().Zadd().Key(leaderboardKey).Lt().ScoreMember().
			ScoreMember(score, string(common.UUIDToBin(saveState.PlayerID))).Build()
	}
	for _, resp := range s.redis.DoMulti(ctx, cmds...) {
		if err = resp.Error(); err != nil {
			return fmt.Errorf("failed to write save states to redis: %w", err)
		}
	}

	return nil
}

func hydrateLeaderboard(entries []rueidis.ZScore, playerId string, playerScore []int64) LeaderboardData {
	var result LeaderboardData
	result.Top = make([]LeaderboardEntry, len(entries))
	for i, entry := range entries {
		result.Top[i] = LeaderboardEntry{
			Player: common.UUIDFromBin([]byte(entry.Member)),
			Score:  int(entry.Score),
			Rank:   i + 1,
		}
	}

	if playerId != "" && playerScore != nil {
		result.Player = &LeaderboardEntry{
			Player: playerId,
			Score:  int(playerScore[1]),
			Rank:   int(playerScore[0]) + 1,
		}
	}

	return result
}
