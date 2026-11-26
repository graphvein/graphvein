DO $$ BEGIN CREATE TABLE x (id bigserial, e text UNIQUE); END $$;
CREATE INDEX x_e_key ON y (a);
