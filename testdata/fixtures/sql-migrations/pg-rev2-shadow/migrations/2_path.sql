-- WF4: after a search_path change, destructive changes of unqualified
-- names keep the known objects and make the tables partial.
SET search_path TO elsewhere, public;
DROP TABLE solid;
ALTER TABLE trimmed DROP COLUMN extra;
ALTER TABLE renamed RENAME TO renamed_v2;
