CREATE TABLE t (a int) PARTITION BY RANGE (a);
ALTER TABLE t SET (fillfactor = 50);
