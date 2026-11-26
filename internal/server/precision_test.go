package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/precision"
	"github.com/graphvein/graphvein/internal/store"
)

// indexMismatched indexes root as repo with another extractor version, as
// an index written by an older binary would be.
func indexMismatched(t *testing.T, root string, s store.Store, repo string) {
	t.Helper()
	ctx := context.Background()
	var stream bytes.Buffer
	opt := index.Options{Repository: repo, ExtractorVersion: "extractors-0", Parser: parse.Options{Workers: 1, CacheDir: filepath.Join(t.TempDir(), "cache")}}
	if _, err := index.Emit(ctx, root, s, &stream, opt); err != nil {
		t.Fatal(err)
	}
	if err := ingest.Stream(ctx, &stream, s); err != nil {
		t.Fatal(err)
	}
}

// precisionOptions extends schemaOptions with "api" (the API section
// fixture: routes and calls) and "mismatch" (the sqlmodel fixture indexed
// at another extractor version).
func precisionOptions(t *testing.T, w, s store.Store) (map[string]Options, map[string]string) {
	t.Helper()
	options, roots := schemaOptions(t, w, s)
	api := apiFixtureRoot(t)
	indexRepository(t, api, w, "api")
	mismatch := t.TempDir()
	if err := os.CopyFS(mismatch, os.DirFS("../../testdata/fixtures/sqlmodel-link")); err != nil {
		t.Fatal(err)
	}
	indexMismatched(t, mismatch, w, "mismatch")
	for repo, dir := range map[string]string{"api": api, "mismatch": mismatch} {
		opt := testOptions(dir, s)
		opt.Repository = repo
		options[repo], roots[repo] = opt, dir
	}
	return options, roots
}

// TestPrecisionGoldens checks GET /api/v0/precision on both backends for a
// built-in index (sqlx sets), an ORM index, an API index, an external
// index, a mismatched index and an empty repository.
func TestPrecisionGoldens(t *testing.T) {
	var mu sync.Mutex
	bodies := map[string]map[string]string{}
	backends(t, func(t *testing.T, w, s store.Store) {
		options, roots := precisionOptions(t, w, s)
		got := map[string]string{}
		for _, repo := range []string{"fixture", "models", "api", "states", "mismatch", "empty"} {
			name := "precision-" + repo
			rec := get(t, options[repo], http.MethodGet, "/api/v0/precision", nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("%s: status %d %s", repo, rec.Code, rec.Body.String())
			}
			v := normalizeAPI(t, rec.Body.Bytes())
			data, err := json.Marshal(v)
			if err != nil {
				t.Fatal(err)
			}
			got[name] = golden.ReplaceRoot(string(data), filepath.ToSlash(roots[repo]))
			golden.CheckRoot(t, filepath.Join("testdata", name+".golden.json"), v, roots[repo])
		}
		mu.Lock()
		bodies[filepath.Base(t.Name())] = got
		mu.Unlock()
	})
	if bodies["sqlite"] != nil && bodies["postgres"] != nil {
		for name, body := range bodies["sqlite"] {
			if bodies["postgres"][name] != body {
				t.Errorf("%s: SQLite and Postgres bodies differ", name)
			}
		}
	}
}

// TestPrecisionRefusals checks that no view labels anything precise for an
// external or mismatched index, and that /precision states why.
func TestPrecisionRefusals(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		options, _ := precisionOptions(t, w, s)
		for repo, reason := range map[string]string{"states": precision.ReasonExternalIndexer, "mismatch": precision.ReasonExtractorMismatch} {
			for _, path := range []string{"/api/v0/precision", "/api/v0/schema/sets"} {
				rec := get(t, options[repo], http.MethodGet, path, nil)
				if rec.Code != http.StatusOK {
					t.Fatalf("%s %s: status %d", repo, path, rec.Code)
				}
				body := rec.Body.String()
				if strings.Contains(body, `"label":"precise"`) {
					t.Errorf("%s %s labels a fact precise: %s", repo, path, body)
				}
				if !strings.Contains(body, `"reason":"`+reason+`"`) && !strings.Contains(body, `"refusal":"`+reason+`"`) {
					t.Errorf("%s %s does not state %s", repo, path, reason)
				}
			}
		}
		// The ORM index at this binary's version earns PL-F2 on its group.
		rec := get(t, options["models"], http.MethodGet, "/api/v0/schema/sets", nil)
		if !strings.Contains(rec.Body.String(), `"precision":{"label":"precise","row":"PL-F2","reason":null}`) {
			t.Errorf("models group is not labelled PL-F2: %s", rec.Body.String())
		}
	})
}

