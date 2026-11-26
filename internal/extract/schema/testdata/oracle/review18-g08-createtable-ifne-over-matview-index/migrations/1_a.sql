create table t (a int);
create materialized view v as select a from t;
create table if not exists v (a int, b int);
create index v_b on v (b);
