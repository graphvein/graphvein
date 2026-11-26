CREATE FUNCTION touch_count() RETURNS integer LANGUAGE sql AS $$ SELECT 1 $$;
SELECT touch_count();
