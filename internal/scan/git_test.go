package scan

import (
	"bytes"
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestGitCommandOptions checks the options and environment GitCommand gives
// every git subprocess (ADR 0056: read-only access to the user's repository).
func TestGitCommandOptions(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct {
		dir  string
		want []string
	}{
		{dir, []string{"-c", "safe.directory=" + filepath.ToSlash(dir), "-C", dir, "ls-tree", "HEAD"}},
		{"", []string{"ls-tree", "HEAD"}},
	} {
		cmd := GitCommand(context.Background(), tc.dir, "ls-tree", "HEAD")
		args := cmd.Args[1:]
		want := append([]string{
			"-c", "protocol.allow=never",
			"-c", "maintenance.auto=false",
			"-c", "gc.auto=0",
			"-c", "core.fsmonitor=false",
		}, tc.want...)
		if strings.Join(args, "\x00") != strings.Join(want, "\x00") {
			t.Fatalf("args = %q, want %q", args, want)
		}
		env := map[string]string{}
		for _, kv := range cmd.Env {
			if k, v, ok := strings.Cut(kv, "="); ok {
				env[k] = v // later entries win, as for exec.Cmd
			}
		}
		for k, v := range map[string]string{"GIT_OPTIONAL_LOCKS": "0", "GIT_NO_LAZY_FETCH": "1", "GIT_TERMINAL_PROMPT": "0"} {
			if env[k] != v {
				t.Fatalf("%s = %q, want %q", k, env[k], v)
			}
		}
	}
}

// TestGitOnlyThroughGitCommand parses the program's Go sources and refuses
// any exec.Command or exec.CommandContext call that starts "git" anywhere
// but GitCommand, so no invocation can miss the read-only options.
func TestGitOnlyThroughGitCommand(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	allowed := filepath.Join(root, "internal", "scan", "git.go")
	checked := 0
	for _, top := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, top), func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() && d.Name() == "testdata" {
				return filepath.SkipDir
			}
			if d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") || p == allowed {
				return nil
			}
			file, err := parser.ParseFile(token.NewFileSet(), p, nil, 0)
			if err != nil {
				return err
			}
			checked++
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || (sel.Sel.Name != "Command" && sel.Sel.Name != "CommandContext") {
					return true
				}
				if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "exec" {
					return true
				}
				i := 0
				if sel.Sel.Name == "CommandContext" {
					i = 1
				}
				if len(call.Args) > i {
					if lit, ok := call.Args[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
						if name, _ := strconv.Unquote(lit.Value); name == "git" {
							t.Errorf("%s starts git without scan.GitCommand", p)
						}
					}
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if checked == 0 {
		t.Fatal("no source files checked")
	}
}

// TestGitCommandTakesNoOptionalLock shows what GIT_OPTIONAL_LOCKS=0 is for:
// git status refreshes stale index stat data and rewrites .git/index, but
// not when run through GitCommand.
func TestGitCommandTakesNoOptionalLock(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q")
	file := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(file, []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "a.txt")
	gitIn(t, dir, "commit", "-q", "-m", "first")
	// Same content, new mtime: the index's stat data are now stale.
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(file, later, later); err != nil {
		t.Fatal(err)
	}
	index := filepath.Join(dir, ".git", "index")
	before, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}
	if out, err := GitCommand(context.Background(), dir, "status", "--porcelain").CombinedOutput(); err != nil {
		t.Fatalf("git status: %v\n%s", err, out)
	}
	if after, _ := os.ReadFile(index); !bytes.Equal(before, after) {
		t.Fatal("git status through GitCommand rewrote .git/index")
	}
	// Control: without the environment, the same status refreshes the index.
	gitIn(t, dir, "status", "--porcelain")
	if after, _ := os.ReadFile(index); bytes.Equal(before, after) {
		t.Fatal("control: plain git status did not refresh the stale index, so the check above proves nothing")
	}
}
