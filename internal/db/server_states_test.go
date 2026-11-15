package db_test

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/hollow-cube/api-server/internal/db"
	"github.com/hollow-cube/api-server/internal/pkg/testutil"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/ory/dockertest/v3"
	"github.com/stretchr/testify/require"
)

const (
	mapA = "00000000-0000-0000-0000-00000000000a"
	mapB = "00000000-0000-0000-0000-00000000000b"
)

func newQueries(t *testing.T) (*db.Queries, func(sql string, args ...any)) {
	t.Helper()
	pool, err := dockertest.NewPool("")
	require.NoError(t, err)
	pg, cleanup := testutil.RunPostgres(pool)
	t.Cleanup(cleanup)

	ctx := context.Background()
	exec := func(sql string, args ...any) {
		t.Helper()
		_, err := pg.Client.Exec(ctx, sql, args...)
		require.NoError(t, err)
	}

	// These belong to other services' migrations, ours only reference them.
	exec(`create table maps (id uuid primary key)`)
	exec(`create table player_data (id uuid primary key)`)
	migrations, err := filepath.Glob("migrations/*.up.sql")
	require.NoError(t, err)
	require.NotEmpty(t, migrations)
	sort.Strings(migrations)
	for _, migration := range migrations {
		content, err := os.ReadFile(migration)
		require.NoError(t, err)
		_, err = pg.Client.Exec(ctx, string(content))
		require.NoError(t, err, migration)
	}

	exec(`insert into maps (id) values ($1), ($2)`, mapA, mapB)
	return db.New(pg.Client), exec
}

func TestGetServerStateByEditingWorld(t *testing.T) {
	ctx := context.Background()
	queries, exec := newQueries(t)

	server := func(id, role string, status int, statusV2, clusterIP string) {
		exec(`insert into server_states (id, role, status, status_v2, cluster_ip) values ($1, $2, $3, $4, $5)`,
			id, role, status, statusV2, clusterIP)
	}
	world := func(id, mapID, serverID string, worldType any) {
		exec(`insert into map_worlds (id, map_id, server_id, type) values ($1, $2, $3, $4)`,
			id, mapID, serverID, worldType)
	}
	reset := func() {
		exec(`delete from server_states`)
	}

	t.Run("no world", func(t *testing.T) {
		t.Cleanup(reset)
		server("map-new", "map", 1, "active", "10.0.0.2")

		_, err := queries.GetServerStateByEditingWorld(ctx, mapA)
		require.ErrorIs(t, err, pgx.ErrNoRows)
	})

	t.Run("terminating server keeps its world", func(t *testing.T) {
		t.Cleanup(reset)
		server("map-old", "map", 0, "draining", "10.0.0.1")
		server("map-new", "map", 1, "active", "10.0.0.2")
		world("w1", mapA, "map-old", "editing")

		state, err := queries.GetServerStateByEditingWorld(ctx, mapA)
		require.NoError(t, err)
		require.Equal(t, "map-old", state.ID)
		require.Equal(t, "10.0.0.1", state.ClusterIp)

		// The ready server is still what everything else is offered.
		ready, err := queries.ListServersWithRoleExcept(ctx, "map", "")
		require.NoError(t, err)
		require.Equal(t, []string{"map-new"}, ready)
	})

	t.Run("other map's world is ignored", func(t *testing.T) {
		t.Cleanup(reset)
		server("map-old", "map", 0, "draining", "10.0.0.1")
		world("w1", mapB, "map-old", "editing")

		_, err := queries.GetServerStateByEditingWorld(ctx, mapA)
		require.ErrorIs(t, err, pgx.ErrNoRows)
	})

	t.Run("removed server takes its worlds with it", func(t *testing.T) {
		t.Cleanup(reset)
		server("map-old", "map", 0, "draining", "10.0.0.1")
		world("w1", mapA, "map-old", "editing")
		require.NoError(t, queries.DeleteServerState(ctx, "map-old"))

		_, err := queries.GetServerStateByEditingWorld(ctx, mapA)
		require.ErrorIs(t, err, pgx.ErrNoRows)
	})

	t.Run("untyped and other worlds are ignored", func(t *testing.T) {
		t.Cleanup(reset)
		server("map-old", "map", 0, "draining", "10.0.0.1")
		world("w1", mapA, "map-old", nil)
		world("w2", mapA, "map-old", "todo")
		world("w3", mapA, "map-old", "playing")
		world("w4", mapA, "map-old", "verifying")

		_, err := queries.GetServerStateByEditingWorld(ctx, mapA)
		require.ErrorIs(t, err, pgx.ErrNoRows)
	})

	t.Run("server without an address is ignored", func(t *testing.T) {
		t.Cleanup(reset)
		server("map-old", "map", 0, "draining", "")
		world("w1", mapA, "map-old", "editing")

		_, err := queries.GetServerStateByEditingWorld(ctx, mapA)
		require.ErrorIs(t, err, pgx.ErrNoRows)
	})

	t.Run("editing world is picked among the map's worlds", func(t *testing.T) {
		t.Cleanup(reset)
		server("map-old", "map", 0, "draining", "10.0.0.1")
		server("map-new", "map", 1, "active", "10.0.0.2")
		world("w1", mapA, "map-new", "playing")
		world("w2", mapA, "map-old", "editing")
		world("w3", mapA, "map-new", nil)

		state, err := queries.GetServerStateByEditingWorld(ctx, mapA)
		require.NoError(t, err)
		require.Equal(t, "map-old", state.ID)
	})
}

