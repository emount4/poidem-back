package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content string
		wantErr bool
	}{
		{name: "empty file"},
		{name: "comments only", content: "# settings will follow\n"},
		{name: "invalid key", content: "INVALID!KEY=value\n", wantErr: true},
		{name: "unclosed quote", content: "POIDEM_TEST_VALUE=\"value\n", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), ".env")
			if err := os.WriteFile(path, []byte(tc.content), 0600); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load(path)
			if (err != nil) != tc.wantErr {
				t.Fatalf("Load() error = %v, wantErr = %v", err, tc.wantErr)
			}
			if !tc.wantErr && cfg == nil {
				t.Fatal("Load() returned nil config")
			}
		})
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "missing.env"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected file-not-found error, got %v", err)
	}
}

func TestLoadEnvironment(t *testing.T) {
	const loadedKey = "POIDEM_CONFIG_TEST_LOADED"
	const existingKey = "POIDEM_CONFIG_TEST_EXISTING"
	// Setenv registers restoration of the original value, even after Unsetenv.
	t.Setenv(loadedKey, "")
	if err := os.Unsetenv(loadedKey); err != nil {
		t.Fatal(err)
	}
	t.Setenv(existingKey, "from environment")
	path := filepath.Join(t.TempDir(), ".env")
	content := "# settings\n" + loadedKey + "=\"hello world\"\n" + existingKey + "=from-file\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv(loadedKey); got != "hello world" {
		t.Fatalf("expected quoted value from .env, got %q", got)
	}
	if got := os.Getenv(existingKey); got != "from environment" {
		t.Fatalf("existing environment variable was overwritten: %q", got)
	}
}
