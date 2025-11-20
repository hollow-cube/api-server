package dbTracing

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Context key for span
type contextKey string

const spanKey contextKey = "db_span"

var (
	_ pgx.QueryTracer = (*Tracer)(nil)

	otelTracer = otel.Tracer("github.com/hollow-cube/hc-services/services/session/internal/db")
)

type Tracer struct {
}

func (t *Tracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	spanName := extractOperationName(data.SQL)

	ctx, span := otelTracer.Start(ctx, spanName)
	span.SetAttributes(attribute.String("db.query", data.SQL))
	return context.WithValue(ctx, spanKey, span)
}

func (t *Tracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	span := ctx.Value(spanKey).(trace.Span)
	if span == nil {
		return
	}
	defer span.End()

	// in the future we might want a more complex system for determining if it's an error.
	// e.g., you might purposefully allow index collisions instead of pre-checking duplications, then just handling the error.
	if data.Err != nil && !errors.Is(data.Err, pgx.ErrNoRows) {
		span.RecordError(data.Err)
		span.SetStatus(codes.Error, data.Err.Error())
	} else {
		span.SetStatus(codes.Ok, "")
		span.SetAttributes(
			attribute.Int64("db.rows_affected", data.CommandTag.RowsAffected()),
		)
	}
}

func extractOperationName(sql string) string {
	if !strings.HasPrefix(sql, "-- name:") {
		return "db.query"
	}

	lines := strings.Split(sql, "\n")
	if len(lines) == 0 {
		return "db.query"
	}

	parts := strings.Fields(lines[0])
	if len(parts) < 3 {
		return "db.query"
	}
	// parts[0] = --
	// parts[1] = name:
	// parts[2] = operation name, e.g. ListTimedOutPlayers
	// parts[3] = :many, :one
	return "db." + parts[2]
}
