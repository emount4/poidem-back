package postgres

import (
	"context"
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
}
