package storage

import (
	"context"
	"errors"

	"github.com/hollow-cube/hc-services/services/map-service/internal/pkg/model"
)

var (
	ErrNotFound       = errors.New("not found")
	ErrDuplicateEntry = errors.New("duplicate entry")
)

const (
	MinPublishedMapId = 1
	MaxPublishedMapId = 999_999_999
)

type contextKey string

type MapSortOrder int

const (
	MapSortAsc MapSortOrder = iota
	MapSortDesc
)

type MapSortType int

const (
	MapSortBest MapSortType = iota
	MapSortPublished
	MapSortRandom
)

type SearchQueryV3 struct {
	Page      int          // Required
	PageSize  int          // Required
	Sort      MapSortType  // Required
	SortOrder MapSortOrder // Required

	Variant    []model.MapVariant    // Empty will allow none
	Quality    []int                 // Empty will allow all
	Difficulty []model.MapDifficulty // Empty will allow all

	Owner   string // Zero value will be ignored
	Query   string // Zero value will be ignored
	Contest string // Zero value will be ignored
}

// deprecated
type Client interface {
	RunTransaction(ctx context.Context, f func(ctx context.Context) error) error

	// Maps

	CreateMap(ctx context.Context, m *model.Map) error
	GetMapById(ctx context.Context, id string) (*model.Map, error)
	GetMapsByIds(ctx context.Context, ids []string) ([]*model.Map, error)
	GetMapByPublishedId(ctx context.Context, publishedId string) (*model.Map, error)
	// UpdateMap performs a complete replacement of the map with the given Id.
	// It does not update any fields, just writes the map.
	UpdateMap(ctx context.Context, m *model.Map) error
	DeleteMapSoft(ctx context.Context, mapId, playerId, deleteReason string) error
	SearchOrgMaps(ctx context.Context, page, pageSize int, orgId string) ([]*model.Map, bool, error)
	SearchMapsV3(ctx context.Context, query SearchQueryV3) (m []*model.Map, err error)
	SearchMapsCountV3(ctx context.Context, query SearchQueryV3) (count int, err error)
	GetMapProgress(ctx context.Context, playerId string, mapIds []string) ([]*model.MapIdAndProgress, error)
	GetRecentMaps(ctx context.Context, page, pageSize int, playerId string, saveStateType model.SaveStateType) ([]*model.Map, bool, error)

	GetMapsBeatenLeaderboard(ctx context.Context) ([]*model.LeaderboardEntry, error)
	GetMapsBeatenLeaderboardForPlayer(ctx context.Context, playerId string) (int, error)
	GetTopTimesLeaderboard(ctx context.Context) ([]*model.LeaderboardEntry, error)
	GetTopTimesLeaderboardForPlayer(ctx context.Context, playerId string) (int, error)

	// Save states

	GetSaveStateById(ctx context.Context, mapId, playerId, saveStateId string) (*model.SaveState, error)
	GetLatestSaveState(ctx context.Context, mapId, playerId string, ssType model.SaveStateType) (*model.SaveState, error)
	GetBestSaveState(ctx context.Context, mapId, playerId string) (*model.SaveState, error)
	GetBestSaveStateSinceBeta(ctx context.Context, mapId, playerId string) (*model.SaveState, error)
	GetAllSaveStates(ctx context.Context, mapId string) ([]*model.SaveState, error)
	DeleteSaveState(ctx context.Context, mapId, playerId, saveStateId string) error
	// Marks save states as deleted. They should not be returned by any other queries.
	SoftDeleteMapPlayerSaveStates(ctx context.Context, mapId, playerId string) error
	SoftDeleteMapSaveStates(ctx context.Context, mapId string, onlyIncomplete bool) error
	SoftDeletePlayerSaveStates(ctx context.Context, playerId string) error
	GetCompletedMaps(ctx context.Context, playerId string) ([]string, error)

	UpdateMapStats(ctx context.Context, mapId string)

	// Organizations

	GetOrgById(ctx context.Context, id string) (*model.Organization, error)

	// Terraform
	GetLocalSession(ctx context.Context, playerId string, worldId string) ([]byte, error)
	UpsertPlayerSession(ctx context.Context, playerId string, state []byte) error
	UpsertLocalSession(ctx context.Context, playerId string, worldId string, state []byte) error

	GetAllSchematics(ctx context.Context, playerId string) ([]*model.SchematicHeader, error)
	GetSchematicData(ctx context.Context, playerId, schemName string) ([]byte, error)
	CreateSchematic(ctx context.Context, playerId string, header *model.SchematicHeader, data []byte) error
	UpdateSchematicHeader(ctx context.Context, playerId string, header *model.SchematicHeader) error
	DeleteSchematic(ctx context.Context, playerId, schemName string) error

	// Obungus
	GetUnreviewedBox(ctx context.Context, player string) (*model.Box, error)
}
