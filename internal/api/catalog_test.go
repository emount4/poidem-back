package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/emount4/poidem-back/internal/catalog"
	"github.com/emount4/poidem-back/internal/platform/logging"
)

func TestDictionaryRoutes(t *testing.T) {
	slug := "moscow"
	catalog := &dictionaryCatalogStub{
		cities: []catalog.Item{
			{ID: 1, Name: "Москва", Slug: &slug},
			{ID: 2, Name: "Без slug"},
		},
		interests:  []catalog.Item{},
		categories: []catalog.Item{{ID: 3, Name: "Спорт"}},
	}
	for _, tc := range []struct {
		path string
		body string
	}{
		{"/api/v1/cities", `[{"id":1,"name":"Москва","slug":"moscow"},{"id":2,"name":"Без slug","slug":null}]`},
		{"/api/v1/interests", `[]`},
		{"/api/v1/event-categories", `[{"id":3,"name":"Спорт","slug":null}]`},
	} {
		t.Run(tc.path, func(t *testing.T) {
			var logs bytes.Buffer
			router := NewRouter(logging.New(&logs), testDependencies(catalog))
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if response.Code != http.StatusOK || response.Body.String() != tc.body {
				t.Fatalf("unexpected response: %d %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestDictionaryInternalError(t *testing.T) {
	var logs bytes.Buffer
	catalog := &dictionaryCatalogStub{err: errors.New("database password must not leak")}
	router := NewRouter(logging.New(&logs), testDependencies(catalog))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/cities", nil))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", response.Code)
	}
	var body struct {
		Error struct {
			Code    string         `json:"code"`
			Message string         `json:"message"`
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != "INTERNAL_ERROR" || body.Error.Message == "" || body.Error.Details == nil {
		t.Fatalf("unexpected error response: %+v", body)
	}
	if strings.Contains(response.Body.String(), "password") {
		t.Fatal("response leaked internal error")
	}
	if !strings.Contains(logs.String(), "database password must not leak") {
		t.Fatal("internal error was not preserved in server logs")
	}
}

func TestAPINotFoundError(t *testing.T) {
	var logs bytes.Buffer
	router := NewRouter(logging.New(&logs), testDependencies(nil))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/missing", nil))
	if response.Code != http.StatusNotFound || response.Body.String() != `{"error":{"code":"NOT_FOUND","message":"Ресурс не найден","details":{}}}` {
		t.Fatalf("unexpected response: %d %s", response.Code, response.Body.String())
	}
}
