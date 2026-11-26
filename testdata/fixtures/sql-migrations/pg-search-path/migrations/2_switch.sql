-- R2: each of these may change where later unqualified names resolve.
SET search_path TO app, public;
create table late (id integer);
alter table early add column note text;
create table app.qualified (id integer);
SET LOCAL search_path = public;
RESET search_path;
SET SCHEMA 'public';
SELECT set_config('search_path', 'app', false);
create index late_id on late (id);
create table pointer (early_id integer references early (id));
