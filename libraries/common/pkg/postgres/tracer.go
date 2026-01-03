package postgresUtil

import (
	"context"
	"errors"
	"fmt"
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
	_ pgx.QueryTracer = (*tracer)(nil)

	otelTracer = otel.Tracer("github.com/hollow-cube/hc-services/libraries/common/pkg/postgres")
)

type Tracer interface {
	pgx.QueryTracer
}

type tracer struct {
	nameExtractor func(sql string) string
}

func NewSqlCTracer() Tracer {
	return &tracer{extractSqlCOperationName}
}

func NewSQLTracer() Tracer {
	return &tracer{extractPlainSqlOperationName}
}

func (t *tracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	spanName := t.nameExtractor(data.SQL)

	ctx, span := otelTracer.Start(ctx, spanName)
	span.SetAttributes(
		attribute.String("db.query", data.SQL),
		attribute.String("db.query.args", fmt.Sprintf("%v", data.Args)),
	)
	return context.WithValue(ctx, spanKey, span)
}

func (t *tracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
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

func extractSqlCOperationName(sql string) string {
	if !strings.HasPrefix(sql, "-- name:") {
		switch sql {
		case "begin", "commit", "rollback":
			return "db.tx." + sql
		default:
			return "db.query.unknown." + sql
		}
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

func extractPlainSqlOperationName(sql string) string {
	trimmed := strings.TrimSpace(sql)
	lower := strings.ToLower(trimmed)

	// Extract the first word (the SQL command: update, delete, etc..)
	parts := strings.Fields(lower)
	if len(parts) == 0 {
		return "db.query"
	}

	operation := parts[0]

	// Try to extract table name based on the operation type
	var tableName string

	switch operation {
	case "select":
		// Look for "FROM table_name"
		tableName = extractTableAfterKeyword(lower, "from")
	case "insert":
		// Look for "INSERT INTO table_name"
		tableName = extractTableAfterKeyword(lower, "into")
	case "update":
		// Look for "UPDATE table_name"
		if len(parts) > 1 {
			tableName = cleanTableName(parts[1])
		}
	case "delete":
		// Look for "DELETE FROM table_name"
		tableName = extractTableAfterKeyword(lower, "from")
	case "with":
		return "db.query" // Too much effort to support for now - we should move everything to SQLC eventually.
	default:
		// For other operations (CREATE, DROP, ALTER, etc.). In theory, we won't perform any of these but... who knows...
		operation = "query"
	}

	// Build the span name
	if tableName != "" {
		return "db." + operation + "." + tableName
	}
	return "db." + operation
}

// extractTableAfterKeyword finds the table name after a specific keyword (e.g., "from", "into")
func extractTableAfterKeyword(sql, keyword string) string {
	idx := strings.Index(sql, keyword)
	if idx == -1 {
		return ""
	}

	// Get everything after the keyword
	afterKeyword := sql[idx+len(keyword):]
	parts := strings.Fields(afterKeyword)
	if len(parts) == 0 {
		return ""
	}

	return cleanTableName(parts[0])
}

// cleanTableName removes common SQL syntax characters from table names
func cleanTableName(name string) string {
	// Remove parentheses, semicolons, commas, etc.
	name = strings.TrimFunc(name, func(r rune) bool {
		return r == '(' || r == ')' || r == ';' || r == ',' || r == '"' || r == '\''
	})

	// Handle schema.table format - just take the table name
	if idx := strings.LastIndex(name, "."); idx != -1 {
		name = name[idx+1:]
	}

	return name
}
