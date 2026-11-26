CREATE TABLE t (a int);
CREATE PUBLICATION p FOR TABLE t WITH (publish = 'bogus');
