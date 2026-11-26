package store_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/store"
)

// TestTauriInvokeRoundTrip stores the linked Tauri fixture scenario (app and
// frontend, see the fixture README) in every backend, compares it with the
// golden's linked projection and replays it without writes. Postgres runs
// only in CI.
func TestTauriInvokeRoundTrip(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/fixtures/tauri-invoke.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var scenarios map[string]any
	if err := json.Unmarshal(raw, &scenarios); err != nil {
		t.Fatal(err)
	}
	want := scenarios["linked"]
	t.Run("tauri-invoke", func(t *testing.T) {
		backends(t, func(t *testing.T, s store.Store, _ *sql.DB) {
			ctx := context.Background()
			root := t.TempDir()
			if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/tauri-invoke")); err != nil {
				t.Fatal(err)
			}
			entries, err := os.ReadDir(root)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range entries {
				if e.Name() != "app" && e.Name() != "frontend" {
					if err := os.RemoveAll(filepath.Join(root, e.Name())); err != nil {
						t.Fatal(err)
					}
				}
			}
			opt := index.Options{Repository: "fixture", GeneratedAt: "2026-11-01T00:00:00Z", Parser: parse.Options{Workers: 1, CacheDir: filepath.Join(t.TempDir(), "cache")}}
			var first []byte
			for run := 0; run < 2; run++ {
				var stream bytes.Buffer
				summary, err := index.Emit(ctx, root, s, &stream, opt)
				if err != nil {
					t.Fatal(err)
				}
				data := append([]byte(nil), stream.Bytes()...)
				writes, err := ingest.StreamWithWrites(ctx, &stream, s)
				if err != nil {
					t.Fatal(err)
				}
				if run == 1 && (!bytes.Equal(first, data) || summary.Parsed != 0 || writes.Files+writes.Nodes+writes.Edges+writes.Deleted != 0) {
					t.Fatal("Tauri invoke replay", summary, writes)
				}
				first = data
				snapshot, err := s.Dump(ctx, "fixture")
				if err != nil {
					t.Fatal(err)
				}
				projection := struct {
					Nodes       []contract.Node
					Edges       []contract.Edge
					Diagnostics []contract.Diagnostic
				}{[]contract.Node{}, []contract.Edge{}, []contract.Diagnostic{}}
				for _, n := range snapshot.Nodes {
					if n.Kind == "ipc_command" || n.Kind == "ipc_call" {
						projection.Nodes = append(projection.Nodes, n)
					}
				}
				for _, e := range snapshot.Edges {
					if e.Kind == "exposes_command" || e.Kind == "invokes_command" || e.Kind == "contains" && (strings.HasPrefix(e.Target, "ipc_command:") || strings.HasPrefix(e.Target, "ipc_call:")) {
						projection.Edges = append(projection.Edges, e)
					}
				}
				for _, d := range snapshot.Diagnostics {
					if strings.HasPrefix(d.Code, "tauri_") {
						projection.Diagnostics = append(projection.Diagnostics, d)
					}
				}
				encoded, err := json.Marshal(projection)
				if err != nil {
					t.Fatal(err)
				}
				var got any
				if err := json.Unmarshal(encoded, &got); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatal("stored Tauri IPC records differ from the golden's linked scenario")
				}
			}
		})
	})
}
