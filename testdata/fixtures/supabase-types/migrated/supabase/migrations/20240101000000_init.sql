create table public.teams (
  id bigint generated always as identity primary key,
  name text not null
);

create table public.members (
  id uuid primary key,
  team_id bigint references public.teams (id),
  email text,
  role text not null
);

create table public.audit (
  id bigint primary key,
  note text
);
