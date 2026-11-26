package config

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/golden"
)

// projectFixtures lists the fixture cases under testdata/project. A case
// named refuse-<rule> or refuse-<rule>--<variant> must be refused under
// rule <rule> of docs/cli/project.md; valid-* cases must load.
func projectFixtures(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join("testdata", "project"))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	return out
}

// manifestDir finds the one directory of a case holding the manifest.
func manifestDir(t *testing.T, root string) string {
	t.Helper()
	var dirs []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil && d.Name() == ProjectFile {
			dirs = append(dirs, filepath.Dir(p))
		}
		return err
	})
	if err != nil || len(dirs) != 1 {
		t.Fatalf("%s: want one %s, got %v (%v)", root, ProjectFile, dirs, err)
	}
	return dirs[0]
}

func TestProjectFixtures(t *testing.T) {
	for _, name := range projectFixtures(t) {
		t.Run(name, func(t *testing.T) {
			root, err := filepath.Abs(filepath.Join("testdata", "project", name))
			if err != nil {
				t.Fatal(err)
			}
			p, err := LoadProject(manifestDir(t, root))
			var got any
			if err != nil {
				if strings.HasPrefix(name, "valid-") {
					t.Fatalf("unexpected error: %v", err)
				}
				msg := strings.ReplaceAll(err.Error(), root, "{CASE}")
				got = map[string]string{"error": filepath.ToSlash(msg)}
			} else {
				if !strings.HasPrefix(name, "valid-") {
					t.Fatalf("expected a refusal, loaded %+v", p)
				}
				got = describeProject(t, root, p)
			}
			golden.Check(t, filepath.Join("testdata", "project", name+".golden.json"), got)
		})
	}
}

// describeProject renders p with paths relative to the case root, so the
// golden reads the same on every OS.
func describeProject(t *testing.T, root string, p Project) map[string]any {
	t.Helper()
	rel := func(path string) string {
		if path == "" {
			return ""
		}
		r, err := filepath.Rel(root, path)
		if err != nil {
			t.Fatal(err)
		}
		return filepath.ToSlash(r)
	}
	consumes := func(list []Consume) []map[string]any {
		var out []map[string]any
		for _, c := range list {
			entry := map[string]any{"repository": c.Repository, "scope": c.Scope,
				"bases": c.Select.Bases, "origins": c.Select.Origins, "path_prefixes": c.Select.PathPrefixes}
			// Only when set, so goldens written before the field existed
			// stay unchanged.
			if c.DefaultScope {
				entry["default_scope"] = true
			}
			out = append(out, entry)
		}
		return out
	}
	var members []map[string]any
	for _, m := range p.Members {
		var scopes []map[string]any
		for _, s := range m.Scopes {
			scopes = append(scopes, map[string]any{"name": s.Name, "path": s.Path, "consumes": consumes(s.Consumes)})
		}
		members = append(members, map[string]any{
			"repository": m.Repository, "repository_source": m.RepositorySource,
			"path": rel(m.Path), "path_source": m.PathSource, "checkout": m.Checkout,
			"consumes": consumes(m.Consumes), "scopes": scopes,
		})
	}
	return map[string]any{"name": p.Name, "dir": rel(p.Dir), "members": members}
}

// TestProjectRulesMatchFixtures keeps the validation rule table in
// docs/cli/project.md and the refusal fixtures 1:1: every documented rule
// has at least one fixture, and every fixture names a documented rule.
func TestProjectRulesMatchFixtures(t *testing.T) {
	doc, err := os.ReadFile(filepath.Join("..", "..", "docs", "cli", "project.md"))
	if err != nil {
		t.Fatal(err)
	}
	rules := map[string]bool{}
	for _, m := range regexp.MustCompile("(?m)^\\| `([a-z-]+)` \\|").FindAllStringSubmatch(string(doc), -1) {
		rules[m[1]] = true
	}
	if len(rules) == 0 {
		t.Fatal("no rule rows found in docs/cli/project.md")
	}
	covered := map[string]bool{}
	for _, name := range projectFixtures(t) {
		if strings.HasPrefix(name, "valid-") {
			continue
		}
		rule, ok := strings.CutPrefix(name, "refuse-")
		if !ok {
			t.Errorf("fixture %s is neither valid-* nor refuse-*", name)
			continue
		}
		rule, _, _ = strings.Cut(rule, "--")
		if !rules[rule] {
			t.Errorf("fixture %s names rule %q, which docs/cli/project.md does not list", name, rule)
		}
		covered[rule] = true
	}
	var missing []string
	for rule := range rules {
		if !covered[rule] {
			missing = append(missing, rule)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("rules without a refusal fixture: %v", missing)
	}
}

// TestProjectLocalAbsolutePath covers what a committed fixture cannot: an
// absolute path in the local file, and the same path refused in the
// manifest.
func TestProjectLocalAbsolutePath(t *testing.T) {
	dir, checkout := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(checkout, "graphvein.yaml"), []byte("version: 1\nrepository: web\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := []byte("version: 1\nname: shop\nmembers:\n  - repository: web\n    path: missing\n")
	local := []byte("version: 1\nmembers:\n  web:\n    path: '" + filepath.ToSlash(checkout) + "'\n")
	p, err := ParseProject(dir, manifest, local)
	if err != nil {
		t.Fatal(err)
	}
	m := p.Members[0]
	if m.Path != filepath.Clean(checkout) || m.PathSource != PathFromLocal || !m.Checkout || m.RepositorySource != RepositoryFromMemberConfig {
		t.Fatalf("member = %+v", m)
	}
	abs := []byte("version: 1\nname: shop\nmembers:\n  - repository: web\n    path: '" + filepath.ToSlash(checkout) + "'\n")
	if _, err := ParseProject(dir, abs, nil); err == nil || !strings.Contains(err.Error(), ProjectLocalFile) {
		t.Fatalf("absolute manifest path: err = %v", err)
	}
}

func TestProjectManifestWriter(t *testing.T) {
	dir := t.TempDir()
	data, err := NewProjectManifest("shop", []ManifestMember{{Repository: "1", Path: "web"}, {Repository: "api", Path: "../api"}})
	if err != nil {
		t.Fatal(err)
	}
	p, err := ParseProject(dir, data, nil)
	if err != nil {
		t.Fatalf("%v\n%s", err, data)
	}
	if p.Members[0].Repository != "1" || p.Members[1].Path != filepath.Clean(filepath.Join(dir, "..", "api")) {
		t.Fatalf("project = %+v", p)
	}
	commented := []byte("# team manifest\nversion: 1\nname: shop # the shop\nmembers:\n  # the frontend\n  - repository: web\n    path: web\n    consumes: [api]\n  - repository: api\n")
	data, err = AddProjectMember(commented, ManifestMember{Repository: "true", Path: "auth"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# team manifest", "# the shop", "# the frontend", "consumes: [api]"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("lost %q:\n%s", want, data)
		}
	}
	p, err = ParseProject(dir, data, nil)
	if err != nil || len(p.Members) != 3 || p.Members[2].Repository != "true" {
		t.Fatalf("after add: %+v %v\n%s", p, err, data)
	}
	if _, err := AddProjectMember([]byte("version: 1\nname: shop\n"), ManifestMember{Repository: "web", Path: "web"}); err == nil {
		t.Fatal("add to a manifest without members succeeded")
	}
}
