// Package account contains authentication and profile-access rules.
package account

import (
	"strings"
	"time"
)

const (
	RoleUser  = "user"
	RoleAdmin = "admin"
)

const (
	StatusActive = "active"
	StatusBanned = "banned"
)

type AccessClaims struct {
	UserID    int64
	SessionID int64
}

type AccessState struct {
	UserID    int64
	FirstName string
	CityID    *int64
	Role      string
	Status    string
}

func (s AccessState) ProfileComplete() bool {
	return strings.TrimSpace(s.FirstName) != "" && s.CityID != nil
}

type Principal struct {
	UserID          int64
	SessionID       int64
	Role            string
	ProfileComplete bool
}

type Session struct {
	ID        int64
	UserID    int64
	ExpiresAt time.Time
	RevokedAt *time.Time
}
