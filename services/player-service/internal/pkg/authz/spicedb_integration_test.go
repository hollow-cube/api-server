//go:build integration

package authz

import (
	"context"
	_ "embed"
	"testing"
	"time"

	v1 "github.com/authzed/authzed-go/proto/authzed/api/v1"
	"github.com/hollow-cube/hc-services/services/player-service/internal/pkg/testutil"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/structpb"
)

var sdb *testutil.SpiceDB

func TestMain(m *testing.M) {
	pool, run, cleanup := testutil.Init(m)
	defer cleanup()

	sdb, cleanup = testutil.RunSpiceDB(pool)
	defer cleanup()

	run()
}

func prepareTest(t *testing.T) Client {
	t.Helper()
	t.Cleanup(sdb.Cleanup)
	return &SpiceDBClient{sdb.Client}
}

func TestReadHypercubeStats(t *testing.T) {

	t.Run("empty", func(t *testing.T) {
		c := prepareTest(t)

		_, _, err := c.GetHypercubeStats(context.TODO(), "player1", "")
		require.ErrorIs(t, err, ErrNotFound)
	})

	t.Run("found", func(t *testing.T) {
		c := prepareTest(t)

		now := time.Now()

		sdb.WriteRelationship(t,
			"mapmaker/platform:0#hypercube@mapmaker/player:player1",
			newCaveat(t, "is_not_expired", map[string]interface{}{
				"start_time": now.Format(time.RFC3339),
				"term":       "1h",
			}),
		)

		start, term, err := c.GetHypercubeStats(context.TODO(), "player1", "")
		require.NoError(t, err)
		require.WithinDuration(t, now, start, time.Minute)
		require.Equal(t, time.Hour, term)
	})

}

func TestAppendHypercube(t *testing.T) {
	t.Run("upsert", func(t *testing.T) {
		c := prepareTest(t)

		err := c.AppendHypercube(context.TODO(), "player1", time.Hour, "")
		require.NoError(t, err)

		result := sdb.ReadRelationship(t, "mapmaker/platform:0#hypercube@mapmaker/player:player1")
		require.NotNil(t, result)
		require.Equal(t, "1h0m0s", result.OptionalCaveat.Context.Fields["term"].GetStringValue())
	})

	t.Run("update existing", func(t *testing.T) {
		c := prepareTest(t)

		sdb.WriteRelationship(t,
			"mapmaker/platform:0#hypercube@mapmaker/player:player1",
			newCaveat(t, "is_not_expired", map[string]interface{}{
				"start_time": time.Now().Format(time.RFC3339),
				"term":       "1h",
			}),
		)

		err := c.AppendHypercube(context.TODO(), "player1", 4*time.Hour, "")
		require.NoError(t, err)

		result := sdb.ReadRelationship(t, "mapmaker/platform:0#hypercube@mapmaker/player:player1")
		require.NotNil(t, result)
		require.Equal(t, "5h0m0s", result.OptionalCaveat.Context.Fields["term"].GetStringValue())
	})

	t.Run("remove expired (negative term)", func(t *testing.T) {
		c := prepareTest(t)

		sdb.WriteRelationship(t,
			"mapmaker/platform:0#hypercube@mapmaker/player:player1",
			newCaveat(t, "is_not_expired", map[string]interface{}{
				"start_time": time.Now().Format(time.RFC3339),
				"term":       "1h",
			}),
		)

		err := c.AppendHypercube(context.TODO(), "player1", -4*time.Hour, "")
		require.NoError(t, err)

		result := sdb.ReadRelationship(t, "mapmaker/platform:0#hypercube@mapmaker/player:player1")
		require.Nil(t, result)
	})

	t.Run("remove expired (expired)", func(t *testing.T) {
		c := prepareTest(t)

		sdb.WriteRelationship(t,
			"mapmaker/platform:0#hypercube@mapmaker/player:player1",
			newCaveat(t, "is_not_expired", map[string]interface{}{
				"start_time": time.Now().Add(-24 * time.Hour).Format(time.RFC3339),
				"term":       "1h",
			}),
		)

		err := c.AppendHypercube(context.TODO(), "player1", 4*time.Hour, "")
		require.NoError(t, err)

		result := sdb.ReadRelationship(t, "mapmaker/platform:0#hypercube@mapmaker/player:player1")
		require.Nil(t, result)
	})
}

func newCaveat(t *testing.T, name string, entries map[string]interface{}) *v1.ContextualizedCaveat {
	s, err := structpb.NewStruct(entries)
	require.NoError(t, err)
	return &v1.ContextualizedCaveat{CaveatName: name, Context: s}
}
