BEGIN;

DROP INDEX IF EXISTS uq_users_username_ci;
ALTER TABLE users
    DROP CONSTRAINT IF EXISTS chk_users_password_credentials_pair,
    DROP CONSTRAINT IF EXISTS chk_users_username_normalized,
    DROP CONSTRAINT IF EXISTS chk_users_username_length,
    DROP COLUMN IF EXISTS password_hash,
    DROP COLUMN IF EXISTS username;

COMMIT;
