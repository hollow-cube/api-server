package tracefx

import (
	"context"
	"io"

	"github.com/hollow-cube/api-server/internal/pkg/common"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/trace"
)

func NewHttpExporter(config common.OtlpConfig) (trace.SpanExporter, error) {
	if config.Endpoint == "" {
		return NewNoopExporter()
	}
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
