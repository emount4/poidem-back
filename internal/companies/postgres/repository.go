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
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const companyColumns = `
	c.id, c.event_id, c.name, c.description, c.max_members, c.join_type, c.rules,
	u.id, u.first_name, u.last_name, u.avatar_url,
	(SELECT count(*) FROM company_members members WHERE members.company_id = c.id),
	c.status, c.created_at, c.updated_at`

const companyFrom = `
	FROM companies c
	JOIN users u ON u.id = c.owner_id
	JOIN events visible_event ON visible_event.id = c.event_id AND visible_event.deleted_at IS NULL `

const applicationSelect = `
	SELECT a.id, a.company_id, u.id, u.first_name, u.last_name, u.avatar_url,
		a.message, a.status, a.resolution_reason, a.created_at, a.resolved_at
	FROM applications a
	JOIN users u ON u.id = a.user_id`

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
			WHERE id = $1 AND deleted_at IS NULL
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
			WHERE e.id = $1 AND e.deleted_at IS NULL AND (
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

func (r *Repository) ListAdmin(ctx context.Context, page companies.Page) ([]companies.Company, int64, error) {
	items, err := r.queryCompanies(ctx, `SELECT `+companyColumns+companyFrom+`
		WHERE c.deleted_at IS NULL
		ORDER BY c.created_at DESC, c.id DESC
		LIMIT $1 OFFSET $2`, page.Limit, page.Offset)
	if err != nil {
		return nil, 0, err
	}
	var total int64
	if err := platformpostgres.Executor(ctx, r.pool).QueryRow(ctx,
		`SELECT count(*) FROM companies WHERE deleted_at IS NULL`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count admin companies: %w", err)
	}
	return items, total, nil
}

func (r *Repository) Block(ctx context.Context, companyID int64, now time.Time) (companies.Company, error) {
	var result companies.Company
	err := r.transactions.WithinTransaction(ctx, func(txCtx context.Context) error {
		db := platformpostgres.Executor(txCtx, r.pool)
		var status string
		err := db.QueryRow(txCtx, `
			SELECT status FROM companies
			WHERE id = $1 AND deleted_at IS NULL
			FOR UPDATE
		`, companyID).Scan(&status)
		if errors.Is(err, pgx.ErrNoRows) {
			return companies.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock company for blocking: %w", err)
		}
		if status == companies.StatusBlocked {
			return companies.ErrCompanyBlocked
		}
		if _, err := db.Exec(txCtx, `
			UPDATE companies SET status = $2, updated_at = $3 WHERE id = $1
		`, companyID, companies.StatusBlocked, now); err != nil {
			return fmt.Errorf("block company: %w", err)
		}
		if _, err := db.Exec(txCtx, `
			UPDATE applications
			SET status = $2, resolved_at = $3, resolution_reason = 'COMPANY_BLOCKED'
			WHERE company_id = $1 AND status = 'pending'
		`, companyID, companies.ApplicationStatusCancelled, now); err != nil {
			return fmt.Errorf("cancel blocked company applications: %w", err)
		}
		result, err = r.getByID(txCtx, companyID)
		return err
	})
	return result, err
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

func (r *Repository) JoinOpen(ctx context.Context, companyID, userID int64, now time.Time) error {
	return r.transactions.WithinTransaction(ctx, func(txCtx context.Context) error {
		db := platformpostgres.Executor(txCtx, r.pool)
		var eventID int64
		err := db.QueryRow(txCtx, `
			SELECT event_id FROM companies
			WHERE id = $1 AND deleted_at IS NULL
		`, companyID).Scan(&eventID)
		if errors.Is(err, pgx.ErrNoRows) {
			return companies.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("find company event: %w", err)
		}

		var eventStatus string
		var startsAt time.Time
		var endsAt *time.Time
		err = db.QueryRow(txCtx, `
			SELECT status, starts_at, ends_at FROM events
			WHERE id = $1 AND deleted_at IS NULL
			FOR UPDATE
		`, eventID).Scan(&eventStatus, &startsAt, &endsAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return companies.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock company event for join: %w", err)
		}

		var companyStatus, joinType string
		var maxMembers int64
		err = db.QueryRow(txCtx, `
			SELECT status, join_type, max_members FROM companies
			WHERE id = $1 AND event_id = $2 AND deleted_at IS NULL
			FOR UPDATE
		`, companyID, eventID).Scan(&companyStatus, &joinType, &maxMembers)
		if errors.Is(err, pgx.ErrNoRows) {
			return companies.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock company for open join: %w", err)
		}
		if companyStatus != companies.StatusActive || joinType != companies.JoinTypeOpen {
			return companies.ErrCompanyClosed
		}
		if !eventAvailable(eventStatus, startsAt, endsAt, now) {
			return companies.ErrEventNotAvailable
		}

		var marker int
		err = db.QueryRow(txCtx, `
			SELECT 1 FROM company_members
			WHERE company_id = $1 AND user_id = $2
		`, companyID, userID).Scan(&marker)
		if err == nil {
			return companies.ErrAlreadyCompanyMember
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("check target company membership: %w", err)
		}

		err = db.QueryRow(txCtx, `
			SELECT 1
			FROM company_members cm
			JOIN companies c ON c.id = cm.company_id
			WHERE cm.user_id = $1 AND c.event_id = $2 AND c.deleted_at IS NULL
			LIMIT 1
		`, userID, eventID).Scan(&marker)
		if err == nil {
			return companies.ErrAlreadyInEventCompany
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("check event company membership: %w", err)
		}

		var membersCount int64
		if err := db.QueryRow(txCtx, `SELECT count(*) FROM company_members WHERE company_id = $1`, companyID).Scan(&membersCount); err != nil {
			return fmt.Errorf("count company capacity: %w", err)
		}
		if membersCount >= maxMembers {
			return companies.ErrCompanyFull
		}

		var participationType string
		participationExists := true
		err = db.QueryRow(txCtx, `
			SELECT participation_type FROM event_participants
			WHERE event_id = $1 AND user_id = $2
			FOR UPDATE
		`, eventID, userID).Scan(&participationType)
		if errors.Is(err, pgx.ErrNoRows) {
			participationExists = false
		} else if err != nil {
			return fmt.Errorf("lock participation for company join: %w", err)
		} else if participationType == "company" {
			return companies.ErrAlreadyInEventCompany
		}

		if _, err := db.Exec(txCtx, `
			INSERT INTO company_members (company_id, user_id, role, joined_at)
			VALUES ($1,$2,$3,$4)
		`, companyID, userID, companies.RoleMember, now); err != nil {
			return fmt.Errorf("insert company member: %w", err)
		}
		if participationExists {
			if _, err := db.Exec(txCtx, `
				UPDATE event_participants
				SET participation_type = 'company', company_id = $3, joined_at = $4
				WHERE event_id = $1 AND user_id = $2
			`, eventID, userID, companyID, now); err != nil {
				return fmt.Errorf("convert solo participation to company: %w", err)
			}
		} else if _, err := db.Exec(txCtx, `
			INSERT INTO event_participants (
				event_id, user_id, participation_type, company_id, joined_at
			) VALUES ($1,$2,'company',$3,$4)
		`, eventID, userID, companyID, now); err != nil {
			return fmt.Errorf("insert company participation: %w", err)
		}
		return nil
	})
}

func (r *Repository) Leave(ctx context.Context, companyID, userID int64) error {
	return r.transactions.WithinTransaction(ctx, func(txCtx context.Context) error {
		db := platformpostgres.Executor(txCtx, r.pool)
		var eventID int64
		err := db.QueryRow(txCtx, `
			SELECT event_id FROM companies
			WHERE id = $1 AND deleted_at IS NULL
		`, companyID).Scan(&eventID)
		if errors.Is(err, pgx.ErrNoRows) {
			return companies.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("find company event for leave: %w", err)
		}

		var lockedEventID int64
		err = db.QueryRow(txCtx, `SELECT id FROM events WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, eventID).Scan(&lockedEventID)
		if errors.Is(err, pgx.ErrNoRows) {
			return companies.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock company event for leave: %w", err)
		}

		var ownerID int64
		err = db.QueryRow(txCtx, `
			SELECT owner_id FROM companies
			WHERE id = $1 AND event_id = $2 AND deleted_at IS NULL
			FOR UPDATE
		`, companyID, eventID).Scan(&ownerID)
		if errors.Is(err, pgx.ErrNoRows) {
			return companies.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock company for leave: %w", err)
		}
		if ownerID == userID {
			return companies.ErrOwnerCannotLeave
		}

		var deletedUserID int64
		err = db.QueryRow(txCtx, `
			DELETE FROM company_members
			WHERE company_id = $1 AND user_id = $2
			RETURNING user_id
		`, companyID, userID).Scan(&deletedUserID)
		if errors.Is(err, pgx.ErrNoRows) {
			return companies.ErrNotCompanyMember
		}
		if err != nil {
			return fmt.Errorf("remove company membership on leave: %w", err)
		}
		if _, err := db.Exec(txCtx, `
			DELETE FROM event_participants
			WHERE event_id = $1 AND user_id = $2 AND company_id = $3
		`, eventID, userID, companyID); err != nil {
			return fmt.Errorf("remove company participation on leave: %w", err)
		}
		return nil
	})
}

func (r *Repository) RemoveMember(ctx context.Context, companyID, ownerID, userID int64) error {
	return r.transactions.WithinTransaction(ctx, func(txCtx context.Context) error {
		db := platformpostgres.Executor(txCtx, r.pool)
		var eventID int64
		err := db.QueryRow(txCtx, `
			SELECT event_id FROM companies
			WHERE id = $1 AND deleted_at IS NULL
		`, companyID).Scan(&eventID)
		if errors.Is(err, pgx.ErrNoRows) {
			return companies.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("find company event for member removal: %w", err)
		}

		var lockedEventID int64
		err = db.QueryRow(txCtx, `SELECT id FROM events WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, eventID).Scan(&lockedEventID)
		if errors.Is(err, pgx.ErrNoRows) {
			return companies.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock company event for member removal: %w", err)
		}

		var actualOwnerID int64
		err = db.QueryRow(txCtx, `
			SELECT owner_id FROM companies
			WHERE id = $1 AND event_id = $2 AND deleted_at IS NULL
			FOR UPDATE
		`, companyID, eventID).Scan(&actualOwnerID)
		if errors.Is(err, pgx.ErrNoRows) {
			return companies.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock company for member removal: %w", err)
		}
		if actualOwnerID != ownerID {
			return companies.ErrNotOwner
		}
		if actualOwnerID == userID {
			return companies.ErrOwnerCannotBeRemoved
		}

		var userExists bool
		if err := db.QueryRow(txCtx, `SELECT EXISTS (SELECT 1 FROM users WHERE id = $1)`, userID).Scan(&userExists); err != nil {
			return fmt.Errorf("check removed user: %w", err)
		}
		if !userExists {
			return companies.ErrUserNotFound
		}

		var deletedUserID int64
		err = db.QueryRow(txCtx, `
			DELETE FROM company_members
			WHERE company_id = $1 AND user_id = $2
			RETURNING user_id
		`, companyID, userID).Scan(&deletedUserID)
		if errors.Is(err, pgx.ErrNoRows) {
			return companies.ErrNotCompanyMember
		}
		if err != nil {
			return fmt.Errorf("remove company member: %w", err)
		}
		if _, err := db.Exec(txCtx, `
			DELETE FROM event_participants
			WHERE event_id = $1 AND user_id = $2 AND company_id = $3
		`, eventID, userID, companyID); err != nil {
			return fmt.Errorf("remove company participation after kick: %w", err)
		}
		return nil
	})
}

func (r *Repository) CreateApplication(ctx context.Context, companyID, userID int64, input companies.CreateApplicationInput, now time.Time) (companies.Application, error) {
	var result companies.Application
	err := r.transactions.WithinTransaction(ctx, func(txCtx context.Context) error {
		db := platformpostgres.Executor(txCtx, r.pool)
		var eventID int64
		err := db.QueryRow(txCtx, `
			SELECT event_id FROM companies
			WHERE id = $1 AND deleted_at IS NULL
		`, companyID).Scan(&eventID)
		if errors.Is(err, pgx.ErrNoRows) {
			return companies.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("find application company event: %w", err)
		}

		var eventStatus string
		var startsAt time.Time
		var endsAt *time.Time
		err = db.QueryRow(txCtx, `
			SELECT status, starts_at, ends_at FROM events
			WHERE id = $1 AND deleted_at IS NULL
			FOR UPDATE
		`, eventID).Scan(&eventStatus, &startsAt, &endsAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return companies.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock application event: %w", err)
		}

		var companyStatus, joinType string
		err = db.QueryRow(txCtx, `
			SELECT status, join_type FROM companies
			WHERE id = $1 AND event_id = $2 AND deleted_at IS NULL
			FOR UPDATE
		`, companyID, eventID).Scan(&companyStatus, &joinType)
		if errors.Is(err, pgx.ErrNoRows) {
			return companies.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock application company: %w", err)
		}
		if companyStatus != companies.StatusActive || joinType != companies.JoinTypeRequest {
			return companies.ErrCompanyClosed
		}
		if !eventAvailable(eventStatus, startsAt, endsAt, now) {
			return companies.ErrEventNotAvailable
		}

		var marker int
		err = db.QueryRow(txCtx, `SELECT 1 FROM company_members WHERE company_id = $1 AND user_id = $2`, companyID, userID).Scan(&marker)
		if err == nil {
			return companies.ErrAlreadyCompanyMember
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("check application company membership: %w", err)
		}
		err = db.QueryRow(txCtx, `
			SELECT 1
			FROM company_members cm
			JOIN companies c ON c.id = cm.company_id
			WHERE cm.user_id = $1 AND c.event_id = $2 AND c.deleted_at IS NULL
			LIMIT 1
		`, userID, eventID).Scan(&marker)
		if err == nil {
			return companies.ErrAlreadyInEventCompany
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("check application event membership: %w", err)
		}

		err = db.QueryRow(txCtx, `
			SELECT 1 FROM applications
			WHERE company_id = $1 AND user_id = $2 AND status = 'pending'
		`, companyID, userID).Scan(&marker)
		if err == nil {
			return companies.ErrApplicationAlreadyExists
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("check pending application: %w", err)
		}

		var applicationID int64
		err = db.QueryRow(txCtx, `
			INSERT INTO applications (company_id, user_id, message, status, created_at)
			VALUES ($1,$2,$3,$4,$5)
			RETURNING id
		`, companyID, userID, input.Message, companies.ApplicationStatusPending, now).Scan(&applicationID)
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.ConstraintName == "uq_applications_pending_company_user" {
				return companies.ErrApplicationAlreadyExists
			}
			return fmt.Errorf("insert company application: %w", err)
		}
		result, err = r.getApplicationByID(txCtx, applicationID)
		return err
	})
	return result, err
}

func (r *Repository) GetMyApplication(ctx context.Context, companyID, userID int64) (companies.Application, error) {
	db := platformpostgres.Executor(ctx, r.pool)
	var companyExists bool
	if err := db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM companies WHERE id = $1 AND deleted_at IS NULL
		)
	`, companyID).Scan(&companyExists); err != nil {
		return companies.Application{}, fmt.Errorf("check application company: %w", err)
	}
	if !companyExists {
		return companies.Application{}, companies.ErrNotFound
	}

	var applicationID int64
	err := db.QueryRow(ctx, `
		SELECT id
		FROM applications
		WHERE company_id = $1 AND user_id = $2
		ORDER BY (status = 'pending') DESC, created_at DESC, id DESC
		LIMIT 1
	`, companyID, userID).Scan(&applicationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return companies.Application{}, companies.ErrApplicationNotFound
	}
	if err != nil {
		return companies.Application{}, fmt.Errorf("find my company application: %w", err)
	}
	return r.getApplicationByID(ctx, applicationID)
}

func (r *Repository) ListApplications(ctx context.Context, companyID, ownerID int64, status string, page companies.Page) ([]companies.Application, int64, error) {
	db := platformpostgres.Executor(ctx, r.pool)
	var actualOwnerID int64
	err := db.QueryRow(ctx, `
		SELECT owner_id FROM companies WHERE id = $1 AND deleted_at IS NULL
	`, companyID).Scan(&actualOwnerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, 0, companies.ErrNotFound
	}
	if err != nil {
		return nil, 0, fmt.Errorf("check application list company: %w", err)
	}
	if actualOwnerID != ownerID {
		return nil, 0, companies.ErrNotOwner
	}

	where := ` WHERE a.company_id = $1`
	args := []any{companyID}
	if status != "" {
		where += ` AND a.status = $2`
		args = append(args, status)
	}
	items, err := r.queryApplications(ctx, applicationSelect+where+`
		ORDER BY CASE a.status WHEN 'pending' THEN 0 ELSE 1 END, a.created_at DESC, a.id DESC
		LIMIT $`+fmt.Sprint(len(args)+1)+` OFFSET $`+fmt.Sprint(len(args)+2), append(args, page.Limit, page.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	var total int64
	if err := db.QueryRow(ctx, `SELECT count(*) FROM applications a`+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count company applications: %w", err)
	}
	return items, total, nil
}

func (r *Repository) ListMyApplications(ctx context.Context, userID int64, page companies.Page) ([]companies.Application, int64, error) {
	items, err := r.queryApplications(ctx, applicationSelect+`
		WHERE a.user_id = $1
		ORDER BY a.created_at DESC, a.id DESC
		LIMIT $2 OFFSET $3`, userID, page.Limit, page.Offset)
	if err != nil {
		return nil, 0, err
	}
	var total int64
	if err := platformpostgres.Executor(ctx, r.pool).QueryRow(ctx,
		`SELECT count(*) FROM applications WHERE user_id = $1`, userID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count my applications: %w", err)
	}
	return items, total, nil
}

func (r *Repository) CancelApplication(ctx context.Context, companyID, userID int64, now time.Time) error {
	return r.transactions.WithinTransaction(ctx, func(txCtx context.Context) error {
		db := platformpostgres.Executor(txCtx, r.pool)
		var lockedCompanyID int64
		err := db.QueryRow(txCtx, `
			SELECT id FROM companies
			WHERE id = $1 AND deleted_at IS NULL
			FOR UPDATE
		`, companyID).Scan(&lockedCompanyID)
		if errors.Is(err, pgx.ErrNoRows) {
			return companies.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock company for application cancellation: %w", err)
		}

		var applicationID int64
		var status string
		err = db.QueryRow(txCtx, `
			SELECT id, status
			FROM applications
			WHERE company_id = $1 AND user_id = $2
			ORDER BY (status = 'pending') DESC, created_at DESC, id DESC
			LIMIT 1
			FOR UPDATE
		`, companyID, userID).Scan(&applicationID, &status)
		if errors.Is(err, pgx.ErrNoRows) {
			return companies.ErrApplicationNotFound
		}
		if err != nil {
			return fmt.Errorf("lock application for cancellation: %w", err)
		}
		if status != companies.ApplicationStatusPending {
			return companies.ErrApplicationAlreadyResolved
		}
		if _, err := db.Exec(txCtx, `
			UPDATE applications
			SET status = $2, resolved_at = $3, resolution_reason = NULL
			WHERE id = $1
		`, applicationID, companies.ApplicationStatusCancelled, now); err != nil {
			return fmt.Errorf("cancel application: %w", err)
		}
		return nil
	})
}

func (r *Repository) ResolveApplication(ctx context.Context, companyID, applicationID, ownerID int64, action string, now time.Time) (companies.Application, error) {
	var result companies.Application
	err := r.transactions.WithinTransaction(ctx, func(txCtx context.Context) error {
		db := platformpostgres.Executor(txCtx, r.pool)
		var eventID int64
		err := db.QueryRow(txCtx, `
			SELECT event_id FROM companies WHERE id = $1 AND deleted_at IS NULL
		`, companyID).Scan(&eventID)
		if errors.Is(err, pgx.ErrNoRows) {
			return companies.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("find resolution company event: %w", err)
		}

		var eventStatus string
		var startsAt time.Time
		var endsAt *time.Time
		err = db.QueryRow(txCtx, `
			SELECT status, starts_at, ends_at FROM events WHERE id = $1 AND deleted_at IS NULL FOR UPDATE
		`, eventID).Scan(&eventStatus, &startsAt, &endsAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return companies.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock resolution event: %w", err)
		}

		var actualOwnerID int64
		var companyStatus string
		var maxMembers int64
		err = db.QueryRow(txCtx, `
			SELECT owner_id, status, max_members FROM companies
			WHERE id = $1 AND event_id = $2 AND deleted_at IS NULL
			FOR UPDATE
		`, companyID, eventID).Scan(&actualOwnerID, &companyStatus, &maxMembers)
		if errors.Is(err, pgx.ErrNoRows) {
			return companies.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock resolution company: %w", err)
		}
		if actualOwnerID != ownerID {
			return companies.ErrNotOwner
		}

		var applicantID int64
		var applicationStatus string
		err = db.QueryRow(txCtx, `
			SELECT user_id, status FROM applications
			WHERE id = $1 AND company_id = $2
			FOR UPDATE
		`, applicationID, companyID).Scan(&applicantID, &applicationStatus)
		if errors.Is(err, pgx.ErrNoRows) {
			return companies.ErrApplicationNotFound
		}
		if err != nil {
			return fmt.Errorf("lock application for resolution: %w", err)
		}
		if applicationStatus != companies.ApplicationStatusPending {
			return companies.ErrApplicationAlreadyResolved
		}
		var applicantStatus string
		if err := db.QueryRow(txCtx, `SELECT status FROM users WHERE id = $1 FOR UPDATE`, applicantID).Scan(&applicantStatus); errors.Is(err, pgx.ErrNoRows) {
			return companies.ErrUserNotFound
		} else if err != nil {
			return fmt.Errorf("lock application user: %w", err)
		}
		if applicantStatus != "active" {
			return companies.ErrUserBanned
		}

		if action == "reject" {
			if _, err := db.Exec(txCtx, `
				UPDATE applications SET status = $2, resolved_at = $3, resolution_reason = NULL
				WHERE id = $1
			`, applicationID, companies.ApplicationStatusRejected, now); err != nil {
				return fmt.Errorf("reject application: %w", err)
			}
			result, err = r.getApplicationByID(txCtx, applicationID)
			return err
		}

		if companyStatus == companies.StatusBlocked {
			return companies.ErrCompanyBlocked
		}
		if !eventAvailable(eventStatus, startsAt, endsAt, now) {
			return companies.ErrEventNotAvailable
		}
		var marker int
		err = db.QueryRow(txCtx, `SELECT 1 FROM company_members WHERE company_id = $1 AND user_id = $2`, companyID, applicantID).Scan(&marker)
		if err == nil {
			return companies.ErrAlreadyCompanyMember
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("check approved company membership: %w", err)
		}
		err = db.QueryRow(txCtx, `
			SELECT 1 FROM company_members cm
			JOIN companies c ON c.id = cm.company_id
			WHERE cm.user_id = $1 AND c.event_id = $2 AND c.deleted_at IS NULL
			LIMIT 1
		`, applicantID, eventID).Scan(&marker)
		if err == nil {
			return companies.ErrAlreadyInEventCompany
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("check approved event membership: %w", err)
		}
		var membersCount int64
		if err := db.QueryRow(txCtx, `SELECT count(*) FROM company_members WHERE company_id = $1`, companyID).Scan(&membersCount); err != nil {
			return fmt.Errorf("count approved company capacity: %w", err)
		}
		if membersCount >= maxMembers {
			return companies.ErrCompanyFull
		}

		var participationType string
		participationExists := true
		err = db.QueryRow(txCtx, `
			SELECT participation_type FROM event_participants
			WHERE event_id = $1 AND user_id = $2 FOR UPDATE
		`, eventID, applicantID).Scan(&participationType)
		if errors.Is(err, pgx.ErrNoRows) {
			participationExists = false
		} else if err != nil {
			return fmt.Errorf("lock approved participation: %w", err)
		} else if participationType == "company" {
			return companies.ErrAlreadyInEventCompany
		}

		if _, err := db.Exec(txCtx, `
			INSERT INTO company_members (company_id, user_id, role, joined_at)
			VALUES ($1,$2,$3,$4)
		`, companyID, applicantID, companies.RoleMember, now); err != nil {
			return fmt.Errorf("insert approved member: %w", err)
		}
		if participationExists {
			if _, err := db.Exec(txCtx, `
				UPDATE event_participants
				SET participation_type = 'company', company_id = $3, joined_at = $4
				WHERE event_id = $1 AND user_id = $2
			`, eventID, applicantID, companyID, now); err != nil {
				return fmt.Errorf("convert approved solo participation: %w", err)
			}
		} else if _, err := db.Exec(txCtx, `
			INSERT INTO event_participants (event_id, user_id, participation_type, company_id, joined_at)
			VALUES ($1,$2,'company',$3,$4)
		`, eventID, applicantID, companyID, now); err != nil {
			return fmt.Errorf("insert approved participation: %w", err)
		}
		if _, err := db.Exec(txCtx, `
			UPDATE applications SET status = $2, resolved_at = $3, resolution_reason = NULL
			WHERE id = $1
		`, applicationID, companies.ApplicationStatusApproved, now); err != nil {
			return fmt.Errorf("approve application: %w", err)
		}
		result, err = r.getApplicationByID(txCtx, applicationID)
		return err
	})
	return result, err
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

func (r *Repository) getApplicationByID(ctx context.Context, applicationID int64) (companies.Application, error) {
	return scanApplication(platformpostgres.Executor(ctx, r.pool).QueryRow(ctx,
		applicationSelect+` WHERE a.id = $1`, applicationID))
}

func (r *Repository) queryApplications(ctx context.Context, query string, args ...any) ([]companies.Application, error) {
	rows, err := platformpostgres.Executor(ctx, r.pool).Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query applications: %w", err)
	}
	defer rows.Close()
	items := make([]companies.Application, 0)
	for rows.Next() {
		item, err := scanApplication(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate applications: %w", err)
	}
	return items, nil
}

func scanApplication(row rowScanner) (companies.Application, error) {
	var item companies.Application
	err := row.Scan(&item.ID, &item.CompanyID, &item.User.ID, &item.User.FirstName,
		&item.User.LastName, &item.User.AvatarURL, &item.Message, &item.Status,
		&item.ResolutionReason, &item.CreatedAt, &item.ResolvedAt)
	if err != nil {
		return companies.Application{}, fmt.Errorf("scan company application: %w", err)
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
		JOIN events e ON e.id = c.event_id AND e.deleted_at IS NULL
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
