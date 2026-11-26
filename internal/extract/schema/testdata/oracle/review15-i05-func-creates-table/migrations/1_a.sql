CREATE FUNCTION mk() RETURNS void LANGUAGE plpgsql AS $$ BEGIN CREATE TABLE x (id int); END $$;
SELECT mk();
CREATE TABLE x (id int);
