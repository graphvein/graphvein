-- Scratch stub of the objects a Supabase project provides before user
-- migrations run (auth, storage, roles, extensions schema). Oracle shim only.
create schema if not exists auth;
create schema if not exists storage;
create schema if not exists extensions;
do $$ begin
  if not exists (select 1 from pg_roles where rolname='anon') then create role anon; end if;
  if not exists (select 1 from pg_roles where rolname='authenticated') then create role authenticated; end if;
  if not exists (select 1 from pg_roles where rolname='service_role') then create role service_role; end if;
end $$;
create table auth.users (id uuid primary key, email text, raw_user_meta_data jsonb, raw_app_meta_data jsonb);
create function auth.uid() returns uuid language sql stable as $$ select null::uuid $$;
create function auth.role() returns text language sql stable as $$ select null::text $$;
create function auth.jwt() returns jsonb language sql stable as $$ select null::jsonb $$;
create table storage.buckets (id text primary key, name text, owner uuid, public boolean, created_at timestamptz, updated_at timestamptz, file_size_limit bigint, allowed_mime_types text[]);
create table storage.objects (id uuid primary key, bucket_id text, name text, owner uuid, owner_id text, metadata jsonb, path_tokens text[], version text, created_at timestamptz, updated_at timestamptz, last_accessed_at timestamptz);
create extension if not exists "uuid-ossp" with schema extensions;
create extension if not exists pgcrypto with schema extensions;
create function storage.foldername(name text) returns text[] language sql immutable as $$ select string_to_array(name, '/') $$;
create function storage.filename(name text) returns text language sql immutable as $$ select name $$;
create type extensions.http_header as (field varchar, value varchar);
create type extensions.http_request as (method text, uri varchar, headers extensions.http_header[], content_type varchar, content varchar);
create publication supabase_realtime;
