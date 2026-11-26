create collation ci (provider = icu, locale = 'und-u-ks-level2', deterministic = false);
create table t (a text);
alter collation ci rename to ci2;
create index t_a on t (a collate ci);
