package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/emount4/poidem-back/internal/account"
	platformpostgres "github.com/emount4/poidem-back/internal/platform/postgres"
	"github.com/jackc/pgx/v5"
)

func (r *Repository) ListUsers(ctx context.Context, search string, page account.AdminPage) ([]account.Profile, int64, error) {
	db := platformpostgres.Executor(ctx, r.pool)
	where := ""
	args := []any{}
	if search != "" {
		where = ` WHERE concat_ws(' ', u.first_name, u.last_name) ILIKE '%' || $1 || '%' OR u.id::text = $1`
		args = append(args, search)
	}
	rows, err := db.Query(ctx, `
		SELECT u.id, u.first_name, u.last_name, u.avatar_url,
		       c.id, c.name, c.slug, u.about,
		       COALESCE(u.role, 'user'), COALESCE(u.status, 'active'),
		       COALESCE(u.created_at, u.updated_at, now())
		FROM users u
		LEFT JOIN cities c ON c.id = u.city_id
	`+where+`
		ORDER BY COALESCE(u.created_at, u.updated_at) DESC NULLS LAST, u.id DESC
		LIMIT $`+fmt.Sprint(len(args)+1)+` OFFSET $`+fmt.Sprint(len(args)+2), append(args, page.Limit, page.Offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("query admin users: %w", err)
	}
	defer rows.Close()
	items := make([]account.Profile, 0)
	ids := make([]int64, 0)
	for rows.Next() {
		var item account.Profile
		var cityID *int64
		var cityName, citySlug *string
		if err := rows.Scan(
			&item.ID, &item.FirstName, &item.LastName, &item.AvatarURL,
			&cityID, &cityName, &citySlug, &item.About,
			&item.Role, &item.Status, &item.CreatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan admin user: %w", err)
		}
		if cityID != nil {
			item.City = &account.DictionaryItem{ID: *cityID, Name: valueOrEmpty(cityName), Slug: citySlug}
		}
		item.Interests = []account.DictionaryItem{}
		items = append(items, item)
		ids = append(ids, item.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate admin users: %w", err)
	}
	if err := loadAdminUserInterests(ctx, db, items, ids); err != nil {
		return nil, 0, err
	}
	var total int64
	if err := db.QueryRow(ctx, `SELECT count(*) FROM users u`+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count admin users: %w", err)
	}
	return items, total, nil
}

func loadAdminUserInterests(ctx context.Context, db platformpostgres.DBTX, items []account.Profile, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	positions := make(map[int64]int, len(ids))
	for i, id := range ids {
		positions[id] = i
	}
	rows, err := db.Query(ctx, `
		SELECT ui.user_id, i.id, i.name, i.slug
		FROM user_interests ui
		JOIN interests i ON i.id = ui.interest_id
		WHERE ui.user_id = ANY($1::bigint[])
		ORDER BY i.name, i.id
	`, ids)
	if err != nil {
		return fmt.Errorf("query admin user interests: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var userID int64
		var interest account.DictionaryItem
		if err := rows.Scan(&userID, &interest.ID, &interest.Name, &interest.Slug); err != nil {
			return fmt.Errorf("scan admin user interest: %w", err)
		}
		items[positions[userID]].Interests = append(items[positions[userID]].Interests, interest)
	}
	return rows.Err()
}

func (r *Repository) SetUserStatus(ctx context.Context, userID int64, status string, now time.Time) (account.Profile, error) {
	var profile account.Profile
	err := r.transactions.WithinTransaction(ctx, func(txCtx context.Context) error {
		db := platformpostgres.Executor(txCtx, r.pool)
		var role string
		err := db.QueryRow(txCtx, `SELECT COALESCE(role, 'user') FROM users WHERE id = $1 FOR UPDATE`, userID).Scan(&role)
		if errors.Is(err, pgx.ErrNoRows) {
			return account.ErrUserNotFound
		}
		if err != nil {
			return fmt.Errorf("lock admin user: %w", err)
		}
		if status == account.StatusBanned && role == account.RoleAdmin {
			return account.ErrCannotBanAdmin
		}
		if _, err := db.Exec(txCtx, `UPDATE users SET status = $2, updated_at = $3 WHERE id = $1`, userID, status, now); err != nil {
			return fmt.Errorf("update admin user status: %w", err)
		}
		if status == account.StatusBanned {
			if _, err := db.Exec(txCtx, `
				UPDATE sessions SET revoked_at = $2, updated_at = $2
				WHERE user_id = $1 AND revoked_at IS NULL
			`, userID, now); err != nil {
				return fmt.Errorf("revoke banned user sessions: %w", err)
			}
		}
		profile, err = r.Profile(txCtx, userID)
		return err
	})
	return profile, err
}

func (r *Repository) Dashboard(ctx context.Context, now time.Time) (account.AdminDashboard, error) {
	var result account.AdminDashboard
	err := platformpostgres.Executor(ctx, r.pool).QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM users),
			(SELECT count(*) FROM events WHERE status = 'active' AND deleted_at IS NULL),
			(SELECT count(*) FROM reports WHERE status = 'pending'),
			(SELECT count(*) FROM users
			 WHERE COALESCE(created_at, updated_at) >= date_trunc('day', $1::timestamptz) - interval '29 days'
			   AND COALESCE(created_at, updated_at) < date_trunc('day', $1::timestamptz) + interval '1 day')
	`, now).Scan(&result.UsersTotal, &result.ActiveEvents, &result.PendingReports, &result.NewRegistrations30d)
	if err != nil {
		return account.AdminDashboard{}, fmt.Errorf("query admin dashboard: %w", err)
	}
	return result, nil
}

func (r *Repository) PromoteAdmin(ctx context.Context, userID int64) error {
	command, err := platformpostgres.Executor(ctx, r.pool).Exec(ctx, `
		UPDATE users SET role = 'admin', updated_at = now() WHERE id = $1
	`, userID)
	if err != nil {
		return fmt.Errorf("promote bootstrap admin: %w", err)
	}
	if command.RowsAffected() == 0 {
		return account.ErrUserNotFound
	}
	return nil
}
