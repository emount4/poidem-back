package account

import (
	"context"
	"errors"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type credentialStoreStub struct {
	createdUsername string
	createdHash     string
	createErr       error
	user            CredentialUser
	session         Session
	profile         Profile
}

func (s *credentialStoreStub) CreateCredentialUser(_ context.Context, username, passwordHash string) (int64, error) {
	s.createdUsername, s.createdHash = username, passwordHash
	if s.createErr != nil {
		return 0, s.createErr
	}
	return s.user.ID, nil
}
func (s *credentialStoreStub) CredentialUser(context.Context, string) (CredentialUser, error) {
	if s.createErr != nil {
		return CredentialUser{}, s.createErr
	}
	return s.user, nil
}
func (s *credentialStoreStub) CreateSession(_ context.Context, userID int64, _ []byte, _ time.Time) (Session, error) {
	s.session.UserID = userID
	return s.session, nil
}
func (s *credentialStoreStub) Profile(context.Context, int64) (Profile, error) {
	return s.profile, nil
}

func TestCredentialRegistrationHashesPasswordAndNormalizesUsername(t *testing.T) {
	store := &credentialStoreStub{user: CredentialUser{ID: 7}, session: Session{ID: 9}, profile: Profile{ID: 7, Interests: []DictionaryItem{}}}
	issuer := &issuerStub{token: "access"}
	service, err := NewCredentialService(store, &txStub{}, issuer, 30*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Register(context.Background(), "  DaNiLa  ", "password123")
	if err != nil {
		t.Fatal(err)
	}
	if store.createdUsername != "danila" || store.createdHash == "password123" {
		t.Fatalf("credentials were not normalized/hashed: %q %q", store.createdUsername, store.createdHash)
	}
	if bcrypt.CompareHashAndPassword([]byte(store.createdHash), []byte("password123")) != nil {
		t.Fatal("stored password hash does not match")
	}
	if result.AccessToken != "access" || result.RefreshToken == "" || issuer.userID != 7 || issuer.sessionID != 9 {
		t.Fatalf("unexpected auth result: %+v", result)
	}
}

func TestCredentialRegistrationValidationAndDuplicate(t *testing.T) {
	service, _ := NewCredentialService(&credentialStoreStub{}, &txStub{}, &issuerStub{}, time.Hour)
	if _, err := service.Register(context.Background(), "ab", "short"); err == nil {
		t.Fatal("invalid credentials accepted")
	}
	store := &credentialStoreStub{user: CredentialUser{ID: 1}, createErr: ErrUsernameTaken}
	service, _ = NewCredentialService(store, &txStub{}, &issuerStub{}, time.Hour)
	if _, err := service.Register(context.Background(), "username", "password123"); !errors.Is(err, ErrUsernameTaken) {
		t.Fatalf("duplicate error=%v", err)
	}
}

func TestCredentialLoginAndBan(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.MinCost)
	store := &credentialStoreStub{
		user:    CredentialUser{ID: 4, PasswordHash: string(hash), Status: StatusActive},
		session: Session{ID: 6}, profile: Profile{ID: 4, Interests: []DictionaryItem{}},
	}
	service, _ := NewCredentialService(store, &txStub{}, &issuerStub{token: "access"}, time.Hour)
	if result, err := service.Login(context.Background(), "USER", "password123"); err != nil || result.AccessToken != "access" {
		t.Fatalf("login result=%+v err=%v", result, err)
	}
	if _, err := service.Login(context.Background(), "USER", "wrongpass"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("wrong password error=%v", err)
	}
	store.user.Status = StatusBanned
	if _, err := service.Login(context.Background(), "USER", "password123"); !errors.Is(err, ErrUserBanned) {
		t.Fatalf("banned login error=%v", err)
	}
}
