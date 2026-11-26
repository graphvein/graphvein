CREATE TABLE t (id int, updated_at timestamptz);
CREATE FUNCTION set_updated_at() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN NEW.updated_at = now(); RETURN NEW; END $$;
CREATE TRIGGER set_updated_at BEFORE UPDATE ON t FOR EACH ROW EXECUTE FUNCTION set_updated_at();
