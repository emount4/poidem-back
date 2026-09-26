BEGIN;

ALTER TABLE users
    ADD COLUMN username VARCHAR(64),
    ADD COLUMN password_hash TEXT,
    ADD CONSTRAINT chk_users_username_length CHECK (
        username IS NULL OR char_length(username) BETWEEN 3 AND 64
    ),
    ADD CONSTRAINT chk_users_username_normalized CHECK (
        username IS NULL OR username = lower(username)
    ),
    ADD CONSTRAINT chk_users_password_credentials_pair CHECK (
        (username IS NULL AND password_hash IS NULL)
        OR (username IS NOT NULL AND password_hash IS NOT NULL)
    );

CREATE UNIQUE INDEX uq_users_username_ci
    ON users(lower(username))
    WHERE username IS NOT NULL;

COMMIT;
