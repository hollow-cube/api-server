package v1

import (
	"encoding/json"
	"time"
)

type MapCreateRequest struct {
	IsOrg bool   `json:"isOrg"`
	Owner string `json:"owner"`
	Size  int    `json:"size"`
	Slot  *int   `json:"slot"`
}

type MapVariant string

const (
	MapVariantParkour  MapVariant = "parkour"
	MapVariantBuilding MapVariant = "building"
)

type MapSettings struct {
	Name       string                 `json:"name"`
	Icon       string                 `json:"icon"`
	Size       int                    `json:"size"`
	Variant    MapVariant             `json:"variant"`
	Subvariant *string                `json:"subvariant"`
	SpawnPoint *Pos                   `json:"spawnPoint"`
	OnlySprint bool                   `json:"onlySprint"`
	NoSprint   bool                   `json:"noSprint"`
	NoJump     bool                   `json:"noJump"`
	NoSneak    bool                   `json:"noSneak"`
	Boat       bool                   `json:"boat"`
	Extra      map[string]interface{} `json:"extra"`
	Tags       []*string              `json:"tags"`
}

type MapDataVerification int

const (
	MapDataVerificationUnverified MapDataVerification = 0
	MapDataVerificationPending    MapDataVerification = 1
	MapDataVerificationVerified   MapDataVerification = 2
)

type MapQuality int

const (
	MapQualityUnrated     MapQuality = 0
	MapQualityGood        MapQuality = 1
	MapQualityGreat       MapQuality = 2
	MapQualityExcellent   MapQuality = 3
	MapQualityOutstanding MapQuality = 4
	MapQualityMasterpiece MapQuality = 5
)

type ObjectData struct {
	Id   string                 `json:"id"`
	Type string                 `json:"type"`
	Pos  *Point                 `json:"pos"`
	Data map[string]interface{} `json:"data"`
}

type MapData struct {
	Id           string              `json:"id"`
	Owner        string              `json:"owner"`
	CreatedAt    time.Time           `json:"createdAt"`
	LastModified time.Time           `json:"lastModified"`
	Settings     *MapSettings        `json:"settings"`
	Verification MapDataVerification `json:"verification"`
	PublishedId  *int64              `json:"publishedId"`
	PublishedAt  *time.Time          `json:"publishedAt"`
	UniquePlays  int                 `json:"uniquePlays"`
	ClearRate    float64             `json:"clearRate"`
	Likes        int                 `json:"likes"`
	Quality      MapQuality          `json:"quality"`
	Objects      []*ObjectData       `json:"objects"`
}

type SearchMapsParams struct {
	Sort     *string `json:"sort"`
	Page     *string `json:"page"`
	PageSize *string `json:"pageSize"`
	Owner    *string `json:"owner"`
	Parkour  *string `json:"parkour"`
	Building *string `json:"building"`
	Query    *string `json:"query"`
}

type MapProgress int

const (
	MapProgressNone     MapProgress = 0
	MapProgressStarted  MapProgress = 1
	MapProgressComplete MapProgress = 2
)

type PersonalizedMapData struct {
	MapData
	Progress MapProgress `json:"progress"`
}

type SearchMapsResponse struct {
	Page     int                    `json:"page"`
	NextPage bool                   `json:"nextPage"`
	Results  []*PersonalizedMapData `json:"results"`
}

type SearchOrgMapsParams struct {
	Page     *string `json:"page"`
	PageSize *string `json:"pageSize"`
	OrgId    string  `json:"orgId"`
}

type SearchOrgMapsResponse struct {
	Page     int        `json:"page"`
	NextPage bool       `json:"nextPage"`
	Results  []*MapData `json:"results"`
}

type DeleteMapRequest struct {
	Reason *string `json:"reason"`
}

