CREATE TABLE tail (id integer);
CREATE FUNCTION broken() RETURNS void LANGUAGE plpgsql AS $$
BEGIN
    PERFORM 1;
