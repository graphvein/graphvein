create collation ci (provider = icu, locale = 'und-u-ks-level2', deterministic = false);
alter collation ci rename to ci2;
comment on collation ci is 'x';
