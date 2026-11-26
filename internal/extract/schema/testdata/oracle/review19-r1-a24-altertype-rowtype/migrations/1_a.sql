CREATE TABLE t (a int, b int);
CREATE TABLE u (x t);
ALTER TABLE t ALTER COLUMN b TYPE bigint;
