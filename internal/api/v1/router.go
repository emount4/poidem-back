// Package v1 contains HTTP handlers and DTOs for the first API version.
package v1

import (
	"net/http"

	accounthttp "github.com/emount4/poidem-back/internal/account/httpv1"
	cataloghttp "github.com/emount4/poidem-back/internal/catalog/httpv1"
	companieshttp "github.com/emount4/poidem-back/internal/companies/httpv1"
	eventshttp "github.com/emount4/poidem-back/internal/events/httpv1"
	reportshttp "github.com/emount4/poidem-back/internal/reports/httpv1"
	"github.com/gin-gonic/gin"
)

type Dependencies struct {
	Catalog        cataloghttp.Catalog
	Sessions       accounthttp.Sessions
	SessionCookies accounthttp.CookieConfig
	OAuth          accounthttp.OAuthRoutesConfig
	Authenticator  accounthttp.Authenticator
	Profiles       accounthttp.Profiles
	Avatars        accounthttp.Avatars
	Admin          accounthttp.Admin
	Events         eventshttp.Events
	Covers         eventshttp.Covers
	Companies      companieshttp.Companies
	Reports        reportshttp.Reports
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
	accounthttp.RegisterProfileRoutes(routes, dependencies.Profiles, dependencies.Authenticator)
	accounthttp.RegisterAvatarRoutes(routes, dependencies.Avatars, dependencies.Authenticator)
	accounthttp.RegisterAdminRoutes(routes, dependencies.Admin, dependencies.Authenticator)
	eventshttp.RegisterRoutes(routes, dependencies.Events, dependencies.Authenticator)
	eventshttp.RegisterCoverRoutes(routes, dependencies.Covers, dependencies.Authenticator)
	companieshttp.RegisterRoutes(routes, dependencies.Companies, dependencies.Authenticator)
	reportshttp.RegisterRoutes(routes, dependencies.Reports, dependencies.Authenticator)
}
