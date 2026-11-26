create table t (a int);
create materialized view mv as select a from t;
create index mv_b on mv (b);
