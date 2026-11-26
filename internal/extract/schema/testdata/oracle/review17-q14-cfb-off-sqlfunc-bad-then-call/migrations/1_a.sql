SET check_function_bodies = off;
CREATE FUNCTION f() RETURNS int LANGUAGE sql AS 'SELECT 1 +';
SELECT f();
