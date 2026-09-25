BEGIN;

ALTER TABLE events
    ADD COLUMN latitude DOUBLE PRECISION,
    ADD COLUMN longitude DOUBLE PRECISION,
    ADD COLUMN location_source VARCHAR(20) NOT NULL DEFAULT 'manual',
    ADD COLUMN moderation_reason TEXT,
    ADD CONSTRAINT chk_events_location_pair CHECK (
        (latitude IS NULL AND longitude IS NULL)
        OR (latitude IS NOT NULL AND longitude IS NOT NULL)
    ),
    ADD CONSTRAINT chk_events_latitude CHECK (latitude IS NULL OR latitude BETWEEN -90 AND 90),
    ADD CONSTRAINT chk_events_longitude CHECK (longitude IS NULL OR longitude BETWEEN -180 AND 180),
    ADD CONSTRAINT chk_events_location_source CHECK (location_source IN ('manual', 'geocoded'));

CREATE INDEX idx_events_map_active
    ON events(status, city_id, starts_at, latitude, longitude)
    WHERE latitude IS NOT NULL AND longitude IS NOT NULL;

COMMIT;
