-- Postgres refuses INTO outside the first SELECT ("INTO is only allowed on
-- first SELECT of UNION/INTERSECT/EXCEPT", "SELECT ... INTO is not allowed
-- here"), so this migration may fail (WF8): its tables are partial and its
-- applied state unknown.
CREATE TABLE before_refused (id integer);
SELECT 1 UNION SELECT 2 INTO late_into;
SELECT * FROM (SELECT 1 INTO sub_into) s;
