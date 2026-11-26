CREATE FUNCTION f(OUT a anyelement) LANGUAGE plpgsql AS $$ BEGIN a := 1; END $$;
