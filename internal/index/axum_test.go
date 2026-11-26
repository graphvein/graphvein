package index_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/golden"
)

func TestAxumGoldenAndMetadataReplay(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/axum")); err != nil {
		t.Fatal(err)
	}
	s, opt := open(t), options(t)
	data, first, _ := emit(t, root, s, opt)
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	golden.Check(t, "../../testdata/fixtures/axum.golden.json", fastapiSnapshot(t, snapshot))
	again, warm, writes := emit(t, root, s, opt)
	if string(data) != string(again) || warm.Parsed != 0 || warm.Reused != first.Parsed || writes.Files+writes.Nodes+writes.Edges+writes.Deleted != 0 {
		t.Fatal("warm replay", warm, writes)
	}
	hashes := map[string]string{}
	for _, n := range snapshot.Nodes {
		if n.Kind == "route" {
			hashes[n.Identity] = n.ContentHash
		}
	}
	edit := func(file, old, new string) {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(file))
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), old) {
			t.Fatal("edit did not match", file, old)
		}
		if err := os.WriteFile(p, []byte(strings.ReplaceAll(string(b), old, new)), 0644); err != nil {
			t.Fatal(err)
		}
	}
	fullEqual := func() {
		t.Helper()
		fresh := open(t)
		actual, _, _ := emit(t, root, s, opt)
		expected, _, _ := emit(t, root, fresh, opt)
		if string(actual) != string(expected) {
			t.Fatal("cached snapshot differs from full snapshot")
		}
	}
	edit("api/src/lib.rs", `"/kit"`, `"/changed"`)
	_, changed, _ := emit(t, root, s, opt)
	if changed.Parsed != 1 || changed.Reused != first.Parsed-1 {
		t.Fatal("constant edit", changed)
	}
	after, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	changedPaths := 0
	for _, n := range after.Nodes {
		if n.Kind != "route" {
			continue
		}
		if n.ContentHash != hashes[n.Identity] {
			t.Fatal("prefix changed route hash", n.Identity)
		}
		for _, p := range routePaths(t, n) {
			text := p["template"].(string)
			if strings.HasPrefix(text, "/kit") {
				t.Fatal("stale prefix", n)
			}
			if strings.HasPrefix(text, "/changed") {
				changedPaths++
			}
		}
	}
	if changedPaths != 10 {
		t.Fatal("composed paths", changedPaths)
	}
	fullEqual()
	edit("api/Cargo.toml", `name = "route-kit"`, `name = "renamed-kit"`)
	_, renamed, _ := emit(t, root, s, opt)
	if renamed.Parsed != 0 || renamed.Reused != first.Parsed {
		t.Fatal("Cargo edit reparsed Rust", renamed)
	}
	renamedSnapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range renamedSnapshot.Edges {
		if e.Kind == "imports" && e.Source == "file:app/src/lib.rs#app%2Fsrc%2Flib.rs" && e.Target == "file:api/src/lib.rs#api%2Fsrc%2Flib.rs" {
			t.Fatal("stale library link", e)
		}
	}
	fullEqual()
	edit("api/Cargo.toml", `name = "renamed-kit"`, `name = "route-kit"`)
	edit("api/src/routes.rs", "get(first).post(handlers::b)", "get(handlers::b).post(handlers::b)")
	emit(t, root, s, opt)
	final, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	different := []string{}
	for _, n := range final.Nodes {
		if n.Kind == "route" && n.ContentHash != hashes[n.Identity] {
			different = append(different, n.Identity)
		}
	}
	if !reflect.DeepEqual(different, []string{factsRouteID("api/src/routes.rs", "GET /")}) {
		t.Fatal("sibling verb hash changed", different)
	}
}

func factsRouteID(file, name string) string {
	id, _ := contract.Identity("route", file, name)
	return id
}

