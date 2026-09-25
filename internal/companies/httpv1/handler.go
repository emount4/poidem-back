// Package httpv1 exposes company use cases through API v1.
package httpv1

import (
	"bytes"
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
	"github.com/emount4/poidem-back/internal/companies"
	"github.com/gin-gonic/gin"
)

const maxCompanyBodyBytes = 64 << 10

type Companies interface {
	Create(context.Context, int64, int64, companies.CreateInput) (companies.Company, error)
	ListEvent(context.Context, int64, companies.Viewer, companies.Page) ([]companies.Company, int64, error)
	GetVisible(context.Context, int64, companies.Viewer) (companies.Company, error)
	ListMembers(context.Context, int64, companies.Viewer, companies.Page) ([]companies.UserShort, int64, error)
	ListMine(context.Context, int64, companies.Page) ([]companies.Company, int64, error)
	Update(context.Context, int64, int64, companies.Patch) (companies.Company, error)
	SetRecruitment(context.Context, int64, int64, bool) (companies.Company, error)
	Delete(context.Context, int64, int64) error
}

func RegisterRoutes(routes *gin.RouterGroup, service Companies, authenticator accounthttp.Authenticator) {
	h := handler{companies: service}
	optional := routes.Group("")
	optional.Use(accounthttp.OptionalAuthentication(authenticator))
	optional.GET("/events/:eventId/companies", h.listEvent)
	optional.GET("/companies/:companyId", h.getVisible)
	optional.GET("/companies/:companyId/members", h.listMembers)

	protected := routes.Group("")
	protected.Use(accounthttp.RequireAuthentication(authenticator))
	protected.GET("/users/me/companies", h.listMine)
	protected.POST("/events/:eventId/companies", accounthttp.RequireCompleteProfile(), h.create)
	protected.PATCH("/companies/:companyId", h.update)
	protected.DELETE("/companies/:companyId", h.delete)
	protected.POST("/companies/:companyId/close", h.closeRecruitment)
	protected.POST("/companies/:companyId/open", h.openRecruitment)
}

type handler struct{ companies Companies }

func (h handler) create(c *gin.Context) {
	eventID, ok := positivePathID(c, "eventId", "EVENT_NOT_FOUND", "Событие не найдено")
	if !ok {
		return
	}
	principal, _ := account.PrincipalFromContext(c.Request.Context())
	var request companyInputRequest
	if fields := decodeJSON(c, &request); len(fields) > 0 {
		apierr.WriteValidation(c, fields)
		return
	}
	item, err := h.companies.Create(c.Request.Context(), eventID, principal.UserID, companies.CreateInput{
		Name: request.Name, Description: request.Description, MaxMembers: request.MaxMembers,
		JoinType: request.JoinType, Rules: request.Rules,
	})
	if h.writeError(c, err) {
		return
	}
	c.JSON(http.StatusCreated, newCompanyResponse(item))
}

func (h handler) listEvent(c *gin.Context) {
	eventID, ok := positivePathID(c, "eventId", "EVENT_NOT_FOUND", "Событие не найдено")
	if !ok {
		return
	}
	page, fields := pagination.Parse(c.Request.URL.Query())
	if len(fields) > 0 {
		apierr.WriteValidation(c, fields)
		return
	}
	items, total, err := h.companies.ListEvent(c.Request.Context(), eventID, viewer(c), companies.Page{Offset: page.Offset(), Limit: page.Limit})
	if h.writeError(c, err) {
		return
	}
	c.JSON(http.StatusOK, pagination.NewResponse(mapCompanies(items), page, total))
}

func (h handler) getVisible(c *gin.Context) {
	companyID, ok := positivePathID(c, "companyId", "COMPANY_NOT_FOUND", "Компания не найдена")
	if !ok {
		return
	}
	item, err := h.companies.GetVisible(c.Request.Context(), companyID, viewer(c))
	if h.writeError(c, err) {
		return
	}
	c.JSON(http.StatusOK, newCompanyResponse(item))
}

