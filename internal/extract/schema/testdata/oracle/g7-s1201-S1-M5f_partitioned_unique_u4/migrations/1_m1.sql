CREATE TABLE pp1 (id int, k int) PARTITION BY LIST (k, id);
CREATE UNIQUE INDEX pi2 ON ONLY pp1 (k, id);
