SET check_function_bodies = off;
CREATE FUNCTION f() RETURNS bigint LANGUAGE sql AS 'SELECT count(*) FROM nosuch_tbl';
SELECT f();
