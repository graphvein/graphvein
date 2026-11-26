CREATE TYPE pair AS (a int, b int);
CREATE FUNCTION pair(a int, b int) RETURNS pair BEGIN ATOMIC SELECT ROW(a, b)::pair; END;
DROP TYPE pair;
