package index_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/lang/typescript"
)

func TestAngularRouterSyntax(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/angular-router-syntax")); err != nil {
		t.Fatal(err)
	}
	s, opt := open(t), options(t)
	first, summary, _ := emit(t, root, s, opt)
	load := func() map[string]typescript.RouterSyntax {
		t.Helper()
		snapshot, err := s.Dump(context.Background(), "fixture")
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]typescript.RouterSyntax{}
		for _, f := range snapshot.Files {
			if f.Language != "typescript" {
				continue
			}
			data, err := s.FactCache(context.Background(), "fixture", f.Path, f.ContentHash, fmt.Sprintf("%s/max=%d", index.Version, 2<<20))
			if err != nil {
				t.Fatal(err)
			}
			var payload typescript.CachedFacts
			if err := json.Unmarshal(data, &payload); err != nil || !payload.Router.Valid() {
				t.Fatal(f.Path, err, string(data))
			}
			out[f.Path] = *payload.Router
		}
		return out
	}
	projection := load()
	golden.Check(t, "../../testdata/fixtures/angular-router-syntax.golden.json", projection)
	if len(projection["local.ts"].Calls) != 0 {
		t.Fatal("local factory captured")
	}
	calls := projection["calls.ts"].Calls
	kinds := map[string]int{}
	for _, c := range calls {
		kinds[c.Kind]++
	}
	if !reflect.DeepEqual(kinds, map[string]int{"provide_router": 10, "for_root": 3, "for_child": 4, "bootstrap_module": 2}) {
		t.Fatal("call filters", kinds)
	}
	host, err := os.ReadFile(filepath.Join(root, "calls.ts"))
	if err != nil {
		t.Fatal(err)
	}
	shadows := 0
	for _, c := range calls {
		raw := string(atSpan(host, c.Span))
		if c.Span.StartLine == 19 && (c.Scope != 0 || !reflect.DeepEqual(c.Shadows, []string{"routes"})) {
			t.Fatal("shadowed argument must retain source-only proof", c)
		}
		if c.Span.StartLine == 36 && (c.Scope != 0 || !reflect.DeepEqual(c.Shadows, []string{"M"})) {
			t.Fatal("inline argument lost local shadow", c)
		}
		if c.Span.StartLine == 38 && !reflect.DeepEqual(c.Shadows, []string{}) {
			t.Fatal("module inline argument acquired shadows", c)
		}
		if c.First != nil && c.Span.StartLine < 36 && string(atSpan(host, c.First.Span)) != "routes" && string(atSpan(host, c.First.Span)) != "M" {
			t.Fatal("argument location", c)
		}
		if strings.HasPrefix(raw, "configure(routes, feature()") && c.Arguments != 2 {
			t.Fatal("argument count", c)
		}
		if strings.HasPrefix(raw, "platformBrowserDynamic().bootstrapModule") && (c.Receiver == nil || c.Receiver.Kind != "call_expression" || c.ReceiverCallee == nil || c.ReceiverCallee.Name != "platformBrowserDynamic") {
			t.Fatal("bootstrap receiver", c)
		}
		if len(c.Shadows) > 0 || c.Scope > 0 {
			shadows++
		}
	}
	// A shadowed root must either use its existing local scope or carry a
	// source-only shadow when falling back to module scope.
	if shadows == 0 {
		t.Fatal("missing local resolution scope")
	}
	for _, c := range calls {
		if (c.Span.StartLine == 28 || c.Span.StartLine == 29) && len(c.Shadows) > 0 {
			t.Fatal("default expression became a binding", c)
		}
	}
	loaders := projection["loaders.ts"].Loaders
	forms, reasons := map[string]int{}, map[string]int{}
	loaderHost, err := os.ReadFile(filepath.Join(root, "loaders.ts"))
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range loaders {
		forms[l.Form]++
		reasons[l.Reason]++
		pair, value := string(atSpan(loaderHost, l.Span)), string(atSpan(loaderHost, l.ValueSpan))
		if !strings.HasSuffix(pair, value) || !strings.Contains(pair, l.Key) {
			t.Fatal("loader locations", l)
		}
		if l.Form == "import_member" && (l.Specifier == nil || *l.Specifier != "./targets") {
			t.Fatal("module target", l)
		}
	}
	if !reflect.DeepEqual(forms, map[string]int{"import_default": 2, "import_member": 2, "sync": 4, "inadmissible": 15}) {
		t.Fatal("loader forms", forms, reasons)
	}
	for _, reason := range []string{"not a function", "async", "generator", "dynamic specifier", "then destructuring", "then arguments", "string loadChildren", "other"} {
		if reasons[reason] == 0 {
			t.Error("missing reason", reason)
		}
	}
	for _, l := range loaders[len(loaders)-2:] {
		if l.Form != "sync" || !reflect.DeepEqual(l.Shadows, []string{"Component"}) {
			t.Error("sync shadow", l)
		}
	}
	arrays := projection["arrays.ts"].Arrays
	for _, l := range projection["empty.ts"].Loaders {
		if l.Specifier == nil || *l.Specifier != "" || l.Form == "inadmissible" {
			t.Fatal("empty literal specifier lost", l)
		}
	}
	if len(arrays) != 2 || arrays[0].Name != "default" || arrays[1].Name != "checked" {
		t.Fatal("additional arrays", arrays)
	}
	for file, p := range projection {
		if file != "arrays.ts" && len(p.Arrays) != 0 {
			t.Fatal("duplicated declaration array", file)
		}
	}
	again, replay, writes := emit(t, root, s, opt)
	if !bytes.Equal(first, again) || replay.Parsed != 0 || replay.Reused != summary.Parsed || writes.Files+writes.Nodes+writes.Edges+writes.Deleted != 0 || !reflect.DeepEqual(projection, load()) {
		t.Fatal("router replay", replay, writes)
	}
	for _, name := range []string{"calls.ts", "local.ts", "loaders.ts", "arrays.ts", "targets.ts", "empty.ts"} {
		p := filepath.Join(root, name)
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, bytes.ReplaceAll(data, []byte("\n"), []byte("\r\n")), 0644); err != nil {
			t.Fatal(err)
		}
	}
	s = open(t)
	emit(t, root, s, opt)
	if !reflect.DeepEqual(projection, load()) {
		t.Fatal("CRLF changed facts")
	}
	// The new payload is mandatory even when the aggregate version matches.
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range snapshot.Files {
		if f.Path != "calls.ts" {
			continue
		}
		version := fmt.Sprintf("%s/max=%d", index.Version, 2<<20)
		data, err := s.FactCache(context.Background(), "fixture", f.Path, f.ContentHash, version)
		if err != nil {
			t.Fatal(err)
		}
		for _, field := range []string{"router", "calls", "loaders", "arrays", "version"} {
			var raw map[string]json.RawMessage
			if err := json.Unmarshal(data, &raw); err != nil {
				t.Fatal(err)
			}
			if field == "router" {
				delete(raw, field)
			} else {
				var router map[string]json.RawMessage
				json.Unmarshal(raw["router"], &router)
				delete(router, field)
				raw["router"], _ = json.Marshal(router)
			}
			broken, _ := json.Marshal(raw)
			if err := s.SaveFactCache(context.Background(), "fixture", f.Path, f.ContentHash, version, broken); err != nil {
				t.Fatal(err)
			}
			_, run, _ := emit(t, root, s, opt)
			if run.Parsed != 1 {
				t.Fatal("incomplete router payload replayed", field, run)
			}
		}
	}
}
