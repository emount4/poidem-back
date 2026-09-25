// Package reports contains user report and moderation business rules.
package reports

import "time"

const (
	TargetUser    = "user"
	TargetCompany = "company"
	TargetEvent   = "event"
)

const (
	StatusPending  = "pending"
	StatusResolved = "resolved"
	StatusRejected = "rejected"
)

type UserShort struct {
	ID        int64
	FirstName string
	LastName  *string
	AvatarURL *string
}

type Report struct {
	ID          int64
	TargetType  string
	TargetID    int64
	Reason      string
	Description *string
	Author      UserShort
	Status      string
	ResolvedBy  *UserShort
	CreatedAt   time.Time
	ResolvedAt  *time.Time
}

type CreateInput struct {
	TargetType  string
	TargetID    int64
	Reason      string
	Description *string
}

type Page struct {
	Offset int64
	Limit  int64
}
