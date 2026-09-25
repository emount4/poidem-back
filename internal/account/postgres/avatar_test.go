package postgres_test

import (
	"context"
	"os"
	"testing"

	accountpostgres "github.com/emount4/poidem-back/internal/account/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestReplaceAndClearAvatarIntegration(t *testing.T) {
	dsn := os.Getenv("POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("POSTGRES_TEST_DSN is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var userID int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO users (first_name, role, status, created_at, updated_at)
		VALUES ('Avatar test', 'user', 'active', now(), now()) RETURNING id
	`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID) })

	repository := accountpostgres.NewRepository(pool)
	oldKey, err := repository.ReplaceAvatar(ctx, userID, "http://media/new.png", "avatars/new.png")
	if err != nil || oldKey != nil {
		t.Fatalf("first replace old=%v err=%v", oldKey, err)
	}
	oldKey, err = repository.ReplaceAvatar(ctx, userID, "http://media/newer.webp", "avatars/newer.webp")
	if err != nil || oldKey == nil || *oldKey != "avatars/new.png" {
		t.Fatalf("second replace old=%v err=%v", oldKey, err)
	}
	oldKey, err = repository.ClearAvatar(ctx, userID)
	if err != nil || oldKey == nil || *oldKey != "avatars/newer.webp" {
		t.Fatalf("clear old=%v err=%v", oldKey, err)
	}
	var url, key *string
	if err := pool.QueryRow(ctx, `SELECT avatar_url, avatar_object_key FROM users WHERE id = $1`, userID).Scan(&url, &key); err != nil {
		t.Fatal(err)
	}
	if url != nil || key != nil {
		t.Fatalf("avatar was not cleared: url=%v key=%v", url, key)
	}
}
