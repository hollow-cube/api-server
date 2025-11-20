package testutil

import (
	"context"
	"fmt"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ory/dockertest/v3"
)

func RunPostgres(pool *dockertest.Pool) (*Postgres, func()) {
	resource, err := pool.Run("postgres", "16", []string{
		"POSTGRES_PASSWORD=postgres",
	})
	if err != nil {
		log.Fatalf("Could not start resource: %s", err)
	}

	var client *pgxpool.Pool

	// exponential backoff-retry, because the application in the container might not be ready to accept connections yet
	if err := pool.Retry(func() error {
		var err error

		client, err = pgxpool.New(context.Background(), fmt.Sprintf(
			"user=postgres password=postgres host=localhost port=%s",
			resource.GetPort("5432/tcp"),
		))
		if err != nil {
			return err
		}
		return client.Ping(context.Background())
	}); err != nil {
		log.Fatalf("Could not connect to database: %s", err)
	}

	return &Postgres{Client: client}, func() {
		if err := pool.Purge(resource); err != nil {
			log.Printf("Could not purge resource: %s", err)
		}
	}
}

type Postgres struct {
	Client *pgxpool.Pool
}

func (p *Postgres) Cleanup() {
	dropAllTablesSQL := `
		DO
		$$
		DECLARE
			r RECORD;
		BEGIN
			FOR r IN (SELECT tablename FROM pg_tables WHERE schemaname = 'public') LOOP
				EXECUTE 'DROP TABLE IF EXISTS public.' || quote_ident(r.tablename) || ' CASCADE';
			END LOOP;
		END
		$$;
	`

	// Execute the SQL
	_, err := p.Client.Exec(context.Background(), dropAllTablesSQL)
	if err != nil {
		log.Fatalf("Failed to drop all tables: %v", err)
	}
}
