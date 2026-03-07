package v4Internal

import (
	"encoding/json"
	"time"

	"github.com/hollow-cube/api-server/internal/mapdb"
	"github.com/hollow-cube/api-server/internal/pkg/model"
	"github.com/hollow-cube/api-server/internal/pkg/util"
)

type MapData struct {
	ID              string      `json:"id"`
	CreatedAt       time.Time   `json:"createdAt"`
	LastModified    time.Time   `json:"lastModified"`
	Owner           string      `json:"owner"`
	Contest         *string     `json:"contest,omitempty"`
	ProtocolVersion int         `json:"protocolVersion"`
	Settings        MapSettings `json:"settings"`

	Verification MapVerification `json:"verification"`

	PublishedAt *time.Time    `json:"publishedAt,omitempty"`
	PublishedId *int          `json:"publishedId,omitempty"`
	Quality     MapQuality    `json:"quality"`
	Difficulty  MapDifficulty `json:"difficulty"`
	ClearRate   float64       `json:"clearRate"`
	UniquePlays int           `json:"uniquePlays"`
	Likes       int           `json:"likes"`
	Listed      bool          `json:"listed"`
}

type MapDifficulty string

const (
	DifficultyUnknown   MapDifficulty = "unknown"
	DifficultyEasy      MapDifficulty = "easy"
	DifficultyMedium    MapDifficulty = "medium"
	DifficultyHard      MapDifficulty = "hard"
	DifficultyExpert    MapDifficulty = "expert"
	DifficultyNightmare MapDifficulty = "nightmare"
)

type MapQuality string

const (
	QualityUnrated     MapQuality = "unrated"
	QualityGood        MapQuality = "good"
	QualityGreat       MapQuality = "great"
	QualityExcellent   MapQuality = "excellent"
	QualityOutstanding MapQuality = "outstanding"
	QualityMasterpiece MapQuality = "masterpiece"
)

type MapVerification string

const (
	VerificationPending    MapVerification = "pending"
	VerificationUnverified MapVerification = "unverified"
	VerificationVerified   MapVerification = "verified"
)

type MapSettings struct {
	Icon       string  `json:"icon"` // Minecraft item ID, eg minecraft:stone
	Name       string  `json:"name"`
	Size       MapSize `json:"size"`
	SpawnPoint Pos     `json:"spawnPoint"`

	Variant    MapVariant `json:"variant"`
	Subvariant *string    `json:"subvariant"`
	Tags       []string   `json:"tags"`

	Extra map[string]interface{} `json:"extra"`
}

type MapSize string

const (
	SizeNormal    MapSize = "normal"
	SizeLarge     MapSize = "large"
	SizeMassive   MapSize = "massive"
	SizeColossal  MapSize = "colossal"
	SizeUnlimited MapSize = "unlimited"
)

type MapVariant string

const (
	VariantParkour  MapVariant = "parkour"
	VariantBuilding MapVariant = "building"
)

type MapSlot struct {
	Map       MapData   `json:"map"`
	CreatedAt time.Time `json:"createdAt"`
}

func hydrateMap(m mapdb.Map, tags []mapdb.MapTag) MapData {
	extra := make(map[string]interface{})
	if m.OptExtra != nil {
		_ = json.Unmarshal(m.OptExtra, &extra)
	}
	if m.OptOnlySprint != nil && *m.OptOnlySprint {
		extra["only_sprint"] = true
	}
	if m.OptNoSprint != nil && *m.OptNoSprint {
		extra["no_sprint"] = true
	}
	if m.OptNoJump != nil && *m.OptNoJump {
		extra["no_jump"] = true
	}
	if m.OptNoSneak != nil && *m.OptNoSneak {
		extra["no_sneak"] = true
	}
	if m.OptBoat != nil && *m.OptBoat {
		extra["boat"] = true
	}

	apiTags := make([]string, len(tags))
	for i, tag := range tags {
		apiTags[i] = string(tag)
	}

	return MapData{
		ID:              m.ID,
		Owner:           m.Owner,
		CreatedAt:       m.CreatedAt,
		LastModified:    m.UpdatedAt,
		ProtocolVersion: *m.ProtocolVersion, // todo shouldnt be nullable in db

		Verification: hydrateMapVerification(m.Verification),
		Settings: MapSettings{
			Name:       util.NilToEmpty(m.OptName), // todo should not be optional in db
			Icon:       util.NilToEmpty(m.OptIcon), // todo should not be optional in db
			Size:       hydrateMapSize(m.Size),
			Variant:    hydrateMapVariant(m.OptVariant),
			Subvariant: m.OptSubvariant,
			Tags:       apiTags,
			SpawnPoint: hydratePos(m.OptSpawnPoint),
			Extra:      extra,
		},

		PublishedId: m.PublishedID,
		PublishedAt: m.PublishedAt,
		Listed:      m.Listed,

		Quality:    hydrateMapQuality(m.QualityOverride),
		Difficulty: DifficultyUnknown,

		Contest: m.Contest,
	}
}

