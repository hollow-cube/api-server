//go:build integration

package handler

import (
	"context"
	"embed"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hollow-cube/hc-services/services/player/internal/pkg/authz"
	"github.com/hollow-cube/hc-services/services/player/internal/pkg/model"
	"github.com/hollow-cube/hc-services/services/player/internal/pkg/storage"
	"github.com/hollow-cube/hc-services/services/player/internal/pkg/testutil"
	"github.com/hollow-cube/tebex-go"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

//go:embed testdata
var testdata embed.FS

var (
	sdb *testutil.SpiceDB
	pg  *testutil.Postgres
)

func TestMain(m *testing.M) {
	pool, run, cleanup := testutil.Init(m)
	defer cleanup()

	sdb, cleanup = testutil.RunSpiceDB(pool)
	defer cleanup()
	pg, cleanup = testutil.RunPostgres(pool)
	defer cleanup()

	run()
}

func prepareTest(t *testing.T) *PaymentsHandler {
	t.Helper()
	t.Cleanup(sdb.Cleanup)
	t.Cleanup(pg.Cleanup)

	storageClient, err := storage.NewPostgresClientFromClient(pg.Client)
	require.NoError(t, err)

	return &PaymentsHandler{
		log:                   zaptest.NewLogger(t).Sugar(),
		tebexSecret:           nil,
		disputeDiscordWebhook: "",
		producer:              nil,
		storageClient:         storageClient,
		authClient:            authz.NewSpiceDBClientFromClient(sdb.Client),
	}
}

func TestApplyChangeList(t *testing.T) {
	t.Run("no such player", func(t *testing.T) {
		//h := prepareTest(t)
		//playerId := createPlayer(t, h)
		//todo
	})

	t.Run("single cubit change", func(t *testing.T) {
		h := prepareTest(t)
		playerId := createPlayer(t, h, 25)
		newBalances, err := h.applyChangeList(context.Background(), newEvent(), playerId, []*model.TebexChange{
			{Target: playerId, Type: model.TebexChangeCubits, Amount: 50},
		})
		require.NoError(t, err)
		require.Equal(t, 75, newBalances[playerId])

		playerData, err := h.storageClient.GetPlayerData(context.Background(), playerId)
		require.NoError(t, err)
		require.Equal(t, 75, playerData.Cubits)
	})

	t.Run("multiple cubit change same player", func(t *testing.T) {
		h := prepareTest(t)
		playerId := createPlayer(t, h, 25)
		newBalances, err := h.applyChangeList(context.Background(), newEvent(), uuid.NewString(), []*model.TebexChange{
			{Target: playerId, Type: model.TebexChangeCubits, Amount: 50},
			{Target: playerId, Type: model.TebexChangeCubits, Amount: 100},
		})
		require.NoError(t, err)
		require.Equal(t, 175, newBalances[playerId])

		playerData, err := h.storageClient.GetPlayerData(context.Background(), playerId)
		require.NoError(t, err)
		require.Equal(t, 175, playerData.Cubits)
	})

	t.Run("multiple cubit change different players", func(t *testing.T) {
		h := prepareTest(t)
		player1 := createPlayer(t, h, 25)
		player2 := createPlayer(t, h, 1000)
		newBalances, err := h.applyChangeList(context.Background(), newEvent(), uuid.NewString(), []*model.TebexChange{
			{Target: player1, Type: model.TebexChangeCubits, Amount: 50},
			{Target: player2, Type: model.TebexChangeCubits, Amount: 100},
		})
		require.NoError(t, err)

		require.Equal(t, 75, newBalances[player1])
		playerData1, err := h.storageClient.GetPlayerData(context.Background(), player1)
		require.NoError(t, err)
		require.Equal(t, 75, playerData1.Cubits)

		require.Equal(t, 1100, newBalances[player2])
		playerData2, err := h.storageClient.GetPlayerData(context.Background(), player2)
		require.NoError(t, err)
		require.Equal(t, 1100, playerData2.Cubits)
	})

	t.Run("single hypercube add", func(t *testing.T) {
		h := prepareTest(t)
		playerId := createPlayer(t, h, 0)
		_, err := h.applyChangeList(context.Background(), newEvent(), playerId, []*model.TebexChange{
			{Target: playerId, Type: model.TebexChangeHypercube, Amount: int((31 * 24 * time.Hour).Minutes())},
		})
		require.NoError(t, err)

		//todo add util.CurrentTime() to get the current time with a constant during test.
		_, term, err := h.authClient.GetHypercubeStats(context.TODO(), playerId, authz.NoKey)
		require.NoError(t, err)
		require.Equal(t, 31*24*time.Hour, term)
	})

	t.Run("single hypercube extend", func(t *testing.T) {
		h := prepareTest(t)
		playerId := createPlayer(t, h, 0)

		err := h.authClient.AppendHypercube(context.TODO(), playerId, 31*24*time.Hour, authz.NoKey)
		require.NoError(t, err)

		_, err = h.applyChangeList(context.Background(), newEvent(), playerId, []*model.TebexChange{
			{Target: playerId, Type: model.TebexChangeHypercube, Amount: int((31 * 24 * time.Hour).Minutes())},
		})
		require.NoError(t, err)

		//todo add util.CurrentTime() to get the current time with a constant during test.
		_, term, err := h.authClient.GetHypercubeStats(context.TODO(), playerId, authz.NoKey)
		require.NoError(t, err)
		require.Equal(t, 2*31*24*time.Hour, term)
	})

}

func TestApplyFullPaymentCompletedEvent(t *testing.T) {
	t.Run("completed_cubits_single", func(t *testing.T) {
		h := prepareTest(t)

		rawEvent, err := testdata.ReadFile("testdata/completed_cubits_single.json")
		require.NoError(t, err)
		event, err := tebex.ParseEvent(rawEvent)
		require.NoError(t, err)

		playerId := createPlayerUsername(t, h, "notmattw")
		err = h.handlePaymentCompletedEvent(context.TODO(), event, event.Subject.(*tebex.PaymentCompletedEvent))
		require.NoError(t, err)

		{ // Player should have the correct balance
			pd, err := h.storageClient.GetPlayerData(context.TODO(), playerId)
			require.NoError(t, err)
			require.Equal(t, 50, pd.Cubits)
		}
		{ // Player should not have hypercube (sanity check basically)
			_, _, err := h.authClient.GetHypercubeStats(context.TODO(), playerId, authz.NoKey)
			require.ErrorIs(t, err, authz.ErrNotFound)
		}
		{ // There should be a state entry which is not reverted
			var txId string
			err := pg.Client.QueryRow(context.TODO(), "SELECT tx_id FROM tebex_state WHERE reverted = false").Scan(&txId)
			require.NoError(t, err)
			require.Equal(t, event.Subject.(*tebex.PaymentCompletedEvent).TransactionId, txId)
		}
	})

	t.Run("completed_hypercube_single", func(t *testing.T) {
		h := prepareTest(t)

		rawEvent, err := testdata.ReadFile("testdata/completed_hypercube_single.json")
		require.NoError(t, err)
		event, err := tebex.ParseEvent(rawEvent)
		require.NoError(t, err)

		now := time.Now()
		playerId := createPlayerUsername(t, h, "notmattw")
		err = h.handlePaymentCompletedEvent(context.TODO(), event, event.Subject.(*tebex.PaymentCompletedEvent))
		require.NoError(t, err)

		{ // Player should have zero balance (sanity check basically)
			pd, err := h.storageClient.GetPlayerData(context.TODO(), playerId)
			require.NoError(t, err)
			require.Equal(t, 0, pd.Cubits)
		}
		{ // Player should have the expected hypercube durations
			startTime, term, err := h.authClient.GetHypercubeStats(context.TODO(), playerId, authz.NoKey)
			require.NoError(t, err)
			require.WithinDuration(t, now, startTime, 10*time.Second)
			require.Equal(t, 31*24*time.Hour, term)
		}
		{ // There should be a state entry which is not reverted
			var txId string
			err := pg.Client.QueryRow(context.TODO(), "SELECT tx_id FROM tebex_state WHERE reverted = false").Scan(&txId)
			require.NoError(t, err)
			require.Equal(t, event.Subject.(*tebex.PaymentCompletedEvent).TransactionId, txId)
		}
	})
}

func createPlayer(t *testing.T, h *PaymentsHandler, cubits int) (playerId string) {
	playerId = uuid.NewString()

	time0 := time.UnixMilli(0)
	err := h.storageClient.CreatePlayerData(context.Background(), &model.PlayerData{
		Id:          playerId,
		Username:    playerId[0:8],
		FirstJoin:   time0,
		LastOnline:  time0,
		Settings:    make(model.PlayerSettings),
		IpHistory:   []string{},
		BetaEnabled: true,
	})
	require.NoError(t, err)

	if cubits > 0 {
		_, err = h.storageClient.AddCurrency(context.Background(), playerId, model.Cubits, cubits, "test_init", nil)
		require.NoError(t, err)
	}

	return
}

func createPlayerUsername(t *testing.T, h *PaymentsHandler, username string) (playerId string) {
	playerId = uuid.NewString()

	time0 := time.UnixMilli(0)
	err := h.storageClient.CreatePlayerData(context.Background(), &model.PlayerData{
		Id:          playerId,
		Username:    username,
		FirstJoin:   time0,
		LastOnline:  time0,
		Settings:    make(model.PlayerSettings),
		IpHistory:   []string{},
		BetaEnabled: true,
	})
	require.NoError(t, err)

	return
}

func newEvent() *tebex.Event {
	return &tebex.Event{
		Id:   uuid.NewString(),
		Type: "payment.completed",
		Date: time.Now(),
	}
}
