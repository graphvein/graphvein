-- T018b1: Postgres keeps the INTO of the leftmost SELECT, also
-- inside parentheses and after a WITH clause (analyze.c
-- transformOptionalSelectInto), so each of these creates a table that is
-- not modeled: unsupported, and CREATE TABLE IF NOT EXISTS of the same
-- name creates no table.
CREATE TABLE kept (id integer);
(SELECT 1 AS a INTO paren_into);
(SELECT 1 INTO union_into) UNION SELECT 2;
(WITH x AS (SELECT 1) SELECT * INTO cte_into FROM x);
CREATE TABLE IF NOT EXISTS paren_into (id integer);
CREATE TABLE IF NOT EXISTS union_into (id integer);
CREATE TABLE IF NOT EXISTS cte_into (id integer);
-- No INTO clause: INSERT INTO and MERGE INTO in a CTE, column labels, a
-- string, a quoted identifier and a comment.
WITH x AS (INSERT INTO kept VALUES (1) RETURNING *) SELECT * FROM x;
WITH x AS (MERGE INTO kept USING (SELECT 2 AS id) s ON kept.id = s.id WHEN NOT MATCHED THEN INSERT VALUES (s.id) RETURNING kept.*) SELECT * FROM x;
SELECT 1 AS into, y.into, 'INTO t' AS "into" /* INTO t */ FROM (SELECT 2 AS into) y;
