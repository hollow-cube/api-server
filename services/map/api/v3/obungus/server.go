//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen -o server.gen.go -package obungus -generate types,strict-server,std-http-server openapi.yaml

package obungus

import (
	"context"
	"encoding/base64"
	"errors"
	"time"

	"github.com/hollow-cube/hc-services/services/map/internal/pkg/storage"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

var _ StrictServerInterface = (*server)(nil)

type ServerParams struct {
	fx.In

	Log *zap.SugaredLogger

	Storage storage.Client
}

func NewServer(params ServerParams) (StrictServerInterface, error) {
	return &server{
		log:           params.Log.With("handler", "obungus"),
		storageClient: params.Storage,
	}, nil
}

type server struct {
	log *zap.SugaredLogger

	storageClient storage.Client
}

func (s *server) GetBoxFromReviewQueue(ctx context.Context, request GetBoxFromReviewQueueRequestObject) (GetBoxFromReviewQueueResponseObject, error) {
	box, err := s.storageClient.GetUnreviewedBox(ctx, request.Params.Player)
	if errors.Is(err, storage.ErrNotFound) {
		return GetBoxFromReviewQueue404Response{}, nil
	} else if err != nil {
		return nil, err
	}

	schemData := base64.StdEncoding.EncodeToString(box.SchematicData)
	return GetBoxFromReviewQueue200JSONResponse{
		Id:             box.Id,
		PlayerId:       box.PlayerId,
		CreatedAt:      box.CreatedAt.Format(time.RFC3339),
		Name:           box.Name,
		Shape:          (*string)(&box.Shape),
		LegacyUsername: box.LegacyUsername,
		SchematicData:  &schemData,
	}, nil
}
