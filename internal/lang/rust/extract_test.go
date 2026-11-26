package rust

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/parse"
)

func extract(t *testing.T, file, source string) facts.Facts {
	t.Helper()
	results, err := parse.Batch(context.Background(), []parse.Job{{Language: "rust", Source: []byte(source)}}, parse.Options{CacheDir: filepath.Join(t.TempDir(), "cache")})
	if err != nil || results[0].Err != nil || results[0].SyntaxError {
		t.Fatal(err, results)
	}
	return Extract(file, []byte(source), results[0].Root)
}

func TestReceiverIdentities(t *testing.T) {
	f := extract(t, "src/lib.rs", `struct Foo<T>(T);
impl<T> Foo<T> { fn save(&self) {} }
impl<'a,T> &'a Foo<T> { fn borrow(&self) {} }
impl<T> crate::Foo<T> { fn scoped(&self) {} }
impl crate::Display<Foo<i32>> for Foo<i32> { fn save(&self) {} }
impl Box<Foo<i32>> { fn boxed() {} }
impl [i32] { fn sliced() {} }
impl (i32, i32) { fn tupled() {} }
impl dyn Display { fn dynamic() {} }
trait Display<T> { fn fmt(&self) {} }
#[cfg(a)] fn choice() {}
#[cfg(b)] fn choice() {}
mod inner { struct Foo; impl Foo { fn save() {} } }
`)
	for _, q := range []string{"Foo.save", "Foo.borrow", "Foo.scoped", "Foo.save@Display", "Box<Foo<i32>>.boxed", "[i32].sliced", "(i32,i32).tupled", "dyn Display.dynamic", "Display.fmt", "choice", "choice@2", "inner.Foo.save"} {
		if f.Exports[q] == "" {
			t.Errorf("missing receiver identity %s", q)
		}
	}
}

func TestCrateRootsAndResolutionBoundaries(t *testing.T) {
	files := map[string]facts.Facts{}
	for file, source := range map[string]string{
		"src/bin/tool.rs":   `mod helper; use crate::helper::{run as task}; fn main() { task(); crate::helper::run(); }`,
		"src/bin/helper.rs": `pub fn run() {}`,
		"src/lib.rs":        `mod nested; mod absent; use crate::nested::{Thing, one::*}; use crate::unavailable::f; use std::mem as memory; fn f() {} fn shadow(f: fn()) { f(); } fn local() { let f = || {}; f(); } fn bound() { use crate::nested::one::hidden; hidden(); } fn unbound() { hidden(); } fn run() { Thing::save(); memory::drop(1); nope(); }`,
		"src/nested.rs":     `pub struct Thing; trait A {} trait B {} impl A for Thing { fn save() {} } impl B for Thing { fn save() {} } pub mod one { pub fn hidden() {} }`,
		"src/main.rs":       `mod model; impl crate::model::User { fn save() {} } fn run() { model::User::save(); } fn main() { run(); }`,
		"src/model.rs":      `pub struct User;`,
		"other/src/lib.rs":  `fn run() {} fn main() { run(); }`,
	} {
		files[file] = extract(t, file, source)
	}
	_, edges, diagnostics := Link(files, map[string][]byte{"Cargo.toml": []byte("[package]\nname = 'toolkit'\nedition = '2021'\n"), "other/Cargo.toml": []byte("[package]\nname = 'other'\nedition = '2021'\n")})
	calls := map[string]bool{}
	for _, e := range edges {
		if e.Kind == "calls" {
			calls[e.Source+" -> "+e.Target] = true
		}
	}
	for _, pair := range []string{"function:src/bin/tool.rs#main -> function:src/bin/helper.rs#run", "function:src/main.rs#main -> function:src/main.rs#run", "function:other/src/lib.rs#main -> function:other/src/lib.rs#run"} {
		if !calls[pair] {
			t.Error("missing", pair)
		}
	}
	if !calls["function:src/lib.rs#bound -> function:src/nested.rs#one.hidden"] || !calls["function:src/lib.rs#unbound -> function:src/nested.rs#one.hidden"] {
		t.Fatal("function-local import scope", calls)
	}
	if !calls["function:src/main.rs#run -> method:src/main.rs#User.save"] {
		t.Fatal("cross-file inherent impl", calls)
	}
	for pair := range calls {
		if pair == "function:src/lib.rs#run -> method:src/nested.rs#Thing.save%40A" || pair == "function:src/lib.rs#run -> method:src/nested.rs#Thing.save%40B" || pair == "function:src/lib.rs#shadow -> function:src/lib.rs#f" || pair == "function:src/lib.rs#local -> function:src/lib.rs#f" {
			t.Error("speculative call", pair)
		}
	}
	counts := map[string]int{}
	for _, d := range diagnostics {
		counts[d.Code]++
	}
	if counts["unresolved_import"] != 1 || counts["unresolved_module"] != 1 {
		t.Fatal(diagnostics)
	}
}
