CREATE FUNCTION su2() RETURNS trigger AS $$ BEGIN RETURN NEW; END; $$ LANGUAGE plpgsql;
CREATE OR REPLACE FUNCTION tu1(tablename text) RETURNS void AS $$
begin
  execute format('CREATE TRIGGER set_upd BEFORE UPDATE ON %I FOR EACH ROW WHEN (OLD is distinct from NEW) EXECUTE FUNCTION su2();', tablename);
end;
$$ LANGUAGE plpgsql;
SELECT tu1('T3');
