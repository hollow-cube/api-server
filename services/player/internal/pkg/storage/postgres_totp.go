package storage

import (
	"context"
	"errors"

	sq "github.com/Masterminds/squirrel"
	"github.com/hollow-cube/hc-services/services/player/internal/pkg/totp"
)

var (
	totpColumns = []string{"player_id", "active", "key", "recovery_codes"}
	totpScan    = func(s *totp.Config) []any {
		return []any{&s.PlayerID, &s.Active, &s.Key, &s.RecoveryCodes}
	}
)

func (c *PostgresClient) GetTOTP(ctx context.Context, playerId string) (*totp.Config, error) {
	query := psql.Select(totpColumns...).
		From("player_totp").
		Where(sq.Eq{"player_id": playerId})
	var result totp.Config
	_, err := c.do(ctx, query, totpScan(&result)...)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *PostgresClient) AddTOTP(ctx context.Context, config *totp.Config) (bool, error) {
	var active bool
	err := c.RunTransaction(ctx, func(ctx context.Context) error {
		// Lock the row for update & to check if already active
		activeQuery := psql.Select("active").From("player_totp").
			Where(sq.Eq{"player_id": config.PlayerID}).Suffix("FOR UPDATE")
		_, err := c.do(ctx, activeQuery, &active)
		if !errors.Is(err, ErrNotFound) && err != nil {
			return err
		} else if active {
			return nil // All good at this point
		}

		// There is guaranteed not an active row through the duration of this transaction
		insertQuery := psql.Insert("player_totp").
			Columns(totpColumns...).
			Values(config.PlayerID, config.Active, config.Key, config.RecoveryCodes).
			Suffix("ON CONFLICT (player_id) DO UPDATE SET key = EXCLUDED.key, recovery_codes = EXCLUDED.recovery_codes, created_at = now() WHERE player_totp.active = false")
		_, err = c.do(ctx, insertQuery)
		return err
	})
	if err != nil {
		return false, err
	}

	return !active, nil
}

func (c *PostgresClient) ActivateTOTP(ctx context.Context, playerId string, key []byte) error {
	query := psql.Update("player_totp").
		Where(sq.Eq{"player_id": playerId, "key": key, "active": false}).
		Set("active", true).
		Suffix("RETURNING 1") // For detection
	var one int
	_, err := c.do(ctx, query, &one)

	// This error will be ErrNotFound if no rows are updated (which is correct), otherwise we got the RETURNING 1
	// back which we can ignore but we succeeded updating the row to active. Yay!
	return err
}

func (c *PostgresClient) DeleteTOTP(ctx context.Context, playerId string) (err error) {
	query := psql.Delete("player_totp").Where(sq.Eq{"player_id": playerId})
	if _, err = c.do(ctx, query); errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}
