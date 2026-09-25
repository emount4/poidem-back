package httpv1

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/emount4/poidem-back/internal/account"
	"github.com/emount4/poidem-back/internal/companies"
	"github.com/gin-gonic/gin"
)

type companiesStub struct {
	created          companies.CreateInput
	eventID          int64
	ownerID          int64
	listViewer       companies.Viewer
	patched          companies.Patch
	joinedCompanyID  int64
	joinedUserID     int64
	leftCompanyID    int64
	leftUserID       int64
	removedCompanyID int64
	removingOwnerID  int64
	removedUserID    int64
	applicationInput companies.CreateApplicationInput
	err              error
}

func (s *companiesStub) Create(_ context.Context, eventID, ownerID int64, input companies.CreateInput) (companies.Company, error) {
	s.eventID, s.ownerID, s.created = eventID, ownerID, input
	if s.err != nil {
		return companies.Company{}, s.err
	}
	return sampleCompany(ownerID), nil
}
func (s *companiesStub) ListEvent(_ context.Context, _ int64, viewer companies.Viewer, _ companies.Page) ([]companies.Company, int64, error) {
	s.listViewer = viewer
	return []companies.Company{}, 0, s.err
}
func (s *companiesStub) GetVisible(context.Context, int64, companies.Viewer) (companies.Company, error) {
	return sampleCompany(7), s.err
}
func (s *companiesStub) ListMembers(context.Context, int64, companies.Viewer, companies.Page) ([]companies.UserShort, int64, error) {
	return []companies.UserShort{}, 0, s.err
}
func (s *companiesStub) ListMine(context.Context, int64, companies.Page) ([]companies.Company, int64, error) {
	return []companies.Company{}, 0, s.err
}
func (s *companiesStub) Update(_ context.Context, _, _ int64, patch companies.Patch) (companies.Company, error) {
	s.patched = patch
	return sampleCompany(7), s.err
}
func (s *companiesStub) SetRecruitment(context.Context, int64, int64, bool) (companies.Company, error) {
	return sampleCompany(7), s.err
}
func (s *companiesStub) Delete(context.Context, int64, int64) error { return s.err }
func (s *companiesStub) JoinOpen(_ context.Context, companyID, userID int64) error {
	s.joinedCompanyID, s.joinedUserID = companyID, userID
	return s.err
}
func (s *companiesStub) Leave(_ context.Context, companyID, userID int64) error {
	s.leftCompanyID, s.leftUserID = companyID, userID
	return s.err
}
func (s *companiesStub) RemoveMember(_ context.Context, companyID, ownerID, userID int64) error {
	s.removedCompanyID, s.removingOwnerID, s.removedUserID = companyID, ownerID, userID
	return s.err
}
func (s *companiesStub) CreateApplication(_ context.Context, companyID, userID int64, input companies.CreateApplicationInput) (companies.Application, error) {
	s.joinedCompanyID, s.joinedUserID, s.applicationInput = companyID, userID, input
	return sampleApplication(userID), s.err
}

type authenticatorStub struct{}

func (authenticatorStub) Authenticate(_ context.Context, token string) (account.Principal, error) {
	switch token {
	case "complete":
		return account.Principal{UserID: 7, Role: account.RoleUser, ProfileComplete: true}, nil
	case "incomplete":
		return account.Principal{UserID: 7, Role: account.RoleUser}, nil
	case "admin":
		return account.Principal{UserID: 8, Role: account.RoleAdmin, ProfileComplete: true}, nil
	default:
		return account.Principal{}, account.ErrUnauthorized
	}
}

func TestCreateCompanyRequiresCompleteProfile(t *testing.T) {
	service := &companiesStub{}
	router := companyRouter(service)
	body := `{"name":"Команда","maxMembers":5,"joinType":"request"}`

	request := httptest.NewRequest(http.MethodPost, "/events/3/companies", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer incomplete")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "PROFILE_INCOMPLETE") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodPost, "/events/3/companies", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer complete")
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusCreated || service.eventID != 3 || service.ownerID != 7 || service.created.Name != "Команда" {
		t.Fatalf("status=%d body=%s input=%#v", response.Code, response.Body.String(), service.created)
	}
	if !strings.Contains(response.Body.String(), `"membersCount":1`) {
		t.Fatalf("unexpected company response: %s", response.Body.String())
	}
}

