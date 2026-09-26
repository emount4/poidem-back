package httpv1

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/emount4/poidem-back/internal/account"
	"github.com/gin-gonic/gin"
)

type profilesStub struct {
	profile account.Profile
	patch   account.ProfilePatch
	err     error
	userID  int64
}

func (s *profilesStub) Get(_ context.Context, userID int64) (account.Profile, error) {
	s.userID = userID
	return s.profile, s.err
}
func (s *profilesStub) Update(_ context.Context, userID int64, patch account.ProfilePatch) (account.Profile, error) {
	s.userID, s.patch = userID, patch
	return s.profile, s.err
}

func profileRouter(profiles Profiles) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterProfileRoutes(router.Group("/api/v1"), profiles, &authenticatorStub{
		principal: account.Principal{UserID: 7, SessionID: 11, Role: account.RoleUser},
	})
	return router
}

func authenticatedRequest(method, target, body string) *http.Request {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer access")
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	return request
}

func TestGetProfileRoutesReturnSameUser(t *testing.T) {
	slug := "moscow"
	gender := "female"
	birthDate := time.Date(2000, 4, 15, 0, 0, 0, 0, time.UTC)
	profiles := &profilesStub{profile: account.Profile{
		ID: 7, FirstName: "Анна", City: &account.DictionaryItem{ID: 1, Name: "Москва", Slug: &slug},
		Gender: &gender, BirthDate: &birthDate,
		Interests: []account.DictionaryItem{}, Role: account.RoleUser, Status: account.StatusActive,
		CreatedAt: time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC),
	}}
	router := profileRouter(profiles)
	for _, path := range []string{"/api/v1/auth/me", "/api/v1/users/me"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, authenticatedRequest(http.MethodGet, path, ""))
		if response.Code != http.StatusOK {
			t.Fatalf("%s status = %d: %s", path, response.Code, response.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body["id"] != float64(7) || body["isProfileComplete"] != true || body["gender"] != "female" || body["birthDate"] != "2000-04-15" {
			t.Fatalf("unexpected profile: %v", body)
		}
		interests, ok := body["interests"].([]any)
		if !ok || len(interests) != 0 {
			t.Fatalf("interests must be []: %v", body["interests"])
		}
	}
}

func TestGetPublicUserRequiresAuthAndReturnsSafeProfile(t *testing.T) {
	profiles := &profilesStub{profile: account.Profile{
		ID: 42, FirstName: "Иван", Interests: []account.DictionaryItem{},
		Role: account.RoleUser, Status: account.StatusActive, CreatedAt: time.Now(),
	}}
	router := profileRouter(profiles)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, authenticatedRequest(http.MethodGet, "/api/v1/users/42", ""))
	if response.Code != http.StatusOK || profiles.userID != 42 {
		t.Fatalf("status=%d body=%s user=%d", response.Code, response.Body.String(), profiles.userID)
	}
	if strings.Contains(response.Body.String(), "username") || strings.Contains(response.Body.String(), "password") {
		t.Fatalf("private credentials leaked: %s", response.Body.String())
	}

	unauthorized := httptest.NewRecorder()
	router.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/v1/users/42", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status=%d", unauthorized.Code)
	}
}

func TestPatchProfilePreservesMissingAndNull(t *testing.T) {
	profiles := &profilesStub{profile: account.Profile{ID: 7, FirstName: "Анна", Interests: []account.DictionaryItem{}}}
	router := profileRouter(profiles)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, authenticatedRequest(http.MethodPatch, "/api/v1/users/me", `{"lastName":null,"about":"О себе","gender":"female","birthDate":"2000-04-15","interestIds":[]}`))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	patch := profiles.patch
	if patch.FirstName.Set || !patch.LastName.Set || !patch.LastName.Null ||
		!patch.About.Set || patch.About.Null || patch.About.Value != "О себе" ||
		!patch.Gender.Set || patch.Gender.Value != "female" ||
		!patch.BirthDate.Set || patch.BirthDate.Value.Format(time.DateOnly) != "2000-04-15" ||
		!patch.InterestIDs.Set || patch.InterestIDs.Value == nil || len(patch.InterestIDs.Value) != 0 {
		t.Fatalf("PATCH semantics lost: %+v", patch)
	}
}

func TestPatchProfileValidation(t *testing.T) {
	for _, tc := range []struct {
		name, body, field string
	}{
		{"null first name", `{"firstName":null}`, "firstName"},
		{"null interests", `{"interestIds":null}`, "interestIds"},
		{"invalid birth date", `{"birthDate":"15.04.2000"}`, "birthDate"},
		{"unknown field", `{"unknown":1}`, "body"},
		{"invalid JSON", `{`, "body"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			profiles := &profilesStub{}
			response := httptest.NewRecorder()
			profileRouter(profiles).ServeHTTP(response, authenticatedRequest(http.MethodPatch, "/api/v1/users/me", tc.body))
			if response.Code != http.StatusBadRequest || errorCode(t, response) != "VALIDATION_ERROR" {
				t.Fatalf("unexpected response: %d %s", response.Code, response.Body.String())
			}
			if profiles.patch.FirstName.Set || profiles.patch.InterestIDs.Set {
				t.Fatal("invalid request reached profile service")
			}
		})
	}
}

func TestPatchProfileMapsRepositoryValidation(t *testing.T) {
	for _, tc := range []struct {
		err   error
		field string
	}{
		{account.ErrCityRequired, "cityId"},
		{account.ErrCityNotFound, "cityId"},
		{account.ErrInterestsInvalid, "interestIds"},
		{&account.ValidationError{Fields: map[string][]string{"firstName": {"bad"}}}, "firstName"},
	} {
		profiles := &profilesStub{err: tc.err}
		response := httptest.NewRecorder()
		profileRouter(profiles).ServeHTTP(response, authenticatedRequest(http.MethodPatch, "/api/v1/users/me", `{}`))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("error %v: status = %d", tc.err, response.Code)
		}
		var body struct {
			Error struct {
				Details struct {
					Fields map[string][]string `json:"fields"`
				} `json:"details"`
			} `json:"error"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || len(body.Error.Details.Fields[tc.field]) == 0 {
			t.Fatalf("field error missing: %s", response.Body.String())
		}
	}
}

func TestGetProfileInternalErrorIsHidden(t *testing.T) {
	profiles := &profilesStub{err: errors.New("database secret")}
	response := httptest.NewRecorder()
	profileRouter(profiles).ServeHTTP(response, authenticatedRequest(http.MethodGet, "/api/v1/auth/me", ""))
	if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), "database secret") {
		t.Fatalf("unexpected response: %d %s", response.Code, response.Body.String())
	}
}
