BEGIN;

ALTER TABLE users
    DROP COLUMN avatar_object_key;

COMMIT;
