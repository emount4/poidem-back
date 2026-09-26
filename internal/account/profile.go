package account

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrCityRequired     = errors.New("city cannot be cleared after onboarding")
	ErrCityNotFound     = errors.New("city not found")
	ErrInterestsInvalid = errors.New("one or more interests do not exist")
)

type DictionaryItem struct {
	ID   int64
	Name string
	Slug *string
}

type Profile struct {
	ID        int64
	FirstName string
	LastName  *string
	AvatarURL *string
	City      *DictionaryItem
	About     *string
	Gender    *string
	BirthDate *time.Time
	Interests []DictionaryItem
	Role      string
	Status    string
	CreatedAt time.Time
}

func (p Profile) IsComplete() bool {
	return strings.TrimSpace(p.FirstName) != "" && p.City != nil
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

type ProfilePatch struct {
	FirstName   Change[string]
	LastName    NullableChange[string]
	CityID      NullableChange[int64]
	About       NullableChange[string]
	Gender      NullableChange[string]
	BirthDate   NullableChange[time.Time]
	InterestIDs Change[[]int64]
}

type ValidationError struct {
	Fields map[string][]string
}

func (e *ValidationError) Error() string { return "profile validation failed" }

type ProfileStore interface {
	Profile(context.Context, int64) (Profile, error)
	UpdateProfile(context.Context, int64, ProfilePatch) error
}

type ProfileService struct {
	store ProfileStore
	tx    TransactionManager
}

func NewProfileService(store ProfileStore, tx TransactionManager) (*ProfileService, error) {
	if store == nil || tx == nil {
		return nil, errors.New("profile dependencies are required")
	}
	return &ProfileService{store: store, tx: tx}, nil
}

func (s *ProfileService) Get(ctx context.Context, userID int64) (Profile, error) {
	if userID <= 0 {
		return Profile{}, ErrUserNotFound
	}
	profile, err := s.store.Profile(ctx, userID)
	if err != nil {
		return Profile{}, fmt.Errorf("load profile: %w", err)
	}
	if profile.Interests == nil {
		profile.Interests = []DictionaryItem{}
	}
	return profile, nil
}

func (s *ProfileService) Update(ctx context.Context, userID int64, patch ProfilePatch) (Profile, error) {
	if userID <= 0 {
		return Profile{}, ErrUserNotFound
	}
	normalizeProfilePatch(&patch)
	if fields := validateProfilePatch(patch); len(fields) > 0 {
		return Profile{}, &ValidationError{Fields: fields}
	}
	var profile Profile
	err := s.tx.WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := s.store.UpdateProfile(txCtx, userID, patch); err != nil {
			return err
		}
		var err error
		profile, err = s.store.Profile(txCtx, userID)
		return err
	})
	if err != nil {
		return Profile{}, fmt.Errorf("update profile: %w", err)
	}
	if profile.Interests == nil {
		profile.Interests = []DictionaryItem{}
	}
	return profile, nil
}

func normalizeProfilePatch(patch *ProfilePatch) {
	if patch.FirstName.Set {
		patch.FirstName.Value = strings.TrimSpace(patch.FirstName.Value)
	}
	if patch.LastName.Set && !patch.LastName.Null {
		patch.LastName.Value = strings.TrimSpace(patch.LastName.Value)
	}
	if patch.Gender.Set && !patch.Gender.Null {
		patch.Gender.Value = strings.ToLower(strings.TrimSpace(patch.Gender.Value))
	}
}

func validateProfilePatch(patch ProfilePatch) map[string][]string {
	fields := make(map[string][]string)
	if patch.FirstName.Set && (patch.FirstName.Value == "" || utf8.RuneCountInString(patch.FirstName.Value) > 100) {
		fields["firstName"] = []string{"Длина должна быть от 1 до 100 символов"}
	}
	if patch.LastName.Set && !patch.LastName.Null && utf8.RuneCountInString(patch.LastName.Value) > 100 {
		fields["lastName"] = []string{"Максимальная длина — 100 символов"}
	}
	if patch.CityID.Set && !patch.CityID.Null && patch.CityID.Value <= 0 {
		fields["cityId"] = []string{"Идентификатор должен быть положительным"}
	}
	if patch.About.Set && !patch.About.Null && utf8.RuneCountInString(patch.About.Value) > 2000 {
		fields["about"] = []string{"Максимальная длина — 2000 символов"}
	}
	if patch.Gender.Set && !patch.Gender.Null && patch.Gender.Value != "male" && patch.Gender.Value != "female" && patch.Gender.Value != "other" {
		fields["gender"] = []string{"Допустимые значения: male, female, other"}
	}
	if patch.BirthDate.Set && !patch.BirthDate.Null {
		today := time.Now().UTC()
		today = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
		if patch.BirthDate.Value.After(today) {
			fields["birthDate"] = []string{"Дата рождения не может быть в будущем"}
		}
	}
	if patch.InterestIDs.Set {
		seen := make(map[int64]struct{}, len(patch.InterestIDs.Value))
		for _, id := range patch.InterestIDs.Value {
			if id <= 0 {
				fields["interestIds"] = []string{"Все идентификаторы должны быть положительными"}
				break
			}
			if _, exists := seen[id]; exists {
				fields["interestIds"] = []string{"Идентификаторы не должны повторяться"}
				break
			}
			seen[id] = struct{}{}
		}
	}
	return fields
}
