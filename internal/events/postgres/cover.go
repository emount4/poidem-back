package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/emount4/poidem-back/internal/events"
	platformpostgres "github.com/emount4/poidem-back/internal/platform/postgres"
)

func (r *Repository) CreateCoverUpload(ctx context.Context, upload events.CoverUpload, contentType string, size int64) (events.CoverUpload, error) {
	err := platformpostgres.Executor(ctx, r.pool).QueryRow(ctx, `
		INSERT INTO event_uploads (owner_id, object_key, public_url, content_type, size_bytes, created_at)
		VALUES ($1,$2,$3,$4,$5,$6)
		RETURNING id
	`, upload.OwnerID, upload.ObjectKey, upload.PublicURL, contentType, size, upload.CreatedAt).Scan(&upload.ID)
	if err != nil {
		return events.CoverUpload{}, fmt.Errorf("insert event cover upload: %w", err)
	}
	return upload, nil
}

func (r *Repository) ExpiredCoverUploads(ctx context.Context, before time.Time, limit int) ([]events.CoverUpload, error) {
	rows, err := platformpostgres.Executor(ctx, r.pool).Query(ctx, `
		SELECT id, owner_id, event_id, object_key, public_url, created_at
		FROM event_uploads
		WHERE event_id IS NULL AND created_at < $1
		ORDER BY created_at, id
		LIMIT $2
	`, before, limit)
	if err != nil {
		return nil, fmt.Errorf("query expired event covers: %w", err)
	}
	defer rows.Close()
	items := make([]events.CoverUpload, 0)
	for rows.Next() {
		var item events.CoverUpload
		if err := rows.Scan(&item.ID, &item.OwnerID, &item.EventID, &item.ObjectKey, &item.PublicURL, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan expired event cover: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate expired event covers: %w", err)
	}
	return items, nil
}

func (r *Repository) DeleteCoverUpload(ctx context.Context, id int64) error {
	_, err := platformpostgres.Executor(ctx, r.pool).Exec(ctx, `
		DELETE FROM event_uploads WHERE id = $1 AND event_id IS NULL
	`, id)
	if err != nil {
		return fmt.Errorf("delete event cover upload: %w", err)
	}
	return nil
}
