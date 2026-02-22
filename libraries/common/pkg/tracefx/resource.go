package tracefx

import (
	"os"

	"github.com/hollow-cube/hc-services/libraries/common/pkg/common"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.39.0"
)

func NewResource(service common.ServiceConfig) (*resource.Resource, error) {
	hostname, _ := os.Hostname()
	return resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(service.Name),
			semconv.ServiceInstanceID(hostname),
		),
	)
}
