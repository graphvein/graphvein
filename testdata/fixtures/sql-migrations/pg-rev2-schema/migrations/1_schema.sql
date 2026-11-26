-- WF6: ALTER SCHEMA ... RENAME TO moves the schema's tables; unqualified
-- tables may live in it, and later unqualified names may resolve elsewhere.
CREATE SCHEMA inv;
CREATE TABLE inv.items (id integer PRIMARY KEY);
CREATE TABLE loose (id integer);
ALTER SCHEMA inv RENAME TO stock;
ALTER TABLE stock.items ADD COLUMN qty integer;
CREATE TABLE after_rename (id integer);
