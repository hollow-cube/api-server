package v4Internal

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/hollow-cube/api-server/internal/mapdb"
	"github.com/hollow-cube/api-server/internal/pkg/notification"
	"github.com/hollow-cube/api-server/internal/pkg/player"
	"github.com/hollow-cube/api-server/internal/playerdb"
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
