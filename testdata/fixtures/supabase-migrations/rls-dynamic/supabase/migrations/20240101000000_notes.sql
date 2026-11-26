-- 2026-11-18 (SB-M9): code the pass cannot read may create a rule.
-- The DO block's dynamic EXECUTE creates a rule whose actions are a
-- superuser's writes to pg_catalog.pg_class and pg_catalog.pg_policy;
-- the INSERT at the end fires it. The set defines no routine, and the
-- rule's text is a string the pass does not read, so only the DO block's
-- unbounded code tells: SB-M9 ends for the set, and the rls flags and the
-- policies of every table, notes created after the block among them, stay
-- unknown. On the Supabase platform the postgres role is not a superuser
-- and the INSERT is refused; the test replays the set as a superuser.
create table public.events (
  id bigint primary key
);

do $$
begin
  execute 'create rule events_audit as on insert to public.events do also ('
    || 'update pg_catalog.pg_class set relrowsecurity = false'
    || ' where relname = ''notes'' and relnamespace = ''public''::regnamespace;'
    || ' delete from pg_catalog.pg_policy where polname = ''notes_read'')';
end
$$;

create table public.notes (
  id bigint primary key,
  body text
);

alter table public.notes enable row level security;

create policy notes_read on public.notes for select using (true);

insert into public.events (id) values (1);
