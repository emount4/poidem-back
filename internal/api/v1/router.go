// Package v1 contains HTTP handlers and DTOs for the first API version.
package v1

import (
	"net/http"

	accounthttp "github.com/emount4/poidem-back/internal/account/httpv1"
	cataloghttp "github.com/emount4/poidem-back/internal/catalog/httpv1"
	"github.com/gin-gonic/gin"
)

type Dependencies struct {
	Catalog        cataloghttp.Catalog
	Sessions       accounthttp.Sessions
	SessionCookies accounthttp.CookieConfig
	OAuth          accounthttp.OAuthRoutesConfig
}

// RegisterRoutes mounts v1 routes on the supplied group.
// Use paths relative to the version prefix, e.g. "/events".
func RegisterRoutes(routes *gin.RouterGroup, dependencies Dependencies) {
	routes.GET("", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"version": "v1"})
	})
	cataloghttp.RegisterRoutes(routes, dependencies.Catalog)
	accounthttp.RegisterSessionRoutes(routes, dependencies.Sessions, dependencies.SessionCookies)
	accounthttp.RegisterOAuthRoutes(routes, dependencies.OAuth)
}
