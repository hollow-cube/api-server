package model

// Events should be named like [object][verb]Event. So MapCreatedEvent

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
	PlayerId   string `mapstructure:"player_id"`
	MapId      string `mapstructure:"map_id"`
	Variant    string `mapstructure:"variant"`
	SubVariant string `mapstructure:"subvariant"`
	Playtime   int    `mapstructure:"playtime"`
	Difficulty string `mapstructure:"difficulty"`
}

type MapReportedEvent struct {
	PlayerId   string   `mapstructure:"player_id"`
	MapId      string   `mapstructure:"map_id"`
	Categories []string `mapstructure:"reason"`
	Comment    *string  `mapstructure:"comment"`
}
