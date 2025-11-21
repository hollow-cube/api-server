package intnl

import (
	"context"
	"errors"
	"time"

	"github.com/hollow-cube/hc-services/services/session-service/internal/pkg/handler"
	"github.com/hollow-cube/hc-services/services/session-service/internal/pkg/model"
)

func (s *serverImpl) InviteToMap(ctx context.Context, request InviteToMapRequestObject) (InviteToMapResponseObject, error) {
	err := s.invites.Create(ctx, inviteFromApi(*request.Body))
	var inviteError *handler.InviteError
	if errors.As(err, &inviteError) {
		return InviteToMap409JSONResponse{
			Code:    inviteError.Code,
			Message: inviteError.Message,
		}, nil
	} else if err != nil {
		return nil, err
	}

	return InviteToMap200Response{}, nil
}

func (s *serverImpl) RequestToJoinMap(ctx context.Context, request RequestToJoinMapRequestObject) (RequestToJoinMapResponseObject, error) {
	err := s.invites.Create(ctx, inviteFromApi(*request.Body))
	var inviteError *handler.InviteError
	if errors.As(err, &inviteError) {
		return RequestToJoinMap409JSONResponse{
			Code:    inviteError.Code,
			Message: inviteError.Message,
		}, nil
	} else if err != nil {
		return nil, err
	}

	return RequestToJoinMap200Response{}, nil
}

func (s *serverImpl) AcceptMapInviteOrRequest(ctx context.Context, request AcceptMapInviteOrRequestRequestObject) (AcceptMapInviteOrRequestResponseObject, error) {
	invite, err := s.invites.Accept(ctx, request.Body.SenderId, request.Body.TargetId)
	var inviteError *handler.InviteError2
	if errors.As(err, &inviteError) {
		return AcceptMapInviteOrRequest400JSONResponse{
			ErrorCode: float32(inviteError.Code),
			ErrorText: inviteError.Message,
		}, nil
	} else if err != nil {
		return nil, err
	}

	return AcceptMapInviteOrRequest200JSONResponse(inviteToApi(invite)), nil
}

func (s *serverImpl) RejectMapInviteOrRequest(ctx context.Context, request RejectMapInviteOrRequestRequestObject) (RejectMapInviteOrRequestResponseObject, error) {
	invite, err := s.invites.Reject(ctx, request.Body.SenderId, request.Body.TargetId)
	var inviteError *handler.InviteError2
	if errors.As(err, &inviteError) {
		return RejectMapInviteOrRequest400JSONResponse{
			ErrorCode: float32(inviteError.Code),
			ErrorText: inviteError.Message,
		}, nil
	} else if err != nil {
		return nil, err
	}

	return RejectMapInviteOrRequest200JSONResponse(inviteToApi(invite)), nil
}

func inviteFromApi(invite MapInvite) *model.MapInvite {
	return &model.MapInvite{
		Type:        model.InviteType(invite.InviteType),
		SenderId:    invite.SenderId,
		RecipientId: invite.RecipientId,
		MapId:       invite.MapId,
		CreatedAt:   time.UnixMilli(int64(invite.Time)),
	}
}

func inviteToApi(invite *model.MapInvite) MapInvite {
	return MapInvite{
		InviteType:  MapInviteInviteType(invite.Type),
		SenderId:    invite.SenderId,
		RecipientId: invite.RecipientId,
		MapId:       invite.MapId,
		Time:        int(invite.CreatedAt.UnixMilli()),
	}
}
