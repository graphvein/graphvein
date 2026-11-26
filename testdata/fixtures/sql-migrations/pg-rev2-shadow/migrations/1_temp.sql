CREATE TABLE kept (id integer, note text);
CREATE TABLE renamed (id integer);
CREATE TABLE trimmed (id integer, extra text);
CREATE TABLE solid (id integer);
-- WF9: pg_temp names the session's temporary schema (namespace.c 673-678):
-- the table is temporary and not part of the schema.
CREATE TABLE pg_temp.scratch_tmp (id integer);
-- WF4: an unqualified name now resolves to the temporary table first, so
-- the drop may not touch the persistent table: it stays, partial.
CREATE TEMP TABLE kept (id integer);
DROP TABLE kept;
