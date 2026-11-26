package schema

import (
	"regexp"
	"slices"
	"strings"
	"testing"
)

// Harness rules of T018a2 Revision 1.6 (H14-H16): the assumption scoring of
// a refusal by its error context, by the kind of object a relation
// template names, and by where the checked rows came from.

// executionFrame reports whether a refusal's context shows that code was
// running when it was raised (H14): the innermost frame (the first line of
// Where that is not the query of an SPI call: `SQL statement "..."`, or
// in its other parse modes `SQL expression "..."` and `PL/pgSQL
// assignment "..."`) is a PL/pgSQL execution frame
// (pl_exec.c plpgsql_exec_error_callback 1263-1288: every form starts
// "PL/pgSQL function "), an SQL function statement, inlining or startup
// (functions.c 1452, 1478; clauses.c 4963), or an extension script. The
// compile frame (pl_comp.c 998 "compilation of PL/pgSQL function"), the
// SQL validator's bare `SQL function "f"` (pg_proc.c 986) and no context
// are not: those refusals are judged.
func executionFrame(where string) bool {
	for _, line := range strings.Split(where, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, `SQL statement "`) || strings.HasPrefix(line, `SQL expression "`) || strings.HasPrefix(line, `PL/pgSQL assignment "`) {
			// spi.c _SPI_error_callback 2984-2996: the query of the SPI
			// call, in each of its parse modes, not a frame of its own.
			continue
		}
		switch {
		case strings.HasPrefix(line, "PL/pgSQL function "):
			return true
		case strings.HasPrefix(line, "extension script"):
			return true
		case strings.HasPrefix(line, `SQL function "`):
			rest := line[strings.LastIndex(line, `"`)+1:]
			rest = strings.TrimSpace(rest)
			return strings.HasPrefix(rest, "statement ") || rest == "during inlining" || rest == "during startup"
		}
		return false
	}
	return false
}

// tablePosition reports whether the statement names the relation name (its
// last part, folded as Postgres prints it) in a table position (H15): the
// target of ALTER TABLE, CREATE INDEX ... ON, CREATE / DROP TRIGGER, POLICY
// or RULE ... ON, REFERENCES, LIKE, INHERITS, PARTITION OF, TRUNCATE, LOCK,
// COPY, ANALYZE, VACUUM, CLUSTER, REINDEX TABLE, COMMENT ON TABLE / COLUMN,
// GRANT / REVOKE ON [TABLE], SECURITY LABEL ON TABLE and FROM / JOIN / INTO
// / UPDATE / DELETE FROM of DML and queries. A name that appears only in a
// string constant, or after INDEX, SEQUENCE, VIEW and the other kind words,
// is not in a table position.
func tablePosition(stmt, name string) bool {
	statements, _ := lexStatements([]byte(stmt))
	if len(statements) == 0 {
		return false
	}
	toks := statements[0].tokens
	word := func(i int) string {
		if i < 0 || i >= len(toks) || toks[i].kind != tokIdent {
			return ""
		}
		return toks[i].value
	}
	punct := func(i int, p string) bool {
		return i >= 0 && i < len(toks) && toks[i].kind == tokPunct && toks[i].value == p
	}
	alterIndex := word(0) == "alter" && word(1) == "index"
	isName := func(i int) bool {
		return i >= 0 && i < len(toks) && (toks[i].kind == tokIdent || toks[i].kind == tokQuoted)
	}
	// nameStart walks back from a name part over "part ." to the first part.
	nameStart := func(i int) int {
		for i >= 2 && punct(i-1, ".") && isName(i-2) {
			i -= 2
		}
		return i
	}
	// head reports whether the token before a qualified name (at start)
	// makes it a table position.
	var head func(start int) bool
	head = func(start int) bool {
		j := start - 1
		for word(j) == "only" || (word(j) == "exists" && word(j-1) == "if") {
			if word(j) == "exists" {
				j -= 2
			} else {
				j--
			}
		}
		switch w := word(j); w {
		case "table":
			// ALTER, COMMENT ON, GRANT ON, LOCK, REINDEX, TRUNCATE, SECURITY
			// LABEL ON, DROP TABLE: every TABLE word names a table.
			return true
		case "on":
			// CREATE INDEX / TRIGGER / POLICY / RULE ... ON, DROP TRIGGER /
			// POLICY / RULE ... ON, COMMENT ON TRIGGER / CONSTRAINT /
			// POLICY n ON, GRANT ... ON: a kind word after ON is not here.
			return !alterIndex
		case "references", "like", "truncate", "lock", "copy", "analyze", "vacuum", "cluster", "verbose", "full", "from", "join", "into", "update":
			return true
		case "of":
			return word(j-1) == "partition"
		case "partition":
			// ALTER TABLE ... ATTACH / DETACH PARTITION p.
			return !alterIndex && (word(j-1) == "attach" || word(j-1) == "detach")
		}
		if punct(j, "(") && word(j-1) == "inherits" {
			return true
		}
		if punct(j, ")") && (word(0) == "analyze" || word(0) == "vacuum") {
			// VACUUM (options) t.
			return true
		}
		if punct(j, ",") {
			// A list element: skip the previous element (a qualified name,
			// optionally followed by *) and decide by the list's head.
			k := j - 1
			if k >= 0 && toks[k].kind == tokOp && toks[k].value == "*" {
				k--
			}
			if !isName(k) {
				return false
			}
			return head(nameStart(k))
		}
		return false
	}
	for i, t := range toks {
		if t.kind != tokIdent && t.kind != tokQuoted || t.value != name {
			continue
		}
		start := nameStart(i)
		if head(start) {
			return true
		}
		// COMMENT ON COLUMN t.c names the table as the prefix.
		if word(start-1) == "column" && punct(i+1, ".") {
			return true
		}
	}
	return false
}

