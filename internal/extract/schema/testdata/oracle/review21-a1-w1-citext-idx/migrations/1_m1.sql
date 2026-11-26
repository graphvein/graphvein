CREATE EXTENSION citext; CREATE TABLE t (id int, e citext); CREATE UNIQUE INDEX t_e ON t (e);
