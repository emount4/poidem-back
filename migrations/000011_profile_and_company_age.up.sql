BEGIN;

ALTER TABLE users
    ADD COLUMN gender VARCHAR(16),
    ADD COLUMN birth_date DATE,
    ADD CONSTRAINT chk_users_gender CHECK (
        gender IS NULL OR gender IN ('male', 'female', 'other')
    );

ALTER TABLE companies
    ADD COLUMN min_age INTEGER,
    ADD COLUMN max_age INTEGER,
    ADD CONSTRAINT chk_companies_min_age CHECK (
        min_age IS NULL OR min_age BETWEEN 14 AND 100
    ),
    ADD CONSTRAINT chk_companies_max_age CHECK (
        max_age IS NULL OR max_age BETWEEN 14 AND 100
    ),
    ADD CONSTRAINT chk_companies_age_range CHECK (
        min_age IS NULL OR max_age IS NULL OR min_age <= max_age
    );

INSERT INTO interests (name, slug) VALUES
    ('Психология', 'psychology'),
    ('Наука', 'science'),
    ('Прогулки', 'walks'),
    ('Йога и здоровье', 'yoga-and-health')
ON CONFLICT (slug) DO NOTHING;

COMMIT;
