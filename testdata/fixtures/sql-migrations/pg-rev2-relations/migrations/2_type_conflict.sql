-- WF3: a table's row type conflicts with an existing type even with IF NOT
-- EXISTS (heap.c 1170-1180). Postgres refuses, so this migration may fail
-- as a whole (WF8): its tables are partial and its applied state unknown.
CREATE TYPE status AS ENUM ('on', 'off');
CREATE TABLE touched_here (id integer);
CREATE TABLE IF NOT EXISTS status (id integer);
