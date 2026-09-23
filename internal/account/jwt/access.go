// Package jwt issues and verifies application access tokens.
package jwt

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/emount4/poidem-back/internal/account"
	jwtlib "github.com/golang-jwt/jwt/v5"
)

const minimumSecretLength = 32

type AccessTokens struct {
	secret   []byte
	issuer   string
	audience string
	ttl      time.Duration
	leeway   time.Duration
	now      func() time.Time
}

type accessClaims struct {
	SessionID int64 `json:"sid"`
	jwtlib.RegisteredClaims
}

func NewAccessTokens(secret, issuer, audience string, ttl time.Duration) (*AccessTokens, error) {
	if len(secret) < minimumSecretLength {
		return nil, fmt.Errorf("JWT secret must contain at least %d bytes", minimumSecretLength)
	}
	if issuer == "" || audience == "" {
		return nil, errors.New("JWT issuer and audience are required")
	}
	if ttl <= 0 {
		return nil, errors.New("JWT access TTL must be positive")
	}
	return &AccessTokens{
		secret: []byte(secret), issuer: issuer, audience: audience, ttl: ttl,
		leeway: 30 * time.Second, now: time.Now,
	}, nil
}

func (a *AccessTokens) Issue(userID, sessionID int64) (string, error) {
	if userID <= 0 || sessionID <= 0 {
		return "", errors.New("user and session IDs must be positive")
	}
	now := a.now()
	claims := accessClaims{
		SessionID: sessionID,
		RegisteredClaims: jwtlib.RegisteredClaims{
			Issuer: a.issuer, Subject: strconv.FormatInt(userID, 10),
			Audience: jwtlib.ClaimStrings{a.audience},
			IssuedAt: jwtlib.NewNumericDate(now), ExpiresAt: jwtlib.NewNumericDate(now.Add(a.ttl)),
		},
	}
	return jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, claims).SignedString(a.secret)
}

func (a *AccessTokens) VerifyAccessToken(raw string) (account.AccessClaims, error) {
	claims := &accessClaims{}
	token, err := jwtlib.ParseWithClaims(raw, claims, func(token *jwtlib.Token) (any, error) {
		return a.secret, nil
	},
		jwtlib.WithValidMethods([]string{jwtlib.SigningMethodHS256.Alg()}),
		jwtlib.WithIssuer(a.issuer),
		jwtlib.WithAudience(a.audience),
		jwtlib.WithExpirationRequired(),
		jwtlib.WithIssuedAt(),
		jwtlib.WithLeeway(a.leeway),
		jwtlib.WithTimeFunc(a.now),
	)
	if err != nil || !token.Valid {
		return account.AccessClaims{}, account.ErrUnauthorized
	}
	userID, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil || userID <= 0 || claims.SessionID <= 0 {
		return account.AccessClaims{}, account.ErrUnauthorized
	}
	return account.AccessClaims{UserID: userID, SessionID: claims.SessionID}, nil
}
