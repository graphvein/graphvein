-- SB-M4: extensions the Supabase platform lets the migration role create
-- (assumption SB-V4): not superuser-only here, so the migration applies.
create extension if not exists vector with schema extensions;
create extension http with schema extensions;
-- SB-M6 (T024c1b R1): with WITH SCHEMA the extensions' effects are known
-- (no relation), so the CREATE TABLE IF NOT EXISTS after them is complete.
create table if not exists embeddings_meta (id integer primary key, label text);
