package v4Internal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gohugoio/hashstructure"
	"github.com/google/uuid"
	"github.com/hollow-cube/api-server/internal/mapdb"
	"github.com/hollow-cube/api-server/internal/pkg/model"
	"github.com/hollow-cube/api-server/internal/pkg/notification"
	"github.com/hollow-cube/api-server/internal/pkg/player"
	"github.com/hollow-cube/api-server/internal/pkg/util"
	"github.com/hollow-cube/api-server/internal/playerdb"
	"github.com/hollow-cube/api-server/pkg/hog"
	"github.com/hollow-cube/api-server/pkg/ox"
	"github.com/redis/rueidis"
	"go.uber.org/zap"
)

type (
	MapRequest struct {
		MapID string `path:"mapId"`
	}
	MapPlayerRequest struct {
		MapID    string `path:"mapId"`
		PlayerID string `path:"playerId"`
	}
)

type CreateMapRequest struct {
	Owner string  `json:"owner"`
	Size  MapSize `json:"size"`
}

// POST /maps
func (s *Server) CreateMap(ctx context.Context, body CreateMapRequest) (*MapData, error) {
	size, ok := sizeIndex[body.Size]
	if !ok {
		return nil, ox.BadRequest{}
	}

	// Ensure they have an available slot
	pd, err := s.player(ctx, body.Owner)
	if err != nil {
		return nil, err
	}
	slots, err := s.mapStore.GetMapSlots(ctx, pd.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get map slots: %w", err)
	}
	if len(slots) >= pd.TotalMapSlots() {
		return nil, fmt.Errorf("player has no available map slots")
	}

	// Ensure they have permission for the size map they are creating
	// TODO: should check here, but for now we assume the server doesnt make a mistake.

	mapParams, err := model.CreateDefaultMap(body.Owner, int(size))
	if err != nil {
		return nil, err
	}
	m, err := mapdb.Tx(ctx, s.mapStore, func(ctx context.Context, tx *mapdb.Store) (m mapdb.Map, err error) {
		m, err = tx.CreateMap(ctx, mapParams)
		if err != nil {
			return m, fmt.Errorf("db write failed: %w", err)
		}

		err = tx.InsertMapSlot(ctx, mapdb.InsertMapSlotParams{
			PlayerID:  pd.ID,
			MapID:     m.ID,
			CreatedAt: time.Now(),
			Index:     -1, // Always -1 for v2 slots
		})
		if err != nil {
			return m, fmt.Errorf("insert slot failed: %w", err)
		}

		return
	})
	if err != nil {
		return nil, err
	}

	hog.Enqueue(hog.Capture{
		DistinctId: body.Owner,
		Event:      "map_created",
		Properties: hog.NewProperties().
			Set("size", body.Size),
	})

	return new(hydrateMap(m, []mapdb.MapTag{})), nil
}

// GET /maps/{mapId}
func (s *Server) GetMap(ctx context.Context, request MapRequest) (*MapData, error) {
	publishedID, err := util.ParseMapPublishedID(request.MapID)
	if err == nil {
		pm, err := s.mapStore.GetPublishedMapByPublishedId(ctx, new(int(publishedID)))
		if errors.Is(err, mapdb.ErrNoRows) {
			return nil, ox.NotFound{}
		} else if err != nil {
			return nil, fmt.Errorf("failed to get published map: %w", err)
		}

		return new(hydratePublishedMap(pm.PublishedMap, pm.Tags)), nil
	} else if uuid.Validate(request.MapID) != nil {
		return nil, ox.NotFound{}
	}

	mt, err := s.mapStore.GetMapWithTagsById(ctx, request.MapID)
	if errors.Is(err, mapdb.ErrNoRows) {
		return nil, ox.NotFound{}
	} else if err != nil {
		return nil, fmt.Errorf("failed to get map: %w", err)
	}

	// If its published go read as published.
	// TODO: materialize all the fields from published and do this in a single query.
	if mt.Map.PublishedAt != nil {
		pm, err := s.mapStore.GetPublishedMapById(ctx, request.MapID)
		if err != nil {
			return nil, fmt.Errorf("failed to get published map: %w", err)
		}

		return new(hydratePublishedMap(pm.PublishedMap, pm.Tags)), nil
	}

	return new(hydrateMap(mt.Map, mt.Tags)), nil
}

