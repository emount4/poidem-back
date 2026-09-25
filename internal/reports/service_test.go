package reports

import (
	"context"
	"errors"
	"testing"
	"time"
)

type storeStub struct {
	input CreateInput
}

func (s *storeStub) Create(_ context.Context, _ int64, input CreateInput, _ time.Time) (Report, error) {
	s.input = input
	return Report{ID: 1, Reason: input.Reason}, nil
}
func (*storeStub) ListAdmin(context.Context, string, Page) ([]Report, int64, error) {
	return nil, 0, nil
}
func (*storeStub) GetAdmin(context.Context, int64) (Report, error) { return Report{}, nil }
func (*storeStub) Resolve(context.Context, int64, int64, string, time.Time) (Report, error) {
	return Report{}, nil
}

func TestCreateNormalizesAndValidates(t *testing.T) {
	store := &storeStub{}
	service := NewService(store)
	description := "  Подробности  "
	item, err := service.Create(context.Background(), 7, CreateInput{
		TargetType: " user ", TargetID: 9, Reason: "  Спам  ", Description: &description,
	})
	if err != nil || item.ID != 1 {
		t.Fatalf("item=%+v err=%v", item, err)
	}
	if store.input.TargetType != TargetUser || store.input.Reason != "Спам" || *store.input.Description != "Подробности" {
		t.Fatalf("input not normalized: %+v", store.input)
	}

	_, err = service.Create(context.Background(), 7, CreateInput{TargetType: "other", Reason: ""})
	var validation *ValidationError
	if !errors.As(err, &validation) || len(validation.Fields) != 3 {
		t.Fatalf("validation error = %#v", err)
	}
}

func TestResolveRejectsUnknownAction(t *testing.T) {
	_, err := NewService(&storeStub{}).Resolve(context.Background(), 1, 2, "close")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v", err)
	}
}
