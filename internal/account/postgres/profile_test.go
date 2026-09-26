package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/emount4/poidem-back/internal/account"
	accountpostgres "github.com/emount4/poidem-back/internal/account/postgres"
	platformpostgres "github.com/emount4/poidem-back/internal/platform/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestProfileUpdateIntegration(t *testing.T) {
	dsn := os.Getenv("POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("POSTGRES_TEST_DSN is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	suffix := time.Now().UnixNano()
	var cityID, firstInterestID, secondInterestID, userID int64
	if err := pool.QueryRow(ctx, `INSERT INTO cities (name, slug) VALUES ($1, $2) RETURNING id`, "Test city", fmt.Sprintf("test-city-%d", suffix)).Scan(&cityID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO interests (name, slug) VALUES ($1, $2) RETURNING id`, "First", fmt.Sprintf("first-%d", suffix)).Scan(&firstInterestID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO interests (name, slug) VALUES ($1, $2) RETURNING id`, "Second", fmt.Sprintf("second-%d", suffix)).Scan(&secondInterestID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO users (first_name, role, status, created_at, updated_at)
		VALUES ('', 'user', 'active', now(), now()) RETURNING id
	`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM user_interests WHERE user_id = $1`, userID)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID)
		_, _ = pool.Exec(ctx, `DELETE FROM interests WHERE id = ANY($1::bigint[])`, []int64{firstInterestID, secondInterestID})
		_, _ = pool.Exec(ctx, `DELETE FROM cities WHERE id = $1`, cityID)
	}()

	service, err := account.NewProfileService(accountpostgres.NewRepository(pool), platformpostgres.NewTxManager(pool))
	if err != nil {
		t.Fatal(err)
	}
	gender := "female"
	birthDate := time.Date(2000, time.April, 15, 0, 0, 0, 0, time.UTC)
	profile, err := service.Update(ctx, userID, account.ProfilePatch{
		FirstName:   account.Change[string]{Set: true, Value: " Анна "},
		LastName:    account.NullableChange[string]{Set: true, Value: "Иванова"},
		Gender:      account.NullableChange[string]{Set: true, Value: gender},
		BirthDate:   account.NullableChange[time.Time]{Set: true, Value: birthDate},
		CityID:      account.NullableChange[int64]{Set: true, Value: cityID},
		About:       account.NullableChange[string]{Set: true, Value: "О себе"},
		InterestIDs: account.Change[[]int64]{Set: true, Value: []int64{secondInterestID, firstInterestID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if profile.FirstName != "Анна" || profile.LastName == nil || *profile.LastName != "Иванова" || profile.Gender == nil || *profile.Gender != gender || profile.BirthDate == nil || !profile.BirthDate.Equal(birthDate) || profile.City == nil || profile.City.ID != cityID || !profile.IsComplete() || len(profile.Interests) != 2 {
		t.Fatalf("unexpected updated profile: %+v", profile)
	}

	_, err = service.Update(ctx, userID, account.ProfilePatch{CityID: account.NullableChange[int64]{Set: true, Null: true}})
	if !errors.Is(err, account.ErrCityRequired) {
		t.Fatalf("clearing city error = %v", err)
	}
	profile, err = service.Get(ctx, userID)
	if err != nil || profile.City == nil || profile.City.ID != cityID {
		t.Fatalf("failed update changed the profile: %+v, %v", profile, err)
	}

	profile, err = service.Update(ctx, userID, account.ProfilePatch{InterestIDs: account.Change[[]int64]{Set: true, Value: []int64{}}})
	if err != nil {
		t.Fatal(err)
	}
	if profile.Interests == nil || len(profile.Interests) != 0 {
		t.Fatalf("cleared interests must be []: %+v", profile.Interests)
	}
	profile, err = service.Update(ctx, userID, account.ProfilePatch{
		LastName:  account.NullableChange[string]{Set: true, Null: true},
		Gender:    account.NullableChange[string]{Set: true, Null: true},
		BirthDate: account.NullableChange[time.Time]{Set: true, Null: true},
		About:     account.NullableChange[string]{Set: true, Null: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if profile.LastName != nil || profile.Gender != nil || profile.BirthDate != nil || profile.About != nil {
		t.Fatalf("nullable fields were not cleared: %+v", profile)
	}
}
