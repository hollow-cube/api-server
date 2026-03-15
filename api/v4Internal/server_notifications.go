package v4Internal

import (
	"context"
	"errors"
	"time"

	"github.com/hollow-cube/api-server/internal/pkg/notification"
	"github.com/hollow-cube/api-server/pkg/ox"
	"go.uber.org/zap"
)

type NotificationRequest struct {
	NotificationID string `path:"notificationId"`
}

type Notification struct {
	ID        string         `json:"id"`
	Key       string         `json:"key"`
	Type      string         `json:"type"`
	CreatedAt time.Time      `json:"createdAt"`
	ExpiresAt *time.Time     `json:"expiresAt"`
	ReadAt    *time.Time     `json:"readAt"`
	Data      map[string]any `json:"data"`
}

type (
	GetNotificationsRequest struct {
		PlayerID   string `query:"playerId"`
		Page       int    `query:"page"`
		PageSize   int    `query:"pageSize"`
		UnreadOnly bool   `query:"unreadOnly"`
	}
	PaginatedNotificationList struct {
		Count   int            `json:"count"`
		Results []Notification `json:"results"`
	}
)

// GET /notifications
func (s *Server) GetNotifications(ctx context.Context, request GetNotificationsRequest) (*PaginatedNotificationList, error) {
	offset, limit := defaultPageParams(request.Page, request.PageSize)

	zap.S().Infow("Getting notifications", "playerId", request.PlayerID, "unreadOnly", request.UnreadOnly, "offset", offset, "limit", limit)
	notifs, count, err := s.notifications.List(ctx, request.PlayerID, request.UnreadOnly, offset, limit)
	if err != nil {
		return nil, err
	}

	results := make([]Notification, len(notifs))
	for i, n := range notifs {
		var data map[string]any
		if n.Data != nil {
			data = *n.Data // TODO: shouldnt be nullable in DB, empty object is fine for notifications which dont use it.
		} else {
			data = make(map[string]any)
		}
		results[i] = Notification{
			ID:        n.ID,
			Key:       n.Key,
			Type:      n.Type,
			CreatedAt: n.CreatedAt,
			ExpiresAt: n.ExpiresAt,
			ReadAt:    n.ReadAt,
			Data:      data,
		}
	}

	return &PaginatedNotificationList{
		Count:   count,
		Results: results,
	}, nil
}

type UpdateNotificationRequest struct {
	Read *bool `json:"read"`
}

// PATCH /notifications/{notificationId}
func (s *Server) UpdateNotification(ctx context.Context, request NotificationRequest, body UpdateNotificationRequest) error {
	if body.Read == nil {
		return nil // Nothing to do
	}

	err := s.notifications.SetReadState(ctx, request.NotificationID, *body.Read)
	if errors.Is(err, notification.ErrNotFound) {
		return ox.NotFound{}
	} else if err != nil {
		return err
	}

	return nil
}

// DELETE /notifications/{notificationId}
func (s *Server) DeleteNotification(ctx context.Context, request NotificationRequest) error {
	err := s.notifications.Delete(ctx, request.NotificationID)
	if errors.Is(err, notification.ErrNotFound) {
		return ox.NotFound{}
	} else if err != nil {
		return err
	}

	return nil
}
