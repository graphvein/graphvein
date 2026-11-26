create type st as enum ('a');
create procedure p(s st) language plpgsql as $$ begin end $$;
drop type st;
