package store_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/store"
)

func TestOpenAPISnapshotParity(t *testing.T) {
	data, err := os.ReadFile("../../testdata/fixtures/openapi.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var expected store.Snapshot
	if err := json.Unmarshal(data, &expected); err != nil {
		t.Fatal(err)
	}
	wantSpecs, wantOps := 0, 0
	for _, n := range expected.Nodes {
		switch n.Kind {
		case "api_spec":
			wantSpecs++
		case "api_operation":
			wantOps++
		}
	}
	if wantSpecs == 0 || wantOps == 0 {
		t.Fatal("OpenAPI golden must contain specs and operations")
	}
	backends(t, func(t *testing.T, s store.Store, _ *sql.DB) {
		root := t.TempDir()
		if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/openapi")); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(filepath.Join(root, "_graphvein.yaml"), filepath.Join(root, "graphvein.yaml")); err != nil {
			t.Fatal(err)
		}
		crlf := filepath.Join(root, "crlf-openapi.yaml")
		data, err := os.ReadFile(crlf)
		if err != nil {
			t.Fatal(err)
		}
		data = bytes.ReplaceAll(bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n")), []byte("\n"), []byte("\r\n"))
		if err := os.WriteFile(crlf, data, 0644); err != nil {
			t.Fatal(err)
		}
		ctx := context.Background()
		emit := func() store.Writes {
			t.Helper()
			var stream bytes.Buffer
			if _, err := index.Emit(ctx, root, s, &stream, index.Options{Repository: "fixture"}); err != nil {
				t.Fatal(err)
			}
			writes, err := ingest.StreamWithWrites(ctx, &stream, s)
			if err != nil {
				t.Fatal(err)
			}
			return writes
		}
		emit()
		out, err := s.Dump(ctx, "fixture")
		if err != nil {
			t.Fatal(err)
		}
		specs, ops := 0, 0
		for _, n := range out.Nodes {
			if n.Kind == "api_spec" {
				specs++
			}
			if n.Kind == "api_operation" {
				ops++
			}
		}
		if specs != wantSpecs || ops != wantOps {
			t.Fatalf("specs=%d ops=%d, want specs=%d ops=%d", specs, ops, wantSpecs, wantOps)
		}
		if w := emit(); w.Files+w.Nodes+w.Edges+w.Deleted != 0 {
			t.Fatal("warm writes", w)
		}
		file := filepath.Join(root, "fragment.yaml")
		data, err = os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(strings.Replace(string(data), "summary: Fragment", "summary: Changed fragment", 1)), 0644); err != nil {
			t.Fatal(err)
		}
		emit()
		out, err = s.Dump(ctx, "fixture")
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, n := range out.Nodes {
			if n.File == "basic.yaml" && n.QualifiedName == "GET /ref" {
				found = n.Attributes["summary"] == "Changed fragment"
			}
		}
		if !found {
			t.Fatal("fragment edit did not propagate through persistence")
		}
	})
}
