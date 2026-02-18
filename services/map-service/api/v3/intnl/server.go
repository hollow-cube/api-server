//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen -o server.gen.go -package intnl -generate types,strict-server,std-http-server openapi.yaml
//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen -o client.gen.go -package intnl -generate client openapi.yaml

package intnl

import (
	"context"
	"time"

	"github.com/hollow-cube/hc-services/libraries/common/pkg/kafkafx"
	"github.com/hollow-cube/hc-services/libraries/common/pkg/metric"
	"github.com/hollow-cube/hc-services/libraries/common/pkg/natsutil"
	"github.com/hollow-cube/hc-services/services/map-service/internal/db"
	"github.com/hollow-cube/hc-services/services/map-service/internal/pkg/authz"
	"github.com/hollow-cube/hc-services/services/map-service/internal/pkg/object"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/redis/rueidis"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

var _ StrictServerInterface = (*server)(nil)

type ServerParams struct {
	fx.In

	Log *zap.SugaredLogger

	Store     *db.Store
	Authz     authz.Client
	Redis     rueidis.Client
	Producer  kafkafx.SyncProducer
	JetStream *natsutil.JetStreamWrapper
	Metrics   metric.Writer

	Object object.Client `name:"object-mapmaker"`
}

type server struct {
	log *zap.SugaredLogger

	store       *db.Store
	authzClient authz.Client
	redis       rueidis.Client
	producer    kafkafx.SyncProducer
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
		authzClient:  params.Authz,
		redis:        params.Redis,
		producer:     params.Producer,
		jetStream:    params.JetStream,
		metrics:      params.Metrics,
		objectClient: params.Object,
	}, nil
}
