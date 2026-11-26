-- R4: an extension may create tables; it is opaque.
CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE TABLE items (id integer PRIMARY KEY, label text);