type UpdateMapRequest struct {
	Name        *string      `json:"name"`
	Icon        *string      `json:"icon"`
	Variant     *MapVariant  `json:"variant"`
	Subvariant  *string      `json:"subvariant"`
	Tags        *[]string    `json:"tags"` // If set replaces all currently set tags
	Leaderboard *Leaderboard `json:"leaderboard"`

	NoJump     *bool           `json:"noJump"`
	NoSneak    *bool           `json:"noSneak"`
	NoSprint   *bool           `json:"noSprint"`
	OnlySprint *bool           `json:"onlySprint"`
	Extra      *map[string]any `json:"extra"`

	SpawnPoint *Pos `json:"spawnPoint"`

	Listed          *bool       `json:"listed"`
	Size            *MapSize    `json:"size"`
	QualityOverride *MapQuality `json:"qualityOverride"`
	ProtocolVersion *int        `json:"protocolVersion"`
}

// PATCH /maps/{mapId}
func (s *Server) UpdateMap(ctx context.Context, request MapRequest, body UpdateMapRequest) error {
	m, err := s.mapStore.GetMapById(ctx, request.MapID)
	if errors.Is(err, mapdb.ErrNoRows) {
		return nil
	} else if err != nil {
		return fmt.Errorf("failed to fetch map: %w", err)
	}

	var changed bool
	update := mapdb.UpdateMap2Params{ID: request.MapID}
	var newTags []mapdb.MapTag

	// TODO: map table deslopify changes
	//  - remove opt prefixes
	//  - use empty strings not null for name + icon + maybe more
	//  - convert variant + subvariant to new tags on old maps + remove those columns
	//  - remove ext
	//  - extra to jsonb
	//  - remove authz key

	if body.Name != nil && *body.Name != "" && (m.OptName == nil || *body.Name != *m.OptName) {
		update.Name = body.Name
		changed = true
	}
	if body.Icon != nil && *body.Icon != "" && (m.OptIcon == nil || *body.Icon != *m.OptIcon) {
		update.Icon = body.Icon
		changed = true
	}
	if body.Variant != nil && m.OptVariant != string(*body.Variant) {
		update.Variant = new(string(*body.Variant))
		changed = true
	}
	if body.Subvariant != nil {
		sv := *body.Subvariant
		if sv == string(model.SubVariantNone) {
			update.ClearSubvariant = true
		} else {
			update.Subvariant = &sv
		}
		changed = true
	}
	if body.Tags != nil {
		newTags = make([]mapdb.MapTag, len(*body.Tags))
		for i, tag := range *body.Tags {
			newTags[i] = mapdb.MapTag(tag)
		}
		changed = true
	}
	if body.Leaderboard != nil {
		isDefault := body.Leaderboard.Asc == defaultPlaytimeLeaderboard.Asc &&
			body.Leaderboard.Format == defaultPlaytimeLeaderboard.Format &&
			strings.EqualFold(strings.TrimSpace(body.Leaderboard.Score), strings.TrimSpace(defaultPlaytimeLeaderboard.Score))
		update.ClearLeaderboard = isDefault
		update.Leaderboard = &mapdb.Leaderboard{
			Asc:    body.Leaderboard.Asc,
			Format: string(body.Leaderboard.Format),
			Score:  body.Leaderboard.Score,
		}
		changed = true
	}
	if body.Extra != nil && len(*body.Extra) > 0 {
		var extra = make(map[string]any)
		_ = json.Unmarshal(m.OptExtra, &extra)
		for k, v := range *body.Extra {
			switch k {
			case "only_sprint":
				update.OnlySprint = new(v.(bool))
			case "no_sprint":
				update.NoSprint = new(v.(bool))
			case "no_jump":
				update.NoJump = new(v.(bool))
			case "no_sneak":
				update.NoSneak = new(v.(bool))
			case "boat":
				update.Boat = new(v.(bool))
			default:
				extra[k] = v
			}
		}
		update.Extra, _ = json.Marshal(extra)
		changed = true
	}
	if body.SpawnPoint != nil {
		update.SpawnPoint = new(dbPos(*body.SpawnPoint))
		changed = true
	}
	if body.Listed != nil && *body.Listed != m.Listed {
		update.Listed = body.Listed
		changed = true
	}
	if body.Size != nil && sizeIndex[*body.Size] != m.Size {
		update.Size = new(sizeIndex[*body.Size])
		changed = true
	}
	if body.QualityOverride != nil && (m.QualityOverride == nil || mapQualityIndex[*body.QualityOverride] != *m.QualityOverride) {
		update.Quality = new(mapQualityIndex[*body.QualityOverride])
		changed = true
	}
	if body.ProtocolVersion != nil && *body.ProtocolVersion != 0 && *body.ProtocolVersion != *m.ProtocolVersion {
		update.ProtocolVersion = body.ProtocolVersion
		changed = true
	}

	if !changed {
		return nil
	}

	return mapdb.TxNoReturn(ctx, s.mapStore, func(ctx context.Context, tx *mapdb.Store) error {
		if err = tx.UpdateMap2(ctx, update); err != nil {
			return fmt.Errorf("failed to update map: %w", err)
		}

		if newTags != nil {
			if err = tx.DeleteMapTags(ctx, update.ID); err != nil {
				return fmt.Errorf("failed to remove old tags: %w", err)
			}

			if len(newTags) > 0 {
				if err = tx.InsertMapTags(ctx, update.ID, newTags); err != nil {
					return fmt.Errorf("failed to upsert new tags: %w", err)
				}
			}
		}

		// If we changed the variant to parkour after publishing, delete any in-progress save states for the map
		if m.PublishedAt != nil && body.Variant != nil && *body.Variant == VariantParkour {
			err = s.mapStore.Unsafe_DeleteMapSaveStates(ctx, m.ID)
			if err != nil {
				// Not fatal, just error log
				s.log.Errorw("failed to delete save states when map became parkour", "mapId", m.ID, "error", err)
			}
		}

		return nil
	})
}

