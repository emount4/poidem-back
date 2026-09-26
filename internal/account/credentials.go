package account

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

var (
	ErrUsernameTaken      = errors.New("username is already taken")
	ErrInvalidCredentials = errors.New("invalid credentials")
)

type CredentialUser struct {
	ID           int64
	PasswordHash string
	Status       string
}

type AuthResult struct {
	AccessToken  string
	RefreshToken string
	User         Profile
}

type CredentialStore interface {
	CreateCredentialUser(context.Context, string, string) (int64, error)
	CredentialUser(context.Context, string) (CredentialUser, error)
	CreateSession(context.Context, int64, []byte, time.Time) (Session, error)
	Profile(context.Context, int64) (Profile, error)
}

type CredentialService struct {
	store      CredentialStore
	tx         TransactionManager
	access     AccessTokenIssuer
	refreshTTL time.Duration
	dummyHash  []byte
	now        func() time.Time
}

func NewCredentialService(store CredentialStore, tx TransactionManager, access AccessTokenIssuer, refreshTTL time.Duration) (*CredentialService, error) {
	if store == nil || tx == nil || access == nil {
		return nil, errors.New("credential dependencies are required")
	}
	if refreshTTL <= 0 {
		return nil, errors.New("refresh token TTL must be positive")
	}
	dummyHash, err := bcrypt.GenerateFromPassword([]byte("invalid-credential-timing-placeholder"), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("prepare credential timing hash: %w", err)
	}
	return &CredentialService{store: store, tx: tx, access: access, refreshTTL: refreshTTL, dummyHash: dummyHash, now: time.Now}, nil
}

func (s *CredentialService) Register(ctx context.Context, username, password string) (AuthResult, error) {
	username = normalizeUsername(username)
	if fields := validateRegistration(username, password); len(fields) > 0 {
		return AuthResult{}, &ValidationError{Fields: fields}
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return AuthResult{}, fmt.Errorf("hash password: %w", err)
	}
	refresh, refreshHash, err := newRefreshToken()
	if err != nil {
		return AuthResult{}, fmt.Errorf("generate registration refresh token: %w", err)
	}
	var session Session
	var profile Profile
	err = s.tx.WithinTransaction(ctx, func(txCtx context.Context) error {
		userID, err := s.store.CreateCredentialUser(txCtx, username, string(passwordHash))
		if err != nil {
			return err
		}
		session, err = s.store.CreateSession(txCtx, userID, refreshHash, s.now().Add(s.refreshTTL))
		if err != nil {
			return fmt.Errorf("create registration session: %w", err)
		}
		profile, err = s.store.Profile(txCtx, userID)
		return err
	})
	if err != nil {
		return AuthResult{}, err
	}
	access, err := s.access.Issue(session.UserID, session.ID)
	if err != nil {
		return AuthResult{}, fmt.Errorf("issue registration access token: %w", err)
	}
	return AuthResult{AccessToken: access, RefreshToken: refresh, User: profile}, nil
}

func (s *CredentialService) Login(ctx context.Context, username, password string) (AuthResult, error) {
	username = normalizeUsername(username)
	if !validLoginShape(username, password) {
		return AuthResult{}, ErrInvalidCredentials
	}
	user, err := s.store.CredentialUser(ctx, username)
	if errors.Is(err, ErrUserNotFound) {
		_ = bcrypt.CompareHashAndPassword(s.dummyHash, []byte(password))
		return AuthResult{}, ErrInvalidCredentials
	}
	if err != nil {
		return AuthResult{}, fmt.Errorf("load credential user: %w", err)
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		return AuthResult{}, ErrInvalidCredentials
	}
	refresh, refreshHash, err := newRefreshToken()
	if err != nil {
		return AuthResult{}, fmt.Errorf("generate login refresh token: %w", err)
	}
	var session Session
	var profile Profile
	err = s.tx.WithinTransaction(ctx, func(txCtx context.Context) error {
		locked, err := s.store.CredentialUser(txCtx, username)
		if errors.Is(err, ErrUserNotFound) {
			return ErrInvalidCredentials
		}
		if err != nil {
			return err
		}
		if locked.ID != user.ID || locked.PasswordHash != user.PasswordHash {
			return ErrInvalidCredentials
		}
		if locked.Status == StatusBanned {
			return ErrUserBanned
		}
		if locked.Status != StatusActive {
			return ErrInvalidCredentials
		}
		session, err = s.store.CreateSession(txCtx, locked.ID, refreshHash, s.now().Add(s.refreshTTL))
		if err != nil {
			return fmt.Errorf("create login session: %w", err)
		}
		profile, err = s.store.Profile(txCtx, locked.ID)
		return err
	})
	if err != nil {
		return AuthResult{}, err
	}
	access, err := s.access.Issue(session.UserID, session.ID)
	if err != nil {
		return AuthResult{}, fmt.Errorf("issue login access token: %w", err)
	}
	return AuthResult{AccessToken: access, RefreshToken: refresh, User: profile}, nil
}

func normalizeUsername(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func validateRegistration(username, password string) map[string][]string {
	fields := make(map[string][]string)
	length := utf8.RuneCountInString(username)
	if length < 3 || length > 64 {
		fields["username"] = []string{"Длина должна быть от 3 до 64 символов"}
	} else if strings.IndexFunc(username, unicode.IsSpace) >= 0 {
		fields["username"] = []string{"Логин не должен содержать пробелы"}
	}
	if utf8.RuneCountInString(password) < 8 {
		fields["password"] = []string{"Пароль должен содержать не менее 8 символов"}
	} else if len([]byte(password)) > 72 {
		fields["password"] = []string{"Пароль не должен превышать 72 байта"}
	}
	return fields
}

func validLoginShape(username, password string) bool {
	return len(validateRegistration(username, password)) == 0
}
