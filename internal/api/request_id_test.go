package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/emount4/poidem-back/internal/platform/logging"
	"github.com/emount4/poidem-back/internal/platform/requestid"
	"github.com/gin-gonic/gin"
)

func TestRequestID(t *testing.T) {
	generated := make(map[string]bool)
	for _, tc := range []struct {
		name    string
		headers []string
		keep    bool
	}{
		{name: "missing"},
		{name: "another request"},
		{name: "client ID", headers: []string{"client-123_abc.XYZ"}, keep: true},
		{name: "maximum length", headers: []string{strings.Repeat("a", 128)}, keep: true},
		{name: "empty", headers: []string{""}},
		{name: "too long", headers: []string{strings.Repeat("a", 129)}},
		{name: "whitespace", headers: []string{"client id"}},
		{name: "control character", headers: []string{"client\nid"}},
		{name: "multiple values", headers: []string{"first", "second"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			router := NewRouter(logging.New(&logs), testDependencies(nil))
			var contextID, headerID string
			router.GET("/id", func(c *gin.Context) {
				contextID = requestid.FromContext(c.Request.Context())
				headerID = c.GetHeader("X-Request-ID")
				c.Status(http.StatusNoContent)
			})
			req := httptest.NewRequest(http.MethodGet, "/id", nil)
			for _, value := range tc.headers {
				req.Header.Add("X-Request-ID", value)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			id := response.Header().Get("X-Request-ID")
			if response.Code != http.StatusNoContent || id == "" {
				t.Fatal("request must succeed with a nonempty response ID")
			}
			if tc.keep {
				if id != tc.headers[0] {
					t.Fatal("valid client ID was not preserved")
				}
			} else {
				if generated[id] || strings.ContainsAny(id, " \r\n\t") {
					t.Fatal("generated ID must be unique and suitable for a header")
				}
				for _, value := range tc.headers {
					if id == value {
						t.Fatal("invalid or ambiguous client ID was preserved")
					}
				}
				generated[id] = true
			}
			var entry map[string]any
			if err := json.Unmarshal(logs.Bytes(), &entry); err != nil {
				t.Fatal(err)
			}
			if contextID != id || headerID != id || entry["request_id"] != id {
				t.Fatal("context, request, response and log must share the same ID")
			}
		})
	}
}

func TestRequestIDOnNotFound(t *testing.T) {
	var logs bytes.Buffer
	router := NewRouter(logging.New(&logs), testDependencies(nil))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/missing", nil))
	if response.Code != http.StatusNotFound || response.Header().Get("X-Request-ID") == "" {
		t.Fatal("404 responses must include a request ID")
	}
}
