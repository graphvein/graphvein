CREATE TABLE fact_cache (repository TEXT NOT NULL, path TEXT NOT NULL, content_hash TEXT NOT NULL, version TEXT NOT NULL, payload TEXT NOT NULL, PRIMARY KEY(repository,path));
