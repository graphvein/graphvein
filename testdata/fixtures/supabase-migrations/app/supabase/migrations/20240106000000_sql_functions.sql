-- T024c1c: LANGUAGE sql bodies that PostgreSQL parse-analyses at
-- creation (check_function_bodies on), proven by R1b.P5's analysed form;
-- the storage functions are the platform's (SB-M8, assumption SB-V2).

create table attachments (
  id uuid primary key default gen_random_uuid(),
  owner_id uuid not null references auth.users (id) on delete cascade,
  path text not null,
  sharing text not null default 'private'
);

create or replace function public.attachment_shared(p_name text)
returns boolean
language sql
security definer
as $$
  select exists (
    select 1
    from attachments
    where (id::text = (storage.foldername(p_name))[2]) and sharing <> 'private'
  );
$$;

create function attachment_named(p_name text)
returns boolean
language sql
stable
as $$
  select exists (select 1 from attachments as a where a.path = storage.filename(p_name))
$$;

create function attachment_path(p_id uuid)
returns text
language sql
stable
as $$ select path from attachments where id = p_id $$;

create index attachments_owner_id_idx on attachments (owner_id);
