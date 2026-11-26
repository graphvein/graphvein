// Package schema extracts database schema facts from SQL migrations at link
// time (ADR 0047). T018a reads sqlx migration sets: it discovers them by
// convention from Cargo manifests, orders their files as sqlx does, applies
// the raw SQL under PostgreSQL semantics and emits the final schema as
// migration, table, column and db_index nodes with contains, alters and
// foreign_key edges.
//
// The pass reads only the link-time sources the scanner retained, plus a
// directory listing of each discovered set so that a member the scanner
// skipped is reported instead of silently shortening the set (Revision 1
// R5). It caches nothing. Whatever cannot be read exactly stays visible as
// a partial table, an opaque count or a diagnostic, never as a guess
// (VISION, principle 6).
package schema

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/testscope"
)

// Input carries the link-time sources (repository-relative path to
// LF-normalized bytes, as in the index), the repository root used to list
// set directories (empty disables the listing) and the test-scope rules.
type Input struct {
	Sources map[string][]byte
	Root    string
	Tests   testscope.Rules
}

// migration is one file of a set that sqlx treats as a migration.
type migration struct {
	file        string
	name        string
	source      []byte
	lines       []int
	identity    string
	versionText string
	version     *int64
	direction   string
	description string
	// applied is nil when sqlx's behaviour is undefined (duplicate versions).
	applied  *bool
	order    int
	analyzed bool
	counts   map[class]int
	// extensions holds CREATE EXTENSION names in order.
	extensions []string
	// opaqueSites are the statements counted in counts[classOpaque], in
	// file order (T018f).
	opaqueSites []opaqueSite
}

// migrationSet is one discovered migrations directory: a sqlx set, or a
// Supabase set (T024c1, supabase.go), whose cargo is its config.toml (or,
// for a bare directory, SB-M5, the migrations directory).
type migrationSet struct {
	// tool is toolSqlx or toolSupabase.
	tool       string
	dir, cargo string
	dialect    string
	// marker is whether the set's sqlx reads -- no-transaction (C4).
	marker markerReading
	// rows counts the claim-contract rows the set's statements were
	// analysed and applied under (B7, TestContractRows).
	rows       map[string]int
	migrations []*migration
	opaque     map[string]int
	unreadable []string
	// notUTF8 are unreadable members the pass could read and found not to
	// be UTF-8: sqlx refuses the whole set (P10).
	notUTF8 []string
	// unlisted is the error of a set directory listing that failed for a
	// reason other than absence (T018b1 review).
	unlisted string
	// rlsEnded is set once a statement of a Supabase set ran code the pass
	// cannot read or created an event trigger (row SB-M9, 2026-11-18
	// TOP-1): every held table's rls flags and policies stay unknown to
	// the end of the set (rlsEnd).
	rlsEnded     bool
	config       bool
	failed       bool
	diagnostics  *[]contract.Diagnostic
	tests        testscope.Rules
	tables       []*table
	sourceLookup map[string][]byte
	// skipped is the legacy first init file the Supabase CLI skips
	// (SB-M1); stoppedAt the member from which a Supabase set's applied
	// states are unknown because its version repeats an earlier one.
	skipped   []string
	stoppedAt string
	// supabaseRows counts the SB-M rows a Supabase set used
	// (TestSupabaseContract).
	supabaseRows map[string]int
	// bare marks a Supabase set read from a supabase/migrations directory
	// without config.toml (SB-M5, assumption SB-V3).
	bare bool
	// platformExtensions are the Supabase platform extensions the set's
	// statements create (SB-M4, assumption SB-V4), sorted.
	platformExtensions []string
	// platformInserts are the INSERT INTO storage.buckets statements the
	// set runs (SB-M7, assumption SB-V5), as written, in order.
	platformInserts []string
}

func (set *migrationSet) diagnostic(m *migration, at byteSpan, severity, code, message string) {
	file, location := set.cargo, ""
	if m != nil {
		file = m.file
	}
	if m != nil && at != (byteSpan{}) {
		line, _ := m.position(at.start)
		location = fmt.Sprintf(":%d: %s", line, describe(at.start, at.end))
	}
	*set.diagnostics = append(*set.diagnostics, contract.Diagnostic{Type: "diagnostic", File: file, Severity: severity, Code: code, Message: set.toolText(fmt.Sprintf("%s%s: %s", file, location, message))})
}

