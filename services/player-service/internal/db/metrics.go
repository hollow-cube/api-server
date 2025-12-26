package db

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
