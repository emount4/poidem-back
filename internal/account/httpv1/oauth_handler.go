package httpv1

import (
	"context"
	"net/http"
	"strings"

	"github.com/emount4/poidem-back/internal/account"
	accountoauth "github.com/emount4/poidem-back/internal/account/oauth"
	"github.com/emount4/poidem-back/internal/api/apierr"
	"github.com/gin-gonic/gin"
)

const oauthFlowCookiePrefix = "oauth_flow_"

type OAuthProvider interface {
	AuthorizationURL(state, codeChallenge string) string
	Identity(context.Context, string, string) (account.ExternalIdentity, error)
}

type OAuthLogin interface {
	Login(context.Context, account.ExternalIdentity) (string, error)
}

type OAuthRoutesConfig struct {
	Providers       map[string]OAuthProvider
	Login           OAuthLogin
	Flow            *accountoauth.Flow
	RefreshCookies  CookieConfig
	FrontendURL     string
	StateCookiePath string
}

func RegisterOAuthRoutes(routes *gin.RouterGroup, config OAuthRoutesConfig) {
	h := oauthHandler{config: config}
	routes.GET("/auth/:provider", h.start)
	routes.GET("/auth/:provider/callback", h.callback)
}

type oauthHandler struct{ config OAuthRoutesConfig }

func (h oauthHandler) start(c *gin.Context) {
	provider := h.config.Providers[c.Param("provider")]
	if provider == nil || h.config.Login == nil || h.config.Flow == nil {
		apierr.Write(c, http.StatusServiceUnavailable, "OAUTH_PROVIDER_UNAVAILABLE", "OAuth-провайдер не настроен", nil)
		return
	}
	authorization, err := h.config.Flow.Start()
	if err != nil {
		apierr.WriteInternal(c, err)
		return
	}
	h.setFlowCookie(c, authorization.CookieValue)
	c.Redirect(http.StatusFound, provider.AuthorizationURL(authorization.State, authorization.CodeChallenge))
}

func (h oauthHandler) callback(c *gin.Context) {
	provider := h.config.Providers[c.Param("provider")]
	if provider == nil || h.config.Login == nil || h.config.Flow == nil {
		h.redirectError(c)
		return
	}
	h.clearFlowCookie(c)
	if c.Query("error") != "" || c.Query("code") == "" {
		h.redirectError(c)
		return
	}
	cookieValue, err := c.Cookie(h.flowCookieName(c))
	if err != nil {
		h.redirectError(c)
		return
	}
	verifier, err := h.config.Flow.Verify(cookieValue, c.Query("state"))
	if err != nil {
		h.redirectError(c)
		return
	}
	identity, err := provider.Identity(c.Request.Context(), c.Query("code"), verifier)
	if err != nil {
		_ = c.Error(err).SetType(gin.ErrorTypePrivate)
		h.redirectError(c)
		return
	}
	refresh, err := h.config.Login.Login(c.Request.Context(), identity)
	if err != nil {
		_ = c.Error(err).SetType(gin.ErrorTypePrivate)
		h.redirectError(c)
		return
	}
	h.config.RefreshCookies.setRefresh(c, refresh)
	c.Redirect(http.StatusFound, strings.TrimRight(h.config.FrontendURL, "/")+"/auth/callback")
}

func (h oauthHandler) redirectError(c *gin.Context) {
	c.Redirect(http.StatusFound, strings.TrimRight(h.config.FrontendURL, "/")+"/auth/error")
}

func (h oauthHandler) setFlowCookie(c *gin.Context, value string) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name: h.flowCookieName(c), Value: value, Path: h.flowCookiePath(c),
		MaxAge: int(h.config.Flow.TTL().Seconds()), HttpOnly: true,
		Secure: h.config.RefreshCookies.Secure, SameSite: http.SameSiteLaxMode,
	})
}

func (h oauthHandler) clearFlowCookie(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name: h.flowCookieName(c), Path: h.flowCookiePath(c), MaxAge: -1,
		HttpOnly: true, Secure: h.config.RefreshCookies.Secure, SameSite: http.SameSiteLaxMode,
	})
}

func (h oauthHandler) flowCookieName(c *gin.Context) string {
	return oauthFlowCookiePrefix + c.Param("provider")
}

func (h oauthHandler) flowCookiePath(c *gin.Context) string {
	return strings.TrimRight(h.config.StateCookiePath, "/") + "/" + c.Param("provider")
}