// position converts a byte offset to a 1-based line and byte column.
func (m *migration) position(offset int) (int, int) {
	if m.lines == nil {
		m.lines = []int{0}
		for i, b := range m.source {
			if b == '\n' {
				m.lines = append(m.lines, i+1)
			}
		}
	}
	line := sort.Search(len(m.lines), func(i int) bool { return m.lines[i] > offset }) - 1
	return line + 1, offset - m.lines[line]
}

func (m *migration) span(b byteSpan) contract.Span {
	sl, sc := m.position(b.start)
	el, ec := m.position(b.end)
	return contract.Span{StartLine: sl, StartColumn: sc, EndLine: el, EndColumn: ec}
}

// Link discovers sqlx sets and emits their schema records. A problem in one
// set or file is a diagnostic; the other sets continue, even when the pass
// fails unexpectedly on one set (AGENTS.md: one failure never stops others).
func Link(in Input) ([]contract.Node, []contract.Edge, []contract.Diagnostic) {
	var diagnostics []contract.Diagnostic
	e := newEmitter()
	sets := discover(in, &diagnostics)
	taken := map[string]bool{}
	for _, set := range sets {
		taken[set.dir] = true
	}
	sets = append(sets, supabaseDiscover(in, taken, &diagnostics)...)
	for _, set := range sets {
		before := len(diagnostics)
		se := newEmitter()
		if err := linkSet(set, se); err != nil {
			diagnostics = append(diagnostics[:before], contract.Diagnostic{Type: "diagnostic", File: set.cargo, Severity: "warning", Code: "schema_internal_error",
				Message: fmt.Sprintf("%s: the schema pass failed on %s set %s and emitted nothing for it: %v", set.cargo, set.tool, set.dir, err)})
			continue
		}
		e.merge(se)
	}
	nodes := make([]contract.Node, 0, len(e.nodes))
	for _, n := range e.nodes {
		nodes = append(nodes, n)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Identity < nodes[j].Identity })
	edges := make([]contract.Edge, 0, len(e.edges))
	for _, k := range e.order {
		edges = append(edges, *e.edges[k])
	}
	return nodes, edges, append(diagnostics, e.diagnostics...)
}

// readDir lists a set directory; tests replace it to inject a listing
// failure, which a root user and every platform cannot produce alike.
var readDir = os.ReadDir

// runSet orders, applies and emits one set; tests replace it to check the
// recover guard in linkSet.
var runSet = func(set *migrationSet, e *emitter) {
	set.run()
	e.emit(set)
}

// linkSet runs one set and turns a panic into an error, so that the other
// sets are still emitted.
func linkSet(set *migrationSet, e *emitter) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%v", r)
		}
	}()
	runSet(set, e)
	return nil
}

// sqlxName applies Q1: split at the first "_", the second part must end in
// ".sql". It returns the version text and the rest.
func sqlxName(name string) (string, string, bool) {
	version, rest, ok := strings.Cut(name, "_")
	if !ok || !strings.HasSuffix(rest, ".sql") {
		return "", "", false
	}
	return version, rest, true
}

