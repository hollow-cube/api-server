package tracefx

import (
	http2 "net/http"

	"github.com/go-chi/chi/v5"
	"github.com/hollow-cube/hc-services/libraries/common/pkg/common"
	"github.com/hollow-cube/hc-services/libraries/common/pkg/http"
	"github.com/hollow-cube/hc-services/libraries/common/pkg/httpfx"
	"github.com/hollow-cube/hc-services/libraries/common/pkg/otelchi"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/fx"
)

var Module = fx.Module("tracing", fx.Provide(
	NewResource,
	NewTraceProvider,

	httpfx.AsMiddlewareProvider(NewChiMiddleware),
))

type TraceProviderParams struct {
	fx.In

	Service common.ServiceConfig

	Exporter sdktrace.SpanExporter
	Resource *resource.Resource
}

func NewTraceProvider(params TraceProviderParams) (trace.TracerProvider, trace.Tracer) {
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(params.Exporter),
		sdktrace.WithResource(params.Resource),
	)
	otel.SetTracerProvider(tp)

	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{}))

	return tp, tp.Tracer(params.Service.Name)
}

func NewChiMiddleware(_ trace.TracerProvider) http.MiddlewareProviderFunc {
	// Add the trace provider here to force it to load, we don't actually need it.
	return func(r chi.Router) http.Middleware {
		return http.MiddlewareFunc(otelchi.Middleware(
			"my-server",
			otelchi.WithChiRoutes(r),
			otelchi.WithFilter(func(r *http2.Request) bool {
				return r.URL.Path != "/alive" && r.URL.Path != "/ready" && r.URL.Path != "/metrics"
			})))
	}
}
