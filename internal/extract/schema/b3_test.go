package schema

import (
	"context"
	"fmt"
	"path"
	"reflect"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/egress"
)

// b3Cases are the C3, C4 and C5 coverage rows of T018a2 B3 (contract row
// M5g-1, the sqlx version requirement for -- no-transaction, and the
// closed list of partial reasons that create no unnamed object), each with
// valid and refused variants; same shape as b1dCases. Every "true" case is
// accepted by Postgres 17 and every other one is refused with the SQLSTATE
// given, or accepted ("ok") where the pass does not prove it.
var b3Cases = []b1dCase{
	// C3: DROP CONSTRAINT of a name the set wrote on the table.
	{"drop a written UNIQUE, keys kept", "CREATE TABLE q (a int PRIMARY KEY, b int CONSTRAINT q_b_uq UNIQUE);\nALTER TABLE q DROP CONSTRAINT q_b_uq;\nCREATE TABLE r (x int REFERENCES q (a));\n", "true", "", "ok"},
	{"drop a written UNIQUE, then its key is gone (refused)", "CREATE TABLE q (a int, b int CONSTRAINT q_b_uq UNIQUE);\nALTER TABLE q DROP CONSTRAINT q_b_uq;\nCREATE TABLE r (x int REFERENCES q (b));\n", "null", "", "42830"},
	{"drop a written primary key, then add another", "CREATE TABLE q (a int CONSTRAINT q_pk PRIMARY KEY, b int NOT NULL);\nALTER TABLE q DROP CONSTRAINT q_pk;\nALTER TABLE q ADD PRIMARY KEY (b);\n", "true", "", "ok"},
	{"drop a written name twice (refused)", "CREATE TABLE q (a int, CONSTRAINT q_a_ck CHECK (a > 0));\nALTER TABLE q DROP CONSTRAINT q_a_ck;\nALTER TABLE q DROP CONSTRAINT q_a_ck;\n", "null", "M5g-1.P1", "42704"},
	{"drop a written name twice with IF EXISTS", "CREATE TABLE q (a int, CONSTRAINT q_a_ck CHECK (a > 0));\nALTER TABLE q DROP CONSTRAINT q_a_ck;\nALTER TABLE q DROP CONSTRAINT IF EXISTS q_a_ck;\n", "true", "", "ok"},
	{"drop a written foreign key", "CREATE TABLE p (id int PRIMARY KEY);\nCREATE TABLE q (a int CONSTRAINT q_a_fk REFERENCES p);\nALTER TABLE q DROP CONSTRAINT q_a_fk;\nDROP TABLE p;\n", "true", "", "ok"},
	{"drop a key a foreign key uses (refused)", "CREATE TABLE p (id int CONSTRAINT p_pk PRIMARY KEY);\nCREATE TABLE q (a int REFERENCES p);\nALTER TABLE p DROP CONSTRAINT p_pk;\n", "null", "M5g-1.P8", "2BP01"},
	{"drop a key a foreign key uses, with CASCADE", "CREATE TABLE p (id int CONSTRAINT p_pk PRIMARY KEY);\nCREATE TABLE q (a int REFERENCES p);\nALTER TABLE p DROP CONSTRAINT p_pk CASCADE;\nDROP TABLE p;\n", "true", "", "ok"},
	{"drop a deferrable key beside the key a foreign key uses", "CREATE TABLE p (id int PRIMARY KEY, CONSTRAINT p_d UNIQUE (id) DEFERRABLE);\nCREATE TABLE q (a int REFERENCES p);\nALTER TABLE p DROP CONSTRAINT p_d;\n", "true", "", "ok"},
	{"drop a name added later in the same statement (refused)", "CREATE TABLE q (a int);\nALTER TABLE q ADD CONSTRAINT q_u UNIQUE (a), DROP CONSTRAINT q_u;\n", "null", "", "42704"},
	{"drop and re-add a name in one statement", "CREATE TABLE q (a int, CONSTRAINT q_u UNIQUE (a));\nALTER TABLE q DROP CONSTRAINT q_u, ADD CONSTRAINT q_u UNIQUE (a);\n", "true", "", "ok"},
	{"drop a primary key a view depends on (refused)", "CREATE TABLE q (a int CONSTRAINT q_pk PRIMARY KEY, b int);\nCREATE VIEW v AS SELECT a, b FROM q GROUP BY a;\nALTER TABLE q DROP CONSTRAINT q_pk;\n", "null", "", "2BP01"},
	{"drop the merged-away name (refused)", "CREATE TABLE q (a int, CONSTRAINT q_u1 UNIQUE (a), CONSTRAINT q_u2 UNIQUE (a));\nALTER TABLE q DROP CONSTRAINT q_u2;\n", "null", "M5g-1.P1", "42704"},
	{"drop the renamed key's new name", "CREATE TABLE q (a int CONSTRAINT q_pk PRIMARY KEY);\nALTER INDEX q_pk RENAME TO q_key;\nALTER TABLE q DROP CONSTRAINT q_key;\n", "true", "", "ok"},
	{"drop the renamed key's old name (refused)", "CREATE TABLE q (a int CONSTRAINT q_pk PRIMARY KEY);\nALTER INDEX q_pk RENAME TO q_key;\nALTER TABLE q DROP CONSTRAINT q_pk;\n", "null", "M5g-1.P1", "42704"},
	{"drop an index name as a constraint (refused)", "CREATE TABLE q (a int);\nCREATE UNIQUE INDEX q_idx_a ON q (a);\nALTER TABLE q DROP CONSTRAINT q_idx_a;\n", "null", "M5g-1.P1", "42704"},
	{"drop after an opaque statement (Revision 7)", "CREATE TABLE q (a int, CONSTRAINT q_a_ck CHECK (a > 0));\nDO $$ BEGIN NULL; END $$;\nALTER TABLE q DROP CONSTRAINT q_a_ck;\n", "null", "", "ok"},
	{"drop the UNIQUE merged into the primary key (refused)", "CREATE TABLE q (a int PRIMARY KEY UNIQUE);\nALTER TABLE q DROP CONSTRAINT q_a_key;\n", "null", "M5g-1.P1", "42704"},
	{"drop a key renamed under another qualification (Revision 7)", "CREATE TABLE q (a int, CONSTRAINT u UNIQUE (a));\nALTER INDEX public.u RENAME TO v;\nALTER TABLE q DROP CONSTRAINT u;\n", "null", "", "42704"},
	{"drop a key index under another qualification (refused)", "CREATE TABLE q (a int, CONSTRAINT u UNIQUE (a));\nDROP INDEX IF EXISTS public.u;\n", "null", "", "2BP01"},
	// C5: a generated-name shape on a partial table.
	{"generated shape after REPLICA IDENTITY FULL", "CREATE TABLE q (a int);\nALTER TABLE q REPLICA IDENTITY FULL;\nCREATE INDEX q_a_idx ON q (a);\n", "true", "", "ok"},
	{"generated shape after OWNER TO, unnamed index held (refused)", "CREATE TABLE q (a int);\nCREATE INDEX ON q (a);\nALTER TABLE q OWNER TO CURRENT_USER;\nCREATE INDEX q_a_idx ON q (a);\n", "null", "", "42P07"},
	{"generated shape after SET (fillfactor)", "CREATE TABLE q (a int PRIMARY KEY);\nALTER TABLE q SET (fillfactor = 70);\nCREATE TABLE q_b_key (x int);\n", "true", "", "ok"},
	{"generated shape after ADD IDENTITY (refused)", "CREATE TABLE q (a int NOT NULL);\nALTER TABLE q REPLICA IDENTITY FULL, ALTER COLUMN a ADD GENERATED ALWAYS AS IDENTITY;\nCREATE SEQUENCE q_a_seq;\n", "null", "", "42P07"},
	// Roots of the 100,000-seed run, fixed by their classes.
	{"rename an index to a written key's name (refused, P2, g1-s34388)", "CREATE TABLE q (id text, CONSTRAINT q_pk PRIMARY KEY (id));\nCREATE INDEX q_i ON q (id);\nALTER INDEX q_i RENAME TO q_pk;\n", "null", "", "42P07"},
	{"ALTER TABLE ONLY ADD COLUMN with children (refused, P8, g1-s27299)", "CREATE TABLE q (a int);\nCREATE TABLE c (b int) INHERITS (q);\nALTER TABLE ONLY q ADD COLUMN x int;\n", "null", "M5a.P8", "42P16"},
	{"ALTER TABLE ONLY ADD COLUMN without children", "CREATE TABLE q (a int);\nALTER TABLE ONLY q ADD COLUMN x int;\n", "true", "", "ok"},
	// Rows the fixtures and oracle cases do not reach (B7, TestContractRows).
	{"SET SCHEMA (M5j)", "CREATE TABLE public.q (a int);\nCREATE SCHEMA b3s;\nALTER TABLE public.q SET SCHEMA b3s;\n", "true", "", "ok"},
	{"a session setting (N2)", "SET LOCAL statement_timeout = 0;\nCREATE TABLE q (a int);\n", "true", "", "ok"},
	{"a lexing setting (O4)", "CREATE TABLE q (a int);\nSET standard_conforming_strings = off;\n", "true", "", "ok"},
	{"generated shape after an opaque statement (accepted, not proven)", "CREATE TABLE q (a int);\nALTER TABLE q REPLICA IDENTITY FULL;\nDO $$ BEGIN NULL; END $$;\nCREATE INDEX q_a_idx ON q (a);\n", "null", "", "ok"},
}

