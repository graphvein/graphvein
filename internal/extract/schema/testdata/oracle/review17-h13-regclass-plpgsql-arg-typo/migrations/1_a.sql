create table users (id int);
create function g(t regclass) returns int language plpgsql as $$ begin return 1; end $$;
select g('userz');
