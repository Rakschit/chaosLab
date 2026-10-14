-- Institutions: a school or college (e.g. SICSR) that gets its own plan.
-- Run in the Supabase SQL editor (same database as the other migrations).

-- 1) The institution itself.
CREATE TABLE IF NOT EXISTS public.institutions (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    name           TEXT        NOT NULL CHECK (char_length(name) BETWEEN 2 AND 120),
    slug           TEXT        NOT NULL UNIQUE CHECK (slug ~ '^[a-z0-9][a-z0-9-]{1,39}$'),
    status         TEXT        NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'suspended')),
    -- 'token'  : the institution hosts the login and sends back a signed token.
    -- 'domain' : students register with your accounts using the institution email domain.
    auth_mode      TEXT        NOT NULL DEFAULT 'domain' CHECK (auth_mode IN ('token', 'domain')),
    login_url      TEXT        NOT NULL DEFAULT '' CHECK (char_length(login_url) <= 500),
    public_key     TEXT        NOT NULL DEFAULT '' CHECK (char_length(public_key) <= 4096),
    -- Games per month by difficulty, for every participant of this institution.
    monthly_limits JSONB       NOT NULL DEFAULT '{"easy":200,"medium":150,"hard":100}'::jsonb,
    max_seats      INTEGER     NOT NULL DEFAULT 0 CHECK (max_seats >= 0),   -- 0 = unlimited
    contact_email  TEXT        NOT NULL DEFAULT '' CHECK (char_length(contact_email) <= 254),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 2) Email domains that belong to an institution (used by the 'domain' method).
CREATE TABLE IF NOT EXISTS public.institution_domains (
    domain         TEXT        PRIMARY KEY CHECK (domain = lower(domain) AND char_length(domain) <= 253),
    institution_id UUID        NOT NULL REFERENCES public.institutions(id) ON DELETE CASCADE,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_institution_domains_institution
    ON public.institution_domains (institution_id);

-- 3) Licenses. The table may already exist (id, license_email, license_key_hash,
--    status, issued_at, expires_at, created_at); this keeps it and adds the link.
CREATE TABLE IF NOT EXISTS public.institution_licenses (
    id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    license_email    TEXT        NOT NULL,
    license_key_hash TEXT,
    status           TEXT        NOT NULL DEFAULT 'active',
    issued_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at       TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
ALTER TABLE public.institution_licenses
    ADD COLUMN IF NOT EXISTS institution_id UUID REFERENCES public.institutions(id) ON DELETE CASCADE;
CREATE INDEX IF NOT EXISTS idx_institution_licenses_institution
    ON public.institution_licenses (institution_id, status);

-- 4) Users: who belongs to which institution.
ALTER TABLE public.users
    ADD COLUMN IF NOT EXISTS institution_id UUID REFERENCES public.institutions(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS external_id    TEXT,
    ADD COLUMN IF NOT EXISTS account_type   TEXT NOT NULL DEFAULT 'user';

ALTER TABLE public.users DROP CONSTRAINT IF EXISTS users_account_type_check;
ALTER TABLE public.users
    ADD CONSTRAINT users_account_type_check CHECK (account_type IN ('user', 'participant'));

-- One participant ID per institution.
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_institution_external
    ON public.users (institution_id, external_id) WHERE external_id IS NOT NULL;

-- Allow the new plan and role values.
ALTER TABLE public.users DROP CONSTRAINT IF EXISTS users_plan_check;
ALTER TABLE public.users
    ADD CONSTRAINT users_plan_check CHECK (plan IN ('free', 'pro', 'institution'));

ALTER TABLE public.users DROP CONSTRAINT IF EXISTS users_role_check;
ALTER TABLE public.users
    ADD CONSTRAINT users_role_check CHECK (role IN ('user', 'admin', 'institution_admin'));

-- 5) Lock the tables down like the others (the API connects with its own role).
ALTER TABLE public.institutions         ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.institution_domains  ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.institution_licenses ENABLE ROW LEVEL SECURITY;
