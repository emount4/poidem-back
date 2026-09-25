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
	ListAdmin(context.Context, companies.Page) ([]companies.Company, int64, error)
	Block(context.Context, int64) (companies.Company, error)
	Update(context.Context, int64, int64, companies.Patch) (companies.Company, error)
	SetRecruitment(context.Context, int64, int64, bool) (companies.Company, error)
	Delete(context.Context, int64, int64) error
	JoinOpen(context.Context, int64, int64) error
	Leave(context.Context, int64, int64) error
	RemoveMember(context.Context, int64, int64, int64) error
	CreateApplication(context.Context, int64, int64, companies.CreateApplicationInput) (companies.Application, error)
	GetMyApplication(context.Context, int64, int64) (companies.Application, error)
	ListApplications(context.Context, int64, int64, string, companies.Page) ([]companies.Application, int64, error)
	ListMyApplications(context.Context, int64, companies.Page) ([]companies.Application, int64, error)
	CancelApplication(context.Context, int64, int64) error
	ResolveApplication(context.Context, int64, int64, int64, string) (companies.Application, error)
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
	protected.GET("/users/me/applications", h.listMyApplications)
	protected.POST("/events/:eventId/companies", accounthttp.RequireCompleteProfile(), h.create)
	protected.PATCH("/companies/:companyId", h.update)
	protected.DELETE("/companies/:companyId", h.delete)
	protected.POST("/companies/:companyId/close", h.closeRecruitment)
	protected.POST("/companies/:companyId/open", h.openRecruitment)
	protected.POST("/companies/:companyId/join", accounthttp.RequireCompleteProfile(), h.joinOpen)
	protected.DELETE("/companies/:companyId/members/me", h.leave)
	protected.DELETE("/companies/:companyId/members/:userId", h.removeMember)
	protected.POST("/companies/:companyId/applications", accounthttp.RequireCompleteProfile(), h.createApplication)
	protected.GET("/companies/:companyId/applications/me", h.getMyApplication)
	protected.DELETE("/companies/:companyId/applications/me", h.cancelMyApplication)
	protected.GET("/companies/:companyId/applications", h.listApplications)
	protected.POST("/companies/:companyId/applications/:applicationId/:action", h.resolveApplication)

	admin := routes.Group("/admin")
	admin.Use(accounthttp.RequireAuthentication(authenticator), accounthttp.RequireAdmin())
	admin.GET("/companies", h.listAdmin)
	admin.POST("/companies/:companyId/block", h.block)
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

func (h handler) listAdmin(c *gin.Context) {
	page, fields := pagination.Parse(c.Request.URL.Query())
	if len(fields) > 0 {
		apierr.WriteValidation(c, fields)
		return
	}
	items, total, err := h.companies.ListAdmin(c.Request.Context(), companies.Page{Offset: page.Offset(), Limit: page.Limit})
	if h.writeError(c, err) {
		return
	}
	c.JSON(http.StatusOK, pagination.NewResponse(mapCompanies(items), page, total))
}

