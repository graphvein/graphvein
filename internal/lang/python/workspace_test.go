package python

import (
	"strings"
	"testing"
)

const hatchMember = "[project]\nname = \"m\"\n[build-system]\nbuild-backend = \"hatchling.build\"\n"

// workspaceCase builds a resolver over a uv workspace at the repository
// root whose member src/m installs the package m, with overrides applied
// ("-" deletes a file), and returns it.
func workspaceCase(t *testing.T, overrides map[string]string, skipped ...string) *ModuleResolver {
	t.Helper()
	files := map[string]string{
		"pyproject.toml":          "[tool.uv.workspace]\nmembers = [\"src/m\"]\n",
		"src/m/pyproject.toml":    hatchMember,
		"src/m/src/m/__init__.py": "X = 1\n",
		"src/app/__init__.py":     "",
		"src/app/use.py":          "import m\n",
	}
	for k, v := range overrides {
		if v == "-" {
			delete(files, k)
		} else {
			files[k] = v
		}
	}
	facts, raw := syntaxFiles(t, files)
	return NewModuleResolver(facts, raw).WithSkipped(skipped)
}

// TestWorkspaceMembers checks row PS-M2's reading of uv's member set and
// of each member's layout beyond the fixture:
// for each case, whether `import m` in src/app/use.py resolves to the
// member's package (src/m/src/m/__init__.py, over the namespace src/m on
// the ancestor root src), and the python_workspace_member_unread reason,
// if any. A refusal leaves the PS-M1 answer (the namespace src/m).
func TestWorkspaceMembers(t *testing.T) {
	ws := func(body string) string { return "[tool.uv.workspace]\n" + body + "\n" }
	member := func(extra string) string { return hatchMember + extra }
	for _, c := range []struct {
		name      string
		files     map[string]string
		skipped   []string
		installed bool
		reason    string
	}{
		{name: "ok", installed: true},
		{name: "packages and include", files: map[string]string{"src/m/pyproject.toml": member("[tool.hatch.build.targets.wheel]\npackages = [\"./src/m/\"]\ninclude = [\"src/m/*.json\"]\n")}, installed: true},
		{name: "question mark", files: map[string]string{"pyproject.toml": ws(`members = ["src/?"]`)}, installed: true},
		{name: "missing literal member (C1)", files: map[string]string{"pyproject.toml": ws(`members = ["src/m", "src/none"]`)}, installed: true},
		{name: "hidden match without pyproject", files: map[string]string{"pyproject.toml": ws(`members = ["src/m", "src/.*"]`), "src/.h/notes.txt": "x\n"}, installed: true},
		{name: "glob match without pyproject", files: map[string]string{"pyproject.toml": ws(`members = ["src/*"]`)}, reason: "src/app has no pyproject.toml"},
		{name: "exclude crosses / (B1)", files: map[string]string{"pyproject.toml": ws("members = [\"src/m\"]\nexclude = [\"s*\"]")}},
		{name: "exclude other", files: map[string]string{"pyproject.toml": ws("members = [\"src/*\"]\nexclude = [\"src/a?p\"]")}, installed: true},
		{name: "recursive glob", files: map[string]string{"pyproject.toml": ws(`members = ["src/**"]`)}, reason: "is not modelled"},
		{name: "character class", files: map[string]string{"pyproject.toml": ws(`members = ["src/[m]"]`)}, reason: "is not modelled"},
		{name: "other workspace key", files: map[string]string{"pyproject.toml": ws("members = [\"src/m\"]\ndefault-members = []")}, reason: "key default-members"},
		{name: "glob into a skipped directory", files: map[string]string{"pyproject.toml": ws(`members = ["src/m", "vendor/*"]`)}, skipped: []string{"vendor/"}, reason: "the scan skipped"},
		{name: "managed false member (B3)", files: map[string]string{"src/m/pyproject.toml": member("[tool.uv]\nmanaged = false\n")}},
		{name: "managed false root", files: map[string]string{"pyproject.toml": ws(`members = ["src/m"]`) + "[tool.uv]\nmanaged = false\n"}},
		{name: "nested workspace (B3)", files: map[string]string{"src/m/pyproject.toml": member("[tool.uv.workspace]\nmembers = []\n")}, reason: "declares its own workspace"},
		{name: "member without project", files: map[string]string{"src/m/pyproject.toml": "[build-system]\nbuild-backend = \"hatchling.build\"\n"}, reason: "has no [project] table"},
		{name: "duplicate names", files: map[string]string{"pyproject.toml": ws(`members = ["src/m", "src/n"]`), "src/n/pyproject.toml": hatchMember}, reason: "have the same name"},
		{name: "hatch.toml (A1)", files: map[string]string{"src/m/hatch.toml": "[build.targets.wheel]\npackages = [\"pkg/m\"]\n"}, reason: "hatch.toml"},
		{name: "package false", files: map[string]string{"src/m/pyproject.toml": member("[tool.uv]\npackage = false\n")}},
		{name: "virtual project (C4)", files: map[string]string{"src/m/pyproject.toml": "[project]\nname = \"m\"\n"}},
		{name: "other backend", files: map[string]string{"src/m/pyproject.toml": "[project]\nname = \"m\"\n[build-system]\nbuild-backend = \"setuptools.build_meta\"\n"}, reason: "build backend"},
		{name: "dynamic name", files: map[string]string{"src/m/pyproject.toml": "[project]\nname = \"m\"\ndynamic = [\"name\"]\n[build-system]\nbuild-backend = \"hatchling.build\"\n"}, reason: "project.name is dynamic"},
		{name: "module default layout", files: map[string]string{"src/m/src/m/__init__.py": "-", "src/m/m.py": "X = 1\n"}, reason: "selects the module m.py"},
		{name: "namespace default layout", files: map[string]string{"src/m/src/m/__init__.py": "-", "src/m/lib/m/__init__.py": ""}, reason: "namespace layout"},
		{name: "force-include", files: map[string]string{"src/m/pyproject.toml": member("[tool.hatch.build.targets.wheel]\nforce-include = { \"x\" = \"y\" }\n")}, reason: "hatch wheel option force-include"},
		{name: "sources", files: map[string]string{"src/m/pyproject.toml": member("[tool.hatch.build.targets.wheel]\nsources = [\"src\"]\n")}, reason: "hatch wheel option sources"},
		{name: "only-include", files: map[string]string{"src/m/pyproject.toml": member("[tool.hatch.build]\nonly-include = [\"src/m\"]\n")}, reason: "hatch build option only-include"},
		{name: "dev-mode-dirs", files: map[string]string{"src/m/pyproject.toml": member("[tool.hatch.build]\ndev-mode-dirs = [\".\"]\n")}, reason: "hatch build option dev-mode-dirs"},
		{name: "dev-mode-exact", files: map[string]string{"src/m/pyproject.toml": member("[tool.hatch.build.targets.wheel]\ndev-mode-exact = true\n")}, reason: "hatch wheel option dev-mode-exact"},
		{name: "include outside", files: map[string]string{"src/m/pyproject.toml": member("[tool.hatch.build.targets.wheel]\npackages = [\"src/m\"]\ninclude = [\"other/*.py\"]\n")}, reason: "outside the packages"},
		{name: "member pyproject unreadable", files: map[string]string{"src/m/pyproject.toml": "a = abc\n"}, reason: "is not TOML"},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := workspaceCase(t, c.files, c.skipped...)
			got := r.Resolve("src/app/use.py", "m")
			diags := r.WorkspaceDiagnostics()
			if member := got.File == "src/m/src/m/__init__.py"; member != c.installed {
				t.Errorf("import m: %+v, want member package %v", got, c.installed)
			}
			if c.reason == "" {
				if len(diags) != 0 {
					t.Errorf("unexpected diagnostics %v", diags)
				}
				return
			}
			if len(diags) != 1 || diags[0].Code != "python_workspace_member_unread" || !strings.Contains(diags[0].Message, c.reason) {
				t.Errorf("diagnostics %v, want one with %q", diags, c.reason)
			}
		})
	}
}

