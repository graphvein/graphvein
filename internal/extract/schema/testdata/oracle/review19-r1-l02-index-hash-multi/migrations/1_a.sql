CREATE TABLE t (a int, b int);
CREATE INDEX t_ab ON t USING hash (a, b);
