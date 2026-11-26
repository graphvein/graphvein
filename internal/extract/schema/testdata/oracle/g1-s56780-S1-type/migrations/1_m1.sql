-- no-transaction
CREATE TABLE t2 (c1 smallint);
CREATE TABLE t3 (extra int) INHERITS (t2);
ALTER TABLE IF EXISTS t3 ADD COLUMN IF NOT EXISTS c1 int;
