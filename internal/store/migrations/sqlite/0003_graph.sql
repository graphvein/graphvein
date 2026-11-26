ALTER TABLE nodes ADD COLUMN name TEXT;
ALTER TABLE nodes ADD COLUMN qualified_name TEXT;
CREATE INDEX nodes_names ON nodes(repository,qualified_name,name,identity);
