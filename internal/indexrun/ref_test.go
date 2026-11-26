package indexrun

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/store"
)

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=Test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false", "-c", "core.autocrlf=false", "-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+filepath.Join(dir, ".git", "no-global"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeFile(t *testing.T, root, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestRunRef indexes two commits of a git copy of the Go fixture into ref
// namespaces and checks them against the working-tree index of the same
// commits: a ref of the indexed tree parses zero files and compares equal,
// an older ref equals a fresh index of its checkout.
func TestRunRef(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	backends(t, func(t *testing.T, rs runStore) {
		ctx := context.Background()
		h := newHarness(t, rs)
		gitIn(t, h.root, "init", "-q")
		gitIn(t, h.root, "add", "-A")
		gitIn(t, h.root, "commit", "-q", "-m", "first")
		c1 := gitIn(t, h.root, "rev-parse", "HEAD")
		writeFile(t, h.root, "main.go", "package main\n\nfunc main() { helperTwo() }\n\nfunc helperTwo() {}\n")
		if err := os.Remove(filepath.Join(h.root, "extra.go")); err != nil {
			t.Fatal(err)
		}
		writeFile(t, h.root, "added.go", "package main\n\nfunc added() { helperTwo() }\n")
		gitIn(t, h.root, "add", "-A")
		gitIn(t, h.root, "commit", "-q", "-m", "second")
		c2 := gitIn(t, h.root, "rev-parse", "HEAD")

		if _, err := Run(ctx, h.options()); err != nil {
			t.Fatal(err)
		}
		s, err := rs.open(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		runs, err := s.ChangeRuns(ctx, "fixture")
		if err != nil || len(runs) != 1 || runs[0].Commit != c2 || runs[0].Dirty == nil || *runs[0].Dirty {
			t.Fatalf("run metadata = %+v %v", runs, err)
		}
		main, err := s.Dump(ctx, "fixture")
		if err != nil {
			t.Fatal(err)
		}

		temps, _ := filepath.Glob(filepath.Join(os.TempDir(), "graphvein-ref-*"))
		ref2, err := RunRef(ctx, RefOptions{Options: h.options(), Commit: c2})
		if err != nil {
			t.Fatal(err)
		}
		if ref2.Summary.Parsed != 0 || ref2.Namespace != store.RefNamespace("fixture", c2) || ref2.ConfigSource != ConfigNone {
			t.Fatalf("ref of the indexed tree: parsed %d, %+v", ref2.Summary.Parsed, ref2)
		}
		same, err := s.ChangeSet(ctx, "fixture", store.ChangeBase{Ref: ref2.Namespace})
		if err != nil || len(same.Changes) != 0 {
			t.Fatalf("ref of the indexed tree differs: %d changes %v", len(same.Changes), err)
		}
		if journal, _ := s.ChangeRuns(ctx, ref2.Namespace); len(journal) != 0 {
			t.Fatal("a ref run wrote a journal")
		}

		// An untracked working-tree graphvein.yaml is used for a ref
		// without one, and makes the working tree dirty.
		writeFile(t, h.root, "graphvein.yaml", "version: 1\n")
		ref1, err := RunRef(ctx, RefOptions{Options: h.options(), Commit: c1})
		if err != nil {
			t.Fatal(err)
		}
		if ref1.ConfigSource != ConfigFromWorkingTree || ref1.Summary.Parsed == 0 {
			t.Fatalf("older ref: %+v", ref1)
		}
		after, _ := filepath.Glob(filepath.Join(os.TempDir(), "graphvein-ref-*"))
		if len(after) > len(temps) {
			t.Fatalf("temporary ref directories left: %v", after)
		}
		refDump, err := s.Dump(ctx, ref1.Namespace)
		if err != nil {
			t.Fatal(err)
		}
		cs, err := s.ChangeSet(ctx, "fixture", store.ChangeBase{Ref: ref1.Namespace, Head: ref2.Namespace})
		if err != nil {
			t.Fatal(err)
		}
		want, err := store.DiffSnapshots(refDump, main)
		if err != nil {
			t.Fatal(err)
		}
		if len(cs.Changes) == 0 || !reflect.DeepEqual(cs.Changes, want) {
			t.Fatalf("ref comparison (%d) differs from the snapshot diff (%d)", len(cs.Changes), len(want))
		}

		// The ref snapshot of c1 equals a fresh index of c1's checkout.
		if err := os.Remove(filepath.Join(h.root, "graphvein.yaml")); err != nil {
			t.Fatal(err)
		}
		gitIn(t, h.root, "checkout", "-q", c1)
		if _, err := Run(ctx, h.options()); err != nil {
			t.Fatal(err)
		}
		checkout, err := s.Dump(ctx, "fixture")
		if err != nil {
			t.Fatal(err)
		}
		diff, err := store.DiffSnapshots(refDump, checkout)
		if err != nil || len(diff) != 0 {
			t.Fatalf("ref snapshot differs from the checkout's index in %d records: %v", len(diff), diff)
		}
		if !reflect.DeepEqual(refDump.Diagnostics, checkout.Diagnostics) {
			t.Fatalf("diagnostics differ:\n%v\n%v", refDump.Diagnostics, checkout.Diagnostics)
		}
		runs, _ = s.ChangeRuns(ctx, "fixture")
		if last := runs[len(runs)-1]; last.Commit != c1 || last.Dirty == nil || *last.Dirty {
			t.Fatalf("checkout run metadata = %+v", last)
		}
		snaps, err := s.RefSnapshots(ctx, "fixture")
		if err != nil || len(snaps) != 2 {
			t.Fatalf("ref snapshots = %+v %v", snaps, err)
		}
		// Modified tracked file: dirty.
		writeFile(t, h.root, "main.go", "package main\n\nfunc main() {}\n")
		if _, err := Run(ctx, h.options()); err != nil {
			t.Fatal(err)
		}
		runs, _ = s.ChangeRuns(ctx, "fixture")
		if last := runs[len(runs)-1]; last.Dirty == nil || !*last.Dirty {
			t.Fatalf("modified tree recorded clean: %+v", last)
		}
	})
}
