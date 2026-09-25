package httpv1

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/emount4/poidem-back/internal/account"
	accounthttp "github.com/emount4/poidem-back/internal/account/httpv1"
	"github.com/emount4/poidem-back/internal/api/apierr"
	"github.com/emount4/poidem-back/internal/api/ratelimit"
	"github.com/emount4/poidem-back/internal/events"
	"github.com/gin-gonic/gin"
)

const maxCoverRequestBytes = events.MaxCoverBytes + 1<<20

type Covers interface {
	Upload(context.Context, int64, io.Reader) (events.CoverUpload, error)
}

func RegisterCoverRoutes(routes *gin.RouterGroup, covers Covers, authenticator accounthttp.Authenticator) {
	h := coverHandler{covers: covers}
	protected := routes.Group("")
	protected.Use(accounthttp.RequireAuthentication(authenticator), accounthttp.RequireCompleteProfile())
	uploadLimiter := ratelimit.New(20, time.Hour)
	protected.POST("/uploads/events", uploadLimiter.Middleware(), h.upload)
}

type coverHandler struct{ covers Covers }

func (h coverHandler) upload(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxCoverRequestBytes)
	header, err := c.FormFile("file")
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			apierr.Write(c, http.StatusRequestEntityTooLarge, "EVENT_COVER_TOO_LARGE", "Обложка превышает 10 МБ", nil)
			return
		}
		apierr.WriteValidation(c, apierr.FieldErrors{"file": {"Файл обязателен"}})
		return
	}
	if header.Size > events.MaxCoverBytes {
		apierr.Write(c, http.StatusRequestEntityTooLarge, "EVENT_COVER_TOO_LARGE", "Обложка превышает 10 МБ", nil)
		return
	}
	file, err := header.Open()
	if err != nil {
		apierr.Write(c, http.StatusInternalServerError, "EVENT_COVER_UPLOAD_FAILED", "Не удалось загрузить обложку", nil)
		return
	}
	defer file.Close()
	principal, _ := account.PrincipalFromContext(c.Request.Context())
	upload, err := h.covers.Upload(c.Request.Context(), principal.UserID, file)
	if h.writeError(c, err) {
		return
	}
	c.JSON(http.StatusCreated, gin.H{"url": upload.PublicURL})
}

func (h coverHandler) writeError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, events.ErrCoverTooLarge):
		apierr.Write(c, http.StatusRequestEntityTooLarge, "EVENT_COVER_TOO_LARGE", "Обложка превышает 10 МБ", nil)
	case errors.Is(err, events.ErrUnsupportedCoverType):
		apierr.Write(c, http.StatusUnsupportedMediaType, "UNSUPPORTED_EVENT_COVER_TYPE", "Поддерживаются JPEG, PNG и WebP", nil)
	case errors.Is(err, events.ErrCoverUnavailable):
		apierr.Write(c, http.StatusServiceUnavailable, "EVENT_COVER_UPLOAD_FAILED", "Хранилище обложек временно недоступно", nil)
	default:
		apierr.Write(c, http.StatusInternalServerError, "EVENT_COVER_UPLOAD_FAILED", "Не удалось обработать обложку", nil)
	}
	return true
}
