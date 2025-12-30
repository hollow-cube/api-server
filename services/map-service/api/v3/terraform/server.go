//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen -o server.gen.go -package terraform -generate types,strict-server,std-http-server openapi.yaml

package terraform

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/hollow-cube/hc-services/services/map-service/internal/db"
	"github.com/hollow-cube/hc-services/services/map-service/internal/pkg/model"
	"github.com/hollow-cube/hc-services/services/map-service/internal/pkg/storage"
	"github.com/jackc/pgx/v5"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

var _ StrictServerInterface = (*server)(nil)

type ServerParams struct {
	fx.In

	Log *zap.SugaredLogger

	Queries *db.Queries
}

type server struct {
	log *zap.SugaredLogger

	queries *db.Queries
}

func NewServer(params ServerParams) (StrictServerInterface, error) {
	return &server{
		log:           params.Log.With("handler", "obungus"),
		storageClient: params.Storage,
	}, nil
}

func (s *server) GetPlayerSession(ctx context.Context, request GetPlayerSessionRequestObject) (GetPlayerSessionResponseObject, error) {
	data, err := s.queries.TfGetPlayerSession(ctx, request.PlayerId)
	if errors.Is(err, pgx.ErrNoRows) {
		return GetPlayerSession404Response{}, nil
	} else if err != nil {
		return nil, err
	}

	return GetPlayerSession200ApplicationvndTerraformPlayerSessionResponse{
		Body:          bytes.NewReader(data),
		ContentLength: int64(len(data)),
	}, nil
}

func (s *server) UpdatePlayerSession(ctx context.Context, request UpdatePlayerSessionRequestObject) (UpdatePlayerSessionResponseObject, error) {
	body, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read request body: %w", err)
	}
	err = s.storageClient.UpsertPlayerSession(ctx, request.PlayerId, body)
	if err != nil {
		return nil, fmt.Errorf("failed to update player session: %w", err)
	}
	return UpdatePlayerSession200Response{}, nil
}

func (s *server) GetLocalSession(ctx context.Context, request GetLocalSessionRequestObject) (GetLocalSessionResponseObject, error) {
	data, err := s.storageClient.GetLocalSession(ctx, request.PlayerId, request.WorldId)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return GetLocalSession404Response{}, nil
		}

		return nil, err
	}

	return GetLocalSession200ApplicationvndTerraformLocalSessionResponse{
		Body:          bytes.NewReader(data),
		ContentLength: int64(len(data)),
	}, nil
}

func (s *server) UpdateLocalSession(ctx context.Context, request UpdateLocalSessionRequestObject) (UpdateLocalSessionResponseObject, error) {
	body, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read request body: %w", err)
	}
	err = s.storageClient.UpsertLocalSession(ctx, request.PlayerId, request.WorldId, body)
	if err != nil {
		return nil, fmt.Errorf("failed to update local session: %w", err)
	}
	return UpdateLocalSession200Response{}, nil
}

func (s *server) ListPlayerSchematics(ctx context.Context, request ListPlayerSchematicsRequestObject) (ListPlayerSchematicsResponseObject, error) {
	headers, err := s.storageClient.GetAllSchematics(ctx, request.PlayerId)
	if err != nil {
		return nil, err
	}

	response := make(ListPlayerSchematics200JSONResponse, len(headers))
	for i, header := range headers {
		response[i] = schematicHeaderToAPI(header)
	}
	return response, nil
}

func (s *server) GetSchematicData(ctx context.Context, request GetSchematicDataRequestObject) (GetSchematicDataResponseObject, error) {
	//todo validate schem name
	data, err := s.storageClient.GetSchematicData(ctx, request.PlayerId, request.SchemName)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return GetSchematicData404Response{}, nil
		}

		return nil, err
	}

	return GetSchematicData200ApplicationvndTerraformSchematicResponse{
		Body:          bytes.NewReader(data),
		ContentLength: int64(len(data)),
	}, nil
}

func (s *server) UpdateSchematicHeader(ctx context.Context, request UpdateSchematicHeaderRequestObject) (UpdateSchematicHeaderResponseObject, error) {
	filetype := "unknown"
	if request.Body.FileType != nil {
		filetype = *request.Body.FileType
	}

	header := &model.SchematicHeader{
		Name:       request.SchemName,
		Dimensions: packCoordinate(int(request.Body.Dimensions.Y), int(request.Body.Dimensions.Y), int(request.Body.Dimensions.Y)),
		FileType:   filetype,
	}

	if err := s.storageClient.UpdateSchematicHeader(ctx, request.PlayerId, header); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return UpdateSchematicHeader404Response{}, nil
		}

		return nil, fmt.Errorf("failed to update schematic header: %w", err)
	}

	return UpdateSchematicHeader200Response{}, nil
}

func (s *server) CreateSchematic(ctx context.Context, request CreateSchematicRequestObject) (CreateSchematicResponseObject, error) {
	//todo validate schem name
	body, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read request body: %w", err)
	}

	var fileType string
	if request.Params.FileType != nil {
		fileType = *request.Params.FileType
	}
	var dimx, dimy, dimz int
	if request.Params.Dimx != nil {
		dimx = *request.Params.Dimx
	}
	if request.Params.Dimy != nil {
		dimy = *request.Params.Dimy
	}
	if request.Params.Dimz != nil {
		dimz = *request.Params.Dimz
	}
	header := &model.SchematicHeader{
		Name: request.SchemName,
		Size: len(body),
		// Both optional
		Dimensions: packCoordinate(dimx, dimy, dimz),
		FileType:   fileType,
	}

	err = s.storageClient.CreateSchematic(ctx, request.PlayerId, header, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create schematic: %w", err)
	}
	return CreateSchematic200Response{}, nil
}

func (s *server) DeleteSchematic(ctx context.Context, request DeleteSchematicRequestObject) (DeleteSchematicResponseObject, error) {
	//todo validate schem name
	if err := s.storageClient.DeleteSchematic(ctx, request.PlayerId, request.SchemName); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return DeleteSchematic404Response{}, nil
		}

		return nil, fmt.Errorf("failed to delete schematic: %w", err)
	}

	return DeleteSchematic200Response{}, nil
}

func schematicHeaderToAPI(header *model.SchematicHeader) SchematicHeader {
	x, y, z := unpackCoordinate(header.Dimensions)
	return SchematicHeader{
		Name: header.Name,
		Size: float32(header.Size),
		Dimensions: &TFPoint{
			X: float32(x),
			Y: float32(y),
			Z: float32(z),
		},
	}
}

// packCoordinate takes a Coordinate and packs it into an int64, preserving the sign.
func packCoordinate(x, y, z int) int64 {
	return int64(x)<<40 | int64(y)<<20 | int64(z)
}

// unpackCoordinate takes an int64 and unpacks it into a Coordinate.
func unpackCoordinate(coord int64) (int, int, int) {
	return int(coord >> 40),
		int(coord >> 20 & 0x0000000000000FFF),
		int(coord & 0x0000000000000FFF)
}
