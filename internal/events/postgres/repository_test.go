package postgres_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/emount4/poidem-back/internal/events"
	eventspostgres "github.com/emount4/poidem-back/internal/events/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestEventLocationLifecycleIntegration(t *testing.T) {
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

	var cityID, categoryID, userID int64
	if err := pool.QueryRow(ctx, `INSERT INTO cities (name) VALUES ('Map test city') RETURNING id`).Scan(&cityID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO event_categories (name) VALUES ('Map test category') RETURNING id`).Scan(&categoryID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO users (first_name, city_id, role, status, created_at, updated_at)
		VALUES ('Map author', $1, 'user', 'active', now(), now()) RETURNING id
	`, cityID).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM events WHERE creator_id = $1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM event_categories WHERE id = $1`, categoryID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM cities WHERE id = $1`, cityID)
	})

	repository := eventspostgres.NewRepository(pool)
	now := time.Now().UTC().Truncate(time.Microsecond)
	imageURL := "http://localhost:9000/poidem-media/events/cover.webp"
	if _, err := repository.CreateCoverUpload(ctx, events.CoverUpload{
		OwnerID: userID, ObjectKey: "events/temp/integration-cover.webp",
		PublicURL: imageURL, CreatedAt: now,
	}, "image/webp", 100); err != nil {
		t.Fatal(err)
	}
	item, err := repository.Create(ctx, userID, events.CreateInput{
		Title: "Map integration event", CategoryID: categoryID, CityID: cityID,
		StartsAt: now.Add(24 * time.Hour), LocationName: "Map point", ImageURL: &imageURL,
		Location: &events.Location{Latitude: 12.345678, Longitude: 98.765432, Source: "manual"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if item.Location == nil || item.Location.Latitude != 12.345678 || item.Location.Source != "manual" {
		t.Fatalf("location round trip failed: %+v", item)
	}
	approved, err := repository.Transition(ctx, item.ID, "approve", nil, now)
	if err != nil || approved.Status != events.StatusActive {
		t.Fatalf("approve item=%+v err=%v", approved, err)
	}
	items, total, err := repository.ListPublic(ctx, events.PublicFilter{
		Now: now, Bounds: &events.Bounds{West: 98.7, South: 12.3, East: 98.8, North: 12.4},
	}, events.Page{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, listed := range items {
		found = found || listed.ID == item.ID
	}
	if total < 1 || !found {
		t.Fatalf("event missing from viewport: total=%d items=%+v", total, items)
	}
	updated, err := repository.UpdateOwned(ctx, item.ID, userID, events.Patch{
		Title: events.Change[string]{Set: true, Value: "Updated map event"},
	}, now.Add(time.Minute))
	if err != nil || updated.Status != events.StatusPending || updated.Title != "Updated map event" {
		t.Fatalf("author update item=%+v err=%v", updated, err)
	}
	if _, err := repository.UpdateOwned(ctx, item.ID, userID+1000, events.Patch{
		Title: events.Change[string]{Set: true, Value: "Forbidden"},
	}, now.Add(time.Minute)); !errors.Is(err, events.ErrForbidden) {
		t.Fatalf("foreign author update error=%v", err)
	}

	withoutCover, err := repository.Create(ctx, userID, events.CreateInput{
		Title: "Incomplete map event", CategoryID: categoryID, CityID: cityID,
		StartsAt: now.Add(48 * time.Hour), LocationName: "Map point",
		Location: &events.Location{Latitude: 12.35, Longitude: 98.77, Source: "manual"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = repository.Transition(ctx, withoutCover.ID, "approve", nil, now)
	var validation *events.ValidationError
	if !errors.As(err, &validation) || validation.Fields["imageUrl"] == nil {
		t.Fatalf("approve validation error=%v", err)
	}
	reason := "Недостаточно информации"
	rejected, err := repository.Transition(ctx, withoutCover.ID, "reject", &reason, now)
	if err != nil || rejected.ModerationReason == nil || *rejected.ModerationReason != reason {
		t.Fatalf("rejected item=%+v err=%v", rejected, err)
	}
	if err := repository.Delete(ctx, item.ID, &userID, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.GetAdmin(ctx, item.ID); !errors.Is(err, events.ErrNotFound) {
		t.Fatalf("deleted event must be hidden, got %v", err)
	}
}
