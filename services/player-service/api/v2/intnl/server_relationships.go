package intnl

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/hollow-cube/hc-services/libraries/common/pkg/util"
	"github.com/hollow-cube/hc-services/services/player-service/internal/db"
	"github.com/hollow-cube/hc-services/services/player-service/internal/pkg/authz"
	"github.com/jackc/pgx/v5"
)

func (s *server) GetBlockedPlayers(ctx context.Context, request GetBlockedPlayersRequestObject) (GetBlockedPlayersResponseObject, error) {
	pageSize := request.Params.PageSize
	offset := (request.Params.Page - 1) * pageSize

	rows, err := s.store.GetBlockedPlayers(ctx, request.PlayerId, pageSize, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to get blocked players: %w", err)
	}

	if len(rows) == 0 {
		return GetBlockedPlayers200JSONResponse{Items: make([]BlockedPlayer, 0), Page: 1, TotalItems: 0}, nil
	}

	targetIds := make([]string, len(rows))
	for i, row := range rows {
		targetIds[i] = row.TargetID
	}
	staffStates, err := s.authzClient.MultiCheckPlatformPermission(ctx, targetIds, authz.NoKey, authz.PlatformBanPlayer)
	if err != nil {
		return nil, fmt.Errorf("failed to check if blocked players are staff members: %w", err)
	}

	blocks := make([]BlockedPlayer, 0, len(rows))
	for _, row := range rows {
		// Ignore blocked players that are staff members (they became staff after they were already blocked)
		staffState, ok := staffStates[row.TargetID]
		if !ok {
			s.log.Warnw("staff state not found in SpiceDB for player %s", "targetId", row.TargetID)
		}
		if staffState == authz.Allow || staffState == authz.Conditional { // we must accept conditional due to the audit log hack applied
			continue
		}

		blocks = append(blocks, BlockedPlayer{
			BlockedAt: row.CreatedAt,
			PlayerId:  row.TargetID,
			Username:  row.Username,
		})
	}

	return GetBlockedPlayers200JSONResponse{
		Items:      blocks,
		Page:       request.Params.Page,
		TotalItems: rows[0].Total,
	}, nil
}

func (s *server) BlockPlayer(ctx context.Context, request BlockPlayerRequestObject) (BlockPlayerResponseObject, error) {
	alreadyBlocked, err := s.store.IsBlocked(ctx, request.PlayerId, request.TargetId)
	if err != nil {
		return nil, fmt.Errorf("failed to check if player is blocked: %w", err)
	}

	if alreadyBlocked {
		return BlockPlayer409Response{}, nil
	}

	// Check if the target is a staff member - players cannot block staff members
	staffState, err := s.authzClient.CheckPlatformPermission(ctx, request.TargetId, authz.NoKey, authz.PlatformBanPlayer)
	if err != nil {
		return nil, fmt.Errorf("failed to check if target is staff member: %w", err)
	}
	if staffState == authz.Allow || staffState == authz.Conditional { // We must accept conditional due to the audit log hack applied
		return BlockPlayer400Response{}, nil
	}

	if err := db.TxNoReturn(ctx, s.store, func(ctx context.Context, txStore *db.Store) error {
		// Delete existing friendships and friend requests before making the block
		if _, err := txStore.DeletePlayerFriendBidirectional(ctx, request.PlayerId, request.TargetId); err != nil {
			return fmt.Errorf("failed to delete friendships: %w", err)
		}
		if _, err := txStore.DeleteFriendRequestBidirectional(ctx, request.PlayerId, request.TargetId); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("failed to delete friend requests: %w", err)
		}
		if err := txStore.CreatePlayerBlock(ctx, request.PlayerId, request.TargetId); err != nil {
			return fmt.Errorf("failed to create block: %w", err)
		}

		return nil
	}); err != nil {
		return nil, fmt.Errorf("failed to block player: %w", err)
	}
	return BlockPlayer201Response{}, nil
}

