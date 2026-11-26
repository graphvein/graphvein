CREATE TABLE t2 (c4 date NOT NULL);
CREATE INDEX ON t2 (lower(c4::text));
