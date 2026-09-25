// Package postgres implements company persistence using PostgreSQL.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/emount4/poidem-back/internal/companies"
	platformpostgres "github.com/emount4/poidem-back/internal/platform/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const companyColumns = `
	c.id, c.event_id, c.name, c.description, c.max_members, c.join_type, c.rules,
	u.id, u.first_name, u.last_name, u.avatar_url,
	(SELECT count(*) FROM company_members members WHERE members.company_id = c.id),
	c.status, c.created_at, c.updated_at`

const companyFrom = ` FROM companies c JOIN users u ON u.id = c.owner_id `

type Repository struct {
	pool         *pgxpool.Pool
	transactions *platformpostgres.TxManager
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool, transactions: platformpostgres.NewTxManager(pool)}
}

func (r *Repository) Create(ctx context.Context, eventID, ownerID int64, input companies.CreateInput, now time.Time) (companies.Company, error) {
	var result companies.Company
	err := r.transactions.WithinTransaction(ctx, func(txCtx context.Context) error {
		db := platformpostgres.Executor(txCtx, r.pool)
		var status string
		var startsAt time.Time
		var endsAt *time.Time
		err := db.QueryRow(txCtx, `
			SELECT status, starts_at, ends_at
			FROM events
			WHERE id = $1
			FOR UPDATE
		`, eventID).Scan(&status, &startsAt, &endsAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return companies.ErrEventNotFound
		}
		if err != nil {
			return fmt.Errorf("lock company event: %w", err)
		}
		if !eventAvailable(status, startsAt, endsAt, now) {
			return companies.ErrEventNotAvailable
		}

		var existingCompanyID int64
		err = db.QueryRow(txCtx, `
			SELECT c.id
			FROM company_members cm
			JOIN companies c ON c.id = cm.company_id
			WHERE cm.user_id = $1 AND c.event_id = $2 AND c.deleted_at IS NULL
			LIMIT 1
			FOR UPDATE OF c
		`, ownerID, eventID).Scan(&existingCompanyID)
		if err == nil {
			return companies.ErrAlreadyInEventCompany
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("check existing event company: %w", err)
		}

		var participationType string
		var participationCompanyID *int64
		participationExists := true
		err = db.QueryRow(txCtx, `
			SELECT participation_type, company_id
			FROM event_participants
			WHERE event_id = $1 AND user_id = $2
			FOR UPDATE
		`, eventID, ownerID).Scan(&participationType, &participationCompanyID)
		if errors.Is(err, pgx.ErrNoRows) {
			participationExists = false
		} else if err != nil {
			return fmt.Errorf("lock owner participation: %w", err)
		} else if participationType == "company" || participationCompanyID != nil {
			return companies.ErrAlreadyInEventCompany
		}

		var companyID int64
		if err := db.QueryRow(txCtx, `
			INSERT INTO companies (
				event_id, owner_id, name, description, max_members, join_type,
				rules, status, created_at, updated_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$9)
			RETURNING id
		`, eventID, ownerID, input.Name, input.Description, input.MaxMembers,
			input.JoinType, input.Rules, companies.StatusActive, now).Scan(&companyID); err != nil {
			return fmt.Errorf("insert company: %w", err)
		}
		if _, err := db.Exec(txCtx, `
			INSERT INTO company_members (company_id, user_id, role, joined_at)
			VALUES ($1,$2,$3,$4)
		`, companyID, ownerID, companies.RoleOwner, now); err != nil {
			return fmt.Errorf("insert company owner: %w", err)
		}
		if participationExists {
			if _, err := db.Exec(txCtx, `
				UPDATE event_participants
				SET participation_type = 'company', company_id = $3, joined_at = $4
				WHERE event_id = $1 AND user_id = $2
			`, eventID, ownerID, companyID, now); err != nil {
				return fmt.Errorf("convert owner participation: %w", err)
			}
		} else if _, err := db.Exec(txCtx, `
			INSERT INTO event_participants (
				event_id, user_id, participation_type, company_id, joined_at
			) VALUES ($1,$2,'company',$3,$4)
		`, eventID, ownerID, companyID, now); err != nil {
			return fmt.Errorf("insert owner participation: %w", err)
		}
		result, err = r.getByID(txCtx, companyID)
		return err
	})
	return result, err
}

func (r *Repository) ListEvent(ctx context.Context, eventID int64, viewer companies.Viewer, page companies.Page, now time.Time) ([]companies.Company, int64, error) {
	db := platformpostgres.Executor(ctx, r.pool)
	var visible bool
	err := db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM events e
			WHERE e.id = $1 AND (
				(e.status = 'active' AND (
					(e.ends_at IS NOT NULL AND e.ends_at > $3)
					OR (e.ends_at IS NULL AND e.starts_at > $3)
				))
				OR ($2::bigint IS NOT NULL AND e.creator_id = $2 AND e.status IN ('pending','rejected'))
			)
		)
	`, eventID, viewer.UserID, now).Scan(&visible)
	if err != nil {
		return nil, 0, fmt.Errorf("check event company visibility: %w", err)
	}
	if !visible {
		return nil, 0, companies.ErrEventNotFound
	}

	where := ` WHERE c.event_id = $1 AND c.deleted_at IS NULL AND (
		c.status <> 'blocked' OR $3::boolean OR (
			$2::bigint IS NOT NULL AND (
				c.owner_id = $2 OR EXISTS (
					SELECT 1 FROM company_members mine
					WHERE mine.company_id = c.id AND mine.user_id = $2
				)
			)
		)
	)`
	items, err := r.queryCompanies(ctx, `SELECT `+companyColumns+companyFrom+where+`
		ORDER BY c.created_at ASC, c.id ASC LIMIT $4 OFFSET $5`, eventID, viewer.UserID, viewer.Admin, page.Limit, page.Offset)
	if err != nil {
		return nil, 0, err
	}
	var total int64
	if err := db.QueryRow(ctx, `SELECT count(*) FROM companies c `+where, eventID, viewer.UserID, viewer.Admin).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count event companies: %w", err)
	}
	return items, total, nil
}