// TestWorkspaceEscapedRoot checks that the workspace root's own name is
// literal in a member glob (uv escapes it): a root named
// w*x does not take wzx/libs/m as a member.
func TestWorkspaceEscapedRoot(t *testing.T) {
	files, raw := syntaxFiles(t, map[string]string{
		"w*x/pyproject.toml":           "[tool.uv.workspace]\nmembers = [\"libs/*\"]\n",
		"w*x/app/__init__.py":          "",
		"w*x/app/use.py":               "import m\n",
		"wzx/libs/m/pyproject.toml":    hatchMember,
		"wzx/libs/m/src/m/__init__.py": "",
	})
	r := NewModuleResolver(files, raw)
	if site := r.siteFor("w*x/app/use.py"); site != nil {
		t.Errorf("w*x installs %v", site)
	}
}

// TestWorkspaceScope checks which files get a workspace's packages, as uv
// discovers a file's project and workspace: a nested
// project that is not a member, or is excluded, is standalone; a file in
// a member or in the root's own tree is in the workspace.
func TestWorkspaceScope(t *testing.T) {
	files, raw := syntaxFiles(t, map[string]string{
		"pyproject.toml":          "[tool.uv.workspace]\nmembers = [\"src/m\", \"tools/*\"]\nexclude = [\"tools/x\"]\n",
		"src/m/pyproject.toml":    hatchMember,
		"src/m/src/m/__init__.py": "",
		"src/m/tests/t.py":        "import m\n",
		"scripts/run.py":          "import m\n",
		"other/pyproject.toml":    "[project]\nname = \"other\"\n",
		"other/use.py":            "import m\n",
		"tools/x/pyproject.toml":  "[project]\nname = \"x\"\n",
		"tools/x/use.py":          "import m\n",
		"tools/y/pyproject.toml":  "[project]\nname = \"y\"\n",
		"tools/y/use.py":          "import m\n",
	})
	r := NewModuleResolver(files, raw)
	for file, want := range map[string]bool{"src/m/tests/t.py": true, "scripts/run.py": true, "tools/y/use.py": true, "other/use.py": false, "tools/x/use.py": false} {
		if got := len(r.siteFor(file)["m"]) == 1; got != want {
			t.Errorf("%s: installed m %v, want %v", file, got, want)
		}
	}
}

