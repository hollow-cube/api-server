package model

import (
	"fmt"

	"github.com/google/uuid"
	mapdb2 "github.com/hollow-cube/api-server/internal/mapdb"
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

func CreateDefaultMap(owner string, size int) (mapdb2.CreateMapParams, error) {
	var m mapdb2.CreateMapParams
	m.ID = uuid.NewString()
	m.Owner = owner
	m.MType = string(TypeDefault)

	if size > MapSize__Max {
		return m, fmt.Errorf("invalid map size: %d", size)
	}
	m.Size = int64(size)
	m.OptVariant = string(Parkour)
	m.OptSpawnPoint = mapdb2.Pos{0, 40, 0, 90, 0}

	return m, nil
}
