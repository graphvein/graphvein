package schema

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
)

// TestContractIDs checks the contract skeleton (T018a2 B7 as amended by
// Revision 1): row and precondition ids are unique, every precondition
// belongs to a row, and every id the code cites (a string literal of the form <row>.<class>,
// passed to precondition directly or through a helper) is listed.
func TestContractIDs(t *testing.T) {
	rows := map[string]bool{}
	for _, r := range contractRows {
		if rows[r.id] {
			t.Errorf("row %s is listed twice", r.id)
		}
		rows[r.id] = true
	}
	pres := map[string]bool{}
	for _, p := range contractPreconditions {
		if pres[p.id] {
			t.Errorf("precondition %s is listed twice", p.id)
		}
		pres[p.id] = true
		row, class, ok := strings.Cut(p.id, ".")
		if !ok || !rows[row] {
			t.Errorf("precondition %s names no contract row", p.id)
		}
		if !regexp.MustCompile(`^(P([1-9]|1[0-4])|K2)$`).MatchString(class) {
			t.Errorf("precondition %s names no Revision 1 class", p.id)
		}
	}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	cited := regexp.MustCompile(`"((?:M|R|N|O|F|T|Q|D)[0-9a-z-]*\.(?:P[0-9]+|K2))"`)
	used := map[string]bool{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || f == "contract.go" {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range cited.FindAllStringSubmatch(string(data), -1) {
			used[m[1]] = true
			if !pres[m[1]] {
				t.Errorf("%s cites precondition %s, which contract.go does not list", f, m[1])
			}
		}
	}
	for id := range pres {
		if !used[id] {
			t.Errorf("precondition %s is listed but no diagnostic cites it", id)
		}
	}
}

// TestContractRows is the B7 1:1 check (T018a2 B7 as amended by Revision
// 1): every row id an analysis or the state records is a row of
// contractRows; every row is exercised by the fixtures or the regression
// cases and by the generator grammar (g1-g7); and forms no row covers go
// to the single default branch D.
func TestContractRows(t *testing.T) {
	ids := map[string]bool{}
	for _, r := range contractRows {
		ids[r.id] = true
	}
	var mu sync.Mutex
	seen := map[string]int{}
	saved := runSet
	runSet = func(set *migrationSet, e *emitter) {
		saved(set, e)
		mu.Lock()
		defer mu.Unlock()
		for r, n := range set.rows {
			seen[r] += n
		}
	}
	defer func() { runSet = saved }()
	run := func(h *oracleHistory) {
		if _, err := extract(h, t.TempDir()); err != nil {
			t.Fatal(err)
		}
	}
	for _, h := range fixtureHistories(t) {
		run(h)
	}
	for _, c := range readCases(t) {
		run(c.History)
	}
	// The SQL cases of the unit tests that check their verdicts against
	// Postgres are regression cases too.
	for _, cases := range [][]b1dCase{b1dCases, b2Cases, b3Cases} {
		for _, tc := range cases {
			linkSQL(t, tc.sql)
		}
	}
	recorded := seen
	seen = map[string]int{}
	missing := func() bool {
		for _, r := range contractRows {
			if seen[r.id] == 0 && notGenerated[r.id] == "" {
				return true
			}
		}
		return false
	}
	// Seeds 1-300 of every generator, then more until every row is seen
	// (rare productions, such as ALTER SCHEMA RENAME, need more seeds).
	for seed := uint64(1); seed <= 3000 && (seed <= 300 || missing()); seed++ {
		for _, v := range []string{"g1", "g2", "g3", "g4", "g5", "g6", "g7", "g8", "g9"} {
			run(generateWith(seed, v).history())
		}
	}
	generated := seen
	for _, m := range []map[string]int{recorded, generated} {
		for r := range m {
			if !ids[r] {
				t.Errorf("row %s is recorded but contract.go does not list it", r)
			}
		}
	}
	for _, r := range contractRows {
		if recorded[r.id] == 0 {
			t.Errorf("row %s is exercised by no fixture or regression case", r.id)
		}
		if generated[r.id] == 0 && notGenerated[r.id] == "" {
			t.Errorf("row %s is exercised by no generated history (g1-g8 seeds 1-3000)", r.id)
		}
		if generated[r.id] > 0 && notGenerated[r.id] != "" {
			t.Errorf("row %s is generated now; remove it from notGenerated", r.id)
		}
	}
	t.Logf("rows recorded %v; generated %v", recorded, generated)
	// The default branch: forms no row covers.
	for _, sql := range defaultForms {
		stmts, _ := lexStatements([]byte(sql + ";"))
		if len(stmts) != 1 {
			t.Fatalf("%q: %d statements", sql, len(stmts))
		}
		a := analyze(stmts[0], []byte(sql+";"), nil, false)
		rows := statementRows(stmts[0].tokens, a)
		if !slices.Contains(rows, "D") {
			t.Errorf("%q: rows %v, want the default branch D", sql, rows)
		}
	}
	for _, sql := range acceptedForms {
		stmts, _ := lexStatements([]byte(sql + ";"))
		a := analyze(stmts[0], []byte(sql+";"), nil, false)
		if rows := statementRows(stmts[0].tokens, a); slices.Contains(rows, "D") {
			t.Errorf("%q: rows %v, want no default branch (Postgres accepts it)", sql, rows)
		}
	}
	// V7 (Revision 1.6, P7): every row's statement keeps its row, and
	// with one extra word it reaches D (or, for the rows listed in
	// extraWordMayFail, the migration may fail).
	for _, r := range contractRows {
		sample, ok := rowSamples[r.id]
		if !ok {
			if rowWithoutSample[r.id] == "" {
				t.Errorf("row %s has no sample statement for the extra-word check", r.id)
			}
			continue
		}
		stmts, _ := lexStatements([]byte(sample + ";"))
		a := analyze(stmts[0], []byte(sample+";"), nil, false)
		if rows := statementRows(stmts[0].tokens, a); !slices.Contains(rows, r.id) {
			t.Errorf("%s sample %q: rows %v, want %s", r.id, sample, rows, r.id)
		}
		extra := sample + " garbage"
		stmts, _ = lexStatements([]byte(extra + ";"))
		a = analyze(stmts[0], []byte(extra+";"), nil, false)
		rows := statementRows(stmts[0].tokens, a)
		if extraWordMayFail[r.id] != "" {
			if applied, _ := linkSQL(t, extra+";"); applied["1_m.sql"] != "null" {
				t.Errorf("%s: %q is applied %s, want null (%s)", r.id, extra, applied["1_m.sql"], extraWordMayFail[r.id])
			}
			continue
		}
		if !slices.Contains(rows, "D") {
			t.Errorf("%s: %q: rows %v, want the default branch D (V7)", r.id, extra, rows)
		}
	}
	// U1: an ALTER TABLE subcommand the parser does not model reaches
	// M5k-a or M5k-b only through an m5kWord word.
	for _, w := range []alterAction{{kind: "unsupported"}, {kind: "unsupported", reason: "constraint form not modeled"}} {
		if r := actionRow(w); r != "D" {
			t.Errorf("an unsupported subcommand without an M5k word maps to %s, want D", r)
		}
	}
}

// defaultForms are statement forms no contract row covers (Revision 1.4
// R6 widened the list): unmodelled statements and ALTER TABLE actions,
// text gram.y REL_17_6 rejects, and text the readers cannot read. Each
// must reach D.
var defaultForms = []string{
	"FROBNICATE t",
	"CREATE WIDGET w",
	"ALTER TABLE t FROBNICATE",
	"ALTER WIDGET w RENAME TO v",
	"CREATE TABLE t (a int",
	"PREPARE p AS SELECT 1",
	"CREATE DATABASE d",
	"ALTER SYSTEM SET work_mem = '4MB'",
	"CREATE TABLESPACE ts LOCATION '/x'",
	"CREATE TABLE t (a int) FROBNICATE",
	"CREATE TABLE t (a int FROBNICATE)",
	"CREATE TABLE t (a int(11))",
	"CREATE TABLE t (a int, CONSTRAINT)",
	"CREATE TABLE t (a int REFERENCES)",
	"CREATE TABLE t (a int, CHECK)",
	"CREATE TABLE t (a int, b int, FOREIGN KEY (a, b) REFERENCES p (x))",
	"CREATE TABLE t (a int GENERATED ALWAYS AS (a))",
	"CREATE TABLE t (a int GENERATED SOMETIMES AS IDENTITY)",
	"CREATE TABLE t (a int DEFAULT)",
	"CREATE TABLE t (a int NULL NOT NULL)",
	"CREATE TABLE t (a serial DEFAULT 1)",
	"CREATE INDEX ON",
	"CREATE INDEX IF NOT EXISTS ON t (a)",
	"ALTER TABLE t ADD COLUMN",
	"ALTER TABLE t RENAME TO u, ADD COLUMN b int",
	"IMPORT FOREIGN SCHEMA s FROM SERVER x INTO y",
	"CREATE SERVER s FOREIGN DATA WRAPPER w",
	"LOAD 'x'",
	"CREATE ACCESS METHOD m TYPE TABLE HANDLER h",
	"CREATE CONVERSION c FOR 'LATIN1' TO 'UTF8' FROM f",
	"CREATE TEXT SEARCH CONFIGURATION c (PARSER = p)",
	"CREATE LANGUAGE l",
	"SELECT INTO t DISTINCT a FROM s",
	"SELECT * FROM x AS into",
	"INSERT INTO t SELECT 1 INTO u",
	"CREATE VIEW v AS SELECT 1 INTO u",
	"DROP WIDGET w",
	"ALTER TABLE t ATTACH PARTITION p FOR VALUES IN (1), ADD COLUMN c int",
	// Revision 1.5 U1: USING INDEX outside row M5f-u.
	"CREATE TABLE t (a int, PRIMARY KEY USING INDEX i)",
	"ALTER TABLE t ADD CONSTRAINT k PRIMARY KEY (a) USING INDEX i",
	"ALTER TABLE t ADD CONSTRAINT k EXCLUDE USING INDEX i",
	"ALTER TABLE t ADD CONSTRAINT k UNIQUE USING INDEX i, ADD COLUMN c int",
	// U8: keywords a name position does not accept.
	"CREATE TABLE t (id int, order int)",
	"CREATE TABLE natural (a int)",
	"CREATE INDEX inner ON t (id)",
	"ALTER TABLE t RENAME COLUMN x TO similar",
	"ALTER TABLE t ADD COLUMN user text",
	"CREATE TYPE verbose AS ENUM ('a')",
	"CREATE FUNCTION coalesce() RETURNS int LANGUAGE sql AS 'SELECT 1'",
	"CREATE TABLE t (a between)",
	// U9: a storage parameter without its value.
	"ALTER TABLE t SET (fillfactor = )",
	// U3: search_path values gram.y rejects.
	"SET search_path TO",
	"SET search_path TO DEFAULT, public",
	// U5: sequence options outside SeqOptElem's use in CREATE, and OWNED BY
	// of one name other than NONE.
	"CREATE SEQUENCE s SEQUENCE NAME x",
	"CREATE SEQUENCE s LOGGED",
	"ALTER SEQUENCE s OWNED BY t",
	// Revision 1.6 V7: one extra word after a statement whose reader read
	// only names (each refused by Postgres with 42601).
	"CREATE ROLE r WITH BYPASSRLS garbage",
	"GRANT ALL ON ALL TABLES IN SCHEMA public TO r garbage",
	"GRANT ra TO rb garbage",
	"ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON TABLES garbage TO r",
	"REVOKE ALL ON t FROM PUBLIC garbage",
	"ALTER ROLE CURRENT_USER SET work_mem = 1000 garbage",
	"COMMENT ON TABLE t IS 'x' garbage",
	"CREATE TRIGGER tr BEFORE INSERT ON t FOR EACH ROW EXECUTE FUNCTION tf() garbage",
	"CREATE POLICY p ON t USING (true) garbage",
	"CREATE COLLATION c1 (provider = icu, locale = 'und') garbage",
	"CREATE TYPE e AS ENUM ('a') garbage",
	"ALTER TYPE e ADD VALUE 'b' garbage",
	"ALTER TYPE e RENAME VALUE 'a' TO 'b' garbage",
	"DROP TYPE e garbage",
	"DROP VIEW v garbage",
	"DROP FUNCTION tf() garbage",
	"ALTER FUNCTION tf() OWNER TO CURRENT_USER garbage",
	"CREATE SCHEMA s garbage",
	"CREATE EXTENSION IF NOT EXISTS pgcrypto garbage",
	"DROP EXTENSION IF EXISTS pgcrypto garbage",
	"LOCK TABLE t IN SHARE MODE garbage",
	"NOTIFY ch garbage",
	"RESET work_mem garbage",
	"BEGIN garbage",
	"COMMIT garbage",
	// Revision 1.6 V3: reserved and type_func_name keywords in element,
	// partition key, operator class, EXCLUDE and expression positions.
	"CREATE INDEX i ON t (order)",
	"CREATE INDEX i ON t (left)",
	"CREATE INDEX i ON t (verbose DESC)",
	"CREATE INDEX i ON t ((order + 1))",
	"CREATE INDEX i ON t (b) WHERE order > 0",
	"CREATE INDEX i ON t (a order)",
	"CREATE TABLE t (a int) PARTITION BY RANGE (left)",
	"CREATE TABLE t (a int CHECK (order > 0))",
	"CREATE TABLE t (a int, EXCLUDE (order WITH =))",
}

// acceptedForms are forms Postgres accepts that sit next to defaultForms
// (Revision 1.5): they must not reach D.
var acceptedForms = []string{
	"CREATE TABLE public.order (a int)",
	"CREATE TYPE values AS ENUM ('a')",
	"ALTER TABLE t ADD CONSTRAINT k PRIMARY KEY USING INDEX i",
	"ALTER TABLE t ADD CONSTRAINT k UNIQUE USING INDEX i",
	// Revision 1.6 V7: the same statements without the extra word.
	"CREATE ROLE r WITH BYPASSRLS",
	"GRANT ALL ON ALL TABLES IN SCHEMA public TO r",
	"GRANT ra TO rb",
	"ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON TABLES TO r",
	"REVOKE ALL ON t FROM PUBLIC",
	"ALTER ROLE CURRENT_USER SET work_mem = 1000",
	"COMMENT ON TABLE t IS 'x'",
	"CREATE TRIGGER tr BEFORE INSERT ON t FOR EACH ROW EXECUTE FUNCTION tf()",
	"CREATE POLICY p ON t USING (true)",
	"CREATE COLLATION c1 (provider = icu, locale = 'und')",
	"CREATE TYPE e AS ENUM ('a')",
	"ALTER TYPE e ADD VALUE 'b'",
	"ALTER TYPE e RENAME VALUE 'a' TO 'b'",
	"DROP TYPE e",
	"DROP VIEW v",
	"DROP FUNCTION tf()",
	"ALTER FUNCTION tf() OWNER TO CURRENT_USER",
	"CREATE SCHEMA s",
	"CREATE EXTENSION IF NOT EXISTS pgcrypto",
	"DROP EXTENSION IF EXISTS pgcrypto",
	"LOCK TABLE t IN SHARE MODE",
	"NOTIFY ch",
	"RESET work_mem",
	"BEGIN",
	"COMMIT",
	// Revision 1.6 V3: col_name keywords and quoted names are ColIds.
	"CREATE INDEX i ON t (int)",
	"CREATE INDEX i ON t (\"order\")",
}

// rowSamples are a statement of each row that Postgres accepts and that
// one extra word makes a syntax error (each checked live, Revision 1.6
// V7); TestContractRows checks the extra-word form reaches D.
var rowSamples = map[string]string{
	"M1": "CREATE TABLE t (a int)", "M1b": "CREATE TABLE t2 (LIKE t)", "M4": "CREATE INDEX i ON t (a)",
	"M5a": "ALTER TABLE t ADD COLUMN b int", "M5b": "ALTER TABLE t DROP COLUMN b",
	"M5c": "ALTER TABLE t ALTER COLUMN a TYPE bigint", "M5d": "ALTER TABLE t ALTER COLUMN a SET NOT NULL",
	"M5e": "ALTER TABLE t ALTER COLUMN a DROP DEFAULT", "M5f": "ALTER TABLE t ADD PRIMARY KEY (a)",
	"M5f-u": "ALTER TABLE t ADD CONSTRAINT k UNIQUE USING INDEX i", "M5g": "ALTER TABLE t DROP CONSTRAINT k",
	"M5h": "ALTER TABLE t RENAME COLUMN a TO b", "M5i": "ALTER TABLE t RENAME TO u", "M5j": "ALTER TABLE t SET SCHEMA s",
	"M5k-a": "ALTER TABLE t SET LOGGED", "M5k-b": "ALTER TABLE t ENABLE TRIGGER tr", "M6": "DROP TABLE t",
	"M7": "DROP INDEX i", "M8": "ALTER INDEX i RENAME TO j", "M9": "ALTER SCHEMA s RENAME TO s2",
	"R1": "CREATE SEQUENCE s", "R2": "CREATE TYPE e AS ENUM ('a')", "R3": "ALTER TYPE e ADD VALUE 'b'",
	"R4": "ALTER VIEW v RENAME TO w", "R5": "DROP VIEW v", "R6": "ALTER FUNCTION f() OWNER TO CURRENT_USER",
	"N1": "COMMENT ON TABLE t IS 'x'", "N2": "RESET work_mem", "N3": "CLUSTER t USING i", "O1": "CALL p()",
	"O2": "CREATE EXTENSION IF NOT EXISTS pgcrypto", "O3": "DISCARD ALL", "O4": "SET standard_conforming_strings = off",
	"O5": "DROP OWNED BY CURRENT_USER", "T1": "BEGIN",
}

// rowWithoutSample are the rows that are no statement form of their own,
// with the reason.
var rowWithoutSample = map[string]string{
	"M2": "a part of M1 and M5a (the column type)", "M3": "a part of M1 and M5f (REFERENCES)",
	"M5g-1": "decided by the state from an M5g statement", "R1b": "a part of R1 and R6 (the query or body)",
	"O1t": "decided by the state from an N3 or O1 statement", "F1": "claims nothing: the migration may fail",
	"Q1": "the set reading, not a statement", "D": "the default branch itself",
}

// extraWordMayFail are the rows whose reader makes text it cannot read
// may fail instead of D, with the precondition (checked applied null).
var extraWordMayFail = map[string]string{
	"O4": "SET is read by U3/U7's reader: a value list it cannot read is not proven (N2.P5)",
}

// notGenerated lists the rows the generators do not produce, with the
// reason; the fixtures and regression cases still exercise them.
var notGenerated = map[string]string{}

// TestContractDocument checks B7 (c): the "Claim contract" section of
// docs/extract/schema.md lists exactly the rows of contractRows (id, kind,
// form and claim, in order) and the preconditions of contractPreconditions
// (id and text, in order).
func TestContractDocument(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "extract", "schema.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, section, ok := strings.Cut(string(data), "\n## Claim contract\n")
	if !ok {
		t.Fatal(`docs/extract/schema.md has no "## Claim contract" section`)
	}
	section, _, _ = strings.Cut(section, "\n## ")
	var rows, pres [][]string
	for _, line := range strings.Split(section, "\n") {
		if !strings.HasPrefix(line, "| `") {
			continue
		}
		cells := splitCells(line)
		id := strings.Trim(cells[0], "`")
		if strings.Contains(id, ".") {
			pres = append(pres, append([]string{id}, cells[1:]...))
		} else {
			rows = append(rows, append([]string{id}, cells[1:]...))
		}
	}
	if len(rows) != len(contractRows) {
		t.Errorf("the document lists %d rows, contract.go %d", len(rows), len(contractRows))
	}
	// Revision 1.9: the last column of both tables is the environment
	// assumptions of contractAssumptions ("-" for none).
	assumes := func(id string) string {
		if a := contractAssumptions[id]; len(a) > 0 {
			return strings.Join(a, ", ")
		}
		return "-"
	}
	for _, r := range rows {
		if got, want := r[len(r)-1], assumes(r[0]); len(r) != 6 || got != want {
			t.Errorf("row %s: the document's assumptions %q, contract.go %q", r[0], got, want)
		}
	}
	for _, p := range pres {
		if got, want := p[len(p)-1], assumes(p[0]); len(p) != 3 || got != want {
			t.Errorf("precondition %s: the document's assumptions %q, contract.go %q", p[0], got, want)
		}
	}
	for i := 0; i < len(rows) && i < len(contractRows); i++ {
		r := contractRows[i]
		if got := rows[i]; len(got) < 4 || got[0] != r.id || got[1] != r.kind || got[2] != r.form || got[3] != r.claim {
			t.Errorf("row %d: document %q, contract.go %q", i, got, []string{r.id, r.kind, r.form, r.claim})
		}
	}
	if len(pres) != len(contractPreconditions) {
		t.Errorf("the document lists %d preconditions, contract.go %d", len(pres), len(contractPreconditions))
	}
	for i := 0; i < len(pres) && i < len(contractPreconditions); i++ {
		p := contractPreconditions[i]
		if got := pres[i]; len(got) < 2 || got[0] != p.id || got[1] != p.text {
			t.Errorf("precondition %d: document %q, contract.go %q", i, got, []string{p.id, p.text})
		}
	}
}

// splitCells splits a Markdown table row at unescaped pipes and unescapes
// the cells.
func splitCells(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimSuffix(strings.TrimPrefix(line, "|"), "|")
	var cells []string
	var b strings.Builder
	for i := 0; i < len(line); i++ {
		switch {
		case line[i] == '\\' && i+1 < len(line) && line[i+1] == '|':
			b.WriteByte('|')
			i++
		case line[i] == '|':
			cells = append(cells, strings.TrimSpace(b.String()))
			b.Reset()
		default:
			b.WriteByte(line[i])
		}
	}
	return append(cells, strings.TrimSpace(b.String()))
}

// TestContractAssumptions checks the environment-assumption metadata of
// T018a2 Revision 1.9 (contractAssumptions): every key is a row or a
// precondition of the contract, every assumption one of E1-E6 or E8 (E7
// belongs to Revision 2.0, after v1), listed once and in order. Revision
// 2.1: E8 is on every row whose claim is a fact or applied: true (every
// row but D and F1) and on no precondition (a precondition's verdict is a
// refusal or a may-fail).
func TestContractAssumptions(t *testing.T) {
	ids := map[string]bool{}
	for _, r := range contractRows {
		ids[r.id] = true
	}
	for _, p := range contractPreconditions {
		ids[p.id] = true
	}
	valid := regexp.MustCompile(`^E([1-6]|8)$`)
	for id, as := range contractAssumptions {
		if !ids[id] {
			t.Errorf("contractAssumptions names %s, which is no row or precondition", id)
		}
		if len(as) == 0 {
			t.Errorf("%s: an empty list (leave it out instead)", id)
		}
		for i, a := range as {
			if !valid.MatchString(a) {
				t.Errorf("%s: %q is no environment assumption (E1-E6, E8)", id, a)
			}
			if i > 0 && as[i-1] >= a {
				t.Errorf("%s: assumptions %v are not distinct and in order", id, as)
			}
		}
	}
	for _, r := range contractRows {
		if got, want := slices.Contains(contractAssumptions[r.id], "E8"), r.id != "D" && r.id != "F1"; got != want {
			t.Errorf("row %s: E8 listed %v, want %v (Revision 2.1)", r.id, got, want)
		}
	}
	for _, p := range contractPreconditions {
		if slices.Contains(contractAssumptions[p.id], "E8") {
			t.Errorf("precondition %s lists E8; E8 is on rows only (Revision 2.1)", p.id)
		}
	}
	// The assumptions the task names as examples.
	for id, want := range map[string]string{"M9.P2": "E5", "R1.P2": "E5", "O5.P4": "E6", "M7.P1": "E1"} {
		if !slices.Contains(contractAssumptions[id], want) {
			t.Errorf("%s rests on %s (Revision 1.9), contractAssumptions has %v", id, want, contractAssumptions[id])
		}
	}
}
