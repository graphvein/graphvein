package rust

import (
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/lang/facts"
)

func TestLibraryReexport(t *testing.T) {
	files := map[string]facts.Facts{
		"api/src/lib.rs":    extract(t, "api/src/lib.rs", `pub mod routes; pub use routes::factory; pub const PREFIX: &str = "/kit";`),
		"api/src/routes.rs": extract(t, "api/src/routes.rs", `pub fn factory() {}`),
		"app/src/lib.rs":    extract(t, "app/src/lib.rs", `use route_kit::{factory, PREFIX}; pub fn app() { factory(); }`),
	}
	r := NewModuleResolver(files, map[string][]byte{"api/Cargo.toml": []byte("[package]\nname = \"route-kit\"\n"), "app/Cargo.toml": []byte("[package]\nname = \"app\"\n")})
	for _, name := range []string{"factory", "route_kit::factory"} {
		b := r.Resolve("app/src/lib.rs", 0, name)
		if b.Identity != "function:api/src/routes.rs#factory" {
			t.Errorf("%s: %+v; modules=%+v", name, b, r.crates)
		}
	}
}

func TestExternalGlobDoesNotHideCratePaths(t *testing.T) {
	file := "src/lib.rs"
	files := map[string]facts.Facts{file: extract(t, file, `use axum::routing::*; use axum as axum; fn f() { axum::Router::new(); get(h); }`)}
	r := NewModuleResolver(files, map[string][]byte{"Cargo.toml": []byte("[package]\nname='app'\n")})
	for path, want := range map[string]string{"axum::Router::new": "Router::new", "get": "routing::get"} {
		b := r.Resolve(file, 0, path)
		if b.External != "axum" || b.Name != want {
			t.Fatal(path, b)
		}
	}
}

func TestMethodsDoNotBecomeBareBindings(t *testing.T) {
	file := "src/lib.rs"
	f := extract(t, file, `struct S; impl S { fn build() {} } trait T { fn default() { use std::mem::drop as discard; discard(1); } } fn f() { S::build(); build(); }`)
	files := map[string]facts.Facts{file: f}
	r := NewModuleResolver(files, map[string][]byte{"Cargo.toml": []byte("[package]\nname='app'\n")})
	if b := r.Resolve(file, 0, "S::build"); b.Identity != "method:src/lib.rs#S.build" {
		t.Fatal(b)
	}
	if b := r.Resolve(file, 0, "build"); b.Identity != "" {
		t.Fatal("associated method bound as free function", b)
	}
	if len(f.Rust.Functions) != 3 {
		t.Fatal("trait defaults not cached", f.Rust.Functions)
	}
}

func TestCurrentInventoryResolution(t *testing.T) {
	files := map[string]facts.Facts{}
	for file, source := range map[string]string{
		"pkg/root.rs":         `mod normal; #[path = "special.rs"] mod custom; mod inline { #[path = "x.rs"] mod child; pub use child::h; } pub use normal::h as exposed; use normal::{self, h as alias}; use custom::*; use normal::h; use custom::h as _; fn run() { alias(); self::h(); crate::exposed(); { use custom::h as local; local(); } }`,
		"pkg/normal.rs":       `pub fn h() {} mod child; pub use child::leaf;`,
		"pkg/normal/child.rs": `pub fn leaf() {} use super::h; fn call() { h(); super::super::custom::h(); }`,
		"pkg/special.rs":      `pub fn h() {}`,
		"pkg/inline/x.rs":     `pub fn h() {}`,
		"other/main.rs":       `use kit::{exposed, inline::h}; fn main() { exposed(); h(); ::kit::normal::leaf(); }`,
	} {
		files[file] = extract(t, file, source)
	}
	sources := map[string][]byte{"pkg/Cargo.toml": []byte("[package]\nname='hyphen-kit'\nedition='2021'\n[lib]\nname='kit'\npath='root.rs'\n"), "other/Cargo.toml": []byte("[package]\nname='app'\nedition='2021'\n[dependencies]\nhyphen-kit = { path = '../pkg' }\n[[bin]]\npath='main.rs'\n")}
	r := NewModuleResolver(files, sources)
	for _, tc := range []struct{ file, path, id string }{
		{"pkg/root.rs", "exposed", "function:pkg/normal.rs#h"},
		{"pkg/root.rs", "alias", "function:pkg/normal.rs#h"},
		{"pkg/root.rs", "normal::leaf", "function:pkg/normal/child.rs#leaf"},
		{"pkg/root.rs", "h", "function:pkg/normal.rs#h"},
		{"pkg/root.rs", "inline::h", "function:pkg/inline/x.rs#h"},
		{"other/main.rs", "exposed", "function:pkg/normal.rs#h"},
		{"other/main.rs", "::kit::normal::leaf", "function:pkg/normal/child.rs#leaf"},
	} {
		if b := r.Resolve(tc.file, 0, tc.path); b.Identity != tc.id {
			t.Errorf("%s: %+v", tc.path, b)
		}
	}
	_, edges, ds := LinkWithResolver(files, r)
	for _, d := range ds {
		if d.Code != "unresolved_calls" {
			t.Error(d)
		}
	}
	local := 0
	for _, e := range edges {
		if e.Kind == "calls" && e.Target == "function:pkg/special.rs#h" {
			local++
		}
	}
	if local != 2 {
		t.Fatal("block import and repeated super", local)
	}
	// Leading :: bypasses a lexical module named kit; bare kit does not.
	files["other/main.rs"] = extract(t, "other/main.rs", `mod kit { pub fn exposed() {} } fn main() { kit::exposed(); ::kit::exposed(); }`)
	r = NewModuleResolver(files, sources)
	if b := r.Resolve("other/main.rs", 0, "kit::exposed"); b.Identity != "function:other/main.rs#kit.exposed" {
		t.Fatal(b)
	}
	if b := r.Resolve("other/main.rs", 0, "::kit::exposed"); b.Identity != "function:pkg/normal.rs#h" {
		t.Fatal(b)
	}
}

