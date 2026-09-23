// Package apierr writes the version-independent API error envelope.
package apierr

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

const (
	CodeValidation = "VALIDATION_ERROR"
	CodeInternal   = "INTERNAL_ERROR"
)

const (
	messageValidation = "Некорректные данные"
	messageInternal   = "Внутренняя ошибка сервера"
)

type Response struct {
	Error Error `json:"error"`
}

type Error struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details"`
}

// FieldErrors maps a JSON field or query parameter to user-facing validation messages.
type FieldErrors map[string][]string

func Write(c *gin.Context, status int, code, message string, details map[string]any) {
	if details == nil {
		details = map[string]any{}
	}
	c.AbortWithStatusJSON(status, Response{Error: Error{
		Code: code, Message: message, Details: details,
	}})
}

func WriteValidation(c *gin.Context, fields FieldErrors) {
	if fields == nil {
		fields = FieldErrors{}
	}
	Write(c, http.StatusBadRequest, CodeValidation, messageValidation, map[string]any{
		"fields": fields,
	})
}

// WriteInternal records the original error for server logs and returns a safe response.
func WriteInternal(c *gin.Context, err error) {
	if err != nil {
		_ = c.Error(err).SetType(gin.ErrorTypePrivate)
	}
	Write(c, http.StatusInternalServerError, CodeInternal, messageInternal, nil)
}