// TestB3RequirementReading pins the Cargo requirement reader of C4: whether
// every version a requirement allows reads -- no-transaction (0.8.x),
// none does (before 0.8.0), or that is unknown.
func TestB3RequirementReading(t *testing.T) {
	for req, want := range map[string]markerReading{
		"0.8": markerRead, "0.8.6": markerRead, "^0.8.1": markerRead, "~0.8": markerRead, "=0.8.0": markerRead,
		"0.8.*": markerRead, ">=0.8, <0.9": markerRead, ">= 0.8.2, < 0.8.5": markerRead,
		"0.7": markerIgnored, "0.7.4": markerIgnored, "=0.6.3": markerIgnored, "0.5.9": markerIgnored, "<0.8": markerIgnored, "~0.7.2": markerIgnored,
		">=0.7": markerUnknown, ">=0.8": markerUnknown, "*": markerUnknown, "0": markerUnknown, "0.9": markerUnknown, "1": markerUnknown,
		"0.8.0-alpha.1": markerUnknown, "": markerUnknown, "0.8.x.1": markerUnknown, ">0.7.9": markerUnknown, "<=0.8.0": markerUnknown,
		">0.8, <0.8": markerUnknown,
	} {
		if got := requirementReading(req); got != want {
			t.Errorf("%q: got %d, want %d", req, got, want)
		}
	}
}

