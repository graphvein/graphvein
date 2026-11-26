package schema

// Oracle tests (T018a2 investigation): the regression cases run in every go test;
// the fixture, generated and real-history runs need
// GRAPHVEIN_TEST_POSTGRES_URL (CI's postgres job, or a cloud session's
// scratch cluster) and skip without it.
//
// Environment variables (no new binary, A2/B8):
//   - GRAPHVEIN_ORACLE_SEEDS: generated seeds, "a-b" (default 1-200 when
//     a server is set, the fast subset; Revision 2.1: 1-1000 passed go
//     test's 10-minute package timeout under load). Gate runs name the
//     range, one generator per process, with -timeout 0;
//   - GRAPHVEIN_ORACLE_HISTORIES: a directory of prepared real histories
//     (the T018a2 task record (internal));
//   - GRAPHVEIN_ORACLE_REPORT: a JSON Lines file the runs append their
//     coverage rows to;
//   - GRAPHVEIN_ORACLE_CASES: a directory minimised repros are written to;
//   - GRAPHVEIN_ORACLE_GENERATORS: the generators to run, "g1,g2,g3,g4,g5,g6,g7,g8,g9"
//     by default (Revision 1: g2 is g1 plus the B1a preconditions, g3 is g2
//     plus the B1b preconditions, g4 is g3 plus Revision 1.3's DML values
//     and N1 columns, g5 is g4 plus B2's literals and coverage rows, g6 is
//     g5 plus Revision 1.4's R1-R5 variants, g7 is g6 plus Revision 1.5's U1-U10 variants, U2 from step C3; g8 is g7 plus Revision 1.6's V1-V13 variants; g9 is g8 plus Revision 1.9's Y-rule variants);
//   - GRAPHVEIN_ORACLE_REVISION: the revision label of coverage rows.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// knownStops returns the root-cause signatures of the recorded repros
// under testdata/oracle/ (the T018a2 task record (internal)), each mapped to
// the Revision 1 classes (owners) of the cases that carry it. A signature
// without a repro fails the runs: it is a new precision stop.
func knownStops(t *testing.T) map[string][]string {
	t.Helper()
	knownOnce.Do(func() {
		knownMap = map[string][]string{}
		for _, c := range readCases(t) {
			if c.Class != "K1" && c.Class != "K2" && !slices.Contains(knownMap[c.Signature], c.Owner) {
				knownMap[c.Signature] = append(knownMap[c.Signature], c.Owner)
			}
		}
	})
	return knownMap
}

var (
	knownOnce sync.Once
	knownMap  map[string][]string
)

// fixedClasses are the Revision 1 classes the steps so far have fixed: a
// root of one of them is a defect of its unit, not an expected root.
// B1a: K1 and K2 (with P1), P1, P2, P3, P10 and the harness defects H1-H6;
// B1b: P4-P9; Revision 1.4: P11; Revision 1.5 step C2: P13 and its rules
// U3-U6 (owners of review15 cases). Every class is fixed but P12 (C3).
var fixedClasses = map[string]bool{"K1": true, "K2": true, "P1": true, "P2": true, "P3": true, "P4": true, "P5": true, "P6": true, "P7": true, "P8": true, "P9": true, "P10": true, "P11": true, "P13": true, "U3": true, "U4": true, "U5": true, "U6": true, "H1": true, "H2": true, "H3": true, "H4": true, "H5": true, "H6": true}

func oracleURL(t *testing.T) string {
	t.Helper()
	u := os.Getenv("GRAPHVEIN_TEST_POSTGRES_URL")
	if u == "" {
		t.Skip("GRAPHVEIN_TEST_POSTGRES_URL is not set; the oracle runs in CI's postgres job")
	}
	return u
}

var (
	oracleOnce   sync.Once
	oracleShared *oracleDB
	oracleErr    error
)

func sharedOracle(t *testing.T) *oracleDB {
	t.Helper()
	u := oracleURL(t)
	oracleOnce.Do(func() {
		oracleShared, oracleErr = openOracle(context.Background(), u)
	})
	if oracleErr != nil {
		t.Fatal(oracleErr)
	}
	return oracleShared
}

func TestMain(m *testing.M) {
	code := m.Run()
	if oracleShared != nil {
		if err := oracleShared.close(context.Background()); err != nil {
			fmt.Fprintln(os.Stderr, "dropping the oracle database:", err)
		}
	}
	os.Exit(code)
}

// judged is one history's oracle result.
type judged struct {
	x      *extraction
	h      *oracleHistory
	v      *verdict
	all    []disagreement
	roots  []disagreement
	counts counts
	// e8 is the H27 judgement (generated and real histories, and
	// regression cases with "gate": "e8"); nil under the strict comparison.
	e8 *e8Judgement
}

func judge(ctx context.Context, t *testing.T, o *oracleDB, h *oracleHistory) (*judged, error) {
	x, err := extractTemp(h)
	if err != nil {
		return nil, err
	}
	v, err := o.run(ctx, h)
	if err != nil {
		return nil, err
	}
	return judgeWith(h, x, v), nil
}

