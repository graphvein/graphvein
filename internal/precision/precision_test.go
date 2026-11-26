package precision_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/precision"
)

// pin records the rule table's digest at an extractor version. The list is
// append-only: a rule change adds a pin at the bumped index.Version.
type pin struct {
	version string
	digest  string
}

// pins is the digest history of the rule table (P6). The first entry is
// the initial table of T020, which changes no label, so it is
// pinned at the version current when it landed.
var pins = []pin{
	{"extractors-37", "sha256:229fbfa737fd72db33f7b547bd5ea84cf1c53497174c0a38b2379f060b1a6581"},
	// T020: row PL-L1 (Go structure).
	{"extractors-39", "sha256:3e7e52754505c706530c2a295f9612129755370ec8257a1665aae5a65d88fe33"},
	// T020: row PL-L2 (Python structure).
	{"extractors-40", "sha256:a11d204995afd35248903eb903a36f31a7cea9ccd1c70a32aee535ca645763a5"},
	// T020: row PL-L3 (TypeScript, TSX, JavaScript structure),
	// with row PL-L2 merged in from main.
	{"extractors-41", "sha256:45ad31f47d57e40aa7c8f5c3937cb1dd62682056d6e155fd19e2e05d5d393559"},
	// T020: row PL-L4 (Rust structure), with rows PL-L2 and PL-L3
	// merged in from main.
	{"extractors-42", "sha256:c6ca6af3d2a93c17dc7926a0d590e69fe911343e81096193c4b03f3bc1115d76"},
	// T023 GO4: row PL-F10 (Go HTTP routes), scope narrowed
	// to the oracle-compared facts before extractors-46 merged.
	{"extractors-46", "sha256:c5cb19bdb9e0012fd6be3cb53f6bed07a151a5fdcc9d4eb0fff48ac463f91b71"},
	// Stacked gate of T024 on T023, T022 and T026: T024c2's row PL-F9
	// covers links into tables of the sqlx, sqlmodel, prisma and drizzle
	// groups only (Supabase links stay best_effort), with T023's PL-F10.
	{"extractors-49", "sha256:fbd9f2f07f09f48c97d49ebd862564b2f309d066810c891395d2b58010a6c18e"},
	// Wording only: row PL-F1's ADR 0047 citation reads "Nine reviews"
	// (public-launch wording); no rule, scope or claim changed.
	{"extractors-62", "sha256:6d420ecb06ddb308cdf8f32961306089c5876ea0fcc75f47716d49f4f511c147"},
}

// root is the module root, two levels above this package.
var root = filepath.Join("..", "..")

// TestPrecisionRules checks every row's evidence (P1-P6 of
// docs/contract/claims/precision-labels.md), keeps the Go table 1:1 with
// docs/extract/precision.md and with the task file, and rejects rows for
// the T021 best-effort tier. The task file is a record of T020: a file row
// added after it was written (PL-L2 onwards) must be one of its planned
// rows with the same scope, and the rows it does hold stay 1:1.
func TestPrecisionRules(t *testing.T) {
	rules := precision.Rules()
	if len(rules) == 0 {
		t.Fatal("rule table is empty")
	}
	t.Run("shape", func(t *testing.T) { checkShape(t, rules) })
	t.Run("t021_guard", func(t *testing.T) { checkT021Guard(t, rules) })
	t.Run("evidence", func(t *testing.T) { checkEvidence(t, rules) })
	t.Run("version_digest", func(t *testing.T) { checkDigest(t) })
	t.Run("one_to_one/precision.md", func(t *testing.T) { checkDocTable(t, rules, "docs/extract/precision.md") })
	t.Run("one_to_one/claims-precision-labels.md", func(t *testing.T) {
		const record = "docs/contract/claims/precision-labels.md"
		checkDocTable(t, recordedRules(t, rules, record), record)
	})
}