func hydratePublishedMap(m mapdb.PublishedMap) MapData {
	extra := make(map[string]interface{})
	if m.OptExtra != nil {
		_ = json.Unmarshal(m.OptExtra, &extra)
	}
	if m.OptOnlySprint != nil && *m.OptOnlySprint {
		extra["only_sprint"] = true
	}
	if m.OptNoSprint != nil && *m.OptNoSprint {
		extra["no_sprint"] = true
	}
	if m.OptNoJump != nil && *m.OptNoJump {
		extra["no_jump"] = true
	}
	if m.OptNoSneak != nil && *m.OptNoSneak {
		extra["no_sneak"] = true
	}
	if m.OptBoat != nil && *m.OptBoat {
		extra["boat"] = true
	}

	return MapData{
		ID:              m.ID,
		Owner:           m.Owner,
		CreatedAt:       m.CreatedAt,
		LastModified:    m.UpdatedAt,
		ProtocolVersion: *m.ProtocolVersion, // todo shouldnt be nullable in db

		Verification: hydrateMapVerification(m.Verification),
		Settings: MapSettings{
			Name:       util.NilToEmpty(m.OptName), // todo should not be optional in db
			Icon:       util.NilToEmpty(m.OptIcon), // todo should not be optional in db
			Size:       hydrateMapSize(m.Size),
			Variant:    hydrateMapVariant(m.OptVariant),
			Subvariant: m.OptSubvariant,
			Tags:       m.OptTags,
			SpawnPoint: hydratePos(m.OptSpawnPoint),
			Extra:      extra,
		},

		PublishedId: m.PublishedID,
		PublishedAt: m.PublishedAt,
		Listed:      m.Listed,

		Quality:     hydrateMapQuality(m.QualityOverride),
		Difficulty:  hydrateDifficulty(m.Difficulty),
		UniquePlays: m.PlayCount,
		ClearRate:   m.ClearRate,
		Likes:       m.TotalLikes,

		Contest: m.Contest,
	}
}

func hydrateMapVerification(verification *int64) MapVerification {
	if verification == nil {
		return VerificationUnverified
	}
	switch *verification {
	case int64(model.VerificationPending):
		return VerificationPending
	case int64(model.VerificationVerified):
		return VerificationVerified
	default:
		return VerificationUnverified
	}
}

func hydrateMapQuality(quality *int64) MapQuality {
	if quality == nil {
		return QualityUnrated
	}
	switch *quality {
	case 1:
		return QualityGood
	case 2:
		return QualityGreat
	case 3:
		return QualityExcellent
	case 4:
		return QualityOutstanding
	case 5:
		return QualityMasterpiece
	default:
		return QualityUnrated
	}
}

func hydrateDifficulty(difficulty int32) MapDifficulty {
	switch int(difficulty) {
	case 0:
		return DifficultyEasy
	case 1:
		return DifficultyMedium
	case 2:
		return DifficultyHard
	case 3:
		return DifficultyExpert
	case 4:
		return DifficultyNightmare
	default:
		return DifficultyUnknown
	}
}

func hydrateMapSize(size int64) MapSize {
	switch size {
	case model.MapSizeNormal:
		return SizeNormal
	case model.MapSizeLarge:
		return SizeLarge
	case model.MapSizeMassive:
		return SizeMassive
	case model.MapSizeColossal:
		return SizeColossal
	case model.MapSizeUnlimited:
		return SizeUnlimited
	default:
		return SizeNormal
	}
}

func hydrateMapVariant(variant string) MapVariant {
	return MapVariant(variant)
}
