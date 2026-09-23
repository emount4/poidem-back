package oauth

import (
	"errors"
	"testing"
	"time"
)

func TestFlowRoundTripAndPKCE(t *testing.T) {
	flow, err := NewFlow("12345678901234567890123456789012", 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	authorization, err := flow.Start()
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := flow.Verify(authorization.CookieValue, authorization.State)
	if err != nil {
		t.Fatal(err)
	}
	if verifier != authorization.CodeVerifier || authorization.CodeChallenge == "" || authorization.CodeChallenge == verifier {
		t.Fatal("invalid OAuth state or PKCE values")
	}
	if _, err := flow.Verify(authorization.CookieValue+"x", authorization.State); !errors.Is(err, ErrInvalidFlow) {
		t.Fatalf("tampered cookie error = %v", err)
	}
	if _, err := flow.Verify(authorization.CookieValue, "other-state"); !errors.Is(err, ErrInvalidFlow) {
		t.Fatalf("mismatched state error = %v", err)
	}
}

func TestFlowExpires(t *testing.T) {
	flow, _ := NewFlow("12345678901234567890123456789012", time.Minute)
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	flow.now = func() time.Time { return now }
	authorization, err := flow.Start()
	if err != nil {
		t.Fatal(err)
	}
	flow.now = func() time.Time { return now.Add(2 * time.Minute) }
	if _, err := flow.Verify(authorization.CookieValue, authorization.State); !errors.Is(err, ErrInvalidFlow) {
		t.Fatalf("expired flow error = %v", err)
	}
}
