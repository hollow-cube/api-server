package tracefx

import (
	"context"

	"go.opentelemetry.io/otel/trace"
)

// TransferTraceCtx transfers trace context values from one context to another
// This may be used in cases where you asynchronously continue a task that can't depend on the parent context
// as it gets canceled.
func TransferTraceCtx(fromCtx, toCtx context.Context) context.Context {
	spanCtx := trace.SpanContextFromContext(fromCtx)
	if spanCtx.IsValid() {
		toCtx = trace.ContextWithRemoteSpanContext(toCtx, spanCtx)
	}
	return toCtx
}

func NewCtxWithTraceCtx(ctx context.Context) context.Context {
	return TransferTraceCtx(ctx, context.Background())
}
