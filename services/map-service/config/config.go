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

type SpiceDB struct {
	Endpoint string `mapstructure:"endpoint"`
	Token    string `mapstructure:"token"`
	TLS      bool   `mapstructure:"tls"`
}

type S3 struct {
	Endpoint   string `mapstructure:"endpoint"`
	Region     string `mapstructure:"region"`
	AccessKey  string `mapstructure:"access_key"`
	SecretKey  string `mapstructure:"secret_key"`
	MapsBucket string `mapstructure:"maps_bucket"`
}

type Redis struct {
	Address string `mapstructure:"address"`
}

type Posthog struct {
	Endpoint       string `mapstructure:"endpoint"`
	PersonalApiKey string `mapstructure:"personal_api_key"` // Required for feature flags
}

type Config struct {
	Env              string             `mapstructure:"env"`
	HTTP             common.HTTPConfig  `mapstructure:"http"`
	Metrics          Metrics            `mapstructure:"metrics"`
	PlayerServiceUrl string             `mapstructure:"player_service_url"`
	Postgres         Postgres           `mapstructure:"postgres"`
	SpiceDB          SpiceDB            `mapstructure:"spicedb"`
	S3               S3                 `mapstructure:"s3"`
	Redis            Redis              `mapstructure:"redis"`
	Kafka            common.KafkaConfig `mapstructure:"kafka"`
	OTLP             common.OtlpConfig  `mapstructure:"otlp"`
	Posthog          Posthog            `mapstructure:"posthog"`
}

//go:embed default.yaml
var defaultConfig string

func NewMergedConfig() *Config {
	viper.SetConfigType("yaml")
	viper.SetEnvPrefix("MAPS")
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
