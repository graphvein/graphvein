drop function f();
create function f() returns trigger as $f$ begin return NULL; end; $f$ language plpgsql;