// TestPrecisionNoRun checks that a repository with no stored run is
// refused as no_run, never described as an external indexer's:
// /precision states no_run on every rule row, serves no run and no
// file counts, and no view says external_indexer or labels anything
// precise.
func TestPrecisionNoRun(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		options, _ := precisionOptions(t, w, s)
		var out precisionResponse
		decodeInto(t, get(t, options["empty"], http.MethodGet, "/api/v0/precision", nil), &out)
		if out.Run != nil || out.Files != nil || out.Index != nil {
			t.Errorf("empty repository: run %+v, files %+v, index %+v; want all null", out.Run, out.Files, out.Index)
		}
		if len(out.Rules) == 0 {
			t.Fatal("no rule rows")
		}
		for _, r := range out.Rules {
			if r.Label != precision.BestEffort || r.Reason == nil || *r.Reason != precision.ReasonNoRun {
				t.Errorf("row %s: label %s reason %v, want best_effort no_run", r.ID, r.Label, r.Reason)
			}
		}
		for _, path := range []string{"/api/v0/precision", "/api/v0/schema/sets", "/api/v0/api/endpoints?scope=all"} {
			rec := get(t, options["empty"], http.MethodGet, path, nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("%s: status %d %s", path, rec.Code, rec.Body.String())
			}
			body := rec.Body.String()
			if strings.Contains(body, precision.ReasonExternalIndexer) || labelsPrecise(t, body) {
				t.Errorf("empty repository %s says external_indexer or labels a fact precise: %s", path, body)
			}
		}
	})
}

// TestSourceFileLabel checks the source endpoint's file fields: the stored
// language and Graphvein's label for a built-in index, the refusal for a
// mismatched one, and the producer's own label kept apart for an external
// one (open point O6).
func TestSourceFileLabel(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		options, _ := precisionOptions(t, w, s)
		ingestFlowGraph(t, w)
		flow := testOptions(t.TempDir(), s)
		flow.Repository = "flowgraph"
		options["flowgraph"] = flow
		str := func(p *string) string {
			if p == nil {
				return "null"
			}
			return *p
		}
		for _, c := range []struct {
			repo, id                          string
			language, label, reason, reported string
		}{
			{"models", "table:app/models.py#Team", "python", "precise", "null", "null"},
			{"mismatch", "table:app/models.py#Team", "python", "best_effort", precision.ReasonExtractorMismatch, "null"},
			{"flowgraph", "function:web/pages.ts#App", "typescript", "best_effort", precision.ReasonExternalIndexer, "precise"},
		} {
			b := getSource(t, options[c.repo], c.id, "")
			f := b.Source.File
			got := []string{str(f.Language), str(f.Precision), str(f.PrecisionReason), str(f.ReportedPrecision)}
			want := []string{c.language, c.label, c.reason, c.reported}
			if strings.Join(got, " ") != strings.Join(want, " ") {
				t.Errorf("%s %s: file label %v, want %v", c.repo, c.id, got, want)
			}
		}
	})
}

