// Package postgres implements account persistence using PostgreSQL.
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/emount4/poidem-back/internal/account"
	platformpostgres "github.com/emount4/poidem-back/internal/platform/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool         *pgxpool.Pool
	transactions *platformpostgres.TxManager
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool, transactions: platformpostgres.NewTxManager(pool)}
}

func (r *Repository) AccessState(ctx context.Context, userID int64) (account.AccessState, error) {
	var state account.AccessState
	err := platformpostgres.Executor(ctx, r.pool).QueryRow(ctx, `
		SELECT id, first_name, city_id, COALESCE(role, ''), COALESCE(status, '')
		FROM users
		WHERE id = $1
	`, userID).Scan(&state.UserID, &state.FirstName, &state.CityID, &state.Role, &state.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return account.AccessState{}, account.ErrUserNotFound
	}
	if err != nil {
		return account.AccessState{}, fmt.Errorf("query user access state: %w", err)
	}
	return state, nil
}
