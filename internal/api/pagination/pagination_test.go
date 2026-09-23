package pagination

import (
	"encoding/json"
	"net/url"
	"reflect"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name       string
		query      url.Values
		want       Params
		wantFields map[string][]string
	}{
		{name: "defaults", query: url.Values{}, want: Params{Page: 1, Limit: 20}},
		{name: "values", query: url.Values{"page": {"3"}, "limit": {"50"}}, want: Params{Page: 3, Limit: 50}},
		{name: "invalid", query: url.Values{"page": {"0"}, "limit": {"101"}}, wantFields: map[string][]string{
			"page": {"Должно быть не меньше 1"}, "limit": {"Не должно превышать 100"},
		}},
		{name: "not integers", query: url.Values{"page": {"first"}, "limit": {""}}, wantFields: map[string][]string{
			"page": {"Должно быть целым числом"}, "limit": {"Должно быть целым числом"},
		}},
		{name: "repeated", query: url.Values{"page": {"1", "2"}}, wantFields: map[string][]string{
			"page": {"Параметр должен быть указан один раз"},
		}},
		{name: "offset overflow", query: url.Values{"page": {"9223372036854775807"}, "limit": {"100"}}, wantFields: map[string][]string{
			"page": {"Слишком большое значение"},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, fields := Parse(tt.query)
			if !reflect.DeepEqual(got, tt.want) || !reflect.DeepEqual(map[string][]string(fields), tt.wantFields) {
				t.Fatalf("Parse() = %+v, %v; want %+v, %v", got, fields, tt.want, tt.wantFields)
			}
		})
	}
}

func TestResponse(t *testing.T) {
	response := NewResponse[string](nil, Params{Page: 3, Limit: 20}, 41)
	if response.Pagination.TotalPages != 3 || response.Pagination.Total != 41 {
		t.Fatalf("unexpected metadata: %+v", response.Pagination)
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"items":[],"pagination":{"page":3,"limit":20,"total":41,"totalPages":3}}`
	if string(encoded) != want {
		t.Fatalf("response = %s, want %s", encoded, want)
	}

	empty := NewResponse([]string{}, Params{Page: 1, Limit: 20}, 0)
	if empty.Pagination.TotalPages != 0 {
		t.Fatalf("empty totalPages = %d, want 0", empty.Pagination.TotalPages)
	}
	if (Params{Page: 2, Limit: 20}).Offset() != 20 {
		t.Fatal("unexpected offset")
	}
}
