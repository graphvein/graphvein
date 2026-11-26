package schema

import (
	"testing"
)

// T018a2 B1b (Revision 1 and 1.1): unit cases for the classes P4-P9 and
// the T018b1 syntax items, each with the verdict a Postgres 17 cluster
// gives (the oracle regression cases and generator g3 pin the same rules
// against live verdicts). want is the applied state of 1_m.sql; cite is
// the precondition a possible failure names.
func TestB1bStatements(t *testing.T) {
	for _, tc := range []struct {
		name string
		sql  string
		want string
		cite string
	}{
		// P4: foreign-key persistence, M5k-b words, kinds, labels.
		{"permanent references unlogged", "CREATE UNLOGGED TABLE a (id int PRIMARY KEY);\nCREATE TABLE b (id int REFERENCES a);\n", "null", "M3.P4"},
		{"unlogged references permanent", "CREATE TABLE a (id int PRIMARY KEY);\nCREATE UNLOGGED TABLE b (id int REFERENCES a);\n", "true", ""},
		{"SET UNLOGGED of a referenced table", "CREATE TABLE a (id int PRIMARY KEY);\nCREATE TABLE b (id int REFERENCES a);\nALTER TABLE a SET UNLOGGED;\n", "null", "M5k-a.P4"},
		{"SET UNLOGGED of a referencing table", "CREATE TABLE a (id int PRIMARY KEY);\nCREATE TABLE b (id int REFERENCES a);\nALTER TABLE b SET UNLOGGED;\n", "true", ""},
		{"temporary table with a foreign key", "CREATE TABLE a (id int PRIMARY KEY);\nCREATE TEMP TABLE b (id int REFERENCES a);\n", "null", "M1.P4"},
		{"ENABLE TRIGGER of a name", "CREATE TABLE a (id int);\nALTER TABLE a ENABLE TRIGGER x;\n", "null", "M5k-b.P4"},
		{"ENABLE TRIGGER ALL", "CREATE TABLE a (id int);\nALTER TABLE a ENABLE TRIGGER ALL;\n", "true", ""},
		{"identity on text", "CREATE TABLE a (c text NOT NULL);\nALTER TABLE a ALTER COLUMN c ADD GENERATED ALWAYS AS IDENTITY;\n", "null", "M5k-b.P4"},
		{"identity on integer", "CREATE TABLE a (c int NOT NULL);\nALTER TABLE a ALTER COLUMN c ADD GENERATED ALWAYS AS IDENTITY;\n", "true", ""},
		{"INHERIT with matching columns", "CREATE TABLE p (a int);\nCREATE TABLE c (a int, b text);\nALTER TABLE c INHERIT p;\n", "true", ""},
		{"INHERIT with a missing column", "CREATE TABLE p (a int, z int);\nCREATE TABLE c (a int);\nALTER TABLE c INHERIT p;\n", "null", "M5k-b.P4"},
		{"NO INHERIT of a parent", "CREATE TABLE p (a int);\nCREATE TABLE c (a int);\nALTER TABLE c INHERIT p;\nALTER TABLE c NO INHERIT p;\n", "true", ""},
		{"NO INHERIT of a non-parent", "CREATE TABLE p (a int);\nCREATE TABLE c (a int);\nALTER TABLE c NO INHERIT p;\n", "null", "M5k-b.P4"},
		{"SELECT of a procedure", "CREATE PROCEDURE p() LANGUAGE sql AS $$ SELECT 1 $$;\nSELECT p();\n", "null", "R6.P4"},
		{"CALL of a function", "CREATE FUNCTION f() RETURNS int LANGUAGE sql AS $$ SELECT 1 $$;\nCALL f();\n", "null", "R6.P4"},
		{"CALL of a procedure", "CREATE PROCEDURE p() LANGUAGE sql AS $$ SELECT 1 $$;\nCALL p();\n", "true", ""},
		{"DROP FUNCTION of a procedure", "CREATE PROCEDURE p() LANGUAGE sql AS $$ SELECT 1 $$;\nDROP FUNCTION IF EXISTS p();\n", "null", "R6.P4"},
		{"DROP ROUTINE of a procedure", "CREATE PROCEDURE p() LANGUAGE sql AS $$ SELECT 1 $$;\nDROP ROUTINE p();\n", "true", ""},
		{"DROP VIEW of a table made by CREATE TABLE AS", "CREATE TABLE a (id int);\nCREATE TABLE b AS SELECT * FROM a;\nDROP VIEW b;\n", "null", "R5.P4"},
		{"SECURITY LABEL", "SECURITY LABEL ON TABLE ext IS 'x';\n", "null", "N1.P4"},
		// P5: columns, defaults, casts, queries, volatility.
		{"COMMENT ON a missing column", "CREATE TABLE a (id int);\nCOMMENT ON COLUMN a.nosuch IS 'x';\n", "null", "N1.P5"},
		{"COMMENT ON a column", "CREATE TABLE a (id int);\nCOMMENT ON COLUMN a.id IS 'x';\n", "true", ""},
		{"CREATE STATISTICS", "CREATE TABLE a (x int, y int);\nCREATE STATISTICS s ON x, y FROM a;\n", "null", "N1.P5"},
		{"DEFAULT now() on a date", "CREATE TABLE a (d date DEFAULT now());\n", "true", ""},
		{"DEFAULT now() on an integer", "CREATE TABLE a (d int DEFAULT now());\n", "null", "M1.P5"},
		{"DEFAULT of invalid input", "CREATE TABLE a (n int DEFAULT 'abc');\n", "null", "M1.P5"},
		{"DEFAULT too long", "CREATE TABLE a (s varchar(3) DEFAULT 'abcd');\n", "null", "M1.P5"},
		{"DEFAULT of an outside function", "CREATE TABLE a (u uuid DEFAULT uuid_generate_v4());\n", "true", ""},
		{"DEFAULT of a timestamp constant", "CREATE TABLE a (t timestamp DEFAULT '1970-01-01 00:00:01+00:00');\n", "true", ""},
		{"SET DEFAULT of another type", "CREATE TABLE a (id bigint);\nALTER TABLE a ALTER COLUMN id SET DEFAULT now();\n", "null", "M5e.P5"},
		{"ALTER TYPE without an assignment cast", "CREATE TABLE a (c text);\nALTER TABLE a ALTER COLUMN c TYPE int;\n", "null", "M5c.P5"},
		{"ALTER TYPE with USING", "CREATE TABLE a (c text);\nALTER TABLE a ALTER COLUMN c TYPE int USING c::int;\n", "true", ""},
		{"ALTER TYPE with an impossible USING", "CREATE TABLE a (c uuid);\nALTER TABLE a ALTER COLUMN c TYPE int USING c::int;\n", "null", "M5c.P5"},
		{"ALTER TYPE widening", "CREATE TABLE a (c int DEFAULT 0);\nALTER TABLE a ALTER COLUMN c TYPE bigint;\n", "true", ""},
		{"ALTER TYPE of a column a CHECK names", "CREATE TABLE a (c int CHECK (c > 0));\nALTER TABLE a ALTER COLUMN c TYPE bigint;\n", "null", "M5c.P8"},
		{"view of a missing column", "CREATE TABLE a (id int);\nCREATE VIEW v AS SELECT nosuch FROM a;\n", "null", "R1b.P5"},
		{"view of a column", "CREATE TABLE a (id int);\nCREATE VIEW v AS SELECT id FROM a;\n", "true", ""},
		{"SQL body of a missing column", "CREATE TABLE a (id int);\nCREATE FUNCTION f() RETURNS bigint LANGUAGE sql AS $$ SELECT count(nosuch) FROM a $$;\n", "null", "R1b.P5"},
		{"SQL body of a constant", "CREATE FUNCTION f() RETURNS bigint LANGUAGE sql AS $$ SELECT 1::bigint $$;\n", "true", ""},
		{"index of a STABLE cast", "CREATE TABLE a (d date);\nCREATE INDEX ON a (lower(d::text));\n", "null", "M4.P5"},
		{"index of an IMMUTABLE function", "CREATE TABLE a (e varchar(80));\nCREATE INDEX ON a (lower(e));\n", "true", ""},
		{"index predicate of now()", "CREATE TABLE a (d timestamptz);\nCREATE INDEX ON a (d) WHERE d > now();\n", "null", "M4.P5"},
		{"index predicate IS NULL", "CREATE TABLE a (d timestamptz);\nCREATE INDEX ON a (d) WHERE d IS NULL;\n", "true", ""},
		{"generated column of now()", "CREATE TABLE a (t timestamptz GENERATED ALWAYS AS (now()) STORED);\n", "null", "M1.P5"},
		{"generated column of a column", "CREATE TABLE a (n numeric, g numeric GENERATED ALWAYS AS (n * 2) STORED);\n", "true", ""},
		// P6: block states.
		{"SAVEPOINT and RELEASE", "SAVEPOINT s;\nRELEASE SAVEPOINT s;\n", "true", ""},
		{"RELEASE of a missing savepoint", "RELEASE SAVEPOINT s;\n", "null", "T1.P6"},
		{"ROLLBACK after a change", "CREATE TABLE a (id int);\nROLLBACK;\n", "null", "T1.P6"},
		{"ROLLBACK first", "ROLLBACK;\nCREATE TABLE a (id int);\n", "true", ""},
		{"SAVEPOINT after COMMIT", "COMMIT;\nSAVEPOINT s;\n", "null", "T1.P6"},
		{"LOCK in the transaction", "CREATE TABLE a (id int);\nLOCK TABLE a;\n", "true", ""},
		{"LOCK alone without the transaction", "-- no-transaction\nLOCK TABLE ext;\n", "null", "T1.P6"},
		{"BEGIN left open without the transaction", "-- no-transaction\nBEGIN;\nCREATE TABLE a (id int);\n", "null", "T1.P6"},
		{"CREATE INDEX CONCURRENTLY", "CREATE INDEX CONCURRENTLY i ON ext (id);\n", "null", "M4.P6"},
		{"DISCARD ALL", "DISCARD ALL;\n", "null", "F1.P6"},
		// P7: syntax.
		{"unnamed CREATE INDEX IF NOT EXISTS", "CREATE TABLE a (id int);\nCREATE INDEX IF NOT EXISTS ON a (id);\n", "null", ""},
		{"DETACH in a list", "CREATE TABLE a (id int);\nALTER TABLE a ADD COLUMN b int, DETACH PARTITION a;\n", "null", ""},
		{"RENAME in a list", "CREATE TABLE a (id int);\nALTER TABLE a ADD COLUMN b int, RENAME COLUMN id TO c;\n", "null", ""},
		// P8: dependents.
		{"TRUNCATE of a referenced table", "CREATE TABLE a (id int PRIMARY KEY);\nCREATE TABLE b (a int REFERENCES a);\nTRUNCATE a;\n", "null", "N3.P8"},
		{"TRUNCATE of both tables", "CREATE TABLE a (id int PRIMARY KEY);\nCREATE TABLE b (a int REFERENCES a);\nTRUNCATE a, b;\n", "true", ""},
		{"TRUNCATE CASCADE", "CREATE TABLE a (id int PRIMARY KEY);\nCREATE TABLE b (a int REFERENCES a);\nTRUNCATE a CASCADE;\n", "true", ""},
		{"CLUSTER without a clustered index", "CREATE TABLE a (id int);\nCLUSTER a;\n", "null", "N3.P8"},
		{"CLUSTER USING, then again", "CREATE TABLE a (id int);\nCREATE INDEX i ON a (id);\nCLUSTER a USING i;\nCLUSTER a;\n", "true", ""},
		{"CLUSTER on a partial index", "CREATE TABLE a (id int);\nCREATE INDEX i ON a (id) WHERE id > 0;\nCLUSTER a USING i;\n", "null", "N3.P8"},
		// P9: subcommand order.
		{"ADD COLUMN then SET DEFAULT", "CREATE TABLE a (id int);\nALTER TABLE a ADD COLUMN b int, ALTER COLUMN b SET DEFAULT 1;\n", "true", ""},
		{"SET DEFAULT then ADD COLUMN", "CREATE TABLE a (id int);\nALTER TABLE a ALTER COLUMN b SET DEFAULT 1, ADD COLUMN b int;\n", "true", ""},
		{"ADD COLUMN then ALTER TYPE", "CREATE TABLE a (id int);\nALTER TABLE a ADD COLUMN b int, ALTER COLUMN b TYPE bigint;\n", "null", ""},
		{"SET NOT NULL then DROP COLUMN", "CREATE TABLE a (id int, b int);\nALTER TABLE a ALTER COLUMN b SET NOT NULL, DROP COLUMN b;\n", "null", ""},
		{"DROP COLUMN then ADD COLUMN", "CREATE TABLE a (id int, b int);\nALTER TABLE a DROP COLUMN b, ADD COLUMN b text;\n", "true", ""},
		{"ALTER TYPE twice", "CREATE TABLE a (b int);\nALTER TABLE a ALTER COLUMN b TYPE bigint, ALTER COLUMN b TYPE int;\n", "null", "M5a.P9"},
		{"SET STATISTICS and DROP COLUMN", "CREATE TABLE a (id int, b int);\nALTER TABLE a ALTER COLUMN b SET STATISTICS 10, DROP COLUMN b;\n", "null", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			applied, diagnostics := linkSQL(t, tc.sql)
			if applied["1_m.sql"] != tc.want {
				t.Fatalf("applied %v, want %s; %v", applied, tc.want, diagnostics)
			}
			if tc.cite != "" && !cites(diagnostics, tc.cite) {
				t.Fatalf("no diagnostic cites %s: %v", tc.cite, diagnostics)
			}
		})
	}
}

