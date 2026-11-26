CREATE TABLE items (id int, name text);
CREATE PROCEDURE add_item(n text) BEGIN ATOMIC INSERT INTO items (name) VALUES (n); END;
ALTER TABLE items DROP COLUMN name;
