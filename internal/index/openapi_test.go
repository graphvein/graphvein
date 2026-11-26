package index_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/store"
)

func apiSnapshot(snapshot store.Snapshot) store.Snapshot {
	out := store.Snapshot{}
	for _, n := range snapshot.Nodes {
		if n.Kind == "api_spec" || n.Kind == "api_operation" {
			out.Nodes = append(out.Nodes, n)
		}
	}
	for _, e := range snapshot.Edges {
		if e.Kind == "contains" && (strings.HasPrefix(e.Target, "api_spec:") || strings.HasPrefix(e.Target, "api_operation:")) {
			out.Edges = append(out.Edges, e)
		}
	}
	for _, d := range snapshot.Diagnostics {
		if strings.HasPrefix(d.Code, "openapi_") {
			out.Diagnostics = append(out.Diagnostics, d)
		}
	}
	return out
}

func TestOpenAPIGoldenAndRelinking(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/openapi")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, "_graphvein.yaml"), filepath.Join(root, "graphvein.yaml")); err != nil {
		t.Fatal(err)
	}
	// Git normalizes fixture text to LF. Restore CRLF in the disposable copy
	// so this case exercises scanner normalization on every platform.
	crlf := filepath.Join(root, "crlf-openapi.yaml")
	data, err := os.ReadFile(crlf)
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.ReplaceAll(bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n")), []byte("\n"), []byte("\r\n"))
	if err := os.WriteFile(crlf, data, 0644); err != nil {
		t.Fatal(err)
	}
	write := func(file, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, file), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("source.ts", "export function unchanged() { return 1; }\n")
	s, opt := open(t), options(t)
	_, first, _ := emit(t, root, s, opt)
	dump := func() store.Snapshot {
		t.Helper()
		out, err := s.Dump(context.Background(), "fixture")
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range out.Diagnostics {
			if d.Code == "extract_invalid_record" || d.Code == "invalid_record" || d.Code == "invalid_reference" {
				t.Fatal(d)
			}
		}
		return apiSnapshot(out)
	}
	before := dump()
	for _, d := range before.Diagnostics {
		if d.File == "graphvein.yaml" || d.File == "settings.yaml" || d.File == "toggle.yaml" || strings.Contains(d.Message, "<nil>") {
			t.Fatalf("unexpected non-spec diagnostic or formatting artifact: %+v", d)
		}
	}
	golden.Check(t, "../../testdata/fixtures/openapi.golden.json", before)
	var specs, ops int
	for _, n := range before.Nodes {
		if n.Kind == "api_spec" {
			specs++
		} else {
			ops++
		}
		if n.File == "excluded-openapi.yaml" {
			t.Fatal("exclude failed")
		}
		if n.File == "scoped.yaml" && n.Attributes["scope"] != "test" {
			t.Fatal("scope missing")
		}
	}
	if specs != 17 || ops != 33 {
		t.Fatalf("fixture specs=%d ops=%d", specs, ops)
	}
	_, warm, _ := emit(t, root, s, opt)
	if warm.Parsed != 0 || warm.Reused != first.Parsed || !reflect.DeepEqual(before, dump()) {
		t.Fatal("warm replay changed", first, warm)
	}
	for _, change := range []struct{ file, old, new string }{
		{"basic.yaml", "summary: A pet", "summary: Edited pet"},
		{"fragment.yaml", "summary: Fragment", "summary: Edited fragment"},
		{"graphvein.yaml", "exclude: [excluded-openapi.yaml]", "exclude: [excluded-openapi.yaml, basic.yaml]"},
	} {
		data, err := os.ReadFile(filepath.Join(root, change.file))
		if err != nil {
			t.Fatal(err)
		}
		write(change.file, strings.Replace(string(data), change.old, change.new, 1))
		_, next, _ := emit(t, root, s, opt)
		if next.Parsed != 0 || next.Reused != first.Parsed {
			t.Fatal("parsed-language cache invalidated", next)
		}
		after := dump()
		if reflect.DeepEqual(before, after) {
			t.Fatal("edit did not relink", change.file)
		}
		if change.file == "fragment.yaml" {
			found := false
			for _, n := range after.Nodes {
				if n.File == "basic.yaml" && n.QualifiedName == "GET /ref" {
					found = n.Attributes["summary"] == "Edited fragment"
				}
			}
			if !found {
				t.Fatal("root retained stale fragment")
			}
		}
		if change.file == "graphvein.yaml" {
			for _, n := range after.Nodes {
				if n.File == "basic.yaml" {
					t.Fatal("exclude did not remove root")
				}
			}
		}
		before = after
	}
}
