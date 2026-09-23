package account

import (
	"context"
	"errors"
	"fmt"
)

var (
	ErrUnauthorized    = errors.New("unauthorized")
	ErrUserBanned      = errors.New("user banned")
	ErrUserNotFound    = errors.New("user not found")
	ErrSessionNotFound = errors.New("session not found")
)

type AccessTokenVerifier interface {
	VerifyAccessToken(token string) (AccessClaims, error)
}

type Repository interface {
	AccessState(ctx context.Context, userID int64) (AccessState, error)
}

type Service struct {
	tokens     AccessTokenVerifier
	repository Repository
}

func NewService(tokens AccessTokenVerifier, repository Repository) *Service {
	return &Service{tokens: tokens, repository: repository}
}

func (s *Service) Authenticate(ctx context.Context, token string) (Principal, error) {
	claims, err := s.tokens.VerifyAccessToken(token)
	if err != nil || claims.UserID <= 0 || claims.SessionID <= 0 {
		return Principal{}, ErrUnauthorized
	}
	state, err := s.repository.AccessState(ctx, claims.UserID)
	if errors.Is(err, ErrUserNotFound) {
		return Principal{}, ErrUnauthorized
	}
	if err != nil {
		return Principal{}, fmt.Errorf("load access state: %w", err)
	}
	if state.Status == StatusBanned {
		return Principal{}, ErrUserBanned
	}
	if state.Status != StatusActive || state.UserID != claims.UserID {
		return Principal{}, ErrUnauthorized
	}
	return Principal{
		UserID:          state.UserID,
		SessionID:       claims.SessionID,
		Role:            state.Role,
		ProfileComplete: state.ProfileComplete(),
	}, nil
}
