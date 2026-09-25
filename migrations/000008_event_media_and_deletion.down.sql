BEGIN;

DROP INDEX IF EXISTS idx_events_creator_editable;
DROP TABLE IF EXISTS event_uploads;
ALTER TABLE applications
    DROP CONSTRAINT chk_applications_resolution_reason,
    ADD CONSTRAINT chk_applications_resolution_reason CHECK (
        resolution_reason IS NULL
        OR resolution_reason IN ('COMPANY_BLOCKED', 'EVENT_COMPLETED')
    );
ALTER TABLE events DROP COLUMN IF EXISTS deleted_at;

COMMIT;
