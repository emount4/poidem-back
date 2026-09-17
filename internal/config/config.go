// Package config loads application configuration.
package config

import (
	"fmt"

	"github.com/joho/godotenv"
)

// Config will contain typed settings as application features are added.
type Config struct{}

// Load reads a .env file without overriding existing environment variables.
// Empty files are valid. The file must exist.
func Load(path string) (*Config, error) {
	if err := godotenv.Load(path); err != nil {
		return nil, fmt.Errorf("load config %q: %w", path, err)
	}
	// Parse typed settings from the environment here as Config gains fields.
	return &Config{}, nil
}
