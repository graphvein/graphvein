-- projects_owner_id_slug_key is the name Postgres generated for the
-- unnamed UNIQUE (owner_id, slug); source never writes it.
ALTER TABLE projects DROP CONSTRAINT projects_owner_id_slug_key;
