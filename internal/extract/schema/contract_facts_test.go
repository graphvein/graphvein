package schema

import (
	"fmt"
	"regexp"

	"github.com/graphvein/graphvein/internal/contract"
	"maps"
	"slices"
	"sort"
	"strings"
	"testing"
)

// Fact kinds a statement can write (T018a2 Revision 1.4 R6, the B7 1:1
// check strengthened): the facts of the state are snapshotted before and
// after each statement, and every fact the statement adds or changes is
// checked against the kinds its contract rows may claim.
const (
	factTable    = "table"    // a table, its name, schema and persistence
	factColumn   = "column"   // a column and its attributes
	factKey      = "key"      // a primary key or UNIQUE constraint
	factFK       = "fk"       // a foreign key
	factIndex    = "index"    // an index
	factCheck    = "check"    // a named CHECK or EXCLUDE constraint
	factRelation = "relation" // a recorded relation or type, enum labels
	factTrigger  = "trigger"  // a trigger name on a table
	factPolicy   = "policy"   // a policy name on a table
)

// rowFacts lists, per contract row, the fact kinds a statement of that
// row may add or change. A row not listed may claim none (N2, N3, R1b,
// F1, O3, O4, Q1, D: their effects are partial marks, counts and
// possible failures, which are not facts).
var rowFacts = map[string][]string{
	"M1":    {factTable, factColumn, factKey, factFK, factCheck, factRelation},
	"M1b":   {factTable, factRelation},
	"M2":    {factColumn},
	"M3":    {factFK},
	"M4":    {factIndex, factRelation},
	"M5a":   {factColumn, factKey, factFK, factCheck},
	"M5b":   {factColumn, factKey, factFK, factIndex, factCheck},
	"M5c":   {factColumn, factFK},
	"M5d":   {factColumn},
	"M5e":   {factColumn},
	"M5f":   {factKey, factFK, factCheck, factColumn}, // ADD PRIMARY KEY sets NOT NULL
	"M5f-u": {factKey, factIndex, factColumn},         // the index becomes the key's; PRIMARY KEY sets NOT NULL
	"M5g":   {factKey, factFK, factCheck},
	"M5g-1": {factKey, factFK, factCheck},
	"M5h":   {factColumn},
	"M5i":   {factTable, factIndex, factKey, factRelation}, // ALTER TABLE ... RENAME TO renames any relation but a composite type (V4 kinds)
	"M5j":   {factTable},
	"M5k-a": {factTable},
	"M5k-b": {factTable},
	"M6":    {factTable, factFK, factRelation},
	"M7":    {factIndex},
	"M8":    {factIndex, factKey, factTable, factRelation},
	"M9":    {factTable, factRelation},
	"R1":    {factRelation},
	"R2":    {factRelation},
	"R3":    {factRelation},
	"R4":    {factRelation, factTable},
	"R5":    {factRelation},
	"R6":    {factRelation},
	"N1":    {factTrigger, factPolicy},
	"O1t":   {factTrigger},
	"O1":    {factRelation},
	"O2":    {factRelation},
	"O5":    {factRelation},
	"T1":    {factRelation, factTable},
}

// factSnapshot is the state's facts by identity (pointers, so that a
// rename changes a value, not every key).
func factSnapshot(s *state) map[string]string {
	out := map[string]string{}
	ptr := func(p any) string { return fmt.Sprintf("%p", p) }
	opt := func(p *string) string {
		if p == nil {
			return "-"
		}
		return *p
	}
	cols := func(cs []*column) string {
		var b []string
		for _, c := range cs {
			b = append(b, ptr(c))
		}
		return strings.Join(b, ",")
	}
	for _, t := range s.tables {
		id := ptr(t)
		out[factTable+":"+id] = fmt.Sprintf("%s|%v", t.q, t.unlogged)
		for _, c := range t.columns {
			out[factColumn+":"+ptr(c)] = fmt.Sprintf("%s|%s|%v|%s|%s|%s|%s", c.name, c.typ, c.nullable, opt(c.def), opt(c.identity), opt(c.generated), opt(c.collation))
		}
		for _, k := range append([]*key{t.primaryKey}, t.uniques...) {
			if k != nil {
				out[factKey+":"+ptr(k)] = fmt.Sprintf("%s|%s|%v", opt(k.name), cols(k.cols), k == t.primaryKey)
			}
		}
		for _, fk := range t.foreignKeys {
			target := "-"
			if fk.table != nil {
				target = ptr(fk.table)
			}
			out[factFK+":"+ptr(fk)] = fmt.Sprintf("%s|%s|%s|%s", opt(fk.name), cols(fk.local), target, cols(fk.columns))
		}
		for _, ix := range t.indexes {
			out[factIndex+":"+ptr(ix)] = fmt.Sprintf("%s|%v|%s", opt(ix.name), ix.unique, cols(ix.columns))
		}
		for _, ch := range t.checks {
			out[factCheck+":"+id+":"+ch.name] = "1"
		}
		for n := range t.triggerNames {
			out[factTrigger+":"+id+":"+n] = "1"
		}
		for n := range t.policyNames {
			out[factPolicy+":"+id+":"+n] = "1"
		}
	}
	for k, r := range s.relations {
		out[factRelation+":"+k] = fmt.Sprintf("%s|%v", r.kind, r.labels)
	}
	return out
}