type DeleteMapRequest struct {
	MapID   string  `json:"mapId"`
	ActorID string  `query:"actorId"` // Player performing the deletion
	Reason  *string `query:"reason"`  // Required if the map is published or the deleter is not the owner
}

// DELETE /maps/{mapId}
func (s *Server) DeleteMap(ctx context.Context, request DeleteMapRequest) error {
	m, err := s.mapStore.GetMapById(ctx, request.MapID)
	if errors.Is(err, mapdb.ErrNoRows) {
		return ox.NotFound{}
	} else if err != nil {
		return fmt.Errorf("failed to fetch map: %w", err)
	}

	reasonRequired := m.PublishedID != nil || m.Owner != request.ActorID
	if reasonRequired && (request.Reason == nil || *request.Reason == "") {
		return ox.BadRequest{} // todo add reason
	}

	var reason string
	if reasonRequired {
		reason = *request.Reason // Nil checked above
	} else {
		reason = "user_deletion"
	}

	err = mapdb.TxNoReturn(ctx, s.mapStore, func(ctx context.Context, tx *mapdb.Store) (err error) {
		if err = tx.DeleteMap(ctx, request.MapID, &request.ActorID, &reason); err != nil {
			return fmt.Errorf("failed to delete map: %w", err)
		}

		if _, err = tx.RemoveMapFromSlots(ctx, m.ID); err != nil {
			return fmt.Errorf("failed to remove map from slots: %w", err)
		}

		if err = tx.DeleteMapBuildersForMap(ctx, m.ID); err != nil {
			return fmt.Errorf("failed to remove map builders from map: %w", err)
		}

		return nil
	})
	if err != nil {
		return err
	}

	deleteMsg := model.MapUpdateMessage{Action: model.MapUpdate_Delete, ID: request.MapID}
	if err = s.jetStream.PublishJSONAsync(ctx, deleteMsg); err != nil {
		return fmt.Errorf("failed to publish map update message: %w", err)
	}

	s.clearCachedMapSearches(ctx)

	return nil
}

