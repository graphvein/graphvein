CREATE TABLE t ("order" int, b int);
CREATE INDEX i ON t (b) WHERE order > 0;
