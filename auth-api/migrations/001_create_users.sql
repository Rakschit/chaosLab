-- Base users table for auth-api.
-- Run first (Supabase SQL editor), before 002_create_email_verification_tokens.sql.
-- Safe to re-run.
--
-- Columns added by later migrations are intentionally NOT here:
--   004_add_user_role.sql      -> role (and the plan check constraint)
--   006_institutions.sql       -> institution_id, external_id, account_type
--                                 (and the final plan/role check constraints)

CREATE TABLE IF NOT EXISTS public.users (
    id                UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    -- The API lowercases and trims the email (max 254 chars) before every lookup.
    email             TEXT        NOT NULL UNIQUE CHECK (char_length(email) BETWEEN 3 AND 254),
    -- bcrypt hash. Institution participants get a fixed unusable placeholder.
    password_hash     TEXT        NOT NULL,
    -- The API allows 1-100 characters.
    display_name      TEXT        NOT NULL CHECK (char_length(display_name) BETWEEN 1 AND 100),
    -- Tightened to ('free','pro') in 004 and ('free','pro','institution') in 006.
    plan              TEXT        NOT NULL DEFAULT 'free',
    -- New sign-ups start as 'pending_verification' and become 'active' when the
    -- email link is used. Anything else (e.g. 'suspended') is blocked at login.
    status            TEXT        NOT NULL DEFAULT 'pending_verification'
                      CHECK (status IN ('pending_verification', 'active', 'suspended')),
    email_verified_at TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Lookups by status (e.g. cleaning up unverified accounts).
CREATE INDEX IF NOT EXISTS idx_users_status
    ON public.users (status);

-- Lock the table down like the others (the API connects with its own role).
ALTER TABLE public.users ENABLE ROW LEVEL SECURITY;
