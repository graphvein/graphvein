CREATE EXTENSION pgcrypto;
CREATE TABLE t (a bytea DEFAULT gen_random_bytes(4));
DROP EXTENSION pgcrypto;
