CREATE TABLE accounts (
    id serial PRIMARY KEY,
    email text NOT NULL UNIQUE
);

CREATE TABLE posts (
    id serial PRIMARY KEY,
    author_id integer NOT NULL REFERENCES accounts (id),
    title text NOT NULL,
    views integer NOT NULL DEFAULT 0
);

CREATE TABLE tags (
    id serial PRIMARY KEY,
    name text NOT NULL UNIQUE
);

CREATE TABLE post_tags (
    post_id integer NOT NULL REFERENCES posts (id),
    tag_id integer NOT NULL REFERENCES tags (id),
    PRIMARY KEY (post_id, tag_id)
);

CREATE TABLE "Mixed" (
    id integer PRIMARY KEY
);

-- R5-B1: a table named like the keyword function user.
CREATE TABLE "user" (
    id integer PRIMARY KEY
);
