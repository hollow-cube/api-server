//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen -o server.gen.go -package public -generate types,strict-server,std-http-server openapi.yaml

package public

import (
	"context"
	"time"

	"github.com/hollow-cube/hc-services/services/player/internal/pkg/storage"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

var _ StrictServerInterface = (*server)(nil)

type ServerParams struct {
	fx.In

	Log     *zap.SugaredLogger
	Storage storage.Client
}

func NewServer(params ServerParams) (StrictServerInterface, error) {
	return &server{
		log:           params.Log.With("handler", "public"),
		storageClient: params.Storage,
	}, nil
}

type server struct {
	log *zap.SugaredLogger

	storageClient storage.Client

	cachedTotalPlayers, cachedTotalPlaytime int
	cachedTotalsLastUpdated                 *time.Time
}

func (s *server) GetPublicStats(ctx context.Context, _ GetPublicStatsRequestObject) (GetPublicStatsResponseObject, error) {
	if s.cachedTotalsLastUpdated == nil || time.Since(*s.cachedTotalsLastUpdated) > 5*time.Minute {
		var err error
		s.cachedTotalPlayers, s.cachedTotalPlaytime, err = s.storageClient.CountPlayerStats(ctx)
		if err != nil {
			s.log.Errorw("failed to get player stats", "error", err)
			return nil, err
		}

		now := time.Now()
		s.cachedTotalsLastUpdated = &now
	}

	return &GetPublicStats200JSONResponse{
		TotalPlayers:  s.cachedTotalPlayers,
		TotalPlaytime: s.cachedTotalPlaytime / 1000,
	}, nil
}
