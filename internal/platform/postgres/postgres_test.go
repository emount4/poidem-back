package postgres

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/emount4/poidem-back/internal/platform/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestConnectionStringEscaping(t *testing.T) {
	cfg := config.Postgres{
		Host: "::1", Port: 5432, User: "user@name", Password: "p@ss:/?#% word",
		Database: "db name", SSLMode: "disable",
	}
	parsed, err := pgxpool.ParseConfig(connectionString(cfg))
	if err != nil {
		t.Fatal("could not parse escaped connection string")
	}
	conn := parsed.ConnConfig
	if conn.Host != cfg.Host || conn.Port != cfg.Port || conn.User != cfg.User || conn.Password != cfg.Password || conn.Database != cfg.Database {
		t.Fatal("connection parameters changed after URL encoding")
	}
}

func TestIntegration(t *testing.T) {
	dsn := os.Getenv("POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set POSTGRES_TEST_DSN to run against a real PostgreSQL database")
	}
	parsed, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal("invalid POSTGRES_TEST_DSN")
	}
	c := parsed.ConnConfig
	sslMode := "require"
	if c.TLSConfig == nil {
		sslMode = "disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := New(ctx, config.Postgres{
		Host: c.Host, Port: c.Port, User: c.User, Password: c.Password,
		Database: c.Database, SSLMode: sslMode,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var value int
	if err := pool.QueryRow(ctx, "SELECT 1").Scan(&value); err != nil {
		t.Fatal(err)
	}
	if value != 1 {
		t.Fatalf("SELECT 1 returned %d", value)
	}

	manager := NewTxManager(pool)
	const table = "tx_manager_integration_test"
	_, _ = pool.Exec(ctx, "DROP TABLE IF EXISTS "+table)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DROP TABLE IF EXISTS "+table) })

	rollbackCause := errors.New("rollback requested")
	err = manager.WithinTransaction(ctx, func(txCtx context.Context) error {
		if _, err := Executor(txCtx, pool).Exec(txCtx, "CREATE TABLE "+table+" (id INT)"); err != nil {
			return err
		}
		return rollbackCause
	})
	if !errors.Is(err, rollbackCause) {
		t.Fatalf("rollback error = %v", err)
	}
	var exists bool
	if err := pool.QueryRow(ctx, "SELECT to_regclass('public."+table+"') IS NOT NULL").Scan(&exists); err != nil || exists {
		t.Fatalf("rolled back table exists=%v, error=%v", exists, err)
	}

	err = manager.WithinTransaction(ctx, func(txCtx context.Context) error {
		_, err := Executor(txCtx, pool).Exec(txCtx, "CREATE TABLE "+table+" (id INT)")
		return err
	})
	if err != nil {
		t.Fatalf("commit transaction: %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT to_regclass('public."+table+"') IS NOT NULL").Scan(&exists); err != nil || !exists {
		t.Fatalf("committed table exists=%v, error=%v", exists, err)
	}

	err = manager.WithinTransaction(ctx, func(txCtx context.Context) error {
		return manager.WithinTransaction(txCtx, func(context.Context) error { return nil })
	})
	if !errors.Is(err, ErrNestedTransaction) {
		t.Fatalf("nested transaction error = %v", err)
	}
}
