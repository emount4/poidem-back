// Package postgres implements event persistence using PostgreSQL.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/emount4/poidem-back/internal/events"
	platformpostgres "github.com/emount4/poidem-back/internal/platform/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const eventColumns = `
	e.id, e.title, e.description, e.category_id, e.city_id,
	e.starts_at, e.ends_at, e.location_name, e.address, e.image_url, e.status,
	e.latitude, e.longitude, e.location_source, e.moderation_reason,
	(SELECT count(*) FROM event_participants ep WHERE ep.event_id = e.id),
	(SELECT count(*) FROM companies c WHERE c.event_id = e.id AND c.deleted_at IS NULL AND c.status <> 'blocked'),
	u.id, u.first_name, u.last_name, u.avatar_url, e.created_at, e.updated_at`

const eventFrom = ` FROM events e JOIN users u ON u.id = e.creator_id `

type Repository struct {
	pool         *pgxpool.Pool
	transactions *platformpostgres.TxManager
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool, transactions: platformpostgres.NewTxManager(pool)}
}

func (r *Repository) Create(ctx context.Context, creatorID int64, input events.CreateInput) (events.Event, error) {
	var result events.Event
	err := r.transactions.WithinTransaction(ctx, func(txCtx context.Context) error {
		db := platformpostgres.Executor(txCtx, r.pool)
		var id int64
		err := db.QueryRow(txCtx, `
			INSERT INTO events (
				creator_id, category_id, city_id, title, description, image_url,
				starts_at, ends_at, location_name, address, latitude, longitude,
				location_source, status, created_at, updated_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,now(),now())
			RETURNING id
		`, creatorID, input.CategoryID, input.CityID, input.Title, input.Description,
			input.ImageURL, input.StartsAt, input.EndsAt, input.LocationName, input.Address,
			locationLatitude(input.Location), locationLongitude(input.Location), locationSource(input.Location),
			events.StatusPending).Scan(&id)
		if err != nil {
			return mapWriteError(err)
		}
		if input.ImageURL != nil {
			if err := attachCover(txCtx, db, *input.ImageURL, creatorID, id, false); err != nil {
				return err
			}
		}
		result, err = r.getByID(txCtx, id)
		return err
	})
	return result, err
}

func (r *Repository) ListPublic(ctx context.Context, filter events.PublicFilter, page events.Page) ([]events.Event, int64, error) {
	where, args := publicWhere(filter)
	order := "e.starts_at ASC, e.id ASC"
	switch filter.Sort {
	case "popular":
		order = "(SELECT count(*) FROM event_participants ep WHERE ep.event_id = e.id) DESC, e.id ASC"
	case "newest":
		order = "e.created_at DESC, e.id DESC"
	}
	items, err := r.queryEvents(ctx, `SELECT `+eventColumns+eventFrom+where+` ORDER BY `+order+` LIMIT $`+fmt.Sprint(len(args)+1)+` OFFSET $`+fmt.Sprint(len(args)+2), append(args, page.Limit, page.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	var total int64
	if err := platformpostgres.Executor(ctx, r.pool).QueryRow(ctx, `SELECT count(*) FROM events e `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count public events: %w", err)
	}
	return items, total, nil
}

func (r *Repository) GetVisible(ctx context.Context, id int64, viewerID *int64) (events.Event, error) {
	query := `SELECT ` + eventColumns + eventFrom + `
		WHERE e.id = $1 AND e.deleted_at IS NULL AND (
			(e.status = 'active' AND ((e.ends_at IS NOT NULL AND e.ends_at > now()) OR (e.ends_at IS NULL AND e.starts_at > now())))
			OR ($2::bigint IS NOT NULL AND e.creator_id = $2 AND e.status IN ('pending','rejected'))
		)`
	return scanEvent(platformpostgres.Executor(ctx, r.pool).QueryRow(ctx, query, id, viewerID))
}

func (r *Repository) ListParticipants(ctx context.Context, eventID int64, viewerID *int64, page events.Page) ([]events.UserShort, int64, error) {
	if _, err := r.GetVisible(ctx, eventID, viewerID); err != nil {
		return nil, 0, err
	}
	db := platformpostgres.Executor(ctx, r.pool)
	rows, err := db.Query(ctx, `
		SELECT u.id, u.first_name, u.last_name, u.avatar_url
		FROM event_participants ep
		JOIN users u ON u.id = ep.user_id
		WHERE ep.event_id = $1
		ORDER BY ep.joined_at, u.id
		LIMIT $2 OFFSET $3
	`, eventID, page.Limit, page.Offset)
	if err != nil {
		return nil, 0, fmt.Errorf("query event participants: %w", err)
	}
	defer rows.Close()
	items := make([]events.UserShort, 0)
	for rows.Next() {
		var item events.UserShort
		if err := rows.Scan(&item.ID, &item.FirstName, &item.LastName, &item.AvatarURL); err != nil {
			return nil, 0, fmt.Errorf("scan event participant: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate event participants: %w", err)
	}
	var total int64
	if err := db.QueryRow(ctx, `SELECT count(*) FROM event_participants WHERE event_id = $1`, eventID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count event participants: %w", err)
	}
	return items, total, nil
}

func (r *Repository) ListMine(ctx context.Context, userID int64, filter events.MyFilter, page events.Page) ([]events.MyEvent, int64, error) {
	condition := `e.deleted_at IS NULL AND (e.creator_id = $1 OR EXISTS (SELECT 1 FROM event_participants mine WHERE mine.event_id=e.id AND mine.user_id=$1))`
	args := []any{userID}
	if filter.Status == "upcoming" {
		args = append(args, filter.Now)
		condition += ` AND e.status <> 'completed' AND ((e.ends_at IS NOT NULL AND e.ends_at > $2) OR (e.ends_at IS NULL AND e.starts_at > $2))`
	} else if filter.Status == "past" {
		args = append(args, filter.Now)
		condition += ` AND (e.status = 'completed' OR (e.ends_at IS NOT NULL AND e.ends_at <= $2) OR (e.ends_at IS NULL AND e.starts_at <= $2))`
	}
	relation := `CASE
		WHEN e.creator_id=$1 AND EXISTS (SELECT 1 FROM event_participants mine WHERE mine.event_id=e.id AND mine.user_id=$1) THEN 'creator_and_participant'
		WHEN e.creator_id=$1 THEN 'creator'
		ELSE 'participant' END`
	query := `SELECT ` + eventColumns + `, ` + relation + eventFrom + ` WHERE (` + condition + `) ORDER BY e.starts_at ASC,e.id ASC LIMIT $` + fmt.Sprint(len(args)+1) + ` OFFSET $` + fmt.Sprint(len(args)+2)
	rows, err := platformpostgres.Executor(ctx, r.pool).Query(ctx, query, append(args, page.Limit, page.Offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("query my events: %w", err)
	}
	defer rows.Close()
	items := make([]events.MyEvent, 0)
	for rows.Next() {
		item, err := scanMyEvent(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate my events: %w", err)
	}
	var total int64
	if err := platformpostgres.Executor(ctx, r.pool).QueryRow(ctx, `SELECT count(*) FROM events e WHERE (`+condition+`)`, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count my events: %w", err)
	}
	return items, total, nil
}

func (r *Repository) ListAdmin(ctx context.Context, filter events.AdminFilter, page events.Page) ([]events.Event, int64, error) {
	where, args := adminWhere(filter)
	items, err := r.queryEvents(ctx, `SELECT `+eventColumns+eventFrom+where+` ORDER BY e.created_at DESC,e.id DESC LIMIT $`+fmt.Sprint(len(args)+1)+` OFFSET $`+fmt.Sprint(len(args)+2), append(args, page.Limit, page.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	var total int64
	if err := platformpostgres.Executor(ctx, r.pool).QueryRow(ctx, `SELECT count(*) FROM events e `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count admin events: %w", err)
	}
	return items, total, nil
}

func (r *Repository) GetAdmin(ctx context.Context, id int64) (events.Event, error) {
	return r.getByID(ctx, id)
}

func (r *Repository) Update(ctx context.Context, id int64, patch events.Patch) (events.Event, error) {
	return r.update(ctx, id, nil, patch, time.Now())
}

func (r *Repository) UpdateOwned(ctx context.Context, id, userID int64, patch events.Patch, now time.Time) (events.Event, error) {
	return r.update(ctx, id, &userID, patch, now)
}

func (r *Repository) update(ctx context.Context, id int64, ownerID *int64, patch events.Patch, now time.Time) (events.Event, error) {
	var result events.Event
	err := r.transactions.WithinTransaction(ctx, func(txCtx context.Context) error {
		db := platformpostgres.Executor(txCtx, r.pool)
		var currentStart time.Time
		var currentEnd *time.Time
		var creatorID int64
		var currentStatus string
		var currentImageURL *string
		if err := db.QueryRow(txCtx, `
			SELECT creator_id, status, starts_at, ends_at, image_url
			FROM events WHERE id=$1 AND deleted_at IS NULL FOR UPDATE
		`, id).Scan(&creatorID, &currentStatus, &currentStart, &currentEnd, &currentImageURL); errors.Is(err, pgx.ErrNoRows) {
			return events.ErrNotFound
		} else if err != nil {
			return fmt.Errorf("lock event: %w", err)
		}
		if ownerID != nil && creatorID != *ownerID {
			return events.ErrForbidden
		}
		if ownerID != nil && currentStatus != events.StatusPending && currentStatus != events.StatusRejected && currentStatus != events.StatusActive {
			return events.ErrNotEditable
		}
		start := currentStart
		if patch.StartsAt.Set {
			start = patch.StartsAt.Value
		}
		var end *time.Time
		if patch.EndsAt.Set {
			if !patch.EndsAt.Null {
				end = &patch.EndsAt.Value
			}
		} else {
			end = currentEnd
		}
		if end != nil && !end.After(start) {
			return &events.ValidationError{Fields: map[string][]string{"endsAt": {"Должно быть позже startsAt"}}}
		}
		if ownerID != nil && !start.After(now) {
			return &events.ValidationError{Fields: map[string][]string{"startsAt": {"Дата начала должна быть в будущем"}}}
		}
		if patch.ImageURL.Set && !patch.ImageURL.Null && (currentImageURL == nil || *currentImageURL != patch.ImageURL.Value) {
			coverOwnerID := creatorID
			if ownerID != nil {
				coverOwnerID = *ownerID
			}
			if err := attachCover(txCtx, db, patch.ImageURL.Value, coverOwnerID, id, ownerID == nil); err != nil {
				return err
			}
		}

		_, err := db.Exec(txCtx, `
			UPDATE events SET
				title=CASE WHEN $2 THEN $3::varchar ELSE title END,
				description=CASE WHEN $4 THEN $5::text ELSE description END,
				category_id=CASE WHEN $6 THEN $7::bigint ELSE category_id END,
				city_id=CASE WHEN $8 THEN $9::bigint ELSE city_id END,
				starts_at=CASE WHEN $10 THEN $11::timestamptz ELSE starts_at END,
				ends_at=CASE WHEN $12 THEN $13::timestamptz ELSE ends_at END,
				location_name=CASE WHEN $14 THEN $15::varchar ELSE location_name END,
				address=CASE WHEN $16 THEN $17::varchar ELSE address END,
				latitude=CASE WHEN $18 THEN $19::double precision ELSE latitude END,
				longitude=CASE WHEN $18 THEN $20::double precision ELSE longitude END,
				location_source=CASE WHEN $18 AND $19::double precision IS NOT NULL THEN $21::varchar ELSE location_source END,
				image_url=CASE WHEN $22 THEN $23::text ELSE image_url END,
				status=CASE WHEN $24 THEN 'pending' ELSE status END,
				moderation_reason=CASE WHEN $24 THEN NULL ELSE moderation_reason END,
				updated_at=$25
			WHERE id=$1
		`, id,
			patch.Title.Set, patch.Title.Value,
			patch.Description.Set, nullableString(patch.Description),
			patch.CategoryID.Set, patch.CategoryID.Value,
			patch.CityID.Set, patch.CityID.Value,
			patch.StartsAt.Set, patch.StartsAt.Value,
			patch.EndsAt.Set, nullableTime(patch.EndsAt),
			patch.LocationName.Set, patch.LocationName.Value,
			patch.Address.Set, nullableString(patch.Address),
			patch.Location.Set, nullableLocationLatitude(patch.Location), nullableLocationLongitude(patch.Location), nullableLocationSource(patch.Location),
			patch.ImageURL.Set, nullableString(patch.ImageURL),
			ownerID != nil, now,
		)
		if err != nil {
			return mapWriteError(err)
		}
		if patch.ImageURL.Set && currentImageURL != nil && (patch.ImageURL.Null || *currentImageURL != patch.ImageURL.Value) {
			if _, err := db.Exec(txCtx, `
				UPDATE event_uploads SET event_id = NULL, linked_at = NULL
				WHERE event_id = $1 AND public_url = $2
			`, id, *currentImageURL); err != nil {
				return fmt.Errorf("release previous event cover: %w", err)
			}
		}
		result, err = r.getByID(txCtx, id)
		return err
	})
	return result, err
}

func (r *Repository) Delete(ctx context.Context, id int64, ownerID *int64, now time.Time) error {
	return r.transactions.WithinTransaction(ctx, func(txCtx context.Context) error {
		db := platformpostgres.Executor(txCtx, r.pool)
		var creatorID int64
		err := db.QueryRow(txCtx, `
			SELECT creator_id FROM events
			WHERE id = $1 AND deleted_at IS NULL
			FOR UPDATE
		`, id).Scan(&creatorID)
		if errors.Is(err, pgx.ErrNoRows) {
			return events.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock event for deletion: %w", err)
		}
		if ownerID != nil && creatorID != *ownerID {
			return events.ErrForbidden
		}
		if _, err := db.Exec(txCtx, `
			UPDATE events
			SET deleted_at = $2, status = 'blocked', moderation_reason = 'Событие удалено', updated_at = $2
			WHERE id = $1
		`, id, now); err != nil {
			return fmt.Errorf("soft delete event: %w", err)
		}
		if _, err := db.Exec(txCtx, `
			UPDATE companies SET status = 'blocked', updated_at = $2
			WHERE event_id = $1 AND deleted_at IS NULL
		`, id, now); err != nil {
			return fmt.Errorf("block deleted event companies: %w", err)
		}
		if _, err := db.Exec(txCtx, `
			UPDATE applications a
			SET status = 'cancelled', resolved_at = $2, resolution_reason = 'EVENT_DELETED'
			FROM companies c
			WHERE a.company_id = c.id AND c.event_id = $1 AND a.status = 'pending'
		`, id, now); err != nil {
			return fmt.Errorf("cancel deleted event applications: %w", err)
		}
		if _, err := db.Exec(txCtx, `DELETE FROM event_participants WHERE event_id = $1`, id); err != nil {
			return fmt.Errorf("remove deleted event participation: %w", err)
		}
		if _, err := db.Exec(txCtx, `
			UPDATE event_uploads SET event_id = NULL, linked_at = NULL WHERE event_id = $1
		`, id); err != nil {
			return fmt.Errorf("release deleted event cover: %w", err)
		}
		return nil
	})
}

func (r *Repository) Transition(ctx context.Context, id int64, action string, reason *string, now time.Time) (events.Event, error) {
	var result events.Event
	err := r.transactions.WithinTransaction(ctx, func(txCtx context.Context) error {
		db := platformpostgres.Executor(txCtx, r.pool)
		var current string
		var startsAt time.Time
		var latitude, longitude *float64
		var imageURL *string
		if err := db.QueryRow(txCtx, `
			SELECT status, starts_at, latitude, longitude, image_url
			FROM events WHERE id=$1 AND deleted_at IS NULL FOR UPDATE
		`, id).Scan(&current, &startsAt, &latitude, &longitude, &imageURL); errors.Is(err, pgx.ErrNoRows) {
			return events.ErrNotFound
		} else if err != nil {
			return fmt.Errorf("lock event status: %w", err)
		}
		target := ""
		switch {
		case current == events.StatusPending && action == "approve":
			fields := make(map[string][]string)
			if latitude == nil || longitude == nil {
				fields["location"] = []string{"Координаты обязательны для публикации"}
			}
			if imageURL == nil || strings.TrimSpace(*imageURL) == "" {
				fields["imageUrl"] = []string{"Обложка обязательна для публикации"}
			}
			if !startsAt.After(now) {
				fields["startsAt"] = []string{"Дата начала должна быть в будущем"}
			}
			if len(fields) > 0 {
				return &events.ValidationError{Fields: fields}
			}
			target = events.StatusActive
		case current == events.StatusPending && action == "reject":
			target = events.StatusRejected
		case current == events.StatusActive && action == "block":
			target = events.StatusBlocked
		default:
			return events.ErrInvalidStatusTransition
		}
		if _, err := db.Exec(txCtx, `
			UPDATE events
			SET status=$2, moderation_reason=$3, updated_at=$4
			WHERE id=$1
		`, id, target, reason, now); err != nil {
			return fmt.Errorf("transition event: %w", err)
		}
		var err error
		result, err = r.getByID(txCtx, id)
		return err
	})
	return result, err
}

func (r *Repository) JoinSolo(ctx context.Context, eventID, userID int64, now time.Time) error {
	return r.transactions.WithinTransaction(ctx, func(txCtx context.Context) error {
		db := platformpostgres.Executor(txCtx, r.pool)
		var status string
		var startsAt time.Time
		var endsAt *time.Time
		err := db.QueryRow(txCtx, `
			SELECT status, starts_at, ends_at
			FROM events
			WHERE id = $1 AND deleted_at IS NULL
			FOR UPDATE
		`, eventID).Scan(&status, &startsAt, &endsAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return events.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock solo event: %w", err)
		}
		if !eventAcceptsParticipation(status, startsAt, endsAt, now) {
			return events.ErrEventNotAvailable
		}

		var participationType string
		err = db.QueryRow(txCtx, `
			SELECT participation_type
			FROM event_participants
			WHERE event_id = $1 AND user_id = $2
			FOR UPDATE
		`, eventID, userID).Scan(&participationType)
		switch {
		case err == nil && participationType == "company":
			return events.ErrAlreadyInEventCompany
		case err == nil:
			return events.ErrAlreadyEventParticipant
		case !errors.Is(err, pgx.ErrNoRows):
			return fmt.Errorf("check solo participation: %w", err)
		}

		if _, err := db.Exec(txCtx, `
			INSERT INTO event_participants (
				event_id, user_id, participation_type, company_id, joined_at
			) VALUES ($1,$2,'solo',NULL,$3)
		`, eventID, userID, now); err != nil {
			return fmt.Errorf("insert solo participation: %w", err)
		}
		return nil
	})
}

func (r *Repository) CancelSolo(ctx context.Context, eventID, userID int64) error {
	return r.transactions.WithinTransaction(ctx, func(txCtx context.Context) error {
		db := platformpostgres.Executor(txCtx, r.pool)
		var lockedID int64
		err := db.QueryRow(txCtx, `SELECT id FROM events WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, eventID).Scan(&lockedID)
		if errors.Is(err, pgx.ErrNoRows) {
			return events.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock event for solo cancellation: %w", err)
		}
		var deletedUserID int64
		err = db.QueryRow(txCtx, `
			DELETE FROM event_participants
			WHERE event_id = $1 AND user_id = $2 AND participation_type = 'solo'
			RETURNING user_id
		`, eventID, userID).Scan(&deletedUserID)
		if errors.Is(err, pgx.ErrNoRows) {
			return events.ErrNotSoloParticipant
		}
		if err != nil {
			return fmt.Errorf("cancel solo participation: %w", err)
		}
		return nil
	})
}

