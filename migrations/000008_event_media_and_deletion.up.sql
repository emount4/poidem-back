BEGIN;

ALTER TABLE events
    ADD COLUMN deleted_at TIMESTAMPTZ;

ALTER TABLE applications
    DROP CONSTRAINT chk_applications_resolution_reason,
    ADD CONSTRAINT chk_applications_resolution_reason CHECK (
        resolution_reason IS NULL
        OR resolution_reason IN ('COMPANY_BLOCKED', 'EVENT_COMPLETED', 'EVENT_DELETED')
    );

CREATE TABLE event_uploads (
    id BIGSERIAL PRIMARY KEY,
    owner_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    event_id BIGINT REFERENCES events(id) ON DELETE SET NULL,
    object_key TEXT NOT NULL UNIQUE,
    public_url TEXT NOT NULL UNIQUE,
    content_type VARCHAR(50) NOT NULL,
    size_bytes BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    linked_at TIMESTAMPTZ,
    CONSTRAINT chk_event_uploads_content_type CHECK (
        content_type IN ('image/jpeg', 'image/png', 'image/webp')
    ),
    CONSTRAINT chk_event_uploads_size CHECK (size_bytes > 0 AND size_bytes <= 10485760),
    CONSTRAINT chk_event_uploads_link CHECK (
        (event_id IS NULL AND linked_at IS NULL)
        OR (event_id IS NOT NULL AND linked_at IS NOT NULL)
    )
);

CREATE INDEX idx_event_uploads_unlinked_cleanup
    ON event_uploads(created_at, id)
    WHERE event_id IS NULL;

CREATE INDEX idx_events_creator_editable
    ON events(creator_id, status, id)
    WHERE deleted_at IS NULL;

COMMIT;
