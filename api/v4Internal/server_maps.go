package v4Internal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/hollow-cube/api-server/internal/mapdb"
	"github.com/hollow-cube/api-server/internal/pkg/model"
	"github.com/hollow-cube/api-server/internal/pkg/notification"
	"github.com/hollow-cube/api-server/internal/pkg/player"
	"github.com/hollow-cube/api-server/internal/playerdb"
	"github.com/hollow-cube/api-server/pkg/hog"
	"github.com/hollow-cube/api-server/pkg/ox"
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
	mt, err := s.mapStore.GetMapWithTagsById(ctx, request.MapID)
	if errors.Is(err, mapdb.ErrNoRows) {
		return nil, ox.NotFound{}
	} else if err != nil {
		return nil, fmt.Errorf("failed to get map: %w", err)
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
	if body.QualityOverride != nil && (m.QualityOverride == nil || qualityIndex[*body.QualityOverride] != *m.QualityOverride) {
		update.Quality = new(qualityIndex[*body.QualityOverride])
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
	if int(usedSlots) >= pd.TotalBuilderSlots() {
		return ox.BadRequest{} // No slots available!!
	}

	_, err = s.mapStore.AcceptMapBuilder(ctx, request.MapID, pd.ID)
	if errors.Is(err, mapdb.ErrNoRows) {
		return ox.NotFound{}
	} else if err != nil {
		return fmt.Errorf("failed to approve pending map builder: %w", err)
	}

	// TODO: would be nice if notifications were strongly typed structs
	// Tell the map author their invite was accepted
	err = s.notifications.Create(ctx, m.Owner, notification.CreateInput{
		Type:      "map_builder_accepted",
		ExpiresIn: nil,
		Data: map[string]interface{}{
			"mapId":     m.ID,
			"builderId": pd.ID,
		},
	})

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
			Map:       hydratePublishedMap(m.PublishedMap),
			CreatedAt: *m.PublishedMap.PublishedAt,
			Role:      RoleOwner,
		}
	}

	return &GetMapSlotsResponse{
		Results: append(unpublishedResults, publishedResults...),
	}, nil
}
