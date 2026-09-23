package postgres_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/emount4/poidem-back/internal/account"
	accountpostgres "github.com/emount4/poidem-back/internal/account/postgres"
	platformpostgres "github.com/emount4/poidem-back/internal/platform/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestConcurrentOAuthLoginCreatesOneAccount(t *testing.T) {
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

	providerUserID := fmt.Sprintf("oauth-integration-%d", time.Now().UnixNano())
	repository := accountpostgres.NewRepository(pool)
	service, err := account.NewOAuthLoginService(repository, platformpostgres.NewTxManager(pool), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	identity := account.ExternalIdentity{Provider: "google", ProviderUserID: providerUserID, FirstName: "Test"}

	errorsByLogin := make(chan error, 2)
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := service.Login(ctx, identity)
			errorsByLogin <- err
		}()
	}
	wait.Wait()
	close(errorsByLogin)
	for err := range errorsByLogin {
		if err != nil {
			t.Fatal(err)
		}
	}

	var userID int64
	if err := pool.QueryRow(ctx, `
		SELECT user_id FROM auth_accounts WHERE provider = $1 AND provider_user_id = $2
	`, identity.Provider, identity.ProviderUserID).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM auth_accounts WHERE user_id = $1`, userID)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID)
	}()

	var accountCount, sessionCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM auth_accounts WHERE provider = $1 AND provider_user_id = $2`, identity.Provider, identity.ProviderUserID).Scan(&accountCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE user_id = $1`, userID).Scan(&sessionCount); err != nil {
		t.Fatal(err)
	}
	if accountCount != 1 || sessionCount != 2 {
		t.Fatalf("accounts = %d, sessions = %d", accountCount, sessionCount)
	}
}
