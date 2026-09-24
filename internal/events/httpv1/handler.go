// Package httpv1 exposes event use cases through API v1.
package httpv1

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/emount4/poidem-back/internal/account"
	accounthttp "github.com/emount4/poidem-back/internal/account/httpv1"
	"github.com/emount4/poidem-back/internal/api/apierr"
	"github.com/emount4/poidem-back/internal/api/pagination"
	"github.com/emount4/poidem-back/internal/events"
	"github.com/gin-gonic/gin"
)

const maxEventBodyBytes = 128 << 10

type Events interface {
	Create(context.Context, int64, events.CreateInput) (events.Event, error)
	ListPublic(context.Context, events.PublicFilter, events.Page) ([]events.Event, int64, error)
	GetVisible(context.Context, int64, *int64) (events.Event, error)
	ListParticipants(context.Context, int64, *int64, events.Page) ([]events.UserShort, int64, error)
	ListMine(context.Context, int64, events.MyFilter, events.Page) ([]events.MyEvent, int64, error)
	ListAdmin(context.Context, events.AdminFilter, events.Page) ([]events.Event, int64, error)
	GetAdmin(context.Context, int64) (events.Event, error)
	Update(context.Context, int64, events.Patch) (events.Event, error)
	Transition(context.Context, int64, string) (events.Event, error)
}

func RegisterRoutes(routes *gin.RouterGroup, service Events, authenticator accounthttp.Authenticator) {
	h := handler{events: service}
	routes.GET("/events", h.listPublic)
	optional := routes.Group("")
	optional.Use(accounthttp.OptionalAuthentication(authenticator))
	optional.GET("/events/:eventId", h.getVisible)
	optional.GET("/events/:eventId/participants", h.listParticipants)

	protected := routes.Group("")
	protected.Use(accounthttp.RequireAuthentication(authenticator))
	protected.GET("/users/me/events", h.listMine)
	protected.POST("/events", accounthttp.RequireCompleteProfile(), h.create)

	admin := routes.Group("/admin")
	admin.Use(accounthttp.RequireAuthentication(authenticator), accounthttp.RequireAdmin())
	admin.GET("/events", h.listAdmin)
	admin.GET("/events/:eventId", h.getAdmin)
	admin.PATCH("/events/:eventId", h.updateAdmin)
	admin.POST("/events/:eventId/:action", h.moderate)
}

type handler struct{ events Events }

func (h handler) create(c *gin.Context) {
	principal, _ := account.PrincipalFromContext(c.Request.Context())
	var request eventInputRequest
	if fields := decodeJSON(c, &request); len(fields) > 0 {
		apierr.WriteValidation(c, fields)
		return
	}
	input, fields := request.input()
	if len(fields) > 0 {
		apierr.WriteValidation(c, fields)
		return
	}
	item, err := h.events.Create(c.Request.Context(), principal.UserID, input)
	if h.writeError(c, err) {
		return
	}
	c.JSON(http.StatusCreated, newEventResponse(item))
}

func (h handler) listPublic(c *gin.Context) {
	page, fields := pagination.Parse(c.Request.URL.Query())
	filter, filterFields := parsePublicFilter(c.Request.URL.Query())
	fields = mergeFields(fields, filterFields)
	if len(fields) > 0 {
		apierr.WriteValidation(c, fields)
		return
	}
	items, total, err := h.events.ListPublic(c.Request.Context(), filter, events.Page{Offset: page.Offset(), Limit: page.Limit})
	if h.writeError(c, err) {
		return
	}
	c.JSON(http.StatusOK, pagination.NewResponse(mapEvents(items), page, total))
}

func (h handler) getVisible(c *gin.Context) {
	id, ok := eventID(c)
	if !ok {
		return
	}
	item, err := h.events.GetVisible(c.Request.Context(), id, viewerID(c))
	if h.writeError(c, err) {
		return
	}
	c.JSON(http.StatusOK, newEventResponse(item))
}

