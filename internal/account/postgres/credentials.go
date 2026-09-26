package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/emount4/poidem-back/internal/account"
	platformpostgres "github.com/emount4/poidem-back/internal/platform/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func (r *Repository) CreateCredentialUser(ctx context.Context, username, passwordHash string) (int64, error) {
	var userID int64
	err := platformpostgres.Executor(ctx, r.pool).QueryRow(ctx, `
		INSERT INTO users (
			first_name, username, password_hash, role, status, created_at, updated_at
		) VALUES ('', $1, $2, $3, $4, now(), now())
		RETURNING id
	`, username, passwordHash, account.RoleUser, account.StatusActive).Scan(&userID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.ConstraintName == "uq_users_username_ci" {
			return 0, account.ErrUsernameTaken
		}
		return 0, fmt.Errorf("insert credential user: %w", err)
	}
	return userID, nil
}

func (r *Repository) CredentialUser(ctx context.Context, username string) (account.CredentialUser, error) {
	var user account.CredentialUser
	err := platformpostgres.Executor(ctx, r.pool).QueryRow(ctx, `
		SELECT id, password_hash, COALESCE(status, 'active')
		FROM users
		WHERE username = $1 AND password_hash IS NOT NULL
		FOR UPDATE
	`, username).Scan(&user.ID, &user.PasswordHash, &user.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return account.CredentialUser{}, account.ErrUserNotFound
	}
	if err != nil {
		return account.CredentialUser{}, fmt.Errorf("query credential user: %w", err)
	}
	return user, nil
}