// extractTemp runs extract in a temporary directory that is removed as
// soon as the pass returns. The extraction is held in memory, so nothing
// reads the directory afterwards. t.TempDir would keep every seed's
// directory until the test ends, which filled the disk on 100,000-seed
// shards (about 3 GB per shard).
func extractTemp(h *oracleHistory) (*extraction, error) {
	dir, err := os.MkdirTemp("", "gv-oracle-")
	if err != nil {
		return nil, fmt.Errorf("creating a temporary directory for %s: %w", h.ID, err)
	}
	defer os.RemoveAll(dir)
	return extract(h, dir)
}

func judgeWith(h *oracleHistory, x *extraction, v *verdict) *judged {
	all, c := compare(x, v)
	j := &judged{x: x, h: h, v: v, all: all, counts: c}
	j.roots = classify(h, v, all)
	return j
}

// classify keeps the root causes: the first applied disagreement (later
// ones and the facts of its migration and later ones follow from it,
// because sqlx stops there) and fact disagreements that no applied
// disagreement explains. Each root gets class K1, K2 or S1.
func classify(h *oracleHistory, v *verdict, all []disagreement) []disagreement {
	index := map[string]int{}
	for i, o := range v.Outcomes {
		index[o.File] = i
	}
	first := -1
	for _, d := range all {
		if d.Kind == "applied" && (first < 0 || index[d.Object] < first) {
			first = index[d.Object]
		}
	}
	var roots []disagreement
	for _, d := range all {
		if d.Kind == "applied" {
			if index[d.Object] != first {
				continue
			}
			o := v.Outcomes[first]
			d.Class = appliedClass(h, v, o)
			d.Tag = statementTag(h, o)
			roots = append(roots, d)
			continue
		}
		if first >= 0 {
			// The catalog stops before the refused migration; facts of
			// tables it or a later migration touched cannot be compared.
			continue
		}
		d.Class = "S1"
		roots = append(roots, d)
	}
	return roots
}

func statementTag(h *oracleHistory, o outcome) string {
	tags := h.Tags[o.File]
	if o.Statement > 0 && o.Statement <= len(tags) {
		return tags[o.Statement-1]
	}
	return ""
}

// appliedClass decides K1 (a refusal naming an object the set creates
// only later: E1 does not cover it), K2 (ALTER TYPE ... ADD VALUE of an
// existing label or a missing type) or S1.
func appliedClass(h *oracleHistory, v *verdict, o outcome) string {
	stmt := strings.ToLower(strings.TrimSpace(stripComments(o.StmtText)))
	addValue := strings.HasPrefix(stmt, "alter type") && strings.Contains(stmt, " add value")
	if o.Status != "refused" {
		return "S1"
	}
	switch o.SQLState {
	case "42710":
		if addValue && strings.Contains(o.Message, "enum label") {
			return "K2"
		}
	case "42P01", "42704", "3F000", "42883":
		at := position(h, o) + len(o.StmtText)
		// The missing object is the first name the message prints
		// ("relation "t"", "rule "r" for relation "t"", "function f()").
		names := append(quotedName.FindAllStringSubmatch(o.Message, -1), functionName.FindAllStringSubmatch(o.Message, -1)...)
		if len(names) > 1 {
			names = names[:1]
		}
		for _, n := range names {
			for _, part := range strings.Split(n[1], ".") {
				part, _, _ = strings.Cut(part, "(")
				if created, ok := firstCreation(h, part); ok && created > at {
					if addValue {
						return "K2"
					}
					return "K1"
				}
			}
		}
		if addValue && o.SQLState == "42704" {
			if _, ok := firstCreation(h, ""); !ok {
				return "K2"
			}
		}
	}
	return "S1"
}

// position orders statements across the history: file index in sqlx
// order times 1e6 plus the byte offset.
func position(h *oracleHistory, o outcome) int {
	order, _ := sqlxOrder(h.Files, sqlxReadsNoTx(h.cargo()))
	for i, m := range order {
		if m.file.Name == o.File {
			off := strings.Index(m.file.SQL, o.StmtText)
			if off < 0 {
				off = 0
			}
			return i*1_000_000 + off
		}
	}
	return 0
}

func firstCreation(h *oracleHistory, name string) (int, bool) {
	if name == "" {
		return 0, false
	}
	order, _ := sqlxOrder(h.Files, sqlxReadsNoTx(h.cargo()))
	for i, m := range order {
		locs := creationPattern.FindAllStringSubmatchIndex(m.file.SQL, -1)
		for _, loc := range selectIntoPattern.FindAllStringSubmatchIndex(m.file.SQL, -1) {
			if oracleIntoCreates(m.file.SQL[:loc[0]]) {
				locs = append(locs, loc)
			}
		}
		slices.SortFunc(locs, func(a, b []int) int { return a[2] - b[2] })
		for _, loc := range locs {
			text := m.file.SQL[loc[2]:loc[3]]
			for _, part := range strings.Split(text, ".") {
				part = strings.TrimSpace(part)
				if strings.HasPrefix(part, `"`) {
					part = strings.ReplaceAll(part[1:len(part)-1], `""`, `"`)
				} else {
					part = strings.ToLower(part)
				}
				if part == name {
					return i*1_000_000 + loc[2], true
				}
			}
		}
	}
	return 0, false
}

