package api

import (
	"github.com/emount4/poidem-back/internal/api/v1"
	"github.com/gin-gonic/gin"
)

// registerAPIRoutes is the single place where supported API versions are mounted.
func registerAPIRoutes(router *gin.Engine, dependencies v1.Dependencies) {
	api := router.Group("/api")
	v1.RegisterRoutes(api.Group("/v1"), dependencies)
}