// TestB3NoTransaction checks C4 end to end: a one-statement migration that
// starts with the marker and runs CREATE INDEX CONCURRENTLY is applied when
// the set's sqlx reads the marker, and may fail when it may not (inside
// sqlx's transaction Postgres refuses it, 25001).
func TestB3NoTransaction(t *testing.T) {
	first := "CREATE TABLE q (a int, p int);\nCREATE TABLE r (a int) PARTITION BY RANGE (a);\nCREATE INDEX q_i ON q (a);\nCREATE INDEX q_j ON q (p);\n"
	for _, tc := range []struct {
		name, dep, second, want string
	}{
		{"0.8 reads the marker", `{ version = "0.8", features = ["postgres"] }`, "-- no-transaction\nCREATE INDEX CONCURRENTLY q_a ON q (a);\n", "true"},
		{"0.8.6 in a table", `{ version = "0.8.6", features = ["postgres"] }`, "-- no-transaction\nDROP INDEX CONCURRENTLY IF EXISTS q_none;\n", "null"},
		{"a held index", `{ version = "0.8.6", features = ["postgres"] }`, "-- no-transaction\nDROP INDEX CONCURRENTLY q_i;\n", "true"},
		{"two indexes are refused", `{ version = "0.8.6", features = ["postgres"] }`, "-- no-transaction\nDROP INDEX CONCURRENTLY q_i, q_j;\n", "null"},
		{"a table outside the set may be partitioned", `{ version = "0.8", features = ["postgres"] }`, "-- no-transaction\nCREATE INDEX CONCURRENTLY ext_a ON ext (id);\n", "null"},
		{"0.7 does not", `{ version = "0.7", features = ["postgres"] }`, "-- no-transaction\nCREATE INDEX CONCURRENTLY q_a ON q (a);\n", "null"},
		{"an open range may not", `{ version = ">=0.7", features = ["postgres"] }`, "-- no-transaction\nCREATE INDEX CONCURRENTLY q_a ON q (a);\n", "null"},
		{"a git dependency may not", `{ git = "https://example.invalid/sqlx", features = ["postgres"] }`, "-- no-transaction\nCREATE INDEX CONCURRENTLY q_a ON q (a);\n", "null"},
		{"two statements run in the implicit block", `{ version = "0.8", features = ["postgres"] }`, "-- no-transaction\nCREATE INDEX CONCURRENTLY q_a ON q (a);\nCREATE INDEX q_p ON q (p);\n", "null"},
		{"a partitioned table is refused", `{ version = "0.8", features = ["postgres"] }`, "-- no-transaction\nCREATE INDEX CONCURRENTLY r_a ON r (a);\n", "null"},
		{"a marker after a blank line is not read", `{ version = "0.8", features = ["postgres"] }`, "\n-- no-transaction\nCREATE INDEX CONCURRENTLY q_a ON q (a);\n", "null"},
		{"0.7 runs a marked migration in the transaction", `{ version = "0.7", features = ["postgres"] }`, "-- no-transaction\nCREATE INDEX q_a ON q (a);\n", "true"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manifest := "[package]\nname = \"a\"\n[dependencies]\nsqlx = " + tc.dep + "\n"
			sources := map[string][]byte{"Cargo.toml": []byte(manifest), "migrations/1_a.sql": []byte(first), "migrations/2_b.sql": []byte(tc.second)}
			nodes, _, diagnostics := Link(Input{Sources: sources})
			got := map[string]string{}
			for _, n := range nodes {
				if n.Kind == "migration" {
					got[path.Base(n.File)] = fmt.Sprint(n.Attributes["applied"])
				}
			}
			want := map[string]string{"1_a.sql": "true", "2_b.sql": tc.want}
			if want["2_b.sql"] == "null" {
				want["2_b.sql"] = "<nil>"
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("applied %v, want %v; %v", got, want, diagnostics)
			}
		})
	}
}