// rootSignature identifies a root cause for the minimiser and the known
// list: the class, SQLSTATE and statement tag of an applied disagreement,
// or the fact kind and object.
func rootSignature(d disagreement, v *verdict) string {
	if d.Kind == "applied" {
		for _, o := range v.Outcomes {
			if o.File == d.Object {
				return fmt.Sprintf("applied|%s|%s|%s", d.Class, o.SQLState, d.Tag)
			}
		}
	}
	return d.Kind + "|" + d.Object
}

// knownSignature is the generator-independent part used in oracleKnown:
// class, SQLSTATE and the contract-row tag without the object.
func knownSignature(d disagreement, v *verdict) string {
	if d.Kind != "applied" {
		return d.Kind
	}
	primary, _, _ := strings.Cut(d.Tag, "+")
	for _, o := range v.Outcomes {
		if o.File == d.Object {
			return fmt.Sprintf("applied|%s|%s|%s", d.Class, o.SQLState, primary)
		}
	}
	return "applied|" + d.Class
}

// Fixtures ------------------------------------------------------------------

// fixtureHistories turns every Postgres set of the sql-migrations fixture
// into a history (A5). pg-app's files 8 and 9 are included: the oracle
// judges them. Sets of other dialects are not histories (the pass makes no
// Postgres claim about them).
func fixtureHistories(t *testing.T) []*oracleHistory {
	t.Helper()
	entries, err := os.ReadDir(fixture)
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.ReadFile(filepath.Join(fixture, "Cargo.toml"))
	if err != nil {
		t.Fatal(err)
	}
	workspace := string(root)
	if i := strings.Index(workspace, "members = ["); i >= 0 {
		end := strings.Index(workspace[i:], "]")
		workspace = workspace[:i] + `members = ["pkg"]` + workspace[i+end+1:]
	}
	var out []*oracleHistory
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "pg-") && e.Name() != "bad-version" && e.Name() != "dup-version" && e.Name() != "with-config" {
			continue
		}
		dir := filepath.Join(fixture, e.Name())
		cargo, err := os.ReadFile(filepath.Join(dir, "Cargo.toml"))
		if err != nil {
			continue
		}
		h := &oracleHistory{ID: "fixture/" + e.Name(), Cargo: string(cargo), FreshDB: true}
		if strings.Contains(string(cargo), "workspace = true") {
			h.Workspace = workspace
		}
		files, err := os.ReadDir(filepath.Join(dir, "migrations"))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			if !f.Type().IsRegular() {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, "migrations", f.Name()))
			if err != nil {
				t.Fatal(err)
			}
			h.Files = append(h.Files, oracleFile{Name: f.Name(), SQL: string(data)})
		}
		switch e.Name() {
		case "pg-unreadable":
			// Created by the tests, as in the golden run (README).
			h.Unreadable = []rawFile{{Name: "2_unreadable.sql", Data: []byte("CREATE TABLE \xff (id integer);\n")}}
		case "pg-all-unreadable":
			h.Unreadable = []rawFile{{Name: "1_only.sql", Data: []byte("CREATE TABLE \xff (id integer);\n")}}
		}
		h.Prelude = fixturePrelude(e.Name())
		out = append(out, h)
	}
	return out
}

// fixturePrelude creates the E1 objects a fixture set names but never
// creates.
func fixturePrelude(name string) []string {
	role := func(r string) string {
		return `DO $$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = '` + r + `') THEN CREATE ROLE ` + r + `; END IF; END $$`
	}
	switch name {
	case "pg-rev2-roles":
		return []string{role("app"), role("legacy_owner")}
	case "pg-search-path":
		return []string{"CREATE SCHEMA app"}
	}
	return nil
}

func TestOracleFixtures(t *testing.T) {
	o := sharedOracle(t)
	ctx := context.Background()
	report := newReport(t)
	var roots []disagreement
	for _, h := range fixtureHistories(t) {
		j, err := judge(ctx, t, o, h)
		if err != nil {
			t.Fatalf("%s: %v", h.ID, err)
		}
		report.history(j, "fixture")
		logClaimedHits(t, j)
		seen := map[string]bool{}
		for _, d := range j.roots {
			t.Logf("%s: %s %s %s: %s [%s]", h.ID, d.Class, d.Kind, d.Object, d.Detail, rootSignature(d, j.v))
			roots = append(roots, d)
			sig := rootSignature(d, j.v)
			if dir := os.Getenv("GRAPHVEIN_ORACLE_CASES"); dir != "" && !seen[d.Kind] {
				seen[d.Kind] = true
				g := fromHistory(h)
				if err := minimiseAndWrite(ctx, t, o, g, d, sig, dir, false); err != nil {
					t.Errorf("minimising %s: %v", h.ID, err)
				}
			}
		}
		for _, oc := range j.v.Outcomes {
			if oc.Assumption != "" {
				t.Logf("%s: assumption hit %s at %s: %s %s", h.ID, oc.Assumption, oc.File, oc.SQLState, oc.Message)
			}
		}
		checkKnown(t, h.ID, j)
	}
	report.close()
}