func (s *server) UnblockPlayer(ctx context.Context, request UnblockPlayerRequestObject) (UnblockPlayerResponseObject, error) {
	modified, err := s.store.DeletePlayerBlock(ctx, request.PlayerId, request.TargetId)
	if err != nil {
		return nil, fmt.Errorf("failed to unblock player: %w", err)
	}

	if modified == 0 {
		return UnblockPlayer404Response{}, nil
	}

	return UnblockPlayer204Response{}, nil
}

func (s *server) GetBlocksBetweenPlayers(ctx context.Context, request GetBlocksBetweenPlayersRequestObject) (GetBlocksBetweenPlayersResponseObject, error) {
	blocks, err := s.store.GetBlocksBetween(ctx, request.PlayerId, request.TargetId, request.Params.Bidirectional)
	if err != nil {
		return nil, fmt.Errorf("failed to get blocks between players: %w", err)
	}

	if len(blocks) == 0 {
		return GetBlocksBetweenPlayers200JSONResponse(make([]BlockedPlayer, 0)), nil
	}

	// filter out any blocks that are staff members (they became staff after they were already blocked)
	staffStates, err := s.authzClient.MultiCheckPlatformPermission(ctx, []string{request.TargetId, request.PlayerId}, authz.NoKey, authz.PlatformBanPlayer)
	if err != nil {
		return nil, fmt.Errorf("failed to check if blocked players are staff members: %w", err)
	}

	blocksToReturn := make([]BlockedPlayer, 0, len(blocks))
	for _, block := range blocks {
		staffState := staffStates[block.TargetID]
		if staffState != authz.Allow && staffState != authz.Conditional { // not staff, so a valid block
			blocksToReturn = append(blocksToReturn, BlockedPlayer{
				BlockedAt: block.CreatedAt,
				PlayerId:  block.TargetID,
				Username:  block.Username,
			})
		}
	}

	return GetBlocksBetweenPlayers200JSONResponse(blocksToReturn), nil
}

func (s *server) GetPlayerFriends(ctx context.Context, request GetPlayerFriendsRequestObject) (GetPlayerFriendsResponseObject, error) {
	pageSize := request.Params.PageSize
	offset := (request.Params.Page - 1) * pageSize

	rows, err := s.store.GetPlayerFriends(ctx, request.PlayerId, pageSize, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to get friends: %w", err)
	}

	totalItems := int64(0)
	if len(rows) > 0 {
		totalItems = rows[0].TotalCount
	}

	friends := make([]PlayerFriend, len(rows))
	for i, row := range rows {
		friends[i] = PlayerFriend{
			FriendsSince: row.CreatedAt,
			PlayerId:     row.TargetID,
			Username:     row.Username,
			Online:       row.Online,
			LastOnline:   row.LastOnline,
		}
	}

	return GetPlayerFriends200JSONResponse{
		Page:       request.Params.Page,
		TotalItems: totalItems,
		Items:      friends,
	}, nil
}

func (s *server) GetFriendRequests(ctx context.Context, request GetFriendRequestsRequestObject) (GetFriendRequestsResponseObject, error) {
	outgoing := request.Params.Direction == Outgoing
	pageSize := request.Params.PageSize
	offset := (request.Params.Page - 1) * pageSize

	totalItems := int64(0)
	var friendRequests []FriendRequest
	if outgoing {
		rows, err := s.store.GetOutgoingFriendRequests(ctx, request.PlayerId, pageSize, offset)
		if err != nil {
			return nil, fmt.Errorf("failed to get friend requests: %w", err)
		}
		if len(rows) > 0 {
			totalItems = rows[0].TotalCount
		}

		friendRequests = make([]FriendRequest, len(rows))
		for i, row := range rows {
			friendRequests[i] = FriendRequest{
				PlayerId: row.TargetID,
				SentAt:   row.CreatedAt,
				Username: row.Username,
			}
		}
	} else {
		rows, err := s.store.GetIncomingFriendRequests(ctx, request.PlayerId, pageSize, offset)
		if err != nil {
			return nil, fmt.Errorf("failed to get friend requests: %w", err)
		}
		if len(rows) > 0 {
			totalItems = rows[0].TotalCount
		}

		friendRequests = make([]FriendRequest, len(rows))
		for i, row := range rows {
			friendRequests[i] = FriendRequest{
				PlayerId: row.PlayerID,
				SentAt:   row.CreatedAt,
				Username: row.Username,
			}
		}
	}

	return GetFriendRequests200JSONResponse{
		Page:       request.Params.Page,
		TotalItems: totalItems,
		Items:      friendRequests,
	}, nil
}

