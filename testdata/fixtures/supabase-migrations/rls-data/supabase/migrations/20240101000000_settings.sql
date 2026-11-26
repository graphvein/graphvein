-- NX-F2 (SB-M9): the set defines code (a trigger function and its
-- trigger), so the UPDATE below is opaque (O1: a trigger it fires may run
-- any statement). Every routine of the set is read and none runs a
-- statement, EXECUTE or setting, so the UPDATE cannot change row level
-- security: the rls flags of both tables stay known (oracle-checked).
create table public.events (
  id bigint primary key,
  at timestamptz
);

create function public.touch_event() returns trigger
language plpgsql
as $$
begin
  new.at = now();
  return new;
end;
$$;

create trigger events_touch before update on public.events
  for each row execute function public.touch_event();

create table public.settings (
  id bigint primary key,
  value text not null
);

alter table public.settings enable row level security;

update public.settings set value = 'b' where id = 1;
