create table history (id int);
create function f() returns integer as $f$ begin return 1; end; $f$ language plpgsql;
create trigger tg after insert on history for each row execute procedure f();
