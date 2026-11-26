create collation ci (provider = icu, locale = 'und-u-ks-level2', deterministic = false);
alter collation ci rename to ci2;
alter collation ci owner to current_user;
