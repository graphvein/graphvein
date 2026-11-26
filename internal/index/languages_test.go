package index_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGoResolutionBoundaries(t *testing.T) {
	root := t.TempDir()
	for p, source := range map[string]string{
		"go.mod": "module example.test/root\n",
		"main.go": `package main
import alias "example.test/root/v2"
type User struct{}
func (u *User) Save() { u.Save() }
func local() {}
func run(local func()) { local(); alias.Help(); var u User; u.Save(); User.Save(u); func(u Unknown) { u.Save() }(u) }
func plain() { helper() }
func constant() { const local = 1; local() }
`,
		"other.go":             "package main\nfunc helper() {}\n",
		"default.go":           "package main\nimport \"example.test/root/v2\"\nfunc DefaultAlias() { surprising.Help() }\n",
		"v2/util.go":           "package surprising\nfunc Help() {}\n",
		"v2/util_test.go":      "package surprising\nfunc TestHelper() {}\n",
		"main_test.go":         "package main\nimport util \"example.test/root/v2\"\nfunc TestCalls() { util.TestHelper(); util.ExternalOnly() }\n",
		"v2/external_test.go":  "package surprising_test\nfunc ExternalOnly() {}\n",
		"nested/go.mod":        "module example.test/nested\n",
		"nested/main.go":       "package nested\nimport \"example.test/nested/child\"\nfunc Run() { child.Help() }\n",
		"nested/child/util.go": "package child\nfunc Help() {}\n",
	} {
		full := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(source), 0644); err != nil {
			t.Fatal(err)
		}
	}
	s := open(t)
	opt := options(t)
	_, summary, _ := emit(t, root, s, opt)
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	calls := map[string]bool{}
	for _, e := range snapshot.Edges {
		if e.Kind == "calls" {
			calls[e.Source+" -> "+e.Target] = true
		}
	}
	for _, pair := range []string{
		"function:main.go#run -> function:v2/util.go#Help",
		"function:main.go#run -> method:main.go#User.Save",
		"method:main.go#User.Save -> method:main.go#User.Save",
		"function:main.go#plain -> function:other.go#helper",
		"function:default.go#DefaultAlias -> function:v2/util.go#Help",
		"function:nested/main.go#Run -> function:nested/child/util.go#Help",
	} {
		if !calls[pair] {
			t.Error("missing", pair)
		}
	}
	if calls["function:main.go#run -> function:main.go#local"] {
		t.Fatal("parameter shadow resolved")
	}
	// Another directory's _test.go files are never part of the package an
	// import reads (go/build TestGoFiles are visible only to the package's
	// own tests; golang.ImportPackage), so util.TestHelper()
	// is undefined for the go command and binds nothing.
	if calls["function:main_test.go#TestCalls -> function:v2/util_test.go#TestHelper"] {
		t.Fatal("import alias resolved into another directory's test file")
	}
	if calls["function:main_test.go#TestCalls -> function:v2/external_test.go#ExternalOnly"] {
		t.Fatal("import alias resolved into external test package")
	}
	if calls["function:main.go#constant -> function:main.go#local"] {
		t.Fatal("constant shadow resolved")
	}
	unresolved := 0
	for _, d := range snapshot.Diagnostics {
		if d.Code == "unresolved_calls" && d.File == "main.go" {
			if d.Message != "5 calls could not be resolved without a type checker (external=0, unknown=5)" {
				t.Fatal(d)
			}
			unresolved++
		}
		if d.Code == "parse_failed" || d.Code == "extract_failed" {
			t.Fatal(d)
		}
	}
	if unresolved != 1 {
		t.Fatal("missing unresolved count", summary)
	}
	// Current module metadata and target symbols participate in relinking even
	// when the caller's per-file extraction is replayed unchanged.
	if err := os.WriteFile(filepath.Join(root, "other.go"), []byte("package main\nfunc renamed() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	_, changed, _ := emit(t, root, s, opt)
	if changed.Parsed != 1 || changed.Reused != summary.Parsed-1 {
		t.Fatal(changed)
	}
	after, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range after.Edges {
		if e.Kind == "calls" && strings.Contains(e.Target, "#helper") {
			t.Fatal("stale cached call", e)
		}
	}
}

func TestPythonResolutionBoundaries(t *testing.T) {
	root := t.TempDir()
	for p, source := range map[string]string{
		"pyproject.toml":          "[project]\nname = 'python-boundaries'\n",
		"src/pkg/__init__.py":     "",
		"src/namespace/member.py": "def action():\n    pass\n",
		"src/driver.py":           "from namespace import member as m\ndef run():\n    m.action()\n",
		"src/pkg/util.py":         "def help():\n    pass\n",
		"src/pkg/sub/__init__.py": "",
		"src/pkg/sub/main.py": `from ..util import help as imported
from pkg.util import help
import os.path
FIRST, SECOND = 1, 2
FIRST = 3
def run(help):
    help()
    imported()
class User:
    def helper(self):
        pass
    def save(self):
        helper()
        self.helper()
    def rebound(self):
        self = object()
        self.helper()
    @staticmethod
    def no_receiver():
        self.helper()
def helper():
    pass
def outside():
    class Local:
        def save(self):
            self.save()
    def validate():
        imported()
    validate()
`,
	} {
		full := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(source), 0644); err != nil {
			t.Fatal(err)
		}
	}
	s := open(t)
	opt := options(t)
	_, summary, _ := emit(t, root, s, opt)
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	calls := map[string]bool{}
	for _, e := range snapshot.Edges {
		if e.Kind == "calls" {
			calls[e.Source+" -> "+e.Target] = true
		}
	}
	constants := map[string]bool{}
	for _, n := range snapshot.Nodes {
		if n.Kind == "constant" {
			constants[n.QualifiedName] = true
		}
	}
	for _, name := range []string{"FIRST", "SECOND", "FIRST@2"} {
		if !constants[name] {
			t.Error("missing constant", name)
		}
	}
	for _, e := range snapshot.Edges {
		if e.Kind == "calls" && (strings.Contains(e.Source, "#User.rebound") || strings.Contains(e.Source, "#User.no_receiver")) {
			t.Fatal("untyped self resolved", e)
		}
	}
	for _, pair := range []string{
		"function:src/pkg/sub/main.py#run -> function:src/pkg/util.py#help",
		"function:src/driver.py#run -> function:src/namespace/member.py#action",
		"method:src/pkg/sub/main.py#User.save -> function:src/pkg/sub/main.py#helper",
		"method:src/pkg/sub/main.py#User.save -> method:src/pkg/sub/main.py#User.helper",
		"method:src/pkg/sub/main.py#outside.Local.save -> method:src/pkg/sub/main.py#outside.Local.save",
		"function:src/pkg/sub/main.py#outside -> function:src/pkg/sub/main.py#outside.validate",
	} {
		if !calls[pair] {
			t.Error("missing", pair)
		}
	}
	for _, d := range snapshot.Diagnostics {
		if d.Code == "unresolved_import" || d.Code == "parse_failed" || d.Code == "extract_failed" {
			t.Fatal(d)
		}
	}
	if summary.Parsed != 6 {
		t.Fatal(summary)
	}
	if err := os.Remove(filepath.Join(root, "src/pkg/util.py")); err != nil {
		t.Fatal(err)
	}
	emit(t, root, s, opt)
	after, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range after.Edges {
		if e.Kind == "calls" && strings.Contains(e.Target, "pkg/util.py") {
			t.Fatal("deleted import survived", e)
		}
	}
}

// TestLanguagesWithoutExtractionStayFileLevel: files of languages without an
// extractor keep only their file node. Java (T021-B2), C# (T021-B3) and
// Dart (T021-B4) are parsed, so their unparsable files are BE-2 files: a
// file node and one syntax_error warning each.
func TestLanguagesWithoutExtractionStayFileLevel(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{"main.java", "main.cs", "main.dart", "page.html", "style.css", "data.json", "config.yml", "config.toml", "README.md", "schema.sql", "run.sh", "unknown.txt"} {
		if err := os.WriteFile(filepath.Join(root, p), []byte("text\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	s := open(t)
	_, summary, _ := emit(t, root, s, options(t))
	if summary.Files != 12 || summary.Nodes != 12 || summary.Parsed != 4 || summary.Diagnostics != 3 {
		t.Fatal(summary)
	}
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	for i, file := range []string{"main.cs", "main.dart", "main.java"} {
		if d := snapshot.Diagnostics[i]; d.File != file || d.Code != "syntax_error" {
			t.Fatal(d)
		}
	}
}
