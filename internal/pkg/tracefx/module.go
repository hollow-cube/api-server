package tracefx

import (
	"github.com/hollow-cube/hc-services/services/session-service/internal/pkg/common"
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
