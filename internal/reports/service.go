package reports

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrTargetNotFound  = errors.New("report target not found")
	ErrNotFound        = errors.New("report not found")
	ErrAlreadyResolved = errors.New("report already resolved")
)

type ValidationError struct{ Fields map[string][]string }

func (e *ValidationError) Error() string { return "report validation failed" }

type Store interface {
	Create(context.Context, int64, CreateInput, time.Time) (Report, error)
	ListAdmin(context.Context, string, Page) ([]Report, int64, error)
	GetAdmin(context.Context, int64) (Report, error)
	Resolve(context.Context, int64, int64, string, time.Time) (Report, error)
}

type Service struct {
	store Store
	now   func() time.Time
}

func NewService(store Store) *Service { return &Service{store: store, now: time.Now} }

func (s *Service) Create(ctx context.Context, authorID int64, input CreateInput) (Report, error) {
	input.TargetType = strings.TrimSpace(input.TargetType)
	input.Reason = strings.TrimSpace(input.Reason)
	if input.Description != nil {
		value := strings.TrimSpace(*input.Description)
		input.Description = &value
	}
	fields := validateCreate(input)
	if len(fields) > 0 {
		return Report{}, &ValidationError{Fields: fields}
	}
	return s.store.Create(ctx, authorID, input, s.now())
}

func (s *Service) ListAdmin(ctx context.Context, status string, page Page) ([]Report, int64, error) {
	return s.store.ListAdmin(ctx, status, page)
}

func (s *Service) GetAdmin(ctx context.Context, reportID int64) (Report, error) {
	return s.store.GetAdmin(ctx, reportID)
}

func (s *Service) Resolve(ctx context.Context, reportID, adminID int64, action string) (Report, error) {
	if action != "resolve" && action != "reject" {
		return Report{}, ErrNotFound
	}
	return s.store.Resolve(ctx, reportID, adminID, action, s.now())
}

func validateCreate(input CreateInput) map[string][]string {
	fields := make(map[string][]string)
	if input.TargetType != TargetUser && input.TargetType != TargetCompany && input.TargetType != TargetEvent {
		fields["targetType"] = []string{"Допустимые значения: user, company, event"}
	}
	if input.TargetID < 1 {
		fields["targetId"] = []string{"Должно быть не меньше 1"}
	}
	if length := utf8.RuneCountInString(input.Reason); length < 1 || length > 100 {
		fields["reason"] = []string{"Длина должна быть от 1 до 100 символов"}
	}
	if input.Description != nil && utf8.RuneCountInString(*input.Description) > 2000 {
		fields["description"] = []string{"Максимальная длина — " + strconv.Itoa(2000) + " символов"}
	}
	return fields
}
