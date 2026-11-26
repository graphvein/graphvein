CREATE TABLE projects (project TEXT PRIMARY KEY, payload TEXT NOT NULL);
CREATE TABLE project_members (project TEXT NOT NULL REFERENCES projects(project) ON DELETE CASCADE, repository TEXT NOT NULL, ordinal INTEGER NOT NULL, payload TEXT NOT NULL, PRIMARY KEY(project,repository));
CREATE TABLE project_links (project TEXT NOT NULL, kind TEXT NOT NULL, source_repository TEXT NOT NULL, source_identity TEXT NOT NULL, target_repository TEXT NOT NULL, target_identity TEXT NOT NULL, payload TEXT NOT NULL, PRIMARY KEY(project,kind,source_repository,source_identity,target_repository,target_identity), FOREIGN KEY(project,source_repository) REFERENCES project_members(project,repository) ON DELETE CASCADE, FOREIGN KEY(project,target_repository) REFERENCES project_members(project,repository) ON DELETE CASCADE);
CREATE INDEX project_links_source ON project_links(project,source_repository,source_identity);
CREATE INDEX project_links_target ON project_links(project,target_repository,target_identity);
CREATE TABLE project_link_runs (project TEXT NOT NULL REFERENCES projects(project) ON DELETE CASCADE, seq INTEGER NOT NULL, payload TEXT NOT NULL, PRIMARY KEY(project,seq));
CREATE TABLE project_outcomes (project TEXT NOT NULL, ordinal INTEGER NOT NULL, source_repository TEXT NOT NULL, source_identity TEXT NOT NULL, status TEXT NOT NULL, payload TEXT NOT NULL, PRIMARY KEY(project,ordinal), FOREIGN KEY(project,source_repository) REFERENCES project_members(project,repository) ON DELETE CASCADE);
CREATE INDEX project_outcomes_source ON project_outcomes(project,source_repository,source_identity);
