DO $$ BEGIN NULL; EXCEPTION WHEN no_such_condition THEN NULL; END $$;
