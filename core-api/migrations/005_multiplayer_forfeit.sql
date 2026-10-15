-- A player who presses "End simulation" in a running race is out of that race.
-- NULL = still playing (or solved); set = ended their own run.
-- Run in the Supabase SQL editor AFTER 004. Safe to re-run.
-- (core_api already has UPDATE on mp_players from 004, so no new grant needed.)
ALTER TABLE public.mp_players ADD COLUMN IF NOT EXISTS ended_at TIMESTAMPTZ;
