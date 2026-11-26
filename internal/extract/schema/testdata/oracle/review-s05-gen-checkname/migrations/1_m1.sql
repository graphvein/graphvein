CREATE TABLE t (a int CHECK (a > 0), CONSTRAINT t_a_check CHECK (a < 5));
