package companies

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrNotFound                   = errors.New("company not found")
	ErrEventNotFound              = errors.New("event not found")
	ErrEventNotAvailable          = errors.New("event not available")
	ErrAlreadyInEventCompany      = errors.New("already in event company")
	ErrNotOwner                   = errors.New("not company owner")
	ErrCompanyBlocked             = errors.New("company blocked")
	ErrCapacityBelowMembers       = errors.New("company capacity below members")
	ErrCompanyFull                = errors.New("company full")
	ErrCompanyClosed              = errors.New("company closed")
	ErrAlreadyCompanyMember       = errors.New("already company member")
	ErrOwnerCannotLeave           = errors.New("owner cannot leave")
	ErrNotCompanyMember           = errors.New("not company member")
	ErrUserNotFound               = errors.New("user not found")
	ErrUserBanned                 = errors.New("user banned")
	ErrOwnerCannotBeRemoved       = errors.New("owner cannot be removed")
	ErrApplicationAlreadyExists   = errors.New("application already exists")
	ErrApplicationNotFound        = errors.New("application not found")
	ErrApplicationAlreadyResolved = errors.New("application already resolved")
)

type ValidationError struct{ Fields map[string][]string }

func (e *ValidationError) Error() string { return "company validation failed" }

type Store interface {
	Create(context.Context, int64, int64, CreateInput, time.Time) (Company, error)
	ListEvent(context.Context, int64, Viewer, Page, time.Time) ([]Company, int64, error)
	GetVisible(context.Context, int64, Viewer) (Company, error)
	ListMembers(context.Context, int64, Viewer, Page) ([]UserShort, int64, error)
	ListMine(context.Context, int64, Page) ([]Company, int64, error)
	ListAdmin(context.Context, Page) ([]Company, int64, error)
	Block(context.Context, int64, time.Time) (Company, error)
	Update(context.Context, int64, int64, Patch, time.Time) (Company, error)
	SetRecruitment(context.Context, int64, int64, string, time.Time) (Company, error)
	Delete(context.Context, int64, int64, time.Time) error
	JoinOpen(context.Context, int64, int64, time.Time) error
	Leave(context.Context, int64, int64) error
	RemoveMember(context.Context, int64, int64, int64) error
	CreateApplication(context.Context, int64, int64, CreateApplicationInput, time.Time) (Application, error)
	GetMyApplication(context.Context, int64, int64) (Application, error)
	ListApplications(context.Context, int64, int64, string, Page) ([]Application, int64, error)
	ListMyApplications(context.Context, int64, Page) ([]Application, int64, error)
	CancelApplication(context.Context, int64, int64, time.Time) error
	ResolveApplication(context.Context, int64, int64, int64, string, time.Time) (Application, error)
}

type Service struct {
	store Store
	now   func() time.Time
}

func NewService(store Store) *Service { return &Service{store: store, now: time.Now} }

func (s *Service) Create(ctx context.Context, eventID, ownerID int64, input CreateInput) (Company, error) {
	normalizeInput(&input)
	if fields := validateInput(input); len(fields) > 0 {
		return Company{}, &ValidationError{Fields: fields}
	}
	return s.store.Create(ctx, eventID, ownerID, input, s.now())
}

func (s *Service) ListEvent(ctx context.Context, eventID int64, viewer Viewer, page Page) ([]Company, int64, error) {
	return s.store.ListEvent(ctx, eventID, viewer, page, s.now())
}

func (s *Service) GetVisible(ctx context.Context, companyID int64, viewer Viewer) (Company, error) {
	return s.store.GetVisible(ctx, companyID, viewer)
}

func (s *Service) ListMembers(ctx context.Context, companyID int64, viewer Viewer, page Page) ([]UserShort, int64, error) {
	return s.store.ListMembers(ctx, companyID, viewer, page)
}

func (s *Service) ListMine(ctx context.Context, userID int64, page Page) ([]Company, int64, error) {
	return s.store.ListMine(ctx, userID, page)
}

func (s *Service) ListAdmin(ctx context.Context, page Page) ([]Company, int64, error) {
	return s.store.ListAdmin(ctx, page)
}

func (s *Service) Block(ctx context.Context, companyID int64) (Company, error) {
	return s.store.Block(ctx, companyID, s.now())
}

func (s *Service) Update(ctx context.Context, companyID, ownerID int64, patch Patch) (Company, error) {
	normalizePatch(&patch)
	if fields := validatePatch(patch); len(fields) > 0 {
		return Company{}, &ValidationError{Fields: fields}
	}
	return s.store.Update(ctx, companyID, ownerID, patch, s.now())
}

func (s *Service) SetRecruitment(ctx context.Context, companyID, ownerID int64, open bool) (Company, error) {
	status := StatusClosed
	if open {
		status = StatusActive
	}
	return s.store.SetRecruitment(ctx, companyID, ownerID, status, s.now())
}

