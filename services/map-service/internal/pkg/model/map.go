package model

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/hollow-cube/hc-services/services/map-service/internal/db"
)

// MapmakerSpawnMapId is the hardcoded Id of the spawn map in the dev server for now
const MapmakerSpawnMapId = "b210fa07-d64c-4100-a2db-1426c35b7533"

type MapType string

const (
	TypeDefault MapType = "default"
	TypeLegacy  MapType = "legacy"
	TypeOrg     MapType = "org"
)

type Verification int

const (
	VerificationUnverified Verification = iota
	VerificationPending
	VerificationVerified
)

type Map struct {
	Id              string
	Owner           string
	Type            MapType
	CreatedAt       time.Time
	UpdatedAt       time.Time
	ProtocolVersion int
	Verification    Verification
	AuthzKey        string // SpiceDB ZedToken if using spicedb authz
	Settings        MapSettings
	Contest         *string // The map contest this map was published as part of, or nil

	MapFileId   string
	LegacyMapId string // Present only for imported omega maps

	PublishedId *int64
	PublishedAt *time.Time
	Listed      bool // Whether the map is shown in search results

	Likes       int     // Present only for published maps
	UniquePlays int     // Present only for published maps
	ClearRate   float64 // Present only for published maps

	QualityOverride int8
	Difficulty2     MapDifficulty

	MapExt // Extra data, stored in json blob
}

// Deprecated (v3+ should use Difficulty2)
func (m *Map) Difficulty() MapDifficulty {
	if m.UniquePlays < 10 {
		return MapDifficultyUnknown
	}

	if m.ClearRate < 0.05 {
		return MapDifficultyNightmare
	}
	if m.ClearRate < 0.25 {
		return MapDifficultyExpert
	}
	if m.ClearRate < 0.5 {
		return MapDifficultyHard
	}
	if m.ClearRate < 0.75 {
		return MapDifficultyMedium
	}
	return MapDifficultyEasy
}

type MapExt struct {
	Objects map[string]*ObjectData `json:"objects"` // Objects by ID
}

type ObjectData struct {
	Id   string                 `json:"id"`
	Type string                 `json:"type"`
	Pos  Point                  `json:"pos"`
	Data map[string]interface{} `json:"data"`
}

type MapVariant string

const (
	Parkour   MapVariant = "parkour"
	Building  MapVariant = "building"
	Adventure MapVariant = "adventure"
)

var MapVariantValidationMap = map[MapVariant]struct{}{
	Parkour:   {},
	Building:  {},
	Adventure: {},
}

type MapSubVariant string

const (
	// SubVariantNone is a marker to represent setting the value to null
	SubVariantNone MapSubVariant = "none"

	// Parkour
	ParkourSpeedrun    MapSubVariant = "speedrun"
	ParkourSectioned   MapSubVariant = "sectioned"
	ParkourRankup      MapSubVariant = "rankup"
	ParkourGauntlet    MapSubVariant = "gauntlet"
	ParkourDropper     MapSubVariant = "dropper"
	ParkourOneJump     MapSubVariant = "one_jump"
	ParkourInformative MapSubVariant = "informative"

	// Building
	BuildingShowcase MapSubVariant = "showcase"
	BuildingTutorial MapSubVariant = "tutorial"
)

var MapSubVariantTypeMap = map[MapSubVariant]MapVariant{
	ParkourSpeedrun:    Parkour,
	ParkourSectioned:   Parkour,
	ParkourRankup:      Parkour,
	ParkourGauntlet:    Parkour,
	ParkourDropper:     Parkour,
	ParkourOneJump:     Parkour,
	ParkourInformative: Parkour,

	BuildingShowcase: Building,
	BuildingTutorial: Building,
}

type MapSize int

const (
	MapSizeUnlimitedOldNotReal = iota - 1
	MapSizeNormal
	MapSizeLarge
	MapSizeMassive
	MapSizeColossal
	MapSizeUnlimited
	MapSizeTall2k
	MapSizeTall4k

	MapSize__Max = MapSizeTall4k
)

var MapSizeValidationMap = map[MapSize]bool{
	MapSizeUnlimited: true, // Only valid in some cases, but tested separately
	MapSizeNormal:    true,
	MapSizeLarge:     true,
	MapSizeMassive:   true,
	MapSizeTall2k:    true,
	MapSizeTall4k:    true,
}

