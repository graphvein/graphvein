CREATE ROLE r4_grant_x;
CREATE TABLE t (a int);
GRANT SELECT ON t TO r4_grant_x;
DROP ROLE r4_grant_x;
