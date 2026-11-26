-- R4: a set-defined function call is opaque, and so is DML in a set that
-- defines a function or trigger (a trigger may run DDL).
CREATE FUNCTION touch() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RETURN NEW;
END
$$;
CREATE TRIGGER items_touch BEFORE UPDATE ON items FOR EACH ROW EXECUTE FUNCTION touch();
INSERT INTO items (id, label) VALUES (1, 'one');
UPDATE items SET label = 'uno' WHERE id = 1;
DELETE FROM items WHERE id = 2;
COPY items FROM '/tmp/items.csv' WITH (FORMAT csv);
