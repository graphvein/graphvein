CREATE TABLE posts (id serial PRIMARY KEY, author_id integer NOT NULL REFERENCES users);
