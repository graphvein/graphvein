create table t (id int);
create view v as select id from t;
create function f() returns trigger as $f$ begin return NEW; end; $f$ language plpgsql;
create trigger tg before insert on v for each row execute function f();
