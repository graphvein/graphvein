CREATE TABLE fact_cache (repository TEXT COLLATE "C" NOT NULL, path TEXT COLLATE "C" NOT NULL, content_hash TEXT NOT NULL, version TEXT NOT NULL, payload TEXT NOT NULL, PRIMARY KEY(repository,path));
