BEGIN;

ALTER TABLE companies
    ADD COLUMN deleted_at TIMESTAMPTZ;

UPDATE companies
SET join_type = COALESCE(join_type, 'request'),
    status = COALESCE(status, 'active'),
    created_at = COALESCE(created_at, now()),
    updated_at = COALESCE(updated_at, created_at, now());

ALTER TABLE companies
    ALTER COLUMN event_id SET NOT NULL,
    ALTER COLUMN owner_id SET NOT NULL,
    ALTER COLUMN join_type SET DEFAULT 'request',
    ALTER COLUMN join_type SET NOT NULL,
    ALTER COLUMN status SET DEFAULT 'active',
    ALTER COLUMN status SET NOT NULL,
    ALTER COLUMN created_at SET DEFAULT now(),
    ALTER COLUMN created_at SET NOT NULL,
    ALTER COLUMN updated_at SET DEFAULT now(),
    ALTER COLUMN updated_at SET NOT NULL,
    ADD CONSTRAINT chk_companies_max_members CHECK (max_members BETWEEN 2 AND 100),
    ADD CONSTRAINT uq_companies_id_event UNIQUE (id, event_id);

UPDATE company_members cm
SET role = CASE WHEN c.owner_id = cm.user_id THEN 'owner' ELSE 'member' END,
    joined_at = COALESCE(cm.joined_at, now())
FROM companies c
WHERE c.id = cm.company_id;

INSERT INTO company_members (company_id, user_id, role, joined_at)
SELECT id, owner_id, 'owner', created_at
FROM companies
ON CONFLICT (company_id, user_id) DO UPDATE
SET role = 'owner',
    joined_at = COALESCE(company_members.joined_at, EXCLUDED.joined_at);

ALTER TABLE company_members
    ALTER COLUMN company_id SET NOT NULL,
    ALTER COLUMN user_id SET NOT NULL,
    ALTER COLUMN role SET DEFAULT 'member',
    ALTER COLUMN role SET NOT NULL,
    ALTER COLUMN joined_at SET DEFAULT now(),
    ALTER COLUMN joined_at SET NOT NULL,
    ADD CONSTRAINT chk_company_members_role CHECK (role IN ('owner', 'member'));

UPDATE event_participants
SET joined_at = COALESCE(joined_at, now());

ALTER TABLE event_participants
    ALTER COLUMN event_id SET NOT NULL,
    ALTER COLUMN user_id SET NOT NULL,
    ALTER COLUMN participation_type SET NOT NULL,
    ALTER COLUMN joined_at SET DEFAULT now(),
    ALTER COLUMN joined_at SET NOT NULL,
    ADD CONSTRAINT chk_event_participants_company CHECK (
        (participation_type = 'solo' AND company_id IS NULL)
        OR (participation_type = 'company' AND company_id IS NOT NULL)
    ),
    ADD CONSTRAINT fk_event_participants_company_event
        FOREIGN KEY (company_id, event_id) REFERENCES companies(id, event_id);

CREATE INDEX idx_companies_event_visible
    ON companies(event_id, created_at, id)
    WHERE deleted_at IS NULL;

CREATE INDEX idx_company_members_user_joined
    ON company_members(user_id, joined_at, company_id);

COMMIT;
