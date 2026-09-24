BEGIN;

DROP INDEX IF EXISTS idx_events_creator_start;
DROP INDEX IF EXISTS idx_events_public_catalog;

ALTER TABLE events
    DROP CONSTRAINT IF EXISTS chk_events_time_range,
    ALTER COLUMN updated_at DROP DEFAULT,
    ALTER COLUMN updated_at DROP NOT NULL,
    ALTER COLUMN created_at DROP DEFAULT,
    ALTER COLUMN created_at DROP NOT NULL,
    ALTER COLUMN status DROP DEFAULT,
    ALTER COLUMN status DROP NOT NULL,
    ALTER COLUMN city_id DROP NOT NULL,
    ALTER COLUMN category_id DROP NOT NULL,
    ALTER COLUMN creator_id DROP NOT NULL;

COMMIT;
