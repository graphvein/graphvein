CREATE FUNCTION f(a int) RETURNS anycompatible LANGUAGE plpgsql AS $$ BEGIN RETURN a; END $$;
