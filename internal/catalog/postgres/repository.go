// Package postgres implements the catalog repository using PostgreSQL.
package postgres

import (
	"context"
	"fmt"

	"github.com/emount4/poidem-back/internal/catalog"
	platformpostgres "github.com/emount4/poidem-back/internal/platform/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Cities(ctx context.Context) ([]catalog.Item, error) {
	return queryDictionary(ctx, platformpostgres.Executor(ctx, r.pool), `SELECT id, name, slug FROM cities ORDER BY name, id`)
}

func (r *Repository) Interests(ctx context.Context) ([]catalog.Item, error) {
	return queryDictionary(ctx, platformpostgres.Executor(ctx, r.pool), `SELECT id, name, slug FROM interests ORDER BY name, id`)
}

func (r *Repository) EventCategories(ctx context.Context) ([]catalog.Item, error) {
	return queryDictionary(ctx, platformpostgres.Executor(ctx, r.pool), `SELECT id, name, slug FROM event_categories ORDER BY name, id`)
}

type queryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func queryDictionary(ctx context.Context, db queryer, query string) ([]catalog.Item, error) {
	rows, err := db.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query dictionary: %w", err)
	}
	items, err := pgx.CollectRows(rows, pgx.RowToStructByPos[catalog.Item])
	if err != nil {
		return nil, fmt.Errorf("scan dictionary: %w", err)
	}
	if items == nil {
		items = []catalog.Item{}
	}
	return items, nil
}