// POST /maps/{mapId}/publish
func (s *Server) PublishMap(ctx context.Context, request MapRequest) (*MapData, error) {
	m, err := s.mapStore.GetMapById(ctx, request.MapID)
	if errors.Is(err, mapdb.ErrNoRows) {
		return nil, ox.NotFound{}
	} else if err != nil {
		return nil, fmt.Errorf("failed to fetch map: %w", err)
	}

	// PRECONDITION: World must exist in object storage (sanity check, but needed for metric anyway)
	worldInfo, err := s.mapsBucket.Stat(ctx, m.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch world info: %w", err)
	}

	// PRECONDITION: Owner must have spent >20m editing the map
	// todo actually implement this (it is currently checked locally before sending request)
	ownerState, err := s.mapStore.GetLatestSaveState(ctx, m.ID, m.Owner, mapdb.SaveStateTypeEditing)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch owner save state: %w", err)
	}

	//todo: check other publish preconditions

	// Update the map info with published Id & time
	publishedId, err := s.mapStore.FindNextPublishedId(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to find next published id: %w", err)
	}

	err = mapdb.TxNoReturn(ctx, s.mapStore, func(ctx context.Context, tx *mapdb.Store) error {
		if err = tx.PublishMap(ctx, m.ID, &publishedId, nil); err != nil {
			return fmt.Errorf("failed to update map: %w", err)
		}

		if _, err = tx.RemoveMapFromSlots(ctx, m.ID); err != nil {
			return fmt.Errorf("failed to remove map from slots: %w", err)
		}

		return nil
	})
	if err != nil {
		// todo this is quite bad, not sure how to roll back the removal of
		zap.S().Errorw("bad thing happened, now we have a published map with no permissions to view it.", "error", err)
		return nil, err
	}

	s.clearCachedMapSearches(ctx)

	subVariantStr := ""
	if m.OptSubvariant != nil {
		subVariantStr = *m.OptSubvariant
	}
	hog.Enqueue(hog.Capture{
		Event:      "map_published",
		DistinctId: m.Owner,
		Properties: hog.NewProperties().
			Set("map_id", m.ID).
			Set("published_map_id", publishedId).
			Set("map_name", m.OptName).
			Set("variant", m.OptVariant).
			Set("sub_variant", subVariantStr).
			Set("world_data_size", int(worldInfo.Size)).
			Set("owner_build_time", ownerState.Playtime).
			Set("contest", m.Contest),
	})

	publishedMap, err := s.mapStore.GetPublishedMapById(ctx, m.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch published map: %w", err)
	}

	return new(hydratePublishedMap(publishedMap.PublishedMap, publishedMap.Tags)), nil
}

// POST /maps/{mapId}/verify
func (s *Server) BeginMapVerification(ctx context.Context, request MapRequest) error {
	m, err := s.mapStore.GetMapById(ctx, request.MapID)
	if errors.Is(err, mapdb.ErrNoRows) {
		return ox.NotFound{}
	} else if err != nil {
		return fmt.Errorf("failed to fetch map: %w", err)
	}

	//todo better errors
	if m.PublishedID != nil || m.OptVariant == string(model.Building) || m.Verification == nil || *m.Verification != int64(model.VerificationUnverified) {
		return ox.BadRequest{}
	}

	// If we are starting to verify, destory any possible editing worlds.
	if err = s.worlds.DestroyAndWait(ctx, m.ID); err != nil {
		// We tried our best, continue anyway. The edit worlds will fail to save from now on.
		zap.S().Errorw("failed to destroy worlds", "mapId", m.ID, "error", err)
	}

	// TODO: there is kind of a race here for new build worlds to be created in this gap before setting the verification.
	// However it kinda also doesnt matter for safety because that world will fail to save.

	newVerification := int64(model.VerificationPending)
	err = s.mapStore.UpdateMapVerification(ctx, m.ID, &newVerification, nil)
	if err != nil {
		return fmt.Errorf("failed to update map: %w", err)
	}

	return nil
}

// DELETE /maps/{mapId}/verify
func (s *Server) DeleteMapVerification(ctx context.Context, request MapRequest) error {
	m, err := s.mapStore.GetMapById(ctx, request.MapID)
	if errors.Is(err, mapdb.ErrNoRows) {
		return ox.NotFound{}
	} else if err != nil {
		return fmt.Errorf("failed to fetch map: %w", err)
	}

	// Delete the leaderboard of the map to wipe the top time
	// Do it first because if it goes without the others it's not a big deal (and it can't be transactional with the others)
	err = s.redis.Do(ctx, s.redis.B().Del().Key(mapLeaderboardKey(m.ID, "playtime")).Build()).Error()
	if err != nil && !errors.Is(err, rueidis.Nil) {
		return fmt.Errorf("failed to delete leaderboard: %w", err)
	}

	// Unset the verification in the database
	err = mapdb.TxNoReturn(ctx, s.mapStore, func(ctx context.Context, tx *mapdb.Store) error {
		if err := tx.UpdateMapVerification(ctx, m.ID, new(int64(model.VerificationUnverified)), nil); err != nil {
			return fmt.Errorf("failed to update map: %w", err)
		}

		if err := tx.DeleteVerifyingStates(ctx, m.ID); err != nil {
			return fmt.Errorf("failed to delete verifying states: %w", err)
		}

		return nil
	})
	if err != nil {
		return err
	}

	return nil
}

