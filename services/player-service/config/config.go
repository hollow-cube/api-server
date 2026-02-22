package config

import (
	"bytes"
	_ "embed"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/hollow-cube/hc-services/libraries/common/pkg/common"
	"github.com/spf13/viper"
)

type Metrics struct {
	Password string `mapstructure:"password"`
}

type Postgres struct {
	URI string `mapstructure:"uri"`
}

type NATS struct {
	Servers string `mapstructure:"servers"`
}

type Tebex struct {
	PrivateKey            string `mapstructure:"private_key"`
	Secret                string `mapstructure:"secret"`
	DisputeDiscordWebhook string `mapstructure:"dispute_discord_webhook"`
}

type Votifier struct {
	ListenAddr string `mapstructure:"listen_addr"`
	Token      string `mapstructure:"token"`
}

type Posthog struct {
	Endpoint       string `mapstructure:"endpoint"`
	PersonalApiKey string `mapstructure:"personal_api_key"` // Required for feature flags
}

type Unleash struct {
	Address string `mapstructure:"address"`
	Token   string `mapstructure:"token"`
}

type PunishmentLadder struct {
	Id      string `mapstructure:"id"`
	Name    string `mapstructure:"name"`
	Type    string `mapstructure:"type"`
	Entries []struct {
		Duration string `mapstructure:"duration"`
	} `mapstructure:"entries"`
	Reasons []struct {
		Id      string   `mapstructure:"id"`
		Aliases []string `mapstructure:"aliases"`
	} `mapstructure:"reasons"`
}

type Redis struct {
	Address string `mapstructure:"address"`
}

type Config struct {
	Env               string             `mapstructure:"env"`
	HTTP              common.HTTPConfig  `mapstructure:"http"`
	Metrics           Metrics            `mapstructure:"metrics"`
	Postgres          Postgres           `mapstructure:"postgres"`
	NATS              NATS               `mapstructure:"nats"`
	Tebex             Tebex              `mapstructure:"tebex"`
	Votifier          Votifier           `mapstructure:"votifier"`
	OTLP              common.OtlpConfig  `mapstructure:"otlp"`
	Posthog           Posthog            `mapstructure:"posthog"`
	Unleash           Unleash            `mapstructure:"unleash"`
	PunishmentLadders []PunishmentLadder `mapstructure:"punishment_ladders"`
	Redis             Redis              `mapstructure:"redis"`
}

//go:embed default.yaml
var defaultConfig string

func NewMergedConfig() *Config {
	viper.SetConfigType("yaml")
	viper.ReadInConfig()
	viper.SetEnvPrefix("PLAYERS")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()
	err := viper.ReadConfig(bytes.NewBuffer([]byte(defaultConfig)))
	if err != nil {
		log.Fatal(fmt.Sprintf("failed to read config: %s", err))
	}

	// Read injected vault secrets and apply them
	var data []byte
	if data, err = os.ReadFile("/vault/secrets/service"); err != nil {
		log.Printf("failed to read vault secrets: %s (this is probably ok)", err)
	} else {
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) != 2 {
				continue
			}
			viper.Set(strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]))
		}
	}

	var conf Config
	if err = viper.Unmarshal(&conf); err != nil {
		log.Fatal(fmt.Sprintf("failed to unmarshal config: %s", err))
	}

	return &conf
}