// copyStatement reads one COPY head: the target name and the direction.
var copyStatement = regexp.MustCompile(`(?is)^copy\s+(binary\s+)?((?:"(?:[^"]|"")+"|[a-z_][a-z0-9_$]*)(?:\s*\.\s*(?:"(?:[^"]|"")+"|[a-z_][a-z0-9_$]*))*)\s*(\([^)]*\))?\s+(from|to)\b`)

var copyWord = regexp.MustCompile(`(?i)\bcopy\b`)

// foreignRowsFor reports whether one of the tables (last name parts, as
// Postgres folds them) may hold rows that did not come from the set's SQL
// (H16): a COPY ... FROM into it, or a COPY whose target or direction the
// harness cannot read, in the text seen so far, or rows of the prelude. Rows
// written by the set's code (DO, CALL, functions, triggers, rules, EXECUTE)
// are the set's own.
func foreignRowsFor(seen string, prelude []string, tables []string) bool {
	for _, p := range prelude {
		if regexp.MustCompile(`(?i)\b(insert|copy|update|merge|into|do|call|execute)\b`).MatchString(p) {
			return true
		}
	}
	text := stripComments(seen)
	for _, loc := range copyWord.FindAllStringIndex(text, -1) {
		rest := text[loc[0]:]
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(rest[4:])), "(") {
			// COPY (query) TO writes no table.
			continue
		}
		m := copyStatement.FindStringSubmatch(rest)
		if m == nil {
			return true
		}
		if strings.EqualFold(m[4], "to") {
			continue
		}
		target := lastNamePart(m[2])
		for _, t := range tables {
			if t == target {
				return true
			}
		}
	}
	return false
}

// lastNamePart folds the last part of a written qualified name.
func lastNamePart(s string) string {
	statements, _ := lexStatements([]byte(s))
	if len(statements) == 0 || len(statements[0].tokens) == 0 {
		return ""
	}
	toks := statements[0].tokens
	return toks[len(toks)-1].value
}

