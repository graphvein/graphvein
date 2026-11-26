create table t (a int, b int);
create index t_ab on t using hash (a, b);