// checkKnown fails on a root cause that has no recorded repro (a new
// precision stop), and on one whose repros all belong to a class a step
// has fixed (fixedClasses). A root whose signature also has a repro of a
// class not fixed yet is an expected root of that class (counted).
func checkKnown(t *testing.T, id string, j *judged) {
	t.Helper()
	for _, d := range j.roots {
		if d.Class == "K1" || d.Class == "K2" {
			t.Errorf("%s: %s root %s %s: %s", id, d.Class, d.Kind, d.Object, d.Detail)
			continue
		}
		sig := knownSignature(d, j.v)
		owners, ok := knownStops(t)[sig]
		if !ok {
			t.Errorf("%s: new precision stop %s: %s %s: %s", id, sig, d.Kind, d.Object, d.Detail)
			continue
		}
		open := false
		for _, o := range owners {
			open = open || !fixedClasses[o]
		}
		if !open {
			t.Errorf("%s: root %s of fixed class %v: %s %s: %s", id, sig, owners, d.Kind, d.Object, d.Detail)
		}
	}
}

// Generated -------------------------------------------------------------------

// defaultSeeds is the generated range of the default suite (T018a2
// Revision 2.1): small enough that the package stays within the postgres
// job's budget, while gate runs name their own range.
const defaultSeeds = "1-200"

func seedRange(t *testing.T) (uint64, uint64) {
	spec := os.Getenv("GRAPHVEIN_ORACLE_SEEDS")
	if spec == "" {
		spec = defaultSeeds
	}
	a, b, ok := strings.Cut(spec, "-")
	from, err1 := strconv.ParseUint(a, 10, 64)
	to, err2 := strconv.ParseUint(b, 10, 64)
	if !ok || err1 != nil || err2 != nil || to < from {
		t.Fatalf("GRAPHVEIN_ORACLE_SEEDS %q: want a-b", spec)
	}
	return from, to
}

// TestOracleGenerated runs the generated histories of each generator in
// GRAPHVEIN_ORACLE_GENERATORS (comma-separated; default "g1,g2,g3,g4,g5,g6,g7,g8,g9").
func TestOracleGenerated(t *testing.T) {
	versions := os.Getenv("GRAPHVEIN_ORACLE_GENERATORS")
	if versions == "" {
		versions = "g1,g2,g3,g4,g5,g6,g7,g8,g9"
	}
	for _, v := range strings.Split(versions, ",") {
		t.Run(v, func(t *testing.T) { runGenerated(t, v) })
	}
}

func runGenerated(t *testing.T, version string) {
	o := sharedOracle(t)
	ctx := context.Background()
	from, to := seedRange(t)
	report := newReport(t)
	casesDir := os.Getenv("GRAPHVEIN_ORACLE_CASES")
	start := time.Now()
	type group struct {
		count int
		first uint64
		roots disagreement
	}
	groups := map[string]*group{}
	hits := map[string]int{}
	var tally e8Tally
	var total counts
	tagCounts := map[string]int{}
	lossMayFail, lossPartial, lossFirst := map[string]int{}, map[string]int{}, map[string]int{}
	for seed := from; seed <= to; seed++ {
		g := generateWith(seed, version)
		h := g.history()
		for _, tags := range h.Tags {
			for _, tag := range tags {
				tagCounts[tag]++
			}
		}
		// H27 (Revision 2.1): generated histories are judged under E8.
		j := judgeLiveE8(ctx, o, h)
		takeNotes(t, o, h.ID)
		tally.add(j.e8)
		logE8(t, j)
		if j.e8.Class == "stopped" {
			continue
		}
		addCounts(&total, j.counts)
		first, mf, pt := j.x.losses()
		if first != "" {
			cause, _, _ := strings.Cut(first, ": ")
			lossFirst[cause]++
		}
		for k, v := range mf {
			lossMayFail[k] += v
		}
		for k, v := range pt {
			lossPartial[k] += v
		}
		for _, oc := range j.v.Outcomes {
			if oc.Assumption != "" {
				hits[oc.Assumption+" "+oc.SQLState+" "+statementTag(h, oc)]++
			}
		}
		// H22's claimed-hit lines are H27's e8-hit lines (logE8). Every
		// root fails the run (logE8); each signature is minimised once.
		for _, d := range j.roots {
			sig := d.Class + " " + knownSignature(d, j.v)
			gr := groups[sig]
			if gr == nil {
				gr = &group{first: seed, roots: d}
				groups[sig] = gr
				if casesDir != "" && d.Class != "stopped" {
					if err := minimiseAndWrite(ctx, t, o, g, d, rootSignature(d, j.v), casesDir, true); err != nil {
						t.Errorf("minimising %s: %v", h.ID, err)
					}
				}
			}
			gr.count++
		}
	}
	var sigs []string
	for s := range groups {
		sigs = append(sigs, s)
	}
	sort.Strings(sigs)
	for _, s := range sigs {
		gr := groups[s]
		t.Logf("root %s: %d histories, first seed %d (%s %s: %s)", s, gr.count, gr.first, gr.roots.Kind, gr.roots.Object, gr.roots.Detail)
	}
	var hs []string
	for k := range hits {
		hs = append(hs, k)
	}
	sort.Strings(hs)
	for _, k := range hs {
		t.Logf("assumption hit %s: %d", k, hits[k])
	}
	t.Logf("H27 gate (%s seeds %d-%d): %s", version, from, to, tally.String())
	t.Logf("seeds %d-%d (%s): %d histories in %s; migrations %d applied %d null %d; known facts %d of %d catalog facts, conditional %d, null %d",
		from, to, version, to-from+1, time.Since(start).Round(time.Second), total.Migrations, total.Applied, total.AppliedNull, total.KnownFacts, total.CatalogFacts, total.Conditional, total.NullFacts)
	if os.Getenv("GRAPHVEIN_ORACLE_TAGS") != "" {
		var tags []string
		for k := range tagCounts {
			tags = append(tags, k)
		}
		sort.Strings(tags)
		for _, k := range tags {
			t.Logf("tag %s: %d", k, tagCounts[k])
		}
	}
	t.Logf("first possible failure by cause %v; may-fail statements by cause %v; partial-table statements by cause %v", lossFirst, lossMayFail, lossPartial)
	roots := map[string]int{}
	for s, gr := range groups {
		roots[s] = gr.count
	}
	report.row(map[string]any{"history": fmt.Sprintf("generated %s seeds %d-%d", version, from, to), "revision": reportRevision(), "kind": "generated", "counts": total, "roots": roots, "e8": tally.String(), "e8_hits": tally.byClass,
		"first_may_fail_by_cause": lossFirst, "may_fail_by_cause": lossMayFail, "partial_by_cause": lossPartial, "tags": tagCounts})
	report.close()
}

