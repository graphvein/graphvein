package schema

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"sort"
	"strings"
	"testing"
)

// Harness rules of T018a2 Revision 2.1: H27 (the E8 gate with
// its prefix judgement), H25 (role cleanup, in dropNewRoles) and H28
// (concurrent setup).

// sqlRunner is what the role setup needs from a connection: *sql.DB and,
// under the setup lock, *sql.Conn.
type sqlRunner interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// setupLockKey is the advisory lock key of H28. Any fixed value works;
// it only has to be the same in every test process.
const setupLockKey = 0x67766f72 // "gvor"

// withSetupLock runs fn on one admin connection while holding the
// session-level advisory lock of H28 (Revision 2.1): two test processes
// that set up gv_migrator on one cluster at the same time failed with
// "tuple concurrently updated" (XX000).
func withSetupLock(ctx context.Context, admin *sql.DB, fn func(sqlRunner) error) error {
	conn, err := admin.Conn(ctx)
	if err != nil {
		return fmt.Errorf("connecting for the role setup: %w", err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "SELECT pg_catalog.pg_advisory_lock($1)", int64(setupLockKey)); err != nil {
		return fmt.Errorf("taking the setup lock: %w", err)
	}
	defer conn.ExecContext(context.Background(), "SELECT pg_catalog.pg_advisory_unlock($1)", int64(setupLockKey))
	return fn(conn)
}

// note records a harness diagnostic (H25).
func (o *oracleDB) note(s string) { o.notes = append(o.notes, s) }

// takeNotes logs and clears the harness diagnostics.
func takeNotes(t *testing.T, o *oracleDB, id string) {
	t.Helper()
	for _, n := range o.notes {
		t.Logf("%s: harness note: %s", id, n)
	}
	o.notes = nil
}

// H27 ---------------------------------------------------------------------

// e8Judgement is one history's class under H27: clean (every migration
// applied), refused (migration k is the first Postgres did not apply) or
// stopped (the harness could not judge it).
type e8Judgement struct {
	Class string
	// K is the 1-based position of migration k among the up migrations
	// in sqlx order (0 when the history is clean); KFile its file.
	K     int
	KFile string
	// KVerdict is the pass's verdict on k: refusal-agrees (applied:
	// false), may-fail (null) or e8-hit (true).
	KVerdict string
	// Hit is the e8-hit line when KVerdict is e8-hit; HitClass its
	// assumption (E1-E6) or R, HitState its SQLSTATE.
	Hit, HitClass, HitState string
	// Prefix is "judged" when the prefix was compared, "empty" when k is
	// the first migration (counted, not judged).
	Prefix string
	// FreshPrefix is set when the prefix was replayed on a fresh database
	// (k is -- no-transaction, or its text controls transactions).
	FreshPrefix bool
	// PrefixVerdict is the fresh replay's verdict.
	PrefixVerdict *verdict
	Roots         []disagreement
	Err           error
}

// prefixFuncs give judgeE8 what it needs beyond the recorded verdict: the
// pass on a prefix history, and a replay of a prefix on a fresh database.
type prefixFuncs struct {
	extract func(*oracleHistory) (*extraction, error)
	replay  func(*oracleHistory) (*verdict, error)
}

// firstStop finds migration k of H27: the index in v.Outcomes of the
// first refused (or set_refused) migration, or of the first up migration
// after one whose post-commit UPDATE failed (H24). It returns -1 when
// every migration applied. via is the outcome that stopped sqlx.
func firstStop(v *verdict) (k int, via outcome) {
	stopped := false
	for i, o := range v.Outcomes {
		if o.Status == "down" {
			continue
		}
		if stopped {
			return i, via
		}
		switch {
		case o.Status == "refused", o.Status == "set_refused", o.Status == "not_run":
			return i, o
		case o.PostCommit != "":
			stopped, via = true, o
		}
	}
	return -1, outcome{}
}

// prefixHistory is the history without the up migrations from k on, in
// sqlx order: the prelude, the manifest, down files, other members and
// file names unchanged.
func prefixHistory(h *oracleHistory, v *verdict, k int) *oracleHistory {
	drop := map[string]bool{}
	for _, o := range v.Outcomes[k:] {
		if o.Status != "down" {
			drop[o.File] = true
		}
	}
	p := *h
	p.ID = h.ID + "/prefix"
	p.Files = slices.DeleteFunc(slices.Clone(h.Files), func(f oracleFile) bool { return drop[f.Name] })
	p.Unreadable = slices.DeleteFunc(slices.Clone(h.Unreadable), func(f rawFile) bool { return drop[f.Name] })
	if h.Tags != nil {
		p.Tags = map[string][]string{}
		for f, tags := range h.Tags {
			if !drop[f] {
				p.Tags[f] = tags
			}
		}
	}
	return &p
}

// needsFreshPrefix reports whether the catalog read after the stop is not
// the prefix's catalog: k ran without sqlx's transaction (the statements
// before its refusal stayed committed), or its own text controls
// transactions (a COMMIT inside it may have committed part of it).
func needsFreshPrefix(h *oracleHistory, kOutcome outcome) bool {
	if kOutcome.Status != "refused" {
		return false
	}
	if kOutcome.NoTx {
		return true
	}
	for _, f := range h.all() {
		if f.Name == kOutcome.File {
			statements, _ := lexStatements([]byte(f.SQL))
			return hasTransactionControl(statements)
		}
	}
	return false
}

// judgeE8 classifies a history under H27 from its extraction x and the
// oracle's verdict v; the prefix judgement uses pf.
func judgeE8(h *oracleHistory, x *extraction, v *verdict, pf prefixFuncs) *e8Judgement {
	e := &e8Judgement{}
	k, via := firstStop(v)
	appliedRoots := func(x *extraction, outcomes []outcome, seen map[string]bool) {
		for _, o := range outcomes {
			if o.Status == "down" || seen[o.File] {
				continue
			}
			m := x.migrations[o.File]
			if m != nil && m.applied != nil && !*m.applied {
				seen[o.File] = true
				e.Roots = append(e.Roots, disagreement{Kind: "applied", Object: o.File, Class: "F", Detail: "applied: false, Postgres applied it", Tag: statementTag(h, o)})
			}
		}
	}
	factRoots := func(all []disagreement) {
		for _, d := range all {
			if d.Kind == "applied" {
				continue
			}
			d.Class = "E"
			e.Roots = append(e.Roots, d)
		}
	}
	if k < 0 {
		e.Class = "clean"
		all, _ := compare(x, v)
		appliedRoots(x, v.Outcomes, map[string]bool{})
		factRoots(all)
		return e
	}
	e.Class = "refused"
	ko := v.Outcomes[k]
	e.KFile = ko.File
	for _, o := range v.Outcomes[:k+1] {
		if o.Status != "down" {
			e.K++
		}
	}
	seen := map[string]bool{}
	appliedRoots(x, v.Outcomes[:k], seen)
	m := x.migrations[ko.File]
	switch {
	case m == nil || m.applied == nil:
		e.KVerdict = "may-fail"
	case !*m.applied:
		e.KVerdict = "refusal-agrees"
	default:
		e.KVerdict = "e8-hit"
		e.HitClass, e.HitState = "R", ko.SQLState
		if ko.Assumption != "" {
			e.HitClass = ko.Assumption
		}
		text := ko.StmtText
		switch {
		case ko.Status == "not_run" && via.PostCommit != "":
			// Not run after a failed post-commit UPDATE (H24).
			e.HitState, _, _ = strings.Cut(via.PostCommit, " ")
			text = "(after " + via.File + ": " + via.PostCommit + ")"
		case ko.Status == "set_refused":
			text = "(sqlx refuses the set)"
		}
		tag := statementTag(h, ko)
		if tag == "" {
			tag = "-"
		}
		state := e.HitState
		if state == "" {
			state = "-"
		}
		e.Hit = fmt.Sprintf("e8-hit %s %s %s %s %s %s", e.HitClass, state, h.ID, ko.File, tag, shortText(text))
	}
	if e.K == 1 {
		e.Prefix = "empty"
		return e
	}
	ph := prefixHistory(h, v, k)
	px, err := pf.extract(ph)
	if err != nil {
		return stoppedE8(e, h, fmt.Errorf("extracting the prefix: %w", err))
	}
	var pv *verdict
	if needsFreshPrefix(h, ko) {
		e.FreshPrefix = true
		if pv, err = pf.replay(ph); err != nil {
			return stoppedE8(e, h, fmt.Errorf("replaying the prefix: %w", err))
		}
		if k2, o := firstStop(pv); k2 >= 0 {
			return stoppedE8(e, h, fmt.Errorf("the prefix replay stopped at %s (%s %s %s), the history's run did not", o.File, o.Status, o.SQLState, o.Message))
		}
		e.PrefixVerdict = pv
	} else {
		// The catalog after the stop is the prefix's: k was rolled back,
		// or (H24) the prefix ends with the migration that committed.
		c := *v
		c.Outcomes = nil
		for i, o := range v.Outcomes {
			if i < k || o.Status == "down" {
				c.Outcomes = append(c.Outcomes, o)
			}
		}
		pv = &c
	}
	e.Prefix = "judged"
	all, _ := compare(px, pv)
	appliedRoots(px, pv.Outcomes, seen)
	factRoots(all)
	return e
}

func stoppedE8(e *e8Judgement, h *oracleHistory, err error) *e8Judgement {
	e.Class, e.Err = "stopped", err
	e.Roots = append(e.Roots, disagreement{Kind: "stopped", Object: h.ID, Class: "stopped", Detail: err.Error()})
	return e
}

// judgeLiveE8 runs a history on the server and judges it under H27. An
// error of the history's own run or extraction is a stop too.
func judgeLiveE8(ctx context.Context, o *oracleDB, h *oracleHistory) *judged {
	pf := prefixFuncs{extract: extractTemp, replay: func(p *oracleHistory) (*verdict, error) { return o.run(ctx, p) }}
	x, err := extractTemp(h)
	if err != nil {
		return stoppedJudged(h, fmt.Errorf("extracting: %w", err))
	}
	v, err := o.run(ctx, h)
	if err != nil {
		return stoppedJudged(h, fmt.Errorf("replaying: %w", err))
	}
	return judgeWithE8(h, x, v, pf)
}

// judgeWithE8 is judgeWith under H27: the roots are the E8 gate's.
func judgeWithE8(h *oracleHistory, x *extraction, v *verdict, pf prefixFuncs) *judged {
	_, c := compare(x, v)
	e := judgeE8(h, x, v, pf)
	return &judged{x: x, h: h, v: v, counts: c, roots: e.Roots, e8: e}
}

func stoppedJudged(h *oracleHistory, err error) *judged {
	e := stoppedE8(&e8Judgement{}, h, err)
	return &judged{h: h, v: &verdict{}, x: &extraction{migrations: map[string]*xMigration{}}, roots: e.Roots, e8: e}
}

// e8Tally is the gate summary of H27 for a run.
type e8Tally struct {
	clean, refused, agrees, mayFail, hits, judged, empty, stopped int
	byClass                                                       map[string]int
}

func (s *e8Tally) add(e *e8Judgement) {
	switch e.Class {
	case "clean":
		s.clean++
	case "refused":
		s.refused++
	case "stopped":
		s.stopped++
	}
	switch e.KVerdict {
	case "refusal-agrees":
		s.agrees++
	case "may-fail":
		s.mayFail++
	case "e8-hit":
		s.hits++
		if s.byClass == nil {
			s.byClass = map[string]int{}
		}
		s.byClass[e.HitClass+" "+e.HitState]++
	}
	switch e.Prefix {
	case "judged":
		s.judged++
	case "empty":
		s.empty++
	}
}

func (s *e8Tally) String() string {
	var keys []string
	for k := range s.byClass {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, " [%s: %d]", k, s.byClass[k])
	}
	return fmt.Sprintf("clean %d, refused %d (refusal-agrees %d, may-fail %d, e8-hit %d), prefix judged %d (k = 1: %d), stopped %d; e8 hits by class and SQLSTATE:%s",
		s.clean, s.refused, s.agrees, s.mayFail, s.hits, s.judged, s.empty, s.stopped, b.String())
}

