DO $$
BEGIN
    EXECUTE 'CREATE TABLE made (x integer)';
END
$$;
CREATE TABLE IF NOT EXISTS made (id integer);
