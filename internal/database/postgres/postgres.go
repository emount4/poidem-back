// Package postgres manages the PostgreSQL connection pool.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"time"

	"github.com/emount4/poidem-back/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

// New opens a pool and verifies the connection before returning it.
// The caller must close the pool after all database users have stopped.
func New(ctx context.Context, cfg config.Postgres) (*pgxpool.Pool, error) {
	poolConfig, err := pgxpool.ParseConfig(connectionString(cfg))
	if err != nil {
		// Parse errors may contain the connection string, including the password.
		return nil, errors.New("parse PostgreSQL connection configuration")
	}
	poolConfig.MaxConns = 10
	poolConfig.ConnConfig.ConnectTimeout = 5 * time.Second
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("create PostgreSQL pool: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping PostgreSQL: %w", err)
	}
	return pool, nil
}

func connectionString(cfg config.Postgres) string {
	u := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(cfg.User, cfg.Password),
		Host:   net.JoinHostPort(cfg.Host, strconv.Itoa(int(cfg.Port))),
		Path:   "/" + cfg.Database,
	}
	query := url.Values{}
	query.Set("sslmode", cfg.SSLMode)
	u.RawQuery = query.Encode()
	return u.String()
}
