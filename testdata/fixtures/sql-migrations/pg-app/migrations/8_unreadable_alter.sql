-- Neither statement is valid against the fixture schema in Postgres; the
-- Postgres check skips this file.
ALTER TABLE members FROBNICATE;
ALTER TABLE public.members ADD COLUMN extra text;
