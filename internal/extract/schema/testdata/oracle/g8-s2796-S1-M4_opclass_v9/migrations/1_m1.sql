CREATE TABLE t1 (a inet);
CREATE UNIQUE INDEX ix2 ON t1 USING gin (a inet_ops);
