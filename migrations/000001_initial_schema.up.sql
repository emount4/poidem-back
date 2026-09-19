BEGIN;

CREATE TABLE cities (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(150) NOT NULL,
    slug VARCHAR(150) UNIQUE
);

CREATE TABLE interests (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    slug VARCHAR(100) UNIQUE
);

CREATE TABLE event_categories (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    slug VARCHAR(100) UNIQUE
);

CREATE TABLE users (
    id BIGSERIAL PRIMARY KEY,
    first_name VARCHAR(100) NOT NULL,
    last_name VARCHAR(100),
    avatar_url TEXT,
    city_id BIGINT REFERENCES cities(id),
    about TEXT,
    role VARCHAR(20) DEFAULT 'user',
    status VARCHAR(20) DEFAULT 'active',
    created_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ
);

CREATE TABLE auth_accounts (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT REFERENCES users(id),
    provider VARCHAR(30),
    provider_user_id VARCHAR(255),
    created_at TIMESTAMPTZ,
    CONSTRAINT uq_auth_accounts_provider UNIQUE (provider, provider_user_id)
);

CREATE TABLE user_interests (
    user_id BIGINT REFERENCES users(id),
    interest_id BIGINT REFERENCES interests(id),
    PRIMARY KEY (user_id, interest_id)
);

CREATE TABLE events (
    id BIGSERIAL PRIMARY KEY,
    creator_id BIGINT REFERENCES users(id),
    category_id BIGINT REFERENCES event_categories(id),
    city_id BIGINT REFERENCES cities(id),
    title VARCHAR(200) NOT NULL,
    description TEXT,
    image_url TEXT,
    starts_at TIMESTAMPTZ NOT NULL,
    ends_at TIMESTAMPTZ,
    location_name VARCHAR(255) NOT NULL,
    address VARCHAR(500),
    status VARCHAR(20),
    created_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ,
    CONSTRAINT chk_events_status CHECK (status IN ('pending', 'active', 'rejected', 'blocked', 'completed'))
);

CREATE TABLE companies (
    id BIGSERIAL PRIMARY KEY,
    event_id BIGINT REFERENCES events(id),
    owner_id BIGINT REFERENCES users(id),
    name VARCHAR(150) NOT NULL,
    description TEXT,
    max_members INT NOT NULL,
    join_type VARCHAR(20),
    rules TEXT,
    status VARCHAR(20),
    created_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ,
    CONSTRAINT chk_companies_join_type CHECK (join_type IN ('open', 'request')),
    CONSTRAINT chk_companies_status CHECK (status IN ('active', 'closed', 'blocked'))
);

CREATE TABLE company_members (
    company_id BIGINT REFERENCES companies(id),
    user_id BIGINT REFERENCES users(id),
    role VARCHAR(20),
    joined_at TIMESTAMPTZ,
    PRIMARY KEY (company_id, user_id)
);

CREATE TABLE applications (
    id BIGSERIAL PRIMARY KEY,
    company_id BIGINT REFERENCES companies(id),
    user_id BIGINT REFERENCES users(id),
    message TEXT,
    status VARCHAR(20),
    created_at TIMESTAMPTZ,
    resolved_at TIMESTAMPTZ,
    CONSTRAINT chk_applications_status CHECK (status IN ('pending', 'approved', 'rejected', 'cancelled'))
);

CREATE TABLE event_participants (
    event_id BIGINT REFERENCES events(id),
    user_id BIGINT REFERENCES users(id),
    participation_type VARCHAR(20),
    company_id BIGINT REFERENCES companies(id),
    joined_at TIMESTAMPTZ,
    PRIMARY KEY (event_id, user_id),
    CONSTRAINT chk_event_participants_type CHECK (participation_type IN ('solo', 'company'))
);

CREATE TABLE reports (
    id BIGSERIAL PRIMARY KEY,
    author_id BIGINT REFERENCES users(id),
    target_type VARCHAR(20),
    target_id BIGINT,
    reason VARCHAR(100),
    description TEXT,
    status VARCHAR(20),
    resolved_by BIGINT REFERENCES users(id),
    created_at TIMESTAMPTZ,
    resolved_at TIMESTAMPTZ,
    CONSTRAINT chk_reports_target_type CHECK (target_type IN ('user', 'company', 'event')),
    CONSTRAINT chk_reports_status CHECK (status IN ('pending', 'resolved', 'rejected'))
);

CREATE INDEX idx_events_city_start ON events(city_id, starts_at);
CREATE INDEX idx_events_category ON events(category_id);
CREATE INDEX idx_events_status ON events(status);
CREATE INDEX idx_companies_event ON companies(event_id);
CREATE INDEX idx_companies_owner ON companies(owner_id);
CREATE INDEX idx_applications_company_status ON applications(company_id, status);
CREATE INDEX idx_applications_user ON applications(user_id);
CREATE INDEX idx_event_participants_user ON event_participants(user_id);
CREATE INDEX idx_reports_status ON reports(status);

COMMIT;
