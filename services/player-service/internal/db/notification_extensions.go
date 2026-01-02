package db

import (
	"context"
	"time"
)

func (s *Store) AddNotification(
	ctx context.Context,
	playerID string,
	notificationType string,
	notificationKey string,
	data *map[string]interface{},
	expiresAt *time.Time,
	replaceUnread bool,
) error {
	return TxNoReturn(ctx, s, func(ctx context.Context, tx *Store) error {
		if replaceUnread {
			if err := tx.Queries.Unsafe_DeleteNotification(ctx, notificationType, notificationKey, playerID); err != nil {
				return err
			}
		}

		params := Unsafe_AddNotificationParams{
			Type:      notificationType,
			Key:       notificationKey,
			PlayerID:  playerID,
			Data:      data,
			ExpiresAt: expiresAt,
		}
		if err := tx.Queries.Unsafe_AddNotification(ctx, params); err != nil {
			return err
		}

		return nil
	})
}
