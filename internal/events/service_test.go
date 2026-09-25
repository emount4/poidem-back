package events

import (
	"context"
	"testing"
	"time"
)

type storeStub struct {
	created       CreateInput
	patched       Patch
	joinedEventID int64
	joinedUserID  int64
	cancelled     bool
}

func (s *storeStub) Create(_ context.Context, _ int64, input CreateInput) (Event, error) {
	s.created = input
	return Event{ID: 1, Title: input.Title}, nil
}
func (*storeStub) ListPublic(context.Context, PublicFilter, Page) ([]Event, int64, error) {
	return nil, 0, nil
}
func (*storeStub) GetVisible(context.Context, int64, *int64) (Event, error) { return Event{}, nil }
func (*storeStub) ListParticipants(context.Context, int64, *int64, Page) ([]UserShort, int64, error) {
	return nil, 0, nil
}
func (*storeStub) ListMine(context.Context, int64, MyFilter, Page) ([]MyEvent, int64, error) {
	return nil, 0, nil
}
func (*storeStub) ListAdmin(context.Context, AdminFilter, Page) ([]Event, int64, error) {
	return nil, 0, nil
}
func (*storeStub) GetAdmin(context.Context, int64) (Event, error) { return Event{}, nil }
func (s *storeStub) Update(_ context.Context, _ int64, patch Patch) (Event, error) {
	s.patched = patch
	return Event{ID: 1}, nil
}
func (*storeStub) Transition(context.Context, int64, string) (Event, error) { return Event{}, nil }
func (s *storeStub) JoinSolo(_ context.Context, eventID, userID int64, _ time.Time) error {
	s.joinedEventID, s.joinedUserID = eventID, userID
	return nil
}
func (s *storeStub) CancelSolo(context.Context, int64, int64) error {
	s.cancelled = true
	return nil
}

func TestCreateNormalizesAndValidates(t *testing.T) {
	store := &storeStub{}
	service := NewService(store)
	start := time.Now().Add(time.Hour)
	end := start.Add(time.Hour)
	description := "  Встреча  "
	item, err := service.Create(context.Background(), 7, CreateInput{
		Title: "  Прогулка  ", Description: &description, CategoryID: 2, CityID: 3,
		StartsAt: start, EndsAt: &end, LocationName: "  Парк  ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if item.ID != 1 || store.created.Title != "Прогулка" || *store.created.Description != "Встреча" || store.created.LocationName != "Парк" {
		t.Fatalf("unexpected normalized input: %#v", store.created)
	}

	_, err = service.Create(context.Background(), 7, CreateInput{Title: "", StartsAt: start, EndsAt: &start})
	validation, ok := err.(*ValidationError)
	if !ok || len(validation.Fields) < 4 || validation.Fields["endsAt"] == nil {
		t.Fatalf("unexpected validation error: %#v", err)
	}
}

func TestUpdateValidatesURL(t *testing.T) {
	store := &storeStub{}
	service := NewService(store)
	_, err := service.Update(context.Background(), 1, Patch{ImageURL: NullableChange[string]{Set: true, Value: "file:///tmp/a"}})
	validation, ok := err.(*ValidationError)
	if !ok || validation.Fields["imageUrl"] == nil {
		t.Fatalf("expected image URL validation, got %v", err)
	}
}

func TestTransitionRejectsUnknownAction(t *testing.T) {
	service := NewService(&storeStub{})
	if _, err := service.Transition(context.Background(), 1, "complete"); err != ErrInvalidStatusTransition {
		t.Fatalf("got %v", err)
	}
}

func TestSoloParticipationDelegatesToStore(t *testing.T) {
	store := &storeStub{}
	service := NewService(store)
	if err := service.JoinSolo(context.Background(), 3, 7); err != nil {
		t.Fatal(err)
	}
	if store.joinedEventID != 3 || store.joinedUserID != 7 {
		t.Fatalf("unexpected join: event=%d user=%d", store.joinedEventID, store.joinedUserID)
	}
	if err := service.CancelSolo(context.Background(), 3, 7); err != nil {
		t.Fatal(err)
	}
	if !store.cancelled {
		t.Fatal("cancel was not delegated")
	}
}
