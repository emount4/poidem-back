package httpv1

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/emount4/poidem-back/internal/account"
	"github.com/emount4/poidem-back/internal/api/apierr"
	"github.com/emount4/poidem-back/internal/api/ratelimit"
	"github.com/gin-gonic/gin"
)

const maxCredentialsBodyBytes = 16 << 10

type Credentials interface {
	Register(context.Context, string, string) (account.AuthResult, error)
	Login(context.Context, string, string) (account.AuthResult, error)
}

func RegisterCredentialRoutes(routes *gin.RouterGroup, credentials Credentials, cookies CookieConfig) {
	h := credentialsHandler{credentials: credentials, cookies: cookies}
	registerLimiter := ratelimit.New(10, time.Hour)
	loginLimiter := ratelimit.New(30, 15*time.Minute)
	routes.POST("/auth/register", registerLimiter.Middleware(), h.register)
	routes.POST("/auth/login", loginLimiter.Middleware(), h.login)
}

type credentialsHandler struct {
	credentials Credentials
	cookies     CookieConfig
}

type credentialsRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (h credentialsHandler) register(c *gin.Context) {
	request, ok := decodeCredentials(c)
	if !ok {
		return
	}
	result, err := h.credentials.Register(c.Request.Context(), request.Username, request.Password)
	if h.writeError(c, err) {
		return
	}
	h.cookies.setRefresh(c, result.RefreshToken)
	c.JSON(http.StatusCreated, gin.H{"accessToken": result.AccessToken, "user": newProfileResponse(result.User)})
}

func (h credentialsHandler) login(c *gin.Context) {
	request, ok := decodeCredentials(c)
	if !ok {
		return
	}
	result, err := h.credentials.Login(c.Request.Context(), request.Username, request.Password)
	if h.writeError(c, err) {
		return
	}
	h.cookies.setRefresh(c, result.RefreshToken)
	c.JSON(http.StatusOK, gin.H{"accessToken": result.AccessToken, "user": newProfileResponse(result.User)})
}

func (h credentialsHandler) writeError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	var validation *account.ValidationError
	switch {
	case errors.As(err, &validation):
		apierr.WriteValidation(c, validation.Fields)
	case errors.Is(err, account.ErrUsernameTaken):
		apierr.Write(c, http.StatusConflict, "USERNAME_TAKEN", "Этот логин уже занят", nil)
	case errors.Is(err, account.ErrInvalidCredentials):
		apierr.Write(c, http.StatusUnauthorized, "INVALID_CREDENTIALS", "Неверный логин или пароль", nil)
	case errors.Is(err, account.ErrUserBanned):
		apierr.Write(c, http.StatusForbidden, "USER_BANNED", "Пользователь заблокирован", nil)
	default:
		apierr.WriteInternal(c, err)
	}
	return true
}

func decodeCredentials(c *gin.Context) (credentialsRequest, bool) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxCredentialsBodyBytes)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	var request credentialsRequest
	if err := decoder.Decode(&request); err != nil {
		apierr.WriteValidation(c, apierr.FieldErrors{"body": {"Некорректный JSON"}})
		return credentialsRequest{}, false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		apierr.WriteValidation(c, apierr.FieldErrors{"body": {"Ожидается один JSON-объект"}})
		return credentialsRequest{}, false
	}
	return request, true
}
