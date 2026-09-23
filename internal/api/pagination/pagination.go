// Package pagination provides the shared HTTP pagination contract.
package pagination

import (
	"math"
	"net/url"
	"strconv"

	"github.com/emount4/poidem-back/internal/api/apierr"
)

const (
	DefaultPage  int64 = 1
	DefaultLimit int64 = 20
	MaxLimit     int64 = 100
)

type Params struct {
	Page  int64
	Limit int64
}

func (p Params) Offset() int64 {
	return (p.Page - 1) * p.Limit
}

type Metadata struct {
	Page       int64 `json:"page"`
	Limit      int64 `json:"limit"`
	Total      int64 `json:"total"`
	TotalPages int64 `json:"totalPages"`
}

type Response[T any] struct {
	Items      []T      `json:"items"`
	Pagination Metadata `json:"pagination"`
}

// Parse reads page and limit without accepting ambiguous repeated parameters.
func Parse(values url.Values) (Params, apierr.FieldErrors) {
	params := Params{Page: DefaultPage, Limit: DefaultLimit}
	fields := apierr.FieldErrors{}
	params.Page = parsePositive(values, "page", DefaultPage, 0, fields)
	params.Limit = parsePositive(values, "limit", DefaultLimit, MaxLimit, fields)
	if len(fields) == 0 && params.Page-1 > math.MaxInt64/params.Limit {
		fields["page"] = []string{"Слишком большое значение"}
	}
	if len(fields) > 0 {
		return Params{}, fields
	}
	return params, nil
}

func NewResponse[T any](items []T, params Params, total int64) Response[T] {
	if items == nil {
		items = []T{}
	}
	totalPages := total / params.Limit
	if total%params.Limit != 0 {
		totalPages++
	}
	return Response[T]{
		Items: items,
		Pagination: Metadata{
			Page: params.Page, Limit: params.Limit, Total: total, TotalPages: totalPages,
		},
	}
}

func parsePositive(values url.Values, name string, fallback, maximum int64, fields apierr.FieldErrors) int64 {
	raw, exists := values[name]
	if !exists {
		return fallback
	}
	if len(raw) != 1 {
		fields[name] = []string{"Параметр должен быть указан один раз"}
		return 0
	}
	value, err := strconv.ParseInt(raw[0], 10, 64)
	if err != nil {
		fields[name] = []string{"Должно быть целым числом"}
		return 0
	}
	if value < 1 {
		fields[name] = []string{"Должно быть не меньше 1"}
		return 0
	}
	if maximum > 0 && value > maximum {
		fields[name] = []string{"Не должно превышать " + strconv.FormatInt(maximum, 10)}
		return 0
	}
	return value
}
