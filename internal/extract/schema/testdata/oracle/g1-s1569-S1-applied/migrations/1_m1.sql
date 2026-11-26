-- no-transaction
BEGIN;
CREATE STATISTICS st1 ON id, id FROM ext_a;
