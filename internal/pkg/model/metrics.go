package model

// Events should be named like [object][verb]Event. So MapCreatedEvent

type NewPlayer struct {
	PlayerId string `mapstructure:"player_id"`
}

type BackpackEntryChanged struct {
	PlayerId string `mapstructure:"player_id"`
	Item     string `mapstructure:"item"`
	Delta    int    `mapstructure:"delta"`
	NewValue int    `mapstructure:"new_value"`
}

type CosmeticUnlocked struct {
	PlayerId string `mapstructure:"player_id"`
	Cosmetic string `mapstructure:"cosmetic"`
}

type PlayerBanned struct {
	PlayerId   string `mapstructure:"player_id"`
	ExecutorId string `mapstructure:"executor_id"`
	LadderId   string `mapstructure:"ladder_id"`
}

type PlayerMuted struct {
	PlayerId   string `mapstructure:"player_id"`
	ExecutorId string `mapstructure:"executor_id"`
	LadderId   string `mapstructure:"ladder_id"`
}

type PlayerKicked struct {
	PlayerId   string `mapstructure:"player_id"`
	ExecutorId string `mapstructure:"executor_id"`
}

type PlayerUnbanned struct {
	PlayerId  string `mapstructure:"player_id"`
	RevokerId string `mapstructure:"revoker_id"`
}

type PlayerUnmuted struct {
	PlayerId  string `mapstructure:"player_id"`
	RevokerId string `mapstructure:"revoker_id"`
}

type MapCreatedEvent struct {
	PlayerId string  `mapstructure:"player_id"`
	Contest  *string `mapstructure:"contest"`
}

type MapPublishedEvent struct {
	PlayerId       string  `mapstructure:"player_id"`
	MapId          string  `mapstructure:"map_id"`
	PublishedMapId int     `mapstructure:"published_map_id"`
	MapName        *string `mapstructure:"map_name"`
	Variant        string  `mapstructure:"variant"`
	SubVariant     string  `mapstructure:"subvariant"`
	WorldDataSize  int     `mapstructure:"world_data_size"`
	OwnerBuildTime int     `mapstructure:"owner_build_time"`
	Contest        *string `mapstructure:"contest"`
}

type MapImportedEvent struct {
	PlayerId string `mapstructure:"player_id"`
	Format   string `mapstructure:"format"`
}

type MapCompletedEvent struct {
	PlayerId   string   `mapstructure:"player_id"`
	MapId      string   `mapstructure:"map_id"`
	Variant    string   `mapstructure:"variant"`
	SubVariant string   `mapstructure:"subvariant"`
	Playtime   int      `mapstructure:"playtime"`
	Score      *float64 `mapstructure:"score"`
	Difficulty string   `mapstructure:"difficulty"`
}

type MapReportedEvent struct {
	PlayerId   string   `mapstructure:"player_id"`
	MapId      string   `mapstructure:"map_id"`
	MapName    string   `mapstructure:"map_name"`
	Categories []string `mapstructure:"reason"`
	Comment    *string  `mapstructure:"comment"`
}
