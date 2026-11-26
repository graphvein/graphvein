create table t (a int primary key);
create view v as select a from t;
create table c (x int references v (a));
