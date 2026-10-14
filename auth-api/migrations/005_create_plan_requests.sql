CREATE TABLE IF NOT EXISTS public.plan_requests (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES public.users(id) ON DELETE CASCADE,
    requested_plan TEXT NOT NULL DEFAULT 'pro' CHECK (requested_plan = 'pro'),
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'approved', 'rejected')),
    message TEXT NOT NULL DEFAULT '' CHECK (char_length(message) <= 500),
    decision_note TEXT NOT NULL DEFAULT '' CHECK (char_length(decision_note) <= 500),
    decided_by UUID REFERENCES public.users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    decided_at TIMESTAMPTZ
);

-- A user can have only one pending request at a time.
CREATE UNIQUE INDEX IF NOT EXISTS idx_plan_requests_one_pending
    ON public.plan_requests(user_id) WHERE status = 'pending';

CREATE INDEX IF NOT EXISTS idx_plan_requests_status_created
    ON public.plan_requests(status, created_at);

ALTER TABLE public.plan_requests ENABLE ROW LEVEL SECURITY;
