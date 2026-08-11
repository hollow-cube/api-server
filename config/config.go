package config

import (
	"bytes"
	_ "embed"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/hollow-cube/api-server/internal/pkg/common"
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

type NATS struct {
	Servers string `mapstructure:"servers"`
}

type Kubernetes struct {
	Namespace string `mapstructure:"namespace"`
}

type Postgres struct {
	URI        string `mapstructure:"uri"`
	PlayersURI string `mapstructure:"players_uri"`
	MapsURI    string `mapstructure:"maps_uri"`
}

type Posthog struct {
	Endpoint       string `mapstructure:"endpoint"`
	PersonalApiKey string `mapstructure:"personal_api_key"` // Required for feature flags
}

type Unleash struct {
	Address string `mapstructure:"address"`
	Token   string `mapstructure:"token"`
}

type Tebex struct {
	PrivateKey            string `mapstructure:"private_key"`
	Secret                string `mapstructure:"secret"`
	DisputeDiscordWebhook string `mapstructure:"dispute_discord_webhook"`
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

type Discord struct {
	ApplicationID string `mapstructure:"application_id"`
	PublicKey     string `mapstructure:"public_key"`
	Token         string `mapstructure:"token"`
}

type S3 struct {
	Endpoint   string `mapstructure:"endpoint"`
	Region     string `mapstructure:"region"`
	AccessKey  string `mapstructure:"access_key"`
	SecretKey  string `mapstructure:"secret_key"`
	MapsBucket string `mapstructure:"maps_bucket"`
}

// DefaultMaxInlineSegmentBytes is the default replay segment inline threshold
// (replay.max_inline_segment_bytes). What keeps a segment in the heap row is
// postgres' TOAST threshold of roughly 2 KiB, not the 8 KiB page size: a larger
// value is pushed out-of-line into the TOAST table anyway, which costs a
// separate write and a chunk per 1996 bytes without ever being cheaper than
// object storage. Zero disables inlining entirely; segments above the threshold
// keep going to object storage.
const DefaultMaxInlineSegmentBytes = 2 * 1024

type Replay struct {
	MaxPreambleBytes      int64 `mapstructure:"max_preamble_bytes"`
	MaxSegmentBytes       int64 `mapstructure:"max_segment_bytes"`
	MaxCommitBytes        int64 `mapstructure:"max_commit_bytes"`
	MaxCompactedBytes     int64 `mapstructure:"max_compacted_bytes"`
	MaxInlineSegmentBytes int64 `mapstructure:"max_inline_segment_bytes"`
}

type MapIsolate struct {
	Disabled  bool `mapstructure:"disabled"`
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

type Keyring struct {
	ActiveKeyID int `mapstructure:"active_key_id"`
	// "<id>:<base64-nopad>[,<id>:<base64-nopad>...]"
	Keys string `mapstructure:"keys"`
}

// AuthClientLifetime is the per-client-kind session expiry policy.
type AuthClientLifetime struct {
	IdleTTL     time.Duration `mapstructure:"idle_ttl"`
	AbsoluteTTL time.Duration `mapstructure:"absolute_ttl"`
}

type Auth struct {
	// ExternalURL is the public origin clients reach the API through (Envoy).
	// Used to reconstruct the DPoP htu, since Envoy's internal view differs.
	ExternalURL    string             `mapstructure:"external_url"`
	AccessTokenTTL time.Duration      `mapstructure:"access_token_ttl"`
	Web            AuthClientLifetime `mapstructure:"web"`
	Desktop        AuthClientLifetime `mapstructure:"desktop"`
}

// Lifetime returns the session policy for a client kind, defaulting to web.
func (a Auth) Lifetime(kind string) AuthClientLifetime {
	if kind == "desktop" {
		return a.Desktop
	}
	return a.Web
}

type Config struct {
	Env               string             `mapstructure:"env"`
	HTTP              common.HTTPConfig  `mapstructure:"http"`
	Metrics           Metrics            `mapstructure:"metrics"`
	OTLP              common.OtlpConfig  `mapstructure:"otlp"`
	Redis             Redis              `mapstructure:"redis"`
	NATS              NATS               `mapstructure:"nats"`
	Kubernetes        Kubernetes         `mapstructure:"kubernetes"`
	Postgres          Postgres           `mapstructure:"postgres"`
	Posthog           Posthog            `mapstructure:"posthog"`
	Unleash           Unleash            `mapstructure:"unleash"`
	MapIsolate        MapIsolate         `mapstructure:"map_isolate"`
	Github            Github             `mapstructure:"github"`
	PunishmentLadders []PunishmentLadder `mapstructure:"punishment_ladders"`
	Tebex             Tebex              `mapstructure:"tebex"`
	S3                S3                 `mapstructure:"s3"`
	Replay            Replay             `mapstructure:"replay"`
	Discord           Discord            `mapstructure:"discord"`
	Keyring           Keyring            `mapstructure:"keyring"`
	Auth              Auth               `mapstructure:"auth"`
	EditorURL         string             `mapstructure:"editor_url"`
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