func TestAmbiguityAndCycles(t *testing.T) {
	files := map[string]facts.Facts{}
	for file, source := range map[string]string{
		"src/lib.rs":      `mod both; mod a; mod b; mod x; mod y; use a::*; use b::*; use x::*; pub use y::relay;`,
		"src/both.rs":     `pub fn f() {}`,
		"src/both/mod.rs": `pub fn f() {}`,
		"src/a.rs":        `pub fn f() {}`,
		"src/b.rs":        `pub use crate::a::f;`,
		"src/x.rs":        `pub use crate::y::*;`,
		"src/y.rs":        `pub use crate::x::*; pub use crate::a::f as relay;`,
	} {
		files[file] = extract(t, file, source)
	}
	sources := map[string][]byte{"Cargo.toml": []byte("[package]\nname='kit'\n")}
	r := NewModuleResolver(files, sources)
	if b := r.Resolve("src/lib.rs", 0, "f"); b.Identity != "function:src/a.rs#f" {
		t.Fatal("agreeing globs", b)
	}
	if b := r.Resolve("src/lib.rs", 0, "relay"); b.Identity != "function:src/a.rs#f" {
		t.Fatal("cyclic globs with terminal", b)
	}
	if b := r.Resolve("src/lib.rs", 0, "x::missing"); b.Identity != "" || b.External != "" {
		t.Fatal("cycle guessed", b)
	}
	if len(r.diagnostics) != 1 || r.diagnostics[0].Code != "ambiguous_module" || !strings.Contains(r.diagnostics[0].Message, "src/both.rs, src/both/mod.rs") {
		t.Fatal(r.diagnostics)
	}
	files["src/b.rs"] = extract(t, "src/b.rs", `pub fn f() {}`)
	r = NewModuleResolver(files, sources)
	if b := r.Resolve("src/lib.rs", 0, "f"); !b.Ambiguous {
		t.Fatal("conflicting globs", b)
	}
	files["other/lib.rs"] = extract(t, "other/lib.rs", `pub fn f() {}`)
	sources["other/Cargo.toml"] = []byte("[package]\nname='kit'\n[lib]\npath='lib.rs'\n")
	r = NewModuleResolver(files, sources)
	if b := r.Resolve("src/lib.rs", 0, "::kit::f"); !b.Ambiguous {
		t.Fatal("duplicate library", b)
	}
}

