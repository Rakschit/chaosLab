-- Institution access requests and per-role limits.
-- Apply after 009_institution_roster.sql (Supabase SQL editor). Safe to re-run.

-- 1) A person whose email domain belongs to an institution, but who is not on
--    its roster, asks the institution admin to add them.
CREATE TABLE IF NOT EXISTS public.institution_access_requests (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    institution_id UUID        NOT NULL REFERENCES public.institutions(id) ON DELETE CASCADE,
    user_id        UUID        NOT NULL REFERENCES public.users(id) ON DELETE CASCADE,
    requested_role TEXT        NOT NULL CHECK (requested_role IN ('student', 'faculty')),
    message        TEXT        NOT NULL DEFAULT '' CHECK (char_length(message) <= 500),
    status         TEXT        NOT NULL DEFAULT 'pending'
                   CHECK (status IN ('pending', 'approved', 'rejected')),
    decision_note  TEXT        NOT NULL DEFAULT '' CHECK (char_length(decision_note) <= 500),
    decided_by     UUID        REFERENCES public.users(id) ON DELETE SET NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    decided_at     TIMESTAMPTZ
);

-- One pending request per user.
CREATE UNIQUE INDEX IF NOT EXISTS idx_institution_access_one_pending
    ON public.institution_access_requests (user_id) WHERE status = 'pending';

CREATE INDEX IF NOT EXISTS idx_institution_access_inst_status
    ON public.institution_access_requests (institution_id, status, created_at);

ALTER TABLE public.institution_access_requests ENABLE ROW LEVEL SECURITY;

-- 2) Limits chosen by the institution admin for students and faculty:
--    {"student":{"easy":50,"medium":30,"hard":10},"faculty":{"easy":200,...}}
--    Empty means "use institutions.monthly_limits". Those platform limits stay
--    the ceiling: a role limit can only lower them, never raise them.
ALTER TABLE public.institutions
    ADD COLUMN IF NOT EXISTS role_limits JSONB NOT NULL DEFAULT '{}'::jsonb;
