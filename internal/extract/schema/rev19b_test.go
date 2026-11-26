package schema

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/egress"
)

// Revision 1.9 step U-b cases (T018a2 Y03, Y04, Y13, Y23, Y24, Y27-Y29,
// Y33, Y34, Y38): each rule's refused, may-fail and accepted forms, as
// rev19Cases. rev19bCases are checked live by TestRev19bCasesPostgres
// (runRolledBack); rev19bTxCases, the transaction-mode cases of Y03, by
// TestRev19bTxCasesPostgres, which runs the case as sqlx runs a migration:
// first in its own transaction, followed by an INSERT into a bookkeeping
// table (a rolled-back transaction, so a session default it sets does not
// last; the post-commit UPDATE is the regression case review19-r1-b05).
var rev19bCases = []b1dCase{
	// Y04: SET CONSTRAINTS.
	{"Y04 a UNIQUE constraint deferred", "CREATE TABLE t (a int CONSTRAINT k UNIQUE);\nSET CONSTRAINTS k DEFERRED;", "null", "N2.P4", "42809"},
	{"Y04 a CHECK constraint deferred", "CREATE TABLE t (a int CONSTRAINT k CHECK (a > 0));\nSET CONSTRAINTS k DEFERRED;", "null", "N2.P4", "42809"},
	{"Y04 a column REFERENCES then a deferrable UNIQUE", "CREATE TABLE p (a int PRIMARY KEY);\nCREATE TABLE c (a int CONSTRAINT k REFERENCES p UNIQUE DEFERRABLE);\nSET CONSTRAINTS k DEFERRED;", "null", "N2.P4", "42809"},
	{"Y04 one of two constraints of the name", "CREATE TABLE t1 (a int CONSTRAINT k UNIQUE DEFERRABLE);\nCREATE TABLE t2 (a int CONSTRAINT k CHECK (a > 0));\nSET CONSTRAINTS k DEFERRED;", "null", "N2.P4", "42809"},
	{"Y04 a missing name", "CREATE TABLE t (a int);\nSET CONSTRAINTS nosuch DEFERRED;", "null", "N2.P4", "42704"},
	{"Y04 a missing name IMMEDIATE", "SET CONSTRAINTS nosuch IMMEDIATE;", "null", "N2.P4", "42704"},
	{"Y04 a generated name", "CREATE TABLE t (a int UNIQUE DEFERRABLE);\nSET CONSTRAINTS t_a_key DEFERRED;", "null", "N2.P4", "ok"},
	{"Y04 a qualified name", "CREATE TABLE t (a int CONSTRAINT k UNIQUE DEFERRABLE);\nSET CONSTRAINTS public.k DEFERRED;", "null", "N2.P4", "42704"},
	{"Y04 IMMEDIATE of a constraint that is not deferrable", "CREATE TABLE t (a int CONSTRAINT k UNIQUE);\nSET CONSTRAINTS k IMMEDIATE;", "true", "", "ok"},
	{"Y04 a deferrable foreign key", "CREATE TABLE p (a int PRIMARY KEY);\nCREATE TABLE c (a int, CONSTRAINT k FOREIGN KEY (a) REFERENCES p ON DELETE SET NULL DEFERRABLE);\nSET CONSTRAINTS k DEFERRED;", "true", "", "ok"},
	{"Y04 a deferrable key added and an ALL", "CREATE TABLE t (a int);\nALTER TABLE t ADD CONSTRAINT k UNIQUE (a) DEFERRABLE INITIALLY DEFERRED;\nSET CONSTRAINTS k, ALL IMMEDIATE;", "null", "N2.P4", "42601"},
	{"Y04 a deferrable key added", "CREATE TABLE t (a int);\nALTER TABLE t ADD CONSTRAINT k UNIQUE (a) DEFERRABLE INITIALLY DEFERRED;\nSET CONSTRAINTS k IMMEDIATE;\nSET CONSTRAINTS ALL DEFERRED;", "true", "", "ok"},
	{"Y04 a reserved word in the list", "CREATE TABLE t (a int CONSTRAINT k UNIQUE DEFERRABLE);\nSET CONSTRAINTS k, all DEFERRED;", "null", "N2.P4", "42601"},
	// Y13: role membership cycles.
	{"Y13 GRANT r TO r", "CREATE ROLE gv19b_a;\nGRANT gv19b_a TO gv19b_a;", "null", "N1.P2", "0LP01"},
	{"Y13 CREATE ROLE r IN ROLE r", "CREATE ROLE gv19b_a IN ROLE gv19b_a;", "null", "N1.P2", "0LP01"},
	{"Y13 a circular grant", "CREATE ROLE gv19b_a;\nCREATE ROLE gv19b_b;\nGRANT gv19b_a TO gv19b_b;\nGRANT gv19b_b TO gv19b_a;", "null", "N1.P2", "0LP01"},
	{"Y13 a cycle of three", "CREATE ROLE gv19b_a;\nCREATE ROLE gv19b_b;\nCREATE ROLE gv19b_c;\nGRANT gv19b_a TO gv19b_b;\nGRANT gv19b_b TO gv19b_c;\nGRANT gv19b_c TO gv19b_a;", "null", "N1.P2", "0LP01"},
	{"Y13 IN ROLE then the reverse grant", "CREATE ROLE gv19b_a;\nCREATE ROLE gv19b_b IN ROLE gv19b_a;\nGRANT gv19b_b TO gv19b_a;", "null", "N1.P2", "0LP01"},
	{"Y13 CREATE ROLE ... ROLE then the reverse grant", "CREATE ROLE gv19b_a;\nCREATE ROLE gv19b_b ROLE gv19b_a;\nGRANT gv19b_a TO gv19b_b;", "null", "N1.P2", "0LP01"},
	{"Y13 ALTER GROUP ADD USER then the reverse grant", "CREATE ROLE gv19b_g;\nCREATE ROLE gv19b_u;\nALTER GROUP gv19b_g ADD USER gv19b_u;\nGRANT gv19b_u TO gv19b_g;", "null", "N1.P2", "0LP01"},
	{"Y13 after a rename", "CREATE ROLE gv19b_a;\nCREATE ROLE gv19b_b;\nGRANT gv19b_a TO gv19b_b;\nALTER ROLE gv19b_a RENAME TO gv19b_a2;\nGRANT gv19b_b TO gv19b_a2;", "null", "N1.P2", "0LP01"},
	{"Y13 CURRENT_USER is not a grantable role name", "CREATE ROLE gv19b_a;\nGRANT CURRENT_USER TO gv19b_a;", "null", "N1.P2", "42601"},
	{"Y13 a chain", "CREATE ROLE gv19b_a;\nCREATE ROLE gv19b_b;\nCREATE ROLE gv19b_c;\nGRANT gv19b_a TO gv19b_b;\nGRANT gv19b_b TO gv19b_c WITH ADMIN OPTION;\nGRANT gv19b_a TO CURRENT_USER;", "true", "", "ok"},
	{"Y13 revoked before the reverse grant", "CREATE ROLE gv19b_a;\nCREATE ROLE gv19b_b;\nGRANT gv19b_a TO gv19b_b;\nREVOKE gv19b_a FROM gv19b_b;\nGRANT gv19b_b TO gv19b_a;", "true", "", "ok"},
	{"Y13 dropped before the reverse grant", "CREATE ROLE gv19b_a;\nCREATE ROLE gv19b_b;\nGRANT gv19b_a TO gv19b_b;\nDROP ROLE gv19b_a;\nCREATE ROLE gv19b_a;\nGRANT gv19b_b TO gv19b_a;", "true", "", "ok"},
	// Y38: roles with dependents.
	{"Y38 a table owner", "CREATE ROLE gv19b_r;\nCREATE TABLE t (a int);\nALTER TABLE t OWNER TO gv19b_r;\nDROP ROLE gv19b_r;", "null", "M5k-b.P4", "2BP01"},
	{"Y38 a table grant", "CREATE ROLE gv19b_r;\nCREATE TABLE t (a int);\nGRANT SELECT ON t TO gv19b_r;\nDROP ROLE gv19b_r;", "null", "N1.P8", "2BP01"},
	{"Y38 a grant partly revoked", "CREATE ROLE gv19b_r;\nCREATE TABLE t (a int);\nGRANT SELECT, INSERT ON TABLE t TO gv19b_r;\nREVOKE INSERT ON t FROM gv19b_r;\nDROP ROLE gv19b_r;", "null", "N1.P8", "2BP01"},
	{"Y38 a column grant revoked by the table privilege (not followed)", "CREATE ROLE gv19b_r;\nCREATE TABLE t (a int);\nGRANT SELECT (a) ON t TO gv19b_r;\nREVOKE SELECT ON t FROM gv19b_r;\nDROP ROLE gv19b_r;", "null", "N1.P8", "ok"},
	{"Y38 a policy", "CREATE ROLE gv19b_r;\nCREATE TABLE t (a int);\nCREATE POLICY p ON t TO gv19b_r USING (true);\nDROP ROLE gv19b_r;", "null", "N1.P8", "2BP01"},
	{"Y38 default privileges", "CREATE ROLE gv19b_r;\nALTER DEFAULT PRIVILEGES GRANT SELECT ON TABLES TO gv19b_r;\nDROP ROLE gv19b_r;", "null", "N1.P8", "2BP01"},
	{"Y38 GRANT ALL", "CREATE ROLE gv19b_r;\nCREATE TABLE t (a int);\nGRANT ALL ON t TO gv19b_r;\nDROP ROLE gv19b_r;", "null", "N1.P8", "2BP01"},
	{"Y38 GRANT ALL, one privilege revoked", "CREATE ROLE gv19b_r;\nCREATE TABLE t (a int);\nGRANT ALL PRIVILEGES ON TABLE t TO gv19b_r;\nREVOKE SELECT ON t FROM gv19b_r;\nDROP ROLE gv19b_r;", "null", "N1.P8", "2BP01"},
	{"Y38 a schema AUTHORIZATION", "CREATE ROLE gv19b_r;\nCREATE SCHEMA gv19b_s AUTHORIZATION gv19b_r;\nDROP ROLE gv19b_r;", "null", "N1.P8", "2BP01"},
	{"Y38 a sequence grant", "CREATE ROLE gv19b_r;\nCREATE SEQUENCE sq;\nGRANT USAGE ON SEQUENCE sq TO gv19b_r;\nDROP ROLE gv19b_r;", "null", "N1.P8", "2BP01"},
	{"Y38 the current role", "CREATE ROLE gv19b_r;\nSET ROLE gv19b_r;\nDROP ROLE gv19b_r;", "null", "N1.P8", "42501"},
	{"Y38 a grant revoked", "CREATE ROLE gv19b_r;\nCREATE TABLE t (a int);\nGRANT SELECT, INSERT ON t TO gv19b_r;\nREVOKE ALL ON t FROM gv19b_r;\nDROP ROLE gv19b_r;", "true", "", "ok"},
	{"Y38 a grant revoked by name", "CREATE ROLE gv19b_r;\nCREATE TABLE t (a int);\nGRANT SELECT, INSERT ON t TO gv19b_r;\nREVOKE SELECT, INSERT ON t FROM gv19b_r;\nDROP ROLE gv19b_r;", "true", "", "ok"},
	{"Y38 the table dropped", "CREATE ROLE gv19b_r;\nCREATE TABLE t (a int);\nGRANT SELECT ON t TO gv19b_r;\nDROP TABLE t;\nDROP ROLE gv19b_r;", "true", "", "ok"},
	{"Y38 the policy dropped", "CREATE ROLE gv19b_r;\nCREATE TABLE t (a int);\nCREATE POLICY p ON t TO gv19b_r USING (true);\nDROP POLICY p ON t;\nDROP ROLE gv19b_r;", "true", "", "ok"},
	{"Y38 memberships, comment and settings", "CREATE ROLE gv19b_a;\nCREATE ROLE gv19b_r IN ROLE gv19b_a;\nGRANT gv19b_r TO CURRENT_USER;\nCOMMENT ON ROLE gv19b_r IS 'x';\nALTER ROLE gv19b_r SET enable_seqscan = off;\nDROP ROLE gv19b_r, gv19b_a;", "true", "", "ok"},
	{"Y38 ownership moved away", "CREATE ROLE gv19b_r;\nCREATE TABLE t (a int);\nALTER TABLE t OWNER TO gv19b_r;\nALTER TABLE t OWNER TO CURRENT_USER;\nDROP ROLE gv19b_r;", "null", "M5k-b.P4", "ok"},
	// Y24: REFRESH ... CONCURRENTLY (an index on a materialized view is
	// already not proven, M4.P1, so the claimed side is not reached).
	{"Y24 no unique index", "CREATE MATERIALIZED VIEW mv AS SELECT 1 AS a;\nREFRESH MATERIALIZED VIEW CONCURRENTLY mv;", "null", "N3.P4", "55000"},
	{"Y24 a partial unique index", "CREATE MATERIALIZED VIEW mv AS SELECT 1 AS a;\nCREATE UNIQUE INDEX mv_a ON mv (a) WHERE a > 0;\nREFRESH MATERIALIZED VIEW CONCURRENTLY mv;", "null", "M4.P1", "55000"},
	{"Y24 an expression unique index", "CREATE MATERIALIZED VIEW mv AS SELECT 1 AS a;\nCREATE UNIQUE INDEX mv_a ON mv ((a + 1));\nREFRESH MATERIALIZED VIEW CONCURRENTLY mv;", "null", "M4.P1", "55000"},
	{"Y24 a plain index", "CREATE MATERIALIZED VIEW mv AS SELECT 1 AS a;\nCREATE INDEX mv_a ON mv (a);\nREFRESH MATERIALIZED VIEW CONCURRENTLY mv;", "null", "M4.P1", "55000"},
	{"Y24 the unique index dropped", "CREATE MATERIALIZED VIEW mv AS SELECT 1 AS a;\nCREATE UNIQUE INDEX mv_a ON mv (a);\nDROP INDEX mv_a;\nREFRESH MATERIALIZED VIEW CONCURRENTLY mv;", "null", "M4.P1", "55000"},
	{"Y24 created WITH NO DATA", "CREATE MATERIALIZED VIEW mv AS SELECT 1 AS a WITH NO DATA;\nCREATE UNIQUE INDEX mv_a ON mv (a);\nREFRESH MATERIALIZED VIEW CONCURRENTLY mv;", "null", "M4.P1", "0A000"},
	{"Y24 refreshed WITH NO DATA", "CREATE MATERIALIZED VIEW mv AS SELECT 1 AS a;\nCREATE UNIQUE INDEX mv_a ON mv (a);\nREFRESH MATERIALIZED VIEW mv WITH NO DATA;\nREFRESH MATERIALIZED VIEW CONCURRENTLY mv;", "null", "M4.P1", "0A000"},
	{"Y24 CONCURRENTLY WITH NO DATA", "CREATE MATERIALIZED VIEW mv AS SELECT 1 AS a;\nCREATE UNIQUE INDEX mv_a ON mv (a);\nREFRESH MATERIALIZED VIEW CONCURRENTLY mv WITH NO DATA;", "null", "M4.P1", "42601"},
	{"Y24 a view the pass does not hold", "REFRESH MATERIALIZED VIEW CONCURRENTLY gv19b_mv;", "null", "", "42P01"},
	{"Y24 a unique index", "CREATE MATERIALIZED VIEW mv AS SELECT 1 AS a, 2 AS b;\nCREATE UNIQUE INDEX ON mv (a, b DESC) INCLUDE (b);\nREFRESH MATERIALIZED VIEW CONCURRENTLY mv;", "null", "M4.P1", "ok"},
	{"Y24 populated again, renamed", "CREATE MATERIALIZED VIEW mv AS SELECT 1 AS a WITH NO DATA;\nCREATE UNIQUE INDEX mv_a ON mv USING btree (a);\nREFRESH MATERIALIZED VIEW mv;\nALTER MATERIALIZED VIEW mv RENAME TO mv2;\nALTER INDEX mv_a RENAME TO mv2_a;\nREFRESH MATERIALIZED VIEW CONCURRENTLY mv2;", "null", "M4.P1", "ok"},
	{"Y24 created WITH NO DATA without an index", "CREATE MATERIALIZED VIEW mv AS SELECT 1 AS a WITH NO DATA;\nREFRESH MATERIALIZED VIEW CONCURRENTLY mv;", "null", "N3.P4", "0A000"},
	{"Y24 CONCURRENTLY WITH NO DATA without an index", "CREATE MATERIALIZED VIEW mv AS SELECT 1 AS a;\nREFRESH MATERIALIZED VIEW CONCURRENTLY mv WITH NO DATA;", "null", "N3.P4", "42601"},
	// Y27: inheritance children's columns.
	{"Y27 ADD COLUMN of a type a child holds differently", "CREATE TABLE p (a int);\nCREATE TABLE c (b text) INHERITS (p);\nALTER TABLE p ADD COLUMN b int;", "null", "M5a.P8", "42804"},
	{"Y27 ADD COLUMN reaching a grandchild", "CREATE TABLE p (a int);\nCREATE TABLE c () INHERITS (p);\nCREATE TABLE g (x text) INHERITS (c);\nALTER TABLE p ADD COLUMN x int;", "null", "M5a.P8", "42804"},
	{"Y27 RENAME COLUMN to a child's column", "CREATE TABLE p (a int);\nCREATE TABLE c (b int) INHERITS (p);\nALTER TABLE p RENAME COLUMN a TO b;", "null", "M5h.P2", "42701"},
	{"Y27 ALTER COLUMN TYPE reaching the children", "CREATE TABLE p (a int);\nCREATE TABLE c (b int) INHERITS (p);\nALTER TABLE p ALTER COLUMN a TYPE bigint;", "null", "", "ok"},
	{"Y27 RENAME of an inherited column on the child", "CREATE TABLE p (a int);\nCREATE TABLE c (b int) INHERITS (p);\nALTER TABLE c RENAME COLUMN a TO z;", "null", "", "42P16"},
	{"Y27 a child column whose type differs from the parent's", "CREATE TABLE p (a int);\nCREATE TABLE c (a text) INHERITS (p);", "null", "M1b.P2", "42804"},
	{"Y27 two parents whose columns differ", "CREATE TABLE p1 (a int);\nCREATE TABLE p2 (a text);\nCREATE TABLE c () INHERITS (p1, p2);", "null", "M1b.P2", "42804"},
	{"Y27 ADD COLUMN merging into a child", "CREATE TABLE p (a int);\nCREATE TABLE c (b integer) INHERITS (p);\nALTER TABLE p ADD COLUMN b int4;", "true", "", "ok"},
	{"Y27 RENAME COLUMN to a free name", "CREATE TABLE p (a int);\nCREATE TABLE c (b int, a int) INHERITS (p);\nALTER TABLE p RENAME COLUMN a TO z;\nALTER TABLE p ADD COLUMN y int;", "true", "", "ok"},
	// Y28: LIKE, OF, INHERITS, PARTITION OF.
	{"Y28 LIKE and a column of its name", "CREATE TABLE t (a int);\nCREATE TABLE u (LIKE t INCLUDING ALL, a int);", "null", "M1b.P2", "42701"},
	{"Y28 two LIKE sources with a column in common", "CREATE TABLE t (a int);\nCREATE TABLE t2 (a int, b int);\nCREATE TABLE u (LIKE t, LIKE t2);", "null", "M1b.P2", "42701"},
	{"Y28 OF an enum", "CREATE TYPE e AS ENUM ('a');\nCREATE TABLE t OF e;", "null", "M1b.P2", "42809"},
	{"Y28 OF a table's row type", "CREATE TABLE r (a int);\nCREATE TABLE t OF r;", "null", "M1b.P2", "42809"},
	{"Y28 INHERITS a temporary table", "CREATE TEMP TABLE p (a int);\nCREATE TABLE c () INHERITS (p);", "null", "M1b.P2", "42809"},
	{"Y28 INHERITS a partitioned table", "CREATE TABLE p (a int) PARTITION BY RANGE (a);\nCREATE TABLE c () INHERITS (p);", "null", "M1b.P2", "42809"},
	{"Y28 INHERITS a view", "CREATE TABLE t (a int);\nCREATE VIEW v AS SELECT a FROM t;\nCREATE TABLE c () INHERITS (v);", "null", "M1b.P2", "42809"},
	{"Y28 INHERITS the same parent twice", "CREATE TABLE p (a int);\nCREATE TABLE c () INHERITS (p, p);", "null", "M1b.P2", "42P07"},
	{"Y28 PARTITION OF a table that is not partitioned", "CREATE TABLE p (a int);\nCREATE TABLE p1 PARTITION OF p FOR VALUES FROM (0) TO (10);", "null", "M1b.P4", "42P17"},
	{"Y28 LIKE and another column", "CREATE TABLE t (a int);\nCREATE TABLE u (LIKE t INCLUDING DEFAULTS, b int);", "true", "", "ok"},
	{"Y28 OF a composite type", "CREATE TYPE ct AS (a int, b text);\nCREATE TABLE t OF ct;", "true", "", "ok"},
	{"Y28 a temporary child of a temporary table", "CREATE TEMP TABLE p (a int);\nCREATE TEMP TABLE c () INHERITS (p);", "true", "", "ok"},
	// Y29: partition bounds (HASH partitioning is already not proven,
	// M1.P4).
	{"Y29 overlapping ranges", "CREATE TABLE p (a int) PARTITION BY RANGE (a);\nCREATE TABLE p1 PARTITION OF p FOR VALUES FROM (1) TO (10);\nCREATE TABLE p2 PARTITION OF p FOR VALUES FROM (5) TO (15);", "null", "M1b.P5", "42P17"},
	{"Y29 a second DEFAULT", "CREATE TABLE p (a int) PARTITION BY LIST (a);\nCREATE TABLE pd PARTITION OF p DEFAULT;\nCREATE TABLE pd2 PARTITION OF p DEFAULT;", "null", "M1b.P5", "42P17"},
	{"Y29 a list bound on a range table", "CREATE TABLE p (a int) PARTITION BY RANGE (a);\nCREATE TABLE p1 PARTITION OF p FOR VALUES IN (1);", "null", "M1b.P5", "42P16"},
	{"Y29 an empty range", "CREATE TABLE p (a int) PARTITION BY RANGE (a);\nCREATE TABLE p1 PARTITION OF p FOR VALUES FROM (10) TO (5);", "null", "M1b.P5", "42P17"},
	{"Y29 a list value held by another partition", "CREATE TABLE p (a int) PARTITION BY LIST (a);\nCREATE TABLE p1 PARTITION OF p FOR VALUES IN (1, 2);\nCREATE TABLE p2 PARTITION OF p FOR VALUES IN (2, 3);", "null", "M1b.P5", "42P17"},
	{"Y29 a bound the key type rejects", "CREATE TABLE p (a int) PARTITION BY RANGE (a);\nCREATE TABLE p1 PARTITION OF p FOR VALUES FROM ('x') TO ('y');", "null", "M1b.P5", "22P02"},
	{"Y29 DEFAULT of a hash table", "CREATE TABLE p (a int) PARTITION BY HASH (a);\nCREATE TABLE pd PARTITION OF p DEFAULT;", "null", "M1.P4", "42P16"},
	{"Y29 a remainder not below the modulus", "CREATE TABLE p (a int) PARTITION BY HASH (a);\nCREATE TABLE p1 PARTITION OF p FOR VALUES WITH (MODULUS 4, REMAINDER 4);", "null", "M1.P4", "42P16"},
	{"Y29 a modulus that is not a factor", "CREATE TABLE p (a int) PARTITION BY HASH (a);\nCREATE TABLE p1 PARTITION OF p FOR VALUES WITH (MODULUS 4, REMAINDER 0);\nCREATE TABLE p2 PARTITION OF p FOR VALUES WITH (MODULUS 6, REMAINDER 1);", "null", "M1.P4", "42P17"},
	{"Y29 a hash remainder held", "CREATE TABLE p (a int) PARTITION BY HASH (a);\nCREATE TABLE p1 PARTITION OF p FOR VALUES WITH (MODULUS 4, REMAINDER 1);\nCREATE TABLE p2 PARTITION OF p FOR VALUES WITH (MODULUS 8, REMAINDER 5);", "null", "M1.P4", "42P17"},
	{"Y29 a range attached over another", "CREATE TABLE p (a int) PARTITION BY RANGE (a);\nCREATE TABLE p1 PARTITION OF p FOR VALUES FROM (MINVALUE) TO (10);\nCREATE TABLE p2 (a int);\nALTER TABLE p ATTACH PARTITION p2 FOR VALUES FROM (9) TO (20);", "null", "M1b.P5", "42P17"},
	{"Y29 a timestamptz key", "CREATE TABLE p (a timestamptz) PARTITION BY RANGE (a);\nCREATE TABLE p1 PARTITION OF p FOR VALUES FROM ('2020-01-01') TO ('2021-01-01');", "null", "M1b.P5", "ok"},
	{"Y29 a text range", "CREATE TABLE p (a text) PARTITION BY RANGE (a);\nCREATE TABLE p1 PARTITION OF p FOR VALUES FROM ('a') TO ('m');", "null", "M1b.P5", "ok"},
	{"Y29 adjacent ranges and a default", "CREATE TABLE p (a bigint, b text) PARTITION BY RANGE (a);\nCREATE TABLE p0 PARTITION OF p FOR VALUES FROM (MINVALUE) TO (1);\nCREATE TABLE p1 PARTITION OF p FOR VALUES FROM (1) TO ('10');\nCREATE TABLE p2 PARTITION OF p FOR VALUES FROM (10) TO (MAXVALUE);\nCREATE TABLE pd PARTITION OF p DEFAULT;", "true", "", "ok"},
	{"Y29 text and NULL list values", "CREATE TABLE p (a text) PARTITION BY LIST (a);\nCREATE TABLE p1 PARTITION OF p FOR VALUES IN ('x', 'y', 'x');\nCREATE TABLE p2 PARTITION OF p FOR VALUES IN (NULL, 'z');", "true", "", "ok"},
	{"Y29 hash partitions", "CREATE TABLE p (a int) PARTITION BY HASH (a);\nCREATE TABLE p1 PARTITION OF p FOR VALUES WITH (MODULUS 4, REMAINDER 0);\nCREATE TABLE p2 PARTITION OF p FOR VALUES WITH (MODULUS 4, REMAINDER 1);\nCREATE TABLE p3 PARTITION OF p FOR VALUES WITH (MODULUS 8, REMAINDER 2);\nCREATE TABLE p4 PARTITION OF p FOR VALUES WITH (MODULUS 8, REMAINDER 6);", "null", "M1.P4", "ok"},
	{"Y29 bounds freed by DETACH", "CREATE TABLE p (a int) PARTITION BY LIST (a);\nCREATE TABLE p1 PARTITION OF p FOR VALUES IN (1);\nCREATE TABLE p2 PARTITION OF p FOR VALUES IN (2);\nALTER TABLE p DETACH PARTITION p1;\nALTER TABLE p DETACH PARTITION p2;\nCREATE TABLE p3 PARTITION OF p FOR VALUES IN (1, 2);", "null", "M5k-b.P4", "ok"},
	// Y33: extension versions and options.
	{"Y33 a version without a script", "CREATE EXTENSION pgcrypto VERSION '9.9';", "null", "O2.P4", "22023"},
	{"Y33 VERSION twice", "CREATE EXTENSION pgcrypto VERSION '1.3' VERSION '1.3';", "null", "O2.P7", "42601"},
	{"Y33 SCHEMA twice", "CREATE EXTENSION IF NOT EXISTS pgcrypto SCHEMA public SCHEMA public;", "null", "O2.P7", "42601"},
	{"Y33 UPDATE TO a version without a path", "CREATE EXTENSION pgcrypto;\nALTER EXTENSION pgcrypto UPDATE TO '9.9';", "null", "O2.P4", "22023"},
	{"Y33 an older version", "CREATE EXTENSION hstore VERSION '1.4';", "null", "O2.P4", "ok"},
	{"Y33 an extension outside the table", "CREATE EXTENSION plpgsql VERSION '1.0';", "null", "O2.P4", "42710"},
	{"Y33 plpgsql is installed", "CREATE EXTENSION plpgsql;", "null", "O2.P4", "42710"},
	{"Y33 plpgsql IF NOT EXISTS", "CREATE EXTENSION IF NOT EXISTS plpgsql;", "true", "", "ok"},
	{"Y33 installed twice", "CREATE EXTENSION pgcrypto;\nCREATE EXTENSION pgcrypto;", "null", "O2.P4", "42710"},
	{"Y33 the default version, UPDATE to it", "CREATE EXTENSION pgcrypto WITH SCHEMA public VERSION '1.3' CASCADE;\nALTER EXTENSION pgcrypto UPDATE TO '1.3';\nALTER EXTENSION pgcrypto UPDATE;\nCREATE EXTENSION IF NOT EXISTS pgcrypto;", "true", "", "ok"},
	// Y34: extension members in use.
	{"Y34 a default calls a member", "CREATE EXTENSION pgcrypto;\nCREATE TABLE t (a bytea DEFAULT gen_random_bytes(4));\nDROP EXTENSION pgcrypto;", "null", "O2.P8", "2BP01"},
	{"Y34 uuid-ossp's function in a default", "CREATE EXTENSION IF NOT EXISTS \"uuid-ossp\";\nCREATE TABLE t (id uuid PRIMARY KEY DEFAULT uuid_generate_v4());\nDROP EXTENSION IF EXISTS \"uuid-ossp\";", "null", "O2.P8", "2BP01"},
	{"Y34 a required extension", "CREATE EXTENSION cube;\nCREATE EXTENSION earthdistance;\nDROP EXTENSION cube;", "null", "O2.P4", "2BP01"},
	{"Y34 an extension with operators", "CREATE EXTENSION hstore;\nCREATE TABLE t (a int);\nDROP EXTENSION hstore;", "null", "O2.P8", "ok"},
	{"Y34 the user dropped first", "CREATE EXTENSION pgcrypto;\nCREATE TABLE t (a bytea DEFAULT gen_random_bytes(4));\nDROP TABLE t;\nCREATE TABLE u (b int);\nDROP EXTENSION pgcrypto;", "true", "", "ok"},
	{"Y34 both dropped together", "CREATE EXTENSION cube;\nCREATE EXTENSION earthdistance;\nDROP EXTENSION earthdistance, cube;", "null", "O2.P4", "ok"},
	// Y23: VALUES column types.
	{"Y23 an integer and a string", "CREATE TABLE t (a int);\nVALUES (1), ('a');", "null", "N3.P5", "22P02"},
	{"Y23 an integer and a boolean", "VALUES (1), (true);", "null", "N3.P5", "42804"},
	{"Y23 a bigint and a string", "VALUES (1), (3000000000), ('x');", "null", "N3.P5", "22P02"},
	{"Y23 in a derived table", "SELECT * FROM (VALUES (1, 'a'), ('b', 'c')) AS v (x, y);", "null", "N3.P5", "22P02"},
	{"Y23 a view AS VALUES", "CREATE VIEW v AS VALUES (1), ('x');", "null", "N3.P5", "22P02"},
	{"Y23 rows of different lengths", "SELECT * FROM (VALUES (1, 2), (3)) AS v;", "null", "N3.P5", "42601"},
	{"Y23 a typed text and an integer", "VALUES ('a'::text), (1);", "null", "N3.P5", "42804"},
	{"Y23 a function call", "VALUES (now()), ('a');", "null", "N3.P5", "22007"},
	{"Y23 integers and a numeric string", "VALUES (1), (2.5), ('3.5'), (NULL);", "true", "", "ok"},
	{"Y23 strings and NULL", "VALUES ('a', NULL), ('b', NULL), (NULL, NULL);", "true", "", "ok"},
	{"Y23 a boolean and its word", "VALUES (true), ('t'), ('off');", "true", "", "ok"},
	{"Y23 an integer string", "VALUES (1), ('2'), (-3);", "true", "", "ok"},
	{"Y23 INSERT VALUES is not common-typed (N3.P5 does not prove 1 for text)", "CREATE TABLE t (a text);\nINSERT INTO t VALUES (1), ('a');", "null", "N3.P5", "ok"},
	{"Y23 INSERT VALUES coerces each row", "CREATE TABLE t (a text);\nINSERT INTO t VALUES ('b'), ('a');", "true", "", "ok"},
	{"Y23 a single row", "VALUES (1, 'a', true);", "true", "", "ok"},
}

