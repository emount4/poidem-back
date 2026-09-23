package account

import (
	"context"
	"errors"
	"testing"
	"time"
)

type loginStoreStub struct {
	identity   ExternalIdentity
	userID     int64
	sessionErr error
	hash       []byte
	expiresAt  time.Time
}

func (s *loginStoreStub) FindOrCreateOAuthUser(_ context.Context, identity ExternalIdentity) (int64, error) {
	s.identity = identity
	return s.userID, nil
}
func (s *loginStoreStub) CreateSession(_ context.Context, userID int64, hash []byte, expiresAt time.Time) (Session, error) {
	s.userID, s.hash, s.expiresAt = userID, hash, expiresAt
	return Session{ID: 1, UserID: userID}, s.sessionErr
}

type txStub struct {
	committed bool
}

func (t *txStub) WithinTransaction(ctx context.Context, fn func(context.Context) error) error {
	err := fn(ctx)
	t.committed = err == nil
	return err
}

func TestOAuthLoginCreatesSessionInTransaction(t *testing.T) {
	store := &loginStoreStub{userID: 42}
	tx := &txStub{}
	service, err := NewOAuthLoginService(store, tx, 30*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	raw, err := service.Login(context.Background(), ExternalIdentity{
		Provider: " google ", ProviderUserID: " 123 ", FirstName: " Anna ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if raw == "" || !tx.committed || store.userID != 42 || len(store.hash) != 32 {
		t.Fatal("OAuth login did not create and commit the refresh session")
	}
	if store.identity.Provider != "google" || store.identity.ProviderUserID != "123" || store.identity.FirstName != "Anna" {
		t.Fatalf("identity was not normalized: %+v", store.identity)
	}
	if !store.expiresAt.Equal(now.Add(30 * 24 * time.Hour)) {
		t.Fatalf("expiresAt = %v", store.expiresAt)
	}
}

func TestOAuthLoginRollsBackWhenSessionFails(t *testing.T) {
	store := &loginStoreStub{userID: 42, sessionErr: errors.New("insert failed")}
	tx := &txStub{}
	service, _ := NewOAuthLoginService(store, tx, time.Hour)
	if _, err := service.Login(context.Background(), ExternalIdentity{Provider: "google", ProviderUserID: "123"}); err == nil {
		t.Fatal("expected login error")
	}
	if tx.committed {
		t.Fatal("failed session creation must roll back the transaction")
	}
}
