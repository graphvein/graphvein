package store_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/graphvein/graphvein/internal/contract"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/query"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/traverse"
)

func TestCLIFixtures(t *testing.T) {
	var cases []struct{ Language, Term, Target, Ambiguous, File string }
	data, err := os.ReadFile("../../testdata/repos/cli-cases.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.Language, func(t *testing.T) {
			backends(t, func(t *testing.T, s store.Store, _ *sql.DB) {
				ctx := context.Background()
				root := t.TempDir()
				if err := os.CopyFS(root, os.DirFS("../../testdata/repos/"+tc.Language)); err != nil {
					t.Fatal(err)
				}
				if tc.Language == "typescript" {
					for _, p := range [][2]string{{"_graphveinignore", ".graphveinignore"}, {"_claude", ".claude"}, {"nested/_gitignore", "nested/.gitignore"}, {"nested/_skip.ts", "nested/skip.ts"}} {
						if err := os.Rename(filepath.Join(root, p[0]), filepath.Join(root, p[1])); err != nil {
							t.Fatal(err)
						}
					}
				}
				var stream bytes.Buffer
				if _, err := index.Emit(ctx, root, s, &stream, index.Options{Repository: "fixture", Parser: parse.Options{CacheDir: filepath.Join(t.TempDir(), "cache")}}); err != nil {
					t.Fatal(err)
				}
				if err := ingest.Stream(ctx, &stream, s); err != nil {
					t.Fatal(err)
				}
				before, err := s.Dump(ctx, "fixture")
				if err != nil {
					t.Fatal(err)
				}
				var outputs []query.Response
				fileTarget, _ := contract.Identity("file", tc.File, tc.File)
				for _, opt := range []query.Options{{Command: "query", Term: tc.Term, Limit: 50}, {Command: "callers", Term: tc.Target, Depth: 2}, {Command: "impact", Term: tc.Target, Depth: 3}, {Command: "impact", Term: tc.Ambiguous, Depth: 3}, {Command: "impact", Term: fileTarget, Depth: 3}, {Command: "callers", Term: "absent", Depth: 1}} {
					opt.Root = root
					opt.Repository = "fixture"
					out, err := query.Execute(ctx, s, opt)
					if err != nil {
						t.Fatal(err)
					}
					outputs = append(outputs, out)
				}
				if len(outputs[0].Nodes) == 0 || len(outputs[1].Results) == 0 {
					t.Fatal("missing query/callers")
				}
				multi := false
				for _, r := range outputs[2].Results {
					multi = multi || r.Distance > 1
					if len(r.Path) != r.Distance {
						t.Fatal("edge path distance", r)
					}
				}
				if !multi || outputs[3].Error != "ambiguous target" || len(outputs[3].Candidates) < 2 || outputs[4].Error != "" || outputs[5].Error != "no matching target" {
					t.Fatal("fixture lacks required traversal/lookup cases", outputs)
				}
				if outputs[2].PossiblyMissingCallers < 1 || outputs[2].PossiblyMissingCallSites[0].File != map[string]string{"go": "blind.go", "python": "blind.py", "rust": "src/blind.rs", "typescript": "src/blind.ts"}[tc.Language] {
					t.Fatal("missing repository-wide name match", outputs[2])
				}
				golden.CheckRoot(t, "../../testdata/repos/"+tc.Language+".cli.golden.json", outputs, root)
				after, err := s.Dump(ctx, "fixture")
				if err != nil || !reflect.DeepEqual(before, after) {
					t.Fatal("read modified graph", err)
				}
				// Combined filters, case-sensitive names and identity-set edge reads.
				filtered, err := s.Nodes(ctx, "fixture", store.Query{Identity: tc.Target, Term: tc.Term, Limit: 1})
				if err != nil || len(filtered) != 1 {
					t.Fatal(filtered, err)
				}
				wrong, err := s.Nodes(ctx, "other", store.Query{Term: tc.Term})
				if err != nil || len(wrong) != 0 {
					t.Fatal("repository leak", wrong, err)
				}
				wrong, err = s.Nodes(ctx, "fixture", store.Query{Term: "HELPER"})
				if err != nil || len(wrong) != 0 {
					t.Fatal("case folding", wrong, err)
				}
				incoming, err := s.Edges(ctx, "fixture", []string{tc.Target, tc.Target}, []string{"calls"}, store.Incoming)
				if err != nil || len(incoming) == 0 {
					t.Fatal(incoming, err)
				}
				outgoing, err := s.Edges(ctx, "fixture", []string{incoming[0].Source}, []string{"calls"}, store.Outgoing)
				if err != nil || len(outgoing) == 0 {
					t.Fatal(outgoing, err)
				}
				if _, err := s.Edges(ctx, "fixture", nil, nil, "invalid"); err == nil {
					t.Fatal("invalid direction accepted")
				}
				if _, err := s.Nodes(ctx, "fixture", store.Query{Limit: -1}); err == nil {
					t.Fatal("negative limit accepted")
				}
				if err := os.WriteFile(filepath.Join(root, "added.txt"), []byte("added\n"), 0644); err != nil {
					t.Fatal(err)
				}
				p := filepath.Join(root, tc.File)
				old, err := os.ReadFile(p)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, append(old, []byte("\n")...), 0644); err != nil {
					t.Fatal(err)
				}
				deleted := ""
				for _, f := range before.Files {
					if f.Path != tc.File && !strings.HasPrefix(filepath.Base(f.Path), ".") {
						deleted = f.Path
						break
					}
				}
				if err := os.Remove(filepath.Join(root, filepath.FromSlash(deleted))); err != nil {
					t.Fatal(err)
				}
				stale, err := query.Execute(ctx, s, query.Options{Command: "query", Term: tc.Term, Root: root, Repository: "fixture", Limit: 50})
				if err != nil || len(stale.Freshness.Changed) != 3 {
					t.Fatal(stale, err)
				}
			})
		})
	}
}

