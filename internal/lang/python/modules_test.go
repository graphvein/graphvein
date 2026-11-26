package python

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/parse"
)

func syntaxFiles(t *testing.T, sources map[string]string) (map[string]facts.Facts, map[string][]byte) {
	t.Helper()
	files := map[string]facts.Facts{}
	raw := map[string][]byte{}
	var paths []string
	var jobs []parse.Job
	for file, source := range sources {
		raw[file] = []byte(source)
		if strings.HasSuffix(file, ".py") {
			paths = append(paths, file)
		}
	}
	sort.Strings(paths)
	for _, file := range paths {
		jobs = append(jobs, parse.Job{Language: "python", Source: raw[file]})
	}
	err := parse.Consume(context.Background(), jobs, parse.Options{Workers: 1, CacheDir: filepath.Join(t.TempDir(), "cache")}, func(i int, r parse.Result) {
		if r.Err != nil || r.SyntaxError {
			t.Fatalf("%s: %v syntax=%v", paths[i], r.Err, r.SyntaxError)
		}
		files[paths[i]] = Extract(paths[i], raw[paths[i]], r.Root)
	})
	if err != nil {
		t.Fatal(err)
	}
	return files, raw
}

func TestProjectRootsAndBindings(t *testing.T) {
	files, raw := syntaxFiles(t, map[string]string{
		"pyproject.toml": "", "src/driver.py": "from namespace import member\n",
		"src/namespace/member.py": "def action():\n    pass\n",
		"pkg.py":                  "def wrong():\n    pass\n", "pkg/__init__.py": "from .leaf import action as exported\nvalue = 1\n",
		"pkg/leaf.py": "def action():\n    pass\n", "pkg/value.py": "def misleading():\n    pass\n",
		"backend/pyproject.toml": "", "backend/pkg/__init__.py": "local = 1\n", "backend/main.py": "",
		"one/setup.cfg": "", "one/shared.py": "", "two/setup.py": "", "two/shared.py": "",
		"unique/setup.cfg": "", "unique/sibling.py": "", "plain/main.py": "",
		"pkg/relative.py": "from .leaf import action\n", "namespace/sub/use.py": "from .. import target\n", "namespace/target.py": "",
		"cycle/a.py": "from .b import name\n", "cycle/b.py": "from .a import name\n",
		"consumer.py": "from pkg import exported\nimport pkg.leaf\nimport pkg.leaf as alias\nfrom pkg import value\n",
	})
	r := NewModuleResolver(files, raw)
	for _, entry := range []struct{ file, module, want string }{
		{"consumer.py", "pkg", "pkg/__init__.py"}, {"backend/main.py", "pkg", "backend/pkg/__init__.py"},
		{"plain/main.py", "sibling", "unique/sibling.py"}, {"src/driver.py", "namespace.member", "src/namespace/member.py"},
		{"pkg/relative.py", ".leaf", "pkg/leaf.py"}, {"namespace/sub/use.py", "..target", "namespace/target.py"},
	} {
		if got := r.Resolve(entry.file, entry.module); got.File != entry.want {
			t.Errorf("%s %s: %+v", entry.file, entry.module, got)
		}
	}
	if got := r.Resolve("plain/main.py", "shared"); strings.Join(got.Ambiguous, ",") != "one/shared.py,two/shared.py" || got.External {
		t.Fatal(got)
	}
	if got := r.Resolve("consumer.py", "..outside"); got.External || got.File != "" {
		t.Fatal(got)
	}
	if got := r.Callable("consumer.py", 0, "exported"); got.Identity != "function:pkg/leaf.py#action" {
		t.Fatal(got)
	}
	for _, name := range []string{"pkg.leaf.action", "alias.action"} {
		if got := r.Callable("consumer.py", 0, name); got.Identity != "function:pkg/leaf.py#action" {
			t.Fatal(name, got)
		}
	}
	// pkg/__init__.py binds value and pkg/value.py exists: CPython's
	// answer depends on whether pkg.value was imported first (fromList,
	// shared with the import edges' undecided_name), so neither is claimed.
	if got := r.Callable("consumer.py", 0, "value"); got.Module != nil || got.File != "" || got.Identity != "" {
		t.Fatal("binding next to a submodule must be undecided", got)
	}
	if got := r.Callable("cycle/a.py", 0, "name"); got.Identity != "" || got.External {
		t.Fatal(got)
	}
}

