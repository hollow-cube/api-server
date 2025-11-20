package tracefx

import (
	"context"
	"io"

	"github.com/hollow-cube/hc-services/libraries/common/pkg/common"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/trace"
)

func NewHttpExporter(config common.OtlpConfig) (trace.SpanExporter, error) {
	return otlptracehttp.New(
		context.Background(),
		otlptracehttp.WithInsecure(),
		otlptracehttp.WithEndpoint(config.Endpoint),
	)
}

func NewNoopExporter() (trace.SpanExporter, error) {
	return stdouttrace.New(
		stdouttrace.WithWriter(io.Discard),
	)
}
