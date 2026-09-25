// Package httpv1 exposes report use cases through API v1.
package httpv1

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/emount4/poidem-back/internal/account"
	accounthttp "github.com/emount4/poidem-back/internal/account/httpv1"
	"github.com/emount4/poidem-back/internal/api/apierr"
	"github.com/emount4/poidem-back/internal/api/pagination"
	"github.com/emount4/poidem-back/internal/reports"
	"github.com/gin-gonic/gin"
)

const maxReportBodyBytes = 64 << 10

type Reports interface {
	Create(context.Context, int64, reports.CreateInput) (reports.Report, error)
	ListAdmin(context.Context, string, reports.Page) ([]reports.Report, int64, error)
	GetAdmin(context.Context, int64) (reports.Report, error)
	Resolve(context.Context, int64, int64, string) (reports.Report, error)
}

func RegisterRoutes(routes *gin.RouterGroup, service Reports, authenticator accounthttp.Authenticator) {
	h := handler{reports: service}

	protected := routes.Group("")
	protected.Use(accounthttp.RequireAuthentication(authenticator))
	protected.POST("/reports", accounthttp.RequireCompleteProfile(), h.create)

	admin := routes.Group("/admin")
	admin.Use(accounthttp.RequireAuthentication(authenticator), accounthttp.RequireAdmin())
	admin.GET("/reports", h.listAdmin)
	admin.GET("/reports/:reportId", h.getAdmin)
	admin.POST("/reports/:reportId/:action", h.resolve)
}

type handler struct{ reports Reports }

func (h handler) create(c *gin.Context) {
	var request reportInputRequest
	if fields := decodeJSON(c, &request); len(fields) > 0 {
		apierr.WriteValidation(c, fields)
		return
	}
	principal, _ := account.PrincipalFromContext(c.Request.Context())
	item, err := h.reports.Create(c.Request.Context(), principal.UserID, reports.CreateInput{
		TargetType: request.TargetType, TargetID: request.TargetID,
		Reason: request.Reason, Description: request.Description,
	})
	if h.writeError(c, err) {
		return
	}
	c.JSON(http.StatusCreated, newReportResponse(item))
}

func (h handler) listAdmin(c *gin.Context) {
	page, fields := pagination.Parse(c.Request.URL.Query())
	status, statusFields := statusQuery(c)
	if fields == nil {
		fields = apierr.FieldErrors{}
	}
	for key, value := range statusFields {
		fields[key] = value
	}
	if len(fields) > 0 {
		apierr.WriteValidation(c, fields)
		return
	}
	items, total, err := h.reports.ListAdmin(c.Request.Context(), status, reports.Page{Offset: page.Offset(), Limit: page.Limit})
	if h.writeError(c, err) {
		return
	}
	c.JSON(http.StatusOK, pagination.NewResponse(mapReports(items), page, total))
}

func (h handler) getAdmin(c *gin.Context) {
	reportID, ok := positiveReportID(c)
	if !ok {
		return
	}
	item, err := h.reports.GetAdmin(c.Request.Context(), reportID)
	if h.writeError(c, err) {
		return
	}
	c.JSON(http.StatusOK, newReportResponse(item))
}

func (h handler) resolve(c *gin.Context) {
	reportID, ok := positiveReportID(c)
	if !ok {
		return
	}
	action := c.Param("action")
	if action != "resolve" && action != "reject" {
		apierr.Write(c, http.StatusNotFound, "REPORT_NOT_FOUND", "Жалоба не найдена", nil)
		return
	}
	principal, _ := account.PrincipalFromContext(c.Request.Context())
	item, err := h.reports.Resolve(c.Request.Context(), reportID, principal.UserID, action)
	if h.writeError(c, err) {
		return
	}
	c.JSON(http.StatusOK, newReportResponse(item))
}

