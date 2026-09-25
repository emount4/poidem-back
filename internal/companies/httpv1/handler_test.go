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
	created    companies.CreateInput
	eventID    int64
	ownerID    int64
	listViewer companies.Viewer
	patched    companies.Patch
	err        error
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
