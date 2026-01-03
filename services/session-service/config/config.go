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

type HTTP struct {
	Address string `mapstructure:"address"`
	Port    int    `mapstructure:"port"`
}

type Metrics struct {
	Password string `mapstructure:"password"`
}

type Redis struct {
	Address string `mapstructure:"address"`
}

type Kubernetes struct {
	Namespace string `mapstructure:"namespace"`
}

type Postgres struct {
	URI string `mapstructure:"uri"`
}

type SpiceDB struct {
	Endpoint string `mapstructure:"endpoint"`
	Token    string `mapstructure:"token"`
	TLS      bool   `mapstructure:"tls"`
}

type Posthog struct {
	Endpoint       string `mapstructure:"endpoint"`
	PersonalApiKey string `mapstructure:"personal_api_key"` // Required for feature flags
}

type Unleash struct {
	Address string `mapstructure:"address"`
	Token   string `mapstructure:"token"`
}

type MapIsolate struct {
	Instances map[string]struct {
		Cpu    int `mapstructure:"cpu"`    // eg 1000m, 0 = no limit
		Memory int `mapstructure:"memory"` // eg 512Mi, required.
	} `mapstructure:"instances"`
	// Should correspond to map sizes, lower cased.
	WorldSizeMapping map[string]string `mapstructure:"world_size_mapping"`
	// Used for unknown
	DefaultSize string `mapstructure:"default_size"`
}

type Github struct {
	PrivateKey string `mapstructure:"private_key"`
}

type Config struct {
	Env              string             `mapstructure:"env"`
	HTTP             common.HTTPConfig  `mapstructure:"http"`
	Metrics          Metrics            `mapstructure:"metrics"`
	OTLP             common.OtlpConfig  `mapstructure:"otlp"`
	PlayerServiceUrl string             `mapstructure:"player_service_url"`
	MapServiceUrl    string             `mapstructure:"map_service_url"`
	Redis            Redis              `mapstructure:"redis"`
	Kafka            common.KafkaConfig `mapstructure:"kafka"`
	Kubernetes       Kubernetes         `mapstructure:"kubernetes"`
	Postgres         Postgres           `mapstructure:"postgres"`
	SpiceDB          SpiceDB            `mapstructure:"spicedb"`
	Posthog          Posthog            `mapstructure:"posthog"`
	Unleash          Unleash            `mapstructure:"unleash"`
	MapIsolate       MapIsolate         `mapstructure:"map_isolate"`
	Github           Github             `mapstructure:"github"`
}

//go:embed default.yaml
var defaultConfig string

func NewMergedConfig() *Config {
	viper.SetConfigType("yaml")
	viper.SetEnvPrefix("SESSION")
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
