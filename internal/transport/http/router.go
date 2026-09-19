// Package http exposes the application's HTTP API through Gin.
package http

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

func NewRouter(log *slog.Logger, pingDatabase func(context.Context) error) *gin.Engine {
	router := gin.New()
	router.Use(requestIDMiddleware(), requestLogger(log), recovery(log))
	registerAPIRoutes(router)
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	router.GET("/ready", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if err := pingDatabase(ctx); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unavailable"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	return router
}
