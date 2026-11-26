CREATE TABLE history (id int, user_id int);
CREATE FUNCTION user_history_count() RETURNS trigger AS $f$ BEGIN RETURN NEW; END; $f$ LANGUAGE plpgsql;
CREATE TRIGGER tg AFTER INSERT ON history FOR EACH ROW EXECUTE PROCEDURE user_history_count();