// recordedRules returns the rows of the Go table the task file's rule table
// holds. Every other row must be a file row the task file lists among its
// planned rows, with the same scope: the record is not edited after the
// fact, but a row it never planned still fails.
func recordedRules(t *testing.T, rules []precision.Rule, path string) []precision.Rule {
	rows, err := readDocTable(path)
	if err != nil {
		t.Fatal(err)
	}
	inTable := map[string]bool{}
	for _, row := range rows {
		inTable[row.id] = true
	}
	planned, err := readPlannedRows(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []precision.Rule
	for _, r := range rules {
		if inTable[r.ID] {
			out = append(out, r)
			continue
		}
		if r.Kind == precision.KindFamily && r.Task != "" {
			// A family row added by a later task (T023's PL-F10) is
			// planned in that task's file; the record does not list it.
			continue
		}
		scope, ok := planned[r.ID]
		switch {
		case !ok:
			t.Errorf("%s: row %s is neither in the rule table nor a planned row", path, r.ID)
		case r.Kind != precision.KindFile:
			t.Errorf("%s: row %s is planned but is not a file row", path, r.ID)
		case scope != r.Scope:
			t.Errorf("%s: planned row %s has scope %q, Go %q", path, r.ID, scope, r.Scope)
		}
	}
	return out
}

// readPlannedRows returns row ID to scope of the table whose header starts
// with "| row | scope | earns through".
func readPlannedRows(path string) (map[string]string, error) {
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	out := map[string]string{}
	in := false
	for _, line := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
		if strings.HasPrefix(line, "| row | scope | earns through") {
			in = true
			continue
		}
		if !in {
			continue
		}
		if !strings.HasPrefix(line, "|") {
			break
		}
		if strings.HasPrefix(line, "|---") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		if len(cells) < 2 {
			return nil, fmt.Errorf("%s: planned row has %d cells: %s", path, len(cells), line)
		}
		out[strings.TrimSpace(cells[0])] = strings.TrimSpace(cells[1])
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s has no planned-rows table", path)
	}
	return out, nil
}

func checkShape(t *testing.T, rules []precision.Rule) {
	seen := map[string]bool{}
	for _, r := range rules {
		if seen[r.ID] {
			t.Errorf("duplicate row %s", r.ID)
		}
		seen[r.ID] = true
		if r.ID == precision.DefaultRow {
			t.Errorf("row %s: the default branch is not a table row", r.ID)
		}
		if r.Scope == "" || r.Marker == "" || r.Refusal == "" {
			t.Errorf("row %s: scope, marker and refusal must be stated", r.ID)
		}
		switch r.Kind {
		case precision.KindFamily:
			if !strings.HasPrefix(r.ID, "PL-F") || r.Match.Family == "" || len(r.Languages) > 0 {
				t.Errorf("row %s: a family row is PL-F*, names a family and no language", r.ID)
			}
			if precision.NeverPrecise(r.Match.Family) {
				t.Errorf("row %s names %s, which is never precise (docs/extract/precision.md \"Cross-repository links\")", r.ID, r.Match.Family)
			}
		case precision.KindFile:
			if !strings.HasPrefix(r.ID, "PL-L") || r.Match.Family != "" || len(r.Languages) == 0 {
				t.Errorf("row %s: a file row is PL-L*, names languages and no family", r.ID)
			}
		default:
			t.Errorf("row %s: unknown kind %q", r.ID, r.Kind)
		}
	}
}

// t021Languages are the T021 best-effort tier: no row may name them in v1.
var t021Languages = []string{"java", "c_sharp", "dart"}

