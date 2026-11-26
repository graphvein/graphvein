CREATE TABLE items (id int, name text);
CREATE FUNCTION first_name() RETURNS text BEGIN ATOMIC SELECT name FROM items LIMIT 1; END;
ALTER TABLE items ALTER COLUMN name TYPE varchar(10);
