package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func cors(allowedOrigin string) gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin == "" {
			c.Next()
			return
		}
		if origin != allowedOrigin {
			if c.Request.Method == http.MethodOptions {
				c.AbortWithStatus(http.StatusForbidden)
				return
			}
			c.Next()
			return
		}
		headers := c.Writer.Header()
		headers.Set("Access-Control-Allow-Origin", allowedOrigin)
		headers.Set("Access-Control-Allow-Credentials", "true")
		headers.Add("Vary", "Origin")
		if c.Request.Method == http.MethodOptions {
			headers.Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
			headers.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Request-ID")
			headers.Set("Access-Control-Max-Age", "600")
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
