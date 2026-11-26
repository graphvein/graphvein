CREATE TABLE t (a int, b int);
CREATE INDEX t_a ON t USING brin (a) INCLUDE (b);
