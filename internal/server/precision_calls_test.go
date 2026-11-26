package server

import (
	"bytes"
	"context"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/graphvein/graphvein/internal/changes"
	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/precision"
	"github.com/graphvein/graphvein/internal/store"
)

// TestCodeCallsNeverPrecise is O-S1-5 (a) of task T020: wherever the API
// shows code `calls` edges next to a file label, it also sends the
// code_calls label, and that label is never precise because a file label
// is. The flow fixture is TypeScript, which has no PL-L row yet, so the
// test stands one in: it makes typescript a precise language (fileRule,
// fileRowOf), stores the flow graph as
// a built-in run of this binary's extractor version with typescript files
// stored precise, and checks that the file label is precise while every
// code calls label stays best_effort from row D: on /flow (whose edges
// include `calls`), on /source of a function, on a change item of a
// `calls` edge, and on /precision's code_calls column, whose count is
// checked against every `calls` edge the store dumps.
func TestCodeCallsNeverPrecise(t *testing.T) {
	saved := fileRule
	fileRule = func(language string) string {
		if language == "typescript" {
			return string(precision.Precise)
		}
		return saved(language)
	}
	savedRow := fileRowOf
	fileRowOf = func(language string) string {
		if language == "typescript" {
			return "PL-L3"
		}
		return savedRow(language)
	}
	t.Cleanup(func() { fileRule, fileRowOf = saved, savedRow })

	raw, err := os.ReadFile(filepath.Join("testdata", "flow-graph.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	builtIn := bytes.Replace(raw, []byte(`"indexer":{"name":"crafted","version":"t016f1"}`), []byte(`"indexer":{"name":"graphvein","version":"`+index.Version+`"}`), 1)
	if bytes.Equal(builtIn, raw) {
		t.Fatal("the flow graph header changed; update this test")
	}
	defaultBranch := func(t *testing.T, where string, l *scopeLabel) {
		t.Helper()
		if l == nil || l.Label != precision.BestEffort || l.Row != precision.DefaultRow || l.Reason == nil || *l.Reason != precision.ReasonDefault {
			t.Errorf("%s: code calls label %+v, want best_effort from row D (default_branch)", where, l)
		}
	}
	backends(t, func(t *testing.T, w, s store.Store) {
		ingestStream(t, w, bytes.NewReader(builtIn), "flowgraph", 25)
		opt := testOptions(t.TempDir(), s)
		opt.Repository = "flowgraph"

		source := getSource(t, opt, "function:server/api.ts#readFlow", "")
		if f := source.Source.File; f.Precision == nil || *f.Precision != string(precision.Precise) {
			t.Fatalf("source: file label %+v, want the stand-in row's precise", f.fileLabel)
		}
		defaultBranch(t, "/source of a function", source.CodeCallsPrecision)
		if route := getSource(t, opt, flowRoute, ""); route.CodeCallsPrecision != nil {
			t.Errorf("/source of a route: code calls label %+v, want null", route.CodeCallsPrecision)
		}

		var flow struct {
			Edges []struct {
				Kind string `json:"kind"`
			} `json:"edges"`
			CodeCallsPrecision *scopeLabel `json:"code_calls_precision"`
		}
		decodeInto(t, get(t, opt, http.MethodGet, "/api/v0/api/flow?id="+url.QueryEscape(flowRoute), nil), &flow)
		calls := 0
		for _, e := range flow.Edges {
			if e.Kind == "calls" {
				calls++
			}
		}
		if calls == 0 {
			t.Fatal("/flow shows no calls edge; the case needs one")
		}
		defaultBranch(t, "/flow", flow.CodeCallsPrecision)

		var coverage precisionResponse
		decodeInto(t, get(t, opt, http.MethodGet, "/api/v0/precision", nil), &coverage)
		snap, err := s.Dump(context.Background(), "flowgraph")
		if err != nil {
			t.Fatal(err)
		}
		want := 0
		for _, e := range snap.Edges {
			if e.Kind == "calls" {
				want++
			}
		}
		if want == 0 {
			t.Fatal("the flow graph stores no calls edge; the count check proves nothing")
		}
		found := false
		for _, f := range coverage.Default.Families {
			if f.Family != familyCodeCalls {
				continue
			}
			found = true
			if f.Label != precision.BestEffort {
				t.Errorf("/precision code_calls label %s, want best_effort", f.Label)
			}
			if f.Facts == nil || f.Facts.Unit != "edges" || f.Facts.Total != want || f.Facts.ByLanguage["typescript"] != want {
				t.Errorf("/precision code_calls facts %+v, want %d typescript edges", f.Facts, want)
			}
		}
		if !found {
			t.Error("/precision has no code_calls family")
		}
	})

	run := runOfIndexer("graphvein", index.Version)
	defaultBranch(t, "change item of a calls edge", itemScope(changes.Item{Record: store.RecordEdge, Kind: "calls"}, run, nil, nil, nil, nil))
	l := codeCallsLabel(run)
	defaultBranch(t, "codeCallsLabel", &l)
}

// TestCodeCallsCountByLanguage is a later addition: the
// code_calls count of /api/v0/precision splits by the language of each
// calls edge's source file across languages, and puts an edge whose
// source file has no stored record (an external node's ".") under "none".
// The totals equal the calls edges Dump holds; edges of other kinds are
// not counted.
func TestCodeCallsCountByLanguage(t *testing.T) {
	file := func(path, language string) contract.File {
		return contract.File{Type: "file", Path: path, Language: language, Precision: precision.FileLabel(language), ContentHash: contract.ContentHash([]byte(path))}
	}
	node := func(kind, path, name string) contract.Node {
		id, err := contract.Identity(kind, path, name)
		if err != nil {
			t.Fatal(err)
		}
		n := contract.Node{Type: "node", Identity: id, Kind: kind, Level: 4, Name: name, QualifiedName: name, File: path, ContentHash: contract.ContentHash([]byte(id)), Attributes: map[string]any{}}
		if path != "." {
			n.Span = &contract.Span{StartLine: 1, EndLine: 1, EndColumn: 2}
			return n
		}
		// A synthetic node (no file) is hashed over its attributes.
		hash, err := contract.SyntheticHash(n.Attributes)
		if err != nil {
			t.Fatal(err)
		}
		n.ContentHash = hash
		return n
	}
	ts1, ts2 := node("function", "web/a.ts", "a"), node("function", "web/b.ts", "b")
	py1, py2 := node("function", "app/m.py", "f"), node("method", "app/m.py", "C.g")
	go1 := node("function", "cmd/main.go", "main")
	ext := node("external", ".", "npm:lib")
	edge := func(kind string, source, target contract.Node) contract.Edge {
		evidence := source.File
		if evidence == "." {
			evidence = target.File
		}
		return contract.Edge{Type: "edge", Kind: kind, Source: source.Identity, Target: target.Identity, Provenance: "fact", Confidence: 1,
			Evidence: []contract.Evidence{{File: evidence, Span: contract.Span{StartLine: 1, EndLine: 1, EndColumn: 1}}}}
	}
	files := []contract.File{file("web/a.ts", "typescript"), file("web/b.ts", "typescript"), file("app/m.py", "python"), file("cmd/main.go", "go")}
	nodes := []contract.Node{ts1, ts2, py1, py2, go1, ext}
	edges := []contract.Edge{
		edge("calls", ts1, ts2), edge("calls", ts2, ts1), edge("calls", ts1, go1),
		edge("calls", py1, py2), edge("calls", py2, py1),
		edge("calls", ext, ts1),
		edge("imports", ts1, ts2), edge("references", go1, py1),
	}
	want := map[string]int{"typescript": 3, "python": 2, noFileRecord: 1}
	backends(t, func(t *testing.T, w, s store.Store) {
		ctx := context.Background()
		header := contract.Header{Type: "header", ContractVersion: contract.Version, Indexer: contract.Indexer{Name: "graphvein", Version: index.Version}, Repository: "calls", GeneratedAt: "2026-11-18T00:00:00Z"}
		tx, err := w.BeginRun(ctx, header)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		for _, f := range files {
			if err := tx.UpsertFile(ctx, f); err != nil {
				t.Fatal(err)
			}
		}
		for _, n := range nodes {
			if err := tx.UpsertNode(ctx, n); err != nil {
				t.Fatal(err)
			}
		}
		for _, e := range edges {
			if err := tx.UpsertEdge(ctx, e); err != nil {
				t.Fatal(err)
			}
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		opt := testOptions(t.TempDir(), s)
		opt.Repository = "calls"
		var coverage precisionResponse
		decodeInto(t, get(t, opt, http.MethodGet, "/api/v0/precision", nil), &coverage)
		snap, err := s.Dump(ctx, "calls")
		if err != nil {
			t.Fatal(err)
		}
		dumped := 0
		for _, e := range snap.Edges {
			if e.Kind == "calls" {
				dumped++
			}
		}
		var got *precisionDefaultFamily
		for i := range coverage.Default.Families {
			if coverage.Default.Families[i].Family == familyCodeCalls {
				got = &coverage.Default.Families[i]
			}
		}
		if got == nil || got.Facts == nil {
			t.Fatalf("/precision has no code_calls facts: %+v", coverage.Default.Families)
		}
		if got.Label != precision.BestEffort || got.Facts.Unit != "edges" {
			t.Errorf("code_calls label %s unit %s, want best_effort edges", got.Label, got.Facts.Unit)
		}
		if got.Facts.Total != dumped || dumped != 6 {
			t.Errorf("code_calls total %d, Dump holds %d calls edges; want both 6", got.Facts.Total, dumped)
		}
		if !reflect.DeepEqual(got.Facts.ByLanguage, want) {
			t.Errorf("code_calls by language %v, want %v", got.Facts.ByLanguage, want)
		}
		if !reflect.DeepEqual(got.Languages, []string{noFileRecord, "python", "typescript"}) {
			t.Errorf("code_calls languages %v", got.Languages)
		}
	})
}
