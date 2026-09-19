package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/emount4/poidem-back/internal/logger"
	"github.com/gin-gonic/gin"
)

func TestHealth(t *testing.T) {
	var logs bytes.Buffer
	router := NewRouter(logger.New(&logs), func(context.Context) error { return nil })
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health", nil))
	if response.Code != http.StatusOK || response.Body.String() != `{"status":"ok"}` {
		t.Fatalf("unexpected response: %d %s", response.Code, response.Body.String())
	}
	var entry map[string]any
	if err := json.Unmarshal(logs.Bytes(), &entry); err != nil {
		t.Fatalf("invalid JSON log: %v", err)
	}
	if entry["path"] != "/health" || entry["status"] != float64(http.StatusOK) {
		t.Fatalf("unexpected request log: %v", entry)
	}
}

func TestRecovery(t *testing.T) {
	var logs bytes.Buffer
	router := NewRouter(logger.New(&logs), func(context.Context) error { return nil })
	router.GET("/panic", func(c *gin.Context) { panic("test panic") })
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/panic", nil))
	if response.Code != http.StatusInternalServerError || response.Body.String() != `{"error":"internal server error"}` {
		t.Fatalf("unexpected response: %d %s", response.Code, response.Body.String())
	}
	lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected panic and request logs, got %s", logs.String())
	}
	var panicLog, requestLog map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &panicLog); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(lines[1]), &requestLog); err != nil {
		t.Fatal(err)
	}
	if panicLog["msg"] != "http panic" || panicLog["stack"] == "" || panicLog["stack"] == nil {
		t.Fatalf("unexpected panic log: %v", panicLog)
	}
	if requestLog["status"] != float64(http.StatusInternalServerError) || requestLog["level"] != "ERROR" {
		t.Fatalf("unexpected request log: %v", requestLog)
	}
	id := response.Header().Get("X-Request-ID")
	if id == "" || panicLog["request_id"] != id || requestLog["request_id"] != id {
		t.Fatal("panic log, request log and response must share the same request ID")
	}
}

func TestReadiness(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
	}{
		{"database available", nil, http.StatusOK},
		{"database unavailable", errors.New("connection refused"), http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			router := NewRouter(logger.New(&logs), func(ctx context.Context) error {
				if _, ok := ctx.Deadline(); !ok {
					t.Error("database readiness check must have a timeout")
				}
				return tc.err
			})
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/ready", nil))
			if response.Code != tc.status {
				t.Fatalf("status = %d, want %d", response.Code, tc.status)
			}
			if strings.Contains(response.Body.String(), "connection refused") {
				t.Fatal("readiness response exposes internal database error")
			}
		})
	}
}
