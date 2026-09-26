package account

import (
	"context"
	"errors"
	"testing"
	"time"
)

type profileStoreStub struct {
	profile Profile
	patch   ProfilePatch
	err     error
	updates int
}

func (s *profileStoreStub) Profile(context.Context, int64) (Profile, error) {
	return s.profile, s.err
}
func (s *profileStoreStub) UpdateProfile(_ context.Context, _ int64, patch ProfilePatch) error {
	s.patch = patch
	s.updates++
	return s.err
}

func TestProfileServiceUpdatesInTransaction(t *testing.T) {
	store := &profileStoreStub{profile: Profile{ID: 7, FirstName: "Anna", Interests: nil}}
	tx := &txStub{}
	service, err := NewProfileService(store, tx)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := service.Update(context.Background(), 7, ProfilePatch{
		FirstName:   Change[string]{Set: true, Value: "  Anna  "},
		InterestIDs: Change[[]int64]{Set: true, Value: []int64{3, 8}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !tx.committed || store.updates != 1 || store.patch.FirstName.Value != "Anna" {
		t.Fatal("profile update was not committed or normalized")
	}
	if profile.Interests == nil {
		t.Fatal("empty interests must be returned as an array")
	}
}

func TestProfileServiceValidation(t *testing.T) {
	for _, tc := range []struct {
		name  string
		patch ProfilePatch
		field string
	}{
		{"empty first name", ProfilePatch{FirstName: Change[string]{Set: true, Value: "  "}}, "firstName"},
		{"invalid city", ProfilePatch{CityID: NullableChange[int64]{Set: true, Value: -1}}, "cityId"},
		{"invalid gender", ProfilePatch{Gender: NullableChange[string]{Set: true, Value: "unknown"}}, "gender"},
		{"future birth date", ProfilePatch{BirthDate: NullableChange[time.Time]{Set: true, Value: time.Now().AddDate(1, 0, 0)}}, "birthDate"},
		{"duplicate interests", ProfilePatch{InterestIDs: Change[[]int64]{Set: true, Value: []int64{2, 2}}}, "interestIds"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &profileStoreStub{}
			tx := &txStub{}
			service, _ := NewProfileService(store, tx)
			_, err := service.Update(context.Background(), 1, tc.patch)
			var validation *ValidationError
			if !errors.As(err, &validation) || len(validation.Fields[tc.field]) == 0 {
				t.Fatalf("error = %v", err)
			}
			if store.updates != 0 || tx.committed {
				t.Fatal("invalid update reached the transaction")
			}
		})
	}
}

func TestProfileServicePreservesBusinessError(t *testing.T) {
	store := &profileStoreStub{err: ErrCityRequired}
	service, _ := NewProfileService(store, &txStub{})
	_, err := service.Update(context.Background(), 1, ProfilePatch{CityID: NullableChange[int64]{Set: true, Null: true}})
	if !errors.Is(err, ErrCityRequired) {
		t.Fatalf("error = %v", err)
	}
}