func addCounts(a *counts, b counts) {
	a.Tables += b.Tables
	a.TablesComplete += b.TablesComplete
	a.Migrations += b.Migrations
	a.Applied += b.Applied
	a.AppliedNull += b.AppliedNull
	a.CatalogFacts += b.CatalogFacts
	a.KnownFacts += b.KnownFacts
	a.Conditional += b.Conditional
	a.NullFacts += b.NullFacts
	a.ComparedFacts += b.ComparedFacts
	a.OpaqueTables += b.OpaqueTables
	a.AssumptionHits += b.AssumptionHits
	a.Disagreements += b.Disagreements
	a.UncomparedTypes += b.UncomparedTypes
}

// Minimiser -----------------------------------------------------------------

// minimise shrinks a generated history while the same root cause (by
// rootSignature) remains: it removes migrations, then statements, then
// merges adjacent migrations, then drops top-level clauses of statements.
// Under H27 (e8) it judges in E8 mode and keeps a history only while it
// reproduces the same root in the same class.
func minimise(ctx context.Context, t *testing.T, o *oracleDB, g *genHistory, want string, e8 bool) (*genHistory, *judged, error) {
	check := func(c *genHistory) (*judged, bool) {
		var j *judged
		if e8 {
			j = judgeLiveE8(ctx, o, c.history())
			o.notes = nil
		} else {
			var err error
			if j, err = judge(ctx, t, o, c.history()); err != nil {
				return nil, false
			}
		}
		for _, d := range j.roots {
			sig := rootSignature(d, j.v)
			if e8 {
				sig = d.Class + " " + sig
			}
			if sig == want {
				return j, true
			}
		}
		return nil, false
	}
	best, ok := check(g)
	if !ok {
		return nil, nil, fmt.Errorf("the disagreement %s does not reproduce", want)
	}
	cur := g.clone()
	for changed := true; changed; {
		changed = false
		for i := len(cur.migs) - 1; i >= 0 && len(cur.migs) > 1; i-- {
			c := cur.clone()
			c.migs = slices.Delete(c.migs, i, i+1)
			if j, ok := check(c); ok {
				cur, best, changed = c, j, true
			}
		}
		for i := range cur.migs {
			for k := len(cur.migs[i].stmts) - 1; k >= 0 && len(cur.migs[i].stmts) > 1; k-- {
				c := cur.clone()
				c.migs[i].stmts = slices.Delete(c.migs[i].stmts, k, k+1)
				if j, ok := check(c); ok {
					cur, best, changed = c, j, true
				}
			}
		}
		for i := 0; i+1 < len(cur.migs); i++ {
			if cur.migs[i].noTx || cur.migs[i+1].noTx {
				continue
			}
			c := cur.clone()
			c.migs[i].stmts = append(c.migs[i].stmts, c.migs[i+1].stmts...)
			c.migs = slices.Delete(c.migs, i+1, i+2)
			if j, ok := check(c); ok {
				cur, best, changed = c, j, true
			}
		}
		for i := range cur.migs {
			for k := range cur.migs[i].stmts {
				for _, shorter := range dropClauses(cur.migs[i].stmts[k].sql) {
					c := cur.clone()
					c.migs[i].stmts[k].sql = shorter
					if j, ok := check(c); ok {
						cur, best, changed = c, j, true
						break
					}
				}
			}
		}
		// The prelude: drop entries the case does not need.
		for i := len(cur.prelude) - 1; i >= 0; i-- {
			c := cur.clone()
			c.prelude = slices.Delete(c.prelude, i, i+1)
			if j, ok := check(c); ok {
				cur, best, changed = c, j, true
			}
		}
	}
	return cur, best, nil
}

// dropClauses returns variants of a statement with one top-level element
// of its first parenthesised list removed (columns, constraints), or one
// trailing option word removed.
func dropClauses(sql string) []string {
	open := strings.Index(sql, "(")
	if open < 0 {
		return nil
	}
	depth, start := 0, open+1
	var parts [][2]int
	end := -1
	for i := open; i < len(sql) && end < 0; i++ {
		switch sql[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				parts = append(parts, [2]int{start, i})
				end = i
			}
		case ',':
			if depth == 1 {
				parts = append(parts, [2]int{start, i})
				start = i + 1
			}
		case '\'':
			for i++; i < len(sql) && sql[i] != '\''; i++ {
			}
		}
	}
	if len(parts) < 2 {
		return nil
	}
	var out []string
	for k := range parts {
		var elems []string
		for m, p := range parts {
			if m != k {
				elems = append(elems, strings.TrimSpace(sql[p[0]:p[1]]))
			}
		}
		out = append(out, sql[:open+1]+strings.Join(elems, ", ")+sql[end:])
	}
	return out
}

