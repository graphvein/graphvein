-- T024: a DO block may run any statement, so it may have changed
-- the row level security of every table before it in either direction:
-- their rls flags are unknown (null), the enabled flag of ledger too.
create table ledger (id integer primary key);
alter table ledger enable row level security;
do $$ begin perform 1; end $$;