// TestFileLabelOf checks the file label rules without a store.
func TestFileLabelOf(t *testing.T) {
	builtIn := runOfIndexer("graphvein", index.Version)
	external := runOfIndexer("crafted", "1")
	str := func(p *string) string {
		if p == nil {
			return "null"
		}
		return *p
	}
	for _, c := range []struct {
		name string
		rec  *contract.File
		run  precision.Run
		want string
		// unrooted: the index stores a structure_unrooted diagnostic.
		unrooted bool
	}{
		{"no record", nil, builtIn, "null null null null null", false},
		{"precise go (PL-L1)", &contract.File{Language: "go", Precision: "precise"}, builtIn, "go precise null PL-L1 null", false},
		// The indexer refused the file's structure (ADR 0068): served as
		// stored, with the row that would apply.
		{"go structure refused", &contract.File{Language: "go", Precision: "best_effort"}, builtIn, "go best_effort structure_refused PL-L1 null", false},
		{"precise python (PL-L2)", &contract.File{Language: "python", Precision: "precise"}, builtIn, "python precise null PL-L2 null", false},
		{"python structure refused", &contract.File{Language: "python", Precision: "best_effort"}, builtIn, "python best_effort structure_refused PL-L2 null", false},
		{"precise rust (PL-L4)", &contract.File{Language: "rust", Precision: "precise"}, builtIn, "rust precise null PL-L4 null", false},
		{"rust structure refused", &contract.File{Language: "rust", Precision: "best_effort"}, builtIn, "rust best_effort structure_refused PL-L4 null", false},
		// A Rust file no crate root reaches (ADR 0071, T020 S4L).
		{"rust unrooted", &contract.File{Language: "rust", Precision: "best_effort"}, builtIn, "rust best_effort structure_unrooted PL-L4 null", true},
		// The diagnostic never changes a label the rule does not lower.
		{"unrooted on a default language", &contract.File{Language: "java", Precision: "best_effort"}, builtIn, "java best_effort default_branch D null", true},
		{"unrooted on a precise record", &contract.File{Language: "rust", Precision: "precise"}, builtIn, "rust precise null PL-L4 null", true},
		{"best effort", &contract.File{Language: "java", Precision: "best_effort"}, builtIn, "java best_effort default_branch D null", false},
		// No file row names java (T021): a stored precise is not this table's label.
		{"stored precise without a row", &contract.File{Language: "java", Precision: "precise"}, builtIn, "java best_effort stored_label_invalid null null", false},
		{"stored value outside the enum", &contract.File{Language: "go", Precision: "certain"}, builtIn, "go best_effort stored_label_invalid null null", false},
		{"external", &contract.File{Language: "go", Precision: "precise"}, external, "go best_effort external_indexer null precise", false},
		{"mismatched extractor", &contract.File{Language: "go", Precision: "precise"}, runOfIndexer("graphvein", "extractors-1"), "go best_effort extractor_mismatch null null", false},
	} {
		l := fileLabelOf(c.rec, c.run, c.unrooted)
		got := strings.Join([]string{str(l.Language), str(l.Precision), str(l.PrecisionReason), str(l.PrecisionRow), str(l.ReportedPrecision)}, " ")
		if got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
}

// TestPrecisionDefaultWording keeps row D's wording equal to the D row of
// docs/extract/precision.md.
func TestPrecisionDefaultWording(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "extract", "precision.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
		if !strings.HasPrefix(line, "| D |") {
			continue
		}
		cells := strings.Split(line, "|")
		if len(cells) < 4 || strings.TrimSpace(cells[2]) != defaultScope || strings.TrimSpace(cells[3]) != defaultCovers {
			t.Errorf("row D in docs/extract/precision.md differs from the served wording: %q", line)
		}
		return
	}
	t.Fatal("no row D in docs/extract/precision.md")
}

// TestPrecisionReasons keeps labelReasons the one list of label reasons:
// it writes the list to the golden the web tests read (each reason needs
// its own words there), and checks that every reason a server golden
// carries on a label, a file label or a run is in it.
func TestPrecisionReasons(t *testing.T) {
	golden.Check(t, filepath.Join("testdata", "precision-reasons.golden.json"), labelReasons())
	listed := map[string]bool{}
	for _, r := range labelReasons() {
		if listed[r] {
			t.Errorf("reason %s is listed twice", r)
		}
		listed[r] = true
	}
	paths, err := filepath.Glob(filepath.Join("testdata", "*.golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		for _, r := range labelReasonsIn(v) {
			seen++
			if !listed[r] {
				t.Errorf("%s carries reason %q, which labelReasons does not list", p, r)
			}
		}
	}
	if seen == 0 {
		t.Fatal("no golden carries a label reason; the walk finds nothing")
	}
}

