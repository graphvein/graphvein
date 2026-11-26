package scan

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=Test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false", "-c", "core.autocrlf=false", "-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+filepath.Join(dir, ".no-global"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// indexedOIDs walks root as an index run does and returns path to blob id.
func indexedOIDs(t *testing.T, root string) map[string]string {
	t.Helper()
	result, err := Walk(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, f := range result.Files {
		out[f.Record.Path] = f.Record.GitBlobOID
	}
	return out
}

func TestReadGitState(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	ctx := context.Background()
	root := t.TempDir()
	if state := ReadGitState(ctx, root, nil); state.Commit != "" || state.Dirty != nil {
		t.Fatalf("outside git = %+v", state)
	}
	gitIn(t, root, "init", "-q")
	write(t, root, "a.go", "package a\n")
	write(t, root, "vendor/v.go", "package v\n")
	write(t, root, "image.bin", "\x00\x01")
	if state := ReadGitState(ctx, root, indexedOIDs(t, root)); state.Commit != "" || state.Dirty != nil {
		t.Fatalf("no commit yet = %+v", state)
	}
	gitIn(t, root, "add", "-A")
	gitIn(t, root, "commit", "-q", "-m", "first")
	head := gitIn(t, root, "rev-parse", "HEAD")
	check := func(name string, wantDirty bool) {
		t.Helper()
		state := ReadGitState(ctx, root, indexedOIDs(t, root))
		if state.Commit != head || state.Dirty == nil || *state.Dirty != wantDirty {
			t.Fatalf("%s: state = %+v (dirty %v), want commit %s dirty %v", name, state, state.Dirty != nil && *state.Dirty, head, wantDirty)
		}
	}
	// Tracked files the scanner skips (vendor/, binary) do not make it dirty.
	check("clean", false)
	write(t, root, "a.go", "package a // changed\n")
	check("modified", true)
	gitIn(t, root, "checkout", "-q", "--", "a.go")
	check("restored", false)
	write(t, root, "new.go", "package a\n")
	check("untracked", true)
	if err := os.Remove(filepath.Join(root, "new.go")); err != nil {
		t.Fatal(err)
	}
	// An untracked file excluded only by .git/info/exclude is still
	// indexed (the scanner reads .gitignore files only), so it is dirty
	// although git status reports nothing.
	write(t, root, ".git/info/exclude", "local.go\n")
	write(t, root, "local.go", "package a\n")
	check("excluded by info/exclude", true)
	if err := os.Remove(filepath.Join(root, "local.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "a.go")); err != nil {
		t.Fatal(err)
	}
	check("deleted", true)
	// A subdirectory of a checkout has no commit of its own.
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if state := ReadGitState(ctx, filepath.Join(root, "sub"), nil); state.Commit != "" || state.Dirty != nil {
		t.Fatalf("subdirectory = %+v", state)
	}
}