func (s *Service) Delete(ctx context.Context, companyID, ownerID int64) error {
	return s.store.Delete(ctx, companyID, ownerID, s.now())
}

func (s *Service) JoinOpen(ctx context.Context, companyID, userID int64) error {
	return s.store.JoinOpen(ctx, companyID, userID, s.now())
}

func (s *Service) Leave(ctx context.Context, companyID, userID int64) error {
	return s.store.Leave(ctx, companyID, userID)
}

func (s *Service) RemoveMember(ctx context.Context, companyID, ownerID, userID int64) error {
	return s.store.RemoveMember(ctx, companyID, ownerID, userID)
}

func (s *Service) CreateApplication(ctx context.Context, companyID, userID int64, input CreateApplicationInput) (Application, error) {
	input.Message = normalizeOptional(input.Message)
	fields := make(map[string][]string)
	validateOptional(fields, "message", input.Message, 1000)
	if len(fields) > 0 {
		return Application{}, &ValidationError{Fields: fields}
	}
	return s.store.CreateApplication(ctx, companyID, userID, input, s.now())
}

func (s *Service) GetMyApplication(ctx context.Context, companyID, userID int64) (Application, error) {
	return s.store.GetMyApplication(ctx, companyID, userID)
}

func (s *Service) ListApplications(ctx context.Context, companyID, ownerID int64, status string, page Page) ([]Application, int64, error) {
	return s.store.ListApplications(ctx, companyID, ownerID, status, page)
}

func (s *Service) ListMyApplications(ctx context.Context, userID int64, page Page) ([]Application, int64, error) {
	return s.store.ListMyApplications(ctx, userID, page)
}

func (s *Service) CancelApplication(ctx context.Context, companyID, userID int64) error {
	return s.store.CancelApplication(ctx, companyID, userID, s.now())
}

func (s *Service) ResolveApplication(ctx context.Context, companyID, applicationID, ownerID int64, action string) (Application, error) {
	if action != "approve" && action != "reject" {
		return Application{}, ErrApplicationNotFound
	}
	return s.store.ResolveApplication(ctx, companyID, applicationID, ownerID, action, s.now())
}

func normalizeInput(input *CreateInput) {
	input.Name = strings.TrimSpace(input.Name)
	input.Description = normalizeOptional(input.Description)
	input.Rules = normalizeOptional(input.Rules)
}

func normalizeOptional(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	return &trimmed
}

func normalizePatch(patch *Patch) {
	if patch.Name.Set {
		patch.Name.Value = strings.TrimSpace(patch.Name.Value)
	}
	if patch.Description.Set && !patch.Description.Null {
		patch.Description.Value = strings.TrimSpace(patch.Description.Value)
	}
	if patch.Rules.Set && !patch.Rules.Null {
		patch.Rules.Value = strings.TrimSpace(patch.Rules.Value)
	}
}

func validateInput(input CreateInput) map[string][]string {
	fields := make(map[string][]string)
	length := utf8.RuneCountInString(input.Name)
	if length == 0 || length > 150 {
		fields["name"] = []string{"Длина должна быть от 1 до 150 символов"}
	}
	validateOptional(fields, "description", input.Description, 2000)
	validateOptional(fields, "rules", input.Rules, 2000)
	if input.MaxMembers < 2 || input.MaxMembers > 100 {
		fields["maxMembers"] = []string{"Значение должно быть от 2 до 100"}
	}
	if input.JoinType != JoinTypeOpen && input.JoinType != JoinTypeRequest {
		fields["joinType"] = []string{"Допустимые значения: open, request"}
	}
	return fields
}

func validatePatch(patch Patch) map[string][]string {
	fields := make(map[string][]string)
	if patch.Name.Set {
		length := utf8.RuneCountInString(patch.Name.Value)
		if length == 0 || length > 150 {
			fields["name"] = []string{"Длина должна быть от 1 до 150 символов"}
		}
	}
	if patch.Description.Set && !patch.Description.Null {
		value := patch.Description.Value
		validateOptional(fields, "description", &value, 2000)
	}
	if patch.Rules.Set && !patch.Rules.Null {
		value := patch.Rules.Value
		validateOptional(fields, "rules", &value, 2000)
	}
	if patch.MaxMembers.Set && (patch.MaxMembers.Value < 2 || patch.MaxMembers.Value > 100) {
		fields["maxMembers"] = []string{"Значение должно быть от 2 до 100"}
	}
	if patch.JoinType.Set && patch.JoinType.Value != JoinTypeOpen && patch.JoinType.Value != JoinTypeRequest {
		fields["joinType"] = []string{"Допустимые значения: open, request"}
	}
	return fields
}

func validateOptional(fields map[string][]string, name string, value *string, maximum int) {
	if value != nil && utf8.RuneCountInString(*value) > maximum {
		fields[name] = []string{"Максимальная длина — " + strconv.Itoa(maximum) + " символов"}
	}
}