// TestWorkspaceStdlibName checks that a member package named like a
// standard-library module claims nothing for that name through PS-M2:
// the standard library comes before site-packages, so
// the answer is the one PS-M1 gives without the workspace.
func TestWorkspaceStdlibName(t *testing.T) {
	sources := map[string]string{
		"pyproject.toml":                 "[tool.uv.workspace]\nmembers = [\"libs/*\"]\n",
		"libs/json/pyproject.toml":       "[project]\nname = \"json\"\n[build-system]\nbuild-backend = \"hatchling.build\"\n",
		"libs/json/src/json/__init__.py": "",
		"json/notes.txt":                 "a namespace portion on the root\n",
		"app/__init__.py":                "",
		"app/use.py":                     "import json\nimport m\n",
		"libs/m/pyproject.toml":          hatchMember,
		"libs/m/src/m/__init__.py":       "",
		"m/notes.txt":                    "a namespace portion on the root\n",
	}
	files, raw := syntaxFiles(t, sources)
	r := NewModuleResolver(files, raw)
	if r.siteFor("app/use.py")["json"] == nil {
		t.Fatal("the workspace does not install json")
	}
	if got := r.structural("app/use.py", "json"); got.target.file == "libs/json/src/json/__init__.py" {
		t.Errorf("import json resolves to the member package: %+v", got)
	}
	if got := r.structural("app/use.py", "m"); got.target.file != "libs/m/src/m/__init__.py" {
		t.Errorf("import m: %+v, want the member package", got)
	}
}

