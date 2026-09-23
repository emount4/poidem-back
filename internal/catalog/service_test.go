package catalog

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type dictionaryRepositoryStub struct {
	items []Item
	err   error
}

func (s dictionaryRepositoryStub) Cities(context.Context) ([]Item, error) {
	return s.items, s.err
}

func (s dictionaryRepositoryStub) Interests(context.Context) ([]Item, error) {
	return s.items, s.err
}

func (s dictionaryRepositoryStub) EventCategories(context.Context) ([]Item, error) {
	return s.items, s.err
}

func TestDictionariesReturnEmptyArray(t *testing.T) {
	service := NewService(dictionaryRepositoryStub{})
	for name, load := range map[string]func(context.Context) ([]Item, error){
		"cities": service.Cities, "interests": service.Interests, "categories": service.EventCategories,
	} {
		t.Run(name, func(t *testing.T) {
			items, err := load(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if items == nil || len(items) != 0 {
				t.Fatalf("expected non-nil empty slice, got %#v", items)
			}
		})
	}
}

func TestDictionariesPreserveErrorCause(t *testing.T) {
	cause := errors.New("connection lost")
	service := NewService(dictionaryRepositoryStub{err: cause})
	_, err := service.Cities(context.Background())
	if !errors.Is(err, cause) || !strings.Contains(err.Error(), "list cities") {
		t.Fatalf("unexpected error: %v", err)
	}
}
