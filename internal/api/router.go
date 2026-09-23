// Package api exposes the application's HTTP API through Gin.
package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/emount4/poidem-back/internal/api/apierr"
	"github.com/emount4/poidem-back/internal/api/v1"
	"github.com/gin-gonic/gin"
)

type Dependencies struct {
	PingDatabase func(context.Context) error
	CORSOrigin   string
	V1           v1.Dependencies
}

func NewRouter(log *slog.Logger, dependencies Dependencies) *gin.Engine {
	router := gin.New()
	router.Use(requestIDMiddleware(), requestLogger(log), recovery(log), cors(dependencies.CORSOrigin))
	registerAPIRoutes(router, dependencies.V1)
	router.NoRoute(func(c *gin.Context) {
		apierr.Write(c, http.StatusNotFound, "NOT_FOUND", "Ресурс не найден", nil)
	})
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	router.GET("/ready", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if err := dependencies.PingDatabase(ctx); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unavailable"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	return router
}
