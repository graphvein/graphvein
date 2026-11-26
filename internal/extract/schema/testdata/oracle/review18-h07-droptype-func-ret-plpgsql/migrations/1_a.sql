create type status as enum ('active', 'inactive');
create function default_status() returns status language plpgsql as $$ begin return 'active'; end $$;
