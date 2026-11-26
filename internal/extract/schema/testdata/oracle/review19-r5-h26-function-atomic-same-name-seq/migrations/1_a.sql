CREATE SEQUENCE next_id;
CREATE FUNCTION next_id() RETURNS bigint BEGIN ATOMIC SELECT nextval('next_id'); END;
DROP SEQUENCE next_id;
