-- Store requested token-mode configuration until the platform admin approves it.
ALTER TABLE public.institution_requests
    ADD COLUMN IF NOT EXISTS auth_mode TEXT NOT NULL DEFAULT 'domain'
        CHECK (auth_mode IN ('domain', 'token')),
    ADD COLUMN IF NOT EXISTS login_url TEXT NOT NULL DEFAULT ''
        CHECK (char_length(login_url) <= 500),
    ADD COLUMN IF NOT EXISTS public_key TEXT NOT NULL DEFAULT ''
        CHECK (char_length(public_key) <= 4096);