func TestTraversalCyclesAndBlindSpots(t *testing.T) {
	backends(t, func(t *testing.T, s store.Store, _ *sql.DB) {
		ctx := context.Background()
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "a.js"), []byte("function target() {}\nfunction first() { target(); second(); }\nfunction second() { target(); first(); }\nfunction far() { first(); second(); }\n"), 0644); err != nil {
			t.Fatal(err)
		}
		var stream bytes.Buffer
		if _, err := index.Emit(ctx, root, s, &stream, index.Options{Repository: "fixture", Parser: parse.Options{CacheDir: filepath.Join(t.TempDir(), "cache")}}); err != nil {
			t.Fatal(err)
		}
		if err := ingest.Stream(ctx, &stream, s); err != nil {
			t.Fatal(err)
		}
		snapshot, err := s.Dump(ctx, "fixture")
		if err != nil {
			t.Fatal(err)
		}
		tx, err := s.BeginRun(ctx, contract.Header{Type: "header", ContractVersion: 1, Indexer: contract.Indexer{Name: "test", Version: "1"}, Repository: "fixture", GeneratedAt: "2026-10-05T00:00:00Z"})
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		for _, f := range snapshot.Files {
			if err := tx.UpsertFile(ctx, f); err != nil {
				t.Fatal(err)
			}
		}
		for _, n := range snapshot.Nodes {
			if n.Kind == "file" {
				n.Attributes["unresolved_calls_unknown"] = 7
				n.Attributes["unresolved_calls_external"] = 200
			}
			if err := tx.UpsertNode(ctx, n); err != nil {
				t.Fatal(err)
			}
		}
		for _, e := range snapshot.Edges {
			if err := tx.UpsertEdge(ctx, e); err != nil {
				t.Fatal(err)
			}
		}
		reference := contract.Edge{Type: "edge", Kind: "references", Source: "function:a.js#far", Target: "function:a.js#target", Provenance: "inferred", Confidence: 0.75, Evidence: []contract.Evidence{{File: "a.js", Span: contract.Span{StartLine: 4, EndLine: 4}}}}
		if err := tx.UpsertEdge(ctx, reference); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		for _, command := range []string{"callers", "impact"} {
			out, err := query.Execute(ctx, s, query.Options{Command: command, Term: "target", Root: root, Repository: "fixture", Depth: 3})
			if err != nil {
				t.Fatal(err)
			}
			if len(out.Results) != 3 {
				t.Fatal("cycle/diamond duplicates", out)
			}
			for _, r := range out.Results {
				if r.Node.Name == "far" {
					want := 2
					if command == "impact" {
						want = 1
					}
					if r.Distance != want {
						t.Fatal("shortest path", r)
					}
					if command == "impact" && r.Path[0].Confidence != 0.75 {
						t.Fatal("reference provenance lost", r)
					}
				}
			}
			if command == "impact" && (out.PossiblyMissingCallers != 0 || out.UnknownCallsInResultFiles != 7 || !out.BlindSpotData) {
				t.Fatal("file count duplicated or external included", out)
			}
		}
		ids := []string{"function:a.js#target", "function:a.js#target"}
		for i := 0; i < 300; i++ {
			ids = append(ids, fmt.Sprintf("absent%d", i))
		}
		edges, err := s.Edges(ctx, "fixture", ids, []string{"calls", "references"}, store.Incoming)
		if err != nil || len(edges) != 3 {
			t.Fatal("chunked edge set", edges, err)
		}
		for _, tc := range []struct {
			filter store.Query
			count  int
		}{{store.Query{Term: "irst", Kind: "function", File: "a.js", Limit: 1}, 1}, {store.Query{Term: "IRST"}, 0}, {store.Query{Term: "%"}, 0}, {store.Query{Term: "first", File: "other.js"}, 0}} {
			n, err := s.Nodes(ctx, "fixture", tc.filter)
			if err != nil || len(n) != tc.count {
				t.Fatal(tc, n, err)
			}
		}
	})
}