func checkT021Guard(t *testing.T, rules []precision.Rule) {
	word := regexp.MustCompile(`(?i)(?:^|[^a-z0-9_])(java|c_sharp|c#|dart)(?:[^a-z0-9_]|$)`)
	for _, r := range rules {
		for _, l := range r.Languages {
			for _, banned := range t021Languages {
				if strings.EqualFold(l, banned) {
					t.Errorf("row %s names %s: the T021 tier stays best_effort in v1", r.ID, l)
				}
			}
		}
		text := strings.Join(append(append([]string{r.Scope, r.Marker}, r.Match.Tools...), r.Match.Dialects...), " ")
		if m := word.FindStringSubmatch(text); m != nil {
			t.Errorf("row %s mentions %q: the T021 tier stays best_effort in v1", r.ID, m[1])
		}
	}
	for _, l := range t021Languages {
		if got := precision.FileLabel(l); got != string(precision.BestEffort) {
			t.Errorf("FileLabel(%q) = %q, want best_effort", l, got)
		}
	}
}

func checkEvidence(t *testing.T, rules []precision.Rule) {
	funcs := map[string]map[string]bool{}
	testFuncs := func(pkg string) map[string]bool {
		if f, ok := funcs[pkg]; ok {
			return f
		}
		f, err := testFunctions(filepath.Join(root, filepath.FromSlash(pkg)))
		if err != nil {
			t.Errorf("reading tests of %s: %v", pkg, err)
		}
		funcs[pkg] = f
		return f
	}
	for _, r := range rules {
		// P1: claim-contract tests.
		if len(r.Contract) == 0 {
			t.Errorf("row %s: P1 names no contract test", r.ID)
		}
		for _, ref := range r.Contract {
			if !testFuncs(ref.Package)[ref.Name] {
				t.Errorf("row %s: P1 contract test %s.%s not found", r.ID, ref.Package, ref.Name)
			}
		}
		// P2: goldens.
		if len(r.Goldens) == 0 {
			t.Errorf("row %s: P2 names no golden", r.ID)
		}
		for _, g := range r.Goldens {
			if !goldenExists(g) {
				t.Errorf("row %s: P2 golden %s.golden.json not found under testdata/fixtures or testdata/repos", r.ID, g)
			}
		}
		// P3 and P4: citations in ADR review notes.
		for _, part := range []struct {
			name  string
			cites []precision.Citation
		}{{"P3 corpus", r.Corpus}, {"P4 independent review", r.Review}} {
			if len(part.cites) == 0 {
				t.Errorf("row %s: %s cites nothing", r.ID, part.name)
			}
			for _, c := range part.cites {
				notes, err := reviewNotes(c.ADR)
				if err != nil {
					t.Errorf("row %s: %s: %v", r.ID, part.name, err)
					continue
				}
				if !strings.Contains(notes, normalise(c.Quote)) {
					t.Errorf("row %s: %s quote not in ADR %s review notes: %q", r.ID, part.name, c.ADR, c.Quote)
				}
			}
		}
		// P5: oracle, or a stated reason none is cheap.
		if len(r.Oracle) == 0 && strings.TrimSpace(r.OracleNone) == "" {
			t.Errorf("row %s: P5 names no oracle and no reason", r.ID)
		}
		if len(r.Oracle) > 0 && r.OracleNone != "" {
			t.Errorf("row %s: P5 names an oracle and a reason for none", r.ID)
		}
		for _, ref := range r.Oracle {
			if !testFuncs(ref.Package)[ref.Name] {
				t.Errorf("row %s: P5 oracle test %s.%s not found", r.ID, ref.Package, ref.Name)
			}
		}
	}
}

// testFunctions returns the top-level functions declared in a directory's
// test files.
func testFunctions(dir string) (map[string]bool, error) {
	names, err := filepath.Glob(filepath.Join(dir, "*_test.go"))
	if err != nil {
		return nil, fmt.Errorf("listing %s: %w", dir, err)
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("no test files in %s", dir)
	}
	out := map[string]bool{}
	fset := token.NewFileSet()
	for _, name := range names {
		f, err := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", name, err)
		}
		for _, d := range f.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok && fn.Recv == nil {
				out[fn.Name.Name] = true
			}
		}
	}
	return out, nil
}

