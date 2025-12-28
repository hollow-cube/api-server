//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen -o server.gen.go -package public -generate types,strict-server,std-http-server openapi.yaml

package public

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hollow-cube/hc-services/services/map-service/internal/db"
	"github.com/hollow-cube/hc-services/services/map-service/internal/pkg/authz"
	"github.com/hollow-cube/hc-services/services/map-service/internal/pkg/handler"
	"github.com/hollow-cube/hc-services/services/map-service/internal/pkg/model"
	"github.com/hollow-cube/hc-services/services/map-service/internal/pkg/model/transform"
	"github.com/hollow-cube/hc-services/services/map-service/internal/pkg/object"
	"github.com/hollow-cube/hc-services/services/map-service/internal/pkg/storage"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

var _ StrictServerInterface = (*server)(nil)

type ServerParams struct {
	fx.In

	Log *zap.SugaredLogger

	Storage storage.Client
	Store   *db.Store
	Authz   authz.Client
	Object  object.Client `name:"object-mapmaker"`
}

func NewServer(params ServerParams) (StrictServerInterface, error) {
	return &server{
		log:           params.Log.With("handler", "obungus"),
		storageClient: params.Storage,
		store:         params.Store,
		authzClient:   params.Authz,
		objectClient:  params.Object,
	}, nil
}

type server struct {
	log *zap.SugaredLogger

	storageClient storage.Client
	store         *db.Store
	authzClient   authz.Client
	objectClient  object.Client

	cachedTotalMaps, cachedTotalFails int
	cachedTotalsLastUpdate            *time.Time
}

func (s *server) GetMapWorld(ctx context.Context, request GetMapWorldRequestObject) (GetMapWorldResponseObject, error) {
	// Ensure the user has access to the map
	userId := StaticApiUserFromContext(ctx)
	if userId == "" {
		return nil, fmt.Errorf("missing user id")
	}
	state, err := s.authzClient.CheckMapPermission(ctx, request.MapId, userId, authz.NoKey, authz.MapEditWorld)
	if err != nil {
		return nil, fmt.Errorf("failed to check map permission: %w", err)
	}
	if state != authz.Allow {
		return nil, fmt.Errorf("unauthorized, todo this messsage")
		//return nil, &commonV1.Error{Code: http.StatusUnauthorized, Message: "unauthorized"}
	}

	// Fetch the world data
	worldData, err := s.objectClient.Download(ctx, request.MapId)
	if errors.Is(err, object.ErrNotFound) {
		return GetMapWorld204Response{}, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to fetch world data: %w", err)
	}

	return GetMapWorld200ApplicationvndHollowcubePolarResponse{
		Body:          bytes.NewReader(worldData),
		ContentLength: int64(len(worldData)),
	}, nil
}

func (s *server) CreateMap(ctx context.Context, request CreateMapRequestObject) (CreateMapResponseObject, error) {
	m, err := model.CreateDefaultMap(request.Body.OrgId, model.MapSizeNormal)
	if err != nil {
		return nil, err
	}

	var pd *model.PlayerData
	m.Type = model.TypeOrg
	m.Owner = request.Body.OrgId
	if request.Body.LegacyMapId != nil && *request.Body.LegacyMapId != "" {
		m.LegacyMapId = *request.Body.LegacyMapId
	}
	m.Settings.SpawnPoint = api2Pos(request.Body.SpawnPoint)
	if request.Body.ExtraData != nil {
		m.Settings.Extra = *request.Body.ExtraData
	}

	if err = handler.SafeWriteMapToDatabase(ctx, s.storageClient, s.authzClient, nil, m, pd); err != nil {
		return nil, fmt.Errorf("failed to write map to database: %w", err)
	}

	// This is very cursed but im lazy forgive me father for i have sinned.
	raw, err := json.Marshal(transform.Map2API(m))
	if err != nil {
		return nil, fmt.Errorf("failed to marshal map data: %w", err)
	}
	var a CreateMap200JSONResponse
	if err = json.Unmarshal(raw, &a); err != nil {
		return nil, err
	}
	return a, nil
}

func (s *server) UpdateMapWorld(ctx context.Context, request UpdateMapWorldRequestObject) (UpdateMapWorldResponseObject, error) {
	// Ensure the user has access to the map
	userId := StaticApiUserFromContext(ctx)
	if userId == "" {
		return nil, fmt.Errorf("missing user id")
	}
	state, err := s.authzClient.CheckMapPermission(ctx, request.MapId, userId, authz.NoKey, authz.MapEditWorld)
	if err != nil {
		return nil, fmt.Errorf("failed to check map permission: %w", err)
	}
	if state != authz.Allow {
		return nil, fmt.Errorf("unauthorized, todo this messsage")
		//return nil, &commonV1.Error{Code: http.StatusUnauthorized, Message: "unauthorized"}
	}

	return UpdateMapWorld204Response{}, s.objectClient.UploadStream(ctx, request.MapId, request.Body)
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

func api2Pos(p Pos) model.Pos {
	return model.Pos{
		X:     float64(p.X),
		Y:     float64(p.Y),
		Z:     float64(p.Z),
		Pitch: float64(p.Pitch),
		Yaw:   float64(p.Yaw),
	}
}
