package storage

import (
	"context"
	"errors"

	"github.com/hollow-cube/hc-services/services/map/internal/pkg/model"
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

type Client interface {
	RunTransaction(ctx context.Context, f func(ctx context.Context) error) error

	// Maps

	CountMaps(ctx context.Context) (int, error)
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
	FindNextPublishedId(ctx context.Context) (int64, error)
	GetRecentMaps(ctx context.Context, page, pageSize int, playerId string, saveStateType model.SaveStateType) ([]*model.Map, bool, error)

	// WriteReport writes a report to the database, returning its numeric ID.
	WriteReport(ctx context.Context, report *model.MapReport) (int, error)

	GetMapsBeatenLeaderboard(ctx context.Context) ([]*model.LeaderboardEntry, error)
	GetMapsBeatenLeaderboardForPlayer(ctx context.Context, playerId string) (int, error)
	GetTopTimesLeaderboard(ctx context.Context) ([]*model.LeaderboardEntry, error)
	GetTopTimesLeaderboardForPlayer(ctx context.Context, playerId string) (int, error)

	// Save states

	CountFailSaveStates(ctx context.Context) (int, error)
	CreateSaveState(ctx context.Context, ss *model.SaveState) error
	GetSaveStateById(ctx context.Context, mapId, playerId, saveStateId string) (*model.SaveState, error)
	GetLatestSaveState(ctx context.Context, mapId, playerId string, ssType model.SaveStateType) (*model.SaveState, error)
	GetBestSaveState(ctx context.Context, mapId, playerId string) (*model.SaveState, error)
	GetBestSaveStateSinceBeta(ctx context.Context, mapId, playerId string) (*model.SaveState, error)
	GetAllSaveStates(ctx context.Context, mapId string) ([]*model.SaveState, error)
	UpdateSaveState(ctx context.Context, ss *model.SaveState) error
	DeleteSaveState(ctx context.Context, mapId, playerId, saveStateId string) error
	DeleteVerifyingStates(ctx context.Context, mapId string) error
	// Marks save states as deleted. They should not be returned by any other queries.
	SoftDeleteMapPlayerSaveStates(ctx context.Context, mapId, playerId string) error
	SoftDeleteMapSaveStates(ctx context.Context, mapId string, onlyIncomplete bool) error
	SoftDeletePlayerSaveStates(ctx context.Context, playerId string) error
	GetCompletedMaps(ctx context.Context, playerId string) ([]string, error)

	// Ratings

	GetMapRating(ctx context.Context, mapId, playerId string) (*model.MapRating, error)
	UpsertMapRating(ctx context.Context, mr *model.MapRating) error

	// Players

	// GetPlayerData fetches a player data from the database, or returns the default player data otherwise.
	// A not found error will _never_ be returned.
	// Deprecated
	GetPlayerData(ctx context.Context, playerId string) (*model.PlayerData, error)
	GetPlayerData2(ctx context.Context, playerId string) (*model.PlayerData, error)
	// UpdatePlayerData writes a player data to the database, creating the record if it does not exist.
	// A not found error will _never_ be returned.
	UpdatePlayerData(ctx context.Context, pd *model.PlayerData) error
	RemoveMapFromSlots(ctx context.Context, mapId string) ([]*model.PlayerData, error)

	// Organizations

	GetOrgById(ctx context.Context, id string) (*model.Organization, error)

	// Terraform
	GetPlayerSession(ctx context.Context, playerId string) ([]byte, error)
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

	// Old garbage that needs to be deleted
	// Returns zed token and file id
	GetMapFileById(ctx context.Context, id string) (string, string, error) // Deprecated
}