var rev19bTxCases = []b1dCase{
	// Y03: transaction modes.
	{"Y03 READ ONLY then a write", "CREATE TABLE t (a int);\nSET TRANSACTION READ ONLY;\nCREATE TABLE u (a int);", "null", "N2.P6", "25006"},
	{"Y03 READ ONLY alone (the bookkeeping INSERT)", "SET TRANSACTION READ ONLY;", "null", "Q1.P13", "25006"},
	{"Y03 READ ONLY then a query", "SET TRANSACTION READ ONLY;\nSELECT 1;", "null", "N2.P6", "25006"},
	{"Y03 READ ONLY then READ WRITE before a query", "SET TRANSACTION READ ONLY;\nSET TRANSACTION READ WRITE;\nCREATE TABLE t (a int);", "true", "", "ok"},
	{"Y03 READ ONLY, a query, READ WRITE", "SET TRANSACTION READ ONLY;\nSELECT 1;\nSET TRANSACTION READ WRITE;", "null", "N2.P6", "25001"},
	{"Y03 READ WRITE after a query in a read-write transaction", "CREATE TABLE t (a int);\nSET TRANSACTION READ WRITE;", "true", "", "ok"},
	{"Y03 ISOLATION to another level after a query", "CREATE TABLE t (a int);\nSET TRANSACTION ISOLATION LEVEL SERIALIZABLE;", "null", "N2.P6", "25001"},
	{"Y03 ISOLATION to the same level after a query", "CREATE TABLE t (a int);\nSET TRANSACTION ISOLATION LEVEL READ COMMITTED;", "true", "", "ok"},
	{"Y03 ISOLATION first", "SET TRANSACTION ISOLATION LEVEL SERIALIZABLE;\nCREATE TABLE t (a int);", "true", "", "ok"},
	{"Y03 ISOLATION twice before a query", "SET TRANSACTION ISOLATION LEVEL REPEATABLE READ;\nSET TRANSACTION ISOLATION LEVEL SERIALIZABLE;\nCREATE TABLE t (a int);", "true", "", "ok"},
	{"Y03 ISOLATION after COMMIT and LOCK", "CREATE TABLE t (a int);\nCOMMIT;\nLOCK TABLE t;\nSET TRANSACTION ISOLATION LEVEL SERIALIZABLE;", "true", "", "ok"},
	{"Y03 transaction_isolation after a query", "SELECT 1;\nSET transaction_isolation TO 'repeatable read';", "null", "N2.P6", "25001"},
	{"Y03 ISOLATION in a savepoint", "SAVEPOINT a;\nSET TRANSACTION ISOLATION LEVEL SERIALIZABLE;\nRELEASE a;", "null", "N2.P6", "25001"},
	{"Y03 the same ISOLATION in a savepoint", "SAVEPOINT a;\nSET TRANSACTION ISOLATION LEVEL READ COMMITTED;\nRELEASE a;", "true", "", "ok"},
	{"Y03 DEFERRABLE after a query", "SELECT 1;\nSET TRANSACTION DEFERRABLE;", "null", "N2.P6", "25001"},
	{"Y03 NOT DEFERRABLE after a query", "CREATE TABLE t (a int);\nSET TRANSACTION NOT DEFERRABLE;", "null", "N2.P6", "25001"},
	{"Y03 DEFERRABLE first", "SET TRANSACTION ISOLATION LEVEL SERIALIZABLE, READ WRITE, DEFERRABLE;\nCREATE TABLE t (a int);", "true", "", "ok"},
	{"Y03 SNAPSHOT after a query", "CREATE TABLE t (a int);\nSET TRANSACTION SNAPSHOT '00000003-0000001B-1';", "null", "N2.P6", "25001"},
	{"Y03 SNAPSHOT under READ COMMITTED", "SET TRANSACTION SNAPSHOT '00000003-0000001B-1';", "null", "N2.P6", "0A000"},
	{"Y03 SET SESSION CHARACTERISTICS then a write", "SET SESSION CHARACTERISTICS AS TRANSACTION READ ONLY;\nCREATE TABLE t (a int);", "true", "", "ok"},
	{"Y03 SET default_transaction_read_only then a write", "SET default_transaction_read_only = on;\nCREATE TABLE t (a int);", "true", "", "ok"},
	{"Y03 SET LOCAL default_transaction_read_only", "SET LOCAL default_transaction_read_only = on;\nCREATE TABLE t (a int);", "true", "", "ok"},
	{"Y03 a default set and reset", "SET default_transaction_read_only = on;\nRESET default_transaction_read_only;\nSET default_transaction_isolation = 'serializable';\nSET default_transaction_isolation TO DEFAULT;", "true", "", "ok"},
	{"Y03 a default in a no-transaction migration", "-- no-transaction\nSET default_transaction_read_only = on;\nSELECT 1;", "null", "N2.P13", "ok"},
	{"Y03 COMMIT then a read-only default", "SET default_transaction_read_only = on;\nCOMMIT;\nCREATE TABLE t (a int);", "null", "N2.P6", "25006"},
}