// logE8 writes a judged history's H27 lines: its e8-hit line and its
// roots; every root fails the test (the gate is 0 roots).
func logE8(t *testing.T, j *judged) {
	t.Helper()
	if j.e8.Hit != "" {
		t.Log(j.e8.Hit)
	}
	for _, d := range j.roots {
		t.Errorf("%s: H27 root %s %s %s: %s", j.h.ID, d.Class, d.Kind, d.Object, d.Detail)
	}
}

// TestHarnessRev21 checks compare in E8 mode (H27) on recorded verdicts.
func TestHarnessRev21(t *testing.T) {
	two := &oracleHistory{ID: "rev21", Files: []oracleFile{
		{Name: "1_a.sql", SQL: "CREATE TABLE t (a int NOT NULL);\n"},
		{Name: "2_b.sql", SQL: "CREATE TABLE u (b int);\n"},
	}}
	accepted := func(files ...string) []outcome {
		var out []outcome
		for _, f := range files {
			out = append(out, outcome{File: f, Status: "accepted"})
		}
		return out
	}
	tableT := catTable{Key: "history.t", Columns: []catColumn{{Name: "a", Type: "integer", NotNull: true}}, PK: []string{}}
	tableU := catTable{Key: "history.u", Columns: []catColumn{{Name: "b", Type: "integer"}}, PK: []string{}}
	noReplay := prefixFuncs{extract: extractTemp, replay: func(*oracleHistory) (*verdict, error) {
		t.Error("a fresh prefix replay was asked for")
		return nil, fmt.Errorf("no replay")
	}}
	run := func(h *oracleHistory, v *verdict, pf prefixFuncs) *e8Judgement {
		t.Helper()
		x, err := extractTemp(h)
		if err != nil {
			t.Fatal(err)
		}
		return judgeE8(h, x, v, pf)
	}
	classes := func(e *e8Judgement) string {
		var out []string
		for _, d := range e.Roots {
			out = append(out, d.Class+" "+d.Kind+" "+d.Object)
		}
		return strings.Join(out, "; ")
	}

	// A clean history: every fact agrees, then a wrong fact is an E root.
	good := &verdict{Schema: "history", Outcomes: accepted("1_a.sql", "2_b.sql"), Tables: []catTable{tableT, tableU}}
	if e := run(two, good, noReplay); e.Class != "clean" || len(e.Roots) > 0 {
		t.Errorf("clean history: %s, roots %s", e.Class, classes(e))
	}
	wrong := &verdict{Schema: "history", Outcomes: accepted("1_a.sql", "2_b.sql"), Tables: []catTable{{Key: "history.t", Columns: []catColumn{{Name: "a", Type: "integer"}}, PK: []string{}}, tableU}}
	if e := run(two, wrong, noReplay); e.Class != "clean" || len(e.Roots) == 0 || e.Roots[0].Class != "E" {
		t.Errorf("a wrong fact in a clean history: %s, roots %s; want an E root", e.Class, classes(e))
	}

	// applied: false on a migration Postgres applies is an F root. The
	// pass proves a refusal (applied: false) for a whole set sqlx refuses
	// (a version that is not an integer); the verdict is made up here.
	refusing := &oracleHistory{ID: "rev21-f", Files: []oracleFile{
		{Name: "1_a.sql", SQL: "CREATE TABLE t (a int);\n"},
		{Name: "x2_b.sql", SQL: "CREATE TABLE u (b int);\n"},
	}}
	if e := run(refusing, &verdict{Schema: "history", Outcomes: accepted("1_a.sql", "x2_b.sql"), Tables: []catTable{{Key: "history.t", Columns: []catColumn{{Name: "a", Type: "integer"}}, PK: []string{}}, {Key: "history.u", Columns: []catColumn{{Name: "b", Type: "integer"}}, PK: []string{}}}}, noReplay); e.Class != "clean" || classes(e) != "F applied 1_a.sql; F applied x2_b.sql" {
		t.Errorf("applied: false on accepted migrations: %s, roots %s; want two F roots", e.Class, classes(e))
	}
	// The same set as sqlx refuses it: refusal-agrees at k = 1.
	setRefused := &verdict{Schema: "history", Outcomes: []outcome{{File: "1_a.sql", Status: "set_refused"}, {File: "x2_b.sql", Status: "set_refused"}}}
	if e := run(refusing, setRefused, noReplay); e.Class != "refused" || e.KVerdict != "refusal-agrees" || e.Prefix != "empty" || len(e.Roots) > 0 {
		t.Errorf("a proven refusal: %+v, roots %s", e, classes(e))
	}
	// A refusal the pass leaves open (null) is may-fail.
	twice := &oracleHistory{ID: "rev21-m", Files: []oracleFile{
		{Name: "1_a.sql", SQL: "CREATE TABLE t (a int NOT NULL);\n"},
		{Name: "2_b.sql", SQL: "CREATE TABLE u (b int, b int);\n"},
	}}
	refusedB := &verdict{Schema: "history", Outcomes: []outcome{{File: "1_a.sql", Status: "accepted"}, {File: "2_b.sql", Status: "refused", SQLState: "42701", Message: `column "b" specified more than once`}},
		Tables: []catTable{tableT}}
	if e := run(twice, refusedB, noReplay); e.Class != "refused" || e.KVerdict != "may-fail" || e.Prefix != "judged" || len(e.Roots) > 0 {
		t.Errorf("a may-fail refusal: %+v, roots %s", e, classes(e))
	}

	// applied: true on the refused migration is an E8 hit, not a root; a
	// wrong fact on a table only k touches is not compared.
	hit := &verdict{Schema: "history", Outcomes: []outcome{{File: "1_a.sql", Status: "accepted"}, {File: "2_b.sql", Status: "refused", SQLState: "42501", Message: "permission denied", Statement: 1, StmtText: "CREATE TABLE u (b int);"}},
		Tables: []catTable{tableT}}
	if e := run(two, hit, noReplay); e.Class != "refused" || e.KVerdict != "e8-hit" || e.HitClass != "R" || e.HitState != "42501" || len(e.Roots) > 0 || !strings.HasPrefix(e.Hit, "e8-hit R 42501 rev21 2_b.sql - CREATE TABLE u") {
		t.Errorf("an E8 hit: %+v, roots %s", e, classes(e))
	}
	hit.Outcomes[1].Assumption = "E1"
	if e := run(two, hit, noReplay); e.HitClass != "E1" || !strings.HasPrefix(e.Hit, "e8-hit E1 42501 ") {
		t.Errorf("an E8 hit an assumption scores: %q", e.Hit)
	}
	// A wrong fact the prefix judgement shows is an E root.
	prefixWrong := &verdict{Schema: "history", Outcomes: hit.Outcomes, Tables: []catTable{{Key: "history.t", Columns: []catColumn{{Name: "a", Type: "bigint", NotNull: true}}, PK: []string{}}}}
	if e := run(two, prefixWrong, noReplay); e.Prefix != "judged" || len(e.Roots) == 0 || e.Roots[0].Class != "E" || !strings.HasPrefix(e.Roots[0].Object, "history.t") {
		t.Errorf("a wrong fact in the prefix: %+v, roots %s; want an E root on history.t", e, classes(e))
	}

	// k = 1 is counted, not judged.
	first := &verdict{Schema: "history", Outcomes: []outcome{{File: "1_a.sql", Status: "refused", SQLState: "42501", Message: "permission denied"}, {File: "2_b.sql", Status: "not_run"}}}
	if e := run(two, first, noReplay); e.K != 1 || e.Prefix != "empty" || e.KVerdict != "e8-hit" || len(e.Roots) > 0 {
		t.Errorf("k = 1: %+v, roots %s", e, classes(e))
	}

	// A -- no-transaction k asks for a fresh prefix replay, whose catalog
	// the prefix is judged against.
	noTx := &oracleHistory{ID: "rev21-notx", Files: []oracleFile{
		{Name: "1_a.sql", SQL: "CREATE TABLE t (a int NOT NULL);\n"},
		{Name: "2_b.sql", SQL: "-- no-transaction\nCREATE TABLE u (b int);\nCREATE INDEX CONCURRENTLY ui ON u (b);\n"},
	}}
	stopNoTx := &verdict{Schema: "history", Outcomes: []outcome{{File: "1_a.sql", Status: "accepted"}, {File: "2_b.sql", Status: "refused", SQLState: "XX000", Message: "x", NoTx: true}},
		Tables: []catTable{tableT, tableU}}
	asked := ""
	replay := prefixFuncs{extract: extractTemp, replay: func(p *oracleHistory) (*verdict, error) {
		var names []string
		for _, f := range p.Files {
			names = append(names, f.Name)
		}
		asked = strings.Join(names, " ")
		return &verdict{Schema: "history", Outcomes: accepted("1_a.sql"), Tables: []catTable{tableT}}, nil
	}}
	if e := run(noTx, stopNoTx, replay); !e.FreshPrefix || asked != "1_a.sql" || e.Prefix != "judged" || len(e.Roots) > 0 {
		t.Errorf("a no-transaction k: %+v (replayed %q), roots %s", e, asked, classes(e))
	}

	// A failed post-commit UPDATE (H24): k is the next migration, and the
	// prefix includes the committed one.
	post := &verdict{Schema: "history", Outcomes: []outcome{{File: "1_a.sql", Status: "accepted", PostCommit: "25006 cannot execute UPDATE in a read-only transaction"}, {File: "2_b.sql", Status: "not_run"}},
		Tables: []catTable{tableT}}
	if e := run(two, post, noReplay); e.KFile != "2_b.sql" || e.KVerdict != "e8-hit" || e.HitState != "25006" || e.Prefix != "judged" || len(e.Roots) > 0 {
		t.Errorf("a post-commit stop: %+v, roots %s", e, classes(e))
	}

	// A stop of the prefix replay is a root of class stopped.
	broken := prefixFuncs{extract: extractTemp, replay: func(*oracleHistory) (*verdict, error) { return nil, fmt.Errorf("server gone") }}
	if e := run(noTx, stopNoTx, broken); e.Class != "stopped" || len(e.Roots) != 1 || e.Roots[0].Class != "stopped" {
		t.Errorf("a stopped prefix replay: %+v, roots %s", e, classes(e))
	}

	var tally e8Tally
	tally.add(&e8Judgement{Class: "refused", KVerdict: "e8-hit", HitClass: "R", HitState: "42703", Prefix: "judged"})
	tally.add(&e8Judgement{Class: "clean"})
	if got := tally.String(); !strings.Contains(got, "clean 1, refused 1 (refusal-agrees 0, may-fail 0, e8-hit 1), prefix judged 1") || !strings.Contains(got, "[R 42703: 1]") {
		t.Errorf("tally: %s", got)
	}
}

