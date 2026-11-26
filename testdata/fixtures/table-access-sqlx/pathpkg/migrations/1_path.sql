CREATE TABLE settled (id integer PRIMARY KEY);
CREATE SCHEMA app;
-- After CREATE SCHEMA, an unqualified CREATE may land in a schema named
-- after the role ("$user"): the record keeps schema null with an O3.P13
-- reason.
CREATE TABLE unsure (id integer PRIMARY KEY);
SET search_path TO app, public;
CREATE TABLE moved (id integer PRIMARY KEY);