// fromHistory splits a file-based history into statements so that the
// minimiser can shrink it (fixture and real histories). Members the pass
// cannot read stay as they are.
func fromHistory(h *oracleHistory) *genHistory {
	g := &genHistory{id: strings.ReplaceAll(h.ID, "/", "-"), fresh: h.FreshDB, prelude: slices.Clone(h.Prelude), cargo: h.Cargo, workspace: h.Workspace, unreadable: h.Unreadable}
	for _, f := range h.Files {
		m := genMigration{name: f.Name, noTx: strings.HasPrefix(f.SQL, "-- no-transaction")}
		statements, failure := lexStatements([]byte(f.SQL))
		for _, st := range statements {
			text := strings.TrimSuffix(strings.TrimSpace(f.SQL[st.start:st.end]), ";")
			m.stmts = append(m.stmts, genStmt{sql: text, tag: "fixture"})
		}
		if failure != nil {
			m.stmts = append(m.stmts, genStmt{sql: strings.TrimSuffix(f.SQL[failure.start:], "\n"), tag: "fixture.unlexed"})
		}
		if len(m.stmts) == 0 {
			m.stmts = append(m.stmts, genStmt{sql: "-- " + f.Name, tag: "fixture.empty"})
		}
		g.migs = append(g.migs, m)
	}
	return g
}

// oracleCase is a recorded minimised repro (testdata/oracle/<case>/).
type oracleCase struct {
	ID        string `json:"id"`
	Signature string `json:"signature"`
	Class     string `json:"class"`
	// Owner is the Revision 1 class the root belongs to (P1-P10, K1, K2,
	// H1-H3): the step that owns the class fixes it.
	Owner   string         `json:"owner"`
	Row     string         `json:"row"`
	History *oracleHistory `json:"history"`
	Verdict *verdict       `json:"verdict"`
	// Disagreements are the root causes today's code produces against the
	// verdict. The investigation records them; after the fix they must be empty.
	Disagreements []disagreement `json:"disagreements"`
	// Gate is "e8" for a case judged under H27 (Revision 2.1); empty or
	// "strict" for today's strict comparison.
	Gate string `json:"gate,omitempty"`
	// E8Hit is the e8-hit line H27 gives the case ("" for none).
	E8Hit string `json:"e8_hit,omitempty"`
	// PrefixVerdict is the fresh replay of the prefix (H27, a
	// no-transaction migration k), so the case is judged without a server.
	PrefixVerdict *verdict `json:"prefix_verdict,omitempty"`
}

func minimiseAndWrite(ctx context.Context, t *testing.T, o *oracleDB, g *genHistory, d disagreement, sig, dir string, e8 bool) error {
	if e8 {
		sig = d.Class + " " + sig
	}
	small, j, err := minimise(ctx, t, o, g, sig, e8)
	if err != nil {
		return err
	}
	what := d.Kind
	if d.Tag != "" {
		what = d.Tag
	}
	id := strings.NewReplacer("|", "-", ".", "_", "+", "_").Replace(fmt.Sprintf("%s-%s-%s", small.id, d.Class, what))
	c := oracleCase{ID: id, Signature: knownSignature(d, j.v), Class: d.Class, Row: d.Tag, History: j.h, Verdict: j.v, Disagreements: j.roots}
	if e8 {
		c.Gate, c.E8Hit, c.PrefixVerdict = "e8", j.e8.Hit, j.e8.PrefixVerdict
	}
	return writeCase(filepath.Join(dir, id), &c)
}

func writeCase(dir string, c *oracleCase) error {
	if err := os.MkdirAll(filepath.Join(dir, "migrations"), 0o755); err != nil {
		return err
	}
	for _, f := range c.History.Files {
		if err := os.WriteFile(filepath.Join(dir, "migrations", f.Name), []byte(f.SQL), 0o644); err != nil {
			return err
		}
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "oracle.json"), append(data, '\n'), 0o644)
}

const oracleCases = "testdata/oracle"

func readCases(t *testing.T) []*oracleCase {
	t.Helper()
	entries, err := os.ReadDir(oracleCases)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []*oracleCase
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(oracleCases, e.Name())
		data, err := os.ReadFile(filepath.Join(dir, "oracle.json"))
		if err != nil {
			t.Fatal(err)
		}
		var c oracleCase
		if err := json.Unmarshal(data, &c); err != nil {
			t.Fatalf("%s: %v", dir, err)
		}
		files, err := os.ReadDir(filepath.Join(dir, "migrations"))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			sql, err := os.ReadFile(filepath.Join(dir, "migrations", f.Name()))
			if err != nil {
				t.Fatal(err)
			}
			c.History.Files = append(c.History.Files, oracleFile{Name: f.Name(), SQL: strings.ReplaceAll(string(sql), "\r\n", "\n")})
		}
		out = append(out, &c)
	}
	return out
}

