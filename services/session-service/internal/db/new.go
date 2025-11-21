//go:generate go tool github.com/sqlc-dev/sqlc/cmd/sqlc generate

package db

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	migratepgx "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	dbTracing "github.com/hollow-cube/hc-services/services/session-service/internal/db/tracing"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

func NewQuerySet(ctx context.Context, databaseUri string) (*Queries, *pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(databaseUri)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse postgres config: %w", err)
	}
	config.ConnConfig.Tracer = &dbTracing.Tracer{}

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect to postgres: %w", err)
	}
	if err = pool.Ping(ctx); err != nil {
		return nil, nil, fmt.Errorf("failed to ping postgres: %w", err)
	}

	// Create a dedicated connection for migrations because migrate wont take a pgx conn (needs database/sql conn)
	migrateConn, err := sql.Open("pgx", databaseUri)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to acquire connection for migrations: %w", err)
	}
	defer migrateConn.Close()

	// Create migrator using above db conn and the embed fs of the migrate directory.
	migrateDriver, err := migratepgx.WithInstance(migrateConn, &migratepgx.Config{
		MigrationsTable: "session-service_migrations",
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create migrate driver: %w", err)
	}
	migrateSource, err := iofs.New(migrationFS, "migrations")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create migrate source: %w", err)
	}
	m, err := migrate.NewWithInstance("migration-fs", migrateSource, "migration-db", migrateDriver)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create migrate instance: %w", err)
	}

	// Apply all migrations up to the latest
	err = m.Up()
	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return nil, nil, fmt.Errorf("failed to apply migrations: %w", err)
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