func TestPrefixRules(t *testing.T) {
	files, raw := syntaxFiles(t, map[string]string{
		"pyproject.toml": "", "config.py": `from pydantic_settings import BaseSettings as Base
from pydantic import Field
PREFIX = '/constant'
REBOUND = '/first'
REBOUND = '/second'
AUGMENTED = '/first'
AUGMENTED += '/second'
ALIAS = PREFIX
if enabled:
    CONDITIONAL = '/conditional'
GLOBAL = '/global'
READ_GLOBAL = '/read'
def read():
    global READ_GLOBAL
    return READ_GLOBAL
def rebind():
    global GLOBAL
    GLOBAL = '/new'
class Settings(Base):
    API: str = '/api'
    FIELD: str = Field('/field')
    REPEATED = '/first'
    REPEATED = '/second'
class Child(Settings):
    pass
class FakeBase:
    pass
class FakeSettings(FakeBase):
    API = '/fake'
class ConditionalSettings(Base):
    if enabled:
        API = '/conditional'
settings = Settings(_env_file=None)
overridden = Settings(API='/override')
positional = Settings('/positional')
unpacked = Settings(**kwargs)
inherited = Child()
fake = FakeSettings()
conditional_settings = ConditionalSettings()
def local():
    PREFIX = '/local'
`, "consumer.py": "from config import PREFIX, settings\nimport config as c\n",
	})
	r := NewModuleResolver(files, raw)
	for _, entry := range []struct {
		file, name, text, resolution string
		ok                           bool
	}{
		{"consumer.py", "PREFIX", "/constant", "constant", true}, {"consumer.py", "settings.API", "/api", "settings_default", true},
		{"consumer.py", "c.settings.API", "/api", "settings_default", true}, {"config.py", "REBOUND", "", "", false},
		{"config.py", "AUGMENTED", "", "", false}, {"config.py", "ALIAS", "", "", false}, {"config.py", "CONDITIONAL", "", "", false},
		{"config.py", "GLOBAL", "", "", false}, {"config.py", "settings.FIELD", "", "", false}, {"config.py", "settings.REPEATED", "", "", false},
		{"config.py", "READ_GLOBAL", "/read", "constant", true}, {"config.py", "fake.API", "", "", false}, {"config.py", "conditional_settings.API", "", "", false},
		{"config.py", "overridden.API", "", "", false}, {"config.py", "positional.API", "", "", false}, {"config.py", "unpacked.API", "", "", false}, {"config.py", "inherited.API", "", "", false},
	} {
		parts := strings.Split(entry.name, ".")
		e := facts.RouteExpr{Kind: "identifier", Name: parts[0]}
		for _, part := range parts[1:] {
			previous := e
			e = facts.RouteExpr{Kind: "attribute", Name: part, Object: &previous}
		}
		text, resolution, ok := r.Prefix(entry.file, 0, e)
		if text != entry.text || resolution != entry.resolution || ok != entry.ok {
			t.Errorf("%s: %q %q %v", entry.name, text, resolution, ok)
		}
	}
	if _, _, ok := r.Prefix("config.py", r.Scope("config.py", "local"), facts.RouteExpr{Kind: "identifier", Name: "PREFIX"}); ok {
		t.Fatal("local prefix counted as module constant")
	}
}