// TestOracleRegression compares today's extraction of every recorded
// repro with its recorded verdict, without Postgres: the root causes must
// be exactly the recorded ones (the investigation pins the precision stops; the fix
// empties them). With a server it also re-checks the verdict.
func TestOracleRegression(t *testing.T) {
	if dir := os.Getenv("GRAPHVEIN_ORACLE_REFRESH"); dir != "" {
		refreshCases(t, dir)
		return
	}
	cases := readCases(t)
	for _, c := range cases {
		t.Run(c.ID, func(t *testing.T) {
			x, err := extractTemp(c.History)
			if err != nil {
				t.Fatal(err)
			}
			if c.Owner == "" {
				t.Errorf("case %s has no owner class (Revision 1)", c.ID)
			}
			var j *judged
			switch c.Gate {
			case "", "strict":
				j = judgeWith(c.History, x, c.Verdict)
				logClaimedHits(t, j)
			case "e8":
				// H27: judged in E8 mode; a fresh prefix replay is the
				// recorded one.
				pf := prefixFuncs{extract: extractTemp, replay: func(*oracleHistory) (*verdict, error) {
					if c.PrefixVerdict == nil {
						return nil, fmt.Errorf("the case records no prefix verdict")
					}
					return c.PrefixVerdict, nil
				}}
				j = judgeWithE8(c.History, x, c.Verdict, pf)
				if j.e8.Hit != c.E8Hit {
					t.Errorf("e8 hit: today %q, recorded %q", j.e8.Hit, c.E8Hit)
				}
			default:
				t.Fatalf("case %s: unknown gate %q", c.ID, c.Gate)
			}
			if got, want := sigs(j.roots, c.Verdict), sigs(c.Disagreements, c.Verdict); !slices.Equal(got, want) {
				t.Errorf("root causes: today %v, recorded %v", got, want)
			}
			if os.Getenv("GRAPHVEIN_TEST_POSTGRES_URL") == "" {
				return
			}
			o := sharedOracle(t)
			v, err := o.run(context.Background(), c.History)
			takeNotes(t, o, c.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got, want := verdictText(v), verdictText(c.Verdict); got != want {
				t.Errorf("the live server's verdict differs from the recorded one:\nlive     %s\nrecorded %s", got, want)
			}
			if c.PrefixVerdict != nil {
				k, _ := firstStop(c.Verdict)
				pv, err := o.run(context.Background(), prefixHistory(c.History, c.Verdict, k))
				takeNotes(t, o, c.ID)
				if err != nil {
					t.Fatal(err)
				}
				if got, want := verdictText(pv), verdictText(c.PrefixVerdict); got != want {
					t.Errorf("the live prefix verdict differs from the recorded one:\nlive     %s\nrecorded %s", got, want)
				}
			}
		})
	}
}

// refreshCases re-runs every case under dir on the live server with the
// current harness and rewrites its verdict, root causes and signature; a
// case that no longer has a root of its class is reported for removal.
// It is a maintenance mode (GRAPHVEIN_ORACLE_REFRESH=<dir>), not a check.
func refreshCases(t *testing.T, dir string) {
	o := sharedOracle(t)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(filepath.Join(p, "oracle.json"))
		if err != nil {
			continue
		}
		var c oracleCase
		if err := json.Unmarshal(data, &c); err != nil {
			t.Fatal(err)
		}
		files, _ := os.ReadDir(filepath.Join(p, "migrations"))
		for _, f := range files {
			sql, _ := os.ReadFile(filepath.Join(p, "migrations", f.Name()))
			c.History.Files = append(c.History.Files, oracleFile{Name: f.Name(), SQL: string(sql)})
		}
		if c.Gate == "e8" {
			j := judgeLiveE8(context.Background(), o, c.History)
			takeNotes(t, o, c.ID)
			c.Verdict, c.Disagreements, c.E8Hit, c.PrefixVerdict = j.v, j.roots, j.e8.Hit, j.e8.PrefixVerdict
			t.Logf("%s: H27 %s, k %d (%s), prefix %s, roots %v", e.Name(), j.e8.Class, j.e8.K, j.e8.KVerdict, j.e8.Prefix, sigs(j.roots, j.v))
			if err := writeCase(p, &c); err != nil {
				t.Fatal(err)
			}
			continue
		}
		j, err := judge(context.Background(), t, o, c.History)
		takeNotes(t, o, c.ID)
		if err != nil {
			t.Fatal(err)
		}
		c.Verdict, c.Disagreements, c.Signature = j.v, j.roots, ""
		for _, d := range j.roots {
			if d.Class == c.Class {
				c.Signature = knownSignature(d, j.v)
				break
			}
		}
		if c.Signature == "" {
			t.Logf("REMOVE %s: no %s root any more (%v)", e.Name(), c.Class, sigs(j.roots, j.v))
		}
		if err := writeCase(p, &c); err != nil {
			t.Fatal(err)
		}
	}
}

func sigs(ds []disagreement, v *verdict) []string {
	var out []string
	for _, d := range ds {
		out = append(out, d.Class+" "+rootSignature(d, v))
	}
	sort.Strings(out)
	return out
}

