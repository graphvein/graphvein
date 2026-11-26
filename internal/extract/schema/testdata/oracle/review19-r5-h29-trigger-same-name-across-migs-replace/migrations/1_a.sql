CREATE TABLE t (id int, updated_at timestamptz);
CREATE OR REPLACE FUNCTION updated() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$;
