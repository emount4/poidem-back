package httpv1

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/emount4/poidem-back/internal/account"
	accountoauth "github.com/emount4/poidem-back/internal/account/oauth"
	"github.com/gin-gonic/gin"
)

type oauthProviderStub struct {
	state, challenge string
	code, verifier   string
	identity         account.ExternalIdentity
}

func (s *oauthProviderStub) AuthorizationURL(state, challenge string) string {
	s.state, s.challenge = state, challenge
	return "https://provider.test/authorize?state=" + url.QueryEscape(state)
}
func (s *oauthProviderStub) Identity(_ context.Context, code, verifier string) (account.ExternalIdentity, error) {
	s.code, s.verifier = code, verifier
	return s.identity, nil
}

type oauthLoginStub struct {
	identity account.ExternalIdentity
	refresh  string
}

func (s *oauthLoginStub) Login(_ context.Context, identity account.ExternalIdentity) (string, error) {
	s.identity = identity
	return s.refresh, nil
}

func TestOAuthRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	flow, _ := accountoauth.NewFlow("12345678901234567890123456789012", 10*time.Minute)
	provider := &oauthProviderStub{identity: account.ExternalIdentity{Provider: "google", ProviderUserID: "42"}}
	login := &oauthLoginStub{refresh: "initial-refresh"}
	config := OAuthRoutesConfig{
		Providers: map[string]OAuthProvider{"google": provider}, Login: login, Flow: flow, FrontendURL: "http://frontend.test",
		StateCookiePath: "/api/v1/auth", RefreshCookies: CookieConfig{
			Path: "/api/v1/auth", MaxAge: 30 * 24 * time.Hour, SameSite: http.SameSiteLaxMode,
		},
	}
	router := gin.New()
	RegisterOAuthRoutes(router.Group("/api/v1"), config)

	startResponse := httptest.NewRecorder()
	router.ServeHTTP(startResponse, httptest.NewRequest(http.MethodGet, "/api/v1/auth/google", nil))
	if startResponse.Code != http.StatusFound || provider.state == "" || provider.challenge == "" {
		t.Fatalf("unexpected start response: %d %s", startResponse.Code, startResponse.Header().Get("Location"))
	}
	startCookies := startResponse.Result().Cookies()
	if len(startCookies) != 1 || !startCookies[0].HttpOnly || startCookies[0].Path != config.StateCookiePath+"/google" {
		t.Fatalf("unexpected flow cookie: %+v", startCookies)
	}

	callback := httptest.NewRequest(http.MethodGet, "/api/v1/auth/google/callback?code=code&state="+url.QueryEscape(provider.state), nil)
	callback.AddCookie(startCookies[0])
	callbackResponse := httptest.NewRecorder()
	router.ServeHTTP(callbackResponse, callback)
	if callbackResponse.Code != http.StatusFound || callbackResponse.Header().Get("Location") != "http://frontend.test/auth/callback" {
		t.Fatalf("unexpected callback: %d %s", callbackResponse.Code, callbackResponse.Header().Get("Location"))
	}
	if provider.code != "code" || provider.verifier == "" || login.identity.ProviderUserID != "42" {
		t.Fatal("OAuth callback did not exchange and persist the identity")
	}
	var refreshFound, flowCleared bool
	for _, cookie := range callbackResponse.Result().Cookies() {
		refreshFound = refreshFound || cookie.Name == refreshCookieName && cookie.Value == "initial-refresh" && cookie.HttpOnly
		flowCleared = flowCleared || cookie.Name == oauthFlowCookiePrefix+"google" && cookie.MaxAge < 0
	}
	if !refreshFound || !flowCleared {
		t.Fatalf("callback cookies are incomplete: %+v", callbackResponse.Result().Cookies())
	}
}

func TestOAuthCallbackRejectsInvalidState(t *testing.T) {
	flow, _ := accountoauth.NewFlow("12345678901234567890123456789012", time.Minute)
	provider := &oauthProviderStub{}
	login := &oauthLoginStub{refresh: "must-not-be-set"}
	router := gin.New()
	RegisterOAuthRoutes(router.Group("/api/v1"), OAuthRoutesConfig{
		Providers: map[string]OAuthProvider{"google": provider}, Login: login, Flow: flow,
		FrontendURL: "http://frontend.test", StateCookiePath: "/api/v1/auth",
	})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/google/callback?code=code&state=bad", nil)
	request.AddCookie(&http.Cookie{Name: oauthFlowCookiePrefix + "google", Value: "tampered"})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusFound || response.Header().Get("Location") != "http://frontend.test/auth/error" || login.identity.Provider != "" {
		t.Fatalf("invalid state was accepted: %d %s", response.Code, response.Header().Get("Location"))
	}
}

func TestOAuthProviderCanBeDisabled(t *testing.T) {
	router := gin.New()
	RegisterOAuthRoutes(router.Group("/api/v1"), OAuthRoutesConfig{})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/auth/google", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", response.Code)
	}
}