func (h handler) listMembers(c *gin.Context) {
	companyID, ok := positivePathID(c, "companyId", "COMPANY_NOT_FOUND", "Компания не найдена")
	if !ok {
		return
	}
	page, fields := pagination.Parse(c.Request.URL.Query())
	if len(fields) > 0 {
		apierr.WriteValidation(c, fields)
		return
	}
	items, total, err := h.companies.ListMembers(c.Request.Context(), companyID, viewer(c), companies.Page{Offset: page.Offset(), Limit: page.Limit})
	if h.writeError(c, err) {
		return
	}
	responses := make([]userShortResponse, 0, len(items))
	for _, item := range items {
		responses = append(responses, newUserShortResponse(item))
	}
	c.JSON(http.StatusOK, pagination.NewResponse(responses, page, total))
}

func (h handler) listMine(c *gin.Context) {
	principal, _ := account.PrincipalFromContext(c.Request.Context())
	page, fields := pagination.Parse(c.Request.URL.Query())
	if len(fields) > 0 {
		apierr.WriteValidation(c, fields)
		return
	}
	items, total, err := h.companies.ListMine(c.Request.Context(), principal.UserID, companies.Page{Offset: page.Offset(), Limit: page.Limit})
	if h.writeError(c, err) {
		return
	}
	c.JSON(http.StatusOK, pagination.NewResponse(mapCompanies(items), page, total))
}

func (h handler) update(c *gin.Context) {
	companyID, ok := positivePathID(c, "companyId", "COMPANY_NOT_FOUND", "Компания не найдена")
	if !ok {
		return
	}
	principal, _ := account.PrincipalFromContext(c.Request.Context())
	var request companyPatchRequest
	if fields := decodeJSON(c, &request); len(fields) > 0 {
		apierr.WriteValidation(c, fields)
		return
	}
	patch, fields := request.patch()
	if len(fields) > 0 {
		apierr.WriteValidation(c, fields)
		return
	}
	item, err := h.companies.Update(c.Request.Context(), companyID, principal.UserID, patch)
	if h.writeError(c, err) {
		return
	}
	c.JSON(http.StatusOK, newCompanyResponse(item))
}

func (h handler) closeRecruitment(c *gin.Context) { h.setRecruitment(c, false) }

func (h handler) openRecruitment(c *gin.Context) { h.setRecruitment(c, true) }

func (h handler) setRecruitment(c *gin.Context, open bool) {
	companyID, ok := positivePathID(c, "companyId", "COMPANY_NOT_FOUND", "Компания не найдена")
	if !ok {
		return
	}
	principal, _ := account.PrincipalFromContext(c.Request.Context())
	item, err := h.companies.SetRecruitment(c.Request.Context(), companyID, principal.UserID, open)
	if h.writeError(c, err) {
		return
	}
	c.JSON(http.StatusOK, newCompanyResponse(item))
}

func (h handler) delete(c *gin.Context) {
	companyID, ok := positivePathID(c, "companyId", "COMPANY_NOT_FOUND", "Компания не найдена")
	if !ok {
		return
	}
	principal, _ := account.PrincipalFromContext(c.Request.Context())
	if h.writeError(c, h.companies.Delete(c.Request.Context(), companyID, principal.UserID)) {
		return
	}
	c.Status(http.StatusNoContent)
}

func (h handler) writeError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	var validation *companies.ValidationError
	switch {
	case errors.As(err, &validation):
		apierr.WriteValidation(c, validation.Fields)
	case errors.Is(err, companies.ErrEventNotFound):
		apierr.Write(c, http.StatusNotFound, "EVENT_NOT_FOUND", "Событие не найдено", nil)
	case errors.Is(err, companies.ErrNotFound):
		apierr.Write(c, http.StatusNotFound, "COMPANY_NOT_FOUND", "Компания не найдена", nil)
	case errors.Is(err, companies.ErrEventNotAvailable):
		apierr.Write(c, http.StatusConflict, "EVENT_NOT_AVAILABLE", "Событие недоступно для участия", nil)
	case errors.Is(err, companies.ErrAlreadyInEventCompany):
		apierr.Write(c, http.StatusConflict, "ALREADY_IN_EVENT_COMPANY", "Пользователь уже состоит в компании этого события", nil)
	case errors.Is(err, companies.ErrNotOwner):
		apierr.Write(c, http.StatusForbidden, "NOT_COMPANY_OWNER", "Действие доступно только владельцу компании", nil)
	case errors.Is(err, companies.ErrCompanyBlocked):
		apierr.Write(c, http.StatusConflict, "COMPANY_BLOCKED", "Компания заблокирована", nil)
	case errors.Is(err, companies.ErrCapacityBelowMembers):
		apierr.Write(c, http.StatusConflict, "COMPANY_CAPACITY_BELOW_MEMBERS", "Лимит меньше текущего состава компании", nil)
	default:
		apierr.WriteInternal(c, err)
	}
	return true
}

