package httpv1

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/emount4/poidem-back/internal/account"
	"github.com/gin-gonic/gin"
)

type avatarsStub struct {
	userID int64
	err    error
}

func (s *avatarsStub) Upload(_ context.Context, userID int64, _ io.Reader) (string, error) {
	s.userID = userID
	return "http://localhost:9000/poidem-media/avatars/7/avatar.png", s.err
}
func (s *avatarsStub) Delete(_ context.Context, userID int64) error {
	s.userID = userID
	return s.err
}

func TestAvatarRoutes(t *testing.T) {
	service := &avatarsStub{}
	router := avatarRouter(service)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	file, err := writer.CreateFormFile("file", "avatar.png")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.Write([]byte("\x89PNG\r\n\x1a\ncontent"))
	_ = writer.Close()

	request := httptest.NewRequest(http.MethodPost, "/api/v1/users/me/avatar", &body)
	request.Header.Set("Authorization", "Bearer access")
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || service.userID != 7 || !bytes.Contains(response.Body.Bytes(), []byte("avatarUrl")) {
		t.Fatalf("status=%d body=%s user=%d", response.Code, response.Body.String(), service.userID)
	}

	request = httptest.NewRequest(http.MethodDelete, "/api/v1/users/me/avatar", nil)
	request.Header.Set("Authorization", "Bearer access")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || service.userID != 7 {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestAvatarErrorMapping(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
	}{
		{account.ErrAvatarTooLarge, http.StatusRequestEntityTooLarge},
		{account.ErrUnsupportedAvatarType, http.StatusUnsupportedMediaType},
		{account.ErrAvatarStorageUnavailable, http.StatusServiceUnavailable},
		{account.ErrAvatarUpload, http.StatusInternalServerError},
	} {
		service := &avatarsStub{err: tc.err}
		request := httptest.NewRequest(http.MethodDelete, "/api/v1/users/me/avatar", nil)
		request.Header.Set("Authorization", "Bearer access")
		response := httptest.NewRecorder()
		avatarRouter(service).ServeHTTP(response, request)
		if response.Code != tc.status {
			t.Fatalf("error=%v status=%d body=%s", tc.err, response.Code, response.Body.String())
		}
	}
}

func TestAvatarUploadRequiresFile(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/users/me/avatar", nil)
	request.Header.Set("Authorization", "Bearer access")
	response := httptest.NewRecorder()
	avatarRouter(&avatarsStub{}).ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func avatarRouter(service Avatars) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterAvatarRoutes(router.Group("/api/v1"), service, &authenticatorStub{
		principal: account.Principal{UserID: 7, Role: account.RoleUser},
	})
	return router
}
