//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen -o server.gen.go -package public -generate types,strict-server,std-http-server openapi.yaml

package public

import (
	"context"
	"time"

	"github.com/hollow-cube/hc-services/services/player-service/internal/mapdb"
	"github.com/hollow-cube/hc-services/services/player-service/internal/object"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

var _ StrictServerInterface = (*server)(nil)

type ServerParams struct {
	fx.In

	Log *zap.SugaredLogger

	Store  *mapdb.Store
	Object object.Client `name:"object-mapmaker"`
}

func NewServer(params ServerParams) (StrictServerInterface, error) {
	return &server{
		log:          params.Log.With("handler", "obungus"),
		store:        params.Store,
		objectClient: params.Object,
	}, nil
}

type server struct {
	log *zap.SugaredLogger

	store        *mapdb.Store
	objectClient object.Client

	cachedTotalMaps, cachedTotalFails int
	cachedTotalsLastUpdate            *time.Time
}

func (s *server) GetMapStats(ctx context.Context, _ GetMapStatsRequestObject) (GetMapStatsResponseObject, error) {
	if s.cachedTotalsLastUpdate == nil || time.Since(*s.cachedTotalsLastUpdate) > 5*time.Minute {
		i, err := s.store.CountMaps(ctx)
		s.cachedTotalMaps = int(i)
		if err != nil {
			return nil, err
		}
		i, err = s.store.CountFailSaveStates(ctx)
		s.cachedTotalFails = int(i)
		if err != nil {
			return nil, err
		}
		now := time.Now()
		s.cachedTotalsLastUpdate = &now
	}

	return GetMapStats200JSONResponse{
		TotalMaps:  s.cachedTotalMaps,
		TotalFails: s.cachedTotalFails,
	}, nil
}
