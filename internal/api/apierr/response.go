// Package apierr writes the version-independent API error envelope.
package apierr

import "github.com/gin-gonic/gin"

type Response struct {
	Error Error `json:"error"`
}

type Error struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details"`
}

func Write(c *gin.Context, status int, code, message string, details map[string]any) {
	if details == nil {
		details = map[string]any{}
	}
	c.AbortWithStatusJSON(status, Response{Error: Error{
		Code: code, Message: message, Details: details,
	}})
}
