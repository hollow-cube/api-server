//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen -o server.gen.go -package public -generate types,strict-server,std-http-server openapi.yaml

package public

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/hollow-cube/hc-services/services/player-service/internal/db"
	"github.com/hollow-cube/hc-services/services/player-service/internal/pkg/storage"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

var _ StrictServerInterface = (*server)(nil)

type ServerParams struct {
	fx.In

	Log     *zap.SugaredLogger
	Store   *db.Store
	Storage storage.Client
}

func NewServer(params ServerParams) (StrictServerInterface, error) {
	return &server{
		log:           params.Log.With("handler", "public"),
		store:         params.Store,
		storageClient: params.Storage,
	}, nil
}

type server struct {
	log *zap.SugaredLogger

	store         *db.Store
	storageClient storage.Client

	cachedTotalPlayers, cachedTotalPlaytime int
	cachedTotalsLastUpdated                 *time.Time
}

func (s *server) GetPublicStats(ctx context.Context, _ GetPublicStatsRequestObject) (GetPublicStatsResponseObject, error) {
	if s.cachedTotalsLastUpdated == nil || time.Since(*s.cachedTotalsLastUpdated) > 5*time.Minute {
		result, err := s.store.GetPlayerStats(ctx)
		if err != nil {
			s.log.Errorw("failed to get player stats", "error", err)
			return nil, err
		}

		s.cachedTotalPlayers = int(result.Count)
		// int64 is probably fine here:
		// 2.56204778e12 hours total possible (divided since we store in ms)
		// An average of 500 hours of playtime would accommodate 5,124,095,560 unique players
		s.cachedTotalPlaytime = int(result.Sum)

		now := time.Now()
		s.cachedTotalsLastUpdated = &now
	}

	return &GetPublicStats200JSONResponse{
		TotalPlayers:  s.cachedTotalPlayers,
		TotalPlaytime: s.cachedTotalPlaytime / 1000,
	}, nil
}

type GetPlayerRecapCorsResponse struct {
	Body GetPlayerRecapResponseObject
}

func (response GetPlayerRecapCorsResponse) VisitGetPlayerRecapResponse(w http.ResponseWriter) error {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")

	return response.Body.VisitGetPlayerRecapResponse(w)
}

func (s *server) GetPlayerRecap(ctx context.Context, request GetPlayerRecapRequestObject) (GetPlayerRecapResponseObject, error) {
	recap, err := s.storageClient.GetPlayerRecapById(ctx, request.Id)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return GetPlayerRecapCorsResponse{Body: GetPlayerRecap404Response{}}, nil
		}
		return nil, fmt.Errorf("failed to get player recap: %w", err)
	}

	return GetPlayerRecapCorsResponse{Body: &GetPlayerRecap200JSONResponse{
		Data:     recap.Data,
		PlayerId: recap.PlayerId,
		Username: recap.Username,
		Year:     recap.Year,
	}}, nil
}
