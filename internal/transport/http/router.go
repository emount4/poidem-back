// Package http exposes the application's HTTP API through Gin.
package http

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
)

func NewRouter(log *slog.Logger) *gin.Engine {
	router := gin.New()
	router.Use(requestLogger(log), recovery(log))
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	return router
}
