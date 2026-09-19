// Package v1 contains HTTP handlers and DTOs for the first API version.
package v1

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// RegisterRoutes mounts v1 routes on the supplied group.
// Use paths relative to the version prefix, e.g. "/events".
func RegisterRoutes(routes *gin.RouterGroup) {
	routes.GET("", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"version": "v1"})
	})
}
