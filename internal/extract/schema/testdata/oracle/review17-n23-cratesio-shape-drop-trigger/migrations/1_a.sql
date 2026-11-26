CREATE TABLE crates (id int, updated_at timestamp);
CREATE FUNCTION set_updated_at() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN NEW.updated_at := now(); RETURN NEW; END $$;
CREATE TRIGGER trigger_crates_set_updated_at BEFORE UPDATE ON crates FOR EACH ROW EXECUTE PROCEDURE set_updated_at();
