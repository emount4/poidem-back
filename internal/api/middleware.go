package api

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/emount4/poidem-back/internal/api/apierr"
	"github.com/emount4/poidem-back/internal/platform/requestid"
	"github.com/gin-gonic/gin"
)

func requestLogger(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		started := time.Now()
		c.Next()
		level := slog.LevelInfo
		if c.Writer.Status() >= http.StatusInternalServerError {
			level = slog.LevelError
		} else if c.Writer.Status() >= http.StatusBadRequest {
			level = slog.LevelWarn
		}
		attributes := []any{
			"request_id", requestid.FromContext(c.Request.Context()),
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"duration", time.Since(started).String(),
		}
		if privateErrors := c.Errors.ByType(gin.ErrorTypePrivate); len(privateErrors) > 0 {
			attributes = append(attributes, "error", privateErrors.String())
		}
		log.Log(c.Request.Context(), level, "http request", attributes...)
	}
}

func recovery(log *slog.Logger) gin.HandlerFunc {
	return gin.CustomRecoveryWithWriter(nil, func(c *gin.Context, recovered any) {
		log.ErrorContext(c.Request.Context(), "http panic",
			"request_id", requestid.FromContext(c.Request.Context()),
			"error", fmt.Sprint(recovered), "stack", string(debug.Stack()))
		apierr.Write(c, http.StatusInternalServerError, "INTERNAL_ERROR", "Внутренняя ошибка сервера", nil)
	})
}