// labelReasonsIn collects the reasons of every scope label ({label, row,
// reason}), file label (precision_reason) and run (refusal) in v.
func labelReasonsIn(v any) []string {
	var out []string
	add := func(x any) {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	switch v := v.(type) {
	case map[string]any:
		if _, ok := v["label"]; ok {
			if _, row := v["row"]; row {
				add(v["reason"])
			}
		}
		add(v["precision_reason"])
		if _, ok := v["built_in"]; ok {
			add(v["refusal"])
		}
		for _, x := range v {
			out = append(out, labelReasonsIn(x)...)
		}
	case []any:
		for _, x := range v {
			out = append(out, labelReasonsIn(x)...)
		}
	}
	return out
}

// indexStream indexes root as repo at extractor version (empty: this
// binary's), with the header's indexer renamed to producer when it is not
// empty, as an external indexer's index of the same facts would be.
func indexStream(t *testing.T, root string, s store.Store, repo, version, producer string) {
	t.Helper()
	ctx := context.Background()
	var stream bytes.Buffer
	opt := index.Options{Repository: repo, ExtractorVersion: version, Parser: parse.Options{Workers: 1, CacheDir: filepath.Join(t.TempDir(), "cache")}}
	if _, err := index.Emit(ctx, root, s, &stream, opt); err != nil {
		t.Fatal(err)
	}
	data := stream.Bytes()
	if producer != "" {
		header, rest, _ := bytes.Cut(data, []byte("\n"))
		renamed := bytes.Replace(header, []byte(`"name":"graphvein"`), []byte(`"name":"`+producer+`"`), 1)
		if bytes.Equal(renamed, header) {
			t.Fatalf("header names no graphvein indexer: %s", header)
		}
		data = append(append(renamed, '\n'), rest...)
	}
	if err := ingest.Stream(ctx, bytes.NewReader(data), s); err != nil {
		t.Fatal(err)
	}
}

// TestPrecisionRefusalViews checks every view that serves a label for an
// index no row may label precise: an external index and one indexed by
// another extractor version (base and head both refused), and a
// comparison whose base side alone is refused. The fixture (change-impact
// rev1 then rev2) holds facts that earn PL-F1, PL-F2 and PL-F9 when the
// run is eligible, so a missing refusal would show.
func TestPrecisionRefusalViews(t *testing.T) {
	rev1, rev2 := t.TempDir(), t.TempDir()
	for dir, rev := range map[string]string{rev1: "rev1", rev2: "rev2"} {
		if err := os.CopyFS(dir, os.DirFS(filepath.Join(changeFixture, rev))); err != nil {
			t.Fatal(err)
		}
	}
	backends(t, func(t *testing.T, w, s store.Store) {
		for _, r := range []struct{ repo, base, head, producer string }{
			{"external", index.Version, index.Version, "crafted"},
			{"mismatch", "extractors-0", "extractors-0", ""},
			{"basemismatch", "extractors-0", "", ""},
			{"eligible", "", "", ""},
		} {
			indexStream(t, rev1, w, r.repo, r.base, r.producer)
			indexStream(t, rev2, w, r.repo, r.head, r.producer)
		}
		options := map[string]Options{}
		for _, repo := range []string{"external", "mismatch", "basemismatch", "eligible"} {
			opt := testOptions(rev2, s)
			opt.Repository = repo
			options[repo] = opt
		}
		// Identities of one labelled fact per view, from the eligible
		// index (every index holds the same facts).
		rd := &reader{ctx: context.Background(), s: s, repo: "eligible"}
		first := func(kind string) string {
			nodes, err := rd.nodes(kind)
			if err != nil || len(nodes) == 0 {
				t.Fatalf("no %s node: %v", kind, err)
			}
			return url.QueryEscape(nodes[0].Identity)
		}
		var table string
		tables, err := rd.nodes("table")
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range tables {
			if n.Attributes["tool"] == "sqlmodel" {
				table = url.QueryEscape(n.Identity)
			}
		}
		route := first("route")
		views := []string{
			"/api/v0/precision",
			"/api/v0/schema/sets",
			"/api/v0/schema/table?id=" + table,
			"/api/v0/api/endpoints?scope=all",
			"/api/v0/api/endpoint?scope=all&id=" + route,
			"/api/v0/api/flow?scope=all&id=" + route,
			"/api/v0/source?id=" + table,
			"/api/v0/changes/items?base=previous&tests=true&limit=500",
		}
		body := func(repo, path string) string {
			t.Helper()
			rec := get(t, options[repo], http.MethodGet, path, nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("%s %s: status %d %s", repo, path, rec.Code, rec.Body.String())
			}
			return rec.Body.String()
		}
		// The eligible index labels the same views precise somewhere, so
		// the views are the ones that would carry a precise label.
		for _, path := range views {
			if path == "/api/v0/source?id="+table || strings.HasPrefix(path, "/api/v0/api/") {
				continue // file labels and endpoint families have no precise row
			}
			if !strings.Contains(body("eligible", path), `"label":"precise"`) {
				t.Errorf("eligible %s labels nothing precise; the refusal check below proves nothing there", path)
			}
		}
		for repo, reason := range map[string]string{"external": precision.ReasonExternalIndexer, "mismatch": precision.ReasonExtractorMismatch} {
			for _, path := range views {
				b := body(repo, path)
				if labelsPrecise(t, b) {
					t.Errorf("%s %s labels a fact precise: %s", repo, path, b)
				}
				if !strings.Contains(b, `"`+reason+`"`) {
					t.Errorf("%s %s does not state %s", repo, path, reason)
				}
			}
		}
		// Base side refused, head eligible: an item with a base side is
		// labelled from the base run (best effort, extractor_mismatch,
		// scope_side base); an added item keeps the head's label.
		var page struct {
			Items []struct {
				Identity  string          `json:"identity"`
				Before    json.RawMessage `json:"before"`
				Precision itemPrecision   `json:"precision"`
			} `json:"items"`
		}
		if err := json.Unmarshal([]byte(body("basemismatch", views[len(views)-1])), &page); err != nil {
			t.Fatal(err)
		}
		based, added, addedPrecise := 0, 0, 0
		for _, it := range page.Items {
			p := it.Precision
			hasBase := len(it.Before) > 0 && string(it.Before) != "null"
			if hasBase {
				if p.ScopeSide != "base" {
					t.Errorf("%s: scope_side %q, want base", it.Identity, p.ScopeSide)
				}
				if p.Scope != nil {
					based++
					if p.Scope.Label != precision.BestEffort || p.Scope.Reason == nil || *p.Scope.Reason != precision.ReasonExtractorMismatch {
						t.Errorf("%s: base-side scope %+v, want best_effort extractor_mismatch", it.Identity, p.Scope)
					}
				}
				for name, f := range p.Fields {
					if f != nil && f.Label == precision.Precise {
						t.Errorf("%s: field %s precise from a refused base", it.Identity, name)
					}
				}
				continue
			}
			if p.ScopeSide != "head" {
				t.Errorf("%s: scope_side %q, want head", it.Identity, p.ScopeSide)
			}
			if p.Scope != nil {
				added++
				if p.Scope.Label == precision.Precise {
					addedPrecise++
				}
			}
		}
		t.Logf("base-refused comparison: %d labelled items with a base side, %d added (%d precise)", based, added, addedPrecise)
		if based == 0 || added == 0 || addedPrecise == 0 {
			t.Errorf("base-refused comparison: %d labelled items with a base side, %d added (%d precise); want each > 0", based, added, addedPrecise)
		}
	})
}

// TestSchemaGroupMarkerConflict checks that a group whose records name
// two tools or dialects is labelled marker_conflict, not default_branch:
// each tool may have a contract, the group as a whole has no row.
func TestSchemaGroupMarkerConflict(t *testing.T) {
	key := "app"
	node := func(attrs map[string]any) contract.Node { return contract.Node{Attributes: attrs} }
	eligible := runOfIndexer("graphvein", index.Version)
	reason := func(m precision.Marker) string {
		l := labelOf(eligible, m)
		if l.Reason == nil {
			return string(l.Label)
		}
		return *l.Reason
	}
	for _, c := range []struct {
		name               string
		model              bool
		migrations, tables []contract.Node
		want               string
	}{
		{"model group of two tools", true, nil, []contract.Node{node(map[string]any{"tool": "prisma"}), node(map[string]any{"tool": "drizzle"})}, precision.ReasonMarkerConflict},
		{"model group of one tool", true, nil, []contract.Node{node(map[string]any{"tool": "prisma"}), node(map[string]any{"tool": "prisma"})}, string(precision.Precise)},
		{"sqlx set of two dialects", false, []contract.Node{node(map[string]any{"tool": "sqlx", "dialect": "postgres"}), node(map[string]any{"tool": "sqlx", "dialect": "sqlite"})}, nil, precision.ReasonMarkerConflict},
		{"sqlx set without a dialect", false, []contract.Node{node(map[string]any{"tool": "sqlx"})}, nil, precision.ReasonDefault},
		{"sqlite set", false, []contract.Node{node(map[string]any{"tool": "sqlx", "dialect": "sqlite"})}, nil, precision.ReasonDefault},
	} {
		if got := reason(schemaGroupMarker(c.model, &key, c.migrations, c.tables)); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
}

// TestCountFiles checks that /precision counts files by Graphvein's label
// (fileLabelOf), never by the stored value: a stored precise without a
// file row, a mismatched run and an external run all count best_effort,
// and only the external run's own values are kept, apart.
func TestCountFiles(t *testing.T) {
	// Java has no file row (T021), so a stored precise is not this table's label.
	files := []contract.File{{Language: "java", Precision: "precise"}, {Language: "java", Precision: "best_effort"}}
	for _, c := range []struct {
		name     string
		run      precision.Run
		reported string
	}{
		{"eligible", runOfIndexer("graphvein", index.Version), "null"},
		{"mismatch", runOfIndexer("graphvein", "extractors-0"), "null"},
		{"external", runOfIndexer("crafted", "1"), `{"best_effort":1,"precise":1}`},
	} {
		got := countFiles(files, c.run).Languages[0]
		reported, _ := json.Marshal(got.ReportedByLabel)
		if len(got.ByLabel) != 1 || got.ByLabel["best_effort"] != 2 || string(reported) != c.reported {
			t.Errorf("%s: by_label %v reported %s, want best_effort 2 and %s", c.name, got.ByLabel, reported, c.reported)
		}
	}
	// Go has row PL-L1: an eligible run counts a stored precise as precise
	// and a file whose structure was refused as best_effort.
	goFiles := []contract.File{{Language: "go", Precision: "precise"}, {Language: "go", Precision: "best_effort"}}
	if got := countFiles(goFiles, runOfIndexer("graphvein", index.Version)).Languages[0].ByLabel; got["precise"] != 1 || got["best_effort"] != 1 {
		t.Errorf("go: by_label %v, want precise 1 and best_effort 1", got)
	}
}

// labelsPrecise reports whether a response body carries a precise label
// anywhere: a "label" or a file "precision" of "precise". A change item's
// before and after "fields" hold stored record values (a file record's
// stored precision among them), not labels, so they are skipped.
func labelsPrecise(t *testing.T, body string) bool {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		t.Fatal(err)
	}
	var walk func(v any, side bool) bool
	walk = func(v any, side bool) bool {
		switch x := v.(type) {
		case map[string]any:
			for k, e := range x {
				if (k == "label" || k == "precision") && e == "precise" {
					return true
				}
				if side && k == "fields" {
					continue
				}
				if walk(e, k == "before" || k == "after") {
					return true
				}
			}
		case []any:
			for _, e := range x {
				if walk(e, false) {
					return true
				}
			}
		}
		return false
	}
	return walk(v, false)
}

// TestPrecisionDefaultFamilies keeps the "Row D families" table of
// docs/extract/precision.md 1:1 with defaultFamilies (order, family and
// unit), and holds every listed family on row D: none of them, the
// cross-repository links (T022 B11) included, can be labelled precise by
// the served table.
func TestPrecisionDefaultFamilies(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "extract", "precision.md"))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	in := false
	for _, line := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
		if strings.HasPrefix(line, "| family | facts counted | unit |") {
			in = true
			continue
		}
		if !in || strings.HasPrefix(line, "|---") {
			continue
		}
		if !strings.HasPrefix(line, "|") {
			break
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		if len(cells) != 3 {
			t.Fatalf("row D family row has %d cells: %s", len(cells), line)
		}
		got = append(got, strings.Trim(strings.TrimSpace(cells[0]), "`")+" "+strings.TrimSpace(cells[2]))
	}
	var want []string
	for _, f := range defaultFamilies {
		want = append(want, string(f)+" "+unitOf(f))
		if row := rowOf(precision.Marker{Family: f}); row != precision.DefaultRow {
			t.Errorf("family %s falls in row %s, not D", f, row)
		}
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("docs/extract/precision.md row D families %v, served %v", got, want)
	}
}
