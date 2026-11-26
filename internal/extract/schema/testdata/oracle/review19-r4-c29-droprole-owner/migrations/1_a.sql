CREATE ROLE r4_owner_x;
CREATE TABLE t (a int);
ALTER TABLE t OWNER TO r4_owner_x;
DROP ROLE r4_owner_x;
