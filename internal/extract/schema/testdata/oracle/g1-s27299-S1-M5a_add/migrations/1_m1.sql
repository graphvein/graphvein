-- no-transaction
CREATE TABLE t2 (c1 timestamp DEFAULT now());
CREATE TABLE t4 (extra int) INHERITS (t2);
ALTER TABLE ONLY t2 ADD COLUMN c8 bytea NOT NULL;
