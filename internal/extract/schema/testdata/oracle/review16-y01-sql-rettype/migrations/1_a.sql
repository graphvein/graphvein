CREATE FUNCTION s1() RETURNS int LANGUAGE sql AS $$ SELECT $x$a$x$::text $$;