func TestLiteralStringsAndShadowing(t *testing.T) {
	files, raw := syntaxFiles(t, map[string]string{
		"pyproject.toml": "", "fastapi.py": "class APIRouter:\n    pass\n",
		"consumer.py": `from fastapi import APIRouter
A = '/one' '/two'
B = f'/{{param}}'
C = r'/raw\path'
D = '/\x61\u0062\143'
E = f'/{dynamic}'
F = b'/bytes'
G = """/triple"""
`,
	})
	r := NewModuleResolver(files, raw)
	if got := r.Callable("consumer.py", 0, "APIRouter"); got.External || got.Identity != "class:fastapi.py#APIRouter" {
		t.Fatal(got)
	}
	for name, want := range map[string]string{"A": "/one/two", "B": "/{param}", "C": `/raw\path`, "D": "/abc", "G": "/triple"} {
		got, res, ok := r.Prefix("consumer.py", 0, facts.RouteExpr{Kind: "identifier", Name: name})
		if !ok || got != want || res != "constant" {
			t.Error(name, got, res, ok)
		}
	}
	for _, name := range []string{"E", "F"} {
		if _, _, ok := r.Prefix("consumer.py", 0, facts.RouteExpr{Kind: "identifier", Name: name}); ok {
			t.Fatal(name)
		}
	}
}

func TestNestedDeclarationsSharingEndSpan(t *testing.T) {
	files, raw := syntaxFiles(t, map[string]string{"main.py": "def outer():\n    def inner():\n        pass\nclass C:\n    def method(self):\n        pass\ndef unrelated():\n    pass\n"})
	r := NewModuleResolver(files, raw)
	if got := r.Callable("main.py", r.Scope("main.py", "outer"), "inner"); got.Identity != "function:main.py#outer.inner" {
		t.Fatal(got)
	}
	if got := r.Callable("main.py", 0, "C.method"); got.Identity != "method:main.py#C.method" {
		t.Fatal(got)
	}
	if got := r.Callable("main.py", 0, "C.unrelated"); got.Identity != "" {
		t.Fatal("class member fell through to module", got)
	}
}

func TestImplicitRootPrecedence(t *testing.T) {
	files := map[string]facts.Facts{}
	for _, file := range []string{
		"legacy/pkg/__init__.py", "legacy/pkg/use.py", "legacy/choice.py",
		"legacy/deeper/pkg/__init__.py", "legacy/deeper/choice.py", "legacy/deeper/pkg/use.py",
		"legacy/src/only_src.py", "namespace/use.py", "namespace/flat.py",
		"src/repo_src.py", "src/src/not_recursive.py",
		"one/shared.py", "two/shared.py", "one/unique.py",
		"legacy/shared.py", "legacy/unique.py", "root_wins.py", "legacy/root_wins.py", "legacy/run.py",
	} {
		files[file] = facts.Facts{}
	}
	r := NewModuleResolver(files, map[string][]byte{"one/setup.cfg": nil, "two/pyproject.toml": nil})
	for _, entry := range []struct{ file, module, want string }{
		{"legacy/pkg/use.py", "choice", "legacy/choice.py"},
		{"legacy/deeper/pkg/use.py", "choice", "legacy/deeper/choice.py"},
		{"legacy/pkg/use.py", "unique", "one/unique.py"}, // explicit sibling before implicit ancestor
		{"legacy/pkg/use.py", "root_wins", "root_wins.py"},
		{"consumer.py", "repo_src", "src/repo_src.py"}, // repository src without metadata
	} {
		if got := r.Resolve(entry.file, entry.module); got.File != entry.want {
			t.Errorf("%s %s: %+v", entry.file, entry.module, got)
		}
	}
	if got := r.Resolve("legacy/pkg/use.py", "shared"); strings.Join(got.Ambiguous, ",") != "one/shared.py,two/shared.py" {
		t.Fatal("implicit root hid explicit ambiguity", got)
	}
	for _, entry := range []struct{ file, module string }{
		{"outside/use.py", "choice"},      // implicit siblings never apply
		{"legacy/pkg/use.py", "only_src"}, // implicit roots get no src child
		{"consumer.py", "not_recursive"},  // explicit src expansion is only one level
	} {
		if got := r.Resolve(entry.file, entry.module); !got.External {
			t.Errorf("unexpected implicit root: %s %s: %+v", entry.file, entry.module, got)
		}
	}
	// Namespaces never create roots; a script in one (python namespace/use.py,
	// or a pytest module there) imports its sibling first: refused, neither
	// the sibling nor an external (script_directory_module).
	if got := r.Resolve("namespace/use.py", "flat"); got.External || got.File != "" {
		t.Errorf("script-directory sibling: %+v", got)
	}
	// A script in an implicit root (python legacy/run.py) searches its own
	// directory first, the policy the explicit roots first: refused only
	// when the two answer differently (script_directory_module).
	for _, module := range []string{"root_wins", "unique"} {
		if got := r.Resolve("legacy/run.py", module); got.External || got.File != "" || len(got.Ambiguous) > 0 {
			t.Errorf("implicit-root script %s: %+v", module, got)
		}
	}
	for _, entry := range []struct{ module, want string }{
		{"choice", "legacy/choice.py"}, {"pkg", "legacy/pkg/__init__.py"}, {"repo_src", "src/repo_src.py"},
	} {
		if got := r.Resolve("legacy/run.py", entry.module); got.File != entry.want {
			t.Errorf("implicit-root script %s: %+v, want %s", entry.module, got, entry.want)
		}
	}
	// Coincident metadata and package roots are explicit, never duplicated in
	// the fallback list and never allowed to create artificial ambiguity.
	r = NewModuleResolver(files, map[string][]byte{"legacy/setup.cfg": nil})
	for _, root := range r.implicit {
		if root == "legacy" {
			t.Fatal("explicit root retained as implicit")
		}
	}
}

