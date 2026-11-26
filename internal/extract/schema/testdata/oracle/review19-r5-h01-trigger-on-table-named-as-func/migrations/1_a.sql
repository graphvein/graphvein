CREATE TABLE audit (id int, at timestamptz);
CREATE FUNCTION audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN NEW.at = now(); RETURN NEW; END $$;
CREATE TRIGGER audit_tg BEFORE INSERT ON audit FOR EACH ROW EXECUTE FUNCTION audit();
DROP FUNCTION audit();
