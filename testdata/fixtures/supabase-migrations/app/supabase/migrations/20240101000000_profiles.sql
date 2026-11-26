-- SB-M2: a key of a platform table (auth.users, assumption SB-V2).
create table profiles (
  id uuid primary key references auth.users (id) on delete cascade,
  username text unique,
  avatar_path text
);

-- SB-M3: row level security and policies are schema-neutral.
alter table profiles enable row level security;

create policy "Profiles are public" on profiles for select using (true);
create policy "Own profile" on profiles for update to authenticated
  using (auth.uid() = id) with check (auth.uid() = id);
create policy "Insert own" on profiles as permissive for insert to authenticated
  with check (auth.uid() = id);

-- A policy on a table the set does not hold: as an unknown reference.
create policy "Avatar uploads" on storage.objects for insert to authenticated
  with check (bucket_id = 'avatars' and (storage.foldername(name))[1] = auth.uid()::text);
