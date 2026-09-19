package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/emount4/poidem-back/internal/logger"
)

func TestAPIV1(t *testing.T) {
	var logs bytes.Buffer
	router := NewRouter(logger.New(&logs), func(context.Context) error { return nil })
	req := httptest.NewRequest(http.MethodGet, "/api/v1", nil)
	req.Header.Set("X-Request-ID", "api-v1-test")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	if response.Code != http.StatusOK || response.Body.String() != `{"version":"v1"}` {
		t.Fatalf("unexpected version response: %d %s", response.Code, response.Body.String())
	}
	if response.Header().Get("X-Request-ID") != "api-v1-test" {
		t.Fatal("versioned API must use the shared request ID middleware")
	}
	var entry map[string]any
	if err := json.Unmarshal(logs.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}
	if entry["path"] != "/api/v1" || entry["request_id"] != "api-v1-test" {
		t.Fatalf("unexpected API request log: %v", entry)
	}
}

func TestUnknownAPIVersion(t *testing.T) {
	var logs bytes.Buffer
	router := NewRouter(logger.New(&logs), func(context.Context) error { return nil })
	for _, path := range []string{"/api/v2", "/api/v99", "/v1", "/api/v1/health", "/api/v1/ready"} {
		t.Run(path, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
			if response.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404", response.Code)
			}
		})
	}
}
