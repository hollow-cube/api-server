package intnl

import (
	"context"
	"encoding/json"
	"time"

	"github.com/hollow-cube/hc-services/services/player-service/internal/db"
	"github.com/hollow-cube/hc-services/services/player-service/internal/pkg/model"
	"github.com/segmentio/kafka-go"
)

const notificationsPerPage = 21

func (s *server) DeletePlayerNotification(ctx context.Context, request DeletePlayerNotificationRequestObject) (DeletePlayerNotificationResponseObject, error) {
	rows, err := s.store.DeleteNotification(ctx, request.PlayerId, request.NotificationId)
	if rows == 0 {
		return DeletePlayerNotification404Response{}, nil
	} else if err != nil {
		return nil, err
	}
	return DeletePlayerNotification200Response{}, nil
}

func (s *server) GetPlayerNotifications(ctx context.Context, request GetPlayerNotificationsRequestObject) (GetPlayerNotificationsResponseObject, error) {
	var unreadOnly = request.Params.Unread != nil && *request.Params.Unread

	var page int32 = 0
	if request.Params.Page != nil {
		page = int32(*request.Params.Page)
	}
	if page < 0 {
		return GetPlayerNotifications400JSONResponse{Message: "page must be non-negative"}, nil
	}

	var pageCount int32 = 0
	if page == 0 {
		count, err := s.store.GetNotificationCount(ctx, request.PlayerId, unreadOnly)
		if err != nil {
			return nil, err
		}
		pageCount = int32(count / notificationsPerPage)
	}

	params := db.GetNotificationsParams{PlayerID: request.PlayerId, Limit: notificationsPerPage, Offset: page * notificationsPerPage, Column4: unreadOnly}
	notifications, err := s.store.GetNotifications(ctx, params)
	if err != nil {
		return nil, err
	}

	results := make([]PlayerNotification, len(notifications))
	for i, n := range notifications {
		results[i] = PlayerNotification{
			Id:        n.ID,
			Type:      n.Type,
			Key:       n.Key,
			Data:      n.Data,
			CreatedAt: n.CreatedAt,
			ReadAt:    n.ReadAt,
			ExpiresAt: n.ExpiresAt,
		}
	}

	return GetPlayerNotifications200JSONResponse{
		Results:   results,
		Page:      page,
		PageCount: pageCount,
	}, nil
}

func (s *server) UpdatePlayerNotification(ctx context.Context, request UpdatePlayerNotificationRequestObject) (UpdatePlayerNotificationResponseObject, error) {
	var rows int64
	var err error
	if request.Body.Read {
		rows, err = s.store.MarkNotificationRead(ctx, request.PlayerId, request.NotificationId)
	} else {
		rows, err = s.store.MarkNotificationUnread(ctx, request.PlayerId, request.NotificationId)
	}

	if rows == 0 {
		return UpdatePlayerNotification404Response{}, nil
	} else if err != nil {
		return nil, err
	}

	return UpdatePlayerNotification200Response{}, nil
}

func (s *server) CreatePlayerNotification(ctx context.Context, request CreatePlayerNotificationRequestObject) (CreatePlayerNotificationResponseObject, error) {
	if err := s.createPlayerNotification(ctx, request); err != nil {
		return nil, err
	}

	return CreatePlayerNotification201Response{}, nil
}

// createPlayerNotification exists to allow local creation of notifications without an HTTP request to itself.
func (s *server) createPlayerNotification(ctx context.Context, request CreatePlayerNotificationRequestObject) error {
	var replace = request.Params.ReplaceUnread != nil && *request.Params.ReplaceUnread
	var expiresAt *time.Time = nil
	if request.Body.ExpiresIn != nil {
		t := time.Now().Add(time.Duration(*request.Body.ExpiresIn) * time.Second)
		expiresAt = &t
	}

	if err := s.store.AddNotification(
		ctx,
		request.PlayerId,
		request.Body.Type,
		request.Body.Key,
		request.Body.Data,
		expiresAt,
		replace,
	); err != nil {
		return err
	}

	if err := s.sendNotificationMessage(ctx, request); err != nil {
		return err
	}

	return nil
}

func (s *server) sendNotificationMessage(ctx context.Context, request CreatePlayerNotificationRequestObject) error {
	msg := model.NotificationUpdateMessage{
		Action:   model.NotificationCreateAction,
		PlayerId: request.PlayerId,
		Type:     request.Body.Type,
		Key:      request.Body.Key,
		Data:     request.Body.Data,
	}

	raw, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	return s.producer.WriteMessages(ctx, kafka.Message{
		Topic: model.NotificationUpdateTopic,
		Key:   []byte(request.PlayerId),
		Value: raw,
	})
}
