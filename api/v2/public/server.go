package public

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/hollow-cube/api-server/internal/playerdb"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

var _ StrictServerInterface = (*server)(nil)

type ServerParams struct {
	fx.In

	Log   *zap.SugaredLogger
	Store *playerdb.Store
}

func NewServer(params ServerParams) (StrictServerInterface, error) {
	return &server{
		log:   params.Log.With("handler", "public"),
		store: params.Store,
	}, nil
}

type server struct {
	log *zap.SugaredLogger

	store *playerdb.Store
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
	recap, err := s.store.GetRecapById(ctx, request.Id)
	if errors.Is(err, playerdb.ErrNoRows) {
		return GetPlayerRecapCorsResponse{Body: GetPlayerRecap404Response{}}, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to get player recap: %w", err)
	}

	return GetPlayerRecapCorsResponse{Body: &GetPlayerRecap200JSONResponse{
		Data:     recap.Data,
		PlayerId: recap.PlayerID,
		Username: recap.Username,
		Year:     recap.Year,
	}}, nil
}