func goldenExists(name string) bool {
	for _, dir := range []string{"testdata/fixtures", "testdata/repos"} {
		if info, err := os.Stat(filepath.Join(root, filepath.FromSlash(dir), name+".golden.json")); err == nil && info.Mode().IsRegular() {
			return true
		}
	}
	return false
}

var whitespace = regexp.MustCompile(`\s+`)

// normalise collapses whitespace and drops Markdown bold markers, so a quote
// matches across the review notes' line wrapping.
func normalise(s string) string {
	return strings.TrimSpace(whitespace.ReplaceAllString(strings.ReplaceAll(s, "**", ""), " "))
}

// reviewNotes returns the normalised "## Review notes" section of an ADR.
func reviewNotes(number string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(root, "docs", "adr", number+"-*.md"))
	if err != nil || len(matches) != 1 {
		return "", fmt.Errorf("ADR %s: want one file, found %d (%v)", number, len(matches), err)
	}
	raw, err := os.ReadFile(matches[0])
	if err != nil {
		return "", fmt.Errorf("reading ADR %s: %w", number, err)
	}
	var section []string
	in := false
	for _, line := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
		if strings.HasPrefix(line, "## ") {
			in = strings.HasPrefix(line, "## Review notes")
			continue
		}
		if in {
			section = append(section, line)
		}
	}
	if len(section) == 0 {
		return "", fmt.Errorf("ADR %s has no Review notes section", number)
	}
	return normalise(strings.Join(section, "\n")), nil
}

// checkDigest is P6: the table's digest is pinned next to index.Version.
func checkDigest(t *testing.T) {
	current, err := extractorNumber(index.Version)
	if err != nil {
		t.Fatal(err)
	}
	last := -1
	for _, p := range pins {
		n, err := extractorNumber(p.version)
		if err != nil {
			t.Fatal(err)
		}
		if n <= last {
			t.Errorf("pin %s: pins must have strictly increasing versions", p.version)
		}
		last = n
	}
	latest := pins[len(pins)-1]
	if last > current {
		t.Errorf("latest pin %s is newer than index.Version %s", latest.version, index.Version)
	}
	got := precision.Digest()
	if got == latest.digest {
		return
	}
	if latest.version == index.Version {
		t.Errorf("the rule table changed without an index.Version bump (digest %s, pinned %s at %s): bump index.Version, then append a pin", got, latest.digest, latest.version)
		return
	}
	t.Errorf("the rule table changed: append pin {%q, %q} (index.Version is newer than the latest pin %s)", index.Version, got, latest.version)
}

func extractorNumber(v string) (int, error) {
	n, err := strconv.Atoi(strings.TrimPrefix(v, "extractors-"))
	if err != nil || !strings.HasPrefix(v, "extractors-") {
		return 0, fmt.Errorf("extractor version %q is not extractors-<n>", v)
	}
	return n, nil
}

// docRow is one row of a Markdown rule table, cells by column.
type docRow struct {
	id, scope, p1, p2, p3, p4, p5 string
}

var (
	backticked = regexp.MustCompile("`([^`]+)`")
	adrNumber  = regexp.MustCompile(`ADR (\d{4})`)
)

// readDocTable returns the rows of the table whose header starts with
// "| row | scope | marker".
func readDocTable(path string) ([]docRow, error) {
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var rows []docRow
	in, found := false, false
	for _, line := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
		if strings.HasPrefix(line, "| row | scope | marker") {
			if found {
				return nil, fmt.Errorf("%s has two rule tables", path)
			}
			in, found = true, true
			continue
		}
		if !in {
			continue
		}
		if !strings.HasPrefix(line, "|") {
			in = false
			continue
		}
		if strings.HasPrefix(line, "|---") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		if len(cells) != 9 {
			return nil, fmt.Errorf("%s: rule table row has %d cells, want 9: %s", path, len(cells), line)
		}
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		rows = append(rows, docRow{id: cells[0], scope: cells[1], p1: cells[3], p2: cells[4], p3: cells[5], p4: cells[6], p5: cells[7]})
	}
	if !found {
		return nil, fmt.Errorf("%s has no rule table", path)
	}
	return rows, nil
}