const (
	freeFriendLimit      = 15
	hypercubeFriendLimit = 1000
)

func (s *server) SendFriendRequest(ctx context.Context, request SendFriendRequestRequestObject) (SendFriendRequestResponseObject, error) {
	// Check if they are already friends
	alreadyFriends, err := s.store.FriendshipExists(ctx, request.PlayerId, request.TargetId)
	if err != nil {
		return nil, fmt.Errorf("failed to check if friends: %w", err)
	}

	if alreadyFriends {
		return SendFriendRequest409JSONResponse{Code: "already_friends", Message: "you are already friends with this player"}, nil
	}

	usageResult, err := s.store.GetPlayerFriendUsage(ctx, request.PlayerId)
	if err != nil {
		return nil, fmt.Errorf("failed to get friend usage: %w", err)
	}

	totalUsage := usageResult.OutgoingFriendRequestCount + usageResult.FriendCount
	if totalUsage >= freeFriendLimit {
		isHyperCube, err := s.authzClient.HasHypercube(ctx, request.PlayerId, authz.NoKey)
		if err != nil {
			return nil, fmt.Errorf("failed to check if player has hypercube: %w", err)
		}
		limit := freeFriendLimit
		if isHyperCube {
			limit = hypercubeFriendLimit
		}

		if totalUsage >= limit {
			return SendFriendRequest401JSONResponse{
				Code: "friend_limit_reached", Message: "you have reached the friend limit for your account",
				Limit:                int32(limit),
				FriendCount:          int32(usageResult.FriendCount),
				OutgoingRequestCount: int32(usageResult.OutgoingFriendRequestCount),
			}, nil
		}
	}

	// Check if an opposite existing friend request already exists, if so they just become friends.
	reqAlreadyExists, err := s.store.FriendRequestExists(ctx, request.TargetId, request.PlayerId)
	if err != nil {
		return nil, fmt.Errorf("failed to check if opposite friend request exists: %w", err)
	}

	if reqAlreadyExists {
		if err := db.TxNoReturn(ctx, s.store, func(ctx context.Context, txStore *db.Store) error {
			if err := txStore.CreatePlayerFriend(ctx, request.PlayerId, request.TargetId); err != nil {
				return fmt.Errorf("failed to create friendship: %w", err)
			}
			if err := txStore.CreatePlayerFriend(ctx, request.TargetId, request.PlayerId); err != nil {
				return fmt.Errorf("failed to create friendship: %w", err)
			}
			if _, err := txStore.DeleteFriendRequest(ctx, request.TargetId, request.PlayerId); err != nil {
				return fmt.Errorf("failed to delete friend request: %w", err)
			}

			return nil
		}); err != nil {
			return nil, fmt.Errorf("failed to create friendship: %w", err)
		}

		notifReq := CreatePlayerNotificationRequestObject{
			PlayerId: request.TargetId,
			Params:   CreatePlayerNotificationParams{ReplaceUnread: util.Ptr(false)},
			Body: &CreatePlayerNotificationJSONRequestBody{
				Type:      "friend_added",
				ExpiresIn: util.Ptr(0), // expire immediately, just for the toast
				Key:       request.PlayerId,
			},
		}
		// Send notification of friend request (to both players)
		if err := s.createPlayerNotification(ctx, notifReq); err != nil {
			s.log.Warnw("failed to send friendship notification - continuing", "playerId", request.PlayerId, "targetId", request.TargetId, "err", err)
		}

		notifReq.PlayerId = request.PlayerId
		notifReq.Body.Key = request.TargetId
		if err := s.createPlayerNotification(ctx, notifReq); err != nil {
			s.log.Warnw("failed to send friendship notification - continuing", "playerId", request.PlayerId, "targetId", request.TargetId, "err", err)
		}
		return SendFriendRequest201JSONResponse{IsRequest: false}, nil
	}

	// Check if target is auto-rejecting friend requests
	targetData, err := s.store.GetPlayerData(ctx, request.TargetId)
	if err != nil {
		return nil, fmt.Errorf("failed to get target settings: %w", err)
	}
	if targetData.Settings.GetBool(db.PlayerSettingAutoRejectFriendRequests) {
		return SendFriendRequest409JSONResponse{Code: "target_auto_rejects_friend_requests", Message: "target has auto-rejecting friend requests enabled"}, nil
	}

	// Check for blocks in both directions

	hasTargetBlocked, err := s.store.IsBlocked(ctx, request.PlayerId, request.TargetId)
	if err != nil {
		return nil, fmt.Errorf("failed to check if target is blocked: %w", err)
	}

	if hasTargetBlocked {
		return SendFriendRequest409JSONResponse{Code: "player_blocked", Message: "cannot perform this action as you have blocked the target"}, nil
	}

	isBlockedByTarget, err := s.store.IsBlocked(ctx, request.TargetId, request.PlayerId)
	if err != nil {
		return nil, fmt.Errorf("failed to check if target is blocked: %w", err)
	}

	if isBlockedByTarget {
		return SendFriendRequest409JSONResponse{Code: "blocked_by_player", Message: "cannot perform this action as you are blocked by the target"}, nil
	}

	// Create a friend request as all previous checks passed

	if err := s.store.CreateFriendRequest(ctx, request.PlayerId, request.TargetId); err != nil {
		if db.ErrIsUniqueViolationWithConstr(err, "player_friend_requests_pkey") {
			return SendFriendRequest409JSONResponse{Code: "friend_request_already_exists", Message: "friend request already sent to this player"}, nil
		}
		return nil, fmt.Errorf("failed to create friend request: %w", err)
	}

	// Send notification of friend request
	if err := s.createPlayerNotification(ctx, CreatePlayerNotificationRequestObject{
		PlayerId: request.TargetId,
		Params:   CreatePlayerNotificationParams{ReplaceUnread: util.Ptr(true)},
		Body: &CreatePlayerNotificationJSONRequestBody{
			Type: "friend_request",
			Key:  request.PlayerId, // Use the player ID as the key for a friend_request type
		},
	}); err != nil {
		s.log.Warnw("failed to send friendship notification - continuing", "playerId", request.PlayerId, "targetId", request.TargetId, "err", err)
	}

	return SendFriendRequest201JSONResponse{IsRequest: true}, nil
}

