-- WF7: without CASCADE, a generated column that uses the column makes
-- Postgres refuse the drop, so this migration may fail as a whole (WF8).
CREATE TABLE totals (net numeric, gross numeric GENERATED ALWAYS AS (net * 2) STORED);
ALTER TABLE totals DROP COLUMN net;
