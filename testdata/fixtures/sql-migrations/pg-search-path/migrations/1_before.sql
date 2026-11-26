-- R2: tables before any search_path change keep schema null and stay complete.
create table early (id integer primary key);
create table untouched (id integer);