// TestResolverSharesStructuralRefusals checks four cases
// on Resolve, which calls, routes and models read: it is the structural
// rows' resolution, refusals included, not a second search.
func TestResolverSharesStructuralRefusals(t *testing.T) {
	files, raw := syntaxFiles(t, map[string]string{
		// B9: CPython binds app to svc/app, which lacks db: the import is
		// refused, so neither the call nor a model may reach app/db.py.
		"svc/pyproject.toml": "", "svc/app/__init__.py": "",
		"svc/app/main.py": "from app.db import helper\nimport app.db as db\nhelper()\n",
		"app/db.py":       "def helper():\n    pass\n",
		// B1: the meta path serves sys; a repository sys.py is never loaded.
		"sys.py": "def exit():\n    pass\n", "uses_sys.py": "import sys\nsys.exit()\n",
		// An unread extension module comes before a.py (shadowed_module).
		"ext/__init__.py": "", "ext/a.py": "def f():\n    pass\n", "ext/a.cpython-314-x86_64-linux-gnu.so": "",
		"uses_ext.py": "from ext.a import f\nf()\n",
		// B5: a directory of data files is a namespace package.
		"static/style.css": "",
	})
	r := NewModuleResolver(files, raw).WithSkipped([]string{"lnkd/"}).WithDirectories([]string{"empty"})
	for _, c := range []struct{ file, name string }{
		{"svc/app/main.py", "helper"}, {"svc/app/main.py", "db.helper"}, {"uses_ext.py", "f"},
	} {
		if got := r.Callable(c.file, 0, c.name); got.Identity != "" || got.External {
			t.Errorf("%s %s: resolved through a refused import: %+v", c.file, c.name, got)
		}
	}
	if got := r.Callable("uses_sys.py", 0, "sys.exit"); !got.External || got.Identity != "" {
		t.Errorf("sys.exit: %+v", got)
	}
	for _, name := range []string{"static", "empty"} {
		if got := r.Resolve("uses_sys.py", name); got.External || len(got.Dirs) != 1 {
			t.Errorf("%s: want a namespace package, got %+v", name, got)
		}
	}
	// B3: a symlinked directory (scan.Result.Skipped "lnkd/") is unread.
	if got := r.Resolve("uses_sys.py", "lnkd.r"); got.External || got.found() {
		t.Errorf("lnkd.r: %+v", got)
	}
}
