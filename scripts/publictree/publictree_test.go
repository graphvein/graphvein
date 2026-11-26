// Package publictree tests the public tree guard of ADR 0080: the snapshot
// of HEAD that becomes the public repository holds no private path and no
// file that names an internal record, and the guard catches both.
package publictree

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func needTools(t *testing.T) {
	t.Helper()
	for _, tool := range []string{"bash", "git", "tar"} {
		if _, err := exec.LookPath(tool); err != nil {
			if os.Getenv("GITHUB_ACTIONS") == "true" {
				t.Fatalf("%s not found in CI", tool)
			}
			t.Skipf("%s not found; public tree guard not checked", tool)
		}
	}
}

func runBash(t *testing.T, root string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("bash", args...)
	cmd.Dir = root
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.String(), err
}

// TestPublicSnapshot exports HEAD with scripts/public-snapshot.sh into a
// temporary directory and runs scripts/check-public-tree.sh on it: no
// private path, the public AGENTS.md at the root, and no file naming
// docs/tasks, docs/research, the handoff notes, the plan or the CI budget
// outside the reviewed exemptions, and no personal detail, internal path or
// private name (scripts/check-public-privacy.sh); the files it places
// (AGENTS.md, .github/dependabot.yml) are their docs/public copies, and the
// working repository has no .github/dependabot.yml of its own. In the
// public repository itself, which has no docs/public/AGENTS.md left to
// move, it checks the checkout.
func TestPublicSnapshot(t *testing.T) {
	needTools(t)
	root := repoRoot(t)
	if _, err := os.Stat(filepath.Join(root, "docs", "public", "AGENTS.md")); os.IsNotExist(err) {
		if out, err := runBash(t, root, "scripts/check-public-tree.sh", filepath.ToSlash(root)); err != nil {
			t.Fatalf("check-public-tree on the public checkout: %v\n%s", err, out)
		}
		if out, err := runBash(t, root, "scripts/check-public-privacy.sh", filepath.ToSlash(root)); err != nil {
			t.Fatalf("check-public-privacy on the public checkout: %v\n%s", err, out)
		}
		return
	}
	tmp := t.TempDir()
	snap := filepath.ToSlash(filepath.Join(tmp, "public"))
	list := filepath.ToSlash(filepath.Join(tmp, "files.txt"))
	if out, err := runBash(t, root, "scripts/public-snapshot.sh", snap, list); err != nil {
		t.Fatalf("public-snapshot: %v\n%s", err, out)
	}
	if out, err := runBash(t, root, "scripts/check-public-tree.sh", snap); err != nil {
		t.Fatalf("check-public-tree on the snapshot: %v\n%s", err, out)
	}
	if out, err := runBash(t, root, "scripts/check-public-privacy.sh", snap); err != nil {
		t.Fatalf("check-public-privacy on the snapshot: %v\n%s", err, out)
	}

	data, err := os.ReadFile(list)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]bool{}
	for _, f := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		files[f] = true
	}
	for _, want := range []string{"AGENTS.md", ".github/dependabot.yml", "LICENSE", "NOTICE", "THIRD_PARTY_NOTICES", "CONTRIBUTING.md", "go.mod"} {
		if !files[want] {
			t.Errorf("the snapshot lacks %s", want)
		}
	}
	for f := range files {
		for _, private := range []string{"docs/tasks/", "docs/research/", "docs/corpora/", "docs/perf/", "docs/HANDOFF.md", "docs/handoff/", "scripts/handoff-check.sh", "scripts/test-handoff-check.sh", "docs/PLAN.md", "docs/ci-budget.md", "docs/public/AGENTS.md", "docs/public/dependabot.yml", "scripts/private/"} {
			if f == strings.TrimSuffix(private, "/") || strings.HasPrefix(f, private) {
				t.Errorf("the snapshot holds private path %s", f)
			}
		}
	}
	// Files the snapshot places: the public copy is the docs/public one,
	// byte for byte (the checkout may hold CRLF on Windows; the archive
	// holds the committed LF).
	for _, placed := range []struct{ from, to string }{
		{"docs/public/AGENTS.md", "AGENTS.md"},
		{"docs/public/dependabot.yml", ".github/dependabot.yml"},
	} {
		public, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(placed.from)))
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(snap, filepath.FromSlash(placed.to)))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(bytes.ReplaceAll(public, []byte("\r\n"), []byte("\n")), got) {
			t.Errorf("the snapshot's %s is not %s", placed.to, placed.from)
		}
	}
	// Dependabot acts in any repository whose default branch holds
	// .github/dependabot.yml: the working repository must not hold it.
	if _, err := os.Lstat(filepath.Join(root, ".github", "dependabot.yml")); err == nil {
		t.Error("the working repository holds .github/dependabot.yml; keep it as docs/public/dependabot.yml (the snapshot places it)")
	}
}

// TestCheckPublicTreeFails proves the guard is not vacuous: in a small
// tree it accepts, adding a private path, the internal AGENTS.md or a
// reference to an internal record each makes it fail.
func TestCheckPublicTreeFails(t *testing.T) {
	needTools(t)
	root := repoRoot(t)
	write := func(dir, name, text string) {
		t.Helper()
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	base := func() string {
		dir := t.TempDir()
		write(dir, "AGENTS.md", "# AGENTS.md\n\nRead docs/VISION.md.\n")
		write(dir, "README.md", "# Graphvein\n")
		return dir
	}
	clean := base()
	if out, err := runBash(t, root, "scripts/check-public-tree.sh", filepath.ToSlash(clean)); err != nil {
		t.Fatalf("a clean tree fails: %v\n%s", err, out)
	}
	cases := map[string]func(dir string){
		"private task folder": func(dir string) { write(dir, "docs/tasks/T999-x.md", "x\n") },
		"handoff notes":       func(dir string) { write(dir, "docs/HANDOFF.md", "x\n") },
		"split handoff notes": func(dir string) { write(dir, "docs/handoff/a.md", "x\n") },
		"handoff check":       func(dir string) { write(dir, "scripts/handoff-check.sh", "x\n") },
		"internal AGENTS.md":  func(dir string) { write(dir, "AGENTS.md", "Ignore `docs/HANDOFF.md`.\n") },
		"task file reference": func(dir string) { write(dir, "docs/adr/0001-x.md", "See docs/tasks/T001-x.md.\n") },
		"bare task file name": func(dir string) { write(dir, "docs/adr/0002-x.md", "Recorded in T011a-inventory.md.\n") },
		"plan reference":      func(dir string) { write(dir, "README.md", "See [plan](docs/PLAN.md).\n") },
		"research reference":  func(dir string) { write(dir, "docs/x.md", "From docs/research/study.md.\n") },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			dir := base()
			mutate(dir)
			if out, err := runBash(t, root, "scripts/check-public-tree.sh", filepath.ToSlash(dir)); err == nil {
				t.Errorf("check-public-tree accepted the tree:\n%s", out)
			}
		})
	}
}
