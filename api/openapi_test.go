package api_test

import (
	"os"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
)

func TestOpenAPIContract(t *testing.T) {
	content, err := os.ReadFile("openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := yaml.Unmarshal(content, &document); err != nil {
		t.Fatalf("invalid OpenAPI YAML: %v", err)
	}
	if document["openapi"] != "3.0.3" {
		t.Fatalf("unexpected OpenAPI version: %v", document["openapi"])
	}
	assertLocalReferences(t, document, document)

	checks := map[string]string{
		"/users/me/events":                    "#/components/schemas/MyEventList",
		"/users/me/companies":                 "#/components/schemas/CompanyList",
		"/users/me/applications":              "#/components/schemas/ApplicationList",
		"/events/{eventId}/participants":      "#/components/schemas/UserShortList",
		"/events/{eventId}/companies":         "#/components/schemas/CompanyList",
		"/companies/{companyId}/members":      "#/components/schemas/UserShortList",
		"/companies/{companyId}/applications": "#/components/schemas/ApplicationList",
		"/admin/users":                        "#/components/schemas/UserList",
		"/admin/events":                       "#/components/schemas/EventList",
		"/admin/companies":                    "#/components/schemas/CompanyList",
		"/admin/reports":                      "#/components/schemas/ReportList",
	}
	for path, want := range checks {
		got := responseSchemaRef(t, document, path)
		if got != want {
			t.Errorf("GET %s response schema = %q, want %q", path, got, want)
		}
	}
}

func responseSchemaRef(t *testing.T, document map[string]any, path string) string {
	t.Helper()
	value := document["paths"].(map[string]any)[path].(map[string]any)["get"].(map[string]any)
	responses := value["responses"].(map[string]any)
	response := responses["200"].(map[string]any)
	content := response["content"].(map[string]any)["application/json"].(map[string]any)
	schema := content["schema"].(map[string]any)
	ref, _ := schema["$ref"].(string)
	return ref
}

func assertLocalReferences(t *testing.T, root map[string]any, node any) {
	t.Helper()
	switch value := node.(type) {
	case map[string]any:
		for key, child := range value {
			if key == "$ref" {
				ref, ok := child.(string)
				if !ok || !strings.HasPrefix(ref, "#/") || !referenceExists(root, ref) {
					t.Errorf("unresolved local reference: %v", child)
				}
				continue
			}
			assertLocalReferences(t, root, child)
		}
	case []any:
		for _, child := range value {
			assertLocalReferences(t, root, child)
		}
	}
}

func referenceExists(root map[string]any, ref string) bool {
	var current any = root
	for _, part := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
		object, ok := current.(map[string]any)
		if !ok {
			return false
		}
		current, ok = object[part]
		if !ok {
			return false
		}
	}
	return true
}
