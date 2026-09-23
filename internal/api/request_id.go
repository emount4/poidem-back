package api

import (
	"crypto/rand"

	"github.com/emount4/poidem-back/internal/platform/requestid"
	"github.com/gin-gonic/gin"
)

const requestIDHeader = "X-Request-ID"

func requestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		values := c.Request.Header.Values(requestIDHeader)
		id := ""
		if len(values) == 1 && validRequestID(values[0]) {
			id = values[0]
		} else {
			id = rand.Text()
		}
		c.Request = c.Request.WithContext(requestid.WithContext(c.Request.Context(), id))
		c.Request.Header.Set(requestIDHeader, id)
		c.Header(requestIDHeader, id)
		c.Next()
	}
}

// Accept bounded, printable IDs suitable for correlation in headers and logs.
// Ambiguous or invalid client IDs are replaced without rejecting the request.
func validRequestID(id string) bool {
	if len(id) == 0 || len(id) > 128 {
		return false
	}
	for _, char := range id {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || char == '-' || char == '_' || char == '.' {
			continue
		}
		return false
	}
	return true
}
