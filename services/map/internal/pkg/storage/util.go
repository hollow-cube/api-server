package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"text/template"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// {28663bbb-478c-441d-bda6-7fbee7a9e62f,20ab02a6-6474-4726-9d32-78150e7d145b,0ae5bde2-205d-493f-8db5-4d7abb04db46,7c1d979c-3660-40fa-a7e3-acf9417411ac,f012192b-df92-4cc5-9ffa-b52a50adf471}

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
