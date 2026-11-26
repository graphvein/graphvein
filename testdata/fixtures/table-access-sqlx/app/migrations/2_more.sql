CREATE TABLE comments (
    id serial PRIMARY KEY,
    post_id integer NOT NULL REFERENCES posts (id),
    body text NOT NULL
);

CREATE TABLE comments_archive (
    id integer PRIMARY KEY,
    post_id integer NOT NULL,
    body text NOT NULL
);

CREATE TABLE drafts (
    id integer PRIMARY KEY
);

ALTER TABLE drafts RENAME TO pending;

CREATE VIEW post_stats AS SELECT post_id, count(*) AS n FROM comments GROUP BY post_id;

-- Last: after CREATE SCHEMA an unqualified CREATE may land in a schema
-- named after the role ("$user"), so qualified tables only from here.
CREATE SCHEMA audit;

CREATE TABLE audit.events (
    id serial PRIMARY KEY,
    kind text NOT NULL
);