type (
	GetMapBuildersRequest struct {
		MapID      string `path:"mapId"`
		OnlyActive bool   `query:"onlyActive"`
	}
	GetMapBuildersResponse struct {
		Results []MapBuilder `json:"results"`
	}
)

// GET /maps/{mapId}/builders
func (s *Server) GetMapBuilders(ctx context.Context, request GetMapBuildersRequest) (*GetMapBuildersResponse, error) {
	builders, err := s.mapStore.MulitGetMapBuilders(ctx, []string{request.MapID})
	if err != nil {
		return nil, fmt.Errorf("failed to get map builders: %w", err)
	}

	results := make([]MapBuilder, 0, len(builders))
	for _, b := range builders {
		if b.IsPending && request.OnlyActive {
			continue
		}

		results = append(results, MapBuilder{
			ID:        b.PlayerID,
			CreatedAt: b.CreatedAt,
			Pending:   b.IsPending,
		})
	}
	return &GetMapBuildersResponse{results}, nil
}

type InviteMapBuilderRequest struct {
	PlayerID string `json:"playerId"` // Player being invited
}

// POST /maps/{mapId}/builders
func (s *Server) InviteMapBuilder(ctx context.Context, request MapRequest, body InviteMapBuilderRequest) error {
	m, err := s.mapStore.GetMapById(ctx, request.MapID)
	if errors.Is(err, mapdb.ErrNoRows) {
		return ox.NotFound{}
	} else if err != nil {
		return fmt.Errorf("failed to get map: %w", err)
	}
	if m.PublishedAt != nil {
		return ox.BadRequest{} // Sanity
	}

	_, err = s.mapStore.CreatePendingMapBuilder(ctx, request.MapID, body.PlayerID)
	if errors.Is(err, mapdb.ErrNoRows) {
		// means it didn't return anything as row already existed
		return ox.Conflict{}
	}
	if err != nil {
		return fmt.Errorf("failed to create pending map builder: %w", err)
	}

	err = s.notifications.Create(ctx, body.PlayerID, notification.CreateInput{
		// TODO: we often have a key + subject, may be worth splitting those. For example, allows
		//  deleting all notifs with subject {map_id} when the map is published.
		//  OR: some subject idea like nats where you can filter with wildcards like map.builders.>
		Key:  fmt.Sprintf("map_builder_invites_%v", m.ID),
		Type: "map_builder_invite",
		Data: map[string]any{
			"inviterId": m.Owner,
			"mapId":     m.ID,
		},
		ReplaceUnread: true,
	})
	if err != nil {
		return fmt.Errorf("failed to send invite notification: %w", err)
	}

	return nil
}

// DELETE /maps/{mapId}/builders/{playerId}
func (s *Server) RemoveMapBuilder(ctx context.Context, request MapPlayerRequest) error {
	m, err := s.mapStore.GetMapById(ctx, request.MapID)
	if errors.Is(err, mapdb.ErrNoRows) {
		return ox.NotFound{}
	} else if err != nil {
		return fmt.Errorf("failed to get map: %w", err)
	}
	if m.PublishedAt != nil {
		return ox.BadRequest{} // Sanity
	}

	_, err = s.mapStore.RemoveMapBuilder(ctx, m.ID, request.PlayerID)
	if errors.Is(err, mapdb.ErrNoRows) {
		return ox.NotFound{}
	} else if err != nil {
		return fmt.Errorf("failed to remove pending map builder: %w", err)
	}

	// If there are any still-pending invite notifications out we need to remove them
	// Note: accepting will still not work if its still sent for whatever reason
	err = s.notifications.DeleteMatching(ctx, notification.Matcher{
		PlayerID: &request.PlayerID,
		Key:      new(fmt.Sprintf("map_builder_invites_%v", m.ID)),
	})
	if err != nil {
		return fmt.Errorf("failed to claw back invite notifications: %w", err)
	}

	// Transfer the player back to the hub in case they are currently in the map.
	// TODO: we should include a message here with the same message logic extracted from interactions
	err = s.jetStream.PublishJSONAsync(ctx, player.TransferMessage{
		PlayerId: request.PlayerID,
		From:     &m.ID,
		To:       "hub",
		State:    "playing",
	})

	return nil
}

