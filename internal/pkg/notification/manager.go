package notification

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hollow-cube/api-server/internal/pkg/natsutil"
	"github.com/hollow-cube/api-server/internal/playerdb"
	"go.uber.org/fx"
)

type Matcher struct {
	PlayerID *string
	Key      *string
}

type Manager interface {
	List(ctx context.Context, playerId string, unreadOnly bool, offset, limit int32) ([]playerdb.PlayerNotification, int, error)
	Create(ctx context.Context, targetId string, input CreateInput) error
	SetReadState(ctx context.Context, id string, read bool) error
	Delete(ctx context.Context, id string) error

	// DeleteKeyed deletes (claws back) notifications by (optional) playerId and key
	DeleteMatching(ctx context.Context, matcher Matcher) error
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

func (m *managerImpl) List(ctx context.Context, playerId string, unreadOnly bool, offset, limit int32) ([]playerdb.PlayerNotification, int, error) {
	notifications, err := m.store.GetNotifications(ctx, playerdb.GetNotificationsParams{
		PlayerID: playerId,
		Column2:  unreadOnly, // ew, fix name
		Offset:   int32(offset),
		Limit:    int32(limit),
	})
	if err != nil {
		return nil, 0, err
	}

	var totalCount int
	results := make([]playerdb.PlayerNotification, len(notifications))
	for i, n := range notifications {
		results[i] = notifications[i].PlayerNotification
		totalCount = int(n.TotalCount)
	}

	return results, totalCount, nil
}

func (m *managerImpl) Create(ctx context.Context, targetId string, input CreateInput) error {
	var replace = input.ReplaceUnread
	var expiresAt *time.Time = nil
	if input.ExpiresIn != nil {
		expiresAt = new(time.Now().Add(time.Duration(*input.ExpiresIn) * time.Second))
	}

	if err := m.store.AddNotification(ctx, targetId, input.Type, input.Key, &input.Data, expiresAt, replace); err != nil {
		return err
	}

	if err := m.sendNotificationMessage(ctx, targetId, &input); err != nil {
		return err
	}

	return nil
}

func (m *managerImpl) SetReadState(ctx context.Context, id string, read bool) (err error) {
	var rows int64
	if read {
		rows, err = m.store.MarkNotificationRead(ctx, id)
	} else {
		rows, err = m.store.MarkNotificationUnread(ctx, id)
	}
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return err
}

func (m *managerImpl) Delete(ctx context.Context, id string) error {
	rows, err := m.store.DeleteNotification(ctx, id)
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (m *managerImpl) DeleteMatching(ctx context.Context, matcher Matcher) error {
	deleted, err := m.store.DeleteMatching(ctx, matcher.PlayerID, matcher.Key)
	if err != nil {
		return fmt.Errorf("failed to delete matching notifications: %w", err)
	}

	_ = deleted
	// TODO: send deleted updates

	return nil
}

func (m *managerImpl) sendNotificationMessage(ctx context.Context, playerId string, input *CreateInput) error {
	msg := UpdateMessage{
		Action:   CreateAction,
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
