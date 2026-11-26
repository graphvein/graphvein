CREATE TABLE x (id int);
DO $$ BEGIN CREATE INDEX x_i ON x (id); END $$;
CREATE INDEX x_i ON x (id);
