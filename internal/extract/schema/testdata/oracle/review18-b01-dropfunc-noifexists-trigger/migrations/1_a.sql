create table history (id int);
create function f() returns trigger as $f$ begin return NEW; end; $f$ language plpgsql;
create trigger tg after insert on history for each row execute procedure f();
drop function f();
