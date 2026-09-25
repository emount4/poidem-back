package postgres

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/emount4/poidem-back/internal/reports"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestReportLifecycleIntegration(t *testing.T) {
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

	var authorID, targetID, adminID int64
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM reports WHERE author_id = $1`, authorID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = ANY($1::bigint[])`, []int64{authorID, targetID, adminID})
	})
	for name, destination := range map[string]*int64{
		"Report author": &authorID, "Report target": &targetID, "Report admin": &adminID,
	} {
		if err := pool.QueryRow(ctx, `
			INSERT INTO users (first_name, role, status, created_at, updated_at)
			VALUES ($1, 'user', 'active', now(), now()) RETURNING id
		`, name).Scan(destination); err != nil {
			t.Fatal(err)
		}
	}
	repository := NewRepository(pool)
	now := time.Now().UTC().Truncate(time.Microsecond)
	item, err := repository.Create(ctx, authorID, reports.CreateInput{
		TargetType: reports.TargetUser, TargetID: targetID, Reason: "Integration report",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if item.Status != reports.StatusPending || item.Author.ID != authorID || !item.CreatedAt.Equal(now) {
		t.Fatalf("created report = %+v", item)
	}

	items, total, err := repository.ListAdmin(ctx, reports.StatusPending, reports.Page{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, listed := range items {
		if listed.ID == item.ID {
			found = true
		}
	}
	if total < 1 || !found {
		t.Fatalf("report not listed: total=%d items=%+v", total, items)
	}

	resolved, err := repository.Resolve(ctx, item.ID, adminID, "resolve", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Status != reports.StatusResolved || resolved.ResolvedBy == nil || resolved.ResolvedBy.ID != adminID || resolved.ResolvedAt == nil {
		t.Fatalf("resolved report = %+v", resolved)
	}
	if _, err := repository.Resolve(ctx, item.ID, adminID, "reject", now.Add(2*time.Minute)); !errors.Is(err, reports.ErrAlreadyResolved) {
		t.Fatalf("repeat resolve error = %v", err)
	}
	if _, err := repository.Create(ctx, authorID, reports.CreateInput{
		TargetType: reports.TargetUser, TargetID: 1<<62 - 1, Reason: "Missing target",
	}, now); !errors.Is(err, reports.ErrTargetNotFound) {
		t.Fatalf("missing target error = %v", err)
	}
}