type MapUpdateRequest struct {
	Name            *string                `json:"name"`
	Icon            *string                `json:"icon"`
	Size            *int                   `json:"size"`
	Variant         ***MapVariant          `json:"variant"`
	Subvariant      *string                `json:"subvariant"`
	SpawnPoint      *Pos                   `json:"spawnPoint"`
	OnlySprint      *bool                  `json:"onlySprint"`
	NoSprint        *bool                  `json:"noSprint"`
	NoJump          *bool                  `json:"noJump"`
	NoSneak         *bool                  `json:"noSneak"`
	Extra           map[string]interface{} `json:"extra"`
	Tags            []*string              `json:"tags"`
	NewObjects      []*ObjectData          `json:"newObjects"`
	RemovedObjects  []*string              `json:"removedObjects"`
	QualityOverride ***MapQuality          `json:"qualityOverride"`
}

type MapWorldData struct {
	Polar   []byte `json:"Polar"`
	Anvil   []byte `json:"Anvil"`
	Anvil18 []byte `json:"Anvil18"`
}

type MapReportCategory int

const (
	MapReportCategoryCheated         MapReportCategory = 0
	MapReportCategoryDiscrimination  MapReportCategory = 1
	MapReportCategoryExplicitContent MapReportCategory = 2
	MapReportCategorySpam            MapReportCategory = 3
	MapReportCategoryTroll           MapReportCategory = 4
)

type MapReport struct {
	Reporter   string                 `json:"reporter"`
	Categories []**MapReportCategory  `json:"categories"`
	Comment    *string                `json:"comment"`
	Location   *Pos                   `json:"location"`
	Context    map[string]interface{} `json:"context"`
}

type LeaderboardEntry struct {
	Player string `json:"player"`
	Score  int    `json:"score"`
	Rank   int    `json:"rank"`
}

type LeaderboardData struct {
	Top    []*LeaderboardEntry `json:"top"`
	Player *LeaderboardEntry   `json:"player"`
}

type SaveStateType string

const (
	SaveStateTypeEditing   SaveStateType = "editing"
	SaveStateTypePlaying   SaveStateType = "playing"
	SaveStateTypeVerifying SaveStateType = "verifying"
)

type SaveState struct {
	Id           string                 `json:"id"`
	PlayerId     string                 `json:"playerId"`
	MapId        string                 `json:"mapId"`
	Type         SaveStateType          `json:"type"`
	Created      time.Time              `json:"created"`
	LastModified time.Time              `json:"lastModified"`
	Completed    bool                   `json:"completed"`
	Playtime     *int                   `json:"playtime"`
	DataVersion  int                    `json:"dataVersion"`
	EditState    map[string]interface{} `json:"editState"`
	PlayState    map[string]interface{} `json:"playState"`
}

type SaveStateUpdateRequest struct {
	Completed   *bool                  `json:"completed"`
	Playtime    *int                   `json:"playtime"`
	DataVersion *int                   `json:"dataVersion"`
	EditState   map[string]interface{} `json:"editState"`
	PlayState   map[string]interface{} `json:"playState"`
}

type SaveStateUpdateResponse struct {
	Rewards      *json.RawMessage `json:"rewards"`
	NewPlacement *int             `json:"newPlacement"`
}

type RatingState int

const (
	RatingStateUnrated  RatingState = 0
	RatingStateLiked    RatingState = 1
	RatingStateDisliked RatingState = 2
)

type MapRating struct {
	State   RatingState `json:"state"`
	Comment *string     `json:"comment"`
}

type MapPlayerData struct {
	Id            string    `json:"id"`
	UnlockedSlots int       `json:"unlockedSlots"`
	MapSlots      []*string `json:"mapSlots"`
	LastPlayedMap *string   `json:"lastPlayedMap"`
	LastEditedMap *string   `json:"lastEditedMap"`
}

type GetLegacyMapsResponseItem struct {
	Id   string `json:"id"`
	Name string `json:"name"`
}

type GetLegacyMapsResponse []*GetLegacyMapsResponseItem

type MapWithSlot struct {
	MapData
	Slot int `json:"slot"`
}

// Point defines model for Point.
type Point struct {
	X float32 `json:"x"`
	Y float32 `json:"y"`
	Z float32 `json:"z"`
}

// Pos defines model for Pos.
type Pos struct {
	Pitch float32 `json:"pitch"`
	X     float32 `json:"x"`
	Y     float32 `json:"y"`
	Yaw   float32 `json:"yaw"`
	Z     float32 `json:"z"`
}
