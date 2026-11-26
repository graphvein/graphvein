create collation ci (provider = icu, locale = 'und-u-ks-level2', deterministic = false);
alter collation ci rename to ci2;
create table t (a text collate ci);