func TestCompanyReadRoutesUseOptionalIdentityAndPagination(t *testing.T) {
	service := &companiesStub{}
	router := companyRouter(service)

	request := httptest.NewRequest(http.MethodGet, "/events/3/companies", nil)
	request.Header.Set("Authorization", "Bearer admin")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || service.listViewer.UserID == nil || *service.listViewer.UserID != 8 || !service.listViewer.Admin {
		t.Fatalf("status=%d body=%s viewer=%#v", response.Code, response.Body.String(), service.listViewer)
	}

	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/companies/2/members?page=0", nil))
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "VALIDATION_ERROR") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestCreateCompanyMapsConflicts(t *testing.T) {
	service := &companiesStub{err: companies.ErrAlreadyInEventCompany}
	router := companyRouter(service)
	request := httptest.NewRequest(http.MethodPost, "/events/3/companies", strings.NewReader(`{"name":"Команда","maxMembers":5,"joinType":"open"}`))
	request.Header.Set("Authorization", "Bearer complete")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "ALREADY_IN_EVENT_COMPANY") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestUpdateCompanyPreservesNullablePatchSemantics(t *testing.T) {
	service := &companiesStub{}
	router := companyRouter(service)
	request := httptest.NewRequest(http.MethodPatch, "/companies/10", strings.NewReader(`{"description":null,"rules":"  Новые правила  "}`))
	request.Header.Set("Authorization", "Bearer complete")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if !service.patched.Description.Set || !service.patched.Description.Null || !service.patched.Rules.Set || service.patched.Rules.Value != "  Новые правила  " {
		t.Fatalf("unexpected patch: %#v", service.patched)
	}

	request = httptest.NewRequest(http.MethodPatch, "/companies/10", strings.NewReader(`{"maxMembers":null}`))
	request.Header.Set("Authorization", "Bearer complete")
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "VALIDATION_ERROR") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestJoinOpenCompanyRequiresCompleteProfile(t *testing.T) {
	service := &companiesStub{}
	router := companyRouter(service)

	request := httptest.NewRequest(http.MethodPost, "/companies/10/join", nil)
	request.Header.Set("Authorization", "Bearer incomplete")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "PROFILE_INCOMPLETE") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodPost, "/companies/10/join", nil)
	request.Header.Set("Authorization", "Bearer complete")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusCreated || service.joinedCompanyID != 10 || service.joinedUserID != 7 {
		t.Fatalf("status=%d company=%d user=%d", response.Code, service.joinedCompanyID, service.joinedUserID)
	}
}

func TestLeaveCompany(t *testing.T) {
	service := &companiesStub{}
	router := companyRouter(service)
	request := httptest.NewRequest(http.MethodDelete, "/companies/10/members/me", nil)
	request.Header.Set("Authorization", "Bearer complete")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || service.leftCompanyID != 10 || service.leftUserID != 7 {
		t.Fatalf("status=%d company=%d user=%d", response.Code, service.leftCompanyID, service.leftUserID)
	}

	service.err = companies.ErrOwnerCannotLeave
	request = httptest.NewRequest(http.MethodDelete, "/companies/10/members/me", nil)
	request.Header.Set("Authorization", "Bearer complete")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "OWNER_CANNOT_LEAVE") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestRemoveCompanyMember(t *testing.T) {
	service := &companiesStub{}
	router := companyRouter(service)
	request := httptest.NewRequest(http.MethodDelete, "/companies/10/members/9", nil)
	request.Header.Set("Authorization", "Bearer complete")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || service.removedCompanyID != 10 || service.removingOwnerID != 7 || service.removedUserID != 9 {
		t.Fatalf("status=%d company=%d owner=%d user=%d", response.Code, service.removedCompanyID, service.removingOwnerID, service.removedUserID)
	}

	service.err = companies.ErrOwnerCannotBeRemoved
	request = httptest.NewRequest(http.MethodDelete, "/companies/10/members/7", nil)
	request.Header.Set("Authorization", "Bearer complete")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "OWNER_CANNOT_BE_REMOVED") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestCreateCompanyApplicationAllowsEmptyBody(t *testing.T) {
	service := &companiesStub{}
	router := companyRouter(service)
	request := httptest.NewRequest(http.MethodPost, "/companies/10/applications", nil)
	request.Header.Set("Authorization", "Bearer complete")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusCreated || service.joinedCompanyID != 10 || service.joinedUserID != 7 {
		t.Fatalf("status=%d body=%s company=%d user=%d", response.Code, response.Body.String(), service.joinedCompanyID, service.joinedUserID)
	}
	if !strings.Contains(response.Body.String(), `"status":"pending"`) || !strings.Contains(response.Body.String(), `"message":null`) {
		t.Fatalf("unexpected response: %s", response.Body.String())
	}
}

func companyRouter(service Companies) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterRoutes(router.Group(""), service, authenticatorStub{})
	return router
}

func sampleCompany(ownerID int64) companies.Company {
	now := time.Date(2030, 1, 2, 12, 0, 0, 0, time.UTC)
	return companies.Company{
		ID: 10, EventID: 3, Name: "Команда", MaxMembers: 5,
		JoinType: companies.JoinTypeRequest, Owner: companies.UserShort{ID: ownerID, FirstName: "Иван"},
		MembersCount: 1, Status: companies.StatusActive, CreatedAt: now, UpdatedAt: now,
	}
}

func sampleApplication(userID int64) companies.Application {
	now := time.Date(2030, 1, 2, 12, 0, 0, 0, time.UTC)
	return companies.Application{
		ID: 20, CompanyID: 10, User: companies.UserShort{ID: userID, FirstName: "Иван"},
		Status: companies.ApplicationStatusPending, CreatedAt: now,
	}
}
