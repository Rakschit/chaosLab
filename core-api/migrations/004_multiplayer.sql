-- Multiplayer races. Run in the Supabase SQL editor AFTER 001 (it creates the
-- core_api role) and 002. Same database as auth-api; needs public.users.
-- Safe to re-run.

-- 1) A room: one host, one shared incident (seed), a join code.
--    The seed is chosen by the server and never sent to players before the race
--    starts. status: waiting -> playing -> finished.
CREATE TABLE IF NOT EXISTS public.mp_rooms (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    code         TEXT        NOT NULL UNIQUE CHECK (code ~ '^[A-Z0-9]{6}$'),
    host_id      UUID        NOT NULL REFERENCES public.users(id) ON DELETE CASCADE,
    host_name    TEXT        NOT NULL DEFAULT '' CHECK (char_length(host_name) <= 120),
    environment  TEXT        NOT NULL DEFAULT 'nginx' CHECK (char_length(environment) <= 32),
    difficulty   TEXT        NOT NULL CHECK (difficulty IN ('easy', 'medium', 'hard')),
    seed         TEXT        NOT NULL CHECK (seed ~ '^[0-9a-f]{16}$'),
    max_players  INTEGER     NOT NULL CHECK (max_players BETWEEN 2 AND 8),
    is_public    BOOLEAN     NOT NULL DEFAULT FALSE,
    status       TEXT        NOT NULL DEFAULT 'waiting' CHECK (status IN ('waiting', 'playing', 'finished')),
    time_limit_s INTEGER     NOT NULL CHECK (time_limit_s BETWEEN 60 AND 7200),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- When the race clock starts (a few seconds after the host presses start).
    started_at   TIMESTAMPTZ,
    finished_at  TIMESTAMPTZ,
    -- A waiting room is dropped after 2 hours; a running one when its time is up.
    expires_at   TIMESTAMPTZ NOT NULL DEFAULT (NOW() + INTERVAL '2 hours')
);
CREATE INDEX IF NOT EXISTS idx_mp_rooms_host_active
    ON public.mp_rooms (host_id) WHERE status <> 'finished';
CREATE INDEX IF NOT EXISTS idx_mp_rooms_public_waiting
    ON public.mp_rooms (created_at DESC) WHERE status = 'waiting' AND is_public;

-- 2) Who is in a room and how they are doing. The best score is kept; the
--    finish time is set once, by the first fully correct submission.
CREATE TABLE IF NOT EXISTS public.mp_players (
    room_id      UUID        NOT NULL REFERENCES public.mp_rooms(id) ON DELETE CASCADE,
    user_id      UUID        NOT NULL REFERENCES public.users(id) ON DELETE CASCADE,
    display_name TEXT        NOT NULL DEFAULT '' CHECK (char_length(display_name) <= 120),
    joined_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    attempts     INTEGER     NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    best_score   INTEGER     NOT NULL DEFAULT 0 CHECK (best_score BETWEEN 0 AND 100),
    matched      INTEGER     NOT NULL DEFAULT 0 CHECK (matched >= 0),
    total        INTEGER     NOT NULL DEFAULT 0 CHECK (total >= 0),
    solved_at    TIMESTAMPTZ,
    PRIMARY KEY (room_id, user_id)
);
CREATE INDEX IF NOT EXISTS idx_mp_players_user ON public.mp_players (user_id);

-- 3) Lock the tables down (same pattern as the other tables).
ALTER TABLE public.mp_rooms   ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.mp_players ENABLE ROW LEVEL SECURITY;

GRANT SELECT, INSERT, UPDATE         ON public.mp_rooms   TO core_api;
GRANT SELECT, INSERT, UPDATE, DELETE ON public.mp_players TO core_api;

DROP POLICY IF EXISTS core_api_mp_rooms_select ON public.mp_rooms;
DROP POLICY IF EXISTS core_api_mp_rooms_insert ON public.mp_rooms;
DROP POLICY IF EXISTS core_api_mp_rooms_update ON public.mp_rooms;
CREATE POLICY core_api_mp_rooms_select ON public.mp_rooms
    FOR SELECT TO core_api USING (true);
CREATE POLICY core_api_mp_rooms_insert ON public.mp_rooms
    FOR INSERT TO core_api WITH CHECK (true);
CREATE POLICY core_api_mp_rooms_update ON public.mp_rooms
    FOR UPDATE TO core_api USING (true) WITH CHECK (true);

DROP POLICY IF EXISTS core_api_mp_players_select ON public.mp_players;
DROP POLICY IF EXISTS core_api_mp_players_insert ON public.mp_players;
DROP POLICY IF EXISTS core_api_mp_players_update ON public.mp_players;
DROP POLICY IF EXISTS core_api_mp_players_delete ON public.mp_players;
CREATE POLICY core_api_mp_players_select ON public.mp_players
    FOR SELECT TO core_api USING (true);
CREATE POLICY core_api_mp_players_insert ON public.mp_players
    FOR INSERT TO core_api WITH CHECK (true);
CREATE POLICY core_api_mp_players_update ON public.mp_players
    FOR UPDATE TO core_api USING (true) WITH CHECK (true);
CREATE POLICY core_api_mp_players_delete ON public.mp_players
    FOR DELETE TO core_api USING (true);
