BEGIN;

DROP INDEX idx_company_members_user_joined;
DROP INDEX idx_companies_event_visible;

ALTER TABLE event_participants
    DROP CONSTRAINT fk_event_participants_company_event,
    DROP CONSTRAINT chk_event_participants_company,
    ALTER COLUMN joined_at DROP DEFAULT,
    ALTER COLUMN joined_at DROP NOT NULL,
    ALTER COLUMN participation_type DROP NOT NULL,
    ALTER COLUMN user_id DROP NOT NULL,
    ALTER COLUMN event_id DROP NOT NULL;

ALTER TABLE company_members
    DROP CONSTRAINT chk_company_members_role,
    ALTER COLUMN joined_at DROP DEFAULT,
    ALTER COLUMN joined_at DROP NOT NULL,
    ALTER COLUMN role DROP DEFAULT,
    ALTER COLUMN role DROP NOT NULL,
    ALTER COLUMN user_id DROP NOT NULL,
    ALTER COLUMN company_id DROP NOT NULL;

ALTER TABLE companies
    DROP CONSTRAINT uq_companies_id_event,
    DROP CONSTRAINT chk_companies_max_members,
    ALTER COLUMN updated_at DROP DEFAULT,
    ALTER COLUMN updated_at DROP NOT NULL,
    ALTER COLUMN created_at DROP DEFAULT,
    ALTER COLUMN created_at DROP NOT NULL,
    ALTER COLUMN status DROP DEFAULT,
    ALTER COLUMN status DROP NOT NULL,
    ALTER COLUMN join_type DROP DEFAULT,
    ALTER COLUMN join_type DROP NOT NULL,
    ALTER COLUMN owner_id DROP NOT NULL,
    ALTER COLUMN event_id DROP NOT NULL,
    DROP COLUMN deleted_at;

COMMIT;
