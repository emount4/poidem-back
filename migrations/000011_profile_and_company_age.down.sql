BEGIN;

ALTER TABLE companies
    DROP CONSTRAINT IF EXISTS chk_companies_age_range,
    DROP CONSTRAINT IF EXISTS chk_companies_max_age,
    DROP CONSTRAINT IF EXISTS chk_companies_min_age,
    DROP COLUMN IF EXISTS max_age,
    DROP COLUMN IF EXISTS min_age;

ALTER TABLE users
    DROP CONSTRAINT IF EXISTS chk_users_gender,
    DROP COLUMN IF EXISTS birth_date,
    DROP COLUMN IF EXISTS gender;

-- Справочники не удаляются: после применения миграции они могли быть выбраны
-- пользователями и считаются прикладными данными.

COMMIT;
