package handler

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hollow-cube/hc-services/services/player-service/internal/pkg/authz/mock_authz"
	"github.com/hollow-cube/hc-services/services/player-service/internal/pkg/model"
	"github.com/hollow-cube/hc-services/services/player-service/internal/pkg/storage/mock_storage"
	"github.com/hollow-cube/hc-services/services/player-service/internal/pkg/testutil"
	"github.com/hollow-cube/tebex-go"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"
)

func setupTest(t *testing.T, autotxn bool) (*PaymentsHandler, *mock_storage.MockClient, *mock_authz.MockClient, func()) {
	t.Helper()

	ctrl := gomock.NewController(t)

	authClient := mock_authz.NewMockClient(ctrl)
	storageClient := mock_storage.NewMockClient(ctrl)

	if autotxn {
		storageClient.EXPECT().RunTransaction(gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, f func(ctx2 context.Context) error) error {
				return f(ctx)
			}).
			AnyTimes()
	}

	return &PaymentsHandler{
		log:           zaptest.NewLogger(t).Sugar(),
		storageClient: storageClient,
		authClient:    authClient,
	}, storageClient, authClient, ctrl.Finish
}

func TestPaymentsHandler_ApplyChangeList(t *testing.T) {
	t.Run("valid complex (single target)", func(t *testing.T) {
		h, storageClient, authClient, assertMocks := setupTest(t, true)

		playerId, txId := uuid.NewString(), uuid.NewString()
		storageClient.EXPECT().LogTebexEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		storageClient.EXPECT().CreateTebexState(gomock.Any(), txId, gomock.Any()).Return(nil).Times(1)
		storageClient.EXPECT().AddCurrency(gomock.Any(), playerId, model.Cubits, 100, model.BalanceChangeReasonTebexOneoff, gomock.Any()).Return(100, nil).Times(1)
		storageClient.EXPECT().AddCurrency(gomock.Any(), playerId, model.Cubits, 70, model.BalanceChangeReasonTebexOneoff, gomock.Any()).Return(170, nil).Times(1)
		authClient.EXPECT().AppendHypercube(gomock.Any(), playerId, 60*time.Minute, gomock.Any()).Return(nil).Times(1)

		newBalances, err := h.applyChangeList(context.Background(), &tebex.Event{Id: txId}, txId, []*model.TebexChange{
			{Target: playerId, Type: model.TebexChangeCubits, Amount: 100},
			{Target: playerId, Type: model.TebexChangeHypercube, Amount: 60 /* minutes */},
			{Target: playerId, Type: model.TebexChangeCubits, Amount: 70},
		})
		require.NoError(t, err)
		require.Equal(t, 170, newBalances[playerId])

		assertMocks() // The rest of the assertions are made by mocked functions
	})

	t.Run("write log fail (hypercube only)", func(t *testing.T) {
		h, storageClient, authClient, assertMocks := setupTest(t, true)

		playerId, txId := uuid.NewString(), uuid.NewString()
		storageClient.EXPECT().LogTebexEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(testutil.ErrMock).Times(1)

		// This is notable, as part of 2pc we should expect to see the initial hypercube write, and then the rollback.
		authClient.EXPECT().AppendHypercube(gomock.Any(), playerId, 60*time.Minute, gomock.Any()).Return(nil).Times(1)
		authClient.EXPECT().AppendHypercube(gomock.Any(), playerId, -60*time.Minute, gomock.Any()).Return(nil).Times(1)

		_, err := h.applyChangeList(context.Background(), &tebex.Event{Id: txId}, txId, []*model.TebexChange{
			{Target: playerId, Type: model.TebexChangeHypercube, Amount: 60 /* minutes */},
		})
		require.ErrorIs(t, err, testutil.ErrMock)

		assertMocks() // The rest of the assertions are made by mocked functions
	})

	t.Run("write create state fail (hypercube only)", func(t *testing.T) {
		h, storageClient, authClient, assertMocks := setupTest(t, true)

		playerId, txId := uuid.NewString(), uuid.NewString()
		storageClient.EXPECT().LogTebexEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		storageClient.EXPECT().CreateTebexState(gomock.Any(), txId, gomock.Any()).Return(testutil.ErrMock).Times(1)

		// This is notable, as part of 2pc we should expect to see the initial hypercube write, and then the rollback.
		authClient.EXPECT().AppendHypercube(gomock.Any(), playerId, 60*time.Minute, gomock.Any()).Return(nil).Times(1)
		authClient.EXPECT().AppendHypercube(gomock.Any(), playerId, -60*time.Minute, gomock.Any()).Return(nil).Times(1)

		_, err := h.applyChangeList(context.Background(), &tebex.Event{Id: txId}, txId, []*model.TebexChange{
			{Target: playerId, Type: model.TebexChangeHypercube, Amount: 60 /* minutes */},
		})
		require.ErrorIs(t, err, testutil.ErrMock)

		assertMocks() // The rest of the assertions are made by mocked functions
	})

	t.Run("partial cubit apply fail (complex)", func(t *testing.T) {
		h, storageClient, authClient, assertMocks := setupTest(t, true)

		playerId, txId := uuid.NewString(), uuid.NewString()
		storageClient.EXPECT().LogTebexEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		storageClient.EXPECT().CreateTebexState(gomock.Any(), txId, gomock.Any()).Return(nil).Times(1)

		// First apply passes, second returns an error.
		storageClient.EXPECT().AddCurrency(gomock.Any(), playerId, model.Cubits, 100, model.BalanceChangeReasonTebexOneoff, gomock.Any()).Return(100, nil).Times(1)
		storageClient.EXPECT().AddCurrency(gomock.Any(), playerId, model.Cubits, 70, model.BalanceChangeReasonTebexOneoff, gomock.Any()).Return(0, testutil.ErrMock).Times(1)

		// This is notable, as part of 2pc we should expect to see the initial hypercube write, and then the rollback.
		authClient.EXPECT().AppendHypercube(gomock.Any(), playerId, 60*time.Minute, gomock.Any()).Return(nil).Times(1)
		authClient.EXPECT().AppendHypercube(gomock.Any(), playerId, -60*time.Minute, gomock.Any()).Return(nil).Times(1)

		_, err := h.applyChangeList(context.Background(), &tebex.Event{Id: txId}, txId, []*model.TebexChange{
			{Target: playerId, Type: model.TebexChangeCubits, Amount: 100},
			{Target: playerId, Type: model.TebexChangeHypercube, Amount: 60 /* minutes */},
			{Target: playerId, Type: model.TebexChangeCubits, Amount: 70},
		})
		require.ErrorIs(t, err, testutil.ErrMock)

		assertMocks() // The rest of the assertions are made by mocked functions
	})

	t.Run("tx commit fail", func(t *testing.T) {
		h, storageClient, authClient, assertMocks := setupTest(t, false)

		playerId, txId := uuid.NewString(), uuid.NewString()
		storageClient.EXPECT().RunTransaction(gomock.Any(), gomock.Any()).Return(testutil.ErrMock).Times(1)

		// As part of 2pc we should expect to see the initial hypercube write, and then the rollback.
		authClient.EXPECT().AppendHypercube(gomock.Any(), playerId, 60*time.Minute, gomock.Any()).Return(nil).Times(1)
		authClient.EXPECT().AppendHypercube(gomock.Any(), playerId, -60*time.Minute, gomock.Any()).Return(nil).Times(1)

		_, err := h.applyChangeList(context.Background(), &tebex.Event{Id: txId}, txId, []*model.TebexChange{
			{Target: playerId, Type: model.TebexChangeHypercube, Amount: 60 /* minutes */},
		})
		require.ErrorIs(t, err, testutil.ErrMock)

		assertMocks() // The rest of the assertions are made by mocked functions
	})
}
