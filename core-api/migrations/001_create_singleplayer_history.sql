-- Run this in the Supabase SQL editor (same database as auth-api).
-- Needs the public.users table from auth-api's migrations.

-- 1) When each simulation was created, so submit can measure the time taken.
--    One row per user + simulation; creating the same simulation again restarts the clock.
CREATE TABLE IF NOT EXISTS public.singleplayer_runs (
    user_id     UUID        NOT NULL REFERENCES public.users(id) ON DELETE CASCADE,
    seed        TEXT        NOT NULL CHECK (seed ~ '^[0-9a-f]{16}$'),
    environment TEXT        NOT NULL CHECK (char_length(environment) <= 32),
    difficulty  TEXT        NOT NULL CHECK (char_length(difficulty) <= 32),
    started_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, seed, environment, difficulty)
);

-- 2) One row per graded submission.
CREATE TABLE IF NOT EXISTS public.singleplayer_results (
    id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id          UUID        NOT NULL REFERENCES public.users(id) ON DELETE CASCADE,
    seed             TEXT        NOT NULL CHECK (seed ~ '^[0-9a-f]{16}$'),
    environment      TEXT        NOT NULL CHECK (char_length(environment) <= 32),
    difficulty       TEXT        NOT NULL CHECK (char_length(difficulty) <= 32),
    attempt_number   INTEGER     NOT NULL CHECK (attempt_number >= 1),
    score            INTEGER     NOT NULL CHECK (score BETWEEN 0 AND 100),
    matched          INTEGER     NOT NULL CHECK (matched >= 0),
    total            INTEGER     NOT NULL CHECK (total >= 1),
    correct          BOOLEAN     NOT NULL,
    findings         JSONB       NOT NULL DEFAULT '[]'::jsonb,
    started_at       TIMESTAMPTZ,
    duration_seconds INTEGER     CHECK (duration_seconds >= 0),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- Two simultaneous submits can never get the same attempt number.
    UNIQUE (user_id, seed, attempt_number)
);

CREATE INDEX IF NOT EXISTS idx_singleplayer_results_user_created
    ON public.singleplayer_results (user_id, created_at DESC);

-- 3) Lock the tables down. With RLS on and no policy for anon/authenticated,
--    Supabase's public API cannot read or write them.
ALTER TABLE public.singleplayer_runs    ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.singleplayer_results ENABLE ROW LEVEL SECURITY;

-- 4) A limited login for core-api. EDIT THE PASSWORD before running this line,
--    then use it in core-api's DATABASE_URL. (Skip if the role already exists.)
CREATE ROLE core_api LOGIN PASSWORD 'CHANGE_ME_TO_A_LONG_RANDOM_PASSWORD';

GRANT USAGE ON SCHEMA public TO core_api;
GRANT SELECT, INSERT         ON public.singleplayer_results TO core_api;
GRANT SELECT, INSERT, UPDATE ON public.singleplayer_runs    TO core_api;

DROP POLICY IF EXISTS core_api_results_select ON public.singleplayer_results;
DROP POLICY IF EXISTS core_api_results_insert ON public.singleplayer_results;
CREATE POLICY core_api_results_select ON public.singleplayer_results
    FOR SELECT TO core_api USING (true);
CREATE POLICY core_api_results_insert ON public.singleplayer_results
    FOR INSERT TO core_api WITH CHECK (true);

DROP POLICY IF EXISTS core_api_runs_select ON public.singleplayer_runs;
DROP POLICY IF EXISTS core_api_runs_insert ON public.singleplayer_runs;
DROP POLICY IF EXISTS core_api_runs_update ON public.singleplayer_runs;
CREATE POLICY core_api_runs_select ON public.singleplayer_runs
    FOR SELECT TO core_api USING (true);
CREATE POLICY core_api_runs_insert ON public.singleplayer_runs
    FOR INSERT TO core_api WITH CHECK (true);
CREATE POLICY core_api_runs_update ON public.singleplayer_runs
    FOR UPDATE TO core_api USING (true) WITH CHECK (true);
