CREATE TABLE p (id int, k int) PARTITION BY LIST (k);
CREATE TABLE q (id int, k int);
ALTER TABLE p ADD CONSTRAINT p_uq UNIQUE (id);