type MapDifficulty int

const (
	MapDifficultyUnknown MapDifficulty = iota - 1
	MapDifficultyEasy
	MapDifficultyMedium
	MapDifficultyHard
	MapDifficultyExpert
	MapDifficultyNightmare
)

func (d MapDifficulty) String() string {
	switch d {
	case MapDifficultyEasy:
		return "easy"
	case MapDifficultyMedium:
		return "medium"
	case MapDifficultyHard:
		return "hard"
	case MapDifficultyExpert:
		return "expert"
	case MapDifficultyNightmare:
		return "nightmare"
	default:
		return "unknown"
	}
}

type MapSettings struct {
	Name string `json:"name,omitempty"`
	Icon string `json:"icon,omitempty"`
	Size int    `json:"size,omitempty"`

	Variant    MapVariant     `json:"variant"`
	SubVariant *MapSubVariant `json:"sub_variant"`

	SpawnPoint Pos `json:"spawn_point"`

	// Gameplay settings
	OnlySprint bool                   `json:"only_sprint"`
	NoSprint   bool                   `json:"no_sprint"`
	NoJump     bool                   `json:"no_jump"`
	NoSneak    bool                   `json:"no_sneak"`
	Boat       bool                   `json:"boat"`
	Extra      map[string]interface{} `json:"extra"` // Unstructured settings (never queryable)

	Tags []string `json:"tags"`
}

type MapUpdateAction int

const (
	MapUpdate_Create MapUpdateAction = iota
	MapUpdate_Delete
	MapUpdate_Drain
)

func (a MapUpdateAction) String() string {
	switch a {
	case MapUpdate_Create:
		return "create"
	case MapUpdate_Delete:
		return "delete"
	case MapUpdate_Drain:
		return "drain"
	default:
		return "unknown"
	}
}

type MapUpdateMessage struct {
	Action MapUpdateAction `json:"action"`
	ID     string          `json:"id"`
}

func (m MapUpdateMessage) Subject() string {
	return fmt.Sprintf("map.%s", m.Action.String())
}

type RatingState int

const (
	RatingStateUnrated RatingState = iota
	RatingStateLiked
	RatingStateDisliked

	RatingState__Min = RatingStateUnrated
	RatingState__Max = RatingStateDisliked
)

type MapRating struct {
	MapId    string
	PlayerId string
	Rating   RatingState
	Comment  *string
}

type LeaderboardEntry struct {
	PlayerId string
	Rank     int
	Score    int
}

type MapReport struct {
	MapId      string
	PlayerId   string
	Timestamp  time.Time
	Categories []int
	Comment    *string
}

const (
	MapReportCheated         = 0
	MapReportDiscrimination  = 1
	MapReportExplicitContent = 2
	MapReportSpam            = 3
	MapReportDCMA            = 4
	MapReportTroll           = 5
	MapReportUnplayable      = 6
)

var ReportCategoryNameMap = []string{
	"Cheated Verification", "Discrimination",
	"Inappropriate/Explicit Content",
	"Spam", "Troll Map", "Crashes/Unloadable",
}

var ReportCategoriesToDislike = map[int]bool{
	MapReportDiscrimination:  true,
	MapReportExplicitContent: true,
	MapReportSpam:            true,
	MapReportTroll:           true,
	MapReportDCMA:            true,
}

type MapSortOrder = string

const (
	MapSortAsc  MapSortOrder = "asc"
	MapSortDesc MapSortOrder = "desc"
)

type MapSortType = string

const (
	MapSortBest      MapSortType = "best"
	MapSortPublished MapSortType = "published"
	MapSortRandom    MapSortType = "random"
)

type MapIdAndProgress struct {
	MapId    string
	Progress int
	Playtime int
}

func CreateDefaultMap(owner string, size int) (db.CreateMapParams, error) {
	var m db.CreateMapParams
	m.ID = uuid.NewString()
	m.Owner = owner
	m.MType = string(TypeDefault)

	if size > MapSize__Max {
		return m, fmt.Errorf("invalid map size: %d", size)
	}
	m.Size = int64(size)
	m.OptVariant = string(Parkour)
	m.OptSpawnPoint = db.Pos{0, 40, 0, 90, 0}

	return m, nil
}