func TestExternalNodesWithoutFileNodes(t *testing.T) {
	backends(t, func(t *testing.T, s store.Store, _ *sql.DB) {
		ctx := context.Background()
		root := t.TempDir()
		source := []byte("function target() {}\n")
		if err := os.WriteFile(filepath.Join(root, "a.js"), source, 0644); err != nil {
			t.Fatal(err)
		}
		tx, err := s.BeginRun(ctx, contract.Header{Type: "header", ContractVersion: 1, Indexer: contract.Indexer{Name: "external", Version: "1"}, Repository: "fixture", GeneratedAt: "2026-10-05T00:00:00Z"})
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		file := contract.File{Type: "file", Path: "a.js", Language: "javascript", Precision: "best_effort", Size: int64(len(source)), ContentHash: contract.ContentHash(source)}
		if err := tx.UpsertFile(ctx, file); err != nil {
			t.Fatal(err)
		}
		span := contract.Span{StartLine: 1, EndLine: 1, EndColumn: len(source) - 1}
		node := contract.Node{Type: "node", Identity: "function:a.js#target", Kind: "function", Level: 4, Name: "target", QualifiedName: "target", File: "a.js", Span: &span, ContentHash: contract.ContentHash(source[:len(source)-1]), Attributes: map[string]any{}}
		if err := tx.UpsertNode(ctx, node); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		out, err := query.Execute(ctx, s, query.Options{Command: "impact", Term: "target", Root: root, Repository: "fixture", Depth: 1})
		if err != nil || out.Target == nil || out.Target.IDE == nil || !out.Target.IDE.Exact || out.BlindSpotData || out.PossiblyMissingCallers != 0 {
			t.Fatal(out, err)
		}
		if err := os.WriteFile(filepath.Join(root, "a.js"), append(source, []byte("// changed\n")...), 0644); err != nil {
			t.Fatal(err)
		}
		out, err = query.Execute(ctx, s, query.Options{Command: "impact", Term: "target", Root: root, Repository: "fixture", Depth: 1})
		if err != nil || out.Target.IDE.Exact {
			t.Fatal(out, err)
		}
	})
}

