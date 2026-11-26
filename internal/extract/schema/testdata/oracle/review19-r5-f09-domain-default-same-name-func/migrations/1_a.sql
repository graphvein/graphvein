CREATE FUNCTION slug() RETURNS text LANGUAGE sql AS $$ select 'x' $$;
CREATE DOMAIN slug AS text DEFAULT slug();
DROP FUNCTION slug();