func (h handler) listParticipants(c *gin.Context) {
	id, ok := eventID(c)
	if !ok {
		return
	}
	page, fields := pagination.Parse(c.Request.URL.Query())
	if len(fields) > 0 {
		apierr.WriteValidation(c, fields)
		return
	}
	items, total, err := h.events.ListParticipants(c.Request.Context(), id, viewerID(c), events.Page{Offset: page.Offset(), Limit: page.Limit})
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
	status, statusFields := enumQuery(c.Request.URL.Query(), "status", "", "upcoming", "past")
	fields = mergeFields(fields, statusFields)
	if len(fields) > 0 {
		apierr.WriteValidation(c, fields)
		return
	}
	items, total, err := h.events.ListMine(c.Request.Context(), principal.UserID, events.MyFilter{Status: status}, events.Page{Offset: page.Offset(), Limit: page.Limit})
	if h.writeError(c, err) {
		return
	}
	responses := make([]myEventResponse, 0, len(items))
	for _, item := range items {
		responses = append(responses, myEventResponse{eventResponse: newEventResponse(item.Event), Relation: item.Relation})
	}
	c.JSON(http.StatusOK, pagination.NewResponse(responses, page, total))
}

func (h handler) listAdmin(c *gin.Context) {
	page, fields := pagination.Parse(c.Request.URL.Query())
	status, statusFields := enumQuery(c.Request.URL.Query(), "status", "", events.StatusPending, events.StatusActive, events.StatusRejected, events.StatusBlocked, events.StatusCompleted)
	fields = mergeFields(fields, statusFields)
	search, searchFields := optionalQuery(c.Request.URL.Query(), "search")
	fields = mergeFields(fields, searchFields)
	if len(fields) > 0 {
		apierr.WriteValidation(c, fields)
		return
	}
	items, total, err := h.events.ListAdmin(c.Request.Context(), events.AdminFilter{Search: search, Status: status}, events.Page{Offset: page.Offset(), Limit: page.Limit})
	if h.writeError(c, err) {
		return
	}
	c.JSON(http.StatusOK, pagination.NewResponse(mapEvents(items), page, total))
}

func (h handler) getAdmin(c *gin.Context) {
	id, ok := eventID(c)
	if !ok {
		return
	}
	item, err := h.events.GetAdmin(c.Request.Context(), id)
	if h.writeError(c, err) {
		return
	}
	c.JSON(http.StatusOK, newEventResponse(item))
}

func (h handler) updateAdmin(c *gin.Context) {
	id, ok := eventID(c)
	if !ok {
		return
	}
	var request eventPatchRequest
	if fields := decodeJSON(c, &request); len(fields) > 0 {
		apierr.WriteValidation(c, fields)
		return
	}
	patch, fields := request.patch()
	if len(fields) > 0 {
		apierr.WriteValidation(c, fields)
		return
	}
	item, err := h.events.Update(c.Request.Context(), id, patch)
	if h.writeError(c, err) {
		return
	}
	c.JSON(http.StatusOK, newEventResponse(item))
}

func (h handler) moderate(c *gin.Context) {
	id, ok := eventID(c)
	if !ok {
		return
	}
	item, err := h.events.Transition(c.Request.Context(), id, c.Param("action"))
	if h.writeError(c, err) {
		return
	}
	c.JSON(http.StatusOK, newEventResponse(item))
}

func (h handler) writeError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	var validation *events.ValidationError
	switch {
	case errors.As(err, &validation):
		apierr.WriteValidation(c, validation.Fields)
	case errors.Is(err, events.ErrCityNotFound):
		apierr.WriteValidation(c, apierr.FieldErrors{"cityId": {"Город не найден"}})
	case errors.Is(err, events.ErrCategoryNotFound):
		apierr.WriteValidation(c, apierr.FieldErrors{"categoryId": {"Категория не найдена"}})
	case errors.Is(err, events.ErrNotFound):
		apierr.Write(c, http.StatusNotFound, "EVENT_NOT_FOUND", "Событие не найдено", nil)
	case errors.Is(err, events.ErrInvalidStatusTransition):
		apierr.Write(c, http.StatusConflict, "INVALID_STATUS_TRANSITION", "Недопустимый переход статуса события", nil)
	default:
		apierr.WriteInternal(c, err)
	}
	return true
}

type eventInputRequest struct {
	Title        string  `json:"title"`
	Description  *string `json:"description"`
	CategoryID   int64   `json:"categoryId"`
	CityID       int64   `json:"cityId"`
	StartsAt     string  `json:"startsAt"`
	EndsAt       *string `json:"endsAt"`
	LocationName string  `json:"locationName"`
	Address      *string `json:"address"`
	ImageURL     *string `json:"imageUrl"`
}