func (r *Repository) CompleteDue(ctx context.Context, now time.Time, limit int64) (int64, error) {
	var count int64
	err := r.transactions.WithinTransaction(ctx, func(txCtx context.Context) error {
		db := platformpostgres.Executor(txCtx, r.pool)
		rows, err := db.Query(txCtx, `
			WITH due AS (
				SELECT id
				FROM events
				WHERE status = 'active' AND deleted_at IS NULL
				  AND ((ends_at IS NOT NULL AND ends_at <= $1) OR (ends_at IS NULL AND starts_at <= $1))
				ORDER BY id
				LIMIT $2
				FOR UPDATE SKIP LOCKED
			)
			UPDATE events e
			SET status = 'completed', updated_at = $1
			FROM due
			WHERE e.id = due.id
			RETURNING e.id
		`, now, limit)
		if err != nil {
			return fmt.Errorf("complete due events: %w", err)
		}
		completedIDs := make([]int64, 0, limit)
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return fmt.Errorf("scan completed event: %w", err)
			}
			completedIDs = append(completedIDs, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return fmt.Errorf("iterate completed events: %w", err)
		}
		count = int64(len(completedIDs))
		if count == 0 {
			return nil
		}
		if _, err := db.Exec(txCtx, `
			UPDATE applications a
			SET status = 'cancelled', resolved_at = $1, resolution_reason = 'EVENT_COMPLETED'
			FROM companies c
			WHERE a.company_id = c.id
			  AND c.event_id = ANY($2::bigint[])
			  AND a.status = 'pending'
		`, now, completedIDs); err != nil {
			return fmt.Errorf("cancel completed event applications: %w", err)
		}
		return nil
	})
	return count, err
}

