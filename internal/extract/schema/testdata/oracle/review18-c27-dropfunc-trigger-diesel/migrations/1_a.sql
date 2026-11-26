CREATE TABLE posts (id serial primary key, updated_at timestamp NOT NULL DEFAULT now());
CREATE OR REPLACE FUNCTION diesel_set_updated_at() RETURNS trigger AS $$ BEGIN IF (NEW IS DISTINCT FROM OLD AND NEW.updated_at IS NOT DISTINCT FROM OLD.updated_at) THEN NEW.updated_at := current_timestamp; END IF; RETURN NEW; END; $$ LANGUAGE plpgsql;
CREATE TRIGGER set_updated_at BEFORE UPDATE ON posts FOR EACH ROW EXECUTE PROCEDURE diesel_set_updated_at();
