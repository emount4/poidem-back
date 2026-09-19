package http

import (
	"github.com/emount4/poidem-back/internal/transport/http/v1"
	"github.com/gin-gonic/gin"
)

// registerAPIRoutes is the single place where supported API versions are mounted.
func registerAPIRoutes(router *gin.Engine) {
	api := router.Group("/api")
	v1.RegisterRoutes(api.Group("/v1"))
}
