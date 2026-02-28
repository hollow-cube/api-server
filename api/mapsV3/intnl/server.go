//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen -o server.gen.go -package intnl -generate types,strict-server,std-http-server openapi.yaml

package intnl

import (
	"context"
	"time"

	"github.com/hollow-cube/hc-services/services/session-service/internal/mapdb"
	"github.com/hollow-cube/hc-services/services/session-service/internal/object"
	"github.com/hollow-cube/hc-services/services/session-service/internal/pkg/metric"
	"github.com/hollow-cube/hc-services/services/session-service/internal/pkg/natsutil"
	"github.com/hollow-cube/hc-services/services/session-service/internal/playerdb"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/redis/rueidis"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

var _ StrictServerInterface = (*server)(nil)

type ServerParams struct {
	fx.In

	Log *zap.SugaredLogger

	Store       *mapdb.Store
	PlayerStore *playerdb.Store
	Redis       rueidis.Client
	JetStream   *natsutil.JetStreamWrapper
	Metrics     metric.Writer

	Object object.Client `name:"object-mapmaker"`
}

type server struct {
	log *zap.SugaredLogger

	store       *mapdb.Store
	playerStore *playerdb.Store
	redis       rueidis.Client
	jetStream   *natsutil.JetStreamWrapper
	metrics     metric.Writer

	objectClient object.Client
}

func NewServer(params ServerParams) (StrictServerInterface, error) {
	err := params.JetStream.UpsertStream(context.Background(), jetstream.StreamConfig{
		Name:       "MAP_MANAGEMENT",
		Subjects:   []string{"map.>"},
		Retention:  jetstream.LimitsPolicy,
		Storage:    jetstream.FileStorage,
		MaxAge:     5 * time.Minute,
		Duplicates: time.Minute,
	})
	if err != nil {
		return nil, err
	}

	return &server{
		log:          params.Log,
		store:        params.Store,
		playerStore:  params.PlayerStore,
		redis:        params.Redis,
		jetStream:    params.JetStream,
		metrics:      params.Metrics,
		objectClient: params.Object,
	}, nil
}
