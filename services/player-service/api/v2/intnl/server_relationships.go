package intnl

import (
	"context"
	"fmt"

	"github.com/hollow-cube/hc-services/services/player-service/internal/db"
)

func (s *server) GetBlockedPlayers(ctx context.Context, request GetBlockedPlayersRequestObject) (GetBlockedPlayersResponseObject, error) {
	rows, err := s.store.GetRelationships(ctx, request.PlayerId, db.RelationshipStatusBlocked)
	if err != nil {
		return nil, fmt.Errorf("failed to get blocked players: %w", err)
	}

	blocks := make([]BlockedPlayer, len(rows))
	for i, row := range rows {
		blocks[i] = BlockedPlayer{
			BlockedAt: row.CreatedAt,
			PlayerId:  row.PlayerID,
			Username:  row.Username,
		}
	}

	return GetBlockedPlayers200JSONResponse(blocks), nil
}

func (s *server) BlockPlayer(ctx context.Context, request BlockPlayerRequestObject) (BlockPlayerResponseObject, error) {
	//TODO implement me
	panic("implement me")
}

func (s *server) UnblockPlayer(ctx context.Context, request UnblockPlayerRequestObject) (UnblockPlayerResponseObject, error) {
	modified, err := s.store.DeleteRelationship(ctx, request.PlayerId, request.TargetId, false)
	if err != nil {
		return nil, fmt.Errorf("failed to unblock player: %w", err)
	}

	if modified == 0 {
		return UnblockPlayer404Response{}, nil
	}

	return UnblockPlayer204Response{}, nil
}

func (s *server) GetPlayerFriends(ctx context.Context, request GetPlayerFriendsRequestObject) (GetPlayerFriendsResponseObject, error) {
	rows, err := s.store.GetRelationships(ctx, request.PlayerId, db.RelationshipStatusFriend)
	if err != nil {
		return nil, fmt.Errorf("failed to get friends: %w", err)
	}

	friends := make([]PlayerFriend, len(rows))
	for i, row := range rows {
		friends[i] = PlayerFriend{
			FriendsSince: row.CreatedAt,
			PlayerId:     row.PlayerID,
			Username:     row.Username,
		}
	}

	return GetPlayerFriends200JSONResponse(friends), nil
}

func (s *server) GetFriendRequests(ctx context.Context, request GetFriendRequestsRequestObject) (GetFriendRequestsResponseObject, error) {
	outgoing := request.Params.Direction == Outgoing

	var friendRequests []FriendRequest
	if outgoing {
		rows, err := s.store.GetRelationships(ctx, request.PlayerId, db.RelationshipStatusPending)
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
	// todo check if the other player has already requested, if so, just make them friends
	// todo check if they are blocked and if so, deny it. Need a specific error code for this.
	_, err := s.store.CreateRelationship(ctx, request.PlayerId, request.Body.TargetId, db.RelationshipStatusPending)
	// todo check for err being a conflict, if so, return a 409
	if err != nil {
		return nil, fmt.Errorf("failed to send friend request: %w", err)
	}

	// todo we need some kind of Kafka/Redis broadcast to notify the other player if they are online

	return SendFriendRequest201Response{}, nil
}

func (s *server) DeleteFriendRequest(ctx context.Context, request DeleteFriendRequestRequestObject) (DeleteFriendRequestResponseObject, error) {
	modified, err := s.store.DeleteRelationship(ctx, request.PlayerId, request.TargetId, false)
	if err != nil {
		return nil, fmt.Errorf("failed to delete friend request: %w", err)
	}

	if modified == 0 {
		return DeleteFriendRequest404Response{}, nil
	}

	return DeleteFriendRequest204Response{}, nil
}

func (s *server) AcceptFriendRequest(ctx context.Context, request AcceptFriendRequestRequestObject) (AcceptFriendRequestResponseObject, error) {
	//TODO implement me
	panic("implement me")
}

func (s *server) RemoveFriend(ctx context.Context, request RemoveFriendRequestObject) (RemoveFriendResponseObject, error) {
	modified, err := s.store.DeleteRelationship(ctx, request.PlayerId, request.TargetId, true)
	if err != nil {
		return nil, fmt.Errorf("failed to remove friend: %w", err)
	}

	if modified == 0 {
		return RemoveFriend404Response{}, nil
	}

	return RemoveFriend204Response{}, nil
}
