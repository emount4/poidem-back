package events

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrNotFound                = errors.New("event not found")
	ErrCityNotFound            = errors.New("city not found")
	ErrCategoryNotFound        = errors.New("event category not found")
	ErrInvalidStatusTransition = errors.New("invalid event status transition")
	ErrEventNotAvailable       = errors.New("event not available")
	ErrAlreadyEventParticipant = errors.New("already event participant")
	ErrAlreadyInEventCompany   = errors.New("already in event company")
	ErrNotSoloParticipant      = errors.New("not solo participant")
	ErrForbidden               = errors.New("event operation forbidden")
	ErrNotEditable             = errors.New("event is not editable")
	ErrCoverNotOwned           = errors.New("event cover is not owned by user")
)

type ValidationError struct{ Fields map[string][]string }

func (e *ValidationError) Error() string { return "event validation failed" }

type Store interface {
	Create(context.Context, int64, CreateInput) (Event, error)
	ListPublic(context.Context, PublicFilter, Page) ([]Event, int64, error)
	GetVisible(context.Context, int64, *int64) (Event, error)
	ListParticipants(context.Context, int64, *int64, Page) ([]UserShort, int64, error)
	ListMine(context.Context, int64, MyFilter, Page) ([]MyEvent, int64, error)
	ListAdmin(context.Context, AdminFilter, Page) ([]Event, int64, error)
	GetAdmin(context.Context, int64) (Event, error)
	Update(context.Context, int64, Patch) (Event, error)
	UpdateOwned(context.Context, int64, int64, Patch, time.Time) (Event, error)
	Delete(context.Context, int64, *int64, time.Time) error
	Transition(context.Context, int64, string, *string, time.Time) (Event, error)
	JoinSolo(context.Context, int64, int64, time.Time) error
	CancelSolo(context.Context, int64, int64) error
}

type Service struct {
	store Store
	now   func() time.Time
}

func NewService(store Store) *Service { return &Service{store: store, now: time.Now} }

func (s *Service) Create(ctx context.Context, creatorID int64, input CreateInput) (Event, error) {
	normalizeInput(&input)
	if fields := validateInput(input, s.now()); len(fields) > 0 {
		return Event{}, &ValidationError{Fields: fields}
	}
	return s.store.Create(ctx, creatorID, input)
}

func (s *Service) ListPublic(ctx context.Context, filter PublicFilter, page Page) ([]Event, int64, error) {
	filter.Search = strings.TrimSpace(filter.Search)
	filter.Now = s.now()
	return s.store.ListPublic(ctx, filter, page)
}

func (s *Service) GetVisible(ctx context.Context, id int64, viewerID *int64) (Event, error) {
	return s.store.GetVisible(ctx, id, viewerID)
}

func (s *Service) ListParticipants(ctx context.Context, id int64, viewerID *int64, page Page) ([]UserShort, int64, error) {
	return s.store.ListParticipants(ctx, id, viewerID, page)
}

func (s *Service) ListMine(ctx context.Context, userID int64, filter MyFilter, page Page) ([]MyEvent, int64, error) {
	filter.Now = s.now()
	return s.store.ListMine(ctx, userID, filter, page)
}

func (s *Service) ListAdmin(ctx context.Context, filter AdminFilter, page Page) ([]Event, int64, error) {
	filter.Search = strings.TrimSpace(filter.Search)
	return s.store.ListAdmin(ctx, filter, page)
}

func (s *Service) GetAdmin(ctx context.Context, id int64) (Event, error) {
	return s.store.GetAdmin(ctx, id)
}

func (s *Service) Update(ctx context.Context, id int64, patch Patch) (Event, error) {
	normalizePatch(&patch)
	if fields := validatePatch(patch); len(fields) > 0 {
		return Event{}, &ValidationError{Fields: fields}
	}
	return s.store.Update(ctx, id, patch)
}

func (s *Service) UpdateOwned(ctx context.Context, id, userID int64, patch Patch) (Event, error) {
	normalizePatch(&patch)
	fields := validatePatch(patch)
	if patch.StartsAt.Set && !patch.StartsAt.Value.After(s.now()) {
		fields["startsAt"] = []string{"Дата начала должна быть в будущем"}
	}
	if len(fields) > 0 {
		return Event{}, &ValidationError{Fields: fields}
	}
	return s.store.UpdateOwned(ctx, id, userID, patch, s.now())
}

func (s *Service) DeleteOwned(ctx context.Context, id, userID int64) error {
	return s.store.Delete(ctx, id, &userID, s.now())
}

func (s *Service) DeleteAdmin(ctx context.Context, id int64) error {
	return s.store.Delete(ctx, id, nil, s.now())
}

func (s *Service) Transition(ctx context.Context, id int64, action string, reason *string) (Event, error) {
	if action != "approve" && action != "reject" && action != "block" {
		return Event{}, ErrInvalidStatusTransition
	}
	reason = normalizeOptional(reason)
	fields := make(map[string][]string)
	if action != "approve" && (reason == nil || *reason == "") {
		fields["reason"] = []string{"Причина обязательна для отклонения или блокировки"}
	}
	validateOptionalString(fields, "reason", reason, 1000)
	if len(fields) > 0 {
		return Event{}, &ValidationError{Fields: fields}
	}
	return s.store.Transition(ctx, id, action, reason, s.now())
}

func (s *Service) JoinSolo(ctx context.Context, eventID, userID int64) error {
	return s.store.JoinSolo(ctx, eventID, userID, s.now())
}

