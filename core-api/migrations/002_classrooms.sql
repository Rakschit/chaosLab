-- Classroom mode. Run in the Supabase SQL editor AFTER 001_create_singleplayer_history.sql
-- (same database as auth-api; needs public.users and public.institutions).
-- Safe to re-run.

-- 1) A classroom: created by a faculty member, never expires.
--    It can only be archived, which stops new games but keeps every score.
CREATE TABLE IF NOT EXISTS public.classrooms (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    institution_id UUID        NOT NULL REFERENCES public.institutions(id) ON DELETE CASCADE,
    owner_id       UUID        NOT NULL REFERENCES public.users(id) ON DELETE CASCADE,
    name           TEXT        NOT NULL CHECK (char_length(name) BETWEEN 2 AND 80),
    owner_name     TEXT        NOT NULL DEFAULT '' CHECK (char_length(owner_name) <= 120),
    join_code      TEXT        NOT NULL UNIQUE CHECK (join_code ~ '^[A-Z0-9]{8}$'),
    environment    TEXT        NOT NULL DEFAULT 'nginx' CHECK (char_length(environment) <= 32),
    difficulty     TEXT        NOT NULL CHECK (difficulty IN ('easy', 'medium', 'hard')),
    -- How many games each student may start in this room.
    max_attempts   INTEGER     NOT NULL DEFAULT 1 CHECK (max_attempts BETWEEN 1 AND 10),
    -- The owner's monthly limits, refreshed every time the owner opens the
    -- classroom page. Student games are charged to the owner against these.
    owner_limits   JSONB       NOT NULL DEFAULT '{}'::jsonb,
    archived       BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_classrooms_owner ON public.classrooms (owner_id, created_at DESC);

-- 2) Who joined. Name and email are copied at join time so core-api never
--    needs to read public.users.
CREATE TABLE IF NOT EXISTS public.classroom_members (
    classroom_id UUID        NOT NULL REFERENCES public.classrooms(id) ON DELETE CASCADE,
    user_id      UUID        NOT NULL REFERENCES public.users(id) ON DELETE CASCADE,
    display_name TEXT        NOT NULL DEFAULT '' CHECK (char_length(display_name) <= 120),
    email        TEXT        NOT NULL DEFAULT '' CHECK (char_length(email) <= 254),
    joined_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (classroom_id, user_id)
);
CREATE INDEX IF NOT EXISTS idx_classroom_members_user ON public.classroom_members (user_id);

-- 3) Tag classroom games and results (NULL = a normal singleplayer game).
ALTER TABLE public.singleplayer_runs
    ADD COLUMN IF NOT EXISTS classroom_id UUID REFERENCES public.classrooms(id) ON DELETE CASCADE;
ALTER TABLE public.singleplayer_results
    ADD COLUMN IF NOT EXISTS classroom_id UUID REFERENCES public.classrooms(id) ON DELETE CASCADE;

CREATE INDEX IF NOT EXISTS idx_runs_classroom_user
    ON public.singleplayer_runs (classroom_id, user_id) WHERE classroom_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_results_classroom_user
    ON public.singleplayer_results (classroom_id, user_id, created_at DESC) WHERE classroom_id IS NOT NULL;

-- 4) Lock the new tables down and give core_api (created in 001) its access.
ALTER TABLE public.classrooms        ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.classroom_members ENABLE ROW LEVEL SECURITY;

GRANT SELECT, INSERT, UPDATE ON public.classrooms        TO core_api;
GRANT SELECT, INSERT         ON public.classroom_members TO core_api;

DROP POLICY IF EXISTS core_api_classrooms_select ON public.classrooms;
DROP POLICY IF EXISTS core_api_classrooms_insert ON public.classrooms;
DROP POLICY IF EXISTS core_api_classrooms_update ON public.classrooms;
CREATE POLICY core_api_classrooms_select ON public.classrooms
    FOR SELECT TO core_api USING (true);
CREATE POLICY core_api_classrooms_insert ON public.classrooms
    FOR INSERT TO core_api WITH CHECK (true);
CREATE POLICY core_api_classrooms_update ON public.classrooms
    FOR UPDATE TO core_api USING (true) WITH CHECK (true);

DROP POLICY IF EXISTS core_api_members_select ON public.classroom_members;
DROP POLICY IF EXISTS core_api_members_insert ON public.classroom_members;
CREATE POLICY core_api_members_select ON public.classroom_members
    FOR SELECT TO core_api USING (true);
CREATE POLICY core_api_members_insert ON public.classroom_members
    FOR INSERT TO core_api WITH CHECK (true);
