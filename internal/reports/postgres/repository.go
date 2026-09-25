// Package postgres implements report persistence using PostgreSQL.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	platformpostgres "github.com/emount4/poidem-back/internal/platform/postgres"
	"github.com/emount4/poidem-back/internal/reports"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const reportSelect = `
	SELECT r.id, r.target_type, r.target_id, r.reason, r.description,
		a.id, a.first_name, a.last_name, a.avatar_url, r.status,
		ru.id, ru.first_name, ru.last_name, ru.avatar_url,
		COALESCE(r.created_at, now()), r.resolved_at
	FROM reports r
	JOIN users a ON a.id = r.author_id
	LEFT JOIN users ru ON ru.id = r.resolved_by`

type Repository struct {
	pool         *pgxpool.Pool
	transactions *platformpostgres.TxManager
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool, transactions: platformpostgres.NewTxManager(pool)}
}

func (r *Repository) Create(ctx context.Context, authorID int64, input reports.CreateInput, now time.Time) (reports.Report, error) {
	db := platformpostgres.Executor(ctx, r.pool)
	exists, err := r.targetExists(ctx, input.TargetType, input.TargetID)
	if err != nil {
		return reports.Report{}, err
	}
	if !exists {
		return reports.Report{}, reports.ErrTargetNotFound
	}
	var id int64
	if err := db.QueryRow(ctx, `
		INSERT INTO reports (
			author_id, target_type, target_id, reason, description, status, created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7)
		RETURNING id
	`, authorID, input.TargetType, input.TargetID, input.Reason, input.Description,
		reports.StatusPending, now).Scan(&id); err != nil {
		return reports.Report{}, fmt.Errorf("insert report: %w", err)
	}
	return r.getByID(ctx, id)
}

func (r *Repository) ListAdmin(ctx context.Context, status string, page reports.Page) ([]reports.Report, int64, error) {
	where := ""
	args := []any{}
	if status != "" {
		where = ` WHERE r.status = $1`
		args = append(args, status)
	}
	items, err := r.query(ctx, reportSelect+where+`
		ORDER BY r.created_at DESC NULLS LAST, r.id DESC
		LIMIT $`+fmt.Sprint(len(args)+1)+` OFFSET $`+fmt.Sprint(len(args)+2),
		append(args, page.Limit, page.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	var total int64
	if err := platformpostgres.Executor(ctx, r.pool).QueryRow(ctx,
		`SELECT count(*) FROM reports r`+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count reports: %w", err)
	}
	return items, total, nil
}

func (r *Repository) GetAdmin(ctx context.Context, reportID int64) (reports.Report, error) {
	return r.getByID(ctx, reportID)
}

func (r *Repository) Resolve(ctx context.Context, reportID, adminID int64, action string, now time.Time) (reports.Report, error) {
	var result reports.Report
	err := r.transactions.WithinTransaction(ctx, func(txCtx context.Context) error {
		db := platformpostgres.Executor(txCtx, r.pool)
		var status string
		err := db.QueryRow(txCtx, `SELECT status FROM reports WHERE id = $1 FOR UPDATE`, reportID).Scan(&status)
		if errors.Is(err, pgx.ErrNoRows) {
			return reports.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock report: %w", err)
		}
		if status != reports.StatusPending {
			return reports.ErrAlreadyResolved
		}
		newStatus := reports.StatusResolved
		if action == "reject" {
			newStatus = reports.StatusRejected
		}
		if _, err := db.Exec(txCtx, `
			UPDATE reports
			SET status = $2, resolved_by = $3, resolved_at = $4
			WHERE id = $1
		`, reportID, newStatus, adminID, now); err != nil {
			return fmt.Errorf("resolve report: %w", err)
		}
		result, err = r.getByID(txCtx, reportID)
		return err
	})
	return result, err
}

func (r *Repository) targetExists(ctx context.Context, targetType string, targetID int64) (bool, error) {
	query := ""
	switch targetType {
	case reports.TargetUser:
		query = `SELECT EXISTS (SELECT 1 FROM users WHERE id = $1)`
	case reports.TargetCompany:
		query = `SELECT EXISTS (SELECT 1 FROM companies WHERE id = $1 AND deleted_at IS NULL)`
	case reports.TargetEvent:
		query = `SELECT EXISTS (SELECT 1 FROM events WHERE id = $1)`
	default:
		return false, reports.ErrTargetNotFound
	}
	var exists bool
	if err := platformpostgres.Executor(ctx, r.pool).QueryRow(ctx, query, targetID).Scan(&exists); err != nil {
		return false, fmt.Errorf("check report target: %w", err)
	}
	return exists, nil
}

func (r *Repository) getByID(ctx context.Context, reportID int64) (reports.Report, error) {
	return scanReport(platformpostgres.Executor(ctx, r.pool).QueryRow(ctx,
		reportSelect+` WHERE r.id = $1`, reportID))
}

func (r *Repository) query(ctx context.Context, query string, args ...any) ([]reports.Report, error) {
	rows, err := platformpostgres.Executor(ctx, r.pool).Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query reports: %w", err)
	}
	defer rows.Close()
	items := make([]reports.Report, 0)
	for rows.Next() {
		item, err := scanReport(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate reports: %w", err)
	}
	return items, nil
}

type rowScanner interface{ Scan(...any) error }

func scanReport(row rowScanner) (reports.Report, error) {
	var item reports.Report
	var resolverID *int64
	var resolverFirstName, resolverLastName, resolverAvatarURL *string
	err := row.Scan(&item.ID, &item.TargetType, &item.TargetID, &item.Reason, &item.Description,
		&item.Author.ID, &item.Author.FirstName, &item.Author.LastName, &item.Author.AvatarURL,
		&item.Status, &resolverID, &resolverFirstName, &resolverLastName, &resolverAvatarURL,
		&item.CreatedAt, &item.ResolvedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return reports.Report{}, reports.ErrNotFound
	}
	if err != nil {
		return reports.Report{}, fmt.Errorf("scan report: %w", err)
	}
	if resolverID != nil {
		item.ResolvedBy = &reports.UserShort{
			ID: *resolverID, FirstName: valueOrEmpty(resolverFirstName),
			LastName: resolverLastName, AvatarURL: resolverAvatarURL,
		}
	}
	return item, nil
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
