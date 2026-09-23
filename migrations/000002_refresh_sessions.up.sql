BEGIN;

CREATE TABLE sessions (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash BYTEA NOT NULL,
    previous_token_hash BYTEA,
    previous_valid_until TIMESTAMPTZ,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_sessions_token_hash UNIQUE (token_hash),
    CONSTRAINT chk_sessions_token_hash_length CHECK (octet_length(token_hash) = 32),
    CONSTRAINT chk_sessions_previous_token_hash_length CHECK (
        previous_token_hash IS NULL OR octet_length(previous_token_hash) = 32
    ),
    CONSTRAINT chk_sessions_previous_token_pair CHECK (
        (previous_token_hash IS NULL) = (previous_valid_until IS NULL)
    ),
    CONSTRAINT chk_sessions_expiration CHECK (expires_at > created_at)
);

CREATE UNIQUE INDEX uq_sessions_previous_token_hash
    ON sessions(previous_token_hash)
    WHERE previous_token_hash IS NOT NULL;

CREATE INDEX idx_sessions_user_active
    ON sessions(user_id)
    WHERE revoked_at IS NULL;

CREATE INDEX idx_sessions_expires_active
    ON sessions(expires_at)
    WHERE revoked_at IS NULL;

COMMIT;