func (r eventInputRequest) input() (events.CreateInput, apierr.FieldErrors) {
	fields := apierr.FieldErrors{}
	startsAt, err := time.Parse(time.RFC3339, r.StartsAt)
	if err != nil {
		fields["startsAt"] = []string{"Ожидается дата и время RFC 3339 с часовым поясом"}
	}
	var endsAt *time.Time
	if r.EndsAt != nil {
		value, parseErr := time.Parse(time.RFC3339, *r.EndsAt)
		if parseErr != nil {
			fields["endsAt"] = []string{"Ожидается дата и время RFC 3339 с часовым поясом"}
		} else {
			endsAt = &value
		}
	}
	return events.CreateInput{Title: r.Title, Description: r.Description, CategoryID: r.CategoryID, CityID: r.CityID, StartsAt: startsAt, EndsAt: endsAt, LocationName: r.LocationName, Address: r.Address, ImageURL: r.ImageURL}, fields
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

type eventPatchRequest struct {
	Title        patchField[string] `json:"title"`
	Description  patchField[string] `json:"description"`
	CategoryID   patchField[int64]  `json:"categoryId"`
	CityID       patchField[int64]  `json:"cityId"`
	StartsAt     patchField[string] `json:"startsAt"`
	EndsAt       patchField[string] `json:"endsAt"`
	LocationName patchField[string] `json:"locationName"`
	Address      patchField[string] `json:"address"`
	ImageURL     patchField[string] `json:"imageUrl"`
}

func (r eventPatchRequest) patch() (events.Patch, apierr.FieldErrors) {
	fields := apierr.FieldErrors{}
	if r.Title.Null {
		fields["title"] = []string{"Поле не может быть null"}
	}
	if r.CategoryID.Null {
		fields["categoryId"] = []string{"Поле не может быть null"}
	}
	if r.CityID.Null {
		fields["cityId"] = []string{"Поле не может быть null"}
	}
	if r.StartsAt.Null {
		fields["startsAt"] = []string{"Поле не может быть null"}
	}
	if r.LocationName.Null {
		fields["locationName"] = []string{"Поле не может быть null"}
	}
	var startsAt time.Time
	if r.StartsAt.Present && !r.StartsAt.Null {
		value, err := time.Parse(time.RFC3339, r.StartsAt.Value)
		if err != nil {
			fields["startsAt"] = []string{"Ожидается дата и время RFC 3339 с часовым поясом"}
		} else {
			startsAt = value
		}
	}
	var endsAt time.Time
	if r.EndsAt.Present && !r.EndsAt.Null {
		value, err := time.Parse(time.RFC3339, r.EndsAt.Value)
		if err != nil {
			fields["endsAt"] = []string{"Ожидается дата и время RFC 3339 с часовым поясом"}
		} else {
			endsAt = value
		}
	}
	return events.Patch{
		Title: events.Change[string]{Set: r.Title.Present, Value: r.Title.Value}, Description: events.NullableChange[string]{Set: r.Description.Present, Null: r.Description.Null, Value: r.Description.Value},
		CategoryID: events.Change[int64]{Set: r.CategoryID.Present, Value: r.CategoryID.Value}, CityID: events.Change[int64]{Set: r.CityID.Present, Value: r.CityID.Value},
		StartsAt: events.Change[time.Time]{Set: r.StartsAt.Present, Value: startsAt}, EndsAt: events.NullableChange[time.Time]{Set: r.EndsAt.Present, Null: r.EndsAt.Null, Value: endsAt},
		LocationName: events.Change[string]{Set: r.LocationName.Present, Value: r.LocationName.Value}, Address: events.NullableChange[string]{Set: r.Address.Present, Null: r.Address.Null, Value: r.Address.Value}, ImageURL: events.NullableChange[string]{Set: r.ImageURL.Present, Null: r.ImageURL.Null, Value: r.ImageURL.Value},
	}, fields
}

type userShortResponse struct {
	ID        int64   `json:"id"`
	FirstName string  `json:"firstName"`
	LastName  *string `json:"lastName"`
	AvatarURL *string `json:"avatarUrl"`
}
type eventResponse struct {
	ID                int64             `json:"id"`
	Title             string            `json:"title"`
	Description       *string           `json:"description"`
	CategoryID        int64             `json:"categoryId"`
	CityID            int64             `json:"cityId"`
	StartsAt          time.Time         `json:"startsAt"`
	EndsAt            *time.Time        `json:"endsAt"`
	LocationName      string            `json:"locationName"`
	Address           *string           `json:"address"`
	ImageURL          *string           `json:"imageUrl"`
	Status            string            `json:"status"`
	ParticipantsCount int64             `json:"participantsCount"`
	CompaniesCount    int64             `json:"companiesCount"`
	Creator           userShortResponse `json:"creator"`
	CreatedAt         time.Time         `json:"createdAt"`
	UpdatedAt         time.Time         `json:"updatedAt"`
}
type myEventResponse struct {
	eventResponse
	Relation string `json:"relation"`
}

func newUserShortResponse(item events.UserShort) userShortResponse {
	return userShortResponse{ID: item.ID, FirstName: item.FirstName, LastName: item.LastName, AvatarURL: item.AvatarURL}
}
func newEventResponse(item events.Event) eventResponse {
	return eventResponse{ID: item.ID, Title: item.Title, Description: item.Description, CategoryID: item.CategoryID, CityID: item.CityID, StartsAt: item.StartsAt, EndsAt: item.EndsAt, LocationName: item.LocationName, Address: item.Address, ImageURL: item.ImageURL, Status: item.Status, ParticipantsCount: item.ParticipantsCount, CompaniesCount: item.CompaniesCount, Creator: newUserShortResponse(item.Creator), CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt}
}
func mapEvents(items []events.Event) []eventResponse {
	result := make([]eventResponse, 0, len(items))
	for _, item := range items {
		result = append(result, newEventResponse(item))
	}
	return result
}

func decodeJSON(c *gin.Context, target any) apierr.FieldErrors {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxEventBodyBytes)
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

func eventID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("eventId"), 10, 64)
	if err != nil || id <= 0 {
		apierr.Write(c, http.StatusNotFound, "EVENT_NOT_FOUND", "Событие не найдено", nil)
		return 0, false
	}
	return id, true
}
func viewerID(c *gin.Context) *int64 {
	principal, ok := account.PrincipalFromContext(c.Request.Context())
	if !ok {
		return nil
	}
	id := principal.UserID
	return &id
}