// checkedTables are the tables whose rows an ALTER TABLE or CREATE INDEX
// checks: its target, and the tables its REFERENCES name. ok is false when
// the harness cannot read the target.
func checkedTables(stmt string) (tables []string, ok bool) {
	statements, _ := lexStatements([]byte(stmt))
	if len(statements) == 0 {
		return nil, false
	}
	toks := statements[0].tokens
	word := func(i int) string {
		if i < 0 || i >= len(toks) || toks[i].kind != tokIdent {
			return ""
		}
		return toks[i].value
	}
	isName := func(i int) bool {
		return i >= 0 && i < len(toks) && (toks[i].kind == tokIdent || toks[i].kind == tokQuoted)
	}
	// qualified reads a name starting at i and returns its last part.
	qualified := func(i int) (string, bool) {
		if !isName(i) {
			return "", false
		}
		last := toks[i].value
		for i+2 < len(toks) && toks[i+1].kind == tokPunct && toks[i+1].value == "." && isName(i+2) {
			i += 2
			last = toks[i].value
		}
		return last, true
	}
	i := 0
	switch {
	case word(0) == "alter" && word(1) == "table":
		i = 2
		if word(i) == "if" && word(i+1) == "exists" {
			i += 2
		}
		if word(i) == "only" {
			i++
		}
	case word(0) == "create":
		on := -1
		for k, t := range toks {
			if t.kind == tokIdent && t.value == "on" {
				on = k
				break
			}
		}
		if on < 0 {
			return nil, false
		}
		i = on + 1
		if word(i) == "only" {
			i++
		}
	default:
		return nil, false
	}
	target, found := qualified(i)
	if !found {
		return nil, false
	}
	tables = append(tables, target)
	for k := range toks {
		if word(k) == "references" {
			if n, found := qualified(k + 1); found {
				tables = append(tables, n)
			}
		}
	}
	return tables, true
}

