package notification

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hollow-cube/api-server/internal/pkg/model"
	"github.com/hollow-cube/api-server/internal/pkg/natsutil"
	"github.com/hollow-cube/api-server/internal/playerdb"
	"go.uber.org/fx"
)

type Manager interface {
	GetNotifications(ctx context.Context, playerId string, page int, unreadOnly bool) (*PaginatedNotifications, error)
	CreateNotification(ctx context.Context, playerId string, input CreateInput) error
	UpdateNotification(ctx context.Context, id string, playerId string, read bool) error
	DeleteNotification(ctx context.Context, id string, playerId string) error
}

type ManagerParams struct {
	fx.In

	Store     *playerdb.Store
	JetStream *natsutil.JetStreamWrapper
}

func NewManager(params ManagerParams) Manager {
	return &managerImpl{
		store:     params.Store,
		jetStream: params.JetStream,
	}
}

var ErrNotFound = errors.New("not found")

var _ Manager = (*managerImpl)(nil)

type managerImpl struct {
	store     *playerdb.Store
	jetStream *natsutil.JetStreamWrapper
}

const notificationsPerPage = 21

func (m *managerImpl) GetNotifications(ctx context.Context, playerId string, page int, unreadOnly bool) (*PaginatedNotifications, error) {
	var pageCount int32 = 0
	if page == 0 {
		count, err := m.store.GetNotificationCount(ctx, playerId, unreadOnly)
		if err != nil {
			return nil, err
		}
		pageCount = int32(count / notificationsPerPage)
	}

	params := playerdb.GetNotificationsParams{
		PlayerID: playerId,
		Limit:    notificationsPerPage,
		Offset:   int32(page * notificationsPerPage),
		Column4:  unreadOnly,
	}
	notifications, err := m.store.GetNotifications(ctx, params)
	if err != nil {
		return nil, err
	}

	results := make([]Notification, len(notifications))
	for i, n := range notifications {
		results[i] = Notification{
			Id:        n.ID,
			Type:      n.Type,
			Key:       n.Key,
			Data:      n.Data,
			CreatedAt: n.CreatedAt,
			ReadAt:    n.ReadAt,
			ExpiresAt: n.ExpiresAt,
		}
	}

	return &PaginatedNotifications{
		Page:      int32(page),
		PageCount: pageCount,
		Results:   results,
	}, nil
}

func (m *managerImpl) CreateNotification(ctx context.Context, playerId string, input CreateInput) error {
	var replace = input.ReplaceUnread
	var expiresAt *time.Time = nil
	if input.ExpiresIn != nil {
		expiresAt = new(time.Now().Add(time.Duration(*input.ExpiresIn) * time.Second))
	}

	if err := m.store.AddNotification(ctx, playerId, input.Type, input.Key, input.Data, expiresAt, replace); err != nil {
		return err
	}

	if err := m.sendNotificationMessage(ctx, playerId, &input); err != nil {
		return err
	}

	return nil
}

func (m *managerImpl) UpdateNotification(ctx context.Context, id string, playerId string, read bool) error {
	var rows int64
	var err error
	if read {
		rows, err = m.store.MarkNotificationRead(ctx, playerId, id)
	} else {
		rows, err = m.store.MarkNotificationUnread(ctx, playerId, id)
	}
	if err != nil {
		return err
	}

	if rows == 0 {
		return ErrNotFound
	}
	return err
}

func (m *managerImpl) DeleteNotification(ctx context.Context, id string, playerId string) error {
	rows, err := m.store.DeleteNotification(ctx, playerId, id)
	if err != nil {
		return err
	}

	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (m *managerImpl) sendNotificationMessage(ctx context.Context, playerId string, input *CreateInput) error {
	msg := model.NotificationUpdateMessage{
		Action:   model.NotificationCreateAction,
		PlayerId: playerId,
		Type:     input.Type,
		Key:      input.Key,
		Data:     input.Data,
	}
	if err := m.jetStream.PublishJSONAsync(ctx, msg); err != nil {
		return fmt.Errorf("failed to publish notification message: %w", err)
	}

	return nil
}