type companyInputRequest struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
	MaxMembers  int     `json:"maxMembers"`
	JoinType    string  `json:"joinType"`
	Rules       *string `json:"rules"`
}

type patchField[T any] struct {
	Present bool
	Null    bool
	Value   T
}

func (f *patchField[T]) UnmarshalJSON(data []byte) error {
	f.Present = true
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		f.Null = true
		return nil
	}
	return json.Unmarshal(data, &f.Value)
}

type companyPatchRequest struct {
	Name        patchField[string] `json:"name"`
	Description patchField[string] `json:"description"`
	MaxMembers  patchField[int]    `json:"maxMembers"`
	JoinType    patchField[string] `json:"joinType"`
	Rules       patchField[string] `json:"rules"`
}

func (r companyPatchRequest) patch() (companies.Patch, apierr.FieldErrors) {
	fields := apierr.FieldErrors{}
	if r.Name.Null {
		fields["name"] = []string{"Поле не может быть null"}
	}
	if r.MaxMembers.Null {
		fields["maxMembers"] = []string{"Поле не может быть null"}
	}
	if r.JoinType.Null {
		fields["joinType"] = []string{"Поле не может быть null"}
	}
	return companies.Patch{
		Name:        companies.Change[string]{Set: r.Name.Present, Value: r.Name.Value},
		Description: companies.NullableChange[string]{Set: r.Description.Present, Null: r.Description.Null, Value: r.Description.Value},
		MaxMembers:  companies.Change[int]{Set: r.MaxMembers.Present, Value: r.MaxMembers.Value},
		JoinType:    companies.Change[string]{Set: r.JoinType.Present, Value: r.JoinType.Value},
		Rules:       companies.NullableChange[string]{Set: r.Rules.Present, Null: r.Rules.Null, Value: r.Rules.Value},
	}, fields
}

type userShortResponse struct {
	ID        int64   `json:"id"`
	FirstName string  `json:"firstName"`
	LastName  *string `json:"lastName"`
	AvatarURL *string `json:"avatarUrl"`
}

type companyResponse struct {
	ID           int64             `json:"id"`
	EventID      int64             `json:"eventId"`
	Name         string            `json:"name"`
	Description  *string           `json:"description"`
	MaxMembers   int               `json:"maxMembers"`
	JoinType     string            `json:"joinType"`
	Rules        *string           `json:"rules"`
	Owner        userShortResponse `json:"owner"`
	MembersCount int64             `json:"membersCount"`
	Status       string            `json:"status"`
	CreatedAt    time.Time         `json:"createdAt"`
	UpdatedAt    time.Time         `json:"updatedAt"`
}

func newUserShortResponse(item companies.UserShort) userShortResponse {
	return userShortResponse{ID: item.ID, FirstName: item.FirstName, LastName: item.LastName, AvatarURL: item.AvatarURL}
}

func newCompanyResponse(item companies.Company) companyResponse {
	return companyResponse{
		ID: item.ID, EventID: item.EventID, Name: item.Name, Description: item.Description,
		MaxMembers: item.MaxMembers, JoinType: item.JoinType, Rules: item.Rules,
		Owner: newUserShortResponse(item.Owner), MembersCount: item.MembersCount,
		Status: item.Status, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
	}
}

func mapCompanies(items []companies.Company) []companyResponse {
	result := make([]companyResponse, 0, len(items))
	for _, item := range items {
		result = append(result, newCompanyResponse(item))
	}
	return result
}

func decodeJSON(c *gin.Context, target any) apierr.FieldErrors {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxCompanyBodyBytes)
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

func positivePathID(c *gin.Context, name, code, message string) (int64, bool) {
	id, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || id <= 0 {
		apierr.Write(c, http.StatusNotFound, code, message, nil)
		return 0, false
	}
	return id, true
}

func viewer(c *gin.Context) companies.Viewer {
	principal, ok := account.PrincipalFromContext(c.Request.Context())
	if !ok {
		return companies.Viewer{}
	}
	id := principal.UserID
	return companies.Viewer{UserID: &id, Admin: principal.Role == account.RoleAdmin}
}
