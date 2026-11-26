CREATE TABLE t (id serial PRIMARY KEY, name text NOT NULL, created timestamptz NOT NULL DEFAULT now());
INSERT INTO t (name) VALUES (x);