func TestB3Cases(t *testing.T) {
	for _, tc := range b3Cases {
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

func TestB3CasesPostgres(t *testing.T) {
	db, err := egress.OpenPostgres(oracleURL(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	for _, tc := range b3Cases {
		t.Run(tc.name, func(t *testing.T) {
			if strings.Contains(tc.name, "under another qualification") {
				// runRolledBack does not run in schema public; these two
				// were checked with psql under search_path public.
				t.Skip("needs search_path public")
			}
			if got := runRolledBack(ctx, t, db, "CREATE TABLE ext (id int);\n"+tc.sql); got != tc.pg {
				t.Fatalf("Postgres: %s, the case says %s", got, tc.pg)
			}
		})
	}
}

// TestB3OutsideReferences pins g1-s48011 (P8, M6.P8): DROP TABLE of a
// table outside the set that a set foreign key references drops the
// foreign key with CASCADE and may be refused without it (2BP01).
func TestB3OutsideReferences(t *testing.T) {
	applied, diagnostics := linkSQL(t, "CREATE TABLE q (a int REFERENCES outside_t);\nDROP TABLE outside_t CASCADE;\n")
	if applied["1_m.sql"] != "true" || !cites(diagnostics, "M6.P8") {
		t.Fatalf("CASCADE: applied %v; %v", applied, diagnostics)
	}
	applied, diagnostics = linkSQL(t, "CREATE TABLE q (a int REFERENCES outside_t);\nDROP TABLE outside_t;\n")
	if applied["1_m.sql"] != "null" || !cites(diagnostics, "M6.P8") {
		t.Fatalf("without CASCADE: applied %v; %v", applied, diagnostics)
	}
}
