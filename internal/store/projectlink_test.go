package store_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/projectlink"
	"github.com/graphvein/graphvein/internal/store"
)

// TestProjectLinkPassRoundTrip runs the project link pass (T022 B3) over
// the projectlink fixture on both backends: the link set is stored and read
// back unchanged through the B2 methods, an unchanged second pass changes
// nothing, and a set computed before a member was indexed again is refused
// as stale.
func TestProjectLinkPassRoundTrip(t *testing.T) {
	backends(t, func(t *testing.T, s store.Store, _ *sql.DB) {
		ctx := context.Background()
		root := t.TempDir()
		if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/projectlink")); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(filepath.Join(root, "frontend", "openapi.yaml.fixture"), filepath.Join(root, "frontend", "openapi.yaml")); err != nil {
			t.Fatal(err)
		}
		emit := func(member string) {
			t.Helper()
			var stream bytes.Buffer
			if _, err := index.Emit(ctx, filepath.Join(root, member), s, &stream, index.Options{Repository: member}); err != nil {
				t.Fatal(err)
			}
			if _, err := ingest.StreamWithWrites(ctx, bytes.NewReader(stream.Bytes()), s); err != nil {
				t.Fatal(err)
			}
		}
		for _, m := range []string{"frontend", "backend", "backend2"} {
			emit(m)
		}
		data, err := os.ReadFile(filepath.Join(root, "scenarios", "selector.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		project, err := config.ParseProject(root, data, nil)
		if err != nil {
			t.Fatal(err)
		}
		result, run, err := projectlink.Run(ctx, s, project, "sha256:selector")
		if err != nil {
			t.Fatal(err)
		}
		// Eight links: the fixture's selector golden (testdata/fixtures/projectlink.selector.golden.json).
		if len(result.Set.Links) != 8 || run.Added != 8 || run.Seq != 1 {
			t.Fatalf("first pass: %d links, journal %+v", len(result.Set.Links), run)
		}
		stored, err := s.ProjectLinks(ctx, project.Name)
		if err != nil {
			t.Fatal(err)
		}
		if linkJSON(t, stored.Links) != linkJSON(t, result.Set.Links) || linkJSON(t, stored.Members) != linkJSON(t, result.Set.Members) || stored.ManifestHash != "sha256:selector" {
			t.Fatalf("stored set differs:\n%s\nwant\n%s", linkJSON(t, stored), linkJSON(t, result.Set))
		}
		_, again, err := projectlink.Run(ctx, s, project, "sha256:selector")
		if err != nil {
			t.Fatal(err)
		}
		if again.Seq != 2 || again.Added+again.Removed+again.Changed != 0 || len(again.Links) != 8 {
			t.Fatalf("second pass journal %+v", again)
		}
		emit("backend")
		if _, err := s.ReplaceProjectLinks(ctx, result.Set); !errors.Is(err, store.ErrProjectStale) {
			t.Fatalf("stale set accepted: %v", err)
		}
		runs, err := s.ProjectLinkRuns(ctx, project.Name)
		if err != nil {
			t.Fatal(err)
		}
		if len(runs) != 2 {
			t.Fatalf("journal has %d passes, want 2", len(runs))
		}
	})
}

// TestProjectLinkScopeOverlayRoundTrip stores the link scope scenario of
// the projectlink fixture (T022 B5) on both backends: overlay links whose
// source and target are in the same member, links into one scope of a
// member, and every outcome are read back unchanged, the member's own
// snapshot is not touched, and an unchanged second pass changes nothing.
func TestProjectLinkScopeOverlayRoundTrip(t *testing.T) {
	backends(t, func(t *testing.T, s store.Store, _ *sql.DB) {
		ctx := context.Background()
		root := t.TempDir()
		if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/projectlink")); err != nil {
			t.Fatal(err)
		}
		for _, dir := range []string{"frontend", "monorepo/admin"} {
			if err := os.Rename(filepath.Join(root, dir, "openapi.yaml.fixture"), filepath.Join(root, dir, "openapi.yaml")); err != nil {
				t.Fatal(err)
			}
		}
		for _, member := range []string{"frontend", "monorepo"} {
			var stream bytes.Buffer
			if _, err := index.Emit(ctx, filepath.Join(root, member), s, &stream, index.Options{Repository: member}); err != nil {
				t.Fatal(err)
			}
			if _, err := ingest.StreamWithWrites(ctx, bytes.NewReader(stream.Bytes()), s); err != nil {
				t.Fatal(err)
			}
		}
		before, err := s.Dump(ctx, "monorepo")
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(root, "scenarios", "monorepo.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		project, err := config.ParseProject(root, data, nil)
		if err != nil {
			t.Fatal(err)
		}
		result, run, err := projectlink.Run(ctx, s, project, "sha256:monorepo")
		if err != nil {
			t.Fatal(err)
		}
		// Ten links, four of them scope overlays inside monorepo: the
		// fixture's golden (testdata/fixtures/projectlink.monorepo.golden.json).
		overlays := 0
		for _, l := range result.Set.Links {
			if l.Source.Repository == "monorepo" && l.Target.Repository == "monorepo" {
				overlays++
			}
		}
		if len(result.Set.Links) != 10 || overlays != 4 || run.Added != 10 {
			t.Fatalf("first pass: %d links (%d overlays), journal %+v", len(result.Set.Links), overlays, run)
		}
		stored, err := s.ProjectLinks(ctx, project.Name)
		if err != nil {
			t.Fatal(err)
		}
		if linkJSON(t, stored.Links) != linkJSON(t, result.Set.Links) || linkJSON(t, stored.Outcomes) != linkJSON(t, result.Outcomes) {
			t.Fatalf("stored set differs:\n%s\nwant\n%s", linkJSON(t, stored), linkJSON(t, result.Set))
		}
		after, err := s.Dump(ctx, "monorepo")
		if err != nil {
			t.Fatal(err)
		}
		if linkJSON(t, after) != linkJSON(t, before) {
			t.Fatal("the link pass changed monorepo's own snapshot")
		}
		_, again, err := projectlink.Run(ctx, s, project, "sha256:monorepo")
		if err != nil {
			t.Fatal(err)
		}
		if again.Added+again.Removed+again.Changed != 0 || len(again.Links) != 10 {
			t.Fatalf("second pass journal %+v", again)
		}
	})
}
