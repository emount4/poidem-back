package companies

import (
	"context"
	"testing"
	"time"
)

type storeStub struct {
	created          CreateInput
	eventID          int64
	ownerID          int64
	joinedCompanyID  int64
	joinedUserID     int64
	leftCompanyID    int64
	leftUserID       int64
	removedCompanyID int64
	removingOwnerID  int64
	removedUserID    int64
	applicationInput CreateApplicationInput
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
func (*storeStub) ListAdmin(context.Context, Page) ([]Company, int64, error) {
	return nil, 0, nil
}
func (*storeStub) Block(context.Context, int64, time.Time) (Company, error) {
	return Company{Status: StatusBlocked}, nil
}
func (s *storeStub) Update(_ context.Context, _, _ int64, patch Patch, _ time.Time) (Company, error) {
	return Company{ID: 10, Name: patch.Name.Value}, nil
}
func (*storeStub) SetRecruitment(context.Context, int64, int64, string, time.Time) (Company, error) {
	return Company{}, nil
}
func (*storeStub) Delete(context.Context, int64, int64, time.Time) error { return nil }
func (s *storeStub) JoinOpen(_ context.Context, companyID, userID int64, _ time.Time) error {
	s.joinedCompanyID, s.joinedUserID = companyID, userID
	return nil
}
func (s *storeStub) Leave(_ context.Context, companyID, userID int64) error {
	s.leftCompanyID, s.leftUserID = companyID, userID
	return nil
}
func (s *storeStub) RemoveMember(_ context.Context, companyID, ownerID, userID int64) error {
	s.removedCompanyID, s.removingOwnerID, s.removedUserID = companyID, ownerID, userID
	return nil
}
func (s *storeStub) CreateApplication(_ context.Context, _, _ int64, input CreateApplicationInput, _ time.Time) (Application, error) {
	s.applicationInput = input
	return Application{ID: 20, Message: input.Message}, nil
}
func (*storeStub) GetMyApplication(context.Context, int64, int64) (Application, error) {
	return Application{ID: 20}, nil
}
func (*storeStub) ListApplications(context.Context, int64, int64, string, Page) ([]Application, int64, error) {
	return nil, 0, nil
}
func (*storeStub) ListMyApplications(context.Context, int64, Page) ([]Application, int64, error) {
	return nil, 0, nil
}
func (*storeStub) CancelApplication(context.Context, int64, int64, time.Time) error { return nil }
func (*storeStub) ResolveApplication(context.Context, int64, int64, int64, string, time.Time) (Application, error) {
	return Application{ID: 20}, nil
}

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
	minAge, maxAge := 50, 20
	service := NewService(&storeStub{})
	_, err := service.Create(context.Background(), 3, 7, CreateInput{
		Name: "", Description: &tooLong, MaxMembers: 1, JoinType: "invite", Rules: &tooLong,
		MinAge: &minAge, MaxAge: &maxAge,
	})
	validation, ok := err.(*ValidationError)
	if !ok {
		t.Fatalf("expected validation error, got %v", err)
	}
	for _, field := range []string{"name", "description", "maxMembers", "joinType", "rules", "maxAge"} {
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
		MinAge:     NullableChange[int]{Set: true, Value: 13},
		MaxAge:     NullableChange[int]{Set: true, Value: 101},
	})
	validation, ok := err.(*ValidationError)
	if !ok || validation.Fields["name"] == nil || validation.Fields["maxMembers"] == nil || validation.Fields["joinType"] == nil || validation.Fields["minAge"] == nil || validation.Fields["maxAge"] == nil {
		t.Fatalf("unexpected validation: %#v", err)
	}
}

func TestJoinOpenDelegatesToStore(t *testing.T) {
	store := &storeStub{}
	service := NewService(store)
	if err := service.JoinOpen(context.Background(), 10, 7); err != nil {
		t.Fatal(err)
	}
	if store.joinedCompanyID != 10 || store.joinedUserID != 7 {
		t.Fatalf("unexpected join: company=%d user=%d", store.joinedCompanyID, store.joinedUserID)
	}
}

func TestLeaveDelegatesToStore(t *testing.T) {
	store := &storeStub{}
	service := NewService(store)
	if err := service.Leave(context.Background(), 10, 7); err != nil {
		t.Fatal(err)
	}
	if store.leftCompanyID != 10 || store.leftUserID != 7 {
		t.Fatalf("unexpected leave: company=%d user=%d", store.leftCompanyID, store.leftUserID)
	}
}

func TestRemoveMemberDelegatesToStore(t *testing.T) {
	store := &storeStub{}
	service := NewService(store)
	if err := service.RemoveMember(context.Background(), 10, 7, 9); err != nil {
		t.Fatal(err)
	}
	if store.removedCompanyID != 10 || store.removingOwnerID != 7 || store.removedUserID != 9 {
		t.Fatalf("unexpected removal: company=%d owner=%d user=%d", store.removedCompanyID, store.removingOwnerID, store.removedUserID)
	}
}

func TestCreateApplicationNormalizesAndValidatesMessage(t *testing.T) {
	store := &storeStub{}
	service := NewService(store)
	message := "  Возьмите меня  "
	item, err := service.CreateApplication(context.Background(), 10, 7, CreateApplicationInput{Message: &message})
	if err != nil || item.ID != 20 || store.applicationInput.Message == nil || *store.applicationInput.Message != "Возьмите меня" {
		t.Fatalf("item=%#v input=%#v err=%v", item, store.applicationInput, err)
	}
	tooLong := string(make([]rune, 1001))
	_, err = service.CreateApplication(context.Background(), 10, 7, CreateApplicationInput{Message: &tooLong})
	validation, ok := err.(*ValidationError)
	if !ok || validation.Fields["message"] == nil {
		t.Fatalf("unexpected validation: %#v", err)
	}
}
