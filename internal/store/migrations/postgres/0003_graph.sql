ALTER TABLE nodes ADD COLUMN name TEXT COLLATE "C";
ALTER TABLE nodes ADD COLUMN qualified_name TEXT COLLATE "C";
CREATE INDEX nodes_names ON nodes(repository,qualified_name,name,identity);
