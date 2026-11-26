create type st as enum ('a');
create table t (s st);
alter type st rename to st2;
drop type st2;
