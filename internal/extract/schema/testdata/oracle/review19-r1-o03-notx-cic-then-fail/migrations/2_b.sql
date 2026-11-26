-- no-transaction
CREATE INDEX CONCURRENTLY t_a ON t USING hash (a, a);
