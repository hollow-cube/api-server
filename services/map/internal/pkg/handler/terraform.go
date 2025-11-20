package handler

import (
	"context"
	"errors"

	v1 "github.com/hollow-cube/hc-services/services/map/api/v1"
	"github.com/hollow-cube/hc-services/services/map/internal/pkg/model"
	"github.com/hollow-cube/hc-services/services/map/internal/pkg/model/transform"
	"github.com/hollow-cube/hc-services/services/map/internal/pkg/storage"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

type TerraformHandler struct {
	log           *zap.SugaredLogger
	storageClient storage.Client
}

type TerraformHandlerParams struct {
	fx.In

	Log     *zap.SugaredLogger
	Storage storage.Client
}

func NewTerraformHandler(p TerraformHandlerParams) (v1.TerraformServer, error) {
	return &TerraformHandler{
		log:           p.Log.With("handler", "terraform"),
		storageClient: p.Storage,
	}, nil
}

func (h *TerraformHandler) GetPlayerSession(ctx context.Context, playerId string) ([]byte, error) {
	data, err := h.storageClient.GetPlayerSession(ctx, playerId)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, v1.ErrSessionNotFound
		}

		return nil, err
	}

	return data, nil
}

func (h *TerraformHandler) GetLocalSession(ctx context.Context, playerId string, worldId string) ([]byte, error) {
	data, err := h.storageClient.GetLocalSession(ctx, playerId, worldId)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, v1.ErrSessionNotFound
		}

		return nil, err
	}

	return data, nil
}

func (h *TerraformHandler) UpdatePlayerSession(ctx context.Context, playerId string, req []byte) error {
	return h.storageClient.UpsertPlayerSession(ctx, playerId, req)
}

func (h *TerraformHandler) UpdateLocalSession(ctx context.Context, playerId string, worldId string, req []byte) error {
	return h.storageClient.UpsertLocalSession(ctx, playerId, worldId, req)
}

func (h *TerraformHandler) ListPlayerSchematics(ctx context.Context, playerId string) ([]*v1.SchematicHeader, error) {
	headers, err := h.storageClient.GetAllSchematics(ctx, playerId)
	if err != nil {
		return nil, err
	}

	response := make([]*v1.SchematicHeader, len(headers))
	for i, header := range headers {
		response[i] = transform.SchematicHeader2API(header)
	}
	return response, nil
}

func (h *TerraformHandler) GetSchematicData(ctx context.Context, playerId string, schemName string) ([]byte, error) {
	//todo validate schem name

	data, err := h.storageClient.GetSchematicData(ctx, playerId, schemName)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, v1.ErrSchemNotFound
		}

		return nil, err
	}

	return data, nil
}

func (h *TerraformHandler) UpdateSchematicHeader(ctx context.Context, playerId string, schemName string, req *v1.UpdateSchematicHeaderRequest) error {
	filetype := "unknown"
	if req.FileType != nil {
		filetype = *req.FileType
	}

	header := &model.SchematicHeader{
		Name:       schemName,
		Dimensions: transform.PackCoordinate(int(req.Dimensions.X), int(req.Dimensions.Y), int(req.Dimensions.Z)),
		FileType:   filetype,
	}

	if err := h.storageClient.UpdateSchematicHeader(ctx, playerId, header); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return v1.ErrSchemNotFound
		}

		return err
	}

	return nil
}

func (h *TerraformHandler) CreateSchematic(ctx context.Context, playerId string, schemName string, dimx int, dimy int, dimz int, fileType string, req []byte) error {
	//todo validate schem name

	header := &model.SchematicHeader{
		Name: schemName,
		Size: len(req),
		// Both optional
		Dimensions: transform.PackCoordinate(dimx, dimy, dimz),
		FileType:   fileType,
	}

	err := h.storageClient.CreateSchematic(ctx, playerId, header, req)
	if err != nil {
		return err
	}

	return nil
}

func (h *TerraformHandler) DeleteSchematic(ctx context.Context, playerId string, schemName string) error {
	//todo validate schem name
	if err := h.storageClient.DeleteSchematic(ctx, playerId, schemName); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return v1.ErrSchemNotFound
		}

		return err
	}

	return nil
}
