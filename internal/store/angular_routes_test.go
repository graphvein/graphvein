package store_test

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/store"
)

// TestAngularRoutesRoundTrip stores ui_route nodes and routing edges in every
// backend and replays them without writes. Postgres runs only in CI.
func TestAngularRoutesRoundTrip(t *testing.T) {
	t.Run("angular-routes", func(t *testing.T) {
		backends(t, func(t *testing.T, s store.Store, _ *sql.DB) {
			ctx := context.Background()
			root := t.TempDir()
			if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/angular-routes")); err != nil {
				t.Fatal(err)
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
					t.Fatal("routes replay", summary, writes)
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
					if n.Kind == "ui_route" {
						projection.Nodes = append(projection.Nodes, n)
					}
				}
				for _, e := range snapshot.Edges {
					if e.Kind == "routes_to" || e.Kind == "loads_children" || e.Kind == "contains" && strings.HasPrefix(e.Target, "ui_route:") {
						projection.Edges = append(projection.Edges, e)
					}
				}
				for _, d := range snapshot.Diagnostics {
					if strings.HasPrefix(d.Code, "angular_route_") {
						projection.Diagnostics = append(projection.Diagnostics, d)
					}
				}
				golden.Check(t, "../../testdata/fixtures/angular-routes.golden.json", projection)
			}
		})
	})
}
