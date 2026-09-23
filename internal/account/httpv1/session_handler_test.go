package httpv1

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/emount4/poidem-back/internal/account"
	"github.com/gin-gonic/gin"
)

type sessionsStub struct {
	pair                 account.TokenPair
	err                  error
	refreshed, loggedOut string
}

func (s *sessionsStub) Refresh(_ context.Context, token string) (account.TokenPair, error) {
	s.refreshed = token
	return s.pair, s.err
}
func (s *sessionsStub) Logout(_ context.Context, token string) error {
	s.loggedOut = token
	return s.err
}

func TestSessionRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sessions := &sessionsStub{pair: account.TokenPair{AccessToken: "access", RefreshToken: "next"}}
	router := gin.New()
	RegisterSessionRoutes(router.Group("/api/v1"), sessions, CookieConfig{Path: "/api/v1/auth", MaxAge: 30 * 24 * time.Hour, SameSite: http.SameSiteLaxMode})

	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	request.AddCookie(&http.Cookie{Name: refreshCookieName, Value: "current"})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != `{"accessToken":"access"}` || sessions.refreshed != "current" {
		t.Fatalf("unexpected refresh: %d %s", response.Code, response.Body.String())
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Value != "next" || !cookies[0].HttpOnly || cookies[0].Path != "/api/v1/auth" || cookies[0].SameSite != http.SameSiteLaxMode {
		t.Fatalf("unexpected cookie: %+v", cookies)
	}

	logout := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	logout.AddCookie(&http.Cookie{Name: refreshCookieName, Value: "next"})
	logoutResponse := httptest.NewRecorder()
	router.ServeHTTP(logoutResponse, logout)
	if logoutResponse.Code != http.StatusNoContent || sessions.loggedOut != "next" {
		t.Fatalf("unexpected logout: %d", logoutResponse.Code)
	}
	if got := logoutResponse.Result().Cookies(); len(got) != 1 || got[0].MaxAge >= 0 {
		t.Fatalf("cookie was not cleared: %+v", got)
	}
}

func TestRefreshUnauthorized(t *testing.T) {
	router := gin.New()
	RegisterSessionRoutes(router.Group("/api/v1"), &sessionsStub{err: account.ErrUnauthorized}, CookieConfig{Path: "/api/v1/auth"})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	request.AddCookie(&http.Cookie{Name: refreshCookieName, Value: "invalid"})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestRefreshInternalError(t *testing.T) {
	router := gin.New()
	RegisterSessionRoutes(router.Group("/api/v1"), &sessionsStub{err: errors.New("database secret")}, CookieConfig{Path: "/api/v1/auth"})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	request.AddCookie(&http.Cookie{Name: refreshCookieName, Value: "valid"})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", response.Code)
	}
}
