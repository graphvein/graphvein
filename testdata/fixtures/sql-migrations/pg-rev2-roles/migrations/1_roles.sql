CREATE TABLE before_role (id integer);
-- G2: a role or database setting changes search_path for later sessions
-- (R2).
ALTER ROLE CURRENT_USER SET search_path TO app, public;
CREATE TABLE after_role (id integer);