// TestHarnessRev21Postgres checks H25 and H28 against Postgres: concurrent
// role setups on one cluster do not fail, and the cleanup of roles a
// history created leaves no role and no note behind.
func TestHarnessRev21Postgres(t *testing.T) {
	o := sharedOracle(t)
	ctx := context.Background()
	errs := make(chan error, 4)
	for range 4 {
		go func() {
			var err error
			for range 5 {
				if err = withSetupLock(ctx, o.admin, func(db sqlRunner) error { return resetMigrator(ctx, db, o.password) }); err != nil {
					break
				}
			}
			errs <- err
		}()
	}
	for range 4 {
		if err := <-errs; err != nil {
			t.Errorf("concurrent role setup (H28): %v", err)
		}
	}
	// H29: a history that creates outside its schema (search_path names
	// a missing schema, then public) leaves nothing for the next run: the
	// same history replays the same verdict (g7-s1695's prefix replay).
	leak := &oracleHistory{ID: "rev21-h29", Files: []oracleFile{{Name: "1_a.sql", SQL: "SELECT set_config('search_path', 'nosuch_sp, public', false);\nCREATE TABLE gv21_pt (a int);\nRESET search_path;\n"}}}
	for i := range 2 {
		v, err := o.run(ctx, leak)
		if err != nil {
			t.Fatal(err)
		}
		if v.Outcomes[0].Status != "accepted" {
			t.Errorf("run %d of a history that created in public: %+v", i+1, v.Outcomes[0])
		}
	}
	h := &oracleHistory{ID: "rev21-h25", Files: []oracleFile{{Name: "1_a.sql", SQL: "CREATE ROLE gv21_h25;\nCREATE TABLE t (a int);\nGRANT SELECT ON t TO gv21_h25;\nALTER DEFAULT PRIVILEGES GRANT SELECT ON TABLES TO gv21_h25;\n"}}}
	v, err := o.run(ctx, h)
	if err != nil {
		t.Fatal(err)
	}
	if v.Outcomes[0].Status != "accepted" {
		t.Fatalf("the history was not applied: %+v", v.Outcomes[0])
	}
	if len(o.notes) > 0 {
		t.Errorf("role cleanup notes: %v", o.notes)
		o.notes = nil
	}
	roles, err := o.roleNames(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if roles["gv21_h25"] {
		t.Error("the history's role is left behind (H25)")
	}
}
