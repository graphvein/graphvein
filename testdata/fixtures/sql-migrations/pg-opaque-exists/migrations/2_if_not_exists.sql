-- R3: IF NOT EXISTS after an opaque statement may be a no-op.
CREATE TABLE IF NOT EXISTS made (id integer, name text);
CREATE TABLE IF NOT EXISTS other (id integer);
-- Without IF NOT EXISTS the written columns are what Postgres creates.
CREATE TABLE plain (id integer);
