package httpv1

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/emount4/poidem-back/internal/account"
	"github.com/emount4/poidem-back/internal/events"
	"github.com/gin-gonic/gin"
)

type eventsStub struct {
	created          events.CreateInput
	participationErr error
	joinedEventID    int64
	joinedUserID     int64
	listFilter       events.PublicFilter
	moderationReason *string
}

func (s *eventsStub) Create(_ context.Context, userID int64, input events.CreateInput) (events.Event, error) {
	s.created = input
	return sampleEvent(userID), nil
}
func (s *eventsStub) ListPublic(_ context.Context, filter events.PublicFilter, _ events.Page) ([]events.Event, int64, error) {
	s.listFilter = filter
	return []events.Event{}, 0, nil
}
func (*eventsStub) GetVisible(context.Context, int64, *int64) (events.Event, error) {
	return sampleEvent(1), nil
}
func (*eventsStub) ListParticipants(context.Context, int64, *int64, events.Page) ([]events.UserShort, int64, error) {
	return []events.UserShort{}, 0, nil
}
func (*eventsStub) ListMine(context.Context, int64, events.MyFilter, events.Page) ([]events.MyEvent, int64, error) {
	return []events.MyEvent{}, 0, nil
}
func (*eventsStub) ListAdmin(context.Context, events.AdminFilter, events.Page) ([]events.Event, int64, error) {
	return []events.Event{}, 0, nil
}
func (*eventsStub) GetAdmin(context.Context, int64) (events.Event, error) { return sampleEvent(1), nil }
func (*eventsStub) Update(context.Context, int64, events.Patch) (events.Event, error) {
	return sampleEvent(1), nil
}
func (*eventsStub) UpdateOwned(context.Context, int64, int64, events.Patch) (events.Event, error) {
	return sampleEvent(1), nil
}
func (*eventsStub) DeleteOwned(context.Context, int64, int64) error { return nil }
func (*eventsStub) DeleteAdmin(context.Context, int64) error        { return nil }
func (s *eventsStub) Transition(_ context.Context, _ int64, _ string, reason *string) (events.Event, error) {
	s.moderationReason = reason
	return sampleEvent(1), nil
}
func (s *eventsStub) JoinSolo(_ context.Context, eventID, userID int64) error {
	s.joinedEventID, s.joinedUserID = eventID, userID
	return s.participationErr
}
func (s *eventsStub) CancelSolo(context.Context, int64, int64) error {
	return s.participationErr
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

func TestCreateEventRequiresCompletedProfile(t *testing.T) {
	service := &eventsStub{}
	router := eventRouter(service)
	body := `{"title":"Прогулка","categoryId":1,"cityId":2,"startsAt":"2030-01-02T12:00:00+03:00","locationName":"Парк"}`

	request := httptest.NewRequest(http.MethodPost, "/events", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer incomplete")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "PROFILE_INCOMPLETE") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodPost, "/events", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer complete")
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusCreated || service.created.Title != "Прогулка" {
		t.Fatalf("status=%d body=%s input=%#v", response.Code, response.Body.String(), service.created)
	}
}

func TestEventFiltersAndAdminAuthorization(t *testing.T) {
	router := eventRouter(&eventsStub{})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/events?dateFrom=tomorrow", nil))
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "VALIDATION_ERROR") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}

	request := httptest.NewRequest(http.MethodGet, "/admin/events", nil)
	request.Header.Set("Authorization", "Bearer complete")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "FORBIDDEN") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/admin/events", nil)
	request.Header.Set("Authorization", "Bearer admin")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestMapBoundsAndTimeAliases(t *testing.T) {
	service := &eventsStub{}
	router := eventRouter(service)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet,
		"/events?west=30.10&south=59.80&east=30.55&north=60.10&from=2030-01-01T00:00:00%2B03:00&status=active", nil))
	if response.Code != http.StatusOK || service.listFilter.Bounds == nil || service.listFilter.Bounds.West != 30.10 || service.listFilter.DateFrom == nil {
		t.Fatalf("status=%d body=%s filter=%+v", response.Code, response.Body.String(), service.listFilter)
	}

	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/events?west=30&south=59", nil))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("incomplete bounds status=%d body=%s", response.Code, response.Body.String())
	}

	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet,
		"/events?from=2030-01-01T00:00:00Z&dateFrom=2030-01-01T00:00:00Z", nil))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("duplicate time alias status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestModerationReason(t *testing.T) {
	service := &eventsStub{}
	router := eventRouter(service)
	request := httptest.NewRequest(http.MethodPost, "/admin/events/1/reject", strings.NewReader(`{"reason":"Нет координат"}`))
	request.Header.Set("Authorization", "Bearer admin")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || service.moderationReason == nil || *service.moderationReason != "Нет координат" {
		t.Fatalf("status=%d body=%s reason=%v", response.Code, response.Body.String(), service.moderationReason)
	}
}

func TestSoloParticipationRoutes(t *testing.T) {
	service := &eventsStub{}
	router := eventRouter(service)

	request := httptest.NewRequest(http.MethodPost, "/events/3/solo-participation", nil)
	request.Header.Set("Authorization", "Bearer incomplete")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "PROFILE_INCOMPLETE") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodPost, "/events/3/solo-participation", nil)
	request.Header.Set("Authorization", "Bearer complete")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusCreated || service.joinedEventID != 3 || service.joinedUserID != 7 {
		t.Fatalf("status=%d event=%d user=%d", response.Code, service.joinedEventID, service.joinedUserID)
	}

	request = httptest.NewRequest(http.MethodDelete, "/events/3/solo-participation", nil)
	request.Header.Set("Authorization", "Bearer complete")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestSoloParticipationConflictMapping(t *testing.T) {
	service := &eventsStub{participationErr: events.ErrAlreadyInEventCompany}
	router := eventRouter(service)
	request := httptest.NewRequest(http.MethodPost, "/events/3/solo-participation", nil)
	request.Header.Set("Authorization", "Bearer complete")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "ALREADY_IN_EVENT_COMPANY") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func eventRouter(service Events) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterRoutes(router.Group(""), service, authenticatorStub{})
	return router
}

func sampleEvent(creatorID int64) events.Event {
	return events.Event{
		ID: 1, Title: "Прогулка", CategoryID: 1, CityID: 2,
		StartsAt:     time.Date(2030, 1, 2, 12, 0, 0, 0, time.FixedZone("MSK", 3*60*60)),
		LocationName: "Парк", Status: events.StatusPending,
		Location:  &events.Location{Latitude: 55.75, Longitude: 37.62, Source: "manual"},
		Creator:   events.UserShort{ID: creatorID, FirstName: "Иван"},
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
}