// POST /maps/{mapId}/builders/{playerId}/accept
func (s *Server) AcceptMapBuilderRequest(ctx context.Context, request MapPlayerRequest) error {
	pd, err := s.playerStore.GetPlayerData(ctx, request.PlayerID)
	if errors.Is(err, playerdb.ErrNoRows) {
		return ox.NotFound{}
	} else if err != nil {
		return fmt.Errorf("failed to get player data: %w", err)
	}

	m, err := s.mapStore.GetMapById(ctx, request.MapID)
	if errors.Is(err, mapdb.ErrNoRows) {
		return ox.NotFound{}
	} else if err != nil {
		return fmt.Errorf("failed to get map: %w", err)
	}
	if m.PublishedAt != nil {
		return ox.BadRequest{} // Sanity
	}

	usedSlots, err := s.mapStore.GetMapBuilderPlayerSlotsCount(ctx, pd.ID)
	if err != nil {
		return fmt.Errorf("failed to get used map builder slots: %w", err)
	}
	if int(usedSlots) >= pd.TotalMapSlots() {
		return ox.BadRequest{} // No slots available!!
	}

	_, err = s.mapStore.AcceptMapBuilder(ctx, request.MapID, pd.ID)
	if errors.Is(err, mapdb.ErrNoRows) {
		return ox.NotFound{}
	} else if err != nil {
		return fmt.Errorf("failed to approve pending map builder: %w", err)
	}

	return nil
}

// POST /maps/{mapId}/builders/{playerId}/reject
func (s *Server) RejectMapBuilderRequest(ctx context.Context, request MapPlayerRequest) error {
	m, err := s.mapStore.GetMapById(ctx, request.MapID)
	if errors.Is(err, mapdb.ErrNoRows) {
		return ox.NotFound{}
	} else if err != nil {
		return fmt.Errorf("failed to get map: %w", err)
	}
	if m.PublishedAt != nil {
		return nil // Silently drop
	}

	_, err = s.mapStore.RejectMapBuilder(ctx, m.ID, request.PlayerID)
	if errors.Is(err, mapdb.ErrNoRows) {
		return ox.NotFound{}
	} else if err != nil {
		return fmt.Errorf("failed to remove pending map builder: %w", err)
	}

	// Tell the map author their invite was denied
	err = s.notifications.Create(ctx, m.Owner, notification.CreateInput{
		Type:      "map_builder_rejected",
		ExpiresIn: nil,
		Data: map[string]interface{}{
			"mapId":     m.ID,
			"builderId": request.PlayerID,
		},
	})

	return nil
}

type ReportMapRequestBody struct {
	Reporter   string              `json:"reporter"`
	Categories []MapReportCategory `json:"categories"`
	Comment    string              `json:"comment"`
}

// POST /maps/{mapId}/report
func (s *Server) ReportMap(ctx context.Context, request MapRequest, body ReportMapRequestBody) error {
	m, err := s.mapStore.GetMapById(ctx, request.MapID)
	if errors.Is(err, mapdb.ErrNoRows) {
		return ox.NotFound{}
	} else if err != nil {
		return fmt.Errorf("failed to fetch map: %w", err)
	}

	var shouldDislike = false
	reportParams := mapdb.InsertMapReportParams{
		MapID:      request.MapID,
		PlayerID:   body.Reporter,
		Categories: make([]int, len(body.Categories)),
		Comment:    util.EmptyToNil(body.Comment),
	}
	var ok bool
	for i, category := range body.Categories {
		reportParams.Categories[i], ok = mapReportCategoryIndex[category]
		if !ok {
			return ox.BadRequest{}
		}

		if model.ReportCategoriesToDislike[reportParams.Categories[i]] == true {
			shouldDislike = true
		}
	}

	// Save the report to the database immediately for future lookup
	report, err := s.mapStore.InsertMapReport(ctx, reportParams)
	if err != nil {
		return fmt.Errorf("failed to write report: %w", err)
	}
	s.log.Infow("created map report #"+strconv.Itoa(report.ID), "report", report)

	if shouldDislike {
		// Submitting a report that is dislikable should always results in disliking the map
		err = s.mapStore.UpsertMapRating(ctx, report.MapID, report.PlayerID, int(model.RatingStateDisliked))
		if err != nil {
			// This is non fatal, just log it
			s.log.Errorw("failed to dislike map during report", "error", err)
		}
	}

	hog.Enqueue(hog.Capture{
		Event:      "map_reported",
		DistinctId: body.Reporter,
		Properties: hog.NewProperties().
			Set("map_id", m.ID).
			Set("map_name", util.NilToEmpty(m.OptName)).
			Set("categories", body.Categories).
			Set("comment", body.Comment),
	})

	return nil
}

