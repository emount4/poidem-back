package httpv1

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/emount4/poidem-back/internal/account"
	"github.com/emount4/poidem-back/internal/api/apierr"
	"github.com/gin-gonic/gin"
)

const refreshCookieName = "refresh_token"

type Sessions interface {
	Refresh(context.Context, string) (account.TokenPair, error)
	Logout(context.Context, string) error
}

type CookieConfig struct {
	Path     string
	Domain   string
	MaxAge   time.Duration
	Secure   bool
	SameSite http.SameSite
}

func RegisterSessionRoutes(routes *gin.RouterGroup, sessions Sessions, cookies CookieConfig) {
	h := sessionHandler{sessions: sessions, cookies: cookies}
	routes.POST("/auth/refresh", h.refresh)
	routes.POST("/auth/logout", h.logout)
}

type sessionHandler struct {
	sessions Sessions
	cookies  CookieConfig
}

func (h sessionHandler) refresh(c *gin.Context) {
	raw, err := c.Cookie(refreshCookieName)
	if err != nil {
		apierr.Write(c, http.StatusUnauthorized, "UNAUTHORIZED", "Refresh-сессия недействительна", nil)
		return
	}
	pair, err := h.sessions.Refresh(c.Request.Context(), raw)
	if errors.Is(err, account.ErrUnauthorized) {
		h.cookies.clearRefresh(c)
		apierr.Write(c, http.StatusUnauthorized, "UNAUTHORIZED", "Refresh-сессия недействительна", nil)
		return
	}
	if err != nil {
		apierr.WriteInternal(c, err)
		return
	}
	h.cookies.setRefresh(c, pair.RefreshToken)
	c.JSON(http.StatusOK, gin.H{"accessToken": pair.AccessToken})
}

func (h sessionHandler) logout(c *gin.Context) {
	raw, _ := c.Cookie(refreshCookieName)
	if err := h.sessions.Logout(c.Request.Context(), raw); err != nil {
		apierr.WriteInternal(c, err)
		return
	}
	h.cookies.clearRefresh(c)
	c.Status(http.StatusNoContent)
}

func (c CookieConfig) setRefresh(ctx *gin.Context, value string) {
	http.SetCookie(ctx.Writer, &http.Cookie{Name: refreshCookieName, Value: value, Path: c.Path, Domain: c.Domain, MaxAge: int(c.MaxAge.Seconds()), HttpOnly: true, Secure: c.Secure, SameSite: c.SameSite})
}
func (c CookieConfig) clearRefresh(ctx *gin.Context) {
	http.SetCookie(ctx.Writer, &http.Cookie{Name: refreshCookieName, Path: c.Path, Domain: c.Domain, MaxAge: -1, HttpOnly: true, Secure: c.Secure, SameSite: c.SameSite})
}
