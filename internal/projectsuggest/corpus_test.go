//go:build graphveincorpus

package projectsuggest

import (
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/golden"
)

func corpusRoot() string {
	if dir := os.Getenv("GRAPHVEIN_CORPUS_DIR"); dir != "" {
		return dir
	}
	return "../../spike/parsing/.corpus"
}

// cleanCorpus checks a corpus checkout's pin and that it is clean.
func cleanCorpus(t *testing.T, name, pin string) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join(corpusRoot(), name))
	if err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) string {
		out, err := exec.Command("git", append([]string{"-C", root}, args...)...).Output()
		if err != nil {
			t.Fatalf("verifying %s: %v", name, err)
		}
		return strings.TrimSpace(string(out))
	}
	if got := git("rev-parse", "HEAD"); got != pin {
		t.Fatalf("%s pin %s, expected %s", name, got, pin)
	}
	if status := git("status", "--porcelain", "--ignored"); status != "" {
		t.Fatalf("%s is dirty: %s", name, status)
	}
	return root
}

// TestCorpusSuggestions is B6's corpus gate, with B6b's checks: suggestions
// for react + express + axum-ddd and for the langflow frontend + langflow
// are listed with evidence (golden), checked against react's stored calls
// (a copy of react is indexed; the other members are not, since none is a
// matched consumer), the checkouts are read in place and stay clean, and
// the manifest is not changed. The case react-published adds a Compose
// file in the project folder that publishes express on react's API_URL
// port (30400), so react's address matches express and reaches the stored
// call check (SG-M5).
func TestCorpusSuggestions(t *testing.T) {
	react := cleanCorpus(t, "react", "969709a379b13935b4e1caae0ad8cad548e5879a")
	express := cleanCorpus(t, "express", "94a864e4e458a5e0ae2b07d3cf762791769e8690")
	axum := cleanCorpus(t, "axum-ddd", "a3a5bc307f24cdf29eca50ee0ea0c7eeb4d50cb2")
	langflow := cleanCorpus(t, "langflow", "504c02fc47e76087b82b0e7cbe4186e9cdd916d4")
	reactCalls := indexCorpusCopy(t, react, "react")
	realworld := [][2]string{{"react", react}, {"express", express}, {"axum-ddd", axum}}
	for _, c := range []struct {
		name    string
		members [][2]string
		compose func(dir string) string
		calls   Calls
	}{
		{"react", realworld, nil, reactCalls},
		{"react-published", realworld, func(dir string) string {
			rel, err := filepath.Rel(dir, express)
			if err != nil {
				t.Fatal(err)
			}
			return "services:\n  api:\n    build: '" + filepath.ToSlash(rel) + "'\n    ports:\n      - '30400:3100'\n"
		}, reactCalls},
		{"langflow", [][2]string{{"langflow-frontend", filepath.Join(langflow, "src", "frontend")}, {"langflow", langflow}}, nil, Calls{}},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			manifest := "version: 1\nname: corpus-" + c.name + "\nmembers:\n"
			local := "version: 1\nmembers:\n"
			for _, m := range c.members {
				manifest += "  - repository: " + m[0] + "\n"
				local += "  " + m[0] + ":\n    path: '" + m[1] + "'\n"
			}
			if err := os.WriteFile(filepath.Join(dir, config.ProjectFile), []byte(manifest), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, config.ProjectLocalFile), []byte(local), 0o644); err != nil {
				t.Fatal(err)
			}
			if c.compose != nil {
				if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte(c.compose(dir)), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			p, err := config.LoadProject(dir)
			if err != nil {
				t.Fatal(err)
			}
			got, err := Suggest(context.Background(), p, c.calls)
			if err != nil {
				t.Fatal(err)
			}
			golden.Check(t, filepath.Join(fixtureDir, "..", "projectsuggest.corpus-"+c.name+".golden.json"), got)
			if data, err := os.ReadFile(filepath.Join(dir, config.ProjectFile)); err != nil || string(data) != manifest {
				t.Fatalf("manifest changed: %v", err)
			}
			for _, m := range c.members {
				if out, err := exec.Command("git", "-C", m[1], "status", "--porcelain", "--ignored").Output(); err != nil || len(out) != 0 {
					t.Fatalf("%s dirty after suggest: %s %v", m[0], out, err)
				}
			}
		})
	}
}

// indexCorpusCopy indexes a copy of a corpus checkout (the checkout itself
// is never written) as repository and returns its stored calls.
func indexCorpusCopy(t *testing.T, source, repository string) Calls {
	t.Helper()
	dest := filepath.Join(t.TempDir(), repository)
	err := filepath.WalkDir(source, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Name() == ".git" && d.IsDir() {
			return filepath.SkipDir
		}
		rel, err := filepath.Rel(source, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return indexProject(t, config.Project{Members: []config.Member{{Repository: repository, Path: dest, Checkout: true}}})
}
