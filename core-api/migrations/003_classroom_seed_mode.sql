-- Classroom seed modes. Run AFTER 002_classrooms.sql. Safe to re-run.
--   random : every student gets a fresh random seed (the old behavior)
--   same   : one seed, chosen when the room is created, for every student
--   pool   : 2 or 3 seeds chosen at creation; each student gets a random one
--            they have not played yet
-- Existing rooms stay on 'random'.
ALTER TABLE public.classrooms
    ADD COLUMN IF NOT EXISTS seed_mode TEXT  NOT NULL DEFAULT 'random',
    ADD COLUMN IF NOT EXISTS seeds     JSONB NOT NULL DEFAULT '[]'::jsonb;

ALTER TABLE public.classrooms DROP CONSTRAINT IF EXISTS classrooms_seed_mode_check;
ALTER TABLE public.classrooms
    ADD CONSTRAINT classrooms_seed_mode_check CHECK (seed_mode IN ('random', 'same', 'pool'));
