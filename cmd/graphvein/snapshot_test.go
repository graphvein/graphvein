package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/store"
)

// treeDigest describes every entry under root (path, mode, content hash),
// so equal digests mean a byte-identical tree.
func treeDigest(t *testing.T, root string) string {
	t.Helper()
	var b strings.Builder
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		fmt.Fprintf(&b, "%s %s", filepath.ToSlash(rel), info.Mode())
		if d.Type().IsRegular() {
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			fmt.Fprintf(&b, " %x", sha256.Sum256(data))
		}
		b.WriteByte('\n')
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestStoreDir(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GRAPHVEIN_POSTGRES_URL", "")
	t.Setenv("GRAPHVEIN_SQLITE_PATH", "")
	if got, want := storeDir(root), filepath.Join(root, ".graphvein"); got != want {
		t.Fatalf("default: %q, want %q", got, want)
	}
	external := filepath.Join(t.TempDir(), "stores", "index.db")
	t.Setenv("GRAPHVEIN_SQLITE_PATH", external)
	if got, want := storeDir(root), filepath.Dir(external); got != want {
		t.Fatalf("GRAPHVEIN_SQLITE_PATH: %q, want %q", got, want)
	}
	t.Setenv("GRAPHVEIN_POSTGRES_URL", "postgres://localhost/unused")
	if got := storeDir(root); got != "" {
		t.Fatalf("Postgres: %q, want the OS temporary directory", got)
	}
}

// TestIndexWithExternalStoreLeavesRootUntouched: graphvein index with
// GRAPHVEIN_SQLITE_PATH outside the repository writes its temporary
// snapshot next to the store, so the indexed tree stays byte-identical
// during and after the run.
func TestIndexWithExternalStoreLeavesRootUntouched(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/repos/go")); err != nil {
		t.Fatal(err)
	}
	storeDirectory := t.TempDir()
	t.Setenv("GRAPHVEIN_POSTGRES_URL", "")
	t.Setenv("GRAPHVEIN_SQLITE_PATH", filepath.Join(storeDirectory, "index.db"))
	before := treeDigest(t, root)
	var during string
	var snapshots []string
	emit := func(ctx context.Context, r string, s store.Store, w io.Writer, o index.Options) (index.Summary, error) {
		summary, err := index.Emit(ctx, r, s, w, o)
		during = treeDigest(t, root)
		snapshots, _ = filepath.Glob(filepath.Join(storeDirectory, "snapshot-*.ndjson"))
		return summary, err
	}
	if code, out, errOut := runIndex(t, emit, "index", root, "--repository", "fixture"); code != 0 {
		t.Fatalf("index = %d\n%s\n%s", code, out, errOut)
	}
	if during != before || treeDigest(t, root) != before {
		t.Fatalf("the run changed the indexed tree:\nduring\n%s\nafter\n%s\nwant\n%s", during, treeDigest(t, root), before)
	}
	if len(snapshots) != 1 {
		t.Fatalf("snapshots next to the store during the run: %v, want one", snapshots)
	}
	if left, _ := filepath.Glob(filepath.Join(storeDirectory, "snapshot-*")); len(left) != 0 {
		t.Fatalf("temporary snapshot left: %v", left)
	}
}

// TestIndexDefaultLayoutRemovesSnapshot: without GRAPHVEIN_SQLITE_PATH the
// store and the temporary snapshot are in <root>/.graphvein, and only the
// store remains there.
func TestIndexDefaultLayoutRemovesSnapshot(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/repos/go")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GRAPHVEIN_POSTGRES_URL", "")
	t.Setenv("GRAPHVEIN_SQLITE_PATH", "")
	dir := filepath.Join(root, ".graphvein")
	var snapshots []string
	emit := func(ctx context.Context, r string, s store.Store, w io.Writer, o index.Options) (index.Summary, error) {
		snapshots, _ = filepath.Glob(filepath.Join(dir, "snapshot-*.ndjson"))
		return index.Emit(ctx, r, s, w, o)
	}
	if code, out, errOut := runIndex(t, emit, "index", root, "--repository", "fixture"); code != 0 {
		t.Fatalf("index = %d\n%s\n%s", code, out, errOut)
	}
	if len(snapshots) != 1 {
		t.Fatalf("snapshots in %s during the run: %v, want one", dir, snapshots)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "snapshot-*")); len(left) != 0 {
		t.Fatalf("temporary snapshot left: %v", left)
	}
	if _, err := os.Stat(filepath.Join(dir, "index.db")); err != nil {
		t.Fatal(err)
	}
}

// TestProjectIndexLeavesMembersUntouched: index --project writes each
// member's temporary snapshot next to the project store (or the OS
// temporary directory for Postgres), never into a member checkout.
func TestProjectIndexLeavesMembersUntouched(t *testing.T) {
	projectBackends(t, func(t *testing.T, _ func() store.Store) {
		root := copyProjectFixture(t, twoManifest)
		before := treeDigest(t, root)
		if code, out, errOut := runIndex(t, nil, "index", "--project", root); code != 0 || errOut != "" {
			t.Fatalf("index --project = %d\n%s\n%s", code, out, errOut)
		}
		if after := treeDigest(t, root); after != before {
			t.Fatalf("index --project changed the checkouts:\n%s\nwant\n%s", after, before)
		}
	})
}
