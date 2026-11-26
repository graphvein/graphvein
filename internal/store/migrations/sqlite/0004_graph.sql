CREATE TABLE change_runs (repository TEXT NOT NULL REFERENCES repositories(repository), seq INTEGER NOT NULL, run_id TEXT NOT NULL, payload TEXT NOT NULL, PRIMARY KEY(repository,seq));
CREATE UNIQUE INDEX change_runs_run ON change_runs(repository,run_id);
CREATE TABLE change_entries (repository TEXT NOT NULL, seq INTEGER NOT NULL, record TEXT NOT NULL, key TEXT NOT NULL, before TEXT, after_hash TEXT, PRIMARY KEY(repository,seq,record,key), FOREIGN KEY(repository,seq) REFERENCES change_runs(repository,seq) ON DELETE CASCADE);
CREATE INDEX change_entries_key ON change_entries(repository,record,key,seq);
CREATE TABLE change_pins (repository TEXT NOT NULL REFERENCES repositories(repository), id TEXT NOT NULL, name TEXT NOT NULL, seq INTEGER NOT NULL, payload TEXT NOT NULL, PRIMARY KEY(repository,id));
CREATE UNIQUE INDEX change_pins_name ON change_pins(repository,name);
CREATE TABLE change_pin_entries (repository TEXT NOT NULL, pin TEXT NOT NULL, record TEXT NOT NULL, key TEXT NOT NULL, before TEXT, PRIMARY KEY(repository,pin,record,key), FOREIGN KEY(repository,pin) REFERENCES change_pins(repository,id) ON DELETE CASCADE);
CREATE TABLE ref_snapshots (repository TEXT NOT NULL REFERENCES repositories(repository), namespace TEXT NOT NULL, commit_id TEXT NOT NULL, extractor TEXT NOT NULL, payload TEXT NOT NULL, PRIMARY KEY(repository,namespace));