// TestContractFacts runs the fixtures, the regression cases, the SQL
// cases and g1-g8 seeds 1-200, and checks that every fact a statement adds
// or changes, while its migration is still applied (no possible failure
// yet), is of a kind its rows may claim (rowFacts).
func TestContractFacts(t *testing.T) {
	var before map[string]string
	var rowsBefore map[string]int
	seen := map[string]int{}
	var bad []string
	saved := statementHook
	statementHook = func(s *state, after bool) {
		if !after {
			before, rowsBefore = factSnapshot(s), maps.Clone(s.set.rows)
			return
		}
		if s.failure != nil {
			return
		}
		var rows []string
		for r, n := range s.set.rows {
			if n > rowsBefore[r] {
				rows = append(rows, r)
			}
		}
		allowed := map[string]bool{}
		for _, r := range rows {
			for _, k := range rowFacts[r] {
				allowed[k] = true
			}
		}
		for k, v := range factSnapshot(s) {
			if old, ok := before[k]; ok && old == v {
				continue
			}
			kind, _, _ := strings.Cut(k, ":")
			seen[kind]++
			if !allowed[kind] {
				sort.Strings(rows)
				bad = append(bad, fmt.Sprintf("%s: rows %v wrote a %s fact (%s)", s.m.file, rows, kind, v))
			}
		}
	}
	defer func() { statementHook = saved }()
	cited := map[string]int{}
	cite := func(ds []contract.Diagnostic) {
		for _, d := range ds {
			for _, m := range citedID.FindAllStringSubmatch(d.Message, -1) {
				cited[m[1]]++
			}
		}
	}
	run := func(h *oracleHistory) {
		x, err := extract(h, t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		cite(x.diagnostics)
	}
	for _, h := range fixtureHistories(t) {
		run(h)
	}
	for _, c := range readCases(t) {
		run(c.History)
	}
	for _, cases := range [][]b1dCase{b1dCases, b2Cases, b3Cases, rev15Cases, rev15C2Cases, rev15C3Cases, rev16Cases, rev16D2Cases, rev17Cases, rev18Cases, rev19Cases, rev19bCases, rev19bTxCases, rev19cCases} {
		for _, tc := range cases {
			_, ds := linkSQL(t, tc.sql)
			cite(ds)
		}
	}
	for _, sql := range preconditionCases {
		_, ds := linkSQL(t, sql)
		cite(ds)
	}
	for seed := uint64(1); seed <= 200; seed++ {
		for _, v := range []string{"g1", "g2", "g3", "g4", "g5", "g6", "g7", "g8", "g9"} {
			run(generateWith(seed, v).history())
		}
	}
	slices.Sort(bad)
	bad = slices.Compact(bad)
	for i, b := range bad {
		if i == 20 {
			t.Errorf("... and %d more", len(bad)-20)
			break
		}
		t.Error(b)
	}
	t.Logf("facts written by kind: %v", seen)
	// Every precondition (the refusal conditions of its row) is met by at
	// least one case: a diagnostic cites it.
	for _, p := range contractPreconditions {
		if why := shadowedPreconditions[p.id]; why != "" {
			if cited[p.id] > 0 {
				t.Errorf("precondition %s is cited now; remove it from shadowedPreconditions", p.id)
			}
			continue
		}
		if cited[p.id] == 0 {
			t.Errorf("precondition %s is cited by no case (fixtures, regression cases, SQL cases, preconditionCases, g1-g8 seeds 1-200)", p.id)
		}
	}
}

var citedID = regexp.MustCompile(`\[contract ([^\]]+)\]`)

// preconditionCases are SQL cases for the preconditions the other cases
// do not meet (each listed with the id it is for).
var preconditionCases = []string{
	// M1b.P1: a partition parent with the generated-name shape of a set
	// table (a plain name the set creates is the token rule's, N1.P1).
	"CREATE TABLE c PARTITION OF t_pkey FOR VALUES IN (1);\nCREATE TABLE t (a int PRIMARY KEY);",
	// M9.P1: a schema the set creates later.
	"ALTER SCHEMA s RENAME TO s2;\nCREATE SCHEMA s;",
	// R5.P1: a schema the set creates later.
	"DROP SCHEMA s;\nCREATE SCHEMA s;",
	// R3.P1: a type the set creates later.
	"ALTER TYPE e ADD VALUE 'b';\nCREATE TYPE e AS ENUM ('a');",
	// F1.P6: PreventInTransactionBlock in sqlx's transaction (VACUUM and
	// REINDEX CONCURRENTLY are refused by the F1 analysis first).
	"REINDEX SCHEMA public;",
	// Q1.P13: a session search_path without the default schema at the end
	// of the migration (sqlx's unqualified INSERT INTO _sqlx_migrations).
	"CREATE SCHEMA other;\nSET search_path TO other;",
}

// shadowedPreconditions are the preconditions whose condition another
// one reports first in every case, with the reason; the test checks that
// they stay uncited (remove an entry when a case cites it).
var shadowedPreconditions = map[string]string{}
