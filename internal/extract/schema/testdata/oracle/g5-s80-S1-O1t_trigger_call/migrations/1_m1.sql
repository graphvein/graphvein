CREATE FUNCTION su2() RETURNS trigger AS $$ BEGIN RETURN NEW; END; $$ LANGUAGE plpgsql;
CREATE OR REPLACE FUNCTION tu1(tablename regclass) RETURNS void AS $$
begin
  perform 1; execute format('CREATE TRIGGER set_upd BEFORE UPDATE ON %s FOR EACH ROW WHEN (NEW.a > 0) EXECUTE FUNCTION su2();', tablename);
end;
$$ LANGUAGE plpgsql;
CREATE TABLE t3 (a int);
CREATE TABLE gone4 (a int);
DROP TABLE gone4;
SELECT tu1('gone4');
