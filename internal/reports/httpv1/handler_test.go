package httpv1

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/emount4/poidem-back/internal/account"
	"github.com/emount4/poidem-back/internal/reports"
	"github.com/gin-gonic/gin"
)

type reportsStub struct {
	input  reports.CreateInput
	status string
	err    error
}

func (s *reportsStub) Create(_ context.Context, _ int64, input reports.CreateInput) (reports.Report, error) {
	s.input = input
	return sampleReport(), s.err
}
func (s *reportsStub) ListAdmin(_ context.Context, status string, _ reports.Page) ([]reports.Report, int64, error) {
	s.status = status
	return []reports.Report{sampleReport()}, 1, s.err
}
func (s *reportsStub) GetAdmin(context.Context, int64) (reports.Report, error) {
	return sampleReport(), s.err
}
func (s *reportsStub) Resolve(_ context.Context, _ int64, adminID int64, action string) (reports.Report, error) {
	item := sampleReport()
	item.ResolvedBy = &reports.UserShort{ID: adminID, FirstName: "Админ"}
	if action == "resolve" {
		item.Status = reports.StatusResolved
	} else {
		item.Status = reports.StatusRejected
	}
	return item, s.err
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

func TestCreateReport(t *testing.T) {
	service := &reportsStub{}
	router := reportRouter(service)
	body := `{"targetType":"company","targetId":10,"reason":"Спам"}`

	request := httptest.NewRequest(http.MethodPost, "/reports", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer incomplete")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "PROFILE_INCOMPLETE") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodPost, "/reports", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer complete")
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusCreated || service.input.TargetType != reports.TargetCompany || service.input.TargetID != 10 {
		t.Fatalf("status=%d body=%s input=%+v", response.Code, response.Body.String(), service.input)
	}
}

func TestAdminReportRoutes(t *testing.T) {
	service := &reportsStub{}
	router := reportRouter(service)

	request := httptest.NewRequest(http.MethodGet, "/admin/reports", nil)
	request.Header.Set("Authorization", "Bearer complete")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/admin/reports?status=pending", nil)
	request.Header.Set("Authorization", "Bearer admin")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || service.status != reports.StatusPending || !strings.Contains(response.Body.String(), `"total":1`) {
		t.Fatalf("status=%d body=%s filter=%s", response.Code, response.Body.String(), service.status)
	}

	request = httptest.NewRequest(http.MethodGet, "/admin/reports/1", nil)
	request.Header.Set("Authorization", "Bearer admin")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"id":1`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodPost, "/admin/reports/1/resolve", nil)
	request.Header.Set("Authorization", "Bearer admin")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"status":"resolved"`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}

	service.err = reports.ErrAlreadyResolved
	request = httptest.NewRequest(http.MethodPost, "/admin/reports/1/reject", nil)
	request.Header.Set("Authorization", "Bearer admin")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "REPORT_ALREADY_RESOLVED") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestAdminReportStatusValidation(t *testing.T) {
	router := reportRouter(&reportsStub{})
	request := httptest.NewRequest(http.MethodGet, "/admin/reports?status=unknown", nil)
	request.Header.Set("Authorization", "Bearer admin")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "VALIDATION_ERROR") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func reportRouter(service Reports) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterRoutes(router.Group(""), service, authenticatorStub{})
	return router
}

func sampleReport() reports.Report {
	now := time.Date(2030, 1, 2, 12, 0, 0, 0, time.UTC)
	return reports.Report{
		ID: 1, TargetType: reports.TargetCompany, TargetID: 10, Reason: "Спам",
		Author: reports.UserShort{ID: 7, FirstName: "Иван"}, Status: reports.StatusPending,
		CreatedAt: now,
	}
}