// TestWorkspacePathSource checks that an editable uv path source is
// installed only when a requirement of the project names it without a
// marker (project.dependencies or a default dependency group, PEP 503
// names), and that a non-editable one is refused.
func TestWorkspacePathSource(t *testing.T) {
	for _, c := range []struct {
		name, project string
		installed     bool
		reason        string
	}{
		{"dependency", "dependencies = [\"Lib_One>=1\"]\n[tool.uv.sources]\nlib-one = { path = \"../lib\", editable = true }\n", true, ""},
		{"dev group", "[dependency-groups]\ndev = [\"lib-one\"]\n[tool.uv.sources]\nlib-one = { path = \"../lib\", editable = true }\n", true, ""},
		{"other group", "[dependency-groups]\nlint = [\"lib-one\"]\n[tool.uv.sources]\nlib-one = { path = \"../lib\", editable = true }\n", false, ""},
		{"optional only", "[project.optional-dependencies]\nx = [\"lib-one\"]\n[tool.uv.sources]\nlib-one = { path = \"../lib\", editable = true }\n", false, ""},
		{"marker", "dependencies = [\"lib-one; sys_platform == 'win32'\"]\n[tool.uv.sources]\nlib-one = { path = \"../lib\", editable = true }\n", false, ""},
		{"undeclared", "[tool.uv.sources]\nlib-one = { path = \"../lib\", editable = true }\n", false, ""},
		{"not editable", "dependencies = [\"lib-one\"]\n[tool.uv.sources]\nlib-one = { path = \"../lib\" }\n", false, "not editable"},
	} {
		t.Run(c.name, func(t *testing.T) {
			project := "[project]\nname = \"app\"\n"
			body := c.project
			if !strings.HasPrefix(body, "dependencies") {
				project += ""
			}
			files, raw := syntaxFiles(t, map[string]string{
				"app/pyproject.toml":          project + body,
				"app/run/__init__.py":         "",
				"app/run/use.py":              "import lib_one\n",
				"lib/pyproject.toml":          "[project]\nname = \"lib-one\"\n[build-system]\nbuild-backend = \"hatchling.build\"\n",
				"lib/src/lib_one/__init__.py": "",
			})
			r := NewModuleResolver(files, raw)
			if got := len(r.siteFor("app/run/use.py")["lib_one"]) == 1; got != c.installed {
				t.Errorf("installed %v, want %v", got, c.installed)
			}
			diags := r.WorkspaceDiagnostics()
			if (c.reason == "") != (len(diags) == 0) || c.reason != "" && !strings.Contains(diags[0].Message, c.reason) {
				t.Errorf("diagnostics %v, want reason %q", diags, c.reason)
			}
		})
	}
}

// TestGlobMatch checks globMatch against the glob crate 0.3.4's
// Pattern::matches_path with the default MatchOptions (the expected values
// were printed by a probe program built against glob 0.3.4, review fix
// S2XF): `*` and `?` cross `/`, hidden names need no literal dot, and
// `{`, `}`, `!` are literal.
func TestGlobMatch(t *testing.T) {
	for _, c := range []struct {
		pattern, path string
		want          bool
	}{
		{"s*", "src/m", true}, {"pk/*", "pk/one/sub", true}, {"src/a?p", "src/app", true},
		{"src/a?p", "src/a/p", true}, {"*", ".hidden", true}, {"a*b", "a/x/b", true},
		{"{x}", "{x}", true}, {"a!b", "a!b", true}, {"libs/skipme", "libs/skipme", true},
		{"libs/skip", "libs/skipme", false}, {"*e", "libs/skipme", true}, {"src/.*", "src/.h", true},
		{"?", "é", true}, {"x*", "x", true},
	} {
		if got := globMatch(c.pattern, c.path); got != c.want {
			t.Errorf("globMatch(%q, %q) = %v, glob 0.3.4 says %v", c.pattern, c.path, got, c.want)
		}
	}
}

// TestWorkspaceProjectName checks that a member whose project.name
// hatchling's is_valid_project_name refuses, read over ASCII, installs
// nothing (review XTR B3): hatchling would not build it, and
// fileNameComponent would otherwise drop characters Python's `\w` keeps
// (`½`, `Ⅷ`, `፩` read as `m_`). Names hatchling's case-insensitive match
// also accepts (`ſ`, U+212A, a trailing newline) are refused as well.
func TestWorkspaceProjectName(t *testing.T) {
	for _, name := range []string{"m½", "mⅧ", "m፩", "mſ", "mK", "ém", "m\\n", "-m", "m-", "m!"} {
		t.Run(name, func(t *testing.T) {
			r := workspaceCase(t, map[string]string{
				"src/m/pyproject.toml":     strings.Replace(hatchMember, `"m"`, `"`+name+`"`, 1),
				"src/m/src/m_/__init__.py": "X = 1\n",
			})
			if site := r.siteFor("src/app/use.py"); len(site) != 0 {
				t.Errorf("the workspace installs %v", site)
			}
			diags := r.WorkspaceDiagnostics()
			if len(diags) != 1 || !strings.Contains(diags[0].Message, "is not a valid project name") {
				t.Errorf("diagnostics %v, want one invalid project name", diags)
			}
		})
	}
	for _, c := range []struct{ name, pkg string }{{"m", "m"}, {"M.x-y_z", "M.x_y_z"}, {"m-x", "m_x"}, {"m9", "m9"}} {
		if got := fileNameComponent(c.name); !validProjectName(c.name) || got != c.pkg {
			t.Errorf("%q: valid %v, file name component %q, want valid and %q", c.name, validProjectName(c.name), got, c.pkg)
		}
	}
}
