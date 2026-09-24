package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/emount4/poidem-back/internal/account"
	platformpostgres "github.com/emount4/poidem-back/internal/platform/postgres"
	"github.com/jackc/pgx/v5"
)

func (r *Repository) Profile(ctx context.Context, userID int64) (account.Profile, error) {
	db := platformpostgres.Executor(ctx, r.pool)
	var profile account.Profile
	var cityID *int64
	var cityName, citySlug *string
	err := db.QueryRow(ctx, `
		SELECT u.id, u.first_name, u.last_name, u.avatar_url,
		       c.id, c.name, c.slug, u.about,
		       COALESCE(u.role, 'user'), COALESCE(u.status, 'active'),
		       COALESCE(u.created_at, u.updated_at, now())
		FROM users u
		LEFT JOIN cities c ON c.id = u.city_id
		WHERE u.id = $1
	`, userID).Scan(
		&profile.ID, &profile.FirstName, &profile.LastName, &profile.AvatarURL,
		&cityID, &cityName, &citySlug, &profile.About,
		&profile.Role, &profile.Status, &profile.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return account.Profile{}, account.ErrUserNotFound
	}
	if err != nil {
		return account.Profile{}, fmt.Errorf("query profile: %w", err)
	}
	if cityID != nil {
		profile.City = &account.DictionaryItem{ID: *cityID, Name: valueOrEmpty(cityName), Slug: citySlug}
	}
	profile.Interests = make([]account.DictionaryItem, 0)
	rows, err := db.Query(ctx, `
		SELECT i.id, i.name, i.slug
		FROM interests i
		JOIN user_interests ui ON ui.interest_id = i.id
		WHERE ui.user_id = $1
		ORDER BY i.name, i.id
	`, userID)
	if err != nil {
		return account.Profile{}, fmt.Errorf("query profile interests: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item account.DictionaryItem
		if err := rows.Scan(&item.ID, &item.Name, &item.Slug); err != nil {
			return account.Profile{}, fmt.Errorf("scan profile interest: %w", err)
		}
		profile.Interests = append(profile.Interests, item)
	}
	if err := rows.Err(); err != nil {
		return account.Profile{}, fmt.Errorf("iterate profile interests: %w", err)
	}
	return profile, nil
}

func (r *Repository) UpdateProfile(ctx context.Context, userID int64, patch account.ProfilePatch) error {
	db := platformpostgres.Executor(ctx, r.pool)
	var currentFirstName string
	var currentCityID *int64
	err := db.QueryRow(ctx, `
		SELECT first_name, city_id
		FROM users
		WHERE id = $1
		FOR UPDATE
	`, userID).Scan(&currentFirstName, &currentCityID)
	if errors.Is(err, pgx.ErrNoRows) {
		return account.ErrUserNotFound
	}
	if err != nil {
		return fmt.Errorf("lock profile: %w", err)
	}
	if patch.CityID.Set && patch.CityID.Null && strings.TrimSpace(currentFirstName) != "" && currentCityID != nil {
		return account.ErrCityRequired
	}
	if patch.CityID.Set && !patch.CityID.Null {
		var exists int
		err := db.QueryRow(ctx, `SELECT 1 FROM cities WHERE id = $1 FOR KEY SHARE`, patch.CityID.Value).Scan(&exists)
		if errors.Is(err, pgx.ErrNoRows) {
			return account.ErrCityNotFound
		}
		if err != nil {
			return fmt.Errorf("check city: %w", err)
		}
	}
	if patch.InterestIDs.Set {
		if err := lockInterests(ctx, db, patch.InterestIDs.Value); err != nil {
			return err
		}
	}
	if patch.FirstName.Set || patch.LastName.Set || patch.CityID.Set || patch.About.Set {
		var lastName, cityID, about any
		if patch.LastName.Set && !patch.LastName.Null {
			lastName = patch.LastName.Value
		}
		if patch.CityID.Set && !patch.CityID.Null {
			cityID = patch.CityID.Value
		}
		if patch.About.Set && !patch.About.Null {
			about = patch.About.Value
		}
		_, err := db.Exec(ctx, `
			UPDATE users
			SET first_name = CASE WHEN $2 THEN $3::varchar ELSE first_name END,
			    last_name = CASE WHEN $4 THEN $5::varchar ELSE last_name END,
			    city_id = CASE WHEN $6 THEN $7::bigint ELSE city_id END,
			    about = CASE WHEN $8 THEN $9::text ELSE about END,
			    updated_at = now()
			WHERE id = $1
		`, userID,
			patch.FirstName.Set, patch.FirstName.Value,
			patch.LastName.Set, lastName,
			patch.CityID.Set, cityID,
			patch.About.Set, about,
		)
		if err != nil {
			return fmt.Errorf("update profile fields: %w", err)
		}
	}
	if patch.InterestIDs.Set {
		if _, err := db.Exec(ctx, `DELETE FROM user_interests WHERE user_id = $1`, userID); err != nil {
			return fmt.Errorf("clear profile interests: %w", err)
		}
		if len(patch.InterestIDs.Value) > 0 {
			if _, err := db.Exec(ctx, `
				INSERT INTO user_interests (user_id, interest_id)
				SELECT $1, unnest($2::bigint[])
			`, userID, patch.InterestIDs.Value); err != nil {
				return fmt.Errorf("replace profile interests: %w", err)
			}
		}
	}
	return nil
}

func lockInterests(ctx context.Context, db platformpostgres.DBTX, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	rows, err := db.Query(ctx, `SELECT id FROM interests WHERE id = ANY($1::bigint[]) FOR KEY SHARE`, ids)
	if err != nil {
		return fmt.Errorf("check interests: %w", err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("scan interest: %w", err)
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate interests: %w", err)
	}
	if count != len(ids) {
		return account.ErrInterestsInvalid
	}
	return nil
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
