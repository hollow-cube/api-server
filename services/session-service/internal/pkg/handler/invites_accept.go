package handler

import (
	"context"
	"fmt"

	"github.com/hollow-cube/hc-services/services/session-service/internal/pkg/model"
	"github.com/hollow-cube/hc-services/services/session-service/internal/pkg/player"
)

func (i *InviteManager) Accept(ctx context.Context, senderId string, targetId string) (*model.MapInvite, error) {
	if targetId == "" {
		return i.acceptNoRecipient(ctx, senderId)
	} else {
		return i.acceptWithRecipient(ctx, senderId, targetId)
	}
}

func (i *InviteManager) acceptNoRecipient(ctx context.Context, senderId string) (*model.MapInvite, error) {
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

	// This is sort of a hacky fix. Default key really needs to be split into an incoming default key and an
	// outgoing default key. Instead we just don't allow accepting an invite which was not directed at you.
	if invite.RecipientId != senderId {
		err = i.removeDefaultKey(ctx, senderId)
		if err != nil {
			return nil, fmt.Errorf("failed to remove default invite key: %w", err)
		}
		return nil, ErrInviteNotFound
	}

	switch invite.Type {
	case model.InviteTypeInvite:
		return i.acceptInvite(ctx, invite)
	case model.InviteTypeRequest:
		return i.acceptRequest(ctx, invite)
	}
	return nil, fmt.Errorf("invalid invite type: %s", invite.Type)
}

func (i *InviteManager) acceptWithRecipient(ctx context.Context, senderId string, targetId string) (*model.MapInvite, error) {
	// We flip the sender and recipient, as the sender is the accept sender and the target is the accept target,
	// where we want invites where the sender of the accept is the invite recipient and the target of the accept is
	// the invite sender
	invite, err := i.GetInvite(ctx, targetId, senderId)
	if err != nil {
		return nil, fmt.Errorf("failed to get invite: %w", err)
	}

	request, err := i.GetRequest(ctx, targetId, senderId)
	if err != nil {
		return nil, fmt.Errorf("failed to get request: %w", err)
	}

	// This is sort of a hacky fix. Default key really needs to be split into an incoming default key and an
	// outgoing default key. Instead we just don't allow accepting an invite which was not directed at you.
	if invite != nil && invite.RecipientId != senderId {
		invite = nil
	}
	if request != nil && request.RecipientId != senderId {
		request = nil
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
			return i.acceptInvite(ctx, invite)
		} else {
			// Request came first - remove invite and accept request
			err = i.remove(ctx, invite)
			if err != nil {
				return nil, fmt.Errorf("failed to remove invite: %w", err)
			}
			return i.acceptRequest(ctx, request)
		}
	} else if invite != nil {
		return i.acceptInvite(ctx, invite)
	} else if request != nil {
		return i.acceptRequest(ctx, request)
	} else {
		return nil, ErrNoInvitesOrRequests
	}
}

func (i *InviteManager) acceptInvite(ctx context.Context, invite *model.MapInvite) (*model.MapInvite, error) {
	if invite == nil {
		return nil, ErrInviteNotFound
	}

	senderId := invite.SenderId
	senderSession, err := i.playerTracker.GetSession(ctx, senderId)
	if err != nil {
		return nil, fmt.Errorf("failed to get target's session: %w", err)
	}

	var inviteErr error
	if senderSession == nil {
		inviteErr = ErrInviteSenderOffline
	}

	if senderSession != nil && senderSession.PType != nil && (*senderSession.PType != string(player.PresenceTypeMapMakerMap) || *senderSession.PMapID != invite.MapId) {
		inviteErr = ErrInviteSenderLeftMap
	}

	err = i.remove(ctx, invite)
	if err != nil {
		return nil, fmt.Errorf("failed to remove invite: %w", err)
	}

	if inviteErr != nil {
		// Allows us to use the same remove logic to remove the invite
		return nil, inviteErr
	}

	err = i.sendAcceptedOrRejectedMessage(ctx, invite, true)
	if err != nil {
		return nil, fmt.Errorf("failed to send accepted message: %w", err)
	}

	return invite, nil
}

func (i *InviteManager) acceptRequest(ctx context.Context, request *model.MapInvite) (*model.MapInvite, error) {
	if request == nil {
		return nil, ErrRequestNotFound
	}

	targetId := request.RecipientId
	targetSession, err := i.playerTracker.GetSession(ctx, targetId)
	if err != nil {
		return nil, fmt.Errorf("failed to get target's session: %w", err)
	}

	var inviteErr error
	if targetSession == nil {
		inviteErr = ErrRequestTargetOffline
	}

	if targetSession != nil && targetSession.PType != nil && (*targetSession.PType != string(player.PresenceTypeMapMakerMap) || *targetSession.PMapID != request.MapId) {
		inviteErr = ErrRequestTargetLeftMap
	}

	err = i.remove(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("failed to remove request: %w", err)
	}

	if inviteErr != nil {
		// Allows us to use the same remove logic to remove the request
		return nil, inviteErr
	}

	err = i.sendAcceptedOrRejectedMessage(ctx, request, true)
	if err != nil {
		return nil, fmt.Errorf("failed to send accepted message: %w", err)
	}

	return request, nil
}
