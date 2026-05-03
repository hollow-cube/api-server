package v4Internal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/hollow-cube/api-server/internal/mapdb"
	"github.com/hollow-cube/api-server/internal/pkg/common"
	"github.com/hollow-cube/api-server/internal/pkg/model"
	"github.com/hollow-cube/api-server/pkg/ox"
	"github.com/redis/rueidis"
)

type (
	SearchMapsRequest struct {
		Building   *bool         `query:"building,omitempty"`
		Contest    *string       `query:"contest,omitempty"`
		Difficulty *string       `query:"difficulty,omitempty"`
		Owner      *string       `query:"owner,omitempty"`
		Parkour    *bool         `query:"parkour,omitempty"`
		Quality    *string       `query:"quality,omitempty"`
		Query      *string       `query:"query,omitempty"`
		Sort       *MapSortType  `query:"sort,omitempty"`
		SortOrder  *MapSortOrder `query:"sortOrder,omitempty"`

		Page     *string `query:"page,omitempty"`
		PageSize *string `query:"pageSize,omitempty"`
	}
	PaginatedMapList struct {
		Count   int       `json:"count"`
		Results []MapData `json:"results"`
	}
	InvalidSearchParams struct {
		ox.BadRequest
		Message string `json:"message"`
	}
)

func (s *Server) SearchMaps(ctx context.Context, request SearchMapsRequest) (*PaginatedMapList, error) {
	var params mapdb.SearchMapsParams
	if errText := parseSearchQueryParams(&params, request); errText != "" {
		return nil, InvalidSearchParams{Message: errText}
	}

	// Try to return cached queries
	cacheKey, shouldCache := createMapSearchCacheKey(&params)
	shouldCache = false
	if shouldCache {
		cachedRaw, err := s.redis.Do(ctx, s.redis.B().Get().Key(cacheKey).Build()).AsBytes()
		if err == nil {
			// We have a cached query, yay! Reparsing this to return is kind of stupid, but oh well what can you do :)
			var cached PaginatedMapList
			if err = json.Unmarshal(cachedRaw, &cached); err == nil {
				return &cached, nil
			}
			s.log.Errorw("failed to parse cached map search result", "err", err)
		} else if !errors.Is(err, rueidis.Nil) {
			// Something else went wrong.
			s.log.Errorw("failed to fetch cached map search result", "err", err)
		}
	}

	entries, err := s.mapStore.SearchMaps(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to query maps: %w", err)
	}
	result := PaginatedMapList{
		Count:   len(entries),
		Results: make([]MapData, len(entries)),
	}
	for i, entry := range entries {
		result.Results[i] = hydratePublishedMap(entry.PublishedMap, entry.Tags)
		result.Count = int(entry.TotalCount)
	}

	// Cache the result for 5 minutes
	if shouldCache {
		var raw []byte
		if raw, err = json.Marshal(result); err == nil {
			err = s.redis.Do(ctx, s.redis.B().Set().Key(cacheKey).Value(string(raw)).Ex(5*time.Minute).Build()).Error()
		}
		if err != nil {
			s.log.Errorw("failed to cache map search result", "err", err)
		}
	}

	return &result, nil
}

func parseSearchQueryParams(params *mapdb.SearchMapsParams, req SearchMapsRequest) string {
	var err error
	var ok bool
	if req.Page != nil && *req.Page != "" {
		params.Page, err = strconv.Atoi(*req.Page)
		if err != nil {
			return err.Error()
		} else if params.Page < 0 {
			return fmt.Sprintf("invalid page: %d", params.Page)
		}
	}
	if req.PageSize != nil && *req.PageSize != "" {
		params.PageSize, err = strconv.Atoi(*req.PageSize)
		if err != nil {
			return err.Error()
		} else if params.PageSize < 0 || params.PageSize > 100 {
			return fmt.Sprintf("invalid page size: %d", params.PageSize)
		}
	} else {
		params.PageSize = 10
	}

	if req.Sort != nil && *req.Sort != "" {
		params.Sort, ok = mapSortIndex[*req.Sort]
		if !ok {
			return fmt.Sprintf("invalid sort value: %s", *req.Sort)
		}
	} else {
		params.Sort = model.MapSortBest
	}
	if req.SortOrder != nil && *req.SortOrder != "" {
		params.SortOrder, ok = mapSortOrderIndex[*req.SortOrder]
		if !ok {
			return fmt.Sprintf("invalid sort order: %s", *req.SortOrder)
		}
	} else {
		params.SortOrder = model.MapSortDesc
	}

	if req.Parkour != nil && *req.Parkour {
		params.Variants = append(params.Variants, string(model.Parkour))
	}
	if req.Building != nil && *req.Building {
		params.Variants = append(params.Variants, string(model.Building))
	}

	if req.Quality != nil && *req.Quality != "" {
		rawQualities := strings.Split(*req.Quality, ",")
		for _, rawQuality := range rawQualities {
			if rawQuality == "" {
				continue
			}
			quality, ok := mapQualityIndex[MapQuality(rawQuality)]
			if !ok {
				return fmt.Sprintf("invalid quality: %s", rawQuality)
			}
			params.Quality = append(params.Quality, int(quality))
		}
	}
	if req.Difficulty != nil && *req.Difficulty != "" {
		rawDifficulties := strings.Split(*req.Difficulty, ",")
		for _, rawDifficulty := range rawDifficulties {
			if rawDifficulty == "" {
				continue
			}
			difficulty, ok := mapDifficultyIndex[MapDifficulty(rawDifficulty)]
			if !ok {
				return fmt.Sprintf("invalid difficulty: %s", rawDifficulty)
			}
			params.Difficulty = append(params.Difficulty, int(difficulty))
		}
	}

	if req.Owner != nil && *req.Owner != "" {
		if !common.IsUUID(*req.Owner) {
			return fmt.Sprintf("invalid owner: %s", *req.Owner)
		}
		params.Owner = req.Owner
	}
	if req.Query != nil && *req.Query != "" {
		nameQuery := "%" + *req.Query + "%"
		params.Name = &nameQuery
	}
	if req.Contest != nil && *req.Contest != "" {
		params.Contest = req.Contest
	}

	return ""
}

type (
	SearchMapProgressRequestBody struct {
		PlayerID string   `json:"playerId"`
		MapIDs   []string `json:"mapIds"`
	}
	SearchMapProgressResponse struct {
		Results []PlayerMapProgress `json:"results"`
	}
	PlayerMapProgress struct {
		PlayerId string      `json:"playerId"`
		MapId    string      `json:"mapId"`
		Progress MapProgress `json:"progress"`
		Playtime int         `json:"playtime,omitempty"`
	}
)

// POST /maps/search/progress
func (s *Server) SearchMapProgress(ctx context.Context, body SearchMapProgressRequestBody) (*SearchMapProgressResponse, error) {
	entries, err := s.mapStore.GetMultiMapProgress(ctx, body.PlayerID, body.MapIDs)
	if err != nil {
		return nil, err
	}

	result := make([]PlayerMapProgress, len(entries))
	for i, entry := range entries {
		progress := MapProgressNone
		if entry.Progress == 1 {
			progress = MapProgressStarted
		} else if entry.Progress == 2 {
			progress = MapProgressComplete
		}

		result[i] = PlayerMapProgress{
			PlayerId: body.PlayerID,
			MapId:    entry.MapID,
			Progress: progress,
			Playtime: int(entry.Playtime),
		}
	}

	return &SearchMapProgressResponse{Results: result}, nil
}
