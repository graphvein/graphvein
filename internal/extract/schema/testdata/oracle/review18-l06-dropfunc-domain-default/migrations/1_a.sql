create function f() returns int language sql immutable as 'select 1';
create domain d as int default f();
drop function f();
