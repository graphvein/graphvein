-- T024c1b: routine bodies the PL/pgSQL subset reads (R6.P14): SELECT ...
-- INTO parameters and declared variables, a function call in FROM, a
-- variable named like an unreserved SQL keyword (content); a routine's
-- SET search_path (N2.P5); a trigger naming public.f for the set's
-- unqualified f (Y09). Every statement applies, so summaries is complete.
create function profile_counts(p uuid, out total int, out content text)
language plpgsql
security definer set search_path = public
as $$
begin
  select into total, content count(*)::int, max(body) from notes where notes.owner = p;
end;
$$;

create function profile_summary(p uuid)
returns text
language plpgsql
as $$
declare
  n int;
  content text;
begin
  select r.total, r.content into strict n, content from public.profile_counts(p) as r;
  return content || n::text;
end;
$$;

create function touch_profile() returns trigger language plpgsql as $$
begin
  return new;
end;
$$;

create trigger profiles_touch before update on profiles
  for each row execute function public.touch_profile();

create table summaries (id bigint primary key, body text);

-- SB-M7 (T024c1b R1): an insert into a platform table succeeds (SB-V5),
-- and no trigger of the set fires on it, so it is no opaque data change:
-- the CREATE TABLE IF NOT EXISTS after it is complete.
insert into storage.buckets (id, name, public) values ('avatars', 'avatars', true);
create table if not exists bucket_notes (bucket text primary key, note text check (char_length(note) <= 200));
create index bucket_notes_note_idx on bucket_notes (note);
