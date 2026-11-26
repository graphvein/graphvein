-- Stacked gate NX-S (SB-M9): a rule's actions run with every INSERT,
-- UPDATE or DELETE on its table, and the pass does not read them. This
-- rule's actions are a superuser's writes to pg_catalog.pg_class and
-- pg_catalog.pg_policy, which change row level security without any
-- statement SB-M3 reads. After CREATE RULE, the rls flags and the
-- policies of every held table are unknown, and every later data
-- statement makes them unknown again (notes is created after the rule).
-- On the Supabase platform the postgres role is not a superuser and the
-- INSERT is refused; the test replays the set as a superuser.
create table public.events (
  id bigint primary key
);

create rule events_audit as on insert to public.events do also (
  update pg_catalog.pg_class set relrowsecurity = false
    where relname = 'notes' and relnamespace = 'public'::regnamespace;
  delete from pg_catalog.pg_policy where polname = 'notes_read'
);

create table public.notes (
  id bigint primary key,
  body text
);

alter table public.notes enable row level security;

create policy notes_read on public.notes for select using (true);

insert into public.events (id) values (1);
