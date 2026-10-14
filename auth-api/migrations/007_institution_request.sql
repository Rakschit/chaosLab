-- Institution requests: a logged-in user asks the platform admin to register
-- their school or college email domain(s). Same idea as plan_requests (Pro).
-- Run in the Supabase SQL editor after 006_institutions.sql.

CREATE TABLE IF NOT EXISTS public.institution_requests (
    id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id          UUID        NOT NULL REFERENCES public.users(id) ON DELETE CASCADE,
    institution_name TEXT        NOT NULL CHECK (char_length(institution_name) BETWEEN 2 AND 120),
    -- Lowercase email domains, for example {sicsr.ac.in}. The requester's own
    -- email domain must be one of them (checked by the API).
    domains          TEXT[]      NOT NULL CHECK (cardinality(domains) BETWEEN 1 AND 5),
    seats_wanted     INTEGER     NOT NULL DEFAULT 0 CHECK (seats_wanted BETWEEN 0 AND 1000000),
    message          TEXT        NOT NULL DEFAULT '' CHECK (char_length(message) <= 500),
    status           TEXT        NOT NULL DEFAULT 'pending'
                     CHECK (status IN ('pending', 'approved', 'rejected')),
    decision_note    TEXT        NOT NULL DEFAULT '' CHECK (char_length(decision_note) <= 500),
    decided_by       UUID        REFERENCES public.users(id) ON DELETE SET NULL,
    -- The institution created when the request was approved.
    institution_id   UUID        REFERENCES public.institutions(id) ON DELETE SET NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    decided_at       TIMESTAMPTZ
);

-- A user can have only one pending request at a time.
CREATE UNIQUE INDEX IF NOT EXISTS idx_institution_requests_one_pending
    ON public.institution_requests (user_id) WHERE status = 'pending';

CREATE INDEX IF NOT EXISTS idx_institution_requests_status_created
    ON public.institution_requests (status, created_at);

ALTER TABLE public.institution_requests ENABLE ROW LEVEL SECURITY;