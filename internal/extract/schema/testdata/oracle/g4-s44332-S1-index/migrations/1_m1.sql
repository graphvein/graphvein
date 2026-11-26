CREATE TABLE t1 (a text, ts timestamptz);
CREATE INDEX ON t1 (coalesce(a, '')) WHERE ts > '2020-01-01';
CREATE INDEX ON t1 (a);
