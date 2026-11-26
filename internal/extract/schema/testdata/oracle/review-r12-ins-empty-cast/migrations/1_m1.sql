CREATE TABLE t (a int PRIMARY KEY, b varchar(3), c numeric(4,2), d date, e boolean, f timestamptz, g uuid);
INSERT INTO t VALUES ($$ 12 $$, $$abc$$, $$12.34$$, $$2024-02-29$$, $$yes$$, $$2024-01-01 23:59:59+15:59$$, $$a0eebc999c0b4ef8bb6d6bb9bd380a11$$);
