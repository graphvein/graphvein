create table notes (
  id bigint generated always as identity primary key,
  owner uuid not null references auth.users,
  body text
);

alter table notes enable row level security;
alter table notes force row level security;

create policy "tmp" on notes using (owner = auth.uid());
alter policy "tmp" on notes rename to "Owner access";
-- Another ALTER POLICY form keeps the policy.
alter policy "Owner access" on notes to authenticated;
create policy "Drop me" on notes for delete using (false);
drop policy "Drop me" on notes;
drop policy if exists "Never created" on notes;
