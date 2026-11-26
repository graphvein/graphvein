CREATE DOMAIN email AS text;
CREATE TABLE t (e email);
ALTER DOMAIN email RENAME TO email_old;
DROP DOMAIN email_old;
