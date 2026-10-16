CREATE TABLE IF NOT EXISTS public.mission_feedback (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES public.users(id) ON DELETE CASCADE,
    rating SMALLINT NOT NULL CHECK (rating BETWEEN 1 AND 5),
    difficulty TEXT NOT NULL DEFAULT '' CHECK (char_length(difficulty) <= 20),
    seed TEXT NOT NULL DEFAULT '' CHECK (char_length(seed) <= 64),
    score INTEGER CHECK (score IS NULL OR (score BETWEEN 0 AND 100)),
    comment TEXT NOT NULL DEFAULT '' CHECK (char_length(comment) <= 1000),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_mission_feedback_created
    ON public.mission_feedback(created_at DESC);

ALTER TABLE public.mission_feedback ENABLE ROW LEVEL SECURITY;
