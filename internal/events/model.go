// Package events contains event catalog and moderation business rules.
package events

import "time"

const (
	StatusPending   = "pending"
	StatusActive    = "active"
	StatusRejected  = "rejected"
	StatusBlocked   = "blocked"
	StatusCompleted = "completed"
)

const (
	RelationCreator               = "creator"
	RelationParticipant           = "participant"
	RelationCreatorAndParticipant = "creator_and_participant"
)

type UserShort struct {
	ID        int64
	FirstName string
	LastName  *string
	AvatarURL *string
}

type Event struct {
	ID                int64
	Title             string
	Description       *string
	CategoryID        int64
	CityID            int64
	StartsAt          time.Time
	EndsAt            *time.Time
	LocationName      string
	Address           *string
	ImageURL          *string
	Status            string
	ParticipantsCount int64
	CompaniesCount    int64
	Creator           UserShort
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type MyEvent struct {
	Event
	Relation string
}

type CreateInput struct {
	Title        string
	Description  *string
	CategoryID   int64
	CityID       int64
	StartsAt     time.Time
	EndsAt       *time.Time
	LocationName string
	Address      *string
	ImageURL     *string
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
	Title        Change[string]
	Description  NullableChange[string]
	CategoryID   Change[int64]
	CityID       Change[int64]
	StartsAt     Change[time.Time]
	EndsAt       NullableChange[time.Time]
	LocationName Change[string]
	Address      NullableChange[string]
	ImageURL     NullableChange[string]
}

type Page struct {
	Offset int64
	Limit  int64
}

type PublicFilter struct {
	Search     string
	CityID     *int64
	CategoryID *int64
	DateFrom   *time.Time
	DateTo     *time.Time
	Sort       string
	Now        time.Time
}

type AdminFilter struct {
	Search string
	Status string
}

type MyFilter struct {
	Status string
	Now    time.Time
}