func (r *Repository) getByID(ctx context.Context, id int64) (events.Event, error) {
	return scanEvent(platformpostgres.Executor(ctx, r.pool).QueryRow(ctx, `SELECT `+eventColumns+eventFrom+` WHERE e.id=$1 AND e.deleted_at IS NULL`, id))
}

type rowScanner interface{ Scan(...any) error }

func scanEvent(row rowScanner) (events.Event, error) {
	var item events.Event
	var latitude, longitude *float64
	var locationSource string
	err := row.Scan(&item.ID, &item.Title, &item.Description, &item.CategoryID, &item.CityID,
		&item.StartsAt, &item.EndsAt, &item.LocationName, &item.Address, &item.ImageURL, &item.Status,
		&latitude, &longitude, &locationSource, &item.ModerationReason,
		&item.ParticipantsCount, &item.CompaniesCount, &item.Creator.ID, &item.Creator.FirstName,
		&item.Creator.LastName, &item.Creator.AvatarURL, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return events.Event{}, events.ErrNotFound
	}
	if err != nil {
		return events.Event{}, fmt.Errorf("scan event: %w", err)
	}
	if latitude != nil && longitude != nil {
		item.Location = &events.Location{Latitude: *latitude, Longitude: *longitude, Source: locationSource}
	}
	return item, nil
}

