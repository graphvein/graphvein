-- R3: an opaque statement that may create a table.
DO $$
BEGIN
    EXECUTE 'CREATE TABLE made (x integer)';
END
$$;
