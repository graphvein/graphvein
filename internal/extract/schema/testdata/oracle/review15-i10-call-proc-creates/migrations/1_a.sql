CREATE PROCEDURE mk() LANGUAGE plpgsql AS $$ BEGIN CREATE TABLE x (id int); END $$;
CALL mk();
CREATE TABLE x (id int);
