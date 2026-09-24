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
	assertOperations(t, document)

	checks := map[string]string{
		"/events":                             "#/components/schemas/EventList",
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

func assertOperations(t *testing.T, document map[string]any) {
	t.Helper()
	methods := map[string]bool{"get": true, "post": true, "put": true, "patch": true, "delete": true}
	knownCodes := map[string]bool{}
	schemas := document["components"].(map[string]any)["schemas"].(map[string]any)
	for _, value := range schemas["ErrorCode"].(map[string]any)["enum"].([]any) {
		knownCodes[value.(string)] = true
	}
	operationIDs := map[string]string{}
	knownTags := map[string]bool{}
	for _, rawTag := range document["tags"].([]any) {
		knownTags[rawTag.(map[string]any)["name"].(string)] = true
	}
	for path, rawPath := range document["paths"].(map[string]any) {
		for method, rawOperation := range rawPath.(map[string]any) {
			if !methods[method] {
				continue
			}
			operation := rawOperation.(map[string]any)
			operationID, ok := operation["operationId"].(string)
			if !ok || operationID == "" {
				t.Errorf("%s %s has no operationId", method, path)
				continue
			}
			if previous, exists := operationIDs[operationID]; exists {
				t.Errorf("duplicate operationId %q on %s %s and %s", operationID, method, path, previous)
			}
			operationIDs[operationID] = method + " " + path
			tags, ok := operation["tags"].([]any)
			if !ok || len(tags) != 1 || !knownTags[tags[0].(string)] {
				t.Errorf("%s %s must have one declared tag", method, path)
			}
			responses := operation["responses"].(map[string]any)
			mappings, _ := operation["x-error-codes"].(map[string]any)
			for status, rawCodes := range mappings {
				if _, exists := responses[status]; !exists {
					t.Errorf("%s %s maps error status %s without documenting its response", method, path, status)
				}
				for _, rawCode := range rawCodes.([]any) {
					code := rawCode.(string)
					if !knownCodes[code] {
						t.Errorf("%s %s uses unknown error code %s", method, path, code)
					}
				}
			}
		}
	}
	if len(operationIDs) != 53 {
		t.Errorf("operation count = %d, want 53", len(operationIDs))
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
