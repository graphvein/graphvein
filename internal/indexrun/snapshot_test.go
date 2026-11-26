package indexrun

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

func TestSnapshotLocation(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "repo")
	outside := filepath.Join(base, "stores", "repo")
	cases := []struct {
		name, dir, want string
	}{
		{"no file store", "", ""},
		{"default layout", filepath.Join(root, ".graphvein"), filepath.Join(root, ".graphvein")},
		{"inside .graphvein", filepath.Join(root, ".graphvein", "x"), filepath.Join(root, ".graphvein", "x")},
		{"outside the root", outside, outside},
		{"sibling with the root as prefix", root + "-stores", root + "-stores"},
		{"the root itself", root, ""},
		{"elsewhere in the root", filepath.Join(root, "data"), ""},
		{"a look-alike directory", filepath.Join(root, ".graphveinx"), ""},
		{"through ..", filepath.Join(root, "data", "..", ".graphvein"), filepath.Join(root, ".graphvein")},
	}
	for _, tc := range cases {
		if got := SnapshotLocation(root, tc.dir); got != tc.want {
			t.Errorf("%s: SnapshotLocation(%q, %q) = %q, want %q", tc.name, root, tc.dir, got, tc.want)
		}
	}
}

// treeDigest describes every entry under root: path, type, mode and file
// content hash, so two digests are equal only for a byte-identical tree.
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

// TestRunExternalStoreLeavesRootUntouched: with the store outside the
// root, neither a finished run nor a run in progress writes anything into
// the indexed tree (no .graphvein directory, no snapshot).
func TestRunExternalStoreLeavesRootUntouched(t *testing.T) {
	backends(t, func(t *testing.T, rs runStore) {
		h := newHarness(t, rs)
		before := treeDigest(t, h.root)
		opt := h.options()
		var during string
		opt.Emit = func(ctx context.Context, root string, s store.Store, w io.Writer, o index.Options) (index.Summary, error) {
			summary, err := index.Emit(ctx, root, s, w, o)
			during = treeDigest(t, h.root)
			return summary, err
		}
		if _, err := Run(context.Background(), opt); err != nil {
			t.Fatal(err)
		}
		if during != before {
			t.Fatalf("root changed during the run:\n%s\nwant\n%s", during, before)
		}
		if after := treeDigest(t, h.root); after != before {
			t.Fatalf("root changed by the run:\n%s\nwant\n%s", after, before)
		}
		h.noSnapshotLeft()
	})
}

// TestRunDefaultLayout: with the store in <root>/.graphvein the snapshot is
// written there (ignored by Git and the scanner) and removed afterwards.
func TestRunDefaultLayout(t *testing.T) {
	root := fixture(t)
	path := filepath.Join(root, sqlite.DefaultPath)
	locker := sqlite.IndexLocker(path)
	defer locker.Close()
	lease, err := locker.Acquire(context.Background(), LockWait)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	dir := filepath.Dir(path)
	var during []string
	_, err = Run(context.Background(), Options{
		Root: root, Repository: "fixture", Lease: lease, SnapshotDir: dir,
		OpenStore: func(ctx context.Context) (store.Store, error) { return sqlite.Open(ctx, path) },
		Emit: func(ctx context.Context, root string, s store.Store, w io.Writer, o index.Options) (index.Summary, error) {
			during, _ = filepath.Glob(filepath.Join(dir, "snapshot-*.ndjson"))
			return index.Emit(ctx, root, s, w, o)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(during) != 1 {
		t.Fatalf("snapshots in %s during the run: %v, want one", dir, during)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "snapshot-*")); len(left) != 0 {
		t.Fatalf("temporary snapshot left: %v", left)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("store not written: %v", err)
	}
}

// TestRunRemovesTheSnapshotDirectoryItCreated: a missing snapshot
// directory is created for the run and removed when it ends, after a
// failure as after a commit; a directory that existed stays.
func TestRunRemovesTheSnapshotDirectoryItCreated(t *testing.T) {
	backends(t, func(t *testing.T, rs runStore) {
		h := newHarness(t, rs)
		emitErr := errors.New("emit failed")
		for _, fail := range []bool{false, true} {
			parent := t.TempDir()
			for _, dir := range []string{
				filepath.Join(parent, "a", "b"),
				// The default layout's directory, missing as with Postgres.
				filepath.Join(h.root, ".graphvein"),
			} {
				opt := h.options()
				opt.SnapshotDir = dir
				var seen bool
				opt.Emit = func(ctx context.Context, root string, s store.Store, w io.Writer, o index.Options) (index.Summary, error) {
					left, _ := filepath.Glob(filepath.Join(dir, "snapshot-*.ndjson"))
					seen = len(left) == 1
					if fail {
						return index.Summary{}, emitErr
					}
					return index.Emit(ctx, root, s, w, o)
				}
				_, err := Run(context.Background(), opt)
				if fail != errors.Is(err, emitErr) || !fail && err != nil {
					t.Fatalf("%s (fail %v): error %v", dir, fail, err)
				}
				if !seen {
					t.Fatalf("%s (fail %v): snapshot not written there", dir, fail)
				}
				if _, err := os.Lstat(dir); !errors.Is(err, fs.ErrNotExist) {
					t.Fatalf("%s (fail %v): created directory left: %v", dir, fail, err)
				}
			}
			if _, err := os.Lstat(filepath.Join(parent, "a")); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("created parent directory left: %v", err)
			}
			if _, err := os.Stat(parent); err != nil {
				t.Fatalf("existing directory removed: %v", err)
			}
		}
		h.noSnapshotLeft()
	})
}

// TestCreateSnapshotKeepsADirectoryInUse: a created directory that holds
// another file when the run ends is kept, and so are its parents.
func TestCreateSnapshotKeepsADirectoryInUse(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "a", "b")
	stream, cleanup, err := createSnapshot(dir)
	if err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(dir, "index.db")
	if err := os.WriteFile(other, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	cleanup()
	if _, err := os.Stat(stream.Name()); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("snapshot left: %v", err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatalf("directory in use removed: %v", err)
	}
}
