package schema

import (
	"context"
	"regexp"
	"strings"
	"testing"
)

// Harness rules of T018a2 Revision 1.7: H17 (startup and inlining frames
// of SQL functions created with check_function_bodies off), H18 (roles a
// history creates are dropped) and H19 (the catalog is read with
// pg_catalog-qualified names, and a history whose facts cannot be read is
// an error).

// sqlStartupFrame returns the function name of an innermost `SQL function
// "f" during startup` or `during inlining` frame (functions.c
// init_sql_fcache, clauses.c inline_function 4963).
func sqlStartupFrame(where string) (string, bool) {
	for _, line := range strings.Split(where, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, `SQL statement "`) || strings.HasPrefix(line, `SQL expression "`) || strings.HasPrefix(line, `PL/pgSQL assignment "`) {
			continue
		}
		rest, ok := strings.CutPrefix(line, `SQL function "`)
		if !ok {
			return "", false
		}
		i := strings.LastIndex(rest, `"`)
		if i < 0 {
			return "", false
		}
		switch strings.TrimSpace(rest[i+1:]) {
		case "during startup", "during inlining":
			return rest[:i], true
		}
		return "", false
	}
	return "", false
}

var (
	functionBodiesMention = regexp.MustCompile(`(?i)check_function_bodies`)
	routineCreation       = regexp.MustCompile(`(?i)\bcreate\s+(?:or\s+replace\s+)?(?:function|procedure)\s+((?:"(?:[^"]|"")+"|[a-z_][a-z0-9_$]*)(?:\s*\.\s*(?:"(?:[^"]|"")+"|[a-z_][a-z0-9_$]*))?)`)
)

// unvalidatedRoutines lists the routines a CREATE FUNCTION or PROCEDURE
// creates after the history's text first mentions check_function_bodies
// (prelude, then migrations in file order): their bodies may not have
// been validated at creation (H17; an over-approximation of "created while
// it was off").
func unvalidatedRoutines(h *oracleHistory) map[string]bool {
	out := map[string]bool{}
	var texts []string
	texts = append(texts, h.Prelude...)
	for _, f := range h.all() {
		texts = append(texts, f.SQL)
	}
	seen := false
	for _, text := range texts {
		from := 0
		if !seen {
			loc := functionBodiesMention.FindStringIndex(text)
			if loc == nil {
				continue
			}
			seen, from = true, loc[1]
		}
		for _, m := range routineCreation.FindAllStringSubmatch(text[from:], -1) {
			parts := strings.Split(m[1], ".")
			part := strings.TrimSpace(parts[len(parts)-1])
			if strings.HasPrefix(part, `"`) {
				out[strings.ReplaceAll(part[1:len(part)-1], `""`, `"`)] = true
			} else {
				out[strings.ToLower(part)] = true
			}
		}
	}
	return out
}

func TestHarnessRev17(t *testing.T) {
	for _, tc := range []struct {
		where string
		name  string
		want  bool
	}{
		{`SQL function "f" during inlining`, "f", true},
		{`SQL function "f" during startup`, "f", true},
		{"SQL statement \"SELECT f()\"\nSQL function \"g\" during startup", "g", true},
		{`SQL function "f" statement 1`, "", false},
		{`SQL function "f"`, "", false},
		{`PL/pgSQL function f() line 1 at RETURN`, "", false},
	} {
		name, ok := sqlStartupFrame(tc.where)
		if ok != tc.want || name != tc.name {
			t.Errorf("sqlStartupFrame(%q) = %q, %v; want %q, %v", tc.where, name, ok, tc.name, tc.want)
		}
	}
	h := &oracleHistory{Files: []oracleFile{
		{Name: "1_a.sql", SQL: "CREATE FUNCTION before() RETURNS int LANGUAGE sql AS 'SELECT 1';\nSET check_function_bodies = off;\nCREATE FUNCTION after1() RETURNS int LANGUAGE sql AS 'SELECT 1';"},
		{Name: "2_b.sql", SQL: "CREATE OR REPLACE FUNCTION s.\"After2\"() RETURNS int LANGUAGE sql AS 'SELECT 1';"},
	}}
	got := unvalidatedRoutines(h)
	if got["before"] || !got["after1"] || !got["After2"] || len(got) != 2 {
		t.Errorf("unvalidatedRoutines = %v", got)
	}
	if a := assumption(outcome{SQLState: "42P01", Message: `relation "x" does not exist`, Where: `SQL function "after1" during inlining`}, nil, nil, func(string) bool { return false }, true, got, ""); a != "" {
		t.Errorf("an unvalidated body's first parse is E3 (%q)", a)
	}
	if a := assumption(outcome{SQLState: "42P01", Message: `relation "x" does not exist`, Where: `SQL function "before" during startup`}, nil, nil, func(string) bool { return false }, true, got, ""); a != "E3" {
		t.Errorf("a validated body's startup is %q, want E3", a)
	}
}

// TestHarnessRev17Postgres checks H18 and H19 against Postgres: a role a
// history creates does not reach the next history, and a history that
// shadows pg_catalog's types in its search_path still has its facts read.
func TestHarnessRev17Postgres(t *testing.T) {
	o := sharedOracle(t)
	ctx := context.Background()
	roles := &oracleHistory{ID: "h18", Files: []oracleFile{{Name: "1_a.sql", SQL: "CREATE ROLE gv_r17_leak;\nCREATE TABLE t (a int);\nGRANT SELECT ON t TO gv_r17_leak;\nALTER DEFAULT PRIVILEGES GRANT SELECT ON TABLES TO gv_r17_leak;"}}}
	for i := 0; i < 2; i++ {
		v, err := o.run(ctx, roles)
		if err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
		if len(v.Outcomes) != 1 || v.Outcomes[0].Status != "accepted" {
			t.Fatalf("run %d: %+v (the role of an earlier run leaked)", i, v.Outcomes)
		}
	}
	for _, fresh := range []bool{true} {
		shadow := &oracleHistory{ID: "h19", FreshDB: fresh, Files: []oracleFile{{Name: "1_a.sql", SQL: "SET search_path TO public, pg_catalog;\nCREATE TABLE text (a int);\nCREATE TABLE int8 (b int PRIMARY KEY);\nCREATE TABLE u (c text);"}}}
		v, err := o.run(ctx, shadow)
		if err != nil {
			t.Fatalf("fresh %v: the facts of a history that shadows pg_catalog types were not read: %v", fresh, err)
		}
		if len(v.Tables) != 3 {
			t.Fatalf("fresh %v: tables %+v", fresh, v.Tables)
		}
	}
}