func TestAxumRecognitionMethodsAndConditionals(t *testing.T) {
	root := t.TempDir()
	write := func(file, source string) {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(file))
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(source), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("Cargo.toml", "[package]\nname='app'\n")
	write("src/lib.rs", `use axum::{Router, routing::*, routing::MethodFilter as Filter};
fn h() {}
fn routes() -> Router {
 let mut r = Router::default().route_service("/svc", service)
 .route("/verbs", get(h).post(h).put(h).patch(h).delete(h).head(h).options(h).trace(h).connect(h))
 .route("/any-service", any_service(service))
 .route("/on-service", on_service(Filter::GET | Filter::POST, service))
 .route("/bad-filter", on(filter, h));
 if let Some(x) = value { r = r.route("/if", get(h)); } else { r = r.route("/else", get(h)); }
 match value { _ => { r = r.route("/match", get(h)); } }
 loop { r = r.route("/loop", get(h)); break; }
 while enabled { r = r.route("/while", get(h)); }
 for item in items { r = r.route("/for", get(h)); }
 #[cfg(feature="x")] let r = r.route("/cfg", get(h));
 return r;
}


struct Fake;
impl Fake { fn new() -> Self { Self } fn route(self, _: &str, _: ()) -> Self { self } }
fn false_route() { use Fake as Router; Router::new().route("/false", ()); }
`)
	s, opt := open(t), options(t)
	emit(t, root, s, opt)
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	methodSet := map[string]bool{}
	for _, n := range snapshot.Nodes {
		if n.Kind != "route" {
			continue
		}
		count++
		pattern := n.Attributes["pattern"].(string)
		if pattern == "/false" || pattern == "/bad-filter" {
			t.Fatal("unproven registration", n)
		}
		if pattern == "/verbs" {
			methodSet[n.Attributes["method"].(string)] = true
		}
		for _, p := range routePaths(t, n) {
			conditional := strings.Contains("/if /else /match /loop /while /for /cfg", pattern)
			if p["conditional"] != conditional {
				t.Fatal("conditional context", n.Identity, p)
			}
		}
		if strings.Contains(pattern, "service") || pattern == "/svc" {
			if n.Attributes["handler"] != nil || n.Attributes["handler_expression"] != "service" {
				t.Fatal("service handler", n)
			}
		}
	}
	if count != 20 || len(methodSet) != 9 {
		t.Fatal("explicit method/conditional counts", count, methodSet)
	}
	// A scanned library named axum takes precedence on a fresh relink even
	// though every importing Rust source is replayed unchanged.
	write("shadow/Cargo.toml", "[package]\nname='axum'\n")
	write("shadow/src/lib.rs", `pub struct Router; pub mod routing { pub fn get() {} }`)
	_, changed, _ := emit(t, root, s, opt)
	if changed.Parsed != 1 {
		t.Fatal(changed)
	}
	after, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range after.Nodes {
		if n.Kind == "route" {
			t.Fatal("scanned axum shadow", n)
		}
	}
}

func TestAxumFactoryBranchesAndCompositionCycle(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Cargo.toml"), []byte("[package]\nname='app'\n[lib]\npath='lib.rs'\n"), 0644); err != nil {
		t.Fatal(err)
	}
	source := `use axum::{Router, routing::get};
fn h() {}
fn a() -> Router { Router::new().route("/a", get(h)).merge(b()) }
fn b() -> Router { Router::new().route("/b", get(h)).merge(a()) }
fn branches() -> Router { if flag { return Router::new().route("/one", get(h)); } else { Router::new().route("/two", get(h)) } }
fn root() -> Router { Router::new().nest("/api", a()).nest("/choice", branches()) }
`
	if err := os.WriteFile(filepath.Join(root, "lib.rs"), []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	s, opt := open(t), options(t)
	emit(t, root, s, opt)
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, n := range snapshot.Nodes {
		if n.Kind == "route" {
			for _, p := range routePaths(t, n) {
				template := p["template"].(string)
				seen[template] = true
				if strings.HasPrefix(template, "/choice") && p["conditional"] != true {
					t.Fatal("return branch", p)
				}
			}
		}
	}
	for _, path := range []string{"/api/a", "/api/b", "/choice/one", "/choice/two"} {
		if !seen[path] {
			t.Error("missing factory/cycle path", path, seen)
		}
	}
	for _, d := range snapshot.Diagnostics {
		if strings.HasPrefix(d.Code, "route_") || d.Code == "parse_failed" || d.Code == "extract_failed" {
			t.Error(d)
		}
	}
}
