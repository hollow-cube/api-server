//go:generate go tool github.com/sqlc-dev/sqlc/cmd/sqlc generate

package db

import (
	"context"
	"fmt"

	postgresUtil "github.com/hollow-cube/api-server/internal/pkg/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNoRows = pgx.ErrNoRows

func NewQuerySet(ctx context.Context, databaseUri string) (*Queries, *pgxpool.Pool, error) {
	// Config options
	config, err := pgxpool.ParseConfig(databaseUri)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse postgres config: %w", err)
	}

	config.ConnConfig.Tracer = postgresUtil.NewSqlCTracer()

	// Create pgx conn pool
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect to postgres: %w", err)
	}
	if err = pool.Ping(ctx); err != nil {
		return nil, nil, fmt.Errorf("failed to ping postgres: %w", err)
	}

	return New(pool), pool, nil
}

func Tx[T any](ctx context.Context, q *Queries, fn func(ctx context.Context, queries *Queries) (*T, error)) (*T, error) {
	pool, ok := q.db.(*pgxpool.Pool)
	if !ok {
		return nil, fmt.Errorf("failed to acquire connection to postgres")
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to start transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	ret, err := fn(ctx, q.WithTx(tx))
	if err != nil {
		return nil, fmt.Errorf("failed to apply transaction: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return ret, nil
}
