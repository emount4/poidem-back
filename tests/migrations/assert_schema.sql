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
        AND table_name IN (
            'cities', 'interests', 'event_categories', 'users', 'auth_accounts',
            'sessions', 'user_interests', 'events', 'companies', 'company_members',
            'applications', 'event_participants', 'reports', 'event_uploads'
        )) <> 14 THEN
        RAISE EXCEPTION 'Missing one or more application tables';
    END IF;
    IF (SELECT count(*) FROM pg_indexes WHERE schemaname = 'public' AND indexname IN (
        'idx_events_city_start', 'idx_events_category', 'idx_events_status',
        'idx_companies_event', 'idx_companies_owner', 'idx_applications_company_status',
        'idx_applications_user', 'idx_event_participants_user', 'idx_reports_status',
        'uq_sessions_previous_token_hash', 'idx_sessions_user_active',
        'idx_sessions_expires_active'
    )) <> 12 THEN
        RAISE EXCEPTION 'Missing recommended indexes';
    END IF;
    IF (SELECT count(*) FROM pg_indexes WHERE schemaname = 'public' AND indexname IN (
        'idx_events_public_catalog', 'idx_events_creator_start'
    )) <> 2 THEN
        RAISE EXCEPTION 'Missing event query indexes';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_indexes WHERE schemaname = 'public'
        AND indexname = 'idx_events_map_active') THEN
        RAISE EXCEPTION 'Missing event map index';
    END IF;
    IF (SELECT count(*) FROM pg_indexes WHERE schemaname = 'public' AND indexname IN (
        'idx_event_uploads_unlinked_cleanup', 'idx_events_creator_editable'
    )) <> 2 THEN
        RAISE EXCEPTION 'Missing event media indexes';
    END IF;
    IF (SELECT count(*) FROM pg_indexes WHERE schemaname = 'public' AND indexname IN (
        'idx_companies_event_visible', 'idx_company_members_user_joined'
    )) <> 2 THEN
        RAISE EXCEPTION 'Missing company query indexes';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_indexes WHERE schemaname = 'public'
        AND indexname = 'uq_applications_pending_company_user') THEN
        RAISE EXCEPTION 'Missing pending application uniqueness';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_events_time_range') THEN
        RAISE EXCEPTION 'Missing event time range constraint';
    END IF;
    IF (SELECT count(*) FROM pg_constraint WHERE conname IN (
        'chk_events_location_pair', 'chk_events_latitude',
        'chk_events_longitude', 'chk_events_location_source'
    )) <> 4 THEN
        RAISE EXCEPTION 'Missing event location constraints';
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'public' AND table_name = 'users' AND column_name = 'avatar_object_key'
    ) THEN
        RAISE EXCEPTION 'Missing avatar object key';
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'public' AND table_name = 'events' AND column_name = 'deleted_at'
    ) THEN
        RAISE EXCEPTION 'Missing event soft delete column';
    END IF;
    IF (SELECT count(*) FROM pg_constraint WHERE conname IN (
        'chk_companies_max_members', 'chk_company_members_role',
        'chk_event_participants_company', 'fk_event_participants_company_event'
    )) <> 4 THEN
        RAISE EXCEPTION 'Missing company participation constraints';
    END IF;
END;
$$;

DO $$
BEGIN
    IF (SELECT count(*) FROM cities WHERE slug IN ('moskva', 'sankt-peterburg')) <> 2 THEN
        RAISE EXCEPTION 'Russian city seed is missing';
    END IF;
    IF (SELECT count(*) FROM event_categories WHERE slug IN ('music', 'sport')) <> 2 THEN
        RAISE EXCEPTION 'Event category seed is missing';
    END IF;
    IF (SELECT count(*) FROM interests WHERE slug IN ('live-music', 'technology')) <> 2 THEN
        RAISE EXCEPTION 'Interest seed is missing';
    END IF;
END;
$$;

INSERT INTO cities (id, name, slug) VALUES (900001, 'Test city', 'test-city');
INSERT INTO interests (id, name, slug) VALUES (900001, 'Walking', 'walking');
INSERT INTO event_categories (id, name, slug) VALUES (900001, 'Outdoors', 'outdoors');
INSERT INTO users (id, first_name, city_id) VALUES (900001, 'Owner', 900001), (900002, 'Guest', 900001);
INSERT INTO auth_accounts (user_id, provider, provider_user_id) VALUES (900001, 'test', 'external-1');
INSERT INTO sessions (user_id, token_hash, expires_at)
VALUES (900001, decode(repeat('ab', 32), 'hex'), now() + interval '30 days');
INSERT INTO user_interests (user_id, interest_id) VALUES (900001, 900001);
INSERT INTO events (id, creator_id, category_id, city_id, title, starts_at, location_name, status)
VALUES (900001, 900001, 900001, 900001, 'Walk', now(), 'Park', 'active');
INSERT INTO companies (id, event_id, owner_id, name, max_members, join_type, status)
VALUES (900001, 900001, 900001, 'Group', 5, 'request', 'active');
INSERT INTO company_members (company_id, user_id, role) VALUES (900001, 900001, 'owner');
INSERT INTO applications (company_id, user_id, message, status) VALUES (900001, 900002, 'Join?', 'pending');
INSERT INTO event_participants (event_id, user_id, participation_type, company_id)
VALUES (900001, 900001, 'company', 900001), (900001, 900002, 'solo', NULL);
INSERT INTO reports (author_id, target_type, target_id, reason, status)
VALUES (900002, 'company', 900001, 'Test report', 'pending');

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM users WHERE id = 900001 AND role = 'user' AND status = 'active') THEN
        RAISE EXCEPTION 'User defaults are incorrect';
    END IF;
