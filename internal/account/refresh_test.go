package account

import (
	"context"
	"errors"
	"testing"
	"time"
)

type sessionStoreStub struct {
	session         Session
	err             error
	presented, next []byte
	revoked         []byte
}

func (s *sessionStoreStub) RotateSession(_ context.Context, presented, next []byte, _, _ time.Time) (Session, error) {
	s.presented, s.next = presented, next
	return s.session, s.err
}
func (s *sessionStoreStub) RevokeSession(_ context.Context, hash []byte, _ time.Time) error {
	s.revoked = hash
	return s.err
}

type issuerStub struct {
	token             string
	userID, sessionID int64
}

func (s *issuerStub) Issue(userID, sessionID int64) (string, error) {
	s.userID, s.sessionID = userID, sessionID
	return s.token, nil
}

func TestRefreshAndLogout(t *testing.T) {
	store := &sessionStoreStub{session: Session{ID: 9, UserID: 4}}
	issuer := &issuerStub{token: "access"}
	service, err := NewRefreshService(store, issuer, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	pair, err := service.Refresh(context.Background(), "old-refresh")
	if err != nil {
		t.Fatal(err)
	}
	if pair.AccessToken != "access" || pair.RefreshToken == "" || pair.RefreshToken == "old-refresh" {
		t.Fatalf("unexpected pair: %+v", pair)
	}
	if len(store.presented) != 32 || len(store.next) != 32 || issuer.userID != 4 || issuer.sessionID != 9 {
		t.Fatal("refresh inputs are incorrect")
	}
	if err := service.Logout(context.Background(), pair.RefreshToken); err != nil {
		t.Fatal(err)
	}
	if len(store.revoked) != 32 {
		t.Fatal("logout must hash refresh token")
	}
}

func TestRefreshRejectsUnknownSession(t *testing.T) {
	service, _ := NewRefreshService(&sessionStoreStub{err: ErrSessionNotFound}, &issuerStub{}, time.Second)
	_, err := service.Refresh(context.Background(), "unknown")
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("error = %v", err)
	}
}
