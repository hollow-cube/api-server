package handler

import (
	"context"
	"fmt"

	"github.com/hollow-cube/hc-services/services/session-service/internal/pkg/model"
	"github.com/hollow-cube/hc-services/services/session-service/internal/pkg/player"
)

func (i *InviteManager) Reject(ctx context.Context, senderId string, recipientId string) (*model.MapInvite, error) {
	if recipientId == "" {
		return i.rejectNoRecipient(ctx, senderId)
	} else {
		return i.rejectWithRecipient(ctx, senderId, recipientId)
	}
}

func (i *InviteManager) rejectNoRecipient(ctx context.Context, senderId string) (*model.MapInvite, error) {
	defaultInviteKey, err := i.getDefaultKey(ctx, senderId)
	if err != nil {
		return nil, fmt.Errorf("failed to get default invite: %w", err)
	}

	if defaultInviteKey == "" {
		return nil, ErrNoInvitesOrRequests
	}

	invite, err := i.get(ctx, defaultInviteKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get invite: %w", err)
	}

	if invite == nil {
		err = i.removeDefaultKey(ctx, senderId)
		if err != nil {
			return nil, fmt.Errorf("failed to remove default invite key: %w", err)
		}
		return nil, ErrInviteNotFound
	}

	switch invite.Type {
	case model.InviteTypeInvite:
		return i.rejectInvite(ctx, invite)
	case model.InviteTypeRequest:
		return i.rejectRequest(ctx, invite)
	}
	return nil, fmt.Errorf("invalid invite type: %s", invite.Type)
}

func (i *InviteManager) rejectWithRecipient(ctx context.Context, senderId string, recipientId string) (*model.MapInvite, error) {
	invite, err := i.GetInvite(ctx, senderId, recipientId)
	if err != nil {
		return nil, fmt.Errorf("failed to get invite: %w", err)
	}

	request, err := i.GetRequest(ctx, senderId, recipientId)
	if err != nil {
		return nil, fmt.Errorf("failed to get request: %w", err)
	}

	if invite != nil && request != nil {
		// Whichever came first wins
		inviteTime := invite.CreatedAt
		requestTime := request.CreatedAt

		if inviteTime.Before(requestTime) {
			// Invite came first - remove request and accept invite
			err = i.remove(ctx, request)
			if err != nil {
				return nil, fmt.Errorf("failed to remove request: %w", err)
			}
			return i.rejectInvite(ctx, invite)
		} else {
			// Request came first - remove invite and accept request
			err = i.remove(ctx, invite)
			if err != nil {
				return nil, fmt.Errorf("failed to remove invite: %w", err)
			}
			return i.rejectRequest(ctx, request)
		}
	} else if invite != nil {
		return i.rejectInvite(ctx, invite)
	} else if request != nil {
		return i.rejectRequest(ctx, request)
	} else {
		return nil, ErrNoInvitesOrRequests
	}
}

func (i *InviteManager) rejectInvite(ctx context.Context, invite *model.MapInvite) (*model.MapInvite, error) {
	if invite == nil {
		return nil, ErrNoInvitesOrRequests
	}

	senderId := invite.SenderId
	senderSession, err := i.playerTracker.GetSession(ctx, senderId)
	if err != nil {
		return nil, fmt.Errorf("failed to get target's session: %w", err)
	}

	var inviteErr error
	if senderSession == nil || senderSession.PType == nil {
		inviteErr = ErrInviteSenderOffline
	} else {
		if *senderSession.PType != string(player.PresenceTypeMapMakerMap) || *senderSession.PMapID != invite.MapId {
			inviteErr = ErrInviteSenderLeftMap
		}
	}

	err = i.remove(ctx, invite)
	if err != nil {
		return nil, fmt.Errorf("failed to remove invite: %w", err)
	}

	if inviteErr != nil {
		// Allows us to use the same remove logic to remove the invite
		return nil, inviteErr
	}

	err = i.sendAcceptedOrRejectedMessage(ctx, invite, false)
	if err != nil {
		return nil, fmt.Errorf("failed to send rejected message: %w", err)
	}

	return invite, nil
}

func (i *InviteManager) rejectRequest(ctx context.Context, request *model.MapInvite) (*model.MapInvite, error) {
	if request == nil {
		return nil, ErrNoInvitesOrRequests
	}

	targetId := request.RecipientId
	targetSession, err := i.playerTracker.GetSession(ctx, targetId)
	if err != nil {
		return nil, fmt.Errorf("failed to get target's session: %w", err)
	}

	var inviteErr error
	if targetSession == nil {
		inviteErr = ErrRequestTargetOffline
	} else {
		if *targetSession.PType != string(player.PresenceTypeMapMakerMap) || *targetSession.PMapID != request.MapId {
			inviteErr = ErrRequestTargetLeftMap
		}
	}

	err = i.remove(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("failed to remove request: %w", err)
	}

	if inviteErr != nil {
		// Allows us to use the same remove logic to remove the request
		return nil, inviteErr
	}

	err = i.sendAcceptedOrRejectedMessage(ctx, request, false)
	if err != nil {
		return nil, fmt.Errorf("failed to send accepted message: %w", err)
	}

	return request, nil
}
