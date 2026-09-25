BEGIN;

ALTER TABLE applications
    ADD COLUMN resolution_reason VARCHAR(50);

UPDATE applications
SET status = COALESCE(status, 'pending'),
    created_at = COALESCE(created_at, now());

ALTER TABLE applications
    ALTER COLUMN company_id SET NOT NULL,
    ALTER COLUMN user_id SET NOT NULL,
    ALTER COLUMN status SET DEFAULT 'pending',
    ALTER COLUMN status SET NOT NULL,
    ALTER COLUMN created_at SET DEFAULT now(),
    ALTER COLUMN created_at SET NOT NULL,
    ADD CONSTRAINT chk_applications_resolution_reason CHECK (
        resolution_reason IS NULL
        OR resolution_reason IN ('COMPANY_BLOCKED', 'EVENT_COMPLETED')
    );

CREATE UNIQUE INDEX uq_applications_pending_company_user
    ON applications(company_id, user_id)
    WHERE status = 'pending';

COMMIT;
