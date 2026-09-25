BEGIN;

DROP INDEX uq_applications_pending_company_user;

ALTER TABLE applications
    DROP CONSTRAINT chk_applications_resolution_reason,
    ALTER COLUMN created_at DROP DEFAULT,
    ALTER COLUMN created_at DROP NOT NULL,
    ALTER COLUMN status DROP DEFAULT,
    ALTER COLUMN status DROP NOT NULL,
    ALTER COLUMN user_id DROP NOT NULL,
    ALTER COLUMN company_id DROP NOT NULL,
    DROP COLUMN resolution_reason;

COMMIT;
