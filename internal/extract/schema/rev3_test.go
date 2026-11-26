package schema

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
)

// linkCase links one Postgres sqlx set written as a case text: each
// "-- FILE: name" line starts a migration file. The Revision 3
// probes are written this way.
func linkCase(t *testing.T, text string) linked {
	t.Helper()
	root := t.TempDir()
	manifest := "[package]\nname = \"case\"\nversion = \"0.1.0\"\n\n[dependencies]\nsqlx = { version = \"0.8.6\", features = [\"postgres\"] }\n"
	if err := os.WriteFile(filepath.Join(root, "Cargo.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "migrations")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var name string
	var body strings.Builder
	flush := func() {
		if name == "" {
			return
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		body.Reset()
	}
	for _, line := range strings.SplitAfter(text, "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimRight(line, "\n"), "-- FILE: "); ok {
			flush()
			name = strings.TrimSpace(rest)
			continue
		}
		body.WriteString(line)
	}
	flush()
	return linkRoot(t, root)
}

// find returns the node of a kind by qualified name ("" when absent); for
// migrations the qualified name is the file name.
func (l linked) find(kind, qn string) (contract.Node, bool) {
	for _, n := range l.nodes {
		if n.Kind != kind {
			continue
		}
		if kind == "migration" && filepath.Base(n.File) == qn || kind != "migration" && n.QualifiedName == qn {
			return n, true
		}
	}
	return contract.Node{}, false
}

// fact reads one fact of a linked case: "state t", "applied 1_a.sql",
// "column t.a" (present or absent), "index t.i", "<attr> <kind> <qn>",
// "reason t <substring>", "diag <code>", "opaque <kind>".
func (l linked) fact(t *testing.T, query string) any {
	t.Helper()
	f := strings.Fields(query)
	switch f[0] {
	case "state":
		if n, ok := l.find("table", f[1]); ok {
			return n.Attributes["state"]
		}
		return "absent"
	case "applied":
		n, ok := l.find("migration", f[1])
		if !ok {
			t.Fatalf("no migration %s", f[1])
		}
		return n.Attributes["applied"]
	case "column", "index":
		kind := map[string]string{"column": "column", "index": "db_index"}[f[0]]
		_, ok := l.find(kind, f[1])
		return ok
	case "reason":
		n, ok := l.find("table", f[1])
		if !ok {
			return false
		}
		want := strings.Join(f[2:], " ")
		for _, r := range n.Attributes["reasons"].([]any) {
			if strings.Contains(r.(map[string]any)["reason"].(string), want) {
				return true
			}
		}
		return false
	case "diag":
		for _, d := range l.diagnostics {
			if d.Code == f[1] {
				return true
			}
		}
		return false
	case "opaque":
		n, ok := l.find("migration", "1_a.sql")
		if !ok {
			t.Fatal("no migration 1_a.sql")
		}
		return n.Attributes["set_opaque_statements"].(map[string]any)["by_kind"].(map[string]any)[f[1]]
	}
	kind := map[string]string{"table": "table", "column": "column", "ref": "column"}[f[1]]
	n, ok := l.find(kind, f[2])
	if !ok {
		t.Fatalf("%s: no %s %s", query, kind, f[2])
	}
	if f[1] == "ref" {
		ref, _ := n.Attributes["references"].(map[string]any)
		return ref[f[0]]
	}
	return n.Attributes[f[0]]
}

// TestRevisionThreeCases covers Revision 3: each
// class rule with its probes (p01-p22) and further forms
// of the same class. Every case failed on the Revision 2 code in at least
// one expectation.
func TestRevisionThreeCases(t *testing.T) {
	type want map[string]any
	for _, tc := range []struct {
		name, sql string
		want      want
	}{
		// Class 1: dependency drops.
		{"p01 DROP VIEW CASCADE drops a column of its row type", `-- FILE: 1_a.sql
CREATE VIEW v AS SELECT 1 AS x;
CREATE TABLE t (id int, r v);
CREATE TABLE plain (id int);
-- FILE: 2_b.sql
DROP VIEW v CASCADE;
`, want{"state t": "partial", "state plain": "complete", "applied 2_b.sql": true}},
		{"p04 DROP TYPE CASCADE through a domain", `-- FILE: 1_a.sql
CREATE TYPE mood AS ENUM ('a','b');
CREATE DOMAIN dmood AS mood;
CREATE TABLE t (id int, m dmood);
-- FILE: 2_b.sql
DROP TYPE mood CASCADE;
`, want{"state t": "partial"}},
		{"p06 DROP TABLE CASCADE drops a column of its row type", `-- FILE: 1_a.sql
CREATE TABLE a (id int);
CREATE TABLE b (id int, r a);
CREATE TABLE c (id int, at timestamptz DEFAULT now());
CREATE TABLE d (id int);
DROP TABLE a CASCADE;
`, want{"state b": "partial", "state c": "partial", "state d": "complete", "state a": "absent"}},
		{"DROP FUNCTION CASCADE: tables with expressions are partial", `-- FILE: 1_a.sql
CREATE TABLE t (id int CHECK (id > 0));
CREATE TABLE u (id int);
CREATE INDEX u_lower ON u ((id + 1));
CREATE TABLE plain (id int, name text);
DROP FUNCTION whatever(int) CASCADE;
`, want{"state t": "partial", "state u": "partial", "state plain": "complete"}},
		{"DROP SERVER CASCADE drops foreign tables whose row types columns use", `-- FILE: 1_a.sql
CREATE TABLE plain (id int);
-- FILE: 2_b.sql
CREATE TABLE t (id int, r remote_rows);
DROP SERVER s CASCADE;
`, want{"state t": "partial", "state plain": "complete"}},
		{"DROP SCHEMA CASCADE: types of the schema", `-- FILE: 1_a.sql
CREATE TABLE app.plain (id int);
-- FILE: 2_b.sql
CREATE TABLE app.t (id int, m kinds.mood);
DROP SCHEMA kinds CASCADE;
`, want{"state app.t": "partial", "state app.plain": "complete"}},
		{"DROP VIEW without CASCADE while a column uses its row type", `-- FILE: 1_a.sql
CREATE VIEW v AS SELECT 1 AS x;
CREATE TABLE t (id int, r v);
-- FILE: 2_b.sql
DROP VIEW v;
-- FILE: 3_c.sql
CREATE TABLE later (id int);
`, want{"applied 2_b.sql": nil, "applied 3_c.sql": nil, "state later": "partial", "diag schema_migration_may_fail": true}},
		{"DROP DOMAIN without CASCADE while a column uses it", `-- FILE: 1_a.sql
CREATE DOMAIN d AS int;
CREATE TABLE t (id d);
-- FILE: 2_b.sql
DROP DOMAIN d;
`, want{"applied 2_b.sql": nil, "state t": "partial"}},
		{"DROP SEQUENCE of a serial column without CASCADE", `-- FILE: 1_a.sql
CREATE TABLE t (id serial);
-- FILE: 2_b.sql
DROP SEQUENCE t_id_seq;
`, want{"applied 2_b.sql": nil}},
		{"DROP TABLE without CASCADE while a column has its row type", `-- FILE: 1_a.sql
CREATE TABLE a (id int);
CREATE TABLE b (id int, r a);
-- FILE: 2_b.sql
DROP TABLE a;
`, want{"applied 2_b.sql": nil, "state a": "partial", "state b": "partial"}},
		{"a built-in type name is not a table's row type", `-- FILE: 1_a.sql
CREATE TABLE date (id int);
CREATE TABLE t (id int, d date);
-- FILE: 2_b.sql
DROP TABLE date;
`, want{"applied 2_b.sql": true, "state t": "complete", "state date": "absent"}},

		// Guard: Graphvein's own store migrations (probe gv) stay complete
		// and linked under the stricter key rule.
		{"gv Graphvein's own migrations", `-- FILE: 1_graph.sql
CREATE TABLE repositories (repository TEXT COLLATE "C" PRIMARY KEY, generation INTEGER NOT NULL DEFAULT 0);
CREATE TABLE index_runs (id TEXT COLLATE "C" PRIMARY KEY, repository TEXT COLLATE "C" NOT NULL REFERENCES repositories(repository), payload TEXT NOT NULL);
CREATE INDEX runs_repository ON index_runs(repository);
CREATE TABLE files (repository TEXT COLLATE "C" NOT NULL REFERENCES repositories(repository), path TEXT COLLATE "C" NOT NULL, content_hash TEXT NOT NULL, payload TEXT NOT NULL, PRIMARY KEY(repository,path));
CREATE TABLE nodes (repository TEXT COLLATE "C" NOT NULL REFERENCES repositories(repository), identity TEXT COLLATE "C" NOT NULL, file TEXT COLLATE "C", kind TEXT COLLATE "C" NOT NULL, level INTEGER NOT NULL, content_hash TEXT NOT NULL, payload TEXT NOT NULL, PRIMARY KEY(repository,identity), FOREIGN KEY(repository,file) REFERENCES files(repository,path) ON DELETE CASCADE);
CREATE INDEX nodes_file ON nodes(repository,file);
CREATE INDEX nodes_kind_level ON nodes(repository,kind,level);
CREATE TABLE edges (repository TEXT COLLATE "C" NOT NULL, kind TEXT COLLATE "C" NOT NULL, source TEXT COLLATE "C" NOT NULL, target TEXT COLLATE "C" NOT NULL, payload TEXT NOT NULL, PRIMARY KEY(repository,kind,source,target), FOREIGN KEY(repository,source) REFERENCES nodes(repository,identity) ON DELETE CASCADE, FOREIGN KEY(repository,target) REFERENCES nodes(repository,identity) ON DELETE CASCADE);
CREATE INDEX edges_outgoing ON edges(repository,source,kind);
CREATE INDEX edges_incoming ON edges(repository,target,kind);
CREATE TABLE diagnostics (repository TEXT COLLATE "C" NOT NULL REFERENCES repositories(repository), ordinal INTEGER NOT NULL, file TEXT COLLATE "C" NOT NULL, payload TEXT NOT NULL, PRIMARY KEY(repository,ordinal));
CREATE INDEX diagnostics_file ON diagnostics(repository,file);

-- FILE: 2_graph.sql
CREATE TABLE fact_cache (repository TEXT COLLATE "C" NOT NULL, path TEXT COLLATE "C" NOT NULL, content_hash TEXT NOT NULL, version TEXT NOT NULL, payload TEXT NOT NULL, PRIMARY KEY(repository,path));

-- FILE: 3_graph.sql
ALTER TABLE nodes ADD COLUMN name TEXT COLLATE "C";
ALTER TABLE nodes ADD COLUMN qualified_name TEXT COLLATE "C";
CREATE INDEX nodes_names ON nodes(repository,qualified_name,name,identity);
`, want{"state nodes": "complete", "state edges": "complete", "state files": "complete",
			"state fact_cache": "complete", "status ref edges.source": "linked", "status ref nodes.file": "linked",
			"status ref diagnostics.repository": "linked", "applied 3_graph.sql": true}},

		// Class 2: renames that missed.
		{"p03 ALTER TABLE renames an index", `-- FILE: 1_a.sql
CREATE TABLE t (id int, e text);
CREATE INDEX t_e_ix ON t (e);
CREATE VIEW v AS SELECT 1;
ALTER TABLE t_e_ix RENAME TO t_e_ix2;
ALTER TABLE v RENAME TO v2;
CREATE TABLE v (x int);
DROP INDEX t_e_ix2;
`, want{"index t.t_e_ix": false, "index t.t_e_ix2": false, "state t": "complete", "state v": "complete"}},
		{"p22 ALTER TABLE renames an index", `-- FILE: 1_a.sql
CREATE TABLE t (id int, a int);
CREATE INDEX t_a_idx2 ON t (a);
ALTER TABLE t_a_idx2 RENAME TO renamed_idx;
`, want{"index t.t_a_idx2": false, "index t.renamed_idx": true}},
		{"ALTER INDEX renames a constraint's index and the constraint", `-- FILE: 1_a.sql
CREATE TABLE t (id int, a int, CONSTRAINT t_a_uq UNIQUE (a));
ALTER INDEX t_a_uq RENAME TO t_a_unique;
`, want{"unique_keys table t": []any{map[string]any{"constraint": "t_a_unique", "columns": []any{"a"}}}}},
		{"ALTER TABLE on a generated index name", `-- FILE: 1_a.sql
CREATE TABLE t (id int PRIMARY KEY);
ALTER TABLE t_pkey RENAME TO t_pk;
`, want{"state t": "partial"}},
		{"ALTER VIEW on an index is refused", `-- FILE: 1_a.sql
CREATE TABLE t (id int);
CREATE INDEX t_ix ON t (id);
ALTER VIEW t_ix RENAME TO t_ix2;
`, want{"applied 1_a.sql": nil, "index t.t_ix": true}},
		{"p18 ALTER FUNCTION RENAME keeps calls opaque", `-- FILE: 1_a.sql
CREATE TABLE t (id int, a int);
CREATE FUNCTION f() RETURNS void LANGUAGE plpgsql AS $$ BEGIN ALTER TABLE t DROP COLUMN a; END $$;
ALTER FUNCTION f() RENAME TO g;
SELECT g();
`, want{"opaque function_call": 1, "opaque_after table t": 1}},

		// Class 3: certain refusals.
		{"p02 duplicate column in CREATE TABLE", `-- FILE: 1_a.sql
CREATE TABLE t (id int, ID text);
-- FILE: 2_b.sql
CREATE TABLE u (id int);
`, want{"applied 1_a.sql": nil, "applied 2_b.sql": nil, "state u": "partial"}},
		{"p11 DROP COLUMN of a column a complete table lacks", `-- FILE: 1_a.sql
CREATE TABLE t (id int, a int);
ALTER TABLE t DROP COLUMN zz;
-- FILE: 2_b.sql
CREATE TABLE later (id int);
`, want{"applied 1_a.sql": nil, "state later": "partial"}},
		{"p12 ALTER COLUMN of a column a complete table lacks", `-- FILE: 1_a.sql
CREATE TABLE t (id int, a int);
ALTER TABLE t ALTER COLUMN zz SET NOT NULL;
-- FILE: 2_b.sql
CREATE TABLE later (id int);
`, want{"applied 1_a.sql": nil, "state later": "partial"}},
		// Revision 8: not a certain refusal, but the column may not exist,
		// so the migration may fail (was applied: true before Revision 8).
		{"a missing column after an opaque statement may fail", `-- FILE: 1_a.sql
CREATE TABLE t (id int, a int);
DO $$ BEGIN ALTER TABLE t ADD COLUMN zz int; END $$;
ALTER TABLE t DROP COLUMN zz;
`, want{"applied 1_a.sql": nil, "state t": "partial"}},
		{"CREATE INDEX on a column a complete table lacks", `-- FILE: 1_a.sql
CREATE TABLE t (id int);
CREATE INDEX t_zz ON t (zz);
`, want{"applied 1_a.sql": nil}},
		{"CREATE INDEX INCLUDE of a column a complete table lacks", `-- FILE: 1_a.sql
CREATE TABLE t (id int);
CREATE INDEX t_ix ON t (id) INCLUDE (zz);
`, want{"applied 1_a.sql": nil, "index t.t_ix": false}},
		{"p13 REFERENCES a column set that is not a key", `-- FILE: 1_a.sql
CREATE TABLE t (id int PRIMARY KEY, a int);
CREATE TABLE u (id int, tid int REFERENCES t (a));
-- FILE: 2_b.sql
CREATE TABLE later (id int);
`, want{"status ref u.tid": "unresolved", "applied 1_a.sql": nil, "state later": "partial"}},
		{"REFERENCES a non-key column of a partial table stays unresolved", `-- FILE: 1_a.sql
CREATE TABLE t (id int PRIMARY KEY, a int);
ALTER TABLE t FROBNICATE;
-- FILE: 2_b.sql
CREATE TABLE u (id int, tid int REFERENCES t (a));
`, want{"status ref u.tid": "unresolved"}},
		{"REFERENCES a deferrable unique constraint", `-- FILE: 1_a.sql
CREATE TABLE t (id int, a int UNIQUE DEFERRABLE);
CREATE TABLE u (id int, ta int REFERENCES t (a));
`, want{"status ref u.ta": "unresolved", "applied 1_a.sql": nil}},
		{"REFERENCES keys of every accepted form", `-- FILE: 1_a.sql
CREATE TABLE t (id int, a int, b int, c int UNIQUE, PRIMARY KEY (a, b));
CREATE UNIQUE INDEX t_id ON t (id);
CREATE TABLE u (id int, ta int, tb int, tc int REFERENCES t (c), ti int REFERENCES t (id), FOREIGN KEY (tb, ta) REFERENCES t (b, a));
CREATE TABLE self (id int REFERENCES self (k), k int, UNIQUE (k));
CREATE TABLE late (id int, x int);
ALTER TABLE late ADD FOREIGN KEY (x) REFERENCES late (id), ADD UNIQUE (id);
`, want{"status ref u.tc": "linked", "status ref u.ti": "linked", "status ref u.tb": "linked", "status ref self.id": "linked",
			"status ref late.x": "linked", "applied 1_a.sql": true}},
		{"p19 DROP TABLE of an INHERITS parent without CASCADE", `-- FILE: 1_a.sql
CREATE TABLE parent (id int, a int);
CREATE TABLE child (b int) INHERITS (parent);
DROP TABLE parent;
-- FILE: 2_b.sql
CREATE TABLE later (id int);
`, want{"state parent": "partial", "applied 1_a.sql": nil, "state later": "partial"}},
		{"DROP TABLE of an INHERITS parent with CASCADE", `-- FILE: 1_a.sql
CREATE TABLE parent (id int, a int);
CREATE TABLE child (b int) INHERITS (parent);
DROP TABLE parent CASCADE;
CREATE TABLE child (x int);
`, want{"state parent": "absent", "state child": "complete", "applied 1_a.sql": true}},
		{"DROP TABLE of an ALTER TABLE INHERIT parent", `-- FILE: 1_a.sql
CREATE TABLE parent (id int);
CREATE TABLE kid (id int);
ALTER TABLE kid INHERIT parent;
DROP TABLE parent;
`, want{"applied 1_a.sql": nil}},
		{"p21 changes of an INHERITS parent apply to the parent", `-- FILE: 1_a.sql
CREATE TABLE parent (id int, a int);
CREATE TABLE child (b int) INHERITS (parent);
ALTER TABLE parent DROP COLUMN a;
ALTER TABLE parent RENAME COLUMN id TO pid;
`, want{"state parent": "complete", "column parent.a": false, "column parent.pid": true}},

		// B1: words after a complete column type.
		{"p08 unknown words after a column type", `-- FILE: 1_a.sql
CREATE TABLE t (id int, name text FROBNICATE);
-- FILE: 2_b.sql
CREATE TABLE later (id int);
`, want{"column t.name": false, "state t": "partial", "applied 1_a.sql": nil, "state later": "partial"}},
		{"ALTER COLUMN TYPE with unknown words", `-- FILE: 1_a.sql
CREATE TABLE t (id int);
ALTER TABLE t ALTER COLUMN id TYPE bigint FROBNICATE;
`, want{"applied 1_a.sql": nil, "type column t.id": "int"}},
		{"multi-word and qualified types", `-- FILE: 1_a.sql
CREATE TABLE t (a timestamp(3) with time zone, b interval day to second(2), c double precision NOT NULL,
  d character varying(10)[], e int array[3], f "char", g bit varying(5), h national character varying(3),
  i time without time zone, j interval, k pg_catalog.int4, l numeric(10, 2) DEFAULT 0, m float(24), n interval year to month);
`, want{"state t": "complete", "type column t.a": "timestamp(3) with time zone", "type column t.b": "interval day to second(2)",
			"type column t.d": "character varying(10)[]", "type column t.e": "int array[3]", "nullable column t.c": false}},

		// G4: a table whose body cannot be read.
		{"G4 a body that cannot be read leaves a partial table", `-- FILE: 1_a.sql
CREATE TABLE r (id int, valid tstzrange, PRIMARY KEY (id, valid WITHOUT OVERLAPS));
-- FILE: 2_b.sql
ALTER TABLE r ADD COLUMN z int;
`, want{"state r": "partial", "column r.id": false, "column r.z": true, "primary_key table r": nil, "applied 1_a.sql": nil}},

		// G8: lexing settings.
		{"p16 standard_conforming_strings off", `-- FILE: 1_a.sql
CREATE TABLE t (id int, a int);
SET standard_conforming_strings = off;
-- FILE: 2_b.sql
INSERT INTO t VALUES (1, 'x\'; DROP TABLE t; --');
CREATE TABLE later (id int);
`, want{"state t": "partial", "state later": "partial", "applied 1_a.sql": true, "applied 2_b.sql": nil}},
		{"standard_conforming_strings on is the assumed lexing", `-- FILE: 1_a.sql
SET standard_conforming_strings = on;
SET backslash_quote TO safe_encoding;
-- FILE: 2_b.sql
CREATE TABLE later (id int);
`, want{"state later": "complete", "applied 2_b.sql": true}},
		{"set_config of backslash_quote", `-- FILE: 1_a.sql
SELECT set_config('backslash_quote', 'off', false);
-- FILE: 2_b.sql
CREATE TABLE later (id int);
`, want{"state later": "partial", "applied 2_b.sql": nil}},
		{"N3 quoted set_config", `-- FILE: 1_a.sql
SELECT "set_config"('search_path', 'app', false);
CREATE TABLE t (id int);
`, want{"state t": "partial", "opaque search_path": 1}},

		// N2: absent facts are unknown on partial tables.
		{"N2 primary_key and nullable on partial and complete tables", `-- FILE: 1_a.sql
CREATE TABLE p (id int PRIMARY KEY, a int, b int NOT NULL);
CREATE TABLE q (id int);
ALTER TABLE p ENABLE ROW LEVEL SECURITY;
`, want{"primary_key table p": []any{"id"}, "primary_key table q": []any{}, "nullable column p.a": nil, "nullable column p.b": false,
			"primary_key column p.a": nil, "primary_key column p.id": true, "nullable column q.id": true, "primary_key column q.id": false}},
		// Revision 1.5 U2 (P12): the code's inventory names t, so t is
		// partial as well.
		{"p14 an opaque statement is counted on a table", `-- FILE: 1_a.sql
CREATE TABLE t (id int, a int);
DO $$ BEGIN EXECUTE 'ALTER TABLE t ADD PRIMARY KEY (a)'; END $$;
`, want{"state t": "partial", "opaque_after table t": 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := linkCase(t, tc.sql)
			for q, w := range tc.want {
				got := l.fact(t, q)
				if fmt.Sprint(got) != fmt.Sprint(w) || !reflect.DeepEqual(normalize(got), normalize(w)) {
					t.Errorf("%s: got %#v, want %#v", q, got, w)
				}
			}
		})
	}
}

// normalize turns typed slices from the emitter into []any so that
// expectations can be written as JSON-like values.
func normalize(v any) any {
	switch x := v.(type) {
	case []string:
		out := make([]any, len(x))
		for i, s := range x {
			out[i] = s
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = normalize(e)
		}
		return out
	case map[string]any:
		out := map[string]any{}
		for k, e := range x {
			out[k] = normalize(e)
		}
		return out
	}
	return v
}
