CREATE TABLE src (id serial, b int UNIQUE);
DO $$ BEGIN CREATE TABLE x (LIKE src INCLUDING ALL); END $$;
CREATE INDEX x_b_key ON src (b);
