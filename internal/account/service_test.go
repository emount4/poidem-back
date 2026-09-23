package account

import (
	"context"
	"errors"
	"testing"
)

type tokenVerifierStub struct {
	claims AccessClaims
	err    error
}

func (s tokenVerifierStub) VerifyAccessToken(string) (AccessClaims, error) {
	return s.claims, s.err
}

type repositoryStub struct {
	state AccessState
	err   error
}

func (s repositoryStub) AccessState(context.Context, int64) (AccessState, error) {
	return s.state, s.err
}

func TestAuthenticate(t *testing.T) {
	cityID := int64(1)
	tests := []struct {
		name      string
		claims    AccessClaims
		state     AccessState
		tokenErr  error
		repoErr   error
		want      Principal
		wantError error
	}{
		{
			name: "complete profile", claims: AccessClaims{UserID: 7, SessionID: 11},
			state: AccessState{UserID: 7, FirstName: "Анна", CityID: &cityID, Role: RoleUser, Status: StatusActive},
			want:  Principal{UserID: 7, SessionID: 11, Role: RoleUser, ProfileComplete: true},
		},
		{
			name: "incomplete profile", claims: AccessClaims{UserID: 7, SessionID: 11},
			state: AccessState{UserID: 7, FirstName: " ", Role: RoleUser, Status: StatusActive},
			want:  Principal{UserID: 7, SessionID: 11, Role: RoleUser, ProfileComplete: false},
		},
		{name: "invalid token", tokenErr: errors.New("bad signature"), wantError: ErrUnauthorized},
		{name: "invalid claims", claims: AccessClaims{UserID: 7}, wantError: ErrUnauthorized},
		{name: "missing user", claims: AccessClaims{UserID: 7, SessionID: 11}, repoErr: ErrUserNotFound, wantError: ErrUnauthorized},
		{
			name: "banned", claims: AccessClaims{UserID: 7, SessionID: 11},
			state: AccessState{UserID: 7, Status: StatusBanned}, wantError: ErrUserBanned,
		},
		{
			name: "unknown status", claims: AccessClaims{UserID: 7, SessionID: 11},
			state: AccessState{UserID: 7, Status: "unknown"}, wantError: ErrUnauthorized,
		},
		{
			name: "database failure", claims: AccessClaims{UserID: 7, SessionID: 11},
			repoErr: errors.New("connection lost"), wantError: errors.New("connection lost"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewService(
				tokenVerifierStub{claims: tt.claims, err: tt.tokenErr},
				repositoryStub{state: tt.state, err: tt.repoErr},
			)
			got, err := service.Authenticate(context.Background(), "token")
			if tt.wantError != nil {
				if tt.name == "database failure" {
					if err == nil || !errors.Is(err, tt.repoErr) {
						t.Fatalf("error = %v, want wrapped repository error", err)
					}
				} else if !errors.Is(err, tt.wantError) {
					t.Fatalf("error = %v, want %v", err, tt.wantError)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("Authenticate() = %+v, %v; want %+v", got, err, tt.want)
			}
		})
	}
}

func TestProfileComplete(t *testing.T) {
	cityID := int64(1)
	for _, tt := range []struct {
		name      string
		firstName string
		cityID    *int64
		want      bool
	}{
		{name: "complete", firstName: "Иван", cityID: &cityID, want: true},
		{name: "missing city", firstName: "Иван"},
		{name: "empty name", firstName: "", cityID: &cityID},
		{name: "blank name", firstName: "  ", cityID: &cityID},
	} {
		t.Run(tt.name, func(t *testing.T) {
			state := AccessState{FirstName: tt.firstName, CityID: tt.cityID}
			if got := state.ProfileComplete(); got != tt.want {
				t.Fatalf("ProfileComplete() = %v, want %v", got, tt.want)
			}
		})
	}
}
