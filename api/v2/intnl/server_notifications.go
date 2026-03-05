package intnl

import (
	"context"
	"errors"
	"fmt"

	"github.com/hollow-cube/api-server/internal/pkg/model"
	"github.com/hollow-cube/api-server/internal/pkg/notification"
)

func (s *Server) DeletePlayerNotification(ctx context.Context, request DeletePlayerNotificationRequestObject) (DeletePlayerNotificationResponseObject, error) {
	if err := s.notificationManager.DeleteNotification(ctx, request.NotificationId, request.PlayerId); err != nil {
		if errors.Is(err, notification.ErrNotFound) {
			return DeletePlayerNotification404Response{}, nil
		}
		return nil, err
	}
	return DeletePlayerNotification200Response{}, nil
}

func (s *Server) GetPlayerNotifications(ctx context.Context, request GetPlayerNotificationsRequestObject) (GetPlayerNotificationsResponseObject, error) {
	var unreadOnly = request.Params.Unread != nil && *request.Params.Unread

	var page = 0
	if request.Params.Page != nil {
		page = *request.Params.Page
	}
	if page < 0 {
		return GetPlayerNotifications400JSONResponse{Message: "page must be non-negative"}, nil
	}

	result, err := s.notificationManager.GetNotifications(ctx, request.PlayerId, page, unreadOnly)
	if err != nil {
		return nil, err
	}

	results := make([]PlayerNotification, len(result.Results))
	for i, notif := range result.Results {
		results[i] = notificationToApi(notif)
	}

	return GetPlayerNotifications200JSONResponse{
		Page:      result.Page,
		PageCount: result.PageCount,
		Results:   results,
	}, nil
}

func notificationToApi(n notification.Notification) PlayerNotification {
	return PlayerNotification{
		Id:        n.Id,
		Type:      n.Type,
		Key:       n.Key,
		Data:      n.Data,
		CreatedAt: n.CreatedAt,
		ReadAt:    n.ReadAt,
		ExpiresAt: n.ExpiresAt,
	}
}

func (s *Server) UpdatePlayerNotification(ctx context.Context, request UpdatePlayerNotificationRequestObject) (UpdatePlayerNotificationResponseObject, error) {
	if err := s.notificationManager.UpdateNotification(ctx, request.NotificationId, request.PlayerId, request.Body.Read); err != nil {
		if errors.Is(err, notification.ErrNotFound) {
			return UpdatePlayerNotification404Response{}, nil
		}
		return nil, err
	}
	return UpdatePlayerNotification200Response{}, nil
}

func (s *Server) CreatePlayerNotification(ctx context.Context, request CreatePlayerNotificationRequestObject) (CreatePlayerNotificationResponseObject, error) {
	replaceUnread := false
	if request.Params.ReplaceUnread != nil {
		replaceUnread = *request.Params.ReplaceUnread
	}

	input := notification.CreateInput{
		Key:           request.Body.Key,
		Type:          request.Body.Type,
		ExpiresIn:     request.Body.ExpiresIn,
		Data:          request.Body.Data,
		ReplaceUnread: replaceUnread,
	}
	if err := s.notificationManager.CreateNotification(ctx, request.PlayerId, input); err != nil {
		return nil, err
	}

	return CreatePlayerNotification201Response{}, nil
}

func (s *Server) sendNotificationMessage(ctx context.Context, request CreatePlayerNotificationRequestObject) error {
	msg := model.NotificationUpdateMessage{
		Action:   model.NotificationCreateAction,
		PlayerId: request.PlayerId,
		Type:     request.Body.Type,
		Key:      request.Body.Key,
		Data:     request.Body.Data,
	}
	if err := s.jetStream.PublishJSONAsync(ctx, msg); err != nil {
		return fmt.Errorf("failed to publish notification message: %w", err)
	}

	return nil
}
