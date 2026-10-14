-- Role: who may review plan requests. Everyone is a normal user by default.
ALTER TABLE public.users
    ADD COLUMN IF NOT EXISTS role TEXT NOT NULL DEFAULT 'user';

ALTER TABLE public.users DROP CONSTRAINT IF EXISTS users_role_check;
ALTER TABLE public.users
    ADD CONSTRAINT users_role_check CHECK (role IN ('user', 'admin'));

-- Plan: free by default, pro only through an approved request.
-- If this fails, some rows hold another value; fix them first with:
--   UPDATE public.users SET plan = 'free' WHERE plan NOT IN ('free', 'pro');
ALTER TABLE public.users ALTER COLUMN plan SET DEFAULT 'free';
ALTER TABLE public.users DROP CONSTRAINT IF EXISTS users_plan_check;
ALTER TABLE public.users
    ADD CONSTRAINT users_plan_check CHECK (plan IN ('free', 'pro'));

-- Create the first admin BY HAND (never through the API), for example:
--   UPDATE public.users SET role = 'admin' WHERE email = 'you@example.com';
