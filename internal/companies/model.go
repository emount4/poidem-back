// Package companies contains company membership and recruitment business rules.
package companies

import "time"

const (
	StatusActive  = "active"
	StatusClosed  = "closed"
	StatusBlocked = "blocked"
)

const (
	JoinTypeOpen    = "open"
	JoinTypeRequest = "request"
)

const (
	RoleOwner  = "owner"
	RoleMember = "member"
)

const ApplicationStatusPending = "pending"

type UserShort struct {
	ID        int64
	FirstName string
	LastName  *string
	AvatarURL *string
}

type Company struct {
	ID           int64
	EventID      int64
	Name         string
	Description  *string
	MaxMembers   int
	JoinType     string
	Rules        *string
	Owner        UserShort
	MembersCount int64
	Status       string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type CreateInput struct {
	Name        string
	Description *string
	MaxMembers  int
	JoinType    string
	Rules       *string
}

type Application struct {
	ID               int64
	CompanyID        int64
	User             UserShort
	Message          *string
	Status           string
	ResolutionReason *string
	CreatedAt        time.Time
	ResolvedAt       *time.Time
}

type CreateApplicationInput struct {
	Message *string
}

type Change[T any] struct {
	Set   bool
	Value T
}

type NullableChange[T any] struct {
	Set   bool
	Null  bool
	Value T
}

type Patch struct {
	Name        Change[string]
	Description NullableChange[string]
	MaxMembers  Change[int]
	JoinType    Change[string]
	Rules       NullableChange[string]
}

type Viewer struct {
	UserID *int64
	Admin  bool
}

type Page struct {
	Offset int64
	Limit  int64
}
