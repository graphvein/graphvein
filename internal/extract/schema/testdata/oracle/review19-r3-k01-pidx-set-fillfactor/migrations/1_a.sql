CREATE TABLE p (a int) PARTITION BY RANGE (a);
CREATE INDEX pi ON p (a);
ALTER INDEX pi SET (fillfactor = 50);