func tokens(cell string) []string {
	var out []string
	for _, m := range backticked.FindAllStringSubmatch(cell, -1) {
		out = append(out, m[1])
	}
	return out
}

func sorted(set map[string]bool) []string {
	var out []string
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func equalSets(a, b map[string]bool) bool {
	return strings.Join(sorted(a), ",") == strings.Join(sorted(b), ",")
}

// checkDocTable keeps a document's rule table 1:1 with the Go table: the
// same row IDs in the same order, the same scopes and the same evidence
// names. A P2 cell may abbreviate a golden as "-suffix", which reuses the
// first golden's prefix up to its last hyphen ("table-access-sqlmodel",
// "-prisma").
func checkDocTable(t *testing.T, rules []precision.Rule, path string) {
	rows, err := readDocTable(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 || rows[len(rows)-1].id != precision.DefaultRow || rows[len(rows)-1].scope != "everything else" {
		t.Errorf("%s: the last row must be the default branch D, \"everything else\"", path)
	}
	var ids, want []string
	for _, row := range rows {
		ids = append(ids, row.id)
	}
	for _, r := range rules {
		want = append(want, r.ID)
	}
	want = append(want, precision.DefaultRow)
	if strings.Join(ids, ",") != strings.Join(want, ",") {
		t.Fatalf("%s: rows %v, Go table %v", path, ids, want)
	}
	for i, r := range rules {
		row := rows[i]
		if row.scope != r.Scope {
			t.Errorf("%s %s: scope %q, Go %q", path, r.ID, row.scope, r.Scope)
		}
		// P1: test names equal; every token that is not a test names a
		// contract package by its path or last element.
		docTests, goTests, packages := map[string]bool{}, map[string]bool{}, map[string]bool{}
		for _, ref := range r.Contract {
			goTests[ref.Name] = true
			packages[ref.Package] = true
		}
		named := map[string]bool{}
		for _, tok := range tokens(row.p1) {
			if strings.HasPrefix(tok, "Test") {
				docTests[tok] = true
				continue
			}
			ok := false
			for pkg := range packages {
				if tok == pkg || tok == pkg[strings.LastIndex(pkg, "/")+1:] {
					named[pkg], ok = true, true
				}
			}
			if !ok {
				t.Errorf("%s %s: P1 token %q names no contract package of the Go row", path, r.ID, tok)
			}
		}
		if !equalSets(docTests, goTests) || !equalSets(named, packages) {
			t.Errorf("%s %s: P1 names tests %v in %v, Go %v in %v", path, r.ID, sorted(docTests), sorted(named), sorted(goTests), sorted(packages))
		}
		// P2: goldens.
		docGoldens, goGoldens := map[string]bool{}, map[string]bool{}
		for _, g := range r.Goldens {
			goGoldens[g] = true
		}
		prefix := ""
		for i, tok := range tokens(row.p2) {
			if i == 0 && strings.Contains(tok, "-") {
				prefix = tok[:strings.LastIndex(tok, "-")]
			}
			if strings.HasPrefix(tok, "-") {
				tok = prefix + tok
			}
			docGoldens[tok] = true
		}
		if !equalSets(docGoldens, goGoldens) {
			t.Errorf("%s %s: P2 goldens %v, Go %v", path, r.ID, sorted(docGoldens), sorted(goGoldens))
		}
		// P3 and P4: the cited ADRs.
		for _, part := range []struct {
			name  string
			cell  string
			cites []precision.Citation
		}{{"P3", row.p3, r.Corpus}, {"P4", row.p4, r.Review}} {
			docADRs, goADRs := map[string]bool{}, map[string]bool{}
			for _, m := range adrNumber.FindAllStringSubmatch(part.cell, -1) {
				docADRs[m[1]] = true
			}
			for _, c := range part.cites {
				goADRs[c.ADR] = true
			}
			if !equalSets(docADRs, goADRs) {
				t.Errorf("%s %s: %s ADRs %v, Go %v", path, r.ID, part.name, sorted(docADRs), sorted(goADRs))
			}
		}
		// P5: oracle tests, or "none" with a reason.
		docOracles, goOracles := map[string]bool{}, map[string]bool{}
		for _, tok := range tokens(row.p5) {
			if strings.HasPrefix(tok, "Test") {
				docOracles[tok] = true
			}
		}
		for _, ref := range r.Oracle {
			goOracles[ref.Name] = true
		}
		if !equalSets(docOracles, goOracles) {
			t.Errorf("%s %s: P5 oracles %v, Go %v", path, r.ID, sorted(docOracles), sorted(goOracles))
		}
		if r.OracleNone != "" && !strings.Contains(row.p5, "none") {
			t.Errorf("%s %s: P5 must state oracle none with the reason", path, r.ID)
		}
	}
}

// plannedFileRows are the languages of the planned file rows PL-L1..PL-L4
// (T020): no other language may get a file row in T020.
var plannedFileRows = map[string]string{
	"go": "PL-L1", "python": "PL-L2", "typescript": "PL-L3", "tsx": "PL-L3", "javascript": "PL-L3", "rust": "PL-L4",
}

// TestFileLabel checks every scanner language's file label and row against
// the file rows of the table, that Go is labelled by PL-L1 (T020)
// and Python by PL-L2, TypeScript, TSX and JavaScript by PL-L3
// and Rust by PL-L4, and that file rows name only the
// languages their planned row names.
func TestFileLabel(t *testing.T) {
	languages := []string{"typescript", "tsx", "javascript", "go", "rust", "python", "java", "c_sharp", "dart",
		"html", "css", "json", "yaml", "toml", "markdown", "sql", "shell", "unknown", ""}
	fileRows := map[string]string{}
	for _, r := range precision.Rules() {
		if r.Kind == precision.KindFile {
			for _, l := range r.Languages {
				if fileRows[l] != "" {
					t.Errorf("language %s is named by rows %s and %s", l, fileRows[l], r.ID)
				}
				fileRows[l] = r.ID
				if plannedFileRows[l] != r.ID {
					t.Errorf("row %s names %s; T020 plans %q for it", r.ID, l, plannedFileRows[l])
				}
			}
		}
	}
	for _, l := range languages {
		want, row := string(precision.BestEffort), precision.DefaultRow
		if id := fileRows[l]; id != "" {
			want, row = string(precision.Precise), id
		}
		if got := precision.FileLabel(l); got != want {
			t.Errorf("FileLabel(%q) = %q, want %q", l, got, want)
		}
		if got := precision.FileRow(l); got != row {
			t.Errorf("FileRow(%q) = %q, want %q", l, got, row)
		}
	}
	if got := precision.FileRow("go"); got != "PL-L1" {
		t.Errorf("FileRow(go) = %q, want PL-L1", got)
	}
	if got := precision.FileRow("python"); got != "PL-L2" {
		t.Errorf("FileRow(python) = %q, want PL-L2", got)
	}
	for _, l := range []string{"typescript", "tsx", "javascript"} {
		if got := precision.FileRow(l); got != "PL-L3" {
			t.Errorf("FileRow(%s) = %q, want PL-L3", l, got)
		}
	}
	if got := precision.FileRow("rust"); got != "PL-L4" {
		t.Errorf("FileRow(rust) = %q, want PL-L4", got)
	}
}

// TestScopeLabel checks the markers of every family row, the default
// branch and the run refusals.
func TestScopeLabel(t *testing.T) {
	eligible := precision.Run{BuiltIn: true, ExtractorVersion: index.Version, BinaryVersion: index.Version}
	precise := func(row string) precision.Decision {
		return precision.Decision{Label: precision.Precise, Row: row}
	}
	fallback := func(reason string) precision.Decision {
		return precision.Decision{Label: precision.BestEffort, Row: precision.DefaultRow, Reason: reason}
	}
	sqlx := precision.Marker{Family: precision.FamilyMigrationSchema, Tool: "sqlx", Dialect: "postgres"}
	model := func(tool, dialect string) precision.Marker {
		return precision.Marker{Family: precision.FamilyModelSchema, Tool: tool, Dialect: dialect}
	}
	cases := []struct {
		name   string
		run    precision.Run
		marker precision.Marker
		want   precision.Decision
	}{
		{"sqlx postgres set", eligible, sqlx, precise("PL-F1")},
		{"sqlx set without dialect", eligible, precision.Marker{Family: precision.FamilyMigrationSchema, Tool: "sqlx"}, fallback(precision.ReasonDefault)},
		{"sqlx sqlite set", eligible, precision.Marker{Family: precision.FamilyMigrationSchema, Tool: "sqlx", Dialect: "sqlite"}, fallback(precision.ReasonDefault)},
		{"migration set of another tool", eligible, precision.Marker{Family: precision.FamilyMigrationSchema, Tool: "diesel", Dialect: "postgres"}, fallback(precision.ReasonDefault)},
		{"sqlmodel", eligible, model("sqlmodel", ""), precise("PL-F2")},
		{"prisma", eligible, model("prisma", "postgresql"), precise("PL-F3")},
		{"drizzle mysql (mapped dialect, O13)", eligible, model("drizzle", "mysql"), precise("PL-F4")},
		{"sequelize", eligible, model("sequelize", "postgres"), precise("PL-F5")},
		{"diesel", eligible, model("diesel", "postgres"), precise("PL-F6")},
		{"gorm", eligible, model("gorm", "sqlite"), precise("PL-F7")},
		{"typeorm mssql (mapped dialect, O13)", eligible, model("typeorm", "mssql"), precise("PL-F8")},
		{"model group without tool", eligible, model("", ""), fallback(precision.ReasonDefault)},
		{"unknown model tool", eligible, model("peewee", ""), fallback(precision.ReasonDefault)},
		{"table access into a sqlx table", eligible, precision.Marker{Family: precision.FamilyTableAccess, Tool: "sqlx"}, precise("PL-F9")},
		{"table access into a drizzle table", eligible, precision.Marker{Family: precision.FamilyTableAccess, Tool: "drizzle"}, precise("PL-F9")},
		{"table access into a supabase table (T024c2)", eligible, precision.Marker{Family: precision.FamilyTableAccess, Tool: "supabase"}, fallback(precision.ReasonDefault)},
		{"table access with an unknown target", eligible, precision.Marker{Family: precision.FamilyTableAccess}, fallback(precision.ReasonDefault)},
		{"routes without a framework", eligible, precision.Marker{Family: precision.FamilyRoutes}, fallback(precision.ReasonDefault)},
		{"chi routes (T023 GO4)", eligible, precision.Marker{Family: precision.FamilyRoutes, Tool: "chi"}, precise("PL-F10")},
		{"net/http routes", eligible, precision.Marker{Family: precision.FamilyRoutes, Tool: "net/http"}, precise("PL-F10")},
		{"express routes", eligible, precision.Marker{Family: precision.FamilyRoutes, Tool: "express"}, fallback(precision.ReasonDefault)},
		{"routes of several frameworks", eligible, precision.Marker{Family: precision.FamilyRoutes, Conflict: true}, fallback(precision.ReasonMarkerConflict)},
		{"nextjs routes (T024)", eligible, precision.Marker{Family: precision.FamilyRoutes, Tool: "nextjs"}, fallback(precision.ReasonDefault)},
		{"no marker", eligible, precision.Marker{}, fallback(precision.ReasonDefault)},
		{"tool without family", eligible, precision.Marker{Tool: "prisma"}, fallback(precision.ReasonDefault)},
		{"model group with conflicting tools", eligible, precision.Marker{Family: precision.FamilyModelSchema, Conflict: true}, fallback(precision.ReasonMarkerConflict)},
		{"sqlx set with conflicting dialects", eligible, precision.Marker{Family: precision.FamilyMigrationSchema, Tool: "sqlx", Conflict: true}, fallback(precision.ReasonMarkerConflict)},
		{"conflict on a refused run", precision.Run{BuiltIn: true}, precision.Marker{Family: precision.FamilyModelSchema, Conflict: true}, fallback(precision.ReasonExtractorMismatch)},
		{"external indexer", precision.Run{BuiltIn: false, ExtractorVersion: index.Version, BinaryVersion: index.Version}, model("prisma", ""), fallback(precision.ReasonExternalIndexer)},
		{"no run stored", precision.Run{NoRun: true, BinaryVersion: index.Version}, model("prisma", "postgresql"), fallback(precision.ReasonNoRun)},
		{"older extractor", precision.Run{BuiltIn: true, ExtractorVersion: "extractors-1", BinaryVersion: index.Version}, model("prisma", ""), fallback(precision.ReasonExtractorMismatch)},
		{"no extractor version", precision.Run{BuiltIn: true}, model("prisma", ""), fallback(precision.ReasonExtractorMismatch)},
	}
	covered := map[string]bool{}
	reasons := map[string]bool{}
	for _, c := range cases {
		got := precision.ScopeLabel(c.run, c.marker)
		if got != c.want {
			t.Errorf("%s: ScopeLabel = %+v, want %+v", c.name, got, c.want)
		}
		covered[got.Row] = true
		if got.Reason != "" {
			reasons[got.Reason] = true
		}
	}
	// Reasons() is the list the UI's words are checked against: every
	// reason a case reaches is listed, and every listed reason is reached.
	listed := map[string]bool{}
	for _, r := range precision.Reasons() {
		listed[r] = true
		if !reasons[r] {
			t.Errorf("reason %s: no ScopeLabel case reaches it", r)
		}
	}
	for r := range reasons {
		if !listed[r] {
			t.Errorf("reason %s is returned but not in Reasons()", r)
		}
	}
	for _, r := range precision.Rules() {
		if r.Kind == precision.KindFamily && !covered[r.ID] {
			t.Errorf("row %s: no ScopeLabel case reaches it", r.ID)
		}
	}
}

// TestCrossRepositoryLinksNeverPrecise is the cross-repository rule of
// docs/extract/precision.md (T022 B11): links between project members are
// best_effort through the default branch on every run, eligible or not,
// and no row of the table names their family.
func TestCrossRepositoryLinksNeverPrecise(t *testing.T) {
	m := precision.Marker{Family: precision.FamilyCrossRepositoryLinks}
	eligible := precision.Run{BuiltIn: true, ExtractorVersion: index.Version, BinaryVersion: index.Version}
	want := precision.Decision{Label: precision.BestEffort, Row: precision.DefaultRow, Reason: precision.ReasonDefault}
	if got := precision.ScopeLabel(eligible, m); got != want {
		t.Errorf("ScopeLabel(eligible run, cross_repository_links) = %+v, want %+v", got, want)
	}
	for _, run := range []precision.Run{{NoRun: true}, {BuiltIn: false}, {BuiltIn: true, ExtractorVersion: "extractors-1", BinaryVersion: index.Version}} {
		if got := precision.ScopeLabel(run, m); got.Label != precision.BestEffort || got.Row != precision.DefaultRow {
			t.Errorf("ScopeLabel(%+v, cross_repository_links) = %+v", run, got)
		}
	}
	for _, r := range precision.Rules() {
		if r.Match.Family == precision.FamilyCrossRepositoryLinks {
			t.Errorf("row %s names cross_repository_links", r.ID)
		}
	}
	if !precision.NeverPrecise(precision.FamilyCrossRepositoryLinks) || precision.NeverPrecise(precision.FamilyModelSchema) {
		t.Error("NeverPrecise disagrees with the cross-repository rule")
	}
}