func scanMyEvent(row rowScanner) (events.MyEvent, error) {
	var item events.MyEvent
	var latitude, longitude *float64
	var locationSource string
	err := row.Scan(&item.ID, &item.Title, &item.Description, &item.CategoryID, &item.CityID,
		&item.StartsAt, &item.EndsAt, &item.LocationName, &item.Address, &item.ImageURL, &item.Status,
		&latitude, &longitude, &locationSource, &item.ModerationReason,
		&item.ParticipantsCount, &item.CompaniesCount, &item.Creator.ID, &item.Creator.FirstName,
		&item.Creator.LastName, &item.Creator.AvatarURL, &item.CreatedAt, &item.UpdatedAt, &item.Relation)
	if err != nil {
		return events.MyEvent{}, fmt.Errorf("scan my event: %w", err)
	}
	if latitude != nil && longitude != nil {
		item.Location = &events.Location{Latitude: *latitude, Longitude: *longitude, Source: locationSource}
	}
	return item, nil
}

func (r *Repository) queryEvents(ctx context.Context, query string, args ...any) ([]events.Event, error) {
	rows, err := platformpostgres.Executor(ctx, r.pool).Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()
	items := make([]events.Event, 0)
	for rows.Next() {
		item, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate events: %w", err)
	}
	return items, nil
}