END;
$$;

SELECT pg_temp.expect_sqlstate('INSERT INTO cities (name, slug) VALUES (''Duplicate'', ''test-city'')', '23505');
SELECT pg_temp.expect_sqlstate('INSERT INTO auth_accounts (provider, provider_user_id) VALUES (''test'', ''external-1'')', '23505');
SELECT pg_temp.expect_sqlstate(
    'INSERT INTO sessions (user_id, token_hash, expires_at) VALUES (900001, decode(repeat(''ab'', 32), ''hex''), now() + interval ''1 day'')',
    '23505'
);
SELECT pg_temp.expect_sqlstate(
    'INSERT INTO sessions (user_id, token_hash, expires_at) VALUES (900001, decode(''ab'', ''hex''), now() + interval ''1 day'')',
    '23514'
);
SELECT pg_temp.expect_sqlstate(
    'INSERT INTO sessions (user_id, token_hash, previous_token_hash, expires_at) VALUES (900001, decode(repeat(''bc'', 32), ''hex''), decode(repeat(''cd'', 32), ''hex''), now() + interval ''1 day'')',
    '23514'
);
SELECT pg_temp.expect_sqlstate('INSERT INTO user_interests VALUES (900001, 900001)', '23505');
SELECT pg_temp.expect_sqlstate('INSERT INTO company_members (company_id, user_id) VALUES (900001, 900001)', '23505');
SELECT pg_temp.expect_sqlstate(
    'INSERT INTO event_participants (event_id, user_id, participation_type, company_id) VALUES (900001, 900001, ''company'', 900001)',
    '23505'
);
SELECT pg_temp.expect_sqlstate('INSERT INTO users (first_name) VALUES (NULL)', '23502');
SELECT pg_temp.expect_sqlstate('UPDATE events SET city_id = 999 WHERE id = 900001', '23503');
SELECT pg_temp.expect_sqlstate('UPDATE event_participants SET company_id = 999 WHERE user_id = 900001', '23503');
SELECT pg_temp.expect_sqlstate('DELETE FROM users WHERE id = 900001', '23503');
SELECT pg_temp.expect_sqlstate('UPDATE events SET status = ''invalid''', '23514');
SELECT pg_temp.expect_sqlstate('UPDATE events SET latitude = 91, longitude = 30 WHERE id = 900001', '23514');
SELECT pg_temp.expect_sqlstate('UPDATE events SET latitude = 55, longitude = NULL WHERE id = 900001', '23514');
SELECT pg_temp.expect_sqlstate('UPDATE events SET location_source = ''invalid'' WHERE id = 900001', '23514');
SELECT pg_temp.expect_sqlstate('UPDATE companies SET status = ''invalid''', '23514');
SELECT pg_temp.expect_sqlstate('UPDATE companies SET join_type = ''invalid''', '23514');
SELECT pg_temp.expect_sqlstate('UPDATE companies SET max_members = 1', '23514');
SELECT pg_temp.expect_sqlstate('UPDATE company_members SET role = ''invalid''', '23514');
SELECT pg_temp.expect_sqlstate('UPDATE applications SET status = ''invalid''', '23514');
SELECT pg_temp.expect_sqlstate(
    'INSERT INTO applications (company_id, user_id, status) VALUES (900001, 900002, ''pending'')',
    '23505'
);
SELECT pg_temp.expect_sqlstate('UPDATE applications SET resolution_reason = ''INVALID''', '23514');
SELECT pg_temp.expect_sqlstate('UPDATE event_participants SET participation_type = ''invalid''', '23514');
SELECT pg_temp.expect_sqlstate('UPDATE event_participants SET company_id = 900001 WHERE user_id = 900002', '23514');
SELECT pg_temp.expect_sqlstate('UPDATE reports SET status = ''invalid''', '23514');
SELECT pg_temp.expect_sqlstate('UPDATE reports SET target_type = ''invalid''', '23514');

ROLLBACK;
