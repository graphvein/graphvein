create table t (a int);
create function g() returns bigint language plpgsql as $$ begin return (select count(*) from t); end $$;
