package catalog

import (
	"context"
	"fmt"
)

// DictionaryRepository provides reference data to application scenarios.
type Repository interface {
	Cities(context.Context) ([]Item, error)
	Interests(context.Context) ([]Item, error)
	EventCategories(context.Context) ([]Item, error)
}

// Dictionaries implements read-only reference-data scenarios.
type Service struct {
	repository Repository
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

func (s *Service) Cities(ctx context.Context) ([]Item, error) {
	items, err := s.repository.Cities(ctx)
	if err != nil {
		return nil, fmt.Errorf("list cities: %w", err)
	}
	return nonNil(items), nil
}

func (s *Service) Interests(ctx context.Context) ([]Item, error) {
	items, err := s.repository.Interests(ctx)
	if err != nil {
		return nil, fmt.Errorf("list interests: %w", err)
	}
	return nonNil(items), nil
}

func (s *Service) EventCategories(ctx context.Context) ([]Item, error) {
	items, err := s.repository.EventCategories(ctx)
	if err != nil {
		return nil, fmt.Errorf("list event categories: %w", err)
	}
	return nonNil(items), nil
}

func nonNil(items []Item) []Item {
	if items == nil {
		return []Item{}
	}
	return items
}
