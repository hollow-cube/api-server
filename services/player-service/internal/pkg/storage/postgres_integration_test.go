package storage

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/hollow-cube/hc-services/services/player-service/internal/pkg/model"
	"github.com/hollow-cube/hc-services/services/player-service/internal/pkg/testutil"
	"github.com/stretchr/testify/require"
)

var pg *testutil.Postgres

func TestMain(m *testing.M) {
	pool, run, cleanup := testutil.Init(m)
	defer cleanup()

	pg, cleanup = testutil.RunPostgres(pool)
	defer cleanup()

	run()
}

func prepareTest(t *testing.T) (Client, string) {
	t.Helper()
	t.Cleanup(pg.Cleanup)

	storageClient, err := NewPostgresClientFromClient(pg.Client)
	require.NoError(t, err)

	playerId := uuid.NewString()
	err = storageClient.CreatePlayerData(context.TODO(), &model.PlayerData{
		Id: playerId, Username: "test", IpHistory: make([]string, 0),
	})
	require.NoError(t, err)

	return storageClient, playerId
}

func TestPostgresClient_UpdateBackpack(t *testing.T) {
	t.Run("insert positive", func(t *testing.T) {
		client, playerId := prepareTest(t)

		newBackpack, err := client.UpdateBackpack(context.TODO(), playerId, model.PlayerBackpack{model.Bricks: 3})
		require.NoError(t, err)
		require.Equal(t, model.PlayerBackpack{model.Bricks: 3}, newBackpack)

		bricks := 0
		err = pg.Client.QueryRow(context.TODO(), "select bricks from player_backpack where player_id = $1", playerId).Scan(&bricks)
		require.NoError(t, err)
		require.Equal(t, 3, bricks)
	})

	t.Run("insert negative", func(t *testing.T) {
		client, playerId := prepareTest(t)

		_, err := client.UpdateBackpack(context.TODO(), playerId, model.PlayerBackpack{model.Bricks: -1})
		require.ErrorIs(t, err, ErrBalanceTooLow)
	})

	t.Run("update valid", func(t *testing.T) {
		client, playerId := prepareTest(t)

		_, err := pg.Client.Exec(context.TODO(), "insert into player_backpack (player_id, bricks) values ($1, 1)", playerId)
		require.NoError(t, err)

		newBackpack, err := client.UpdateBackpack(context.TODO(), playerId, model.PlayerBackpack{
			model.Bricks: 2,
		})
		require.NoError(t, err)
		require.Equal(t, model.PlayerBackpack{model.Bricks: 3}, newBackpack)

		bricks := 0
		err = pg.Client.QueryRow(context.TODO(), "select bricks from player_backpack where player_id = $1", playerId).Scan(&bricks)
		require.NoError(t, err)
		require.Equal(t, 3, bricks)
	})

	t.Run("pos/neg issue", func(t *testing.T) {
		client, playerId := prepareTest(t)

		_, err := client.UpdateBackpack(context.TODO(), playerId, model.PlayerBackpack{
			model.Bricks:        2,
			model.NightmareFuel: -1,
		})
		require.Error(t, err)
	})

	t.Run("update subtract", func(t *testing.T) {
		client, playerId := prepareTest(t)

		_, err := pg.Client.Exec(context.TODO(), "insert into player_backpack (player_id, bricks) values ($1, 2)", playerId)
		require.NoError(t, err)

		newBackpack, err := client.UpdateBackpack(context.TODO(), playerId, model.PlayerBackpack{model.Bricks: -1})
		require.NoError(t, err)
		require.Equal(t, model.PlayerBackpack{model.Bricks: 1}, newBackpack)

		bricks := 0
		err = pg.Client.QueryRow(context.TODO(), "select bricks from player_backpack where player_id = $1", playerId).Scan(&bricks)
		require.NoError(t, err)
		require.Equal(t, 1, bricks)
	})

	t.Run("update subtract too much", func(t *testing.T) {
		client, playerId := prepareTest(t)

		_, err := pg.Client.Exec(context.TODO(), "insert into player_backpack (player_id, bricks) values ($1, 2)", playerId)
		require.NoError(t, err)

		_, err = client.UpdateBackpack(context.TODO(), playerId, model.PlayerBackpack{model.Bricks: -5})
		require.ErrorIs(t, err, ErrBalanceTooLow)
	})
}
