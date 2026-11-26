-- T024 (SB-M9): a superuser's writes to pg_catalog.pg_class and
-- pg_catalog.pg_policy change row level security without any statement
-- SB-M3 reads. A data statement on a relation the set does not hold makes
-- the rls flags and the policies of every held table unknown; SB-M9 keeps
-- them known only across data statements on held tables. On the Supabase
-- platform the postgres role is not a superuser and both writes are
-- refused; the oracle replays the set as a superuser.
create table public.notes (
  id bigint primary key,
  body text
);

alter table public.notes enable row level security;

create policy notes_read on public.notes for select using (true);

create function public.touch_note() returns trigger
language plpgsql
as $$
begin
  return new;
end;
$$;

create trigger notes_touch before insert on public.notes
  for each row execute function public.touch_note();

update pg_catalog.pg_class set relrowsecurity = false
  where oid = 'public.notes'::regclass;

delete from pg_catalog.pg_policy where polname = 'notes_read';
