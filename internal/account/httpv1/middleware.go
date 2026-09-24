// Package httpv1 exposes account authentication rules to API v1.
package httpv1

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/emount4/poidem-back/internal/account"
	"github.com/emount4/poidem-back/internal/api/apierr"
	"github.com/gin-gonic/gin"
)

type Authenticator interface {
	Authenticate(ctx context.Context, token string) (account.Principal, error)
}

func RequireAuthentication(authenticator Authenticator) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, ok := bearerToken(c.GetHeader("Authorization"))
		if !ok {
			apierr.Write(c, http.StatusUnauthorized, "UNAUTHORIZED", "Требуется авторизация", nil)
			return
		}
		principal, err := authenticator.Authenticate(c.Request.Context(), token)
		switch {
		case errors.Is(err, account.ErrUnauthorized):
			apierr.Write(c, http.StatusUnauthorized, "UNAUTHORIZED", "Недействительный access token", nil)
			return
		case errors.Is(err, account.ErrUserBanned):
			apierr.Write(c, http.StatusForbidden, "USER_BANNED", "Пользователь заблокирован", nil)
			return
		case err != nil:
			apierr.WriteInternal(c, err)
			return
		}
		c.Request = c.Request.WithContext(account.WithPrincipal(c.Request.Context(), principal))
		c.Next()
	}
}

// OptionalAuthentication attaches a principal when a valid token is present.
// Missing, expired and banned-user tokens are treated as an anonymous public request.
func OptionalAuthentication(authenticator Authenticator) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, ok := bearerToken(c.GetHeader("Authorization"))
		if !ok {
			c.Next()
			return
		}
		principal, err := authenticator.Authenticate(c.Request.Context(), token)
		if errors.Is(err, account.ErrUnauthorized) || errors.Is(err, account.ErrUserBanned) {
			c.Next()
			return
		}
		if err != nil {
			apierr.WriteInternal(c, err)
			return
		}
		c.Request = c.Request.WithContext(account.WithPrincipal(c.Request.Context(), principal))
		c.Next()
	}
}

func RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		principal, ok := account.PrincipalFromContext(c.Request.Context())
		if !ok {
			apierr.Write(c, http.StatusUnauthorized, "UNAUTHORIZED", "Требуется авторизация", nil)
			return
		}
		if principal.Role != account.RoleAdmin {
			apierr.Write(c, http.StatusForbidden, "FORBIDDEN", "Недостаточно прав", nil)
			return
		}
		c.Next()
	}
}

func RequireCompleteProfile() gin.HandlerFunc {
	return func(c *gin.Context) {
		principal, ok := account.PrincipalFromContext(c.Request.Context())
		if !ok {
			apierr.Write(c, http.StatusUnauthorized, "UNAUTHORIZED", "Требуется авторизация", nil)
			return
		}
		if !principal.ProfileComplete {
			apierr.Write(c, http.StatusForbidden, "PROFILE_INCOMPLETE", "Завершите заполнение профиля", nil)
			return
		}
		c.Next()
	}
}

func bearerToken(header string) (string, bool) {
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
		return "", false
	}
	return parts[1], true
}
