// Package config loads application configuration.
package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	Postgres             Postgres
	Auth                 Auth
	OAuth                OAuth
	Storage              Storage
	BootstrapAdminUserID int64
}

type Storage struct {
	Endpoint  string
	PublicURL string
	AccessKey string
	SecretKey string
	Bucket    string
	Region    string
	UseSSL    bool
}

type Auth struct {
	JWTSecret          string
	JWTIssuer          string
	JWTAudience        string
	AccessTTL          time.Duration
	RefreshTTL         time.Duration
	RefreshRetryWindow time.Duration
	CookieSecure       bool
	CookieSameSite     string
	CookieDomain       string
	CookiePath         string
}

type OAuth struct {
	FrontendURL        string
	FrontendOrigin     string
	APIURL             string
	CallbackURL        string
	StateSecret        string
	FlowTTL            time.Duration
	GoogleClientID     string
	GoogleClientSecret string
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
	if raw := strings.TrimSpace(os.Getenv("BOOTSTRAP_ADMIN_USER_ID")); raw != "" {
		value, parseErr := strconv.ParseInt(raw, 10, 64)
		if parseErr != nil || value <= 0 {
			return nil, fmt.Errorf("BOOTSTRAP_ADMIN_USER_ID must be a positive integer")
		}
		cfg.BootstrapAdminUserID = value
	}
	switch cfg.Postgres.SSLMode {
	case "disable", "allow", "prefer", "require", "verify-ca", "verify-full":
	default:
		return nil, fmt.Errorf("POSTGRES_SSLMODE is invalid")
	}
	accessTTL, err := durationEnv("ACCESS_TOKEN_TTL", "15m")
	if err != nil {
		return nil, err
	}
	refreshTTL, err := durationEnv("REFRESH_TOKEN_TTL", "720h")
	if err != nil {
		return nil, err
	}
	retryWindow, err := durationEnv("REFRESH_RETRY_WINDOW", "10s")
	if err != nil {
		return nil, err
	}
	cookieSecure, err := strconv.ParseBool(env("REFRESH_COOKIE_SECURE", "false"))
	if err != nil {
		return nil, fmt.Errorf("REFRESH_COOKIE_SECURE must be true or false")
	}
	cfg.Auth = Auth{
		JWTSecret: os.Getenv("JWT_SECRET"), JWTIssuer: env("JWT_ISSUER", "poydem"),
		JWTAudience: env("JWT_AUDIENCE", "poydem-api"), AccessTTL: accessTTL,
		RefreshTTL: refreshTTL, RefreshRetryWindow: retryWindow, CookieSecure: cookieSecure,
		CookieSameSite: strings.ToLower(env("REFRESH_COOKIE_SAME_SITE", "lax")),
		CookieDomain:   os.Getenv("REFRESH_COOKIE_DOMAIN"), CookiePath: env("REFRESH_COOKIE_PATH", "/api/v1/auth"),
	}
	if len(cfg.Auth.JWTSecret) < 32 {
		return nil, fmt.Errorf("JWT_SECRET must contain at least 32 bytes")
	}
	if cfg.Auth.RefreshTTL <= cfg.Auth.AccessTTL {
		return nil, fmt.Errorf("REFRESH_TOKEN_TTL must exceed ACCESS_TOKEN_TTL")
	}
	if cfg.Auth.CookiePath == "" || cfg.Auth.CookiePath[0] != '/' {
		return nil, fmt.Errorf("REFRESH_COOKIE_PATH must start with /")
	}
	switch cfg.Auth.CookieSameSite {
	case "lax", "strict", "none":
	default:
		return nil, fmt.Errorf("REFRESH_COOKIE_SAME_SITE must be lax, strict or none")
	}
	if cfg.Auth.CookieSameSite == "none" && !cfg.Auth.CookieSecure {
		return nil, fmt.Errorf("SameSite=None requires REFRESH_COOKIE_SECURE=true")
	}
	flowTTL, err := durationEnv("OAUTH_FLOW_TTL", "10m")
	if err != nil {
		return nil, err
	}
	apiURL := strings.TrimRight(env("API_URL", "http://localhost:8080"), "/")
	cfg.OAuth = OAuth{
		FrontendURL: strings.TrimRight(env("FRONTEND_URL", "http://localhost:3000"), "/"),
		APIURL:      apiURL, CallbackURL: env("OAUTH_CALLBACK_URL", apiURL+"/api/v1/auth/google/callback"),
		StateSecret: env("OAUTH_STATE_SECRET", cfg.Auth.JWTSecret), FlowTTL: flowTTL,
		GoogleClientID: os.Getenv("GOOGLE_CLIENT_ID"), GoogleClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
	}
	if err := validateBaseURL("FRONTEND_URL", cfg.OAuth.FrontendURL); err != nil {
		return nil, err
	}
	cfg.OAuth.FrontendOrigin = origin(cfg.OAuth.FrontendURL)
	if err := validateBaseURL("API_URL", cfg.OAuth.APIURL); err != nil {
		return nil, err
	}
	if err := validateBaseURL("OAUTH_CALLBACK_URL", cfg.OAuth.CallbackURL); err != nil {
		return nil, err
	}
	if len(cfg.OAuth.StateSecret) < 32 {
		return nil, fmt.Errorf("OAUTH_STATE_SECRET must contain at least 32 bytes")
	}
	if (cfg.OAuth.GoogleClientID == "") != (cfg.OAuth.GoogleClientSecret == "") {
		return nil, fmt.Errorf("GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET must be set together")
	}
	storageSSL, err := strconv.ParseBool(env("S3_USE_SSL", "false"))
	if err != nil {
		return nil, fmt.Errorf("S3_USE_SSL must be true or false")
	}
	cfg.Storage = Storage{
		Endpoint: env("S3_ENDPOINT", "localhost:9000"), PublicURL: strings.TrimRight(env("S3_PUBLIC_URL", "http://localhost:9000"), "/"),
		AccessKey: env("S3_ACCESS_KEY", "poidem_minio"), SecretKey: env("S3_SECRET_KEY", "poidem_minio_secret"),
		Bucket: env("S3_BUCKET", "poidem-media"), Region: env("S3_REGION", "us-east-1"), UseSSL: storageSSL,
	}
	if strings.Contains(cfg.Storage.Endpoint, "://") || strings.TrimSpace(cfg.Storage.Endpoint) == "" {
		return nil, fmt.Errorf("S3_ENDPOINT must be host:port without a URL scheme")
	}
	if err := validateBaseURL("S3_PUBLIC_URL", cfg.Storage.PublicURL); err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.Storage.AccessKey) == "" || strings.TrimSpace(cfg.Storage.SecretKey) == "" || strings.TrimSpace(cfg.Storage.Bucket) == "" {
		return nil, fmt.Errorf("S3_ACCESS_KEY, S3_SECRET_KEY and S3_BUCKET are required")
	}
	return cfg, nil
}

func validateBaseURL(key, value string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("%s must be an absolute HTTP(S) URL", key)
	}
	return nil
}

func origin(value string) string {
	parsed, _ := url.Parse(value)
	return parsed.Scheme + "://" + parsed.Host
}

func durationEnv(key, fallback string) (time.Duration, error) {
	value, err := time.ParseDuration(env(key, fallback))
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", key)
	}
	return value, nil
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