func parsePublicFilter(values url.Values) (events.PublicFilter, apierr.FieldErrors) {
	fields := apierr.FieldErrors{}
	search, f := optionalQuery(values, "search")
	fields = mergeFields(fields, f)
	sort, f := enumQuery(values, "sort", "date", "date", "popular", "newest")
	fields = mergeFields(fields, f)
	city, f := positiveIDQuery(values, "cityId")
	fields = mergeFields(fields, f)
	category, f := positiveIDQuery(values, "categoryId")
	fields = mergeFields(fields, f)
	from, f := timeQuery(values, "dateFrom")
	fields = mergeFields(fields, f)
	to, f := timeQuery(values, "dateTo")
	fields = mergeFields(fields, f)
	if from != nil && to != nil && to.Before(*from) {
		fields["dateTo"] = []string{"Должно быть не раньше dateFrom"}
	}
	return events.PublicFilter{Search: search, Sort: sort, CityID: city, CategoryID: category, DateFrom: from, DateTo: to}, fields
}

func optionalQuery(values url.Values, name string) (string, apierr.FieldErrors) {
	raw, ok := values[name]
	if !ok {
		return "", apierr.FieldErrors{}
	}
	if len(raw) != 1 {
		return "", apierr.FieldErrors{name: {"Параметр должен быть указан один раз"}}
	}
	return raw[0], apierr.FieldErrors{}
}
func enumQuery(values url.Values, name, fallback string, allowed ...string) (string, apierr.FieldErrors) {
	value, fields := optionalQuery(values, name)
	if len(fields) > 0 {
		return "", fields
	}
	if value == "" {
		return fallback, fields
	}
	for _, candidate := range allowed {
		if value == candidate {
			return value, fields
		}
	}
	return "", apierr.FieldErrors{name: {"Недопустимое значение"}}
}
func positiveIDQuery(values url.Values, name string) (*int64, apierr.FieldErrors) {
	value, fields := optionalQuery(values, name)
	if len(fields) > 0 || value == "" {
		return nil, fields
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return nil, apierr.FieldErrors{name: {"Идентификатор должен быть положительным"}}
	}
	return &parsed, fields
}
func timeQuery(values url.Values, name string) (*time.Time, apierr.FieldErrors) {
	value, fields := optionalQuery(values, name)
	if len(fields) > 0 || value == "" {
		return nil, fields
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, apierr.FieldErrors{name: {"Ожидается дата и время RFC 3339 с часовым поясом"}}
	}
	return &parsed, fields
}
func mergeFields(target, source apierr.FieldErrors) apierr.FieldErrors {
	if target == nil {
		target = apierr.FieldErrors{}
	}
	for key, value := range source {
		target[key] = value
	}
	return target
}
