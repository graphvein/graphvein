CREATE TABLE t (id int, updated_at timestamptz);
CREATE TABLE u (id int);
CREATE FUNCTION touch() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN NEW.updated_at = now(); RETURN NEW; END $$;
CREATE TRIGGER touch BEFORE UPDATE ON t FOR EACH ROW EXECUTE FUNCTION touch();
DROP TRIGGER IF EXISTS touch ON u;
DROP FUNCTION touch();