func publicWhere(filter events.PublicFilter) (string, []any) {
	conditions := []string{`e.deleted_at IS NULL`, `e.status='active'`, `((e.ends_at IS NOT NULL AND e.ends_at>$1) OR (e.ends_at IS NULL AND e.starts_at>$1))`}
	args := []any{filter.Now}
	add := func(condition string, value any) {
		args = append(args, value)
		conditions = append(conditions, fmt.Sprintf(condition, len(args)))
	}
	if filter.Search != "" {
		args = append(args, filter.Search)
		index := len(args)
		conditions = append(conditions, fmt.Sprintf(`(e.title ILIKE '%%'||$%d||'%%' OR COALESCE(e.description,'') ILIKE '%%'||$%d||'%%' OR e.location_name ILIKE '%%'||$%d||'%%')`, index, index, index))
	}
	if filter.CityID != nil {
		add(`e.city_id=$%d`, *filter.CityID)
	}
	if filter.CategoryID != nil {
		add(`e.category_id=$%d`, *filter.CategoryID)
	}
	if filter.DateFrom != nil {
		add(`e.starts_at >= $%d`, *filter.DateFrom)
	}
	if filter.DateTo != nil {
		add(`e.starts_at <= $%d`, *filter.DateTo)
	}
	if filter.Bounds != nil {
		add(`e.latitude >= $%d`, filter.Bounds.South)
		add(`e.latitude <= $%d`, filter.Bounds.North)
		if filter.Bounds.West <= filter.Bounds.East {
			args = append(args, filter.Bounds.West, filter.Bounds.East)
			conditions = append(conditions, fmt.Sprintf(`e.longitude >= $%d AND e.longitude <= $%d`, len(args)-1, len(args)))
		} else {
			args = append(args, filter.Bounds.West, filter.Bounds.East)
			conditions = append(conditions, fmt.Sprintf(`(e.longitude >= $%d OR e.longitude <= $%d)`, len(args)-1, len(args)))
		}
	}
	return " WHERE " + strings.Join(conditions, " AND "), args
}

