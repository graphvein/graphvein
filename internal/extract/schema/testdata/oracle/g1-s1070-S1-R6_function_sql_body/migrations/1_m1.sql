CREATE TABLE t2 (c2 timestamptz NOT NULL);
CREATE FUNCTION f5() RETURNS bigint LANGUAGE sql AS $$ SELECT count(nosuch) FROM t2 $$;
