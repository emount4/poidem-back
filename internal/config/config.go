// Package config loads application configuration.
package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	Postgres Postgres
}

type Postgres struct {
	Host     string
	Port     uint16
	User     string
	Password string
	Database string
	SSLMode  string
}

// Load reads a .env file without overriding existing environment variables.
// An empty path skips the file and uses the process environment (e.g. in Docker).
func Load(path string) (*Config, error) {
	if path != "" {
		if err := godotenv.Load(path); err != nil {
			return nil, fmt.Errorf("load config %q: %w", path, err)
		}
	}
	port, err := strconv.ParseUint(env("POSTGRES_PORT", "5432"), 10, 16)
	if err != nil || port == 0 {
		return nil, fmt.Errorf("POSTGRES_PORT must be between 1 and 65535")
	}
	cfg := &Config{Postgres: Postgres{
		Host:     env("POSTGRES_HOST", "localhost"),
		Port:     uint16(port),
		User:     env("POSTGRES_USER", "poidem"),
		Password: os.Getenv("POSTGRES_PASSWORD"),
		Database: env("POSTGRES_DB", "poidem"),
		SSLMode:  env("POSTGRES_SSLMODE", "disable"),
	}}
	if cfg.Postgres.Password == "" {
		return nil, fmt.Errorf("POSTGRES_PASSWORD is required")
	}
	switch cfg.Postgres.SSLMode {
	case "disable", "allow", "prefer", "require", "verify-ca", "verify-full":
	default:
		return nil, fmt.Errorf("POSTGRES_SSLMODE is invalid")
	}
	return cfg, nil
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