// TestB1bTransactionTable checks the P6 table entry by entry in each block
// state (transactions.go).
func TestB1bTransactionTable(t *testing.T) {
	for _, tc := range []struct {
		state blockState
		sql   string
		ok    bool
	}{
		{blockSqlx, "BEGIN", true}, {blockImplicit, "BEGIN", true}, {blockNone, "BEGIN", true},
		{blockSqlx, "SAVEPOINT s", true}, {blockImplicit, "SAVEPOINT s", false}, {blockNone, "SAVEPOINT s", false},
		{blockSqlx, "RELEASE s", false}, {blockImplicit, "RELEASE SAVEPOINT s", false},
		{blockSqlx, "LOCK TABLE t", true}, {blockImplicit, "LOCK TABLE t", true}, {blockNone, "LOCK TABLE t", false},
		{blockSqlx, "VACUUM", false}, {blockImplicit, "VACUUM", false}, {blockNone, "VACUUM", true},
		{blockNone, "CREATE INDEX CONCURRENTLY i ON t (a)", true}, {blockSqlx, "DROP INDEX CONCURRENTLY i", false},
		{blockSqlx, "CLUSTER", false}, {blockSqlx, "CLUSTER t", true}, {blockSqlx, "PREPARE TRANSACTION 'x'", false},
		{blockNone, "COMMIT", true}, {blockNone, "ROLLBACK", true},
	} {
		stmts, _ := lexStatements([]byte(tc.sql))
		p := &txPath{state: tc.state}
		kind, name, what := txStatement(stmts[0].tokens, []byte(tc.sql))
		o := p.step(kind, name, what)
		if (o.refused == "" && o.undo == "") != tc.ok {
			t.Errorf("%s in state %d: %+v, want ok %v", tc.sql, tc.state, o, tc.ok)
		}
	}
}

// TestB1bCatalog spot-checks the generated pg_catalog tables against
// REL_17_6 facts the proofs rely on.
func TestB1bCatalog(t *testing.T) {
	if castContext("int8", "int4") != 'a' || castContext("int4", "int8") != 'i' || castContext("text", "int4") != 'e' || castContext("int4", "text") != 'a' || castContext("uuid", "int4") != 0 {
		t.Error("cast contexts")
	}
	if castVolatility("date", "text") != 's' || castVolatility("int4", "text") != 'i' || castVolatility("varchar", "text") != 'i' {
		t.Error("cast volatility")
	}
	if procs := pgProcs["now"]; len(procs) != 1 || procs[0].ret != "timestamptz" || procs[0].vol != 's' {
		t.Errorf("now(): %v", procs)
	}
	if !btreeType("varchar") || btreeType("json") || !btreeType("int8") {
		t.Error("btree operator classes")
	}
}
