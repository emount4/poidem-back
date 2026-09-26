package httpv1

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/emount4/poidem-back/internal/account"
	"github.com/gin-gonic/gin"
)

type credentialsStub struct {
	result account.AuthResult
	err    error
}

func (s *credentialsStub) Register(context.Context, string, string) (account.AuthResult, error) {
	return s.result, s.err
}
func (s *credentialsStub) Login(context.Context, string, string) (account.AuthResult, error) {
	return s.result, s.err
}

func TestCredentialRoutesReturnTokenUserAndCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &credentialsStub{result: account.AuthResult{
		AccessToken: "access", RefreshToken: "refresh",
		User: account.Profile{ID: 1, Interests: []account.DictionaryItem{}, Role: account.RoleUser, Status: account.StatusActive, CreatedAt: time.Now()},
	}}
	router := gin.New()
	RegisterCredentialRoutes(router.Group("/api/v1"), service, CookieConfig{Path: "/api/v1/auth", MaxAge: time.Hour, SameSite: http.SameSiteLaxMode})
	for _, test := range []struct {
		path string
		want int
	}{{"/api/v1/auth/register", http.StatusCreated}, {"/api/v1/auth/login", http.StatusOK}} {
		request := httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(`{"username":"danila","password":"password123"}`))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != test.want || !strings.Contains(response.Body.String(), `"accessToken":"access"`) || !strings.Contains(response.Body.String(), `"user"`) {
			t.Fatalf("%s: %d %s", test.path, response.Code, response.Body.String())
		}
		cookies := response.Result().Cookies()
		if len(cookies) != 1 || cookies[0].Name != refreshCookieName || cookies[0].Value != "refresh" || !cookies[0].HttpOnly {
			t.Fatalf("unexpected cookie: %+v", cookies)
		}
	}
}

func TestCredentialRouteErrors(t *testing.T) {
	for _, test := range []struct {
		err  error
		want int
		code string
	}{{account.ErrUsernameTaken, http.StatusConflict, "USERNAME_TAKEN"}, {account.ErrInvalidCredentials, http.StatusUnauthorized, "INVALID_CREDENTIALS"}, {account.ErrUserBanned, http.StatusForbidden, "USER_BANNED"}} {
		router := gin.New()
		RegisterCredentialRoutes(router.Group("/api/v1"), &credentialsStub{err: test.err}, CookieConfig{})
		request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"danila","password":"password123"}`))
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != test.want || !strings.Contains(response.Body.String(), test.code) {
			t.Fatalf("err=%v status=%d body=%s", test.err, response.Code, response.Body.String())
		}
	}
}
