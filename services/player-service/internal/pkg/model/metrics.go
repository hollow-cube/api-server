package model

type NewPlayer struct {
	PlayerId string `mapstructure:"player_id"`
}

type CoinBalanceChanged struct {
	PlayerId string `mapstructure:"player_id"`
	Delta    int    `mapstructure:"delta"`
	NewValue int    `mapstructure:"new_value"`
}

type CubitBalanceChanged struct {
	PlayerId string `mapstructure:"player_id"`
	Delta    int    `mapstructure:"delta"`
	NewValue int    `mapstructure:"new_value"`
}

type ExpChanged struct {
	PlayerId string `mapstructure:"player_id"`
	Delta    int    `mapstructure:"delta"`
	NewValue int    `mapstructure:"new_value"`
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
