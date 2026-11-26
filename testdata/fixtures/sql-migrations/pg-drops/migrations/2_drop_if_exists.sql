-- IF EXISTS: not a possible failure, but every key of notes is unknown.
ALTER TABLE notes DROP CONSTRAINT IF EXISTS notes_body_check;