func TestInsertMapWorld(t *testing.T) {
	ctx := context.Background()
	queries, exec := newQueries(t)
	exec(`insert into server_states (id, role, status, cluster_ip) values ('map-old', 'map', 1, '10.0.0.1'), ('map-new', 'map', 1, '10.0.0.2')`)

	worlds := func(mapID string) map[string]string {
		t.Helper()
		rows, err := queries.GetWorldsForMap(ctx, mapID)
		require.NoError(t, err)
		result := make(map[string]string, len(rows))
		for _, row := range rows {
			worldType := "<null>"
			if row.Type != nil {
				worldType = *row.Type
			}
			result[row.ID] = row.ServerID + "/" + worldType
		}
		return result
	}

	t.Run("an existing row is kept as it is", func(t *testing.T) {
		t.Cleanup(func() { exec(`delete from map_worlds`) })
		require.NoError(t, queries.InsertMapWorld(ctx, db.InsertMapWorldParams{
			ID: "w1", MapID: mapA, ServerID: "map-old", Type: new("editing"),
		}))
		require.NoError(t, queries.InsertMapWorld(ctx, db.InsertMapWorldParams{
			ID: "w1", MapID: mapA, ServerID: "map-new", Type: new("todo"),
		}))
		require.NoError(t, queries.InsertMapWorld(ctx, db.InsertMapWorldParams{
			ID: "w1", MapID: mapA, ServerID: "map-new",
		}))
		require.Equal(t, map[string]string{"w1": "map-old/editing"}, worlds(mapA))
	})

	t.Run("one editing world per map", func(t *testing.T) {
		t.Cleanup(func() { exec(`delete from map_worlds`) })
		require.NoError(t, queries.InsertMapWorld(ctx, db.InsertMapWorldParams{
			ID: "w1", MapID: mapA, ServerID: "map-old", Type: new("editing"),
		}))

		err := queries.InsertMapWorld(ctx, db.InsertMapWorldParams{
			ID: "w2", MapID: mapA, ServerID: "map-new", Type: new("editing"),
		})
		var pgErr *pgconn.PgError
		require.ErrorAs(t, err, &pgErr)
		require.Equal(t, "23505", pgErr.Code)
		require.Equal(t, "map_worlds_edit_map_idx", pgErr.ConstraintName)

		// Other maps and other world types are unconstrained.
		require.NoError(t, queries.InsertMapWorld(ctx, db.InsertMapWorldParams{
			ID: "w3", MapID: mapB, ServerID: "map-new", Type: new("editing"),
		}))
		for _, id := range []string{"w4", "w5"} {
			require.NoError(t, queries.InsertMapWorld(ctx, db.InsertMapWorldParams{
				ID: id, MapID: mapA, ServerID: "map-new", Type: new("playing"),
			}))
		}
		for _, id := range []string{"w6", "w7"} {
			require.NoError(t, queries.InsertMapWorld(ctx, db.InsertMapWorldParams{
				ID: id, MapID: mapA, ServerID: "map-new",
			}))
		}
		require.Len(t, worlds(mapA), 5)
	})
}
