package postgres

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/emount4/poidem-back/internal/account"
	platformpostgres "github.com/emount4/poidem-back/internal/platform/postgres"
	"github.com/jackc/pgx/v5"
)

func (r *Repository) FindOrCreateOAuthUser(ctx context.Context, identity account.ExternalIdentity) (int64, error) {
	db := platformpostgres.Executor(ctx, r.pool)
	lockKey := strconv.Itoa(len(identity.Provider)) + ":" + identity.Provider + identity.ProviderUserID
	if _, err := db.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, lockKey); err != nil {
		return 0, fmt.Errorf("lock OAuth account: %w", err)
	}

	var userID int64
	err := db.QueryRow(ctx, `
		SELECT user_id
		FROM auth_accounts
		WHERE provider = $1 AND provider_user_id = $2
	`, identity.Provider, identity.ProviderUserID).Scan(&userID)
	if err == nil {
		return userID, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("query OAuth account: %w", err)
	}

	err = db.QueryRow(ctx, `
		INSERT INTO users (first_name, last_name, avatar_url, role, status, created_at, updated_at)
		VALUES ($1, NULLIF($2, ''), NULLIF($3, ''), $4, $5, now(), now())
		RETURNING id
	`, identity.FirstName, identity.LastName, identity.AvatarURL, account.RoleUser, account.StatusActive).Scan(&userID)
	if err != nil {
		return 0, fmt.Errorf("insert OAuth user: %w", err)
	}
	if _, err := db.Exec(ctx, `
		INSERT INTO auth_accounts (user_id, provider, provider_user_id, created_at)
		VALUES ($1, $2, $3, now())
	`, userID, identity.Provider, identity.ProviderUserID); err != nil {
		return 0, fmt.Errorf("insert OAuth account: %w", err)
	}
	return userID, nil
}
