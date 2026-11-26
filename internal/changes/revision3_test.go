package changes_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/graphvein/graphvein/internal/changes"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/indexrun"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/store"
)

// writeFiles writes name -> content under root ("" removes the file).
func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if content == "" {
			if err := os.Remove(p); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// indexRun indexes the working tree as indexrun.Run does for graphvein
// index, so the run records its commit.
func indexRun(t *testing.T, ctx context.Context, b backend, root, cache string) {
	t.Helper()
	locker := b.locker()
	defer locker.Close()
	lease, err := locker.Acquire(ctx, indexrun.LockWait)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	opt := indexrun.Options{Root: root, Repository: "fixture", Lease: lease, OpenStore: b.open,
		Parser: parse.Options{Workers: 1, CacheDir: cache}, MaxFileSize: indexrun.DefaultMaxFileSize}
	if _, err := indexrun.Run(ctx, opt); err != nil {
		t.Fatal(err)
	}
}

// indexRefs stores ref snapshots of commits under the index lock.
func indexRefs(t *testing.T, ctx context.Context, b backend, root, cache string, commits ...string) {
	t.Helper()
	locker := b.locker()
	defer locker.Close()
	lease, err := locker.Acquire(ctx, indexrun.LockWait)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	opt := indexrun.Options{Root: root, Repository: "fixture", Lease: lease, OpenStore: b.open,
		Parser: parse.Options{Workers: 1, CacheDir: cache}, MaxFileSize: indexrun.DefaultMaxFileSize}
	for _, commit := range commits {
		if _, err := indexrun.RunRef(ctx, indexrun.RefOptions{Options: opt, Commit: commit}); err != nil {
			t.Fatal(err)
		}
	}
}

func fileItem(c *changes.Comparison, path string) *changes.Item {
	for _, it := range c.AllItems() {
		if it.Record == store.RecordFile && it.Identity == path {
			return &it
		}
	}
	return nil
}

func str(p *string) string {
	if p == nil {
		return "<nil>"
	}
	return *p
}

// TestRemovedFileWording covers G8 (Revision 3, R3-4) on both backends: a
// file the head commit still has but the index excludes (here by a
// committed .graphveinignore) is no_longer_in_index with reason excluded;
// a file the head commit lacks is deleted. Against a base that does not
// know the files (gaps_unknown) neither is removed (R3-7).
func TestRemovedFileWording(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	backends(t, func(t *testing.T, b backend) {
		ctx := context.Background()
		s, err := b.open(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		root := filepath.Join(t.TempDir(), "repo")
		cache := filepath.Join(t.TempDir(), "cache")
		writeFiles(t, root, map[string]string{
			"keep.ts": "export function keep() {\n  return 1;\n}\n",
			"gone.ts": "export function gone() {\n  return 2;\n}\n",
		})
		git(t, root, "init", "-q")
		git(t, root, "add", "-A")
		git(t, root, "commit", "-q", "-m", "one")
		indexInto(t, ctx, s, root, cache)
		writeFiles(t, root, map[string]string{".graphveinignore": "keep.ts\n", "gone.ts": ""})
		git(t, root, "add", "-A")
		git(t, root, "commit", "-q", "-m", "two")
		indexRefs(t, ctx, b, root, cache, git(t, root, "rev-parse", "HEAD~1"), git(t, root, "rev-parse", "HEAD"))

		check := func(c *changes.Comparison) {
			t.Helper()
			keep, gone := fileItem(c, "keep.ts"), fileItem(c, "gone.ts")
			if keep == nil || keep.Change != changes.Removed || str(keep.Wording) != "no_longer_in_index" || str(keep.Reason) != changes.ReasonExcluded {
				t.Errorf("keep.ts = %+v, want removed, no_longer_in_index, excluded", keep)
			}
			if gone == nil || gone.Change != changes.Removed || str(gone.Wording) != "deleted" || gone.Reason != nil {
				t.Errorf("gone.ts = %+v, want removed, deleted", gone)
			}
		}
		check(compare(t, ctx, s, "ref:HEAD~1..HEAD", root))

		// R3-7: the previous base here is older than the journal
		// (gaps_unknown), so it does not know either file: neither is
		// removed, both are known_now with reason gap.
		indexInto(t, ctx, s, root, cache)
		c := compare(t, ctx, s, "previous", root)
		if !c.Summary.Base.Partial {
			t.Fatalf("previous base %+v knows its gaps; the case needs gaps_unknown", c.Summary.Base)
		}
		for _, path := range []string{"keep.ts", "gone.ts"} {
			if it := fileItem(c, path); it == nil || it.Change != changes.KnownNow || str(it.Reason) != "gap" || it.Wording != nil {
				t.Errorf("%s against an unknowing base = %+v, want known_now, gap", path, it)
			}
		}
	})
}

// TestRefMergeBaseFromIndexedCommit covers R3-5 and R3-6 on both
// backends: ref:BASE takes its merge base with the commit the current
// index recorded, not the live HEAD, and says when HEAD has moved; a
// snapshot of another extractor version is not used.
func TestRefMergeBaseFromIndexedCommit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	backends(t, func(t *testing.T, b backend) {
		ctx := context.Background()
		s, err := b.open(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		root := filepath.Join(t.TempDir(), "repo")
		cache := filepath.Join(t.TempDir(), "cache")
		writeFiles(t, root, map[string]string{"a.ts": "export function a() {\n  return 1;\n}\n"})
		git(t, root, "init", "-q", "-b", "master")
		git(t, root, "add", "-A")
		git(t, root, "commit", "-q", "-m", "a")
		fork := git(t, root, "rev-parse", "HEAD")
		git(t, root, "checkout", "-q", "-b", "feat")
		writeFiles(t, root, map[string]string{"feat.ts": "export function feat() {\n  return 2;\n}\n"})
		git(t, root, "add", "-A")
		git(t, root, "commit", "-q", "-m", "feat")
		git(t, root, "checkout", "-q", "master")
		writeFiles(t, root, map[string]string{"master.ts": "export function m() {\n  return 3;\n}\n"})
		git(t, root, "add", "-A")
		git(t, root, "commit", "-q", "-m", "master")

		git(t, root, "checkout", "-q", "feat")
		indexRun(t, ctx, b, root, cache)
		indexed, err := changes.IndexedCommit(ctx, s, "fixture")
		if err != nil || indexed != git(t, root, "rev-parse", "feat") {
			t.Fatalf("indexed commit = %q, %v; want feat's", indexed, err)
		}
		// An index without a recorded commit refuses.
		if _, err := changes.ResolveRefs(ctx, root, changes.Base{Kind: changes.BaseRef, Ref: "master"}, ""); !errors.Is(err, changes.ErrReindexNeeded) {
			t.Errorf("no recorded commit = %v, want ErrReindexNeeded", err)
		}

		git(t, root, "checkout", "-q", "master")
		refs, err := changes.ResolveRefs(ctx, root, changes.Base{Kind: changes.BaseRef, Ref: "master"}, indexed)
		if err != nil {
			t.Fatal(err)
		}
		if refs.Base != fork || !refs.HeadCommitDiffers {
			t.Fatalf("refs = %+v, want the fork point %s and head_commit_differs", refs, fork)
		}
		indexRefs(t, ctx, b, root, cache, fork)
		c := compare(t, ctx, s, "ref:master", root)
		if !c.Summary.HeadCommitDiffers {
			t.Error("summary does not say head_commit_differs")
		}
		if it := fileItem(c, "master.ts"); it != nil {
			t.Errorf("master.ts (never on feat) is an item: %+v", it)
		}
		if it := fileItem(c, "feat.ts"); it == nil || it.Change != changes.Added {
			t.Errorf("feat.ts = %+v, want added", it)
		}

		// R3-6: a snapshot of another extractor version is indexed again.
		bc, _ := changes.ParseBase("ref:master")
		_, err = changes.Compare(ctx, s, "fixture", bc, changes.Options{Root: root, ExtractorVersion: index.Version + "-other"})
		var notIndexed *changes.RefNotIndexedError
		if !errors.As(err, &notIndexed) {
			t.Errorf("snapshot of another extractor version = %v, want RefNotIndexedError", err)
		}
	})
}