func (s *Service) CancelSolo(ctx context.Context, eventID, userID int64) error {
	return s.store.CancelSolo(ctx, eventID, userID)
}

func normalizeInput(input *CreateInput) {
	input.Title = strings.TrimSpace(input.Title)
	input.LocationName = strings.TrimSpace(input.LocationName)
	input.Description = normalizeOptional(input.Description)
	input.Address = normalizeOptional(input.Address)
	input.ImageURL = normalizeOptional(input.ImageURL)
	if input.Location != nil {
		input.Location.Source = strings.TrimSpace(input.Location.Source)
		if input.Location.Source == "" {
			input.Location.Source = "manual"
		}
	}
}

func normalizePatch(patch *Patch) {
	if patch.Title.Set {
		patch.Title.Value = strings.TrimSpace(patch.Title.Value)
	}
	if patch.LocationName.Set {
		patch.LocationName.Value = strings.TrimSpace(patch.LocationName.Value)
	}
	normalizeNullable(&patch.Description)
	normalizeNullable(&patch.Address)
	normalizeNullable(&patch.ImageURL)
	if patch.Location.Set && !patch.Location.Null {
		patch.Location.Value.Source = strings.TrimSpace(patch.Location.Value.Source)
		if patch.Location.Value.Source == "" {
			patch.Location.Value.Source = "manual"
		}
	}
}

func normalizeOptional(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	return &trimmed
}

func normalizeNullable(change *NullableChange[string]) {
	if change.Set && !change.Null {
		change.Value = strings.TrimSpace(change.Value)
	}
}

func validateInput(input CreateInput, now time.Time) map[string][]string {
	fields := make(map[string][]string)
	validateRequiredString(fields, "title", input.Title, 140)
	validateRequiredString(fields, "locationName", input.LocationName, 255)
	validateOptionalString(fields, "description", input.Description, 5000)
	validateOptionalString(fields, "address", input.Address, 500)
	validateURI(fields, "imageUrl", input.ImageURL)
	if input.CategoryID <= 0 {
		fields["categoryId"] = []string{"Идентификатор должен быть положительным"}
	}
	if input.CityID <= 0 {
		fields["cityId"] = []string{"Идентификатор должен быть положительным"}
	}
	if input.StartsAt.IsZero() {
		fields["startsAt"] = []string{"Обязательное поле"}
	} else if !input.StartsAt.After(now) {
		fields["startsAt"] = []string{"Дата начала должна быть в будущем"}
	}
	if input.EndsAt != nil && !input.EndsAt.After(input.StartsAt) {
		fields["endsAt"] = []string{"Должно быть позже startsAt"}
	}
	validateLocation(fields, input.Location)
	return fields
}

func validatePatch(patch Patch) map[string][]string {
	fields := make(map[string][]string)
	if patch.Title.Set {
		validateRequiredString(fields, "title", patch.Title.Value, 140)
	}
	if patch.LocationName.Set {
		validateRequiredString(fields, "locationName", patch.LocationName.Value, 255)
	}
	if patch.Description.Set && !patch.Description.Null {
		value := patch.Description.Value
		validateOptionalString(fields, "description", &value, 5000)
	}
	if patch.Address.Set && !patch.Address.Null {
		value := patch.Address.Value
		validateOptionalString(fields, "address", &value, 500)
	}
	if patch.ImageURL.Set && !patch.ImageURL.Null {
		value := patch.ImageURL.Value
		validateURI(fields, "imageUrl", &value)
	}
	if patch.CategoryID.Set && patch.CategoryID.Value <= 0 {
		fields["categoryId"] = []string{"Идентификатор должен быть положительным"}
	}
	if patch.CityID.Set && patch.CityID.Value <= 0 {
		fields["cityId"] = []string{"Идентификатор должен быть положительным"}
	}
	if patch.StartsAt.Set && patch.StartsAt.Value.IsZero() {
		fields["startsAt"] = []string{"Некорректная дата"}
	}
	if patch.Location.Set && !patch.Location.Null {
		location := patch.Location.Value
		validateLocation(fields, &location)
	}
	return fields
}

func validateLocation(fields map[string][]string, location *Location) {
	if location == nil {
		return
	}
	if location.Latitude < -90 || location.Latitude > 90 {
		fields["location.latitude"] = []string{"Значение должно быть от -90 до 90"}
	}
	if location.Longitude < -180 || location.Longitude > 180 {
		fields["location.longitude"] = []string{"Значение должно быть от -180 до 180"}
	}
	if location.Source != "manual" && location.Source != "geocoded" {
		fields["location.source"] = []string{"Допустимые значения: manual, geocoded"}
	}
}

func validateRequiredString(fields map[string][]string, name, value string, maximum int) {
	length := utf8.RuneCountInString(value)
	if length == 0 || length > maximum {
		fields[name] = []string{"Длина должна быть от 1 до " + strconv.Itoa(maximum) + " символов"}
	}
}

func validateOptionalString(fields map[string][]string, name string, value *string, maximum int) {
	if value != nil && utf8.RuneCountInString(*value) > maximum {
		fields[name] = []string{"Максимальная длина — " + strconv.Itoa(maximum) + " символов"}
	}
}

func validateURI(fields map[string][]string, name string, value *string) {
	if value == nil || *value == "" {
		return
	}
	parsed, err := url.ParseRequestURI(*value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		fields[name] = []string{"Должен быть корректный HTTP(S) URL"}
	}
}
