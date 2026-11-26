package schema

import (
	"context"
	"testing"

	"github.com/graphvein/graphvein/internal/egress"
)

// Revision 1.6 cases of step D1 (T018a2 V2-V4, V7, V9-V13), as
// rev15Cases, each checked live by TestRev16CasesPostgres. Statements name
// objects outside the set where E1 lets them (roles, tables), so that
// Postgres reaches the check the case is about.
var rev16Cases = []b1dCase{
	// V7 (P7): each reader's statement with one extra word is row D.
	{"V7 create role garbage", "CREATE ROLE gv16_r WITH BYPASSRLS garbage;", "null", "", "42601"},
	{"V7 grant all tables garbage", "GRANT ALL ON ALL TABLES IN SCHEMA public TO CURRENT_USER garbage;", "null", "", "42601"},
	{"V7 grant role garbage", "CREATE ROLE gv16_ra;\nCREATE ROLE gv16_rb;\nGRANT gv16_ra TO gv16_rb garbage;", "null", "", "42601"},
	{"V7 default privileges garbage", "ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON TABLES garbage TO CURRENT_USER;", "null", "", "42601"},
	{"V7 revoke garbage", "CREATE TABLE t (a int);\nREVOKE ALL ON t FROM PUBLIC garbage;", "null", "", "42601"},
	{"V7 alter role set garbage", "ALTER ROLE CURRENT_USER SET work_mem = 1000 garbage;", "null", "", "42601"},
	{"V7 comment table garbage", "CREATE TABLE t (a int);\nCOMMENT ON TABLE t IS 'x' garbage;", "null", "", "42601"},
	{"V7 create trigger garbage", "CREATE TABLE t (a int);\nCREATE FUNCTION tf() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$;\nCREATE TRIGGER tr BEFORE INSERT ON t FOR EACH ROW EXECUTE FUNCTION tf() garbage;", "null", "", "42601"},
	{"V7 create policy garbage", "CREATE TABLE t (a int);\nCREATE POLICY p ON t USING (true) garbage;", "null", "", "42601"},
	{"V7 create collation garbage", "CREATE COLLATION c1 (provider = icu, locale = 'und') garbage;", "null", "", "42601"},
	{"V7 create enum garbage", "CREATE TYPE e AS ENUM ('a') garbage;", "null", "", "42601"},
	{"V7 add value garbage", "CREATE TYPE e AS ENUM ('a');\nALTER TYPE e ADD VALUE 'b' garbage;", "null", "R3.P7", "42601"},
	{"V7 rename value garbage", "CREATE TYPE e AS ENUM ('a');\nALTER TYPE e RENAME VALUE 'a' TO 'b' garbage;", "null", "R3.P7", "42601"},
	{"V7 drop type garbage", "CREATE TYPE e AS ENUM ('a');\nDROP TYPE e garbage;", "null", "R5.P7", "42601"},
	{"V7 drop view garbage", "CREATE VIEW v AS SELECT 1 AS a;\nDROP VIEW v garbage;", "null", "R5.P7", "42601"},
	{"V7 drop function garbage", "CREATE FUNCTION tf() RETURNS int LANGUAGE sql AS 'SELECT 1';\nDROP FUNCTION tf() garbage;", "null", "R5.P7", "42601"},
	{"V7 alter function garbage", "CREATE FUNCTION tf() RETURNS int LANGUAGE sql AS 'SELECT 1';\nALTER FUNCTION tf() OWNER TO CURRENT_USER garbage;", "null", "R6.P7", "42601"},
	{"V7 create schema garbage", "CREATE SCHEMA s garbage;", "null", "R1.P7", "42601"},
	{"V7 create extension garbage", "CREATE EXTENSION IF NOT EXISTS pgcrypto garbage;", "null", "O2.P7", "42601"},
	{"V7 drop extension garbage", "DROP EXTENSION IF EXISTS pgcrypto garbage;", "null", "O2.P7", "42601"},
	{"V7 lock garbage", "CREATE TABLE t (a int);\nLOCK TABLE t IN SHARE MODE garbage;", "null", "T1.P7", "42601"},
	{"V7 notify garbage", "NOTIFY ch garbage;", "null", "N1.P7", "42601"},
	{"V7 reset garbage", "RESET work_mem garbage;", "null", "N2.P7", "42601"},
	{"V7 begin garbage", "BEGIN garbage;", "null", "T1.P7", "42601"},
	{"V7 commit garbage", "COMMIT garbage;", "null", "T1.P7", "42601"},
	// V7, the same class on the other rows (each claimed applied before).
	{"V7 like garbage", "CREATE TABLE t (a int);\nCREATE TABLE t2 (LIKE t) garbage;", "null", "M1b.P7", "42601"},
	{"V7 set logged garbage", "CREATE TABLE t (a int);\nALTER TABLE t SET LOGGED garbage;", "null", "M5k-a.P7", "42601"},
	{"V7 enable trigger garbage", "CREATE TABLE t (a int);\nALTER TABLE t ENABLE TRIGGER ALL garbage;", "null", "M5k-a.P7", "42601"},
	{"V7 set default garbage outside", "ALTER TABLE outside_t ALTER COLUMN a SET DEFAULT 1 garbage;", "null", "M5e.P7", "42601"},
	{"V7 alter schema garbage", "CREATE SCHEMA s;\nALTER SCHEMA s RENAME TO s2 garbage;", "null", "M9.P7", "42601"},
	{"V7 alter view garbage", "CREATE VIEW v AS SELECT 1 AS a;\nALTER VIEW v RENAME TO w garbage;", "null", "R4.P7", "42601"},
	{"V7 call garbage", "CREATE PROCEDURE pr() LANGUAGE sql AS 'SELECT 1';\nCALL pr() garbage;", "null", "O1.P7", "42601"},
	{"V7 discard all garbage", "DISCARD ALL garbage;", "null", "O3.P7", "42601"},
	{"V7 drop owned garbage", "DROP OWNED BY CURRENT_USER garbage;", "null", "O5.P7", "42601"},
	{"V7 composite type garbage", "CREATE TYPE c AS (a int) garbage;", "null", "R1.P7", "42601"},
	{"V7 cluster garbage", "CREATE TABLE t (a int);\nCREATE INDEX i ON t (a);\nCLUSTER t USING i garbage;", "null", "N3.P7", "42601"},
	{"V7 copy garbage", "CREATE TABLE t (a int);\nCOPY t TO STDOUT garbage;", "null", "N3.P7", "42601"},
	// V7: the same statements without the extra word keep their rows.
	{"V7 create role", "CREATE ROLE gv16_r WITH BYPASSRLS;", "true", "", "ok"},
	{"V7 grant all tables", "GRANT ALL ON ALL TABLES IN SCHEMA public TO CURRENT_USER;", "true", "", "ok"},
	{"V7 grant all privileges sequences", "GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO CURRENT_USER;", "true", "", "ok"},
	{"V7 grant role", "CREATE ROLE gv16_ra;\nCREATE ROLE gv16_rb;\nGRANT gv16_ra TO gv16_rb;", "true", "", "ok"},
	{"V7 default privileges", "ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON TABLES TO CURRENT_USER;", "true", "", "ok"},
	{"V7 revoke", "CREATE TABLE t (a int);\nREVOKE ALL ON t FROM PUBLIC;", "true", "", "ok"},
	{"V7 alter role set", "ALTER ROLE CURRENT_USER SET work_mem = 1000;", "true", "", "ok"},
	{"V7 comment table", "CREATE TABLE t (a int);\nCOMMENT ON TABLE t IS 'x';", "true", "", "ok"},
	{"V7 create trigger", "CREATE TABLE t (a int);\nCREATE FUNCTION tf() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$;\nCREATE TRIGGER tr BEFORE INSERT ON t FOR EACH ROW EXECUTE FUNCTION tf();", "true", "", "ok"},
	{"V7 create policy", "CREATE TABLE t (a int);\nCREATE POLICY p ON t USING (true);", "true", "", "ok"},
	{"V7 create collation", "CREATE COLLATION c1 (provider = icu, locale = 'und');", "true", "", "ok"},
	{"V7 create enum", "CREATE TYPE e AS ENUM ('a');", "true", "", "ok"},
	{"V7 add value", "CREATE TYPE e AS ENUM ('a');\nALTER TYPE e ADD VALUE 'b';", "true", "", "ok"},
	{"V7 rename value", "CREATE TYPE e AS ENUM ('a');\nALTER TYPE e RENAME VALUE 'a' TO 'b';", "true", "", "ok"},
	{"V7 drop type", "CREATE TYPE e AS ENUM ('a');\nDROP TYPE e;", "true", "", "ok"},
	{"V7 drop view", "CREATE VIEW v AS SELECT 1 AS a;\nDROP VIEW v;", "true", "", "ok"},
	{"V7 drop function", "CREATE FUNCTION tf() RETURNS int LANGUAGE sql AS 'SELECT 1';\nDROP FUNCTION tf();", "true", "", "ok"},
	{"V7 alter function", "CREATE FUNCTION tf() RETURNS int LANGUAGE sql AS 'SELECT 1';\nALTER FUNCTION tf() OWNER TO CURRENT_USER;", "true", "", "ok"},
	{"V7 create schema", "CREATE SCHEMA s;", "true", "", "ok"},
	{"V7 create extension", "CREATE EXTENSION IF NOT EXISTS pgcrypto;", "true", "", "ok"},
	{"V7 lock", "CREATE TABLE t (a int);\nLOCK TABLE t IN SHARE MODE;", "true", "", "ok"},
	{"V7 notify", "NOTIFY ch;", "true", "", "ok"},
	{"V7 reset", "RESET work_mem;", "true", "", "ok"},
	{"V7 like", "CREATE TABLE t (a int);\nCREATE TABLE t2 (LIKE t INCLUDING ALL);", "true", "", "ok"},
	{"V7 set logged", "CREATE TABLE t (a int);\nALTER TABLE t SET LOGGED;", "true", "", "ok"},
	{"V7 alter view", "CREATE VIEW v AS SELECT 1 AS a;\nALTER VIEW v RENAME TO w;", "true", "", "ok"},
	{"V7 composite type", "CREATE TYPE c AS (a int);", "true", "", "ok"},
	{"V7 cluster", "CREATE TABLE t (a int);\nCREATE INDEX i ON t (a);\nCLUSTER t USING i;", "true", "", "ok"},
	// V3 (P7): keyword categories in element and expression positions.
	{"V3 index element left", "CREATE TABLE t (\"left\" int);\nCREATE INDEX i ON t (left);", "null", "M4.P7", "42601"},
	{"V3 index element order", "CREATE TABLE t (\"order\" int);\nCREATE INDEX i ON t (order);", "null", "M4.P7", "42601"},
	{"V3 index element verbose desc", "CREATE TABLE t (\"verbose\" int);\nCREATE INDEX i ON t (verbose DESC);", "null", "M4.P7", "42601"},
	{"V3 index element user", "CREATE TABLE t (\"user\" text);\nCREATE INDEX i ON t (user);", "null", "M4.P5", "42P17"},
	{"V3 index element current_user", "CREATE TABLE t (\"current_user\" text);\nCREATE INDEX i ON t (current_user);", "null", "M4.P5", "42P17"},
	{"V3 index expression order", "CREATE TABLE t (\"order\" int);\nCREATE INDEX i ON t ((order + 1));", "null", "M4.P7", "42601"},
	{"V3 index predicate order", "CREATE TABLE t (\"order\" int, b int);\nCREATE INDEX i ON t (b) WHERE order > 0;", "null", "M4.P7", "42601"},
	{"V3 index opclass order", "CREATE TABLE t (a text);\nCREATE INDEX i ON t (a order);", "null", "M4.P7", "42601"},
	{"V3 check order", "CREATE TABLE t (\"order\" int CHECK (order > 0));", "null", "M1.P7", "42601"},
	{"V3 generated order", "CREATE TABLE t (\"order\" int, b int GENERATED ALWAYS AS (order + 1) STORED);", "null", "M1.P7", "42601"},
	{"V3 generated user", "CREATE TABLE t (a int, b text GENERATED ALWAYS AS (user) STORED);", "null", "M1.P5", "42P17"},
	{"V3 partition list order", "CREATE TABLE t (\"order\" int) PARTITION BY LIST (order);", "null", "M1.P7", "42601"},
	{"V3 partition range left", "CREATE TABLE t (\"left\" int) PARTITION BY RANGE (left);", "null", "M1.P7", "42601"},
	{"V3 exclude order", "CREATE TABLE t (\"order\" int, EXCLUDE (order WITH =));", "null", "M1.P7", "42601"},
	{"V3 add check order", "CREATE TABLE t (\"order\" int);\nALTER TABLE t ADD CHECK (order > 0);", "null", "M5a.P7", "42601"},
	{"V3 domain check order", "CREATE DOMAIN d AS int CHECK (order > 0);", "null", "R1.P7", "42601"},
	{"V3 index element int", "CREATE TABLE t (\"int\" int);\nCREATE INDEX i ON t (int);", "true", "", "ok"},
	{"V3 index element quoted", "CREATE TABLE t (\"order\" int);\nCREATE INDEX i ON t (\"order\");", "true", "", "ok"},
	{"V3 include order", "CREATE TABLE t (a int, \"order\" int);\nCREATE INDEX i ON t (a) INCLUDE (\"order\");", "true", "", "ok"},
	{"V3 default user", "CREATE TABLE t (a text DEFAULT user);", "true", "", "ok"},
	{"V3 check qualified keyword", "CREATE TABLE t (\"order\" int CHECK (t.order > 0));", "null", "", "ok"},
	// V4 (P1): objects of kinds E1 does not cover.
	{"V4 alter index rename outside", "ALTER INDEX nosuch_idx RENAME TO y;", "null", "M8.P1", "42P01"},
	{"V4 alter index set outside", "ALTER INDEX nosuch_idx SET (fillfactor = 50);", "null", "M8.P1", "42P01"},
	{"V4 alter index attach outside", "ALTER INDEX nosuch_a ATTACH PARTITION nosuch_b;", "null", "M8.P7", "42P01"},
	{"V4 alter index statistics outside", "ALTER INDEX nosuch_idx ALTER COLUMN 1 SET STATISTICS 100;", "null", "M8.P7", "42P01"},
	{"V7 alter index statistics held", "CREATE TABLE t (a int);\nCREATE INDEX ix ON t (a);\nALTER INDEX ix ALTER COLUMN 1 SET STATISTICS 100;", "null", "M8.P7", "0A000"},
	{"V10 names unquoted", "SET NAMES UTF8;", "null", "N2.P7", "42601"},
	{"V11 typed table add attribute restrict", "CREATE TYPE c AS (a int);\nCREATE TABLE t OF c;\nALTER TYPE c ADD ATTRIBUTE b int RESTRICT;", "null", "R4.P2", "2BP01"},
	{"V11 typed table add attribute cascade", "CREATE TYPE c AS (a int);\nCREATE TABLE t OF c;\nALTER TYPE c ADD ATTRIBUTE b int CASCADE;", "true", "", "ok"},
	{"V4 alter view outside", "ALTER VIEW nosuch_v RENAME TO y;", "null", "R4.P1", "42P01"},
	{"V4 alter view default outside", "ALTER VIEW nosuch_v ALTER COLUMN a SET DEFAULT 1;", "null", "R4.P1", "42P01"},
	{"V4 alter materialized view outside", "ALTER MATERIALIZED VIEW nosuch_mv RENAME TO y;", "null", "R4.P1", "42P01"},
	{"V4 comment index outside", "COMMENT ON INDEX nosuch_idx IS 'x';", "null", "N1.P1", "42P01"},
	{"V4 comment sequence outside", "COMMENT ON SEQUENCE nosuch_seq IS 'x';", "null", "N1.P1", "42P01"},
	{"V4 comment view outside", "COMMENT ON VIEW nosuch_v IS 'x';", "null", "N1.P1", "42P01"},
	{"V4 grant on sequence outside", "GRANT SELECT ON SEQUENCE nosuch_seq TO PUBLIC;", "null", "N1.P1", "42P01"},
	{"V4 drop sequence outside", "DROP SEQUENCE nosuch_seq;", "null", "R5.P1", "42P01"},
	{"V4 drop view outside", "DROP VIEW nosuch_v;", "null", "R5.P1", "42P01"},
	{"V4 drop materialized view outside", "DROP MATERIALIZED VIEW nosuch_mv;", "null", "R5.P1", "42P01"},
	{"V4 refresh outside", "REFRESH MATERIALIZED VIEW nosuch_mv;", "null", "N3.P1", "42P01"},
	{"V4 reindex index outside", "REINDEX INDEX nosuch_idx;", "null", "N3.P1", "42P01"},
	{"V4 drop trigger on outside table", "DROP TRIGGER tr ON nosuch_t;", "null", "N1.P1", "42P01"},
	{"V4 alter sequence outside", "ALTER SEQUENCE nosuch_seq RESTART;", "null", "", "42P01"},
	{"V4 drop index outside", "DROP INDEX nosuch_idx;", "null", "M7.P1", "42704"},
	{"V4 drop sequence if exists", "DROP SEQUENCE IF EXISTS nosuch_seq;", "true", "", "ok"},
	{"V4 comment on held index", "CREATE TABLE t (a int);\nCREATE INDEX i ON t (a);\nCOMMENT ON INDEX i IS 'x';", "true", "", "ok"},
	{"V4 grant on serial sequence", "CREATE TABLE t (a serial);\nGRANT SELECT ON SEQUENCE t_a_seq TO PUBLIC;", "true", "", "ok"},
	{"V4 drop held view", "CREATE VIEW v AS SELECT 1 AS a;\nDROP VIEW v;", "true", "", "ok"},
	// V4 kinds (2026-11-07 root fix, g8-s4299): relations share pg_class's
	// namespace, so a statement addressed to one relation kind meets a
	// name held by another kind, also with IF EXISTS.
	{"V4 kinds alter index if exists view", "CREATE TABLE t (a int);\nCREATE VIEW v AS SELECT a FROM t;\nALTER INDEX IF EXISTS v SET (fillfactor = 50);", "null", "M8.P1", "42809"},
	{"V4 kinds alter index if exists sequence", "CREATE SEQUENCE sq;\nALTER INDEX IF EXISTS sq SET TABLESPACE pg_default;", "null", "M8.P1", "42809"},
	{"V4 kinds alter index if exists matview reset", "CREATE MATERIALIZED VIEW mv AS SELECT 1 AS a;\nALTER INDEX IF EXISTS mv RESET (fillfactor);", "null", "M8.P1", "42809"},
	{"V4 kinds alter index rename composite", "CREATE TYPE ct AS (a int);\nALTER INDEX IF EXISTS ct RENAME TO zz;", "null", "M8.P1", "42809"},
	{"V4 kinds alter index rename view", "CREATE TABLE t (a int);\nCREATE VIEW v AS SELECT a FROM t;\nALTER INDEX IF EXISTS v RENAME TO zz;", "true", "", "ok"},
	{"V4 kinds alter index set logged", "CREATE TABLE t (a int);\nCREATE INDEX ix ON t (a);\nALTER INDEX ix SET LOGGED;", "null", "M8.P1", "42809"},
	{"V4 kinds alter index if exists set access method", "CREATE TABLE t (a int);\nCREATE INDEX ix ON t (a);\nALTER INDEX IF EXISTS ix SET ACCESS METHOD heap;", "null", "M8.P1", "42809"},
	{"V4 kinds alter index rename enum", "CREATE TYPE e AS ENUM ('a');\nALTER INDEX e RENAME TO zz;", "null", "M8.P1", "42P01"},
	{"V4 kinds drop index if exists view", "CREATE TABLE t (a int);\nCREATE VIEW v AS SELECT a FROM t;\nDROP INDEX IF EXISTS v;", "null", "R5.P4", "42809"},
	{"V4 kinds drop index matview", "CREATE MATERIALIZED VIEW mv AS SELECT 1 AS a;\nDROP INDEX mv CASCADE;", "null", "R5.P4", "42809"},
	{"V4 kinds drop index if exists composite", "CREATE TYPE ct AS (a int);\nDROP INDEX IF EXISTS ct;", "null", "R5.P4", "42809"},
	{"V4 kinds alter table if exists sequence add column", "CREATE SEQUENCE sq;\nALTER TABLE IF EXISTS sq ADD COLUMN b int;", "null", "M5a.P1", "42809"},
	{"V4 kinds alter table sequence set default", "CREATE SEQUENCE sq;\nALTER TABLE sq ALTER COLUMN a SET DEFAULT 1;", "null", "M5a.P1", "42809"},
	{"V4 kinds alter table view set logged", "CREATE TABLE t (a int);\nCREATE VIEW v AS SELECT a FROM t;\nALTER TABLE IF EXISTS v SET LOGGED;", "null", "M5a.P1", "42809"},
	{"V4 kinds alter table matview drop column", "CREATE MATERIALIZED VIEW mv AS SELECT 1 AS a;\nALTER TABLE mv DROP COLUMN a;", "null", "M5a.P1", "42809"},
	{"V4 kinds alter table matview row security", "CREATE MATERIALIZED VIEW mv AS SELECT 1 AS a;\nALTER TABLE IF EXISTS mv ENABLE ROW LEVEL SECURITY;", "null", "M5a.P1", "42809"},
	{"V4 kinds alter table view rename column", "CREATE TABLE t (a int);\nCREATE VIEW v AS SELECT a FROM t;\nALTER TABLE v RENAME COLUMN a TO b;", "null", "M5a.P1", "ok"},
	{"V4 kinds alter table sequence owner", "CREATE SEQUENCE sq;\nALTER TABLE sq OWNER TO CURRENT_USER;", "true", "", "ok"},
	{"V4 kinds alter table sequence set unlogged", "CREATE SEQUENCE sq;\nALTER TABLE IF EXISTS sq SET UNLOGGED;", "true", "", "ok"},
	{"V4 kinds alter table view rename", "CREATE TABLE t (a int);\nCREATE VIEW v AS SELECT a FROM t;\nALTER TABLE IF EXISTS v RENAME TO zz;", "true", "", "ok"},
	{"V4 kinds alter table enum", "CREATE TYPE e AS ENUM ('a');\nALTER TABLE e OWNER TO CURRENT_USER;", "null", "M5a.P1", "42P01"},
	{"V4 kinds alter table if exists enum", "CREATE TYPE e AS ENUM ('a');\nALTER TABLE IF EXISTS e OWNER TO CURRENT_USER;", "true", "", "ok"},
	{"V4 kinds alter type view", "CREATE TABLE t (a int);\nCREATE VIEW v AS SELECT a FROM t;\nALTER TYPE v OWNER TO CURRENT_USER;", "null", "R4.P1", "42809"},
	{"V4 kinds alter type sequence", "CREATE SEQUENCE sq;\nALTER TYPE sq RENAME TO zz;", "null", "R4.P1", "42704"},
	{"V4 kinds alter type enum add attribute", "CREATE TYPE e AS ENUM ('a');\nALTER TYPE e ADD ATTRIBUTE b int;", "null", "R4.P1", "42P01"},
	{"V4 kinds alter type matview rename attribute", "CREATE MATERIALIZED VIEW mv AS SELECT 1 AS a;\nALTER TYPE mv RENAME ATTRIBUTE a TO b;", "null", "R4.P1", "ok"},
	{"V4 kinds alter type composite rename attribute", "CREATE TYPE ct AS (a int);\nALTER TYPE ct ADD ATTRIBUTE b int;", "true", "", "ok"},
	{"V4 kinds alter domain enum", "CREATE TYPE e AS ENUM ('a');\nALTER DOMAIN e SET NOT NULL;", "null", "R4.P1", "42809"},
	{"V4 kinds alter domain composite", "CREATE TYPE ct AS (a int);\nALTER DOMAIN ct OWNER TO CURRENT_USER;", "null", "R4.P1", "42809"},
	{"V4 kinds alter domain drop constraint", "CREATE DOMAIN d AS int;\nALTER DOMAIN d DROP CONSTRAINT c;", "null", "R4.P1", "42704"},
	{"V4 kinds alter domain drop constraint if exists", "CREATE DOMAIN d AS int;\nALTER DOMAIN d DROP CONSTRAINT IF EXISTS c;", "true", "", "ok"},
	{"V4 kinds add value table row type", "CREATE TABLE t (a int);\nALTER TYPE t ADD VALUE 'b';", "null", "R3.P1", "42809"},
	{"V4 kinds alter view rename column matview", "CREATE MATERIALIZED VIEW mv AS SELECT 1 AS a;\nALTER VIEW mv RENAME COLUMN a TO b;", "null", "R4.P1", "ok"},
	{"V4 kinds alter view if exists enum rename column", "CREATE TYPE e AS ENUM ('a');\nALTER VIEW IF EXISTS e RENAME COLUMN a TO b;", "true", "", "ok"},
	{"V4 kinds alter type outside", "ALTER TYPE nosuch_ty ADD ATTRIBUTE b int;", "null", "R4.P1", "42P01"},
	{"V4 kinds add value outside", "ALTER TYPE nosuch_e ADD VALUE IF NOT EXISTS 'z';", "null", "R3.P1", "42704"},
	{"V4 kinds alter domain outside", "ALTER DOMAIN nosuch_d DROP CONSTRAINT IF EXISTS c;", "null", "R4.P1", "42704"},
	{"V4 kinds alter view if exists sequence", "CREATE SEQUENCE sq;\nALTER VIEW IF EXISTS sq OWNER TO CURRENT_USER;", "null", "", "42809"},
	// V2 (P1): languages that must exist.
	{"V2 plperl", "CREATE FUNCTION f() RETURNS int LANGUAGE plperl AS $$ return 1; $$;", "null", "R6.P4", "42704"},
	{"V2 quoted upper", "CREATE FUNCTION a() RETURNS int LANGUAGE 'PLPGSQL' AS $$ BEGIN RETURN 1; END $$;", "null", "R6.P1", "42704"},
	{"V2 do plpython", "DO LANGUAGE plpython3u $$ pass $$;", "null", "O1.P14", "42704"},
	{"V2 quoted plpgsql", "CREATE FUNCTION a() RETURNS int LANGUAGE 'plpgsql' AS $$ BEGIN RETURN 1; END $$;", "true", "", "ok"},
	// V9 (P4): written operator classes.
	{"V9 text_ops on int", "CREATE TABLE t (a int);\nCREATE INDEX t_a ON t (a text_ops);", "null", "M4.P4", "42804"},
	{"V9 int4_ops on bigint", "CREATE TABLE t (a bigint);\nCREATE INDEX t_a ON t (a int4_ops);", "null", "M4.P4", "42804"},
	{"V9 pattern ops on int", "CREATE TABLE t (a int);\nCREATE UNIQUE INDEX t_a ON t (a text_pattern_ops);", "null", "M4.P4", "42804"},
	{"V9 unique gin", "CREATE TABLE t (a int[]);\nCREATE UNIQUE INDEX t_a ON t USING gin (a array_ops);", "null", "M4.P4", "0A000"},
	{"V9 unique hash", "CREATE TABLE t (a int);\nCREATE UNIQUE INDEX t_a ON t USING hash (a);", "null", "M4.P4", "0A000"},
	{"V9 int4_ops on int", "CREATE TABLE t (a int);\nCREATE INDEX t_a ON t (a int4_ops);", "true", "", "ok"},
	{"V9 text_pattern_ops on varchar", "CREATE TABLE t (a varchar(10));\nCREATE INDEX t_a ON t (a text_pattern_ops);", "true", "", "ok"},
	{"V9 array_ops on int array", "CREATE TABLE t (a int[]);\nCREATE INDEX t_a ON t USING gin (a array_ops);", "true", "", "ok"},
	// V10 (P5): SET NAMES.
	{"V10 unknown encoding", "SET NAMES 'nosuch_enc';", "null", "N2.P5", "22023"},
	{"V10 utf8", "SET NAMES 'UTF-8';", "true", "", "ok"},
	{"V10 default", "SET NAMES DEFAULT;", "true", "", "ok"},
	{"V10 latin1", "SET client_encoding TO 'latin1';", "null", "N2.P5", "ok"},
	// V11 (P2): system column names in other forms.
	{"V11 matview output", "CREATE MATERIALIZED VIEW mv AS SELECT 1 AS cmax;", "null", "R1.P2", "42701"},
	{"V11 ctas output", "CREATE TABLE x AS SELECT 1 AS tableoid;", "null", "R1.P2", "42701"},
	{"V11 ctas implicit", "CREATE TABLE src (a int);\nCREATE TABLE x AS SELECT ctid FROM src;", "null", "R1.P2", "42701"},
	{"V11 select into", "SELECT 1 AS xmin INTO x;", "null", "R1.P2", "42701"},
	{"V11 like column", "CREATE TABLE src (a int);\nCREATE TABLE t (LIKE src, cmin int);", "null", "M1b.P2", "42701"},
	{"V11 inherits column", "CREATE TABLE p (a int);\nCREATE TABLE ch (xmin int) INHERITS (p);", "null", "M1b.P2", "42701"},
	{"V11 typed table", "CREATE TYPE c AS (ctid int);\nCREATE TABLE t OF c;", "null", "M1b.P2", "42701"},
	{"V11 add attribute cascade", "CREATE TYPE c AS (a int);\nCREATE TABLE t OF c;\nALTER TYPE c ADD ATTRIBUTE xmin int CASCADE;", "null", "R4.P2", "42701"},
	{"V11 view accepts", "CREATE VIEW v AS SELECT 1 AS xmin;", "true", "", "ok"},
	{"V11 add attribute without typed tables", "CREATE TYPE c AS (a int);\nALTER TYPE c ADD ATTRIBUTE xmin int;", "true", "", "ok"},
	// V12 (P5): storage parameters of views and materialized views.
	{"V12 alter table set on view", "CREATE TABLE t (a int);\nCREATE VIEW v AS SELECT a FROM t;\nALTER TABLE v SET (fillfactor = 50);", "null", "M5k-a.P5", "22023"},
	{"V12 view option value", "CREATE TABLE t (a int);\nCREATE VIEW v WITH (security_barrier = maybe) AS SELECT a FROM t;", "null", "R1.P5", "22023"},
	{"V12 matview fillfactor bound", "CREATE MATERIALIZED VIEW mv WITH (fillfactor = 5) AS SELECT 1 AS a;", "null", "R1.P5", "22023"},
	{"V12 alter view set", "CREATE TABLE t (a int);\nCREATE VIEW v AS SELECT a FROM t;\nALTER VIEW v SET (fillfactor = 50);", "null", "R4.P5", "22023"},
	{"V12 view security barrier", "CREATE TABLE t (a int);\nCREATE VIEW v WITH (security_barrier = true) AS SELECT a FROM t;", "true", "", "ok"},
	// V13 (P1, P4): types valid only in some positions.
	{"V13 composite serial", "CREATE TYPE c AS (a serial);", "null", "M2.P1", "42704"},
	{"V13 domain serial", "CREATE DOMAIN d AS serial;", "null", "M2.P1", "42704"},
	{"V13 function serial argument", "CREATE FUNCTION f(a serial) RETURNS int LANGUAGE sql AS 'SELECT 1';", "null", "M2.P1", "42704"},
	{"V13 unknown column", "CREATE TABLE t (a unknown);", "null", "M2.P4", "42P16"},
	{"V13 record attribute", "CREATE TYPE c AS (a record);", "null", "M2.P4", "42P16"},
	{"V13 void domain", "CREATE DOMAIN d AS void;", "null", "M2.P4", "42804"},
	{"V3 check is distinct", "CREATE TABLE t (a int, b int CHECK (a IS DISTINCT FROM b AND a NOT IN (1, 2)));", "null", "M1.P5", "ok"},
}

func TestRev16Cases(t *testing.T) {
	for _, tc := range rev16Cases {
		t.Run(tc.name, func(t *testing.T) {
			applied, diagnostics := linkSQL(t, tc.sql)
			if applied["1_m.sql"] != tc.want {
				t.Fatalf("applied %v, want %s; %v", applied, tc.want, diagnostics)
			}
			if tc.cite != "" && !cites(diagnostics, tc.cite) {
				t.Fatalf("no diagnostic cites %s: %v", tc.cite, diagnostics)
			}
			if tc.want == "true" && tc.pg != "ok" {
				t.Fatalf("the case claims applied, but Postgres refuses it (%s)", tc.pg)
			}
		})
	}
}

func TestRev16CasesPostgres(t *testing.T) {
	db, err := egress.OpenPostgres(oracleURL(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	for _, tc := range rev16Cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := runRolledBack(ctx, t, db, tc.sql); got != tc.pg {
				t.Fatalf("Postgres: %s, the case says %s", got, tc.pg)
			}
		})
	}
}