func (h handler) block(c *gin.Context) {
	companyID, ok := positivePathID(c, "companyId", "COMPANY_NOT_FOUND", "Компания не найдена")
	if !ok {
		return
	}
	item, err := h.companies.Block(c.Request.Context(), companyID)
	if h.writeError(c, err) {
		return
	}
	c.JSON(http.StatusOK, newCompanyResponse(item))
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

func (h handler) joinOpen(c *gin.Context) {
	companyID, ok := positivePathID(c, "companyId", "COMPANY_NOT_FOUND", "Компания не найдена")
	if !ok {
		return
	}
	principal, _ := account.PrincipalFromContext(c.Request.Context())
	if h.writeError(c, h.companies.JoinOpen(c.Request.Context(), companyID, principal.UserID)) {
		return
	}
	c.Status(http.StatusCreated)
}

func (h handler) leave(c *gin.Context) {
	companyID, ok := positivePathID(c, "companyId", "COMPANY_NOT_FOUND", "Компания не найдена")
	if !ok {
		return
	}
	principal, _ := account.PrincipalFromContext(c.Request.Context())
	if h.writeError(c, h.companies.Leave(c.Request.Context(), companyID, principal.UserID)) {
		return
	}
	c.Status(http.StatusNoContent)
}

func (h handler) removeMember(c *gin.Context) {
	companyID, ok := positivePathID(c, "companyId", "COMPANY_NOT_FOUND", "Компания не найдена")
	if !ok {
		return
	}
	userID, ok := positivePathID(c, "userId", "USER_NOT_FOUND", "Пользователь не найден")
	if !ok {
		return
	}
	principal, _ := account.PrincipalFromContext(c.Request.Context())
	if h.writeError(c, h.companies.RemoveMember(c.Request.Context(), companyID, principal.UserID, userID)) {
		return
	}
	c.Status(http.StatusNoContent)
}

func (h handler) createApplication(c *gin.Context) {
	companyID, ok := positivePathID(c, "companyId", "COMPANY_NOT_FOUND", "Компания не найдена")
	if !ok {
		return
	}
	principal, _ := account.PrincipalFromContext(c.Request.Context())
	var request applicationInputRequest
	if fields := decodeOptionalJSON(c, &request); len(fields) > 0 {
		apierr.WriteValidation(c, fields)
		return
	}
	item, err := h.companies.CreateApplication(c.Request.Context(), companyID, principal.UserID, companies.CreateApplicationInput{Message: request.Message})
	if h.writeError(c, err) {
		return
	}
	c.JSON(http.StatusCreated, newApplicationResponse(item))
}

func (h handler) getMyApplication(c *gin.Context) {
	companyID, ok := positivePathID(c, "companyId", "COMPANY_NOT_FOUND", "Компания не найдена")
	if !ok {
		return
	}
	principal, _ := account.PrincipalFromContext(c.Request.Context())
	item, err := h.companies.GetMyApplication(c.Request.Context(), companyID, principal.UserID)
	if h.writeError(c, err) {
		return
	}
	c.JSON(http.StatusOK, newApplicationResponse(item))
}

func (h handler) cancelMyApplication(c *gin.Context) {
	companyID, ok := positivePathID(c, "companyId", "COMPANY_NOT_FOUND", "Компания не найдена")
	if !ok {
		return
	}
	principal, _ := account.PrincipalFromContext(c.Request.Context())
	if h.writeError(c, h.companies.CancelApplication(c.Request.Context(), companyID, principal.UserID)) {
		return
	}
	c.Status(http.StatusNoContent)
}

func (h handler) listApplications(c *gin.Context) {
	companyID, ok := positivePathID(c, "companyId", "COMPANY_NOT_FOUND", "Компания не найдена")
	if !ok {
		return
	}
	page, fields := pagination.Parse(c.Request.URL.Query())
	status, statusFields := applicationStatusQuery(c)
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
	principal, _ := account.PrincipalFromContext(c.Request.Context())
	items, total, err := h.companies.ListApplications(c.Request.Context(), companyID, principal.UserID, status, companies.Page{Offset: page.Offset(), Limit: page.Limit})
	if h.writeError(c, err) {
		return
	}
	c.JSON(http.StatusOK, pagination.NewResponse(mapApplications(items), page, total))
}

func (h handler) listMyApplications(c *gin.Context) {
	page, fields := pagination.Parse(c.Request.URL.Query())
	if len(fields) > 0 {
		apierr.WriteValidation(c, fields)
		return
	}
	principal, _ := account.PrincipalFromContext(c.Request.Context())
	items, total, err := h.companies.ListMyApplications(c.Request.Context(), principal.UserID, companies.Page{Offset: page.Offset(), Limit: page.Limit})
	if h.writeError(c, err) {
		return
	}
	c.JSON(http.StatusOK, pagination.NewResponse(mapApplications(items), page, total))
}

func (h handler) resolveApplication(c *gin.Context) {
	companyID, ok := positivePathID(c, "companyId", "COMPANY_NOT_FOUND", "Компания не найдена")
	if !ok {
		return
	}
	applicationID, ok := positivePathID(c, "applicationId", "APPLICATION_NOT_FOUND", "Заявка не найдена")
	if !ok {
		return
	}
	action := c.Param("action")
	if action != "approve" && action != "reject" {
		apierr.Write(c, http.StatusNotFound, "APPLICATION_NOT_FOUND", "Заявка не найдена", nil)
		return
	}
	principal, _ := account.PrincipalFromContext(c.Request.Context())
	item, err := h.companies.ResolveApplication(c.Request.Context(), companyID, applicationID, principal.UserID, action)
	if h.writeError(c, err) {
		return
	}
	c.JSON(http.StatusOK, newApplicationResponse(item))
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
	case errors.Is(err, companies.ErrCompanyFull):
		apierr.Write(c, http.StatusConflict, "COMPANY_FULL", "В компании нет свободных мест", nil)
	case errors.Is(err, companies.ErrCompanyClosed):
		apierr.Write(c, http.StatusConflict, "COMPANY_CLOSED", "Открытое вступление в компанию недоступно", nil)
	case errors.Is(err, companies.ErrAlreadyCompanyMember):
		apierr.Write(c, http.StatusConflict, "ALREADY_COMPANY_MEMBER", "Пользователь уже состоит в этой компании", nil)
	case errors.Is(err, companies.ErrOwnerCannotLeave):
		apierr.Write(c, http.StatusConflict, "OWNER_CANNOT_LEAVE", "Владелец не может покинуть компанию", nil)
	case errors.Is(err, companies.ErrNotCompanyMember):
		apierr.Write(c, http.StatusConflict, "NOT_COMPANY_MEMBER", "Пользователь не состоит в компании", nil)
	case errors.Is(err, companies.ErrUserNotFound):
		apierr.Write(c, http.StatusNotFound, "USER_NOT_FOUND", "Пользователь не найден", nil)
	case errors.Is(err, companies.ErrUserBanned):
		apierr.Write(c, http.StatusConflict, "APPLICANT_BANNED", "Заблокированного пользователя нельзя принять в компанию", nil)
	case errors.Is(err, companies.ErrOwnerCannotBeRemoved):
		apierr.Write(c, http.StatusConflict, "OWNER_CANNOT_BE_REMOVED", "Владелец не может быть исключён из компании", nil)
	case errors.Is(err, companies.ErrApplicationAlreadyExists):
		apierr.Write(c, http.StatusConflict, "APPLICATION_ALREADY_EXISTS", "Активная заявка уже существует", nil)
	case errors.Is(err, companies.ErrApplicationNotFound):
		apierr.Write(c, http.StatusNotFound, "APPLICATION_NOT_FOUND", "Заявка не найдена", nil)
	case errors.Is(err, companies.ErrApplicationAlreadyResolved):
		apierr.Write(c, http.StatusConflict, "APPLICATION_ALREADY_RESOLVED", "Заявка уже обработана", nil)
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

type applicationInputRequest struct {
	Message *string `json:"message"`
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

type applicationResponse struct {
	ID               int64             `json:"id"`
	CompanyID        int64             `json:"companyId"`
	User             userShortResponse `json:"user"`
	Message          *string           `json:"message"`
	Status           string            `json:"status"`
	ResolutionReason *string           `json:"resolutionReason"`
	CreatedAt        time.Time         `json:"createdAt"`
	ResolvedAt       *time.Time        `json:"resolvedAt"`
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

func newApplicationResponse(item companies.Application) applicationResponse {
	return applicationResponse{
		ID: item.ID, CompanyID: item.CompanyID, User: newUserShortResponse(item.User),
		Message: item.Message, Status: item.Status, ResolutionReason: item.ResolutionReason,
		CreatedAt: item.CreatedAt, ResolvedAt: item.ResolvedAt,
	}
}

func mapApplications(items []companies.Application) []applicationResponse {
	result := make([]applicationResponse, 0, len(items))
	for _, item := range items {
		result = append(result, newApplicationResponse(item))
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

func decodeOptionalJSON(c *gin.Context, target any) apierr.FieldErrors {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxCompanyBodyBytes)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		if errors.Is(err, io.EOF) {
			return apierr.FieldErrors{}
		}
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

func applicationStatusQuery(c *gin.Context) (string, apierr.FieldErrors) {
	raw, ok := c.Request.URL.Query()["status"]
	if !ok {
		return "", apierr.FieldErrors{}
	}
	if len(raw) != 1 {
		return "", apierr.FieldErrors{"status": {"Параметр должен быть указан один раз"}}
	}
	for _, allowed := range []string{
		companies.ApplicationStatusPending, companies.ApplicationStatusApproved,
		companies.ApplicationStatusRejected, companies.ApplicationStatusCancelled,
	} {
		if raw[0] == allowed {
			return raw[0], apierr.FieldErrors{}
		}
	}
	return "", apierr.FieldErrors{"status": {"Недопустимое значение"}}
}

func viewer(c *gin.Context) companies.Viewer {
	principal, ok := account.PrincipalFromContext(c.Request.Context())
	if !ok {
		return companies.Viewer{}
	}
	id := principal.UserID
	return companies.Viewer{UserID: &id, Admin: principal.Role == account.RoleAdmin}
}
