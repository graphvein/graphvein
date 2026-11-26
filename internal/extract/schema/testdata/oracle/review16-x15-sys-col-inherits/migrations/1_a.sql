CREATE TYPE c AS (ctid int);
CREATE TABLE p (a int);
CREATE TABLE ch (xmin int) INHERITS (p);