func (h handler) writeError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	var validation *reports.ValidationError
	switch {
	case errors.As(err, &validation):
		apierr.WriteValidation(c, validation.Fields)
	case errors.Is(err, reports.ErrTargetNotFound):
		apierr.Write(c, http.StatusNotFound, "REPORT_TARGET_NOT_FOUND", "Объект жалобы не найден", nil)
	case errors.Is(err, reports.ErrNotFound):
		apierr.Write(c, http.StatusNotFound, "REPORT_NOT_FOUND", "Жалоба не найдена", nil)
	case errors.Is(err, reports.ErrAlreadyResolved):
		apierr.Write(c, http.StatusConflict, "REPORT_ALREADY_RESOLVED", "Жалоба уже обработана", nil)
	default:
		apierr.WriteInternal(c, err)
	}
	return true
}

type reportInputRequest struct {
	TargetType  string  `json:"targetType"`
	TargetID    int64   `json:"targetId"`
	Reason      string  `json:"reason"`
	Description *string `json:"description"`
}

type userShortResponse struct {
	ID        int64   `json:"id"`
	FirstName string  `json:"firstName"`
	LastName  *string `json:"lastName"`
	AvatarURL *string `json:"avatarUrl"`
}

type reportResponse struct {
	ID          int64              `json:"id"`
	TargetType  string             `json:"targetType"`
	TargetID    int64              `json:"targetId"`
	Reason      string             `json:"reason"`
	Description *string            `json:"description"`
	Author      userShortResponse  `json:"author"`
	Status      string             `json:"status"`
	ResolvedBy  *userShortResponse `json:"resolvedBy"`
	CreatedAt   time.Time          `json:"createdAt"`
	ResolvedAt  *time.Time         `json:"resolvedAt"`
}

func newReportResponse(item reports.Report) reportResponse {
	response := reportResponse{
		ID: item.ID, TargetType: item.TargetType, TargetID: item.TargetID,
		Reason: item.Reason, Description: item.Description,
		Author: userShortResponse{
			ID: item.Author.ID, FirstName: item.Author.FirstName,
			LastName: item.Author.LastName, AvatarURL: item.Author.AvatarURL,
		},
		Status: item.Status, CreatedAt: item.CreatedAt, ResolvedAt: item.ResolvedAt,
	}
	if item.ResolvedBy != nil {
		response.ResolvedBy = &userShortResponse{
			ID: item.ResolvedBy.ID, FirstName: item.ResolvedBy.FirstName,
			LastName: item.ResolvedBy.LastName, AvatarURL: item.ResolvedBy.AvatarURL,
		}
	}
	return response
}

func mapReports(items []reports.Report) []reportResponse {
	result := make([]reportResponse, 0, len(items))
	for _, item := range items {
		result = append(result, newReportResponse(item))
	}
	return result
}

func decodeJSON(c *gin.Context, target any) apierr.FieldErrors {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxReportBodyBytes)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return apierr.FieldErrors{"body": {"Некорректный JSON"}}
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return apierr.FieldErrors{"body": {"Ожидается один JSON-объект"}}
	}
	return apierr.FieldErrors{}
}

func positiveReportID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("reportId"), 10, 64)
	if err != nil || id <= 0 {
		apierr.Write(c, http.StatusNotFound, "REPORT_NOT_FOUND", "Жалоба не найдена", nil)
		return 0, false
	}
	return id, true
}

func statusQuery(c *gin.Context) (string, apierr.FieldErrors) {
	raw, ok := c.Request.URL.Query()["status"]
	if !ok {
		return "", apierr.FieldErrors{}
	}
	if len(raw) != 1 {
		return "", apierr.FieldErrors{"status": {"Параметр должен быть указан один раз"}}
	}
	for _, allowed := range []string{reports.StatusPending, reports.StatusResolved, reports.StatusRejected} {
		if raw[0] == allowed {
			return raw[0], apierr.FieldErrors{}
		}
	}
	return "", apierr.FieldErrors{"status": {"Недопустимое значение"}}
}