func TestRev19bCases(t *testing.T) {
	for _, tc := range append(append([]b1dCase{}, rev19bCases...), rev19bTxCases...) {
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

// TestRev19bSessionDefaults checks Y03's session-default rule across
// migrations: a committed default that makes later transactions read-only
// stops sqlx at its post-commit UPDATE (H24), so the later migrations are
// not applied; the migration that set it is.
func TestRev19bSessionDefaults(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files []string
		want  string
	}{
		{"default_transaction_read_only", []string{"CREATE TABLE t (a int);\nSET default_transaction_read_only = on;", "CREATE TABLE u (a int);"}, "true null"},
		{"SESSION CHARACTERISTICS", []string{"SET SESSION CHARACTERISTICS AS TRANSACTION READ ONLY;", "SELECT 1;"}, "true null"},
		{"set and reset", []string{"SET default_transaction_read_only = on;\nSET default_transaction_read_only = off;", "CREATE TABLE u (a int);"}, "true true"},
		{"isolation default then ISOLATION after a query", []string{"SET default_transaction_isolation = 'serializable';", "SELECT 1;\nSET TRANSACTION ISOLATION LEVEL SERIALIZABLE;"}, "true true"},
		{"isolation default then another level after a query", []string{"SET SESSION CHARACTERISTICS AS TRANSACTION ISOLATION LEVEL REPEATABLE READ;", "SELECT 1;\nSET TRANSACTION ISOLATION LEVEL READ COMMITTED;"}, "true null"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			applied, diagnostics := linkSQL(t, tc.files...)
			if got := applied["1_m.sql"] + " " + applied["2_m.sql"]; got != tc.want {
				t.Fatalf("applied %s, want %s; %v", got, tc.want, diagnostics)
			}
		})
	}
}

