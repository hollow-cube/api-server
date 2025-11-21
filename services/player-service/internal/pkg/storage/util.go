package storage

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"text/template"

	sq "github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Scanner[T any] interface {
	*T
	Scan() (ptrs []any)
}

func queryFunc[T any](ctx context.Context, db *pgxpool.Pool, binder func(*T) []any, query string, args ...interface{}) ([]*T, error) {
	rows, err := db.Query(ctx, query, args...)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []*T
	for rows.Next() {
		var result T
		if err = rows.Scan(binder(&result)...); err != nil {
			return nil, err
		}
		results = append(results, &result)
	}

	return results, nil
}

func queryFunc2[T any](ctx context.Context, db *pgxpool.Pool, binder func(*T) []any, query sq.Sqlizer) ([]*T, error) {
	sql, args, err := query.ToSql()
	if err != nil {
		return nil, err
	}

	rows, err := db.Query(ctx, sql, args...)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []*T
	for rows.Next() {
		var result T
		if err = rows.Scan(binder(&result)...); err != nil {
			return nil, err
		}
		results = append(results, &result)
	}

	return results, nil
}

func querySingleFunc[T any](ctx context.Context, db *pgxpool.Pool, binder func(*T) []any, query string, args ...interface{}) (*T, error) {
	result, err := queryFunc[T](ctx, db, binder, query, args...)
	if err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return nil, ErrNotFound
	}
	return result[0], nil
}

func querySingleFunc2[T any](ctx context.Context, db *pgxpool.Pool, binder func(*T) []any, query sq.Sqlizer) (*T, error) {
	sql, args, err := query.ToSql()
	if err != nil {
		return nil, err
	}

	result, err := queryFunc[T](ctx, db, binder, sql, args...)
	if err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return nil, ErrNotFound
	}
	return result[0], nil
}

func (c *PostgresClient) templateQuery(query string, args any) string {
	c.hash.Reset()
	_, _ = c.hash.Write([]byte(query))
	hash := int(c.hash.Sum32())

	var err error
	tmpl, ok := c.templates[hash]
	if !ok {
		tmpl, err = template.New(fmt.Sprintf("query_%d", hash)).Parse(query)
		if err != nil {
			panic(err)
		}
		c.templates[hash] = tmpl
	}

	var sb strings.Builder
	if err = tmpl.Execute(&sb, args); err != nil {
		panic(err)
	}
	return sb.String()
}

type whereClauseBuilder struct {
	params []string
}

func (w *whereClauseBuilder) add(param string) {
	w.params = append(w.params, param)
}

func (w *whereClauseBuilder) build() string {
	if len(w.params) == 0 {
		return ""
	}

	result := strings.Builder{}
	first := true
	for i, param := range w.params {
		if param == "" {
			continue
		}

		if first {
			first = false
			result.WriteString("WHERE ")
		} else {
			result.WriteString(" AND ")
		}
		result.WriteString(param)
		result.WriteString(" = $")
		result.WriteString(strconv.Itoa(i + 1))
	}

	return result.String()
}
