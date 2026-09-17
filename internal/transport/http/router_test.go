package http

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/emount4/poidem-back/internal/logger"
	"github.com/gin-gonic/gin"
)

func TestHealth(t *testing.T) {
	var logs bytes.Buffer
	router := NewRouter(logger.New(&logs))
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
	router := NewRouter(logger.New(&logs))
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
}
