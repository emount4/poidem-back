package httpv1

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/emount4/poidem-back/internal/account"
	"github.com/gin-gonic/gin"
)

type authenticatorStub struct {
	principal account.Principal
	err       error
	token     string
}

func (s *authenticatorStub) Authenticate(_ context.Context, token string) (account.Principal, error) {
	s.token = token
	return s.principal, s.err
}

func TestRequireAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name       string
		header     string
		authError  error
		wantStatus int
		wantCode   string
	}{
		{name: "missing header", wantStatus: http.StatusUnauthorized, wantCode: "UNAUTHORIZED"},
		{name: "malformed header", header: "Basic token", wantStatus: http.StatusUnauthorized, wantCode: "UNAUTHORIZED"},
		{name: "invalid token", header: "Bearer token", authError: account.ErrUnauthorized, wantStatus: http.StatusUnauthorized, wantCode: "UNAUTHORIZED"},
		{name: "banned user", header: "Bearer token", authError: account.ErrUserBanned, wantStatus: http.StatusForbidden, wantCode: "USER_BANNED"},
		{name: "internal error", header: "Bearer token", authError: errors.New("database secret"), wantStatus: http.StatusInternalServerError, wantCode: "INTERNAL_ERROR"},
		{name: "valid", header: "bearer token", wantStatus: http.StatusNoContent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			authenticator := &authenticatorStub{
				principal: account.Principal{UserID: 5, SessionID: 8, ProfileComplete: true},
				err:       tt.authError,
			}
			router := gin.New()
			router.GET("/protected", RequireAuthentication(authenticator), func(c *gin.Context) {
				principal, ok := account.PrincipalFromContext(c.Request.Context())
				if !ok || principal.UserID != 5 {
					c.Status(http.StatusInternalServerError)
					return
				}
				c.Status(http.StatusNoContent)
			})
			request := httptest.NewRequest(http.MethodGet, "/protected", nil)
			if tt.header != "" {
				request.Header.Set("Authorization", tt.header)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d: %s", response.Code, tt.wantStatus, response.Body.String())
			}
			if tt.wantCode != "" {
				if got := errorCode(t, response); got != tt.wantCode {
					t.Fatalf("code = %q, want %q", got, tt.wantCode)
				}
			}
			if strings.Contains(response.Body.String(), "database secret") {
				t.Fatal("internal error leaked into response")
			}
			if tt.wantStatus == http.StatusNoContent && authenticator.token != "token" {
				t.Fatalf("token = %q, want token", authenticator.token)
			}
		})
	}
}

func TestRequireCompleteProfile(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tt := range []struct {
		name       string
		principal  *account.Principal
		wantStatus int
		wantCode   string
	}{
		{name: "missing principal", wantStatus: http.StatusUnauthorized, wantCode: "UNAUTHORIZED"},
		{name: "incomplete", principal: &account.Principal{UserID: 1}, wantStatus: http.StatusForbidden, wantCode: "PROFILE_INCOMPLETE"},
		{name: "complete", principal: &account.Principal{UserID: 1, ProfileComplete: true}, wantStatus: http.StatusNoContent},
	} {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			middlewares := []gin.HandlerFunc{}
			if tt.principal != nil {
				middlewares = append(middlewares, func(c *gin.Context) {
					c.Request = c.Request.WithContext(account.WithPrincipal(c.Request.Context(), *tt.principal))
					c.Next()
				})
			}
			middlewares = append(middlewares, RequireCompleteProfile(), func(c *gin.Context) {
				c.Status(http.StatusNoContent)
			})
			router.GET("/profile", middlewares...)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/profile", nil))
			if response.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, tt.wantStatus)
			}
			if tt.wantCode != "" && errorCode(t, response) != tt.wantCode {
				t.Fatalf("unexpected response: %s", response.Body.String())
			}
		})
	}
}

func errorCode(t *testing.T, response *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid error response: %v", err)
	}
	return body.Error.Code
}