func TestManifestAndStringRules(t *testing.T) {
	m, err := readManifest("[package]\nname=\"k\\u0069t\" # comment\n[lib]\npath='root file.rs'\n")
	if err != nil || m.name != "kit" || m.libPath != "root file.rs" {
		t.Fatal("TOML string decoding", m, err)
	}
	for text, want := range map[string]string{`"/a\u{62}"`: "/ab", `r##"/a\b"##`: `/a\b`, `"/a\x62"`: "/ab"} {
		if got, ok := StringLiteral(text); !ok || got != want {
			t.Fatal(text, got, ok)
		}
	}
	for _, text := range []string{`b"/bytes"`, `concat!("/a", "/b")`, `"\xFF"`} {
		if _, ok := StringLiteral(text); ok {
			t.Fatal("invalid string", text)
		}
	}
	files := map[string]facts.Facts{"src/lib.rs": extract(t, "src/lib.rs", `fn h() {}`)}
	r := NewModuleResolver(files, map[string][]byte{"Cargo.toml": []byte("[package]\nname=123\n")})
	if len(r.diagnostics) != 1 || r.diagnostics[0].Code != "config_parse_error" || r.Resolve("src/lib.rs", 0, "h").Identity == "" {
		t.Fatal("invalid manifest fallback", r.diagnostics)
	}
}

func TestPathAttributeDirectories(t *testing.T) {
	files := map[string]facts.Facts{}
	for file, source := range map[string]string{
		"src/lib.rs":              `mod foo; #[path="thread_files"] mod thread { #[path="tls.rs"] mod local; }`,
		"src/foo.rs":              `#[path="sibling file.rs"] mod sibling; mod inline { #[path="inner.rs"] mod inner; }`,
		"src/sibling file.rs":     `pub fn h() {}`,
		"src/foo/inline/inner.rs": `pub fn h() {}`,
		"src/thread_files/tls.rs": `pub fn h() {}`,
	} {
		files[file] = extract(t, file, source)
	}
	r := NewModuleResolver(files, map[string][]byte{"Cargo.toml": []byte("[package]\nname='kit'\n")})
	for name, file := range map[string]string{"foo::sibling::h": "src/sibling file.rs", "foo::inline::inner::h": "src/foo/inline/inner.rs", "thread::local::h": "src/thread_files/tls.rs"} {
		b := r.Resolve("src/lib.rs", 0, name)
		if b.Identity != facts.ID("function", file, "h") {
			t.Fatal(name, b)
		}
	}
}

