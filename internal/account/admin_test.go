package account

import (
	"context"
	"testing"
	"time"
)

type adminStoreStub struct {
	status string
}

func (*adminStoreStub) ListUsers(context.Context, string, AdminPage) ([]Profile, int64, error) {
	return []Profile{}, 0, nil
}
func (*adminStoreStub) Profile(context.Context, int64) (Profile, error) { return Profile{ID: 2}, nil }
func (s *adminStoreStub) SetUserStatus(_ context.Context, id int64, status string, _ time.Time) (Profile, error) {
	s.status = status
	return Profile{ID: id, Status: status}, nil
}
func (*adminStoreStub) Dashboard(context.Context, time.Time) (AdminDashboard, error) {
	return AdminDashboard{UsersTotal: 3}, nil
}

func TestAdminCannotChangeOwnStatus(t *testing.T) {
	service := NewAdminService(&adminStoreStub{})
	if _, err := service.SetUserStatus(context.Background(), 5, 5, StatusBanned); err != ErrCannotChangeOwnStatus {
		t.Fatalf("got %v", err)
	}
}

func TestAdminStatusDelegatesToStore(t *testing.T) {
	store := &adminStoreStub{}
	service := NewAdminService(store)
	profile, err := service.SetUserStatus(context.Background(), 1, 2, StatusBanned)
	if err != nil || profile.Status != StatusBanned || store.status != StatusBanned {
		t.Fatalf("profile=%+v err=%v status=%s", profile, err, store.status)
	}
}
