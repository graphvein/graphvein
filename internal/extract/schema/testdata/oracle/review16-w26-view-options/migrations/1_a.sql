CREATE TABLE t (a int);
CREATE VIEW v WITH (security_barrier = maybe) AS SELECT a FROM t;
