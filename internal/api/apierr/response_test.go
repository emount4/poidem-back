package apierr

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestWriteValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)

	WriteValidation(context, FieldErrors{"cityId": {"Город не найден"}})

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.Code)
	}
	var body Response
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	fields, ok := body.Error.Details["fields"].(map[string]any)
	if !ok || fields["cityId"] == nil {
		t.Fatalf("unexpected validation details: %#v", body.Error.Details)
	}
}

func TestWriteInternalKeepsPrivateCause(t *testing.T) {
	gin.SetMode(gin.TestMode)
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	cause := errors.New("database secret")

	WriteInternal(context, cause)

	if response.Code != http.StatusInternalServerError || len(context.Errors) != 1 {
		t.Fatalf("status = %d, errors = %v", response.Code, context.Errors)
	}
	if !errors.Is(context.Errors[0], cause) || context.Errors[0].Type != gin.ErrorTypePrivate {
		t.Fatalf("private cause was not preserved: %v", context.Errors[0])
	}
	if body := response.Body.String(); body == "" || contains(body, "database secret") {
		t.Fatalf("unsafe response: %s", body)
	}
}

func contains(value, part string) bool {
	for i := 0; i+len(part) <= len(value); i++ {
		if value[i:i+len(part)] == part {
			return true
		}
	}
	return false
}
