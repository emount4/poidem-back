package companies

import (
	"context"
	"testing"
	"time"
)

type storeStub struct {
	created CreateInput
	eventID int64
	ownerID int64
}

func (s *storeStub) Create(_ context.Context, eventID, ownerID int64, input CreateInput, _ time.Time) (Company, error) {
	s.eventID, s.ownerID, s.created = eventID, ownerID, input
	return Company{ID: 10, Name: input.Name}, nil
}
func (*storeStub) ListEvent(context.Context, int64, Viewer, Page, time.Time) ([]Company, int64, error) {
	return nil, 0, nil
}
func (*storeStub) GetVisible(context.Context, int64, Viewer) (Company, error) {
	return Company{}, nil
}
func (*storeStub) ListMembers(context.Context, int64, Viewer, Page) ([]UserShort, int64, error) {
	return nil, 0, nil
}
func (*storeStub) ListMine(context.Context, int64, Page) ([]Company, int64, error) {
	return nil, 0, nil
}
func (s *storeStub) Update(_ context.Context, _, _ int64, patch Patch, _ time.Time) (Company, error) {
	return Company{ID: 10, Name: patch.Name.Value}, nil
}
func (*storeStub) SetRecruitment(context.Context, int64, int64, string, time.Time) (Company, error) {
	return Company{}, nil
}
func (*storeStub) Delete(context.Context, int64, int64, time.Time) error { return nil }

func TestCreateNormalizesAndDelegates(t *testing.T) {
	store := &storeStub{}
	service := NewService(store)
	description := "  Любим гулять  "
	rules := "  Без опозданий  "
	item, err := service.Create(context.Background(), 3, 7, CreateInput{
		Name: "  Команда  ", Description: &description, MaxMembers: 5,
		JoinType: JoinTypeRequest, Rules: &rules,
	})
	if err != nil {
		t.Fatal(err)
	}
	if item.ID != 10 || store.eventID != 3 || store.ownerID != 7 {
		t.Fatalf("unexpected delegation: item=%#v event=%d owner=%d", item, store.eventID, store.ownerID)
	}
	if store.created.Name != "Команда" || *store.created.Description != "Любим гулять" || *store.created.Rules != "Без опозданий" {
		t.Fatalf("input was not normalized: %#v", store.created)
	}
}

func TestCreateValidatesAllCompanyFields(t *testing.T) {
	tooLong := string(make([]rune, 2001))
	service := NewService(&storeStub{})
	_, err := service.Create(context.Background(), 3, 7, CreateInput{
		Name: "", Description: &tooLong, MaxMembers: 1, JoinType: "invite", Rules: &tooLong,
	})
	validation, ok := err.(*ValidationError)
	if !ok {
		t.Fatalf("expected validation error, got %v", err)
	}
	for _, field := range []string{"name", "description", "maxMembers", "joinType", "rules"} {
		if validation.Fields[field] == nil {
			t.Fatalf("missing validation for %s: %#v", field, validation.Fields)
		}
	}
}

func TestUpdateValidatesPatch(t *testing.T) {
	service := NewService(&storeStub{})
	_, err := service.Update(context.Background(), 10, 7, Patch{
		Name:       Change[string]{Set: true, Value: "  "},
		MaxMembers: Change[int]{Set: true, Value: 101},
		JoinType:   Change[string]{Set: true, Value: "private"},
	})
	validation, ok := err.(*ValidationError)
	if !ok || validation.Fields["name"] == nil || validation.Fields["maxMembers"] == nil || validation.Fields["joinType"] == nil {
		t.Fatalf("unexpected validation: %#v", err)
	}
}
