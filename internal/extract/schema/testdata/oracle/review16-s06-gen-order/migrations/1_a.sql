CREATE TABLE t ("order" int, b int GENERATED ALWAYS AS (order + 1) STORED);
