create function f() returns int language sql immutable as 'select 1';
create table t (id int default f(), b int);
alter function f() rename to g;
drop function g();
