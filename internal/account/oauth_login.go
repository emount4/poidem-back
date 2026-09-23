package account

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

type ExternalIdentity struct {
	Provider       string
	ProviderUserID string
	FirstName      string
	LastName       string
	AvatarURL      string
}

type OAuthLoginStore interface {
	FindOrCreateOAuthUser(context.Context, ExternalIdentity) (int64, error)
	CreateSession(context.Context, int64, []byte, time.Time) (Session, error)
}

type TransactionManager interface {
	WithinTransaction(context.Context, func(context.Context) error) error
}

type OAuthLoginService struct {
	store      OAuthLoginStore
	tx         TransactionManager
	refreshTTL time.Duration
	now        func() time.Time
}

func NewOAuthLoginService(store OAuthLoginStore, tx TransactionManager, refreshTTL time.Duration) (*OAuthLoginService, error) {
	if store == nil || tx == nil {
		return nil, errors.New("OAuth login dependencies are required")
	}
	if refreshTTL <= 0 {
		return nil, errors.New("refresh token TTL must be positive")
	}
	return &OAuthLoginService{store: store, tx: tx, refreshTTL: refreshTTL, now: time.Now}, nil
}

func (s *OAuthLoginService) Login(ctx context.Context, identity ExternalIdentity) (string, error) {
	identity.Provider = strings.TrimSpace(identity.Provider)
	identity.ProviderUserID = strings.TrimSpace(identity.ProviderUserID)
	identity.FirstName = strings.TrimSpace(identity.FirstName)
	identity.LastName = strings.TrimSpace(identity.LastName)
	identity.AvatarURL = strings.TrimSpace(identity.AvatarURL)
	if identity.Provider == "" || identity.ProviderUserID == "" {
		return "", errors.New("OAuth provider and user ID are required")
	}
	refresh, tokenHash, err := newRefreshToken()
	if err != nil {
		return "", fmt.Errorf("generate initial refresh token: %w", err)
	}
	expiresAt := s.now().Add(s.refreshTTL)
	err = s.tx.WithinTransaction(ctx, func(txCtx context.Context) error {
		userID, err := s.store.FindOrCreateOAuthUser(txCtx, identity)
		if err != nil {
			return fmt.Errorf("resolve OAuth user: %w", err)
		}
		if _, err := s.store.CreateSession(txCtx, userID, tokenHash, expiresAt); err != nil {
			return fmt.Errorf("create initial refresh session: %w", err)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return refresh, nil
}
