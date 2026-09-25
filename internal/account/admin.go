package account

import (
	"context"
	"errors"
	"strings"
	"time"
)

var (
	ErrCannotChangeOwnStatus = errors.New("admin cannot change own status")
	ErrCannotBanAdmin        = errors.New("admin user cannot be banned")
)

type AdminPage struct {
	Offset int64
	Limit  int64
}

type AdminDashboard struct {
	UsersTotal          int64
	ActiveEvents        int64
	PendingReports      int64
	NewRegistrations30d int64
}

type AdminStore interface {
	ListUsers(context.Context, string, AdminPage) ([]Profile, int64, error)
	Profile(context.Context, int64) (Profile, error)
	SetUserStatus(context.Context, int64, string, time.Time) (Profile, error)
	Dashboard(context.Context, time.Time) (AdminDashboard, error)
}

type AdminService struct {
	store AdminStore
	now   func() time.Time
}

func NewAdminService(store AdminStore) *AdminService {
	return &AdminService{store: store, now: time.Now}
}

func (s *AdminService) ListUsers(ctx context.Context, search string, page AdminPage) ([]Profile, int64, error) {
	return s.store.ListUsers(ctx, strings.TrimSpace(search), page)
}

func (s *AdminService) GetUser(ctx context.Context, userID int64) (Profile, error) {
	return s.store.Profile(ctx, userID)
}

func (s *AdminService) SetUserStatus(ctx context.Context, actorID, userID int64, status string) (Profile, error) {
	if actorID == userID {
		return Profile{}, ErrCannotChangeOwnStatus
	}
	if status != StatusActive && status != StatusBanned {
		return Profile{}, ErrUserNotFound
	}
	return s.store.SetUserStatus(ctx, userID, status, s.now())
}

func (s *AdminService) Dashboard(ctx context.Context) (AdminDashboard, error) {
	return s.store.Dashboard(ctx, s.now())
}
