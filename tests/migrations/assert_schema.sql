-- Run with psql -v ON_ERROR_STOP=1 against an isolated, migrated test database.
BEGIN;

CREATE FUNCTION pg_temp.expect_sqlstate(statement TEXT, expected TEXT)
RETURNS VOID LANGUAGE plpgsql AS $$
BEGIN
    BEGIN
        EXECUTE statement;
    EXCEPTION WHEN OTHERS THEN
        IF SQLSTATE = expected THEN
            RETURN;
        END IF;
        RAISE;
    END;
    RAISE EXCEPTION 'Expected SQLSTATE % for statement: %', expected, statement;
END;
$$;

DO $$
BEGIN
    IF (SELECT count(*) FROM information_schema.tables
        WHERE table_schema = 'public' AND table_type = 'BASE TABLE'
        AND table_name <> 'schema_migrations') <> 12 THEN
        RAISE EXCEPTION 'Expected 12 application tables';
    END IF;
    IF (SELECT count(*) FROM pg_indexes WHERE schemaname = 'public' AND indexname IN (
        'idx_events_city_start', 'idx_events_category', 'idx_events_status',
        'idx_companies_event', 'idx_companies_owner', 'idx_applications_company_status',
        'idx_applications_user', 'idx_event_participants_user', 'idx_reports_status'
    )) <> 9 THEN
        RAISE EXCEPTION 'Missing recommended indexes';
    END IF;
END;
$$;

INSERT INTO cities (id, name, slug) VALUES (1, 'Test city', 'test-city');
INSERT INTO interests (id, name, slug) VALUES (1, 'Walking', 'walking');
INSERT INTO event_categories (id, name, slug) VALUES (1, 'Outdoors', 'outdoors');
INSERT INTO users (id, first_name, city_id) VALUES (1, 'Owner', 1), (2, 'Guest', 1);
INSERT INTO auth_accounts (user_id, provider, provider_user_id) VALUES (1, 'test', 'external-1');
INSERT INTO user_interests (user_id, interest_id) VALUES (1, 1);
INSERT INTO events (id, creator_id, category_id, city_id, title, starts_at, location_name, status)
VALUES (1, 1, 1, 1, 'Walk', now(), 'Park', 'active');
INSERT INTO companies (id, event_id, owner_id, name, max_members, join_type, status)
VALUES (1, 1, 1, 'Group', 5, 'request', 'active');
INSERT INTO company_members (company_id, user_id, role) VALUES (1, 1, 'owner');
INSERT INTO applications (company_id, user_id, message, status) VALUES (1, 2, 'Join?', 'pending');
INSERT INTO event_participants (event_id, user_id, participation_type, company_id)
VALUES (1, 1, 'company', 1), (1, 2, 'solo', NULL);
INSERT INTO reports (author_id, target_type, target_id, reason, status)
VALUES (2, 'company', 1, 'Test report', 'pending');

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM users WHERE id = 1 AND role = 'user' AND status = 'active') THEN
        RAISE EXCEPTION 'User defaults are incorrect';
    END IF;
END;
$$;

SELECT pg_temp.expect_sqlstate('INSERT INTO cities (name, slug) VALUES (''Duplicate'', ''test-city'')', '23505');
SELECT pg_temp.expect_sqlstate('INSERT INTO auth_accounts (provider, provider_user_id) VALUES (''test'', ''external-1'')', '23505');
SELECT pg_temp.expect_sqlstate('INSERT INTO user_interests VALUES (1, 1)', '23505');
SELECT pg_temp.expect_sqlstate('INSERT INTO company_members (company_id, user_id) VALUES (1, 1)', '23505');
SELECT pg_temp.expect_sqlstate('INSERT INTO event_participants (event_id, user_id) VALUES (1, 1)', '23505');
SELECT pg_temp.expect_sqlstate('INSERT INTO users (first_name) VALUES (NULL)', '23502');
SELECT pg_temp.expect_sqlstate('UPDATE events SET city_id = 999 WHERE id = 1', '23503');
SELECT pg_temp.expect_sqlstate('UPDATE event_participants SET company_id = 999 WHERE user_id = 1', '23503');
SELECT pg_temp.expect_sqlstate('DELETE FROM users WHERE id = 1', '23503');
SELECT pg_temp.expect_sqlstate('UPDATE events SET status = ''invalid''', '23514');
SELECT pg_temp.expect_sqlstate('UPDATE companies SET status = ''invalid''', '23514');
SELECT pg_temp.expect_sqlstate('UPDATE companies SET join_type = ''invalid''', '23514');
SELECT pg_temp.expect_sqlstate('UPDATE applications SET status = ''invalid''', '23514');
SELECT pg_temp.expect_sqlstate('UPDATE event_participants SET participation_type = ''invalid''', '23514');
SELECT pg_temp.expect_sqlstate('UPDATE reports SET status = ''invalid''', '23514');
SELECT pg_temp.expect_sqlstate('UPDATE reports SET target_type = ''invalid''', '23514');

ROLLBACK;