func (s *server) DeleteFriendRequest(ctx context.Context, request DeleteFriendRequestRequestObject) (DeleteFriendRequestResponseObject, error) {
	var deletedReq FriendRequest
	if request.Params.Bidirectional {
		row, err := s.store.DeleteFriendRequestBidirectional(ctx, request.PlayerId, request.TargetId)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return DeleteFriendRequest404Response{}, nil
			}
			return nil, fmt.Errorf("failed to delete friend request: %w", err)
		}
		deletedReq = FriendRequest{
			PlayerId: row.PlayerID,
			SentAt:   row.CreatedAt,
			Username: row.Username,
		}
	} else {
		row, err := s.store.DeleteFriendRequest(ctx, request.PlayerId, request.TargetId)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return DeleteFriendRequest404Response{}, nil
			}
			return nil, fmt.Errorf("failed to delete friend request: %w", err)
		}
		deletedReq = FriendRequest{
			PlayerId: row.TargetID,
			SentAt:   row.CreatedAt,
			Username: row.Username,
		}
	}

	// todo delete notification

	return DeleteFriendRequest200JSONResponse(deletedReq), nil
}

func (s *server) RemoveFriend(ctx context.Context, request RemoveFriendRequestObject) (RemoveFriendResponseObject, error) {
	modified, err := s.store.DeletePlayerFriendBidirectional(ctx, request.PlayerId, request.TargetId)
	if err != nil {
		return nil, fmt.Errorf("failed to remove friend: %w", err)
	}

	if modified == 0 {
		return RemoveFriend404Response{}, nil
	}

	return RemoveFriend204Response{}, nil
}
