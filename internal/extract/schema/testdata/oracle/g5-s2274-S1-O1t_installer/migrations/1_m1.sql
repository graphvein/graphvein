CREATE FUNCTION su2() RETURNS trigger AS $$ BEGIN RETURN NEW; END; $$ LANGUAGE plpgsql;
CREATE OR REPLACE FUNCTION tu1(tablename text) RETURNS void AS $$
begin
  execute format('CREATE TRIGGER set_upd AFTER INSERT OR UPDATE ON %I FOR EACH ROW EXECUTE FUNCTION su2();', tablename);
end;
$$ LANGUAGE plpgsql;
CREATE TABLE t3 (a int);
CREATE TABLE gone4 (a int);
DROP TABLE gone4;
SELECT tu1('"t3"');
