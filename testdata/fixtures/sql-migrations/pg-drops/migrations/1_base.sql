CREATE TABLE accounts (
    account_id serial PRIMARY KEY,
    email text NOT NULL CONSTRAINT accounts_email_key UNIQUE
);

CREATE TABLE projects (
    project_id integer NOT NULL,
    owner_id integer NOT NULL REFERENCES accounts ON DELETE CASCADE,
    slug text NOT NULL,
    PRIMARY KEY (owner_id, project_id),
    UNIQUE (owner_id, slug)
);

CREATE TABLE notes (
    note_id integer PRIMARY KEY,
    body text CHECK (body <> '')
);
