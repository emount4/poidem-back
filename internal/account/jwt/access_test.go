package jwt

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/emount4/poidem-back/internal/account"
)

func TestAccessTokenRoundTrip(t *testing.T) {
	clock := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	tokens, err := NewAccessTokens(strings.Repeat("s", 32), "poydem", "poydem-api", 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	tokens.now = func() time.Time { return clock }
	raw, err := tokens.Issue(42, 7)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := tokens.VerifyAccessToken(raw)
	if err != nil || claims != (account.AccessClaims{UserID: 42, SessionID: 7}) {
		t.Fatalf("claims = %+v, error = %v", claims, err)
	}

	tokens.now = func() time.Time { return clock.Add(16 * time.Minute) }
	if _, err := tokens.VerifyAccessToken(raw); !errors.Is(err, account.ErrUnauthorized) {
		t.Fatalf("expired token error = %v", err)
	}
}

func TestAccessTokenRejectsWrongConfiguration(t *testing.T) {
	if _, err := NewAccessTokens("short", "poydem", "poydem-api", time.Minute); err == nil {
		t.Fatal("short secret must be rejected")
	}
	first, _ := NewAccessTokens(strings.Repeat("a", 32), "poydem", "poydem-api", time.Minute)
	second, _ := NewAccessTokens(strings.Repeat("b", 32), "poydem", "poydem-api", time.Minute)
	raw, _ := first.Issue(1, 1)
	if _, err := second.VerifyAccessToken(raw); !errors.Is(err, account.ErrUnauthorized) {
		t.Fatalf("wrong signature error = %v", err)
	}
}
