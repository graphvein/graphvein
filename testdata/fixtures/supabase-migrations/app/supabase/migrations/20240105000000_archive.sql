-- T024: a policy's table follows a rename of the table, as
-- pg_policies does (the policy below is on public.archive_old).
create table public.archive (id bigint primary key);
alter table public.archive enable row level security;
create policy "archive read" on public.archive for select using (true);
alter table public.archive rename to archive_old;
