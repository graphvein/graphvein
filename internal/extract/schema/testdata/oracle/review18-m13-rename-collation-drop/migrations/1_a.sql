create collation ci (provider = icu, locale = 'und-u-ks-level2', deterministic = false);
create table u (name text collate ci);
alter collation ci rename to ci2;
drop collation ci2;
