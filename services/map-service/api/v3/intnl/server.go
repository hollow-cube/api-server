//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen -o server.gen.go -package intnl -generate types,strict-server,std-http-server openapi.yaml
//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen -o client.gen.go -package intnl -generate client openapi.yaml

package intnl

import (
	"github.com/hollow-cube/hc-services/libraries/common/pkg/metric"
	"github.com/hollow-cube/hc-services/services/map-service/internal/db"
	"github.com/hollow-cube/hc-services/services/map-service/internal/pkg/authz"
	"github.com/hollow-cube/hc-services/services/map-service/internal/pkg/object"
	"github.com/hollow-cube/hc-services/services/map-service/internal/pkg/storage"
	"github.com/hollow-cube/hc-services/services/map-service/internal/pkg/wkafka"
	"github.com/redis/rueidis"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

var _ StrictServerInterface = (*server)(nil)

type ServerParams struct {
	fx.In

	Log *zap.SugaredLogger

	Storage  storage.Client
	Queries  *db.Queries
	Authz    authz.Client
	Redis    rueidis.Client
	Producer wkafka.SyncWriter
	Metrics  metric.Writer

	Object object.Client `name:"object-mapmaker"`
}

type server struct {
	log *zap.SugaredLogger

	storageClient storage.Client
	queries       *db.Queries
	authzClient   authz.Client
	redis         rueidis.Client
	producer      wkafka.SyncWriter
	metrics       metric.Writer

	objectClient object.Client
}

func NewServer(params ServerParams) StrictServerInterface {
	return &server{
		log:           params.Log,
		storageClient: params.Storage,
		queries:       params.Queries,
		authzClient:   params.Authz,
		redis:         params.Redis,
		producer:      params.Producer,
		metrics:       params.Metrics,
		objectClient:  params.Object,
	}
}
