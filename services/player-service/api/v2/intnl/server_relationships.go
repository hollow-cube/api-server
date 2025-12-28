package intnl

import (
	"context"
	"errors"
	"fmt"

	"github.com/hollow-cube/hc-services/services/player-service/internal/db"
	"github.com/jackc/pgx/v5"
)

func (s *server) GetBlockedPlayers(ctx context.Context, request GetBlockedPlayersRequestObject) (GetBlockedPlayersResponseObject, error) {
	rows, err := s.store.GetBlockedPlayers(ctx, request.PlayerId)
	if err != nil {
		return nil, fmt.Errorf("failed to get blocked players: %w", err)
	}

	blocks := make([]BlockedPlayer, len(rows))
	for i, row := range rows {
		blocks[i] = BlockedPlayer{
			BlockedAt: row.CreatedAt,
			PlayerId:  row.TargetID,
			Username:  row.Username,
		}
	}

	return GetBlockedPlayers200JSONResponse(blocks), nil
}

func (s *server) BlockPlayer(ctx context.Context, request BlockPlayerRequestObject) (BlockPlayerResponseObject, error) {
	alreadyBlocked, err := s.store.IsBlocked(ctx, request.PlayerId, request.TargetId)
	if err != nil {
		return nil, fmt.Errorf("failed to check if player is blocked: %w", err)
	}

	if alreadyBlocked {
		return BlockPlayer409Response{}, nil
	}

	if err := db.TxNoReturn(ctx, s.store, func(ctx context.Context, txStore *db.Store) error {
		// Delete existing friendships and friend requests before making the block
		if _, err := txStore.DeletePlayerFriendBidirectional(ctx, request.PlayerId, request.TargetId); err != nil {
			return fmt.Errorf("failed to delete friendships: %w", err)
		}
		if _, err := txStore.DeleteFriendRequestBidirectional(ctx, request.PlayerId, request.TargetId); err != nil {
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

func (s *server) GetPlayerFriends(ctx context.Context, request GetPlayerFriendsRequestObject) (GetPlayerFriendsResponseObject, error) {
	rows, err := s.store.GetPlayerFriends(ctx, request.PlayerId)
	if err != nil {
		return nil, fmt.Errorf("failed to get friends: %w", err)
	}

	friends := make([]PlayerFriend, len(rows))
	for i, row := range rows {
		friends[i] = PlayerFriend{
			FriendsSince: row.CreatedAt,
			PlayerId:     row.TargetID,
			Username:     row.Username,
		}
	}

	return GetPlayerFriends200JSONResponse(friends), nil
}

func (s *server) GetFriendRequests(ctx context.Context, request GetFriendRequestsRequestObject) (GetFriendRequestsResponseObject, error) {
	outgoing := request.Params.Direction == Outgoing

	var friendRequests []FriendRequest
	if outgoing {
		rows, err := s.store.GetOutgoingFriendRequests(ctx, request.PlayerId)
		if err != nil {
			return nil, fmt.Errorf("failed to get friend requests: %w", err)
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
		rows, err := s.store.GetIncomingFriendRequests(ctx, request.PlayerId)
		if err != nil {
			return nil, fmt.Errorf("failed to get friend requests: %w", err)
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

	return GetFriendRequests200JSONResponse(friendRequests), nil
}

func (s *server) SendFriendRequest(ctx context.Context, request SendFriendRequestRequestObject) (SendFriendRequestResponseObject, error) {
	// Check if they are already friends
	alreadyFriends, err := s.store.FriendshipExists(ctx, request.PlayerId, request.TargetId)
	if err != nil {
		return nil, fmt.Errorf("failed to check if friends: %w", err)
	}

	if alreadyFriends {
		return SendFriendRequest409JSONResponse{Code: "already_friends", Message: "you are already friends with this player"}, nil
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

		// todo use new Notification service once implemented to notify of friendship
		return SendFriendRequest201JSONResponse{IsRequest: false}, nil
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

	// todo use new Notification service once implemented to notify of request

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
