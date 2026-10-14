-- Institution-controlled roster. Apply after 008_institution_request_token_mode.sql.
CREATE TABLE IF NOT EXISTS public.institution_roster (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    institution_id UUID NOT NULL REFERENCES public.institutions(id) ON DELETE CASCADE,
    -- HMAC-SHA256 of normalized email; raw email is never stored here.
    email_hash TEXT,
    -- Opaque identifier supplied by the institution; used as token sub.
    external_id TEXT,
    roster_role TEXT NOT NULL CHECK (roster_role IN ('student', 'faculty')),
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (email_hash IS NOT NULL OR external_id IS NOT NULL),
    UNIQUE (institution_id, email_hash),
    UNIQUE (institution_id, external_id)
);
CREATE INDEX IF NOT EXISTS idx_institution_roster_active
    ON public.institution_roster (institution_id, active);

ALTER TABLE public.institutions
    ADD COLUMN IF NOT EXISTS roster_enforced BOOLEAN NOT NULL DEFAULT FALSE;

CREATE TABLE IF NOT EXISTS public.institution_audit (
    id BIGSERIAL PRIMARY KEY,
    institution_id UUID NOT NULL REFERENCES public.institutions(id) ON DELETE CASCADE,
    actor_user_id UUID REFERENCES public.users(id) ON DELETE SET NULL,
    action TEXT NOT NULL,
    target_hash TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_institution_audit_inst_created
    ON public.institution_audit (institution_id, created_at DESC);

ALTER TABLE public.institution_roster ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.institution_audit ENABLE ROW LEVEL SECURITY;
