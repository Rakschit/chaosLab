-- 1) Host can remove a player from a room, and that player cannot rejoin it.
--    Run in the Supabase SQL editor AFTER 004 and 005. Safe to re-run.
CREATE TABLE IF NOT EXISTS public.mp_kicks (
    room_id   UUID        NOT NULL REFERENCES public.mp_rooms(id) ON DELETE CASCADE,
    user_id   UUID        NOT NULL REFERENCES public.users(id)    ON DELETE CASCADE,
    kicked_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (room_id, user_id)
);

ALTER TABLE public.mp_kicks ENABLE ROW LEVEL SECURITY;
GRANT SELECT, INSERT ON public.mp_kicks TO core_api;

DROP POLICY IF EXISTS core_api_mp_kicks_select ON public.mp_kicks;
DROP POLICY IF EXISTS core_api_mp_kicks_insert ON public.mp_kicks;
CREATE POLICY core_api_mp_kicks_select ON public.mp_kicks
    FOR SELECT TO core_api USING (true);
CREATE POLICY core_api_mp_kicks_insert ON public.mp_kicks
    FOR INSERT TO core_api WITH CHECK (true);

-- 2) Multiplayer answers go into the same history table as singleplayer ones.
--    room_code is NULL for a normal game. (core_api already has SELECT/INSERT on
--    singleplayer_results from 001, so no new grant is needed.)
ALTER TABLE public.singleplayer_results
    ADD COLUMN IF NOT EXISTS room_code TEXT CHECK (room_code IS NULL OR room_code ~ '^[A-Z0-9]{6}$');
