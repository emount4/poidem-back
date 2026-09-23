// Package oauth contains provider-independent OAuth flow primitives.
package oauth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var ErrInvalidFlow = errors.New("invalid OAuth flow")

type Authorization struct {
	State         string
	CodeVerifier  string
	CodeChallenge string
	CookieValue   string
}

type flowPayload struct {
	State        string `json:"state"`
	CodeVerifier string `json:"codeVerifier"`
	ExpiresAt    int64  `json:"expiresAt"`
}

type Flow struct {
	secret []byte
	ttl    time.Duration
	now    func() time.Time
}

func NewFlow(secret string, ttl time.Duration) (*Flow, error) {
	if len(secret) < 32 {
		return nil, errors.New("OAuth state secret must contain at least 32 bytes")
	}
	if ttl <= 0 {
		return nil, errors.New("OAuth flow TTL must be positive")
	}
	return &Flow{secret: []byte(secret), ttl: ttl, now: time.Now}, nil
}

func (f *Flow) Start() (Authorization, error) {
	state, err := randomValue()
	if err != nil {
		return Authorization{}, err
	}
	verifier, err := randomValue()
	if err != nil {
		return Authorization{}, err
	}
	sum := sha256.Sum256([]byte(verifier))
	payload, err := json.Marshal(flowPayload{
		State: state, CodeVerifier: verifier, ExpiresAt: f.now().Add(f.ttl).Unix(),
	})
	if err != nil {
		return Authorization{}, err
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	return Authorization{
		State: state, CodeVerifier: verifier,
		CodeChallenge: base64.RawURLEncoding.EncodeToString(sum[:]),
		CookieValue:   encoded + "." + f.signature(encoded),
	}, nil
}

func (f *Flow) Verify(cookieValue, returnedState string) (string, error) {
	parts := strings.Split(cookieValue, ".")
	if len(parts) != 2 || !hmac.Equal([]byte(f.signature(parts[0])), []byte(parts[1])) {
		return "", ErrInvalidFlow
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", ErrInvalidFlow
	}
	var payload flowPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", ErrInvalidFlow
	}
	if payload.State == "" || payload.CodeVerifier == "" || returnedState == "" ||
		!hmac.Equal([]byte(payload.State), []byte(returnedState)) || f.now().Unix() > payload.ExpiresAt {
		return "", ErrInvalidFlow
	}
	return payload.CodeVerifier, nil
}

func (f *Flow) TTL() time.Duration { return f.ttl }

func (f *Flow) signature(value string) string {
	mac := hmac.New(sha256.New, f.secret)
	_, _ = mac.Write([]byte(value))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func randomValue() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
