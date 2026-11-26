package schema

import (
	"testing"
)

// T018a2 B1c (Revision 1.2): the column references of DML and queries
// (class P1 extended to columns, precondition N3.P1). want is the applied
// state of 1_m.sql; cite is the precondition a possible failure names.
// Every "true" case is accepted by a Postgres 17 cluster, and every case
// whose comment says "refused" is refused by it (checked when the cases
// were written; the oracle regression cases g3-s1613, g3-s6790 pin the
// root against live verdicts). Cases marked "Revision 1.3" are accepted
// by Postgres but no longer claimed: their values, conditions or the
// constraints of the table are not proven (N3.P5, TestB1dValues).
func TestB1cColumns(t *testing.T) {
	const tbl = "CREATE TABLE a (id int PRIMARY KEY, name text, g int GENERATED ALWAYS AS (id * 2) STORED);\n"
	for _, tc := range []struct {
		name string
		sql  string
		want string
		cite string
	}{
		// UPDATE.
		{"UPDATE of a missing column (refused)", "CREATE TABLE t1 (c int);\nUPDATE t1 SET id = id;\n", "null", "N3.P1"},
		{"UPDATE of columns", tbl + "UPDATE a SET name = name || 'x' WHERE id = 1 AND name IS NOT NULL;\n", "null", "N3.P5"}, // Revision 1.3
		{"UPDATE reading a missing column (refused)", tbl + "UPDATE a SET name = nosuch;\n", "null", "N3.P1"},
		{"UPDATE WHERE a missing column (refused)", tbl + "UPDATE a SET name = 'x' WHERE nosuch = 1;\n", "null", "N3.P1"},
		{"UPDATE with an alias", tbl + "UPDATE a AS x SET name = x.name WHERE x.id = 1;\n", "null", "N3.P5"}, // Revision 1.3
		{"UPDATE qualified by the table behind an alias (refused)", tbl + "UPDATE a AS x SET name = a.name;\n", "null", "N3.P1"},
		{"UPDATE of a generated column (refused)", tbl + "UPDATE a SET g = 1;\n", "null", "N3.P1"},
		{"UPDATE SET DEFAULT", tbl + "UPDATE a SET name = DEFAULT;\n", "null", "N3.P5"},                                                        // Revision 1.3
		{"UPDATE with a cast and a function", tbl + "UPDATE a SET name = lower(id::text) WHERE name IS DISTINCT FROM 'x';\n", "null", "N3.P5"}, // Revision 1.3
		{"UPDATE of a partial table", "CREATE TABLE a (id int);\nDO $$ BEGIN END $$;\nUPDATE a SET id = id;\n", "null", "N3.P1"},
		{"UPDATE of an outside table", "UPDATE ext SET id = id;\n", "null", "N3.P1"},
		{"UPDATE ... FROM", tbl + "CREATE TABLE b (id int);\nUPDATE a SET name = 'x' FROM b WHERE a.id = b.id;\n", "null", "N3.P1"},
		{"UPDATE RETURNING", tbl + "UPDATE a SET name = 'x' RETURNING id, a.name AS n;\n", "null", "N3.P5"}, // Revision 1.3
		{"UPDATE RETURNING a missing column (refused)", tbl + "UPDATE a SET name = 'x' RETURNING nosuch;\n", "null", "N3.P1"},
		// INSERT.
		{"INSERT with a column list", tbl + "INSERT INTO a (id, name) VALUES (1, 'one'), (2, DEFAULT);\n", "null", "N3.P5"}, // Revision 1.3
		{"INSERT of a missing column (refused)", tbl + "INSERT INTO a (id, nosuch) VALUES (1, 'one');\n", "null", "N3.P1"},
		{"INSERT naming a column twice (refused)", tbl + "INSERT INTO a (id, id) VALUES (1, 2);\n", "null", "N3.P1"},
		{"INSERT into a generated column (refused)", tbl + "INSERT INTO a (id, g) VALUES (1, 2);\n", "null", "N3.P1"},
		{"INSERT of too many values (refused)", tbl + "INSERT INTO a (id) VALUES (1, 'one');\n", "null", "N3.P1"},
		{"INSERT without a column list", "CREATE TABLE b (id int, name text);\nINSERT INTO b VALUES (1, 'one');\n", "true", ""},
		{"INSERT without a column list, too many values (refused)", "CREATE TABLE b (id int);\nINSERT INTO b VALUES (1, 'one');\n", "null", "N3.P1"},
		{"INSERT VALUES naming a column (refused)", tbl + "INSERT INTO a (id) VALUES (id);\n", "null", "N3.P1"},
		{"INSERT DEFAULT VALUES of an outside table", "INSERT INTO ext DEFAULT VALUES;\n", "null", "N3.P5"}, // Revision 1.3
		{"INSERT into an outside table with a list", "INSERT INTO ext (id) VALUES (1);\n", "null", "N3.P1"},
		{"INSERT ON CONFLICT DO NOTHING", tbl + "INSERT INTO a (id) VALUES (1) ON CONFLICT DO NOTHING;\n", "null", "N3.P5"}, // Revision 1.3
		{"INSERT ON CONFLICT with a target", tbl + "INSERT INTO a (id) VALUES (1) ON CONFLICT (id) DO NOTHING;\n", "null", "N3.P1"},
		{"INSERT ... SELECT", tbl + "INSERT INTO a (id) SELECT 1;\n", "null", "N3.P1"},
		{"INSERT RETURNING *", tbl + "INSERT INTO a (id) VALUES (1) RETURNING *;\n", "null", "N3.P5"}, // Revision 1.3
		// DELETE.
		{"DELETE without WHERE of an outside table", "DELETE FROM ext;\n", "null", "N3.P5"},                       // Revision 1.3
		{"DELETE WHERE a column", tbl + "DELETE FROM a WHERE id IN (1, 2) OR name LIKE 'x%';\n", "null", "N3.P5"}, // Revision 1.3
		{"DELETE WHERE a missing column (refused)", tbl + "DELETE FROM a WHERE nosuch = 1;\n", "null", "N3.P1"},
		{"DELETE WHERE a subquery", tbl + "DELETE FROM a WHERE id IN (SELECT 1);\n", "null", "N3.P1"},
		// COPY and ANALYZE.
		{"COPY with a column list", tbl + "COPY a (id, name) TO STDOUT;\n", "true", ""},
		{"COPY of a missing column (refused)", tbl + "COPY a (nosuch) TO STDOUT;\n", "null", "N3.P1"},
		{"COPY without a column list", "COPY ext FROM '/tmp/x.csv';\n", "true", ""},
		{"ANALYZE with a column list", tbl + "ANALYZE a (id, name);\n", "true", ""},
		{"ANALYZE of a missing column (refused)", tbl + "ANALYZE a (nosuch);\n", "null", "N3.P1"},
		{"ANALYZE without a column list", "ANALYZE ext;\n", "true", ""},
		// Queries.
		{"SELECT of a qualified column (not read by the R1b reader)", tbl + "SELECT id, a.name FROM a;\n", "null", "N3.P1"},
		{"SELECT of a column", tbl + "SELECT id FROM a;\n", "true", ""},
		{"SELECT of a missing column (refused)", tbl + "SELECT nosuch FROM a;\n", "null", "N3.P1"},
		{"SELECT of constants and functions", "SELECT 1, lower('X') AS x, now()::date, current_timestamp;\n", "null", "N3.P5"}, // Revision 1.3
		{"SELECT of a name without FROM (refused)", "SELECT nosuch;\n", "null", "N3.P1"},
		{"SELECT * without FROM (refused)", "SELECT *;\n", "null", "N3.P1"},
		{"SELECT of a join", tbl + "CREATE TABLE b (id int);\nSELECT a.id FROM a JOIN b ON a.id = b.id;\n", "null", "N3.P1"},
		{"VALUES of constants", "VALUES (1, 'a'), (2, 'b');\n", "true", ""},
		{"VALUES of different lengths (refused)", "VALUES (1), (2, 3);\n", "null", "N3.P1"},
		{"WITH query", "WITH x AS (SELECT 1) SELECT * FROM x;\n", "null", "N3.P1"},
		{"MERGE", tbl + "MERGE INTO a USING (SELECT 1 AS id) s ON a.id = s.id WHEN NOT MATCHED THEN INSERT (id) VALUES (s.id);\n", "null", "N3.P1"},
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

// TestB1cKeywords checks the reader's keyword table against kwlist.h's
// categories: every non-column keyword is a keyword, and every word the
// reader skips inside an expression is one of them (so it cannot be a
// column reference).
func TestB1cKeywords(t *testing.T) {
	if len(nonColumnKeywords) != 101 {
		t.Errorf("nonColumnKeywords has %d entries, kwlist.h REL_17_6 has 101 reserved and type_func_name keywords", len(nonColumnKeywords))
	}
	for w := range nonColumnKeywords {
		if !sqlKeywords[w] {
			t.Errorf("%s is not in sqlKeywords", w)
		}
	}
	for w := range expressionKeywords {
		if !nonColumnKeywords[w] {
			t.Errorf("expression keyword %s may be a column reference", w)
		}
	}
	for w := range sqlValueFunctions {
		if !nonColumnKeywords[w] {
			t.Errorf("SQL value function %s may be a column reference", w)
		}
	}
}
