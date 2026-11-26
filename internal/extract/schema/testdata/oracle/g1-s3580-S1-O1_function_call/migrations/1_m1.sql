-- no-transaction
CREATE PROCEDURE p1() LANGUAGE sql AS $$ SELECT 1 $$;
SELECT p1();
