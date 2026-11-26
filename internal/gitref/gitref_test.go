package gitref

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

// gitRepo creates a repository in a temporary directory; git runs with a
// fixed identity, without the user's global or system configuration and
// without automatic maintenance, so no maintenance started by the test's
// own commits can still be writing into .git while a test inspects it.
type gitRepo struct {
	t   *testing.T
	dir string
}

func newRepo(t *testing.T) *gitRepo {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	r := &gitRepo{t: t, dir: t.TempDir()}
	r.git("init", "-q")
	return r
}

func (r *gitRepo) git(args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=Test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false", "-c", "core.autocrlf=false", "-c", "maintenance.auto=false", "-c", "gc.auto=0", "-C", r.dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+filepath.Join(r.dir, ".no-global"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (r *gitRepo) write(name, content string) {
	r.t.Helper()
	p := filepath.Join(r.dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *gitRepo) commit(message string) string {
	r.t.Helper()
	r.git("add", "-A")
	r.git("commit", "-q", "-m", message)
	return r.git("rev-parse", "HEAD")
}

// listing returns every file under dir (relative, slash-separated) except
// the .git directory, with contents.
func listing(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		if d.IsDir() && rel == ".git" {
			return filepath.SkipDir
		}
		if !d.IsDir() {
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			out[filepath.ToSlash(rel)] = string(data)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// gitEntry is what gitDirState records about one entry under .git.
type gitEntry struct {
	dir  bool
	mode fs.FileMode
	size int64
	sum  [sha256.Size]byte
	// mtime is reported when an entry differs but is not compared: see
	// gitDirState.
	mtime time.Time
}

func (e gitEntry) String() string {
	if e.dir {
		return fmt.Sprintf("dir mode %v mtime %s", e.mode, e.mtime.Format(time.RFC3339Nano))
	}
	return fmt.Sprintf("file size %d mode %v sha256 %x mtime %s", e.size, e.mode, e.sum[:8], e.mtime.Format(time.RFC3339Nano))
}

// gitDirState records the repository's .git so a test can prove that
// materialising wrote nothing into it: every path, its kind and mode, and
// for files the size and content hash. Modification times are not
// compared: on Windows the times a directory walk reports come from the
// parent directory's entry, which NTFS may update after the writing
// process has exited, and directory times change with any lock file
// created and removed in them. Neither is a change to the repository's
// content, while every real write (an index refreshed, a ref, FETCH_HEAD,
// a pack, a commit-graph, gc.log, a lock file left behind) changes a file's
// bytes or the set of paths, which this state compares.
func gitDirState(t *testing.T, dir string) map[string]gitEntry {
	t.Helper()
	root := filepath.Join(dir, ".git")
	out := map[string]gitEntry{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := os.Lstat(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		e := gitEntry{dir: info.IsDir(), mode: info.Mode(), mtime: info.ModTime()}
		if info.Mode().IsRegular() {
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			e.size = int64(len(data))
			e.sum = sha256.Sum256(data)
		}
		out[filepath.ToSlash(rel)] = e
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// gitDirChanges lists every entry that was added, removed or changed
// between two gitDirState results, with both sides, sorted by path.
func gitDirChanges(before, after map[string]gitEntry) []string {
	var out []string
	for p, b := range before {
		a, ok := after[p]
		switch {
		case !ok:
			out = append(out, fmt.Sprintf("removed %s (was %v)", p, b))
		case a.dir != b.dir || a.mode != b.mode || a.size != b.size || a.sum != b.sum:
			out = append(out, fmt.Sprintf("changed %s: before %v, after %v", p, b, a))
		}
	}
	for p, a := range after {
		if _, ok := before[p]; !ok {
			out = append(out, fmt.Sprintf("added %s (%v)", p, a))
		}
	}
	sort.Strings(out)
	return out
}

func TestResolveAndMergeBase(t *testing.T) {
	ctx := context.Background()
	r := newRepo(t)
	r.write("a.txt", "a\n")
	first := r.commit("first")
	r.git("branch", "feature")
	r.write("a.txt", "a2\n")
	second := r.commit("second")
	r.git("checkout", "-q", "feature")
	r.write("b.txt", "b\n")
	feature := r.commit("feature")
	if got, err := Resolve(ctx, r.dir, "HEAD"); err != nil || got != feature {
		t.Fatalf("HEAD = %q %v", got, err)
	}
	if got, err := Resolve(ctx, r.dir, first[:10]); err != nil || got != first {
		t.Fatalf("short id = %q %v", got, err)
	}
	for _, bad := range []string{"", "-h", "--all", "no-such-branch", "HEAD:a.txt"} {
		if _, err := Resolve(ctx, r.dir, bad); !errors.Is(err, ErrRefUnknown) {
			t.Fatalf("Resolve(%q) = %v", bad, err)
		}
	}
	if base, err := MergeBase(ctx, r.dir, second, feature); err != nil || base != first {
		t.Fatalf("merge base = %q %v", base, err)
	}
	if _, err := Resolve(ctx, t.TempDir(), "HEAD"); !errors.Is(err, ErrGitUnavailable) {
		t.Fatalf("outside a repository = %v", err)
	}
}

func TestMaterialise(t *testing.T) {
	ctx := context.Background()
	r := newRepo(t)
	r.write("a.txt", "line\r\nkept as committed\r\n")
	r.write("dir/b.go", "package dir\n")
	r.write("tool.sh", "#!/bin/sh\n")
	r.write(".gitattributes", "*.txt export-ignore eol=lf\n")
	if err := os.Chmod(filepath.Join(r.dir, "tool.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	head := r.commit("first")
	r.git("update-index", "--chmod=+x", "tool.sh")
	// A symlink and a submodule entry, written into the index directly so
	// the test runs on platforms without symlink rights.
	linkOID := strings.TrimSpace(r.gitInput("target\n", "hash-object", "-w", "--stdin"))
	r.git("update-index", "--add", "--cacheinfo", "120000,"+linkOID+",link")
	r.git("update-index", "--add", "--cacheinfo", "160000,"+head+",sub")
	r.git("commit", "-q", "-m", "second")
	commit := r.git("rev-parse", "HEAD")
	before := gitDirState(t, r.dir)
	tree, err := Materialise(ctx, r.dir, commit)
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Remove()
	// Compared right away, before the test's own git calls below.
	if changes := gitDirChanges(before, gitDirState(t, r.dir)); len(changes) > 0 {
		t.Fatalf("materialising wrote into the repository's .git:\n%s", strings.Join(changes, "\n"))
	}
	got := listing(t, tree.Dir)
	// The bytes are the committed blob's (git add applied eol=lf), and no
	// attribute applies on the way out (no export-ignore, no eol).
	committed := r.gitInput("", "cat-file", "blob", commit+":a.txt")
	if committed != "line\nkept as committed\n" {
		t.Fatalf("committed blob = %q", committed)
	}
	want := map[string]string{"a.txt": committed, "dir/b.go": "package dir\n", "tool.sh": "#!/bin/sh\n", ".gitattributes": "*.txt export-ignore eol=lf\n"}
	if len(got) != len(want) {
		t.Fatalf("materialised %v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("%s = %q, want %q", k, got[k], v)
		}
	}
	if tree.Files != 4 || tree.Commit != commit {
		t.Fatalf("tree = %+v", tree)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(tree.Dir, "tool.sh"))
		if err != nil || info.Mode().Perm()&0o100 == 0 {
			t.Fatalf("executable mode lost: %v %v", info, err)
		}
	}
	codes := map[string]string{}
	for _, d := range tree.Diagnostics {
		codes[d.File] = d.Code
	}
	if codes["link"] != "symlink" || codes["sub"] != "ref_submodule" || len(codes) != 2 {
		t.Fatalf("diagnostics = %+v", tree.Diagnostics)
	}
	// The temporary directory is a git repository of the same object format,
	// so the scanner computes blob ids there as in the working tree.
	if out, err := exec.Command("git", "-C", tree.Dir, "rev-parse", "--show-object-format").Output(); err != nil || strings.TrimSpace(string(out)) != "sha1" {
		t.Fatalf("object format = %q %v", out, err)
	}
	dir := tree.Dir
	if err := tree.Remove(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("temporary directory left: %v", err)
	}
}

func (r *gitRepo) gitInput(input string, args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", append([]string{"-C", r.dir}, args...)...)
	cmd.Stdin = strings.NewReader(input)
	out, err := cmd.Output()
	if err != nil {
		r.t.Fatalf("git %v: %v", args, err)
	}
	return string(out)
}

// TestMaterialiseMissingObjects deletes a blob from the object store: the
// materialisation fails with ErrObjectsMissing (nothing is fetched) and
// leaves no temporary directory.
func TestMaterialiseMissingObjects(t *testing.T) {
	ctx := context.Background()
	r := newRepo(t)
	r.write("a.txt", "present\n")
	r.write("b.txt", "will be missing\n")
	commit := r.commit("first")
	oid := strings.TrimSpace(r.git("rev-parse", commit+":b.txt"))
	if err := os.Remove(filepath.Join(r.dir, ".git", "objects", oid[:2], oid[2:])); err != nil {
		t.Fatal(err)
	}
	pattern := filepath.Join(os.TempDir(), "graphvein-ref-*")
	before, _ := filepath.Glob(pattern)
	if _, err := Materialise(ctx, r.dir, commit); !errors.Is(err, ErrObjectsMissing) {
		t.Fatalf("missing blob = %v", err)
	}
	after, _ := filepath.Glob(pattern)
	if len(after) > len(before) {
		t.Fatalf("temporary directory left after a failure: %v", after)
	}
	if _, err := Materialise(ctx, r.dir, strings.Repeat("0", 40)); err == nil {
		t.Fatal("unknown commit materialised")
	}
}

// TestMaterialisePartialClone materialises a commit of a blob:none
// partial clone: git's cat-file dies on the first blob it may not fetch,
// and the failure is ErrObjectsMissing, not the reader's EOF (R3-9).
func TestMaterialisePartialClone(t *testing.T) {
	ctx := context.Background()
	src := newRepo(t)
	src.write("a.txt", "content\n")
	src.commit("first")
	src.git("config", "uploadpack.allowFilter", "true")
	clone := &gitRepo{t: t, dir: filepath.Join(t.TempDir(), "clone")}
	url := "file://" + filepath.ToSlash(src.dir)
	if !strings.HasPrefix(filepath.ToSlash(src.dir), "/") {
		url = "file:///" + filepath.ToSlash(src.dir)
	}
	src.git("clone", "-q", "--no-local", "--filter=blob:none", "--no-checkout", url, clone.dir)
	commit := strings.TrimSpace(clone.git("rev-parse", "HEAD"))
	_, err := Materialise(ctx, clone.dir, commit)
	if !errors.Is(err, ErrObjectsMissing) {
		t.Fatalf("partial clone = %v, want ErrObjectsMissing", err)
	}
}

func TestSafePath(t *testing.T) {
	for p, want := range map[string]bool{"a/b.go": true, ".github/x.yml": true, "../x": false, "a/../b": false, ".git/config": false, "a/.GIT/x": false, "/abs": false, "": false} {
		if got := safePath(p); got != want {
			t.Errorf("safePath(%q) = %v", p, got)
		}
	}
}

// TestRemoteOf reads the origin remote without credentials and the
// repository's top level, from the root and from a folder inside it; a
// repository without origin has no URL, and a plain folder is no
// repository.
func TestRemoteOf(t *testing.T) {
	ctx := context.Background()
	r := newRepo(t)
	if got, err := RemoteOf(ctx, r.dir); err != nil || got.URL != "" || !sameDir(t, got.TopLevel, r.dir) {
		t.Errorf("without origin: %+v %v", got, err)
	}
	r.git("remote", "add", "origin", "https://someone:s3cret@example.invalid/o/r.git")
	r.write("apps/web/a.txt", "x")
	for _, dir := range []string{r.dir, filepath.Join(r.dir, "apps", "web")} {
		got, err := RemoteOf(ctx, dir)
		if err != nil || got.URL != "https://example.invalid/o/r.git" || !sameDir(t, got.TopLevel, r.dir) {
			t.Errorf("%s: %+v %v", dir, got, err)
		}
	}
	if _, err := RemoteOf(ctx, t.TempDir()); !errors.Is(err, ErrGitUnavailable) {
		t.Errorf("plain folder: %v", err)
	}
}

func sameDir(t *testing.T, a, b string) bool {
	t.Helper()
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	return err1 == nil && err2 == nil && ra == rb
}
