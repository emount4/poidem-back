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
				starts_at, ends_at, location_name, address, status, created_at, updated_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,now(),now())
			RETURNING id
		`, creatorID, input.CategoryID, input.CityID, input.Title, input.Description,
			input.ImageURL, input.StartsAt, input.EndsAt, input.LocationName, input.Address,
			events.StatusPending).Scan(&id)
		if err != nil {
			return mapWriteError(err)
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
		WHERE e.id = $1 AND (
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
	condition := `(e.creator_id = $1 OR EXISTS (SELECT 1 FROM event_participants mine WHERE mine.event_id=e.id AND mine.user_id=$1))`
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
	var result events.Event
	err := r.transactions.WithinTransaction(ctx, func(txCtx context.Context) error {
		db := platformpostgres.Executor(txCtx, r.pool)
		var currentStart time.Time
		var currentEnd *time.Time
		if err := db.QueryRow(txCtx, `SELECT starts_at, ends_at FROM events WHERE id=$1 FOR UPDATE`, id).Scan(&currentStart, &currentEnd); errors.Is(err, pgx.ErrNoRows) {
			return events.ErrNotFound
		} else if err != nil {
			return fmt.Errorf("lock event: %w", err)
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
				image_url=CASE WHEN $18 THEN $19::text ELSE image_url END,
				updated_at=now()
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
			patch.ImageURL.Set, nullableString(patch.ImageURL),
		)
		if err != nil {
			return mapWriteError(err)
		}
		result, err = r.getByID(txCtx, id)
		return err
	})
	return result, err
}

func (r *Repository) Transition(ctx context.Context, id int64, action string) (events.Event, error) {
	var result events.Event
	err := r.transactions.WithinTransaction(ctx, func(txCtx context.Context) error {
		db := platformpostgres.Executor(txCtx, r.pool)
		var current string
		if err := db.QueryRow(txCtx, `SELECT status FROM events WHERE id=$1 FOR UPDATE`, id).Scan(&current); errors.Is(err, pgx.ErrNoRows) {
			return events.ErrNotFound
		} else if err != nil {
			return fmt.Errorf("lock event status: %w", err)
		}
		target := ""
		switch {
		case current == events.StatusPending && action == "approve":
			target = events.StatusActive
		case current == events.StatusPending && action == "reject":
			target = events.StatusRejected
		case current == events.StatusActive && action == "block":
			target = events.StatusBlocked
		default:
			return events.ErrInvalidStatusTransition
		}
		if _, err := db.Exec(txCtx, `UPDATE events SET status=$2,updated_at=now() WHERE id=$1`, id, target); err != nil {
			return fmt.Errorf("transition event: %w", err)
		}
		var err error
		result, err = r.getByID(txCtx, id)
		return err
	})
	return result, err
}

func (r *Repository) CompleteDue(ctx context.Context, now time.Time, limit int64) (int64, error) {
	rows, err := platformpostgres.Executor(ctx, r.pool).Query(ctx, `
		WITH due AS (
			SELECT id
			FROM events
			WHERE status = 'active'
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
		return 0, fmt.Errorf("complete due events: %w", err)
	}
	defer rows.Close()
	var count int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return 0, fmt.Errorf("scan completed event: %w", err)
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate completed events: %w", err)
	}
	return count, nil
}

func (r *Repository) getByID(ctx context.Context, id int64) (events.Event, error) {
	return scanEvent(platformpostgres.Executor(ctx, r.pool).QueryRow(ctx, `SELECT `+eventColumns+eventFrom+` WHERE e.id=$1`, id))
}

type rowScanner interface{ Scan(...any) error }

func scanEvent(row rowScanner) (events.Event, error) {
	var item events.Event
	err := row.Scan(&item.ID, &item.Title, &item.Description, &item.CategoryID, &item.CityID,
		&item.StartsAt, &item.EndsAt, &item.LocationName, &item.Address, &item.ImageURL, &item.Status,
		&item.ParticipantsCount, &item.CompaniesCount, &item.Creator.ID, &item.Creator.FirstName,
		&item.Creator.LastName, &item.Creator.AvatarURL, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return events.Event{}, events.ErrNotFound
	}
	if err != nil {
		return events.Event{}, fmt.Errorf("scan event: %w", err)
	}
	return item, nil
}

func scanMyEvent(row rowScanner) (events.MyEvent, error) {
	var item events.MyEvent
	err := row.Scan(&item.ID, &item.Title, &item.Description, &item.CategoryID, &item.CityID,
		&item.StartsAt, &item.EndsAt, &item.LocationName, &item.Address, &item.ImageURL, &item.Status,
		&item.ParticipantsCount, &item.CompaniesCount, &item.Creator.ID, &item.Creator.FirstName,
		&item.Creator.LastName, &item.Creator.AvatarURL, &item.CreatedAt, &item.UpdatedAt, &item.Relation)
	if err != nil {
		return events.MyEvent{}, fmt.Errorf("scan my event: %w", err)
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
	conditions := []string{`e.status='active'`, `((e.ends_at IS NOT NULL AND e.ends_at>$1) OR (e.ends_at IS NULL AND e.starts_at>$1))`}
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
	return " WHERE " + strings.Join(conditions, " AND "), args
}

func adminWhere(filter events.AdminFilter) (string, []any) {
	conditions := make([]string, 0, 2)
	args := make([]any, 0, 2)
	if filter.Search != "" {
		args = append(args, filter.Search)
		conditions = append(conditions, `(e.title ILIKE '%'||$1||'%' OR COALESCE(e.description,'') ILIKE '%'||$1||'%')`)
	}
	if filter.Status != "" {
		args = append(args, filter.Status)
		conditions = append(conditions, `e.status=$`+fmt.Sprint(len(args)))
	}
	if len(conditions) == 0 {
		return "", args
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