func discover(in Input, diagnostics *[]contract.Diagnostic) []*migrationSet {
	manifests := map[string]manifest{}
	var paths []string
	for p, source := range in.Sources {
		if path.Base(p) == "Cargo.toml" {
			manifests[p] = readCargo(p, source)
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	var sets []*migrationSet
	for _, p := range paths {
		m := manifests[p]
		if !m.readable {
			// Whether this manifest declares a package with a sqlx set is
			// unknown, so "no set here" cannot be stated (T018b1 review).
			*diagnostics = append(*diagnostics, contract.Diagnostic{Type: "diagnostic", File: p, Severity: "warning", Code: "schema_manifest_unreadable",
				Message: fmt.Sprintf("%s: the manifest could not be read (%s); whether it declares a sqlx migration set is unknown", p, m.problem)})
			continue
		}
		if !m.pkg {
			continue
		}
		dep := readSqlx(m, findWorkspaceRoot(m, manifests))
		if !dep.found {
			continue
		}
		dir := path.Dir(p)
		migDir := path.Join(dir, "migrations")
		set := &migrationSet{tool: toolSqlx, dir: migDir, cargo: p, dialect: dep.dialect, marker: dep.marker, opaque: map[string]int{}, diagnostics: diagnostics, tests: in.Tests, sourceLookup: in.Sources}
		names := map[string]bool{}
		for file := range in.Sources {
			rest, ok := strings.CutPrefix(file, migDir+"/")
			if ok && !strings.Contains(rest, "/") {
				names[rest] = true
			}
		}
		if in.Root != "" {
			entries, err := readDir(filepath.Join(in.Root, filepath.FromSlash(migDir)))
			if err != nil && !errors.Is(err, fs.ErrNotExist) {
				// T018b1 review: a member the scanner skipped cannot be
				// reported, so the set is reported instead of shortened.
				set.unlisted = err.Error()
			}
			if err == nil {
				for _, entry := range entries {
					name := entry.Name()
					if names[name] {
						continue
					}
					if _, _, ok := sqlxName(name); !ok {
						continue
					}
					full := filepath.Join(in.Root, filepath.FromSlash(migDir), name)
					info, err := os.Stat(full)
					if err == nil && !info.Mode().IsRegular() {
						continue
					}
					// P10: sqlx reads every member with read_to_string
					// (following symlinks) before it applies any migration,
					// and refuses the whole set when one is not UTF-8
					// (source.rs 76-87, 118-124 at 0.8.6).
					if data, err := os.ReadFile(full); err == nil && !utf8.Valid(data) {
						set.notUTF8 = append(set.notUTF8, name)
					}
					set.unreadable = append(set.unreadable, name)
				}
			}
		}
		var members []string
		for name := range names {
			if _, _, ok := sqlxName(name); ok {
				members = append(members, name)
			}
		}
		if len(members) == 0 && len(set.unreadable) == 0 && set.unlisted == "" {
			continue
		}
		if dep.rootUnread != "" {
			// P10: the dialect depends on a manifest the pass cannot read, so
			// no record is emitted; the set is listed without records.
			*diagnostics = append(*diagnostics, contract.Diagnostic{Type: "diagnostic", File: p, Severity: "info", Code: "schema_unsupported_dialect",
				Message: fmt.Sprintf("%s: sqlx set %s: a dependency is inherited from the workspace (workspace = true) and %s, so whether it is sqlx and with which database feature is unknown; no records are emitted%s", p, migDir, dep.rootUnread, precondition("Q1.P10"))})
			continue
		}
		sort.Strings(members)
		sort.Strings(set.unreadable)
		for _, name := range members {
			file := migDir + "/" + name
			set.migrations = append(set.migrations, newMigration(file, name, in.Sources[file]))
		}
		if dep.dialect == "unknown" {
			set.diagnostic(nil, byteSpan{}, "info", "schema_unsupported_dialect", fmt.Sprintf("sqlx set %s: dialect unknown (%s); only migration records are emitted", migDir, dep.reason))
		} else if dep.dialect != "postgres" {
			set.diagnostic(nil, byteSpan{}, "info", "schema_unsupported_dialect", fmt.Sprintf("sqlx set %s: dialect %s is not modeled; only migration records are emitted", migDir, dep.dialect))
		}
		if _, ok := in.Sources[path.Join(dir, "sqlx.toml")]; ok {
			set.config = true
			*diagnostics = append(*diagnostics, contract.Diagnostic{Type: "diagnostic", File: path.Join(dir, "sqlx.toml"), Severity: "info", Code: "schema_unsupported_config", Message: fmt.Sprintf("%s: sqlx.toml is not read; it may change the migration settings of %s, whose tables are partial", path.Join(dir, "sqlx.toml"), migDir)})
		}
		if set.unlisted != "" {
			set.diagnostic(nil, byteSpan{}, "info", "schema_member_unreadable", fmt.Sprintf("sqlx set %s: the directory could not be listed (%s), so a member the scanner skipped cannot be reported; every table of the set is partial", migDir, set.unlisted))
		}
		for _, name := range set.unreadable {
			if slices.Contains(set.notUTF8, name) {
				set.diagnostic(nil, byteSpan{}, "warning", "schema_member_unreadable", fmt.Sprintf("sqlx set %s: member %s is not UTF-8 text; sqlx reads every member before it applies any and refuses the whole set, so no migration is applied%s", migDir, name, precondition("Q1.P10")))
				continue
			}
			set.diagnostic(nil, byteSpan{}, "info", "schema_member_unreadable", fmt.Sprintf("sqlx set %s: member %s is not readable as UTF-8 text by the scanner (symlink, binary, ignored or unreadable); sqlx may apply it or refuse the whole set, so whether any migration is applied is unknown, and every table of the set is partial%s", migDir, name, precondition("Q1.P10")))
		}
		sets = append(sets, set)
	}
	return sets
}

func newMigration(file, name string, source []byte) *migration {
	versionText, rest, _ := sqlxName(name)
	m := &migration{file: file, name: name, source: source, versionText: versionText, direction: "simple"}
	m.identity, _ = contract.Identity("migration", file, "migration")
	suffix := ".sql"
	switch {
	case strings.HasSuffix(rest, ".up.sql"):
		m.direction, suffix = "up", ".up.sql"
	case strings.HasSuffix(rest, ".down.sql"):
		m.direction, suffix = "down", ".down.sql"
	}
	// trim_end_matches removes every trailing repetition of the suffix.
	for strings.HasSuffix(rest, suffix) {
		rest = strings.TrimSuffix(rest, suffix)
	}
	m.description = strings.ReplaceAll(rest, "_", " ")
	// Rust's i64 FromStr accepts an optional sign and leading zeros, like
	// strconv.ParseInt with base 10.
	if v, err := strconv.ParseInt(versionText, 10, 64); err == nil {
		m.version = &v
	}
	return m
}

// unreadMembers applies P10 (Q1): when a member sqlx reads could not be
// read by the pass, or the directory listing failed, sqlx may apply that
// member or refuse the whole set, so no migration's applied state is
// known.
func (set *migrationSet) unreadMembers() {
	// A Supabase set stops at its first unreadable member instead
	// (orderSupabase); an unlisted directory leaves every member unknown.
	if len(set.unreadable) == 0 && set.unlisted == "" || set.failed || set.tool == toolSupabase && set.unlisted == "" {
		return
	}
	for _, m := range set.migrations {
		if m.applied != nil && *m.applied {
			m.applied = nil
		}
	}
}

// order applies Q1-Q3 and Q4's skipping of down migrations.
func (set *migrationSet) order() {
	f := false
	for _, m := range set.migrations {
		if m.version == nil {
			set.failed = true
			set.diagnostic(m, byteSpan{}, "warning", "schema_migration_order", fmt.Sprintf("version %q is not an integer; sqlx refuses the whole set, nothing is applied", m.versionText))
		}
	}
	if len(set.notUTF8) > 0 {
		set.failed = true
	}
	for _, name := range set.unreadable {
		version, _, _ := sqlxName(name)
		if _, err := strconv.ParseInt(version, 10, 64); err != nil {
			set.failed = true
			set.diagnostic(nil, byteSpan{}, "warning", "schema_migration_order", fmt.Sprintf("member %s has a version that is not an integer; sqlx refuses the whole set", name))
		}
	}
	if set.failed {
		for _, m := range set.migrations {
			m.applied = &f
		}
		return
	}
	sort.SliceStable(set.migrations, func(i, j int) bool { return *set.migrations[i].version < *set.migrations[j].version })
	seen := map[int64]int{}
	for _, m := range set.migrations {
		if m.direction != "down" {
			seen[*m.version]++
		}
	}
	var duplicate *int64
	for _, m := range set.migrations {
		if m.direction != "down" && seen[*m.version] > 1 {
			if duplicate == nil || *m.version < *duplicate {
				v := *m.version
				duplicate = &v
			}
			set.diagnostic(m, byteSpan{}, "warning", "schema_migration_order", fmt.Sprintf("version %d is used by more than one migration; their order is undefined and sqlx fails on the second, so no schema is derived from the set", *m.version))
		}
	}
	if duplicate != nil {
		set.failed = true
	}
	t := true
	order := 0
	for _, m := range set.migrations {
		switch {
		case m.direction == "down":
			m.applied = &f
		case duplicate != nil && *m.version >= *duplicate:
			m.applied = nil
		default:
			order++
			m.applied, m.order = &t, order
		}
	}
}

// run orders the set and, for Postgres sets that sqlx would apply, applies
// every applied migration to a fresh schema state.
func (set *migrationSet) run() {
	defer set.unreadMembers()
	// Q1: the set's members as sqlx reads them decide every applied state.
	set.rows = map[string]int{"Q1": 1}
	if set.tool == toolSupabase {
		set.orderSupabase()
	} else {
		set.order()
	}
	if set.failed || set.dialect != "postgres" {
		return
	}
	type lexed struct {
		m          *migration
		statements []statement
		failure    *lexFailure
	}
	var files []lexed
	functions := map[string]bool{}
	defines := false
	pkDependents := false
	// SB-M6 (T024c1b R1): whether a statement of the set or of its code
	// creates an operator, cast, operator class or family, access method
	// or aggregate, whose names the extensions' object lists do not cover.
	operatorObjects := false
	// SB-M7: whether a statement of the set or of its code may make a
	// trigger or rule fire on storage.buckets (it names buckets other
	// than as INSERT INTO storage.buckets, or runs dynamic EXECUTE).
	bucketCode := false
	// SB-M9 (NX-S): whether code of the set (a DO block or routine body)
	// creates a rule, whose actions the pass does not read.
	ruleCode, eventCode := false, false
	// P1 (T018a2 Revision 1): every name the set creates anywhere.
	names := newNameSet()
	for _, m := range set.migrations {
		if m.applied == nil || !*m.applied {
			continue
		}
		statements, failure := lexStatements(m.source)
		files = append(files, lexed{m, statements, failure})
		for _, st := range statements {
			a := analyze(st, m.source, nil, false)
			names.collect(namesOf(a, st.tokens))
			if a.function != nil {
				functions[*a.function] = true
				defines = true
			}
			if a.trigger {
				defines = true
			}
			if pkDependency(st.tokens) {
				pkDependents = true
			}
			if createsOperatorObject(st.tokens) {
				operatorObjects = true
			}
			if mayTouchBuckets(st.tokens, m.source) {
				bucketCode = true
			}
			// P12 (U2): names, routines and triggers that code in DO
			// blocks and routine bodies may create are the set's too.
			codeStatements(st, m.source, 0, func(cst statement, src []byte) {
				ca := analyze(cst, src, nil, false)
				names.collect(namesOf(ca, cst.tokens))
				if ca.function != nil {
					functions[*ca.function] = true
					defines = true
				}
				if ca.trigger || slices.ContainsFunc(cst.tokens, func(t token) bool { return t.kind == tokIdent && t.value == "trigger" }) {
					defines = true
				}
				if pkDependency(cst.tokens) {
					pkDependents = true
				}
				if createsOperatorObject(cst.tokens) {
					operatorObjects = true
				}
				if mayTouchBuckets(cst.tokens, src) {
					bucketCode = true
				}
				if createsRule(cst.tokens) {
					ruleCode = true
				}
				if ca.opaqueKind == "event_trigger" {
					eventCode = true
				}
			})
		}
	}
	s := newState(set)
	s.names = names
	s.pkDependents = pkDependents
	s.operatorObjects = operatorObjects
	s.bucketCode = bucketCode
	s.rlsRuled = ruleCode
	if set.tool == toolSupabase && eventCode {
		// SB-M9 (TOP-1 of 2026-11-18): an event trigger code of the set
		// creates may run on any DDL statement, from the set's start.
		s.rlsEnd()
	}
	for _, f := range files {
		m := f.m
		m.analyzed, m.counts, m.extensions = true, map[class]int{}, []string{}
		s.begin(m)
		tx := newTxTracker(m, len(f.statements), set.marker)
		if set.tool == toolSupabase {
			tx = supabaseTracker()
		}
		s.tx = tx
		s.rev19bBegin(tx)
		s.pathMode = migrationPathMode(tx, f.statements)
		m.position(0) // line offsets of the file's own text (O1t swaps it)
		for _, st := range f.statements {
			if statementHook != nil {
				statementHook(s, false)
			}
			s.at = byteSpan{st.start, st.end}
			source := m.source
			// W2 (Revision 1.7): string arguments of reg* parameters, read
			// from the statement as written (before O1t swaps it).
			s.regArguments(st.tokens, m.source)
			swapped := false
			if text, ok := s.o1t.call(st.tokens, m.source); ok {
				swapped = true
				// O1t (B2): the call runs exactly this CREATE TRIGGER.
				sub, _ := lexStatements([]byte(text))
				m.source, st = []byte(text), sub[0]
				set.rows["O1t"]++
			}
			a := analyze(st, m.source, functions, defines)
			if set.tool == toolSupabase {
				// SB-M6: a platform extension's known effect.
				s.platformExtensionEffect(st.tokens, &a)
				// SB-M7: an insert into storage.buckets no trigger of the
				// set fires on.
				s.platformInsertEffect(st.tokens, &a)
				// SB-M9: a data statement whose code cannot change row
				// level security.
				a.rlsInert = s.rlsInertData(st, m.source, a, functions)
				// SB-M9 (T024 review): a data statement on a relation
				// the set does not hold.
				s.rlsForeignData(st.tokens, m.source)
				// SB-M9 (NX-S): a rule's actions are not read.
				s.rlsRuleStatement(st.tokens)
				// SB-M9 (TOP-1 of 2026-11-18): an event trigger runs code on
				// every later DDL statement.
				if a.opaqueKind == "event_trigger" {
					s.rlsEnd()
				}
				// SB-M10: a statement that may alter or drop a platform
				// type.
				s.platformTypeStatement(st.tokens)
			}
			if a.class == classError {
				// SB-M9: an unread statement may have created code.
				s.rlsBlocked = true
			}
			rows := statementRows(st.tokens, a)
			for _, r := range rows {
				set.rows[r]++
			}
			if swapped {
				rows = append([]string{"O1t"}, rows...)
			}
			s.row = rows[0]
			before := s.factPrints()
			s.inBlock = tx.inBlock()
			txWhy, txID := tx.step(st.tokens, m.source)
			m.counts[a.class]++
			if a.class == classOpaque {
				m.opaqueSites = append(m.opaqueSites, opaqueSite{at: s.at, row: a.row, kind: a.opaqueKind})
			}
			if a.extension != nil {
				m.extensions = append(m.extensions, *a.extension)
			}
			switch a.class {
			case classUnsupported:
				set.diagnostic(m, s.at, "info", "schema_unsupported_statement", a.reason)
			case classOpaque:
				set.diagnostic(m, s.at, "info", "schema_opaque_statement", a.reason)
			case classError:
				set.diagnostic(m, s.at, "warning", "schema_parse_error", a.reason)
			}
			if a.runsFunction != "" {
				set.diagnostic(m, s.at, "info", "schema_opaque_statement", fmt.Sprintf("runs %s, a function defined in the migration set, on existing rows", a.runsFunction))
			}
			s.unsafeUse(st, a)
			// U2 (P12): names and dependents code may have changed.
			s.codeConsumers(st, a)
			// X1, X2, X4 (Revision 1.8): names a CASCADE or a collation
			// rename removed, a schema of an unknown name.
			s.rev18Before(st, a)
			// U3: unqualified creations under the session's search_path,
			// and statements that may move the bookkeeping table.
			s.placeCreation(&a)
			s.bookkeepingStatement(st.tokens)
			sn := namesOf(a, st.tokens)
			if a.sequence != nil {
				// U5: the options, before the state records the sequence.
				s.sequenceStatement(a.sequence)
			}
			// V4 (Revision 1.6): objects of kinds E1 does not cover, before
			// the statement changes what the pass holds.
			s.objectKinds(st.tokens)
			// W4, W5 (Revision 1.7): databases, drops of kinds not held.
			s.rev17Objects(st.tokens)
			// U-a (Revision 1.9): statement validators.
			s.rev19Before(st, a)
			// U-b (Revision 1.9): state records.
			s.rev19bBefore(st, a)
			// U-c (Revision 1.9): superuser-only statements, code run from
			// constants.
			s.rev19cBefore(st, a)
			s.viewOptions(st.tokens)
			if exactNames(a) && (a.create == nil || a.create.unmodeled == "") {
				s.apply(a)
			} else {
				s.tokenRule(st, sn)
				// Revision 1.2: column references are resolved when the
				// statement is analysed, before it runs (and before an
				// opaque data change's own effect).
				s.columnReferences(st.tokens, a)
				s.apply(a)
			}
			s.statementChecks(st, a)
			// X1 (Revision 1.8): dependents of a dropped object, by the
			// pass's dependency record.
			s.rev18After(st, a)
			s.rev19After(st, a)
			s.rev19bAfter(st, a)
			s.rev19cAfter(st, a)
			s.sequenceAfter(a)
			s.rowWrites(st, a)
			s.o1t.track(st.tokens, m.source, a)
			s.ledger.apply(sn)
			if conditionalCreate(st.tokens) {
				s.codeAfter(sn)
			}
			if a.class == classOpaque && o1Kinds[a.opaqueKind] || a.runsFunction != "" {
				// U2 (P12): what the code the statement runs may do.
				inv := s.inventory(st, m.source, a, functions)
				// SB-M9 (T024 review): code that writes a relation the
				// set does not hold.
				s.rlsForeignCode(inv)
				s.codeEffects(inv)
			}
			s.recordRoutine(st.tokens, m.source)
			if txWhy != "" {
				// P6: refused or undone in this block state, or the outcome
				// depends on whether sqlx reads -- no-transaction.
				s.unknownFact(txWhy + precondition(txID))
			}
			s.recordClaims(before, rows)
			s.row = ""
			if statementHook != nil {
				statementHook(s, true)
			}
			m.source = source
		}
		// Y22 (Revision 1.9): a lexer error anywhere refuses the whole
		// migration.
		s.rev19Junk(f.statements)
		if why := tx.finish(); why != "" && len(f.statements) > 0 {
			last := f.statements[len(f.statements)-1]
			s.at = byteSpan{last.start, last.end}
			s.unknownFact(why + precondition("T1.P6"))
		}
		if len(f.statements) > 0 {
			last := f.statements[len(f.statements)-1]
			s.at = byteSpan{last.start, last.end}
		}
		// Y03 (Revision 1.9): sqlx's bookkeeping in a read-only
		// transaction.
		s.rev19bEnd(tx)
		s.endPath()
		if f.failure != nil {
			s.at = byteSpan{f.failure.start, len(m.source)}
			m.counts[classError]++
			set.diagnostic(m, s.at, "warning", "schema_parse_error", f.failure.reason+"; the rest of the file is one opaque region")
			s.opaque("lexing_failure")
			if set.tool == toolSupabase {
				// SB-M9 (TOP-1 of 2026-11-18): the unread rest may create a
				// rule or an event trigger.
				s.rlsEnd()
			}
			s.row = "D" // the unlexed rest of the file is the default branch
			s.mayFail("the file could not be lexed: " + f.failure.reason)
			s.row = ""
		}
	}
	s.m, s.at = nil, byteSpan{}
	if s.failure != nil {
		// Revision 2 WF8: whether the failing migration and the later ones
		// were applied is unknown; their sqlx order is still defined.
		for _, m := range set.migrations {
			if m.applied != nil && *m.applied && m.order >= s.failure.m.order {
				m.applied = nil
			}
		}
	}
	for _, t := range s.tables {
		if set.unlisted != "" {
			t.reasons = append(t.reasons, reason{text: "the set directory could not be listed (" + set.unlisted + "); a member may be missing"})
		}
		if len(set.unreadable) > 0 {
			t.reasons = append(t.reasons, reason{text: "a member of the set is not readable (" + strings.Join(set.unreadable, ", ") + "); sqlx may apply it or refuse the whole set"})
		}
		if set.config {
			t.reasons = append(t.reasons, reason{text: "sqlx.toml is not read; it may change the migration settings"})
		}
		if set.stoppedAt != "" {
			t.reasons = append(t.reasons, reason{text: "migration " + set.stoppedAt + " repeats an earlier version; whether it and the later migrations are applied is unknown"})
		}
	}
	set.tables = s.tables
}
