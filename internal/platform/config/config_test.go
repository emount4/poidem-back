package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	setTestEnvironment(t)
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
	setTestEnvironment(t)
	const loadedKey = "POSTGRES_PASSWORD"
	const existingKey = "POSTGRES_HOST"
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
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv(loadedKey); got != "hello world" {
		t.Fatalf("expected quoted value from .env, got %q", got)
	}
	if got := os.Getenv(existingKey); got != "from environment" {
		t.Fatalf("existing environment variable was overwritten: %q", got)
	}
	if cfg.Postgres.Password != "hello world" || cfg.Postgres.Host != "from environment" {
		t.Fatal("config must use values loaded from .env with environment priority")
	}
}

func setTestEnvironment(t *testing.T) {
	t.Helper()
	for key, value := range map[string]string{
		"POSTGRES_HOST": "", "POSTGRES_PORT": "", "POSTGRES_USER": "",
		"POSTGRES_DB": "", "POSTGRES_SSLMODE": "", "POSTGRES_PASSWORD": "test-password",
		"JWT_SECRET":       "12345678901234567890123456789012",
		"ACCESS_TOKEN_TTL": "", "REFRESH_TOKEN_TTL": "", "REFRESH_RETRY_WINDOW": "",
		"REFRESH_COOKIE_SECURE": "", "REFRESH_COOKIE_SAME_SITE": "",
		"REFRESH_COOKIE_DOMAIN": "", "REFRESH_COOKIE_PATH": "",
		"FRONTEND_URL": "", "API_URL": "", "OAUTH_CALLBACK_URL": "",
		"OAUTH_STATE_SECRET": "", "OAUTH_FLOW_TTL": "",
		"GOOGLE_CLIENT_ID": "", "GOOGLE_CLIENT_SECRET": "",
		"S3_ENDPOINT": "", "S3_PUBLIC_URL": "", "S3_ACCESS_KEY": "", "S3_SECRET_KEY": "",
		"S3_BUCKET": "", "S3_REGION": "", "S3_USE_SSL": "",
	} {
		t.Setenv(key, value)
	}
}

func TestLoadStorageSettings(t *testing.T) {
	setTestEnvironment(t)
	t.Setenv("S3_ENDPOINT", "minio:9000")
	t.Setenv("S3_PUBLIC_URL", "https://media.example.com/")
	t.Setenv("S3_ACCESS_KEY", "access")
	t.Setenv("S3_SECRET_KEY", "secret-value")
	t.Setenv("S3_BUCKET", "avatars")
	t.Setenv("S3_REGION", "ru-central1")
	t.Setenv("S3_USE_SSL", "true")
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Storage.Endpoint != "minio:9000" || cfg.Storage.PublicURL != "https://media.example.com" || !cfg.Storage.UseSSL {
		t.Fatalf("unexpected storage config: %+v", cfg.Storage)
	}

	for _, tc := range []struct{ key, value string }{
		{"S3_ENDPOINT", "http://minio:9000"},
		{"S3_PUBLIC_URL", "minio:9000"},
		{"S3_USE_SSL", "sometimes"},
	} {
		t.Run(tc.key, func(t *testing.T) {
			setTestEnvironment(t)
			t.Setenv(tc.key, tc.value)
			if _, err := Load(""); err == nil {
				t.Fatal("expected invalid storage configuration to fail")
			}
		})
	}
}

func TestLoadOAuthSettings(t *testing.T) {
	setTestEnvironment(t)
	t.Setenv("API_URL", "https://api.example.com/")
	t.Setenv("FRONTEND_URL", "https://app.example.com/")
	t.Setenv("GOOGLE_CLIENT_ID", "client")
	t.Setenv("GOOGLE_CLIENT_SECRET", "secret")
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OAuth.CallbackURL != "https://api.example.com/api/v1/auth/google/callback" || cfg.OAuth.FrontendURL != "https://app.example.com" {
		t.Fatalf("unexpected OAuth URLs: %+v", cfg.OAuth)
	}

	for _, tc := range []struct{ key, value string }{
		{"FRONTEND_URL", "localhost:3000"},
		{"API_URL", "ftp://api.example.com"},
		{"OAUTH_FLOW_TTL", "0s"},
		{"OAUTH_STATE_SECRET", "short"},
		{"GOOGLE_CLIENT_ID", "client-without-secret"},
	} {
		t.Run(tc.key, func(t *testing.T) {
			setTestEnvironment(t)
			t.Setenv(tc.key, tc.value)
			if _, err := Load(""); err == nil {
				t.Fatal("expected invalid OAuth configuration to fail")
			}
		})
	}
}

func TestLoadEnvironmentOnly(t *testing.T) {
	setTestEnvironment(t)
	t.Setenv("POSTGRES_HOST", "database")
	t.Setenv("POSTGRES_PORT", "5433")
	t.Setenv("POSTGRES_USER", "backend")
	t.Setenv("POSTGRES_DB", "backend_db")
	t.Setenv("POSTGRES_SSLMODE", "require")
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	want := Postgres{Host: "database", Port: 5433, User: "backend", Password: "test-password", Database: "backend_db", SSLMode: "require"}
	if cfg.Postgres != want {
		t.Fatal("PostgreSQL settings do not match environment")
	}
}

func TestLoadInvalidPostgresSettings(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{"POSTGRES_PORT", "0"}, {"POSTGRES_PORT", "65536"},
		{"POSTGRES_PORT", "abc"}, {"POSTGRES_PORT", "-1"},
		{"POSTGRES_PASSWORD", ""}, {"POSTGRES_SSLMODE", "invalid"},
	} {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			setTestEnvironment(t)
			t.Setenv(tc.key, tc.value)
			if _, err := Load(""); err == nil {
				t.Fatal("expected invalid configuration to fail")
			}
		})
	}
}

func TestLoadInvalidAuthSettings(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{"JWT_SECRET", "short"},
		{"ACCESS_TOKEN_TTL", "invalid"},
		{"REFRESH_TOKEN_TTL", "10m"},
		{"REFRESH_RETRY_WINDOW", "0s"},
		{"REFRESH_COOKIE_SECURE", "sometimes"},
		{"REFRESH_COOKIE_SAME_SITE", "invalid"},
		{"REFRESH_COOKIE_PATH", "auth"},
	} {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			setTestEnvironment(t)
			t.Setenv(tc.key, tc.value)
			if _, err := Load(""); err == nil {
				t.Fatal("expected invalid auth configuration to fail")
			}
		})
	}
	setTestEnvironment(t)
	t.Setenv("REFRESH_COOKIE_SAME_SITE", "none")
	t.Setenv("REFRESH_COOKIE_SECURE", "false")
	if _, err := Load(""); err == nil {
		t.Fatal("SameSite=None without Secure must fail")
	}
}
