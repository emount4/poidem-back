package account

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"
)

type AccessTokenIssuer interface {
	Issue(userID, sessionID int64) (string, error)
}

type SessionStore interface {
	RotateSession(context.Context, []byte, []byte, time.Time, time.Time) (Session, error)
	RevokeSession(context.Context, []byte, time.Time) error
}

type TokenPair struct {
	AccessToken  string
	RefreshToken string
}

type RefreshService struct {
	sessions SessionStore
	access   AccessTokenIssuer
	window   time.Duration
	now      func() time.Time
}

func NewRefreshService(sessions SessionStore, access AccessTokenIssuer, retryWindow time.Duration) (*RefreshService, error) {
	if retryWindow <= 0 {
		return nil, errors.New("refresh retry window must be positive")
	}
	return &RefreshService{sessions: sessions, access: access, window: retryWindow, now: time.Now}, nil
}

func (s *RefreshService) Refresh(ctx context.Context, current string) (TokenPair, error) {
	if current == "" {
		return TokenPair{}, ErrUnauthorized
	}
	next, nextHash, err := newRefreshToken()
	if err != nil {
		return TokenPair{}, fmt.Errorf("generate refresh token: %w", err)
	}
	now := s.now()
	session, err := s.sessions.RotateSession(ctx, hashToken(current), nextHash, now, now.Add(s.window))
	if errors.Is(err, ErrSessionNotFound) {
		return TokenPair{}, ErrUnauthorized
	}
	if err != nil {
		return TokenPair{}, fmt.Errorf("rotate refresh session: %w", err)
	}
	access, err := s.access.Issue(session.UserID, session.ID)
	if err != nil {
		return TokenPair{}, fmt.Errorf("issue access token: %w", err)
	}
	return TokenPair{AccessToken: access, RefreshToken: next}, nil
}

func (s *RefreshService) Logout(ctx context.Context, refresh string) error {
	if refresh == "" {
		return nil
	}
	if err := s.sessions.RevokeSession(ctx, hashToken(refresh), s.now()); err != nil {
		return fmt.Errorf("revoke refresh session: %w", err)
	}
	return nil
}

func newRefreshToken() (string, []byte, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	return token, hashToken(token), nil
}

func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}
