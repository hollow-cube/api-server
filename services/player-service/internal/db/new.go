//go:generate go tool github.com/sqlc-dev/sqlc/cmd/sqlc generate

package db

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"os"

	"github.com/golang-migrate/migrate/v4"
	migratepgx "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/hollow-cube/hc-services/libraries/common/pkg/metric"
	postgresUtil "github.com/hollow-cube/hc-services/libraries/common/pkg/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

var ErrNoRows = pgx.ErrNoRows

//go:embed migrations/*.sql
var migrationFS embed.FS

// Store is a wrapper around the generated Queries type that includes metrics writing.
// Some query methods are private with public overrides for this (e.g., addPlayerExperience and AddPlayerExperience)
type Store struct {
	*Queries

	metrics metric.Writer
}

func (s *Store) WithTx(tx pgx.Tx) *Store {
	return &Store{
		Queries: s.Queries.WithTx(tx),
		metrics: s.metrics,
	}
}

func NewQuerySet(ctx context.Context, metrics metric.Writer, databaseUri string) (*Store, *pgxpool.Pool, error) {
	// TODO: move the bulk of this function to common-go

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

	// Create a dedicated connection for migrations because migrate wont take a pgx conn (needs database/sql conn)
	migrateConn, err := sql.Open("pgx", databaseUri)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to acquire connection for migrations: %w", err)
	}
	defer migrateConn.Close()

	// Create migrator using above db conn and the embed fs of the migrate directory.
	migrateDriver, err := migratepgx.WithInstance(migrateConn, &migratepgx.Config{
		MigrationsTable: "player-service_migrations",
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

	targetVersion, err := migrateSource.First()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get latest migration version: %w", err)
	}
	for {
		next, err := migrateSource.Next(targetVersion)
		if errors.Is(err, os.ErrNotExist) {
			break
		}
		if err != nil {
			return nil, nil, err
		}
		targetVersion = next
	}

	version, dirty, err := m.Version()
	if err != nil && !errors.Is(err, migrate.ErrNilVersion) {
		return nil, nil, fmt.Errorf("failed to get current migration version: %w", err)
	}
	if dirty || version < targetVersion {
		// Apply all migrations up our current version
		err = m.Up()
		if err != nil && !errors.Is(err, migrate.ErrNoChange) {
			return nil, nil, fmt.Errorf("failed to apply migrations: %w", err)
		}
	}
	if version > targetVersion {
		zap.S().Infow("database has later version than I know about!", "target_version", targetVersion, "current_version", version)
	}

	return &Store{Queries: New(pool), metrics: metrics}, pool, nil
}

func Tx[T any](ctx context.Context, s *Store, fn func(ctx context.Context, txStore *Store) (T, error)) (T, error) {
	if _, ok := s.db.(pgx.Tx); ok {
		return fn(ctx, s) // Already in a transaction, just proxy
	}

	pool, ok := s.db.(*pgxpool.Pool)
	if !ok {
		var z T
		return z, fmt.Errorf("failed to acquire connection to postgres")
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		var z T
		return z, fmt.Errorf("failed to start transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	ret, err := fn(ctx, s.WithTx(tx))
	if err != nil {
		var z T
		return z, fmt.Errorf("failed to apply transaction: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		var z T
		return z, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return ret, nil
}

func TxNoReturn(ctx context.Context, s *Store, fn func(ctx context.Context, txStore *Store) error) error {
	if _, ok := s.db.(pgx.Tx); ok {
		return fn(ctx, s) // Already in a transaction, just proxy
	}

	pool, ok := s.db.(*pgxpool.Pool)
	if !ok {
		return fmt.Errorf("failed to acquire connection to postgres")
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("failed to start transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := fn(ctx, s.WithTx(tx)); err != nil {
		return fmt.Errorf("failed to apply transaction: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}
