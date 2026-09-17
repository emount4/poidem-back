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

	"github.com/emount4/poidem-back/internal/config"
	httptransport "github.com/emount4/poidem-back/internal/transport/http"
	"github.com/gin-gonic/gin"
)

func Run(ctx context.Context, configPath string, log *slog.Logger) error {
	// Config is intentionally empty. Wire its fields here when added.
	if _, err := config.Load(configPath); err != nil {
		return err
	}
	gin.SetMode(gin.ReleaseMode)
	server := &http.Server{
		Addr:              ":8080",
		Handler:           httptransport.NewRouter(log),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return fmt.Errorf("listen HTTP: %w", err)
	}
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