func (r *Repository) GetVisible(ctx context.Context, companyID int64, viewer companies.Viewer) (companies.Company, error) {
	query := `SELECT ` + companyColumns + companyFrom + `
		WHERE c.id = $1 AND c.deleted_at IS NULL AND (
			c.status <> 'blocked' OR $3::boolean OR (
				$2::bigint IS NOT NULL AND (
					c.owner_id = $2 OR EXISTS (
						SELECT 1 FROM company_members mine
						WHERE mine.company_id = c.id AND mine.user_id = $2
					)
				)
			)
		)`
	return scanCompany(platformpostgres.Executor(ctx, r.pool).QueryRow(ctx, query, companyID, viewer.UserID, viewer.Admin))
}

func (r *Repository) ListMembers(ctx context.Context, companyID int64, viewer companies.Viewer, page companies.Page) ([]companies.UserShort, int64, error) {
	if _, err := r.GetVisible(ctx, companyID, viewer); err != nil {
		return nil, 0, err
	}
	db := platformpostgres.Executor(ctx, r.pool)
	rows, err := db.Query(ctx, `
		SELECT u.id, u.first_name, u.last_name, u.avatar_url
		FROM company_members cm
		JOIN users u ON u.id = cm.user_id
		WHERE cm.company_id = $1
		ORDER BY CASE cm.role WHEN 'owner' THEN 0 ELSE 1 END, cm.joined_at, u.id
		LIMIT $2 OFFSET $3
	`, companyID, page.Limit, page.Offset)
	if err != nil {
		return nil, 0, fmt.Errorf("query company members: %w", err)
	}
	defer rows.Close()
	items := make([]companies.UserShort, 0)
	for rows.Next() {
		var item companies.UserShort
		if err := rows.Scan(&item.ID, &item.FirstName, &item.LastName, &item.AvatarURL); err != nil {
			return nil, 0, fmt.Errorf("scan company member: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate company members: %w", err)
	}
	var total int64
	if err := db.QueryRow(ctx, `SELECT count(*) FROM company_members WHERE company_id = $1`, companyID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count company members: %w", err)
	}
	return items, total, nil
}

func (r *Repository) ListMine(ctx context.Context, userID int64, page companies.Page) ([]companies.Company, int64, error) {
	where := ` WHERE c.deleted_at IS NULL AND EXISTS (
		SELECT 1 FROM company_members mine WHERE mine.company_id = c.id AND mine.user_id = $1
	)`
	items, err := r.queryCompanies(ctx, `SELECT `+companyColumns+companyFrom+where+`
		ORDER BY c.created_at DESC, c.id DESC LIMIT $2 OFFSET $3`, userID, page.Limit, page.Offset)
	if err != nil {
		return nil, 0, err
	}
	var total int64
	if err := platformpostgres.Executor(ctx, r.pool).QueryRow(ctx, `SELECT count(*) FROM companies c `+where, userID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count my companies: %w", err)
	}
	return items, total, nil
}

func (r *Repository) Update(ctx context.Context, companyID, ownerID int64, patch companies.Patch, now time.Time) (companies.Company, error) {
	var result companies.Company
	err := r.transactions.WithinTransaction(ctx, func(txCtx context.Context) error {
		locked, err := r.lockCompany(txCtx, companyID)
		if err != nil {
			return err
		}
		if locked.OwnerID != ownerID {
			return companies.ErrNotOwner
		}
		if locked.CompanyStatus == companies.StatusBlocked {
			return companies.ErrCompanyBlocked
		}
		if !eventAvailable(locked.EventStatus, locked.StartsAt, locked.EndsAt, now) {
			return companies.ErrEventNotAvailable
		}
		if patch.MaxMembers.Set && int64(patch.MaxMembers.Value) < locked.MembersCount {
			return companies.ErrCapacityBelowMembers
		}
		db := platformpostgres.Executor(txCtx, r.pool)
		if _, err := db.Exec(txCtx, `
			UPDATE companies SET
				name = CASE WHEN $2 THEN $3::varchar ELSE name END,
				description = CASE WHEN $4 THEN $5::text ELSE description END,
				max_members = CASE WHEN $6 THEN $7::integer ELSE max_members END,
				join_type = CASE WHEN $8 THEN $9::varchar ELSE join_type END,
				rules = CASE WHEN $10 THEN $11::text ELSE rules END,
				updated_at = $12
			WHERE id = $1
		`, companyID,
			patch.Name.Set, patch.Name.Value,
			patch.Description.Set, nullableString(patch.Description),
			patch.MaxMembers.Set, patch.MaxMembers.Value,
			patch.JoinType.Set, patch.JoinType.Value,
			patch.Rules.Set, nullableString(patch.Rules), now,
		); err != nil {
			return fmt.Errorf("update company: %w", err)
		}
		result, err = r.getByID(txCtx, companyID)
		return err
	})
	return result, err
}

func (r *Repository) SetRecruitment(ctx context.Context, companyID, ownerID int64, status string, now time.Time) (companies.Company, error) {
	var result companies.Company
	err := r.transactions.WithinTransaction(ctx, func(txCtx context.Context) error {
		locked, err := r.lockCompany(txCtx, companyID)
		if err != nil {
			return err
		}
		if locked.OwnerID != ownerID {
			return companies.ErrNotOwner
		}
		if locked.CompanyStatus == companies.StatusBlocked {
			return companies.ErrCompanyBlocked
		}
		if status == companies.StatusActive && !eventAvailable(locked.EventStatus, locked.StartsAt, locked.EndsAt, now) {
			return companies.ErrEventNotAvailable
		}
		if _, err := platformpostgres.Executor(txCtx, r.pool).Exec(txCtx,
			`UPDATE companies SET status = $2, updated_at = $3 WHERE id = $1`, companyID, status, now); err != nil {
			return fmt.Errorf("set company recruitment: %w", err)
		}
		result, err = r.getByID(txCtx, companyID)
		return err
	})
	return result, err
}

func (r *Repository) Delete(ctx context.Context, companyID, ownerID int64, now time.Time) error {
	return r.transactions.WithinTransaction(ctx, func(txCtx context.Context) error {
		db := platformpostgres.Executor(txCtx, r.pool)
		var actualOwnerID int64
		err := db.QueryRow(txCtx, `
			SELECT owner_id FROM companies
			WHERE id = $1 AND deleted_at IS NULL
			FOR UPDATE
		`, companyID).Scan(&actualOwnerID)
		if errors.Is(err, pgx.ErrNoRows) {
			return companies.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock company for deletion: %w", err)
		}
		if actualOwnerID != ownerID {
			return companies.ErrNotOwner
		}
		if _, err := db.Exec(txCtx, `
			UPDATE companies SET deleted_at = $2, updated_at = $2 WHERE id = $1
		`, companyID, now); err != nil {
			return fmt.Errorf("soft-delete company: %w", err)
		}
		if _, err := db.Exec(txCtx, `
			UPDATE applications
			SET status = 'cancelled', resolved_at = $2
			WHERE company_id = $1 AND status = 'pending'
		`, companyID, now); err != nil {
			return fmt.Errorf("cancel company applications: %w", err)
		}
		if _, err := db.Exec(txCtx, `DELETE FROM event_participants WHERE company_id = $1`, companyID); err != nil {
			return fmt.Errorf("remove company participation: %w", err)
		}
		return nil
	})
}

func (r *Repository) getByID(ctx context.Context, companyID int64) (companies.Company, error) {
	return scanCompany(platformpostgres.Executor(ctx, r.pool).QueryRow(ctx,
		`SELECT `+companyColumns+companyFrom+` WHERE c.id = $1 AND c.deleted_at IS NULL`, companyID))
}

func (r *Repository) queryCompanies(ctx context.Context, query string, args ...any) ([]companies.Company, error) {
	rows, err := platformpostgres.Executor(ctx, r.pool).Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query companies: %w", err)
	}
	defer rows.Close()
	items := make([]companies.Company, 0)
	for rows.Next() {
		item, err := scanCompany(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate companies: %w", err)
	}
	return items, nil
}

type rowScanner interface{ Scan(...any) error }

func scanCompany(row rowScanner) (companies.Company, error) {
	var item companies.Company
	err := row.Scan(&item.ID, &item.EventID, &item.Name, &item.Description,
		&item.MaxMembers, &item.JoinType, &item.Rules, &item.Owner.ID,
		&item.Owner.FirstName, &item.Owner.LastName, &item.Owner.AvatarURL,
		&item.MembersCount, &item.Status, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return companies.Company{}, companies.ErrNotFound
	}
	if err != nil {
		return companies.Company{}, fmt.Errorf("scan company: %w", err)
	}
	return item, nil
}

type lockedCompany struct {
	OwnerID       int64
	CompanyStatus string
	EventStatus   string
	StartsAt      time.Time
	EndsAt        *time.Time
	MembersCount  int64
}

func (r *Repository) lockCompany(ctx context.Context, companyID int64) (lockedCompany, error) {
	var item lockedCompany
	err := platformpostgres.Executor(ctx, r.pool).QueryRow(ctx, `
		SELECT c.owner_id, c.status, e.status, e.starts_at, e.ends_at,
			(SELECT count(*) FROM company_members cm WHERE cm.company_id = c.id)
		FROM companies c
		JOIN events e ON e.id = c.event_id
		WHERE c.id = $1 AND c.deleted_at IS NULL
		FOR UPDATE OF c, e
	`, companyID).Scan(&item.OwnerID, &item.CompanyStatus, &item.EventStatus,
		&item.StartsAt, &item.EndsAt, &item.MembersCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return lockedCompany{}, companies.ErrNotFound
	}
	if err != nil {
		return lockedCompany{}, fmt.Errorf("lock company: %w", err)
	}
	return item, nil
}

func eventAvailable(status string, startsAt time.Time, endsAt *time.Time, now time.Time) bool {
	if status != "active" {
		return false
	}
	if endsAt != nil {
		return endsAt.After(now)
	}
	return startsAt.After(now)
}

func nullableString(change companies.NullableChange[string]) any {
	if change.Null {
		return nil
	}
	return change.Value
}
