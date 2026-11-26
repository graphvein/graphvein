package clients_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

func TestEnvironmentWrappersGoldenAndReplay(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../../testdata/fixtures/env-wrappers")); err != nil {
		t.Fatal(err)
	}
	s, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	options := index.Options{Repository: "fixture", GeneratedAt: "2026-10-24T00:00:00Z", Parser: parse.Options{CacheDir: filepath.Join(t.TempDir(), "wasm")}}
	var first bytes.Buffer
	if _, err := index.Emit(ctx, root, s, &first, options); err != nil {
		t.Fatal(err)
	}
	if err := ingest.Stream(ctx, bytes.NewReader(first.Bytes()), s); err != nil {
		t.Fatal(err)
	}
	snapshot, err := s.Dump(ctx, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	edges := map[string][]contract.Edge{}
	for _, edge := range snapshot.Edges {
		if edge.Kind == "calls_route" {
			edges[edge.Source] = append(edges[edge.Source], edge)
		}
	}
	var projection []map[string]any
	byLine := map[int]contract.Node{}
	for _, node := range snapshot.Nodes {
		if node.Kind != "client_call" {
			continue
		}
		projection = append(projection, map[string]any{"file": node.File, "line": node.Span.StartLine, "identity": node.Identity, "attributes": node.Attributes, "edges": edges[node.Identity]})
		if node.File == "frontend.ts" {
			byLine[node.Span.StartLine] = node
		}
	}
	// Independent expectations keep golden updates from blessing method loss.
	methods := map[int]string{7: "GET", 8: "GET", 9: "POST", 10: "PATCH", 11: "DELETE", 14: "POST", 16: "POST", 17: "GET", 18: "GET", 19: "GET", 20: "POST", 21: "GET", 22: "GET", 23: "POST", 24: "PATCH", 25: "DELETE", 27: "POST", 28: "POST", 30: "POST", 31: "PATCH", 36: "POST", 38: "POST", 39: "GET", 40: "GET", 41: "GET", 42: "GET", 43: "PATCH", 44: "POST", 46: "DELETE", 47: "POST"}
	for line, method := range methods {
		if byLine[line].Attributes["method"] != method {
			t.Errorf("frontend.ts:%d method=%v, want %s", line, byLine[line].Attributes["method"], method)
		}
	}
	for _, line := range []int{12, 13, 15, 29, 37, 51, 52, 76} {
		if n, exists := byLine[line]; !exists || n.Attributes["method"] != nil || n.Attributes["status"] != "dynamic" || len(edges[n.Identity]) != 0 {
			t.Errorf("frontend.ts:%d must have an unknown method and no edge: %+v", line, n)
		}
	}
	if byLine[23].Attributes["status"] != "dynamic" || len(edges[byLine[23].Identity]) != 0 {
		t.Error("relative invocation cannot discard its unresolved prefix")
	}
	for _, line := range []int{26, 32, 33} {
		if _, exists := byLine[line]; exists {
			t.Errorf("unsupported wrapper invocation at frontend.ts:%d emitted a node", line)
		}
	}
	for line, method := range map[int]string{55: "GET", 58: "POST", 59: "GET", 74: "GET", 75: "POST"} {
		n := byLine[line]
		if n.Attributes["status"] != "linked" || n.Attributes["method"] != method || len(edges[n.Identity]) != 1 {
			t.Errorf("frontend.ts:%d must remain a linked request with method %s: %+v", line, method, n)
		}
	}
	if n := byLine[60]; n.Attributes["method"] != nil || n.Attributes["status"] != "dynamic" {
		t.Fatal("unknown spread was lost through options.method", n)
	}
	if got := byLine[63].Attributes["base_expression"]; got != "(process.env.APIasstring)" {
		t.Fatal("asserted base lost its request-site spelling", got)
	}
	for line, method := range map[int]string{67: "GET", 71: "DELETE"} {
		if got := byLine[line].Attributes["method"]; got != method {
			t.Errorf("frontend.ts:%d lexical undefined method=%v, want %s", line, got, method)
		}
	}
	golden.Check(t, "../../../testdata/fixtures/env-wrappers.golden.json", projection)
	var second bytes.Buffer
	summary, err := index.Emit(ctx, root, s, &second, options)
	if err != nil || summary.Parsed != 0 || !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatalf("cache replay differed: %+v %v", summary, err)
	}
	for _, diagnostic := range snapshot.Diagnostics {
		if diagnostic.Code == "extract_failed" || diagnostic.Code == "extract_invalid_record" || diagnostic.Code == "parse_failed" {
			t.Fatal(diagnostic)
		}
	}
	// A changed imported default must relink callers whose syntax was reused.
	file := filepath.Join(root, "transport.ts")
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.Replace(data, []byte("const POST = {method: 'POST'}"), []byte("const POST = {method: 'PATCH'}"), 1)
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	var changed bytes.Buffer
	if _, err := index.Emit(ctx, root, s, &changed, options); err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(first.Bytes(), changed.Bytes()) {
		t.Fatal("imported default change did not relink")
	}
	for _, line := range bytes.Split(changed.Bytes(), []byte("\n")) {
		var node contract.Node
		if json.Unmarshal(line, &node) == nil && node.Kind == "client_call" && node.File == "frontend.ts" && node.Span.StartLine == 27 && node.Attributes["method"] != "PATCH" {
			t.Fatal("cached caller retained stale default", node)
		}
	}
}