// TestTraversalCapsAndEnds checks the shared traversal caps of callers and
// impact (T030 D2): a fan-out over traverse.MaxFanOut and a depth cut are
// reported where they bound, a function with no stored caller is a "? not
// modelled" end (G10), a depth above the cap is refused, and query reports
// its total (G1).
func TestTraversalCapsAndEnds(t *testing.T) {
	backends(t, func(t *testing.T, s store.Store, _ *sql.DB) {
		ctx := context.Background()
		root := t.TempDir()
		var src strings.Builder
		src.WriteString("function target() {}\nfunction outer() { inner(); }\nfunction inner() { target(); }\n")
		for i := 0; i < traverse.MaxFanOut+5; i++ {
			fmt.Fprintf(&src, "function caller%03d() { target(); }\n", i)
		}
		if err := os.WriteFile(filepath.Join(root, "a.js"), []byte(src.String()), 0644); err != nil {
			t.Fatal(err)
		}
		var stream bytes.Buffer
		if _, err := index.Emit(ctx, root, s, &stream, index.Options{Repository: "fixture", Parser: parse.Options{CacheDir: filepath.Join(t.TempDir(), "cache")}}); err != nil {
			t.Fatal(err)
		}
		if err := ingest.Stream(ctx, &stream, s); err != nil {
			t.Fatal(err)
		}
		out, err := query.Execute(ctx, s, query.Options{Command: "callers", Term: "target", Root: root, Repository: "fixture", Depth: 1})
		if err != nil {
			t.Fatal(err)
		}
		if len(out.Results) != traverse.MaxFanOut {
			t.Fatalf("results %d, want the fan-out cap %d", len(out.Results), traverse.MaxFanOut)
		}
		// caller200..204 and inner are kept out; the callers reached have
		// no caller, so the depth cap does not bind.
		wantBounds := []traverse.Bound{{Cap: traverse.CapFanOut, Step: 1, Beyond: 6}}
		if !reflect.DeepEqual(out.Bounds, wantBounds) || !reflect.DeepEqual(out.Caps, []string{traverse.CapFanOut}) {
			t.Fatalf("bounds %+v caps %v, want %+v", out.Bounds, out.Caps, wantBounds)
		}
		out, err = query.Execute(ctx, s, query.Options{Command: "callers", Term: "inner", Root: root, Repository: "fixture", Depth: 3})
		if err != nil {
			t.Fatal(err)
		}
		if len(out.Results) != 1 || out.Results[0].Node.Name != "outer" || len(out.Caps) != 0 {
			t.Fatalf("callers of inner %+v caps %v", out.Results, out.Caps)
		}
		if len(out.Ends) != 1 || out.Ends[0].Node != "function:a.js#outer" || out.Ends[0].Text != query.CallersEndText {
			t.Fatalf("ends %+v, want outer as the one end", out.Ends)
		}
		out, err = query.Execute(ctx, s, query.Options{Command: "callers", Term: "inner", Root: root, Repository: "fixture", Depth: 1})
		if err != nil {
			t.Fatal(err)
		}
		if len(out.Bounds) != 0 || len(out.Results) != 1 {
			// outer has no caller: nothing lies beyond depth 1, so the
			// depth cap does not bind.
			t.Fatalf("bounds %+v results %+v", out.Bounds, out.Results)
		}
		if _, err := query.Execute(ctx, s, query.Options{Command: "impact", Term: "inner", Root: root, Repository: "fixture", Depth: query.MaxDepth + 1}); err == nil {
			t.Fatal("depth above the cap accepted")
		}
		out, err = query.Execute(ctx, s, query.Options{Command: "query", Term: "target", Root: root, Repository: "fixture", Limit: 1})
		if err != nil || out.Total == nil || *out.Total != 1 || len(out.Nodes) != 1 {
			t.Fatal("query total", out, err)
		}
		out, err = query.Execute(ctx, s, query.Options{Command: "query", Term: "caller000", Root: root, Repository: "fixture", Kind: "function", Limit: 1})
		if err != nil || out.Total == nil || *out.Total != 1 {
			t.Fatal("query total", out, err)
		}
		out, err = query.Execute(ctx, s, query.Options{Command: "query", Term: "", Root: root, Repository: "fixture", Kind: "function", Limit: 2})
		if err != nil || out.Total == nil || *out.Total != traverse.MaxFanOut+8 || len(out.Nodes) != 2 {
			t.Fatal("query total of a cut list", out.Total, len(out.Nodes), err)
		}
		// A freshness check the caller already made (T030 MCP-2) is
		// reported as given: the checkout is not scanned again, so a file
		// added since does not appear, while the stored hashes still prove
		// the unchanged file's editor link exact.
		if err := os.WriteFile(filepath.Join(root, "b.js"), []byte("function later() {}\n"), 0644); err != nil {
			t.Fatal(err)
		}
		given := &query.Freshness{Indexed: true, Changed: []string{"given.js"}}
		out, err = query.Execute(ctx, s, query.Options{Command: "query", Term: "target", Root: root, Repository: "fixture", Fresh: given})
		if err != nil || !reflect.DeepEqual(out.Freshness.Changed, []string{"given.js"}) || len(out.Nodes) != 1 || out.Nodes[0].IDE == nil || !out.Nodes[0].IDE.Exact {
			t.Fatal("precomputed freshness", out.Freshness, out.Nodes, err)
		}
		out, err = query.Execute(ctx, s, query.Options{Command: "query", Term: "target", Root: root, Repository: "fixture"})
		if err != nil || !reflect.DeepEqual(out.Freshness.Changed, []string{"b.js"}) {
			t.Fatal("own freshness scan", out.Freshness, err)
		}
	})
}