func adminWhere(filter events.AdminFilter) (string, []any) {
	conditions := []string{`e.deleted_at IS NULL`}
	args := make([]any, 0, 2)
	if filter.Search != "" {
		args = append(args, filter.Search)
		conditions = append(conditions, `(e.title ILIKE '%'||$1||'%' OR COALESCE(e.description,'') ILIKE '%'||$1||'%')`)
	}
	if filter.Status != "" {
		args = append(args, filter.Status)
		conditions = append(conditions, `e.status=$`+fmt.Sprint(len(args)))
	}
	return " WHERE " + strings.Join(conditions, " AND "), args
}

func mapWriteError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.ConstraintName {
		case "events_city_id_fkey":
			return events.ErrCityNotFound
		case "events_category_id_fkey":
			return events.ErrCategoryNotFound
		}
	}
	return fmt.Errorf("write event: %w", err)
}

func attachCover(ctx context.Context, db platformpostgres.DBTX, publicURL string, ownerID, eventID int64, admin bool) error {
	var actualOwnerID int64
	var linkedEventID *int64
	err := db.QueryRow(ctx, `
		SELECT owner_id, event_id
		FROM event_uploads
		WHERE public_url = $1
		FOR UPDATE
	`, publicURL).Scan(&actualOwnerID, &linkedEventID)
	if errors.Is(err, pgx.ErrNoRows) {
		return events.ErrCoverNotOwned
	}
	if err != nil {
		return fmt.Errorf("lock event cover: %w", err)
	}
	if !admin && actualOwnerID != ownerID {
		return events.ErrCoverNotOwned
	}
	if linkedEventID != nil && *linkedEventID != eventID {
		return events.ErrCoverNotOwned
	}
	if _, err := db.Exec(ctx, `
		UPDATE event_uploads
		SET event_id = $2, linked_at = COALESCE(linked_at, now())
		WHERE public_url = $1
	`, publicURL, eventID); err != nil {
		return fmt.Errorf("attach event cover: %w", err)
	}
	return nil
}

func nullableString(change events.NullableChange[string]) any {
	if change.Null {
		return nil
	}
	return change.Value
}
func nullableTime(change events.NullableChange[time.Time]) any {
	if change.Null {
		return nil
	}
	return change.Value
}

func locationLatitude(location *events.Location) any {
	if location == nil {
		return nil
	}
	return location.Latitude
}

func locationLongitude(location *events.Location) any {
	if location == nil {
		return nil
	}
	return location.Longitude
}

func locationSource(location *events.Location) string {
	if location == nil {
		return "manual"
	}
	return location.Source
}

func nullableLocationLatitude(change events.NullableChange[events.Location]) any {
	if change.Null {
		return nil
	}
	return change.Value.Latitude
}

func nullableLocationLongitude(change events.NullableChange[events.Location]) any {
	if change.Null {
		return nil
	}
	return change.Value.Longitude
}

func nullableLocationSource(change events.NullableChange[events.Location]) any {
	if change.Null {
		return nil
	}
	return change.Value.Source
}

func eventAcceptsParticipation(status string, startsAt time.Time, endsAt *time.Time, now time.Time) bool {
	if status != events.StatusActive {
		return false
	}
	if endsAt != nil {
		return endsAt.After(now)
	}
	return startsAt.After(now)
}
