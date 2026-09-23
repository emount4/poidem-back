package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/emount4/poidem-back/internal/account"
	platformpostgres "github.com/emount4/poidem-back/internal/platform/postgres"
	"github.com/jackc/pgx/v5"
)

func (r *Repository) CreateSession(
	ctx context.Context,
	userID int64,
	tokenHash []byte,
	expiresAt time.Time,
) (account.Session, error) {
	var session account.Session
	err := platformpostgres.Executor(ctx, r.pool).QueryRow(ctx, `
		INSERT INTO sessions (user_id, token_hash, expires_at)
		VALUES ($1, $2, $3)
		RETURNING id, user_id, expires_at, revoked_at
	`, userID, tokenHash, expiresAt).Scan(
		&session.ID, &session.UserID, &session.ExpiresAt, &session.RevokedAt,
	)
	if err != nil {
		return account.Session{}, fmt.Errorf("insert session: %w", err)
	}
	return session, nil
}

func (r *Repository) RotateSession(
	ctx context.Context,
	presentedHash, nextHash []byte,
	now, previousValidUntil time.Time,
) (account.Session, error) {
	var session account.Session
	err := r.transactions.WithinTransaction(ctx, func(txCtx context.Context) error {
		db := platformpostgres.Executor(txCtx, r.pool)
		err := db.QueryRow(txCtx, `
		SELECT id, user_id, expires_at, revoked_at
		FROM sessions
		WHERE token_hash = $1
		   OR (previous_token_hash = $1 AND previous_valid_until >= $2)
		FOR UPDATE
	`, presentedHash, now).Scan(
			&session.ID, &session.UserID, &session.ExpiresAt, &session.RevokedAt,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			return account.ErrSessionNotFound
		}
		if err != nil {
			return fmt.Errorf("lock session: %w", err)
		}
		if session.RevokedAt != nil || !session.ExpiresAt.After(now) {
			return account.ErrSessionNotFound
		}
		_, err = db.Exec(txCtx, `
		UPDATE sessions
		SET previous_token_hash = token_hash,
		    previous_valid_until = $2,
		    token_hash = $3,
		    updated_at = $4
		WHERE id = $1
	`, session.ID, previousValidUntil, nextHash, now)
		if err != nil {
			return fmt.Errorf("rotate session: %w", err)
		}
		return nil
	})
	if err != nil {
		return account.Session{}, err
	}
	return session, nil
}

func (r *Repository) RevokeSession(ctx context.Context, tokenHash []byte, now time.Time) error {
	_, err := platformpostgres.Executor(ctx, r.pool).Exec(ctx, `
		UPDATE sessions
		SET revoked_at = $2, updated_at = $2
		WHERE (token_hash = $1 OR previous_token_hash = $1) AND revoked_at IS NULL
	`, tokenHash, now)
	if err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	return nil
}

func (r *Repository) RevokeUserSessions(ctx context.Context, userID int64, now time.Time) error {
	_, err := platformpostgres.Executor(ctx, r.pool).Exec(ctx, `
		UPDATE sessions
		SET revoked_at = $2, updated_at = $2
		WHERE user_id = $1 AND revoked_at IS NULL
	`, userID, now)
	if err != nil {
		return fmt.Errorf("revoke user sessions: %w", err)
	}
	return nil
}