func TestRev19bCasesPostgres(t *testing.T) {
	db, err := egress.OpenPostgres(oracleURL(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	for _, tc := range rev19bCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := runRolledBack(ctx, t, db, tc.sql); got != tc.pg {
				t.Fatalf("Postgres: %s, the case says %s", got, tc.pg)
			}
		})
	}
	for _, tc := range rev19bTxCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := runAsMigration(ctx, t, db, tc.sql); got != tc.pg {
				t.Fatalf("Postgres: %s, the case says %s", got, tc.pg)
			}
		})
	}
}

// runAsMigration runs sqlText as the first statements of a transaction,
// then an INSERT into a bookkeeping table, as sqlx runs a migration, and
// rolls back. The schema and table are created before, outside the
// transaction, and dropped after.
func runAsMigration(ctx context.Context, t *testing.T, db *sql.DB, sqlText string) string {
	t.Helper()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	schema := fmt.Sprintf("y03_%d", len(sqlText))
	if _, err := conn.ExecContext(ctx, fmt.Sprintf("DROP SCHEMA IF EXISTS %[1]s CASCADE; CREATE SCHEMA %[1]s; CREATE TABLE %[1]s.bk (v int); SET search_path TO %[1]s", schema)); err != nil {
		t.Fatal(err)
	}
	defer conn.ExecContext(ctx, fmt.Sprintf("ROLLBACK; RESET ALL; DROP SCHEMA IF EXISTS %s CASCADE", schema))
	if _, err := conn.ExecContext(ctx, "BEGIN"); err != nil {
		t.Fatal(err)
	}
	result := "ok"
	if _, err := conn.ExecContext(ctx, sqlText); err != nil {
		result = strings.TrimSpace(err.Error())
		if pe, ok := readPgError(err); ok {
			result = pe.code
		}
	} else if _, err := conn.ExecContext(ctx, "INSERT INTO "+schema+".bk VALUES (1)"); err != nil {
		result = strings.TrimSpace(err.Error())
		if pe, ok := readPgError(err); ok {
			result = pe.code
		}
	}
	conn.ExecContext(ctx, "ROLLBACK")
	return result
}
