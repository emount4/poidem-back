package httpv1

import (
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/emount4/poidem-back/internal/account"
	"github.com/emount4/poidem-back/internal/api/apierr"
	"github.com/gin-gonic/gin"
)

const maxAvatarRequestBytes = account.MaxAvatarBytes + 1<<20

type Avatars interface {
	Upload(context.Context, int64, io.Reader) (string, error)
	Delete(context.Context, int64) error
}

func RegisterAvatarRoutes(routes *gin.RouterGroup, avatars Avatars, authenticator Authenticator) {
	h := avatarHandler{avatars: avatars}
	protected := routes.Group("")
	protected.Use(RequireAuthentication(authenticator))
	protected.POST("/users/me/avatar", h.upload)
	protected.DELETE("/users/me/avatar", h.delete)
}

type avatarHandler struct{ avatars Avatars }

func (h avatarHandler) upload(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxAvatarRequestBytes)
	header, err := c.FormFile("file")
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			apierr.Write(c, http.StatusRequestEntityTooLarge, "AVATAR_TOO_LARGE", "Файл аватара превышает 5 МБ", nil)
			return
		}
		apierr.WriteValidation(c, apierr.FieldErrors{"file": {"Файл обязателен"}})
		return
	}
	if header.Size > account.MaxAvatarBytes {
		apierr.Write(c, http.StatusRequestEntityTooLarge, "AVATAR_TOO_LARGE", "Файл аватара превышает 5 МБ", nil)
		return
	}
	file, err := header.Open()
	if err != nil {
		apierr.Write(c, http.StatusInternalServerError, "AVATAR_UPLOAD_FAILED", "Не удалось загрузить аватар", nil)
		return
	}
	defer file.Close()
	principal, _ := account.PrincipalFromContext(c.Request.Context())
	avatarURL, err := h.avatars.Upload(c.Request.Context(), principal.UserID, file)
	if h.writeError(c, err) {
		return
	}
	c.JSON(http.StatusOK, gin.H{"avatarUrl": avatarURL})
}

func (h avatarHandler) delete(c *gin.Context) {
	principal, _ := account.PrincipalFromContext(c.Request.Context())
	if h.writeError(c, h.avatars.Delete(c.Request.Context(), principal.UserID)) {
		return
	}
	c.Status(http.StatusNoContent)
}

func (h avatarHandler) writeError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, account.ErrAvatarTooLarge):
		apierr.Write(c, http.StatusRequestEntityTooLarge, "AVATAR_TOO_LARGE", "Файл аватара превышает 5 МБ", nil)
	case errors.Is(err, account.ErrUnsupportedAvatarType):
		apierr.Write(c, http.StatusUnsupportedMediaType, "UNSUPPORTED_AVATAR_TYPE", "Поддерживаются JPEG, PNG и WebP", nil)
	case errors.Is(err, account.ErrAvatarStorageUnavailable):
		apierr.Write(c, http.StatusServiceUnavailable, "AVATAR_UPLOAD_FAILED", "Хранилище аватаров временно недоступно", nil)
	default:
		apierr.Write(c, http.StatusInternalServerError, "AVATAR_UPLOAD_FAILED", "Не удалось обработать аватар", nil)
	}
	return true
}
