//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen -o server.gen.go -package intnl -generate types,strict-server,std-http-server openapi.yaml
//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen -o client.gen.go -package intnl -generate client openapi.yaml

package intnl

import (
	"github.com/hollow-cube/hc-services/libraries/common/pkg/metric"
	"github.com/hollow-cube/hc-services/services/map/internal/pkg/authz"
	"github.com/hollow-cube/hc-services/services/map/internal/pkg/object"
	"github.com/hollow-cube/hc-services/services/map/internal/pkg/storage"
	"github.com/hollow-cube/hc-services/services/map/internal/pkg/wkafka"
	"github.com/redis/rueidis"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

var _ StrictServerInterface = (*server)(nil)

type ServerParams struct {
	fx.In

	Log *zap.SugaredLogger

	Storage  storage.Client
	Authz    authz.Client
	Redis    rueidis.Client
	Producer wkafka.SyncWriter
	Metrics  metric.Writer

	Object object.Client `name:"object-mapmaker"`
}

func NewServer(params ServerParams) StrictServerInterface {
	return &server{
		log:           params.Log,
		storageClient: params.Storage,
		authzClient:   params.Authz,
		redis:         params.Redis,
		producer:      params.Producer,
		metrics:       params.Metrics,
		objectClient:  params.Object,
	}
}

type server struct {
	log *zap.SugaredLogger

	storageClient storage.Client
	authzClient   authz.Client
	redis         rueidis.Client
	producer      wkafka.SyncWriter
	metrics       metric.Writer

	objectClient object.Client
}
