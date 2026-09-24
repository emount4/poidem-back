BEGIN;

UPDATE events
SET status = COALESCE(status, 'pending'),
    created_at = COALESCE(created_at, now()),
    updated_at = COALESCE(updated_at, created_at, now());

ALTER TABLE events
    ALTER COLUMN creator_id SET NOT NULL,
    ALTER COLUMN category_id SET NOT NULL,
    ALTER COLUMN city_id SET NOT NULL,
    ALTER COLUMN status SET DEFAULT 'pending',
    ALTER COLUMN status SET NOT NULL,
    ALTER COLUMN created_at SET DEFAULT now(),
    ALTER COLUMN created_at SET NOT NULL,
    ALTER COLUMN updated_at SET DEFAULT now(),
    ALTER COLUMN updated_at SET NOT NULL,
    ADD CONSTRAINT chk_events_time_range CHECK (ends_at IS NULL OR ends_at > starts_at);

CREATE INDEX idx_events_public_catalog
    ON events(status, starts_at, id);

CREATE INDEX idx_events_creator_start
    ON events(creator_id, starts_at, id);

COMMIT;
