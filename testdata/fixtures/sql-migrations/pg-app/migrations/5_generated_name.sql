-- An unnamed index: Postgres generates its name (projects_slug_idx).
-- The DROP CONSTRAINT cases are in pg-drops (T018a Revision 7).
CREATE INDEX ON projects (slug);
