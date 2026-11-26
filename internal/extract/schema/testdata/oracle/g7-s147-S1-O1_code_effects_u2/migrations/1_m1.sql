-- no-transaction
CREATE TABLE ct1 (id int PRIMARY KEY);
CREATE PROCEDURE cr4() LANGUAGE plpgsql AS $$ BEGIN EXECUTE 'DROP INDEX ct1_pkey'; END $$;
CALL cr4();
CREATE TABLE cf3 (id int);
