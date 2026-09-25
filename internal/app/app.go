// Package app assembles dependencies and manages the application lifecycle.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/emount4/poidem-back/internal/account"
	accounthttp "github.com/emount4/poidem-back/internal/account/httpv1"
	accountjwt "github.com/emount4/poidem-back/internal/account/jwt"
	accountoauth "github.com/emount4/poidem-back/internal/account/oauth"
	googleoauth "github.com/emount4/poidem-back/internal/account/oauth/google"
	accountpostgres "github.com/emount4/poidem-back/internal/account/postgres"
	"github.com/emount4/poidem-back/internal/api"
	"github.com/emount4/poidem-back/internal/api/v1"
	"github.com/emount4/poidem-back/internal/catalog"
	catalogpostgres "github.com/emount4/poidem-back/internal/catalog/postgres"
	"github.com/emount4/poidem-back/internal/companies"
	companiespostgres "github.com/emount4/poidem-back/internal/companies/postgres"
	"github.com/emount4/poidem-back/internal/events"
	eventspostgres "github.com/emount4/poidem-back/internal/events/postgres"
	"github.com/emount4/poidem-back/internal/platform/avatarstorage"
	"github.com/emount4/poidem-back/internal/platform/config"
	"github.com/emount4/poidem-back/internal/platform/postgres"
	"github.com/emount4/poidem-back/internal/reports"
	reportspostgres "github.com/emount4/poidem-back/internal/reports/postgres"
	"github.com/gin-gonic/gin"
)

func Run(ctx context.Context, configPath string, log *slog.Logger) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	pool, err := postgres.New(ctx, cfg.Postgres)
	if err != nil {
		return err
	}
	defer pool.Close()
	log.Info("postgres connected")
	catalogService := catalog.NewService(catalogpostgres.NewRepository(pool))
	accountRepository := accountpostgres.NewRepository(pool)
	if cfg.BootstrapAdminUserID > 0 {
		if err := accountRepository.PromoteAdmin(ctx, cfg.BootstrapAdminUserID); err != nil {
			return fmt.Errorf("bootstrap admin user %d: %w", cfg.BootstrapAdminUserID, err)
		}
		log.Info("bootstrap admin role ensured", "user_id", cfg.BootstrapAdminUserID)
	}
	transactions := postgres.NewTxManager(pool)
	accessTokens, err := accountjwt.NewAccessTokens(cfg.Auth.JWTSecret, cfg.Auth.JWTIssuer, cfg.Auth.JWTAudience, cfg.Auth.AccessTTL)
	if err != nil {
		return fmt.Errorf("configure access tokens: %w", err)
	}
	refreshService, err := account.NewRefreshService(accountRepository, accessTokens, cfg.Auth.RefreshRetryWindow)
	if err != nil {
		return fmt.Errorf("configure refresh sessions: %w", err)
	}
	loginService, err := account.NewOAuthLoginService(accountRepository, transactions, cfg.Auth.RefreshTTL)
	if err != nil {
		return fmt.Errorf("configure OAuth login: %w", err)
	}
	authService := account.NewService(accessTokens, accountRepository)
	profileService, err := account.NewProfileService(accountRepository, transactions)
	if err != nil {
		return fmt.Errorf("configure profiles: %w", err)
	}
	adminService := account.NewAdminService(accountRepository)
	avatarStorage, err := avatarstorage.NewMinIO(ctx, cfg.Storage)
	if err != nil {
		return fmt.Errorf("configure avatar storage: %w", err)
	}
	avatarService, err := account.NewAvatarService(accountRepository, avatarStorage)
	if err != nil {
		return fmt.Errorf("configure avatars: %w", err)
	}
	eventRepository := eventspostgres.NewRepository(pool)
	eventService := events.NewService(eventRepository)
	coverService, err := events.NewCoverService(eventRepository, avatarStorage)
	if err != nil {
		return fmt.Errorf("configure event covers: %w", err)
	}
	companyService := companies.NewService(companiespostgres.NewRepository(pool))
	reportService := reports.NewService(reportspostgres.NewRepository(pool))
	eventCompletion := events.NewCompletionWorker(eventRepository, log, time.Minute, 100)
	coverCleanup := events.NewCoverCleanupWorker(eventRepository, avatarStorage, log)
	oauthFlow, err := accountoauth.NewFlow(cfg.OAuth.StateSecret, cfg.OAuth.FlowTTL)
	if err != nil {
		return fmt.Errorf("configure OAuth flow: %w", err)
	}
	oauthProviders := make(map[string]accounthttp.OAuthProvider)
	if cfg.OAuth.GoogleClientID != "" {
		googleProvider, providerErr := googleoauth.New(googleoauth.Config{
			ClientID: cfg.OAuth.GoogleClientID, ClientSecret: cfg.OAuth.GoogleClientSecret,
			RedirectURL: cfg.OAuth.CallbackURL,
		})
		if providerErr != nil {
			return fmt.Errorf("configure Google OAuth: %w", providerErr)
		}
		oauthProviders["google"] = googleProvider
		log.Info("Google OAuth enabled")
	} else {
		log.Info("Google OAuth disabled: client credentials are not configured")
	}
	refreshCookies := accounthttp.CookieConfig{
		Path: cfg.Auth.CookiePath, Domain: cfg.Auth.CookieDomain,
		MaxAge: cfg.Auth.RefreshTTL, Secure: cfg.Auth.CookieSecure,
		SameSite: sameSite(cfg.Auth.CookieSameSite),
	}
	gin.SetMode(gin.ReleaseMode)
	server := &http.Server{
		Addr: ":8080",
		Handler: api.NewRouter(log, api.Dependencies{
			PingDatabase: pool.Ping,
			CORSOrigin:   cfg.OAuth.FrontendOrigin,
			V1: v1.Dependencies{
				Catalog: catalogService, Sessions: refreshService, SessionCookies: refreshCookies,
				Authenticator: authService, Profiles: profileService, Avatars: avatarService, Admin: adminService,
				Events: eventService, Covers: coverService, Companies: companyService, Reports: reportService,
				OAuth: accounthttp.OAuthRoutesConfig{
					Providers: oauthProviders, Login: loginService, Flow: oauthFlow,
					RefreshCookies: refreshCookies, FrontendURL: cfg.OAuth.FrontendURL,
					StateCookiePath: "/api/v1/auth",
				},
			},
		}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return fmt.Errorf("listen HTTP: %w", err)
	}
	go eventCompletion.Run(ctx)
	go coverCleanup.Run(ctx)
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()
	log.Info("http server started", "address", server.Addr)
	select {
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve HTTP: %w", err)
		}
		return nil
	case <-ctx.Done():
		log.Info("http server stopping")
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		_ = server.Close()
		return fmt.Errorf("shutdown HTTP: %w", err)
	}
	if err := <-serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve HTTP: %w", err)
	}
	log.Info("http server stopped")
	return nil
}

func sameSite(value string) http.SameSite {
	switch value {
	case "strict":
		return http.SameSiteStrictMode
	case "none":
		return http.SameSiteNoneMode
	default:
		return http.SameSiteLaxMode
	}
}