// TestDuplicateCrateNameDiagnostics checks that two library crates with one
// name report duplicate_crate_name on each declaring Cargo.toml, in manifest
// order, never on the repository root "." the contract rejects.
func TestDuplicateCrateNameDiagnostics(t *testing.T) {
	files := map[string]facts.Facts{
		"a/src/lib.rs": extract(t, "a/src/lib.rs", `pub fn f() {}`),
		"b/src/lib.rs": extract(t, "b/src/lib.rs", `pub fn f() {}`),
	}
	r := NewModuleResolver(files, map[string][]byte{"b/Cargo.toml": []byte("[package]\nname = \"db\"\n"), "a/Cargo.toml": []byte("[package]\nname = \"db\"\n")})
	var got []string
	for _, d := range r.diagnostics {
		if d.Code == "duplicate_crate_name" {
			got = append(got, d.File+" "+d.Message)
		}
	}
	want := []string{"a/Cargo.toml db: a/src/lib.rs, b/src/lib.rs", "b/Cargo.toml db: a/src/lib.rs, b/src/lib.rs"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("diagnostics:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// TestManifestStatements checks the manifest reader on the TOML forms of
// a later fix: multi-line strings and values spread over several
// lines are one value (never a header or an assignment), a value the reader
// does not decode is unknown, and a manifest it cannot read is refused.
func TestManifestStatements(t *testing.T) {
	m, err := readManifest("[package]\nname = \"\"\"kit\"\"\"\ndescription = \"\"\"\n[lib]\npath = \"wrong.rs\"\n\"\"\"\nkeywords = [\n  \"a]\", # ]\n  'b',\n]\nreadme = '''\n[[bin]]\npath = 'nope.rs'\n'''\n[dependencies]\ninline = { path = \"../inline\",\n  version = \"1\", }\nescaped = { path = \"../\\x65\" }\nml = { path = '''../ml''' }\n[lib]\npath = \"src/root.rs\"\n")
	if err != nil {
		t.Fatal(err)
	}
	if m.name != "kit" || m.libPath != "src/root.rs" || len(m.targets["bin"]) != 0 {
		t.Errorf("multi-line values misread: %+v", m)
	}
	deps := m.dependencies(false)
	if d := deps["inline"]; len(d) != 1 || d[0].Source != "path" || d[0].Path != "../inline" {
		t.Errorf("multi-line inline table: %+v", d)
	}
	if d := deps["escaped"]; len(d) != 1 || d[0].Source != "unknown" {
		t.Errorf("TOML 1.1 escape read: %+v", d)
	}
	if d := deps["ml"]; len(d) != 1 || d[0].Source != "path" || d[0].Path != "../ml" {
		t.Errorf("multi-line literal path: %+v", d)
	}
	for _, text := range []string{
		"[package]\nname = \"kit\"\ndescription = \"\"\"\nnever closed\n",
		"[package]\nname = \"kit\"\nlist = [\n",
		"lib.path = \"x.rs\"\n[package]\nname = \"kit\"\n",
		"package = { name = \"kit\" }\n",
		"[package]\nname = \"kit\"\nautobins = \"no\"\n",
		"[package]\nname = \"kit\"\n[[bin]]\npath = 1\n",
		"[package]\nname = \"kit\"\nbuild.workspace = true\n",
		"[package]\nname = \"kit\"\nloose line\n",
	} {
		if _, err := readManifest(text); err == nil {
			t.Errorf("read a manifest it cannot read: %q", text)
		}
	}
}

// TestPackageRoots checks target discovery against cargo 1.97's own (probed
// with `cargo metadata`; the explicit-target rows).
func TestPackageRoots(t *testing.T) {
	for _, tc := range []struct {
		name, manifest string
		files          []string
		want           string
		fails          bool
	}{
		{"auto keys off", "[package]\nname='a'\nedition='2021'\nautobins=false\nautotests=false\n",
			[]string{"src/main.rs", "src/bin/x.rs", "src/lib.rs", "tests/t.rs", "build.rs"}, "lib:src/lib.rs custom-build:build.rs", false},
		{"explicit and discovered", "[package]\nname='b'\nedition='2021'\nbuild=false\n[[bin]]\nname='x'\npath='cli/x.rs'\n[[bin]]\nname='y'\n[[test]]\nname='t'\n[[test]]\nname='z'\npath='tests/u.rs'\n",
			[]string{"src/main.rs", "src/bin/x.rs", "src/bin/y.rs", "cli/x.rs", "tests/t.rs", "tests/u.rs", "build.rs"}, "bin:cli/x.rs bin:src/bin/y.rs bin:src/main.rs test:tests/t.rs test:tests/u.rs", false},
		{"edition 2015 explicit only", "[package]\nname='c'\n[[test]]\nname='t'\n[[bin]]\nname='c'\n",
			[]string{"src/main.rs", "tests/t.rs", "tests/u.rs", "src/bin/q.rs"}, "bin:src/main.rs test:tests/t.rs", false},
		{"named example with autoexamples off", "[package]\nname='h'\nedition='2021'\nautoexamples=false\n[[example]]\nname='sub'\n[[bench]]\nname='bb'\npath='benches/b.rs'\n",
			[]string{"src/main.rs", "examples/e.rs", "examples/sub/main.rs", "benches/b.rs"}, "bin:src/main.rs example:examples/sub/main.rs bench:benches/b.rs", false},
		{"autolib off", "[package]\nname='f'\nedition='2021'\nautolib=false\nbuild='tools/b.rs'\n",
			[]string{"src/lib.rs", "src/main.rs", "tools/b.rs"}, "bin:src/main.rs custom-build:tools/b.rs", false},
		{"name without a file", "[package]\nname='d'\nedition='2021'\n[[bin]]\nname='other'\n", []string{"src/main.rs", "src/other.rs"}, "", true},
		{"duplicate discovered name", "[package]\nname='j'\nedition='2021'\n", []string{"src/bin/x/main.rs", "src/bin/x.rs"}, "", true},
		{"main.rs and src/bin with the package name", "[package]\nname='l'\nedition='2021'\n", []string{"src/main.rs", "src/bin/l.rs"}, "", true},
	} {
		m, err := readManifest(tc.manifest)
		if err != nil {
			t.Fatal(tc.name, err)
		}
		files := map[string]facts.Facts{}
		for _, f := range tc.files {
			files["p/"+f] = facts.Facts{}
		}
		root, _, ok := workspaceRoot("p", map[string]manifest{"p": m}, nil)
		roots, err := packageRoots("p", m, packageEdition(m, root, ok), files)
		if (err != nil) != tc.fails {
			t.Errorf("%s: error %v", tc.name, err)
			continue
		}
		var got []string
		for _, r := range roots {
			got = append(got, r.Kind+":"+strings.TrimPrefix(r.File, "p/"))
		}
		if strings.Join(got, " ") != tc.want {
			t.Errorf("%s: roots %q, want %q", tc.name, strings.Join(got, " "), tc.want)
		}
	}
}