// verdictText is the part of a verdict a re-check compares: outcomes with
// SQLSTATE and the catalog facts.
func verdictText(v *verdict) string {
	var b strings.Builder
	for _, o := range v.Outcomes {
		fmt.Fprintf(&b, "%s:%s:%s ", o.File, o.Status, o.SQLState)
	}
	data, _ := json.Marshal(v.Tables)
	b.Write(data)
	return b.String()
}

// Real histories --------------------------------------------------------------

// realHistories reads prepared histories: each directory holds
// history.json (id, cargo, prelude, fresh_db) and migrations/.
func realHistories(t *testing.T, root string) []*oracleHistory {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	var out []*oracleHistory
	for _, e := range entries {
		dir := filepath.Join(root, e.Name())
		data, err := os.ReadFile(filepath.Join(dir, "history.json"))
		if err != nil {
			continue
		}
		var h oracleHistory
		if err := json.Unmarshal(data, &h); err != nil {
			t.Fatalf("%s: %v", dir, err)
		}
		files, err := os.ReadDir(filepath.Join(dir, "migrations"))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			if !f.Type().IsRegular() {
				continue
			}
			sql, err := os.ReadFile(filepath.Join(dir, "migrations", f.Name()))
			if err != nil {
				t.Fatal(err)
			}
			h.Files = append(h.Files, oracleFile{Name: f.Name(), SQL: string(sql)})
		}
		out = append(out, &h)
	}
	return out
}

func TestOracleRealHistories(t *testing.T) {
	root := os.Getenv("GRAPHVEIN_ORACLE_HISTORIES")
	if root == "" {
		t.Skip("GRAPHVEIN_ORACLE_HISTORIES is not set; real histories run in the long job")
	}
	o := sharedOracle(t)
	ctx := context.Background()
	report := newReport(t)
	var tally e8Tally
	for _, h := range realHistories(t, root) {
		start := time.Now()
		// H27 (Revision 2.1): real histories are judged under E8.
		j := judgeLiveE8(ctx, o, h)
		takeNotes(t, o, h.ID)
		tally.add(j.e8)
		logE8(t, j)
		if j.e8.Class == "stopped" {
			continue
		}
		report.history(j, "real")
		t.Logf("%s: H27 %s, k %d %s (%s), prefix %s", h.ID, j.e8.Class, j.e8.K, j.e8.KFile, j.e8.KVerdict, j.e8.Prefix)
		t.Logf("%s: %d migrations (%d applied, %d null), tables %d (%d complete), known facts %d of %d, cut at %d, %s", h.ID, j.counts.Migrations, j.counts.Applied, j.counts.AppliedNull, j.counts.Tables, j.counts.TablesComplete, j.counts.KnownFacts, j.counts.CatalogFacts, j.counts.CutAt, time.Since(start).Round(time.Millisecond))
		for _, oc := range j.v.Outcomes {
			if oc.Status == "refused" {
				t.Logf("%s: %s refused (%s) %s %s; statement %d: %.200s", h.ID, oc.File, oc.Assumption, oc.SQLState, oc.Message, oc.Statement, oc.StmtText)
			}
		}
	}
	t.Logf("H27 gate (real histories): %s", tally.String())
	report.close()
}

// Report ----------------------------------------------------------------------

// reportRevision labels coverage rows (GRAPHVEIN_ORACLE_REVISION; the investigation's
// label by default).
func reportRevision() string {
	if r := os.Getenv("GRAPHVEIN_ORACLE_REVISION"); r != "" {
		return r
	}
	return "baseline (origin/main 73aca2e)"
}

type reportFile struct {
	t *testing.T
	f *os.File
}

func newReport(t *testing.T) *reportFile {
	p := os.Getenv("GRAPHVEIN_ORACLE_REPORT")
	if p == "" {
		return &reportFile{t: t}
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	return &reportFile{t: t, f: f}
}

func (r *reportFile) row(v map[string]any) {
	if r.f == nil {
		return
	}
	data, err := json.Marshal(v)
	if err != nil {
		r.t.Fatal(err)
	}
	r.f.Write(append(data, '\n'))
}

func (r *reportFile) history(j *judged, kind string) {
	c := j.counts
	share := func(a, b int) float64 {
		if b == 0 {
			return 0
		}
		return float64(int(float64(a)/float64(b)*10000)) / 10000
	}
	var roots []string
	for _, d := range j.roots {
		roots = append(roots, d.Class+" "+rootSignature(d, j.v))
	}
	var hits []string
	for _, o := range j.v.Outcomes {
		if o.Assumption != "" {
			hits = append(hits, fmt.Sprintf("%s %s %s: %s", o.Assumption, o.File, o.SQLState, o.Message))
		}
	}
	statements := 0
	for _, f := range j.h.Files {
		st, _ := lexStatements([]byte(f.SQL))
		statements += len(st)
	}
	first, mayFail, partial := j.x.losses()
	r.row(map[string]any{
		"history": j.h.ID, "kind": kind, "revision": reportRevision(), "files": len(j.h.Files), "statements": statements,
		"first_may_fail": first, "may_fail_by_cause": mayFail, "partial_by_cause": partial,
		"tables_complete": share(c.TablesComplete, c.Tables), "migrations_applied": share(c.Applied, c.Migrations), "facts_known": share(c.KnownFacts, c.CatalogFacts),
		"counts": c, "roots": roots, "assumption_hits": hits,
	})
}

func (r *reportFile) close() {
	if r.f != nil {
		r.f.Close()
	}
}
