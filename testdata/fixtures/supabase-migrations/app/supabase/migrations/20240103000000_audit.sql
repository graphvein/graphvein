create table audit_log (
  id bigserial primary key,
  profile uuid references profiles (id),
  bucket text references storage.buckets (id),
  object uuid references storage.objects
);

alter table audit_log enable row level security;
alter table audit_log disable row level security;
alter table notes no force row level security;

-- The name may mean audit_log under another qualification: the pass does
-- not equate public.audit_log with audit_log, so its policies are unknown.
create policy "Service only" on public.audit_log to service_role using (true);
