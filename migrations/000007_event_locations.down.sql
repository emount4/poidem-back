BEGIN;

DROP INDEX idx_events_map_active;

ALTER TABLE events
    DROP CONSTRAINT chk_events_location_source,
    DROP CONSTRAINT chk_events_longitude,
    DROP CONSTRAINT chk_events_latitude,
    DROP CONSTRAINT chk_events_location_pair,
    DROP COLUMN moderation_reason,
    DROP COLUMN location_source,
    DROP COLUMN longitude,
    DROP COLUMN latitude;

COMMIT;