// GET /maps/{mapId}/ratings/{playerId}
func (s *Server) GetMapPlayerRating(ctx context.Context, request MapPlayerRequest) (*MapRating, error) {
	rating, err := s.mapStore.GetMapRatingForMapBy(ctx, request.MapID, request.PlayerID)
	if errors.Is(err, mapdb.ErrNoRows) {
		rating.MapID = request.MapID
		rating.PlayerID = request.PlayerID
		rating.Rating = int(model.RatingStateUnrated)
	} else if err != nil {
		return nil, fmt.Errorf("failed to fetch map rating: %w", err)
	}

	return new(hydrateMapRating(rating)), nil
}

type RateMapRequestBody struct {
	State MapRatingState `json:"state"`
}

// PUT /maps/{mapId}/ratings/{playerId}
func (s *Server) SetMapPlayerRating(ctx context.Context, request MapPlayerRequest, body RateMapRequestBody) error {
	rating, ok := mapRatingStateIndex[body.State]
	if !ok {
		return ox.BadRequest{}
	}

	err := s.mapStore.UpsertMapRating(ctx, request.MapID, request.PlayerID, rating)
	if err != nil {
		return fmt.Errorf("failed to set map rating: %w", err)
	}

	//todo update map stats

	return nil
}

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
	// we could probably get builders in the same query, but sqlc generates useless types for that scenario.
	// TODO: can delete map_builders table
	builders, err := s.mapStore.MulitGetMapBuilders(ctx, mapIds)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch map builders: %w", err)
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

	unpublishedResults := make([]MapSlot, len(slots))
	for i, slot := range slots {
		for _, m := range maps {
			if m.Map.ID != slot.MapID {
				continue
			}

			// TODO: builder slots should have the created time set to the acceptance time probably
			sl := MapSlot{
				Map:       hydrateMap(m.Map, m.Tags),
				CreatedAt: slot.CreatedAt,
				Builders:  []MapBuilder{},
			}

			if request.PlayerId == m.Map.Owner {
				sl.Role = RoleOwner
			} else {
				sl.Role = RoleBuilder
			}

			for _, b := range builders {
				if b.MapID != slot.MapID {
					continue
				}
				// Don't include the owner in the builder list for now. In the future it may be better to include
				if b.PlayerID == m.Map.Owner {
					continue
				}

				sl.Builders = append(sl.Builders, MapBuilder{
					ID:        b.PlayerID,
					CreatedAt: b.CreatedAt,
					Pending:   b.IsPending,
				})
			}

			unpublishedResults[i] = sl
		}
	}

	slices.SortFunc(unpublishedResults, func(a, b MapSlot) int {
		return b.CreatedAt.Compare(a.CreatedAt)
	})

	publishedResults := make([]MapSlot, len(published))
	for i, m := range published {
		publishedResults[i] = MapSlot{
			Map:       hydratePublishedMap(m.PublishedMap, m.Tags),
			CreatedAt: *m.PublishedMap.PublishedAt,
			Role:      RoleOwner,
		}
	}

	return &GetMapSlotsResponse{
		Results: append(unpublishedResults, publishedResults...),
	}, nil
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
	end := int(limit)

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

func (s *Server) clearCachedMapSearches(ctx context.Context) {
	cachedKeys, err := s.redis.Do(ctx, s.redis.B().Keys().Pattern("maps:search:*").Build()).AsStrSlice()
	if err != nil || len(cachedKeys) == 0 {
		return // DNC about error, we tried our best
	}
	err = s.redis.Do(ctx, s.redis.B().Del().Key(cachedKeys...).Build()).Error()
	if err != nil {
		s.log.Errorw("failed to clear cached map searches", "err", err)
	}
}