// TestHarnessRev16 checks the scoring helpers of H14-H16 on the error
// contexts and statements of the Revision 1.6 probes.
func TestHarnessRev16(t *testing.T) {
	for _, tc := range []struct {
		where string
		want  bool
	}{
		{"PL/pgSQL function f() line 3 at RAISE", true},
		{"SQL statement \"INSERT INTO t VALUES (1)\"\nPL/pgSQL function inline_code_block line 1 at SQL statement", true},
		{"PL/pgSQL function f() during function exit", true},
		{"PL/pgSQL assignment \"x := 1\"\nPL/pgSQL function f(integer) line 1 at assignment", true},
		{"SQL expression \"1/0\"\nPL/pgSQL function f() line 1 at RETURN", true},
		{"SQL expression \"1\"\ncompilation of PL/pgSQL function \"f\" near line 1", false},
		{"SQL function \"s1\" statement 1", true},
		{"SQL function \"s1\" during inlining", true},
		{"SQL function \"s1\" during startup", true},
		{"extension script file \"x--1.0.sql\", near line 3", true},
		{"compilation of PL/pgSQL function \"f\" near line 1", false},
		{"SQL function \"s1\"", false},
		{"", false},
		{"SQL statement \"SELEC 1\"\ncompilation of PL/pgSQL function \"f\" near line 1", false},
	} {
		if got := executionFrame(tc.where); got != tc.want {
			t.Errorf("executionFrame(%q) = %v, want %v", tc.where, got, tc.want)
		}
	}
	for _, tc := range []struct {
		stmt, name string
		want       bool
	}{
		{"ALTER TABLE nosuch_t DROP CONSTRAINT c", "nosuch_t", true},
		{"ALTER TABLE IF EXISTS ONLY s.nosuch_t ADD COLUMN a int", "nosuch_t", true},
		{"CREATE INDEX i ON nosuch_t (a)", "nosuch_t", true},
		{"DROP TRIGGER tr ON nosuch_t", "nosuch_t", true},
		{"CREATE TABLE c (a int REFERENCES nosuch_t)", "nosuch_t", true},
		{"CREATE TABLE c (LIKE nosuch_t)", "nosuch_t", true},
		{"CREATE TABLE c () INHERITS (a, nosuch_t)", "nosuch_t", true},
		{"CREATE TABLE c PARTITION OF nosuch_t DEFAULT", "nosuch_t", true},
		{"TRUNCATE a, nosuch_t", "nosuch_t", true},
		{"LOCK TABLE nosuch_t IN SHARE MODE", "nosuch_t", true},
		{"COMMENT ON COLUMN nosuch_t.c IS 'x'", "nosuch_t", true},
		{"GRANT SELECT ON nosuch_t TO r", "nosuch_t", true},
		{"SELECT a FROM x, nosuch_t", "nosuch_t", true},
		{"UPDATE nosuch_t SET a = 1", "nosuch_t", true},
		{"ALTER INDEX nosuch_idx RENAME TO y", "nosuch_idx", false},
		{"ALTER INDEX i ATTACH PARTITION nosuch_idx", "nosuch_idx", false},
		{"ALTER VIEW nosuch_v RENAME TO y", "nosuch_v", false},
		{"COMMENT ON INDEX nosuch_idx IS 'x'", "nosuch_idx", false},
		{"GRANT SELECT ON SEQUENCE nosuch_seq TO r", "nosuch_seq", false},
		{"REFRESH MATERIALIZED VIEW nosuch_mv", "nosuch_mv", false},
		{"REINDEX INDEX nosuch_idx", "nosuch_idx", false},
		{"CLUSTER t USING nosuch_idx", "nosuch_idx", false},
		{"SELECT 'nosuch_x'::regclass", "nosuch_x", false},
		{"SELECT nextval('nosuch_seq')", "nosuch_seq", false},
	} {
		if got := tablePosition(tc.stmt, tc.name); got != tc.want {
			t.Errorf("tablePosition(%q, %q) = %v, want %v", tc.stmt, tc.name, got, tc.want)
		}
	}
	for _, tc := range []struct {
		seen    string
		prelude []string
		tables  []string
		want    bool
	}{
		{"CREATE TABLE t (a int); DO $$ BEGIN INSERT INTO t VALUES (NULL); END $$;", nil, []string{"t"}, false},
		{"CREATE TABLE t (a int); COPY t FROM STDIN;", nil, []string{"t"}, true},
		{"CREATE TABLE t (a int); COPY u (a) FROM '/x';", nil, []string{"t"}, false},
		{"COPY (SELECT 1) TO STDOUT; COPY t TO STDOUT;", nil, []string{"t"}, false},
		{"DO $$ BEGIN EXECUTE 'COPY ' || 't' || ' FROM STDIN'; END $$;", nil, []string{"t"}, true},
		{"", []string{"INSERT INTO p VALUES (1)"}, []string{"t"}, true},
	} {
		if got := foreignRowsFor(tc.seen, tc.prelude, tc.tables); got != tc.want {
			t.Errorf("foreignRowsFor(%q, %q, %v) = %v, want %v", tc.seen, tc.prelude, tc.tables, got, tc.want)
		}
	}
	for _, tc := range []struct {
		stmt string
		want []string
	}{
		{"ALTER TABLE ONLY s.c ADD FOREIGN KEY (pid) REFERENCES p (id)", []string{"c", "p"}},
		{"CREATE UNIQUE INDEX IF NOT EXISTS i ON ONLY t (a)", []string{"t"}},
	} {
		got, ok := checkedTables(tc.stmt)
		if !ok || !slices.Equal(got, tc.want) {
			t.Errorf("checkedTables(%q) = %v %v, want %v", tc.stmt, got, ok, tc.want)
		}
	}
}
