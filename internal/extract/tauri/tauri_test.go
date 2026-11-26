package tauri

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/lang/rust"
	"github.com/graphvein/graphvein/internal/lang/typescript"
	"github.com/graphvein/graphvein/internal/parse"
)

// Focused tests for the Revision 2 rules (review 1). The index fixture
// tests cover the full pipeline; these build the link inputs from sources
// directly, so each rule is checked on the smallest repository.

const cargo = "[package]\nname = \"app\"\nversion = \"0.1.0\"\n\n[dependencies]\ntauri = \"2\"\ntokio = \"1\"\n"

const web = "import { invoke } from '@tauri-apps/api/core'\nexport const a = () => invoke('greet')\n"

type linked struct {
	nodes       map[string]contract.Node
	edges       []string
	diagnostics []string
}

func link(t *testing.T, files map[string]string) linked {
	t.Helper()
	if _, ok := files["package.json"]; !ok {
		// The front end declares the Tauri API, as every Tauri web package
		// does: an import is an npm package only when declared (row TS-I5).
		with := map[string]string{"package.json": `{"private": true, "dependencies": {"@tauri-apps/api": "2.0.0"}}`}
		for p, source := range files {
			with[p] = source
		}
		files = with
	}
	var paths []string
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var jobs []parse.Job
	var parsed []string
	for _, p := range paths {
		switch filepath.Ext(p) {
		case ".rs":
			jobs = append(jobs, parse.Job{Language: "rust", Source: []byte(files[p])})
		case ".ts":
			jobs = append(jobs, parse.Job{Language: "typescript", Source: []byte(files[p])})
		case ".tsx":
			jobs = append(jobs, parse.Job{Language: "tsx", Source: []byte(files[p])})
		default:
			continue
		}
		parsed = append(parsed, p)
	}
	results, err := parse.Batch(context.Background(), jobs, parse.Options{CacheDir: filepath.Join(t.TempDir(), "cache")})
	if err != nil {
		t.Fatal(err)
	}
	sources := map[string][]byte{}
	for _, p := range paths {
		sources[p] = []byte(files[p])
	}
	rs, ts := map[string]facts.Facts{}, map[string]facts.Facts{}
	for _, p := range paths {
		ts[p] = facts.Facts{Exports: map[string]string{}}
	}
	for i, p := range parsed {
		if results[i].Err != nil || results[i].SyntaxError {
			t.Fatal(p, results[i].Err)
		}
		if strings.HasSuffix(p, ".rs") {
			rs[p] = rust.Extract(p, sources[p], results[i].Root)
		} else {
			ts[p] = typescript.Extract(p, sources[p], results[i].Root)
		}
	}
	nodes, edges, diagnostics := Link(Input{Rust: rs, Resolver: rust.NewModuleResolver(rs, sources), TypeScript: ts, Modules: typescript.NewModuleResolver(ts, sources), Sources: sources})
	out := linked{nodes: map[string]contract.Node{}}
	for _, n := range nodes {
		out.nodes[n.Identity] = n
	}
	for _, e := range edges {
		if e.Kind == "invokes_command" {
			out.edges = append(out.edges, e.Source+" -> "+e.Target)
		}
	}
	for _, d := range diagnostics {
		out.diagnostics = append(out.diagnostics, d.Code+" "+d.Message)
	}
	sort.Strings(out.edges)
	sort.Strings(out.diagnostics)
	return out
}

func app(main string) map[string]string {
	return map[string]string{"app/Cargo.toml": cargo, "app/src/main.rs": main, "web/main.ts": web}
}

func status(t *testing.T, l linked, call string) map[string]any {
	t.Helper()
	n, ok := l.nodes["ipc_call:web/main.ts#"+call]
	if !ok {
		t.Fatalf("missing ipc_call %s in %v", call, l.nodes)
	}
	return n.Attributes
}

const proven = `#[tauri::command]
fn greet() {}
fn main() {
    tauri::Builder::default()
        .invoke_handler(tauri::generate_handler![greet])
        .run(tauri::generate_context!())
        .expect("run");
}
`

// M1: an earlier cfg-gated arm of the same name does not hide a later arm;
// the call links to every arm that is the target in some build.
func TestCfgGatedDuplicateArms(t *testing.T) {
	l := link(t, app(`#[tauri::command(rename = "greet")]
fn greet_v1() {}
#[tauri::command]
fn greet() {}
#[tauri::command]
fn greet_v3() {}
fn main() {
    tauri::Builder::default()
        .invoke_handler(tauri::generate_handler![
            #[cfg(feature = "v1")]
            greet_v1,
            greet,
            greet_v3,
        ])
        .run(tauri::generate_context!())
        .expect("run");
}
`))
	a := status(t, l, "a.INVOKE%20%27greet%27")
	if a["status"] != "linked" || a["conditional"] != true || a["command"] != "greet" {
		t.Errorf("call: %v", a)
	}
	want := []string{
		"ipc_call:web/main.ts#a.INVOKE%20%27greet%27 -> ipc_command:app/src/main.rs#main.COMMAND%20greet",
		"ipc_call:web/main.ts#a.INVOKE%20%27greet%27 -> ipc_command:app/src/main.rs#main.COMMAND%20greet_v1",
	}
	if !reflect.DeepEqual(l.edges, want) {
		t.Errorf("edges %v", l.edges)
	}
	for id, unreachable := range map[string]bool{"greet_v1": false, "greet": false} {
		if _, has := l.nodes["ipc_command:app/src/main.rs#main.COMMAND%20"+id].Attributes["unreachable"]; has != unreachable {
			t.Errorf("unreachable %s", id)
		}
	}
	// greet_v3 has no rename, so it is not a duplicate; a later ungated arm
	// after the ungated greet would be.
	l = link(t, app(`#[tauri::command]
fn greet() {}
#[tauri::command(rename = "greet")]
fn again() {}
fn main() {
    tauri::Builder::default()
        .invoke_handler(tauri::generate_handler![greet, #[cfg(x)] again])
        .run(tauri::generate_context!())
        .expect("run");
}
`))
	if l.nodes["ipc_command:app/src/main.rs#main.COMMAND%20again"].Attributes["unreachable"] != true || len(l.edges) != 1 {
		t.Errorf("arm after an ungated arm: %v %v", l.nodes, l.edges)
	}
}

// Revision 3, mi1 (review 2 probe e1): arms gated by P and not(P) together
// cover every build, so the arm list stops at the second of them and later
// arms of that name are unreachable. Only exact pairs are recognized.
func TestComplementaryCfgArms(t *testing.T) {
	const pair = `#[tauri::command(rename = "greet")]
fn g1() {}
#[tauri::command(rename = "greet")]
fn g2() {}
#[tauri::command]
fn greet() {}
fn main() {
    tauri::Builder::default().invoke_handler(tauri::generate_handler![
        %s g1,
        %s g2,
        greet
    ]).run(tauri::generate_context!()).expect("run");
}
`
	const prefix = "ipc_call:web/main.ts#a.INVOKE%20%27greet%27 -> ipc_command:app/src/main.rs#main.COMMAND%20"
	for _, c := range []struct {
		first, second string
		edges         []string
		unreachable   bool
	}{
		// e1 as probed, the reverse order and spacing differences.
		{`#[cfg(feature = "a")]`, `#[cfg(not(feature = "a"))]`, []string{prefix + "g1", prefix + "g2"}, true},
		{`#[cfg(not(feature="a"))]`, `#[cfg( feature = "a" )]`, []string{prefix + "g1", prefix + "g2"}, true},
		{`#[cfg(test)]`, `#[cfg(not(test))]`, []string{prefix + "g2"}, true},
		// Not an exact pair: every arm stays a target.
		{`#[cfg(feature = "a")]`, `#[cfg(not(feature = "b"))]`, []string{prefix + "g1", prefix + "g2", prefix + "greet"}, false},
		{`#[cfg(feature = "a b")]`, `#[cfg(not(feature = "ab"))]`, []string{prefix + "g1", prefix + "g2", prefix + "greet"}, false},
		{`#[cfg(unix)] #[cfg(feature = "a")]`, `#[cfg(not(feature = "a"))]`, []string{prefix + "g1", prefix + "g2", prefix + "greet"}, false},
	} {
		l := link(t, app(fmt.Sprintf(pair, c.first, c.second)))
		if !reflect.DeepEqual(l.edges, c.edges) {
			t.Errorf("%s / %s: edges %v", c.first, c.second, l.edges)
		}
		greet := l.nodes["ipc_command:app/src/main.rs#main.COMMAND%20greet"].Attributes
		if unreachable, _ := greet["unreachable"].(bool); unreachable != c.unreachable {
			t.Errorf("%s / %s: greet %v", c.first, c.second, greet)
		}
		duplicate := false
		for _, d := range l.diagnostics {
			duplicate = duplicate || strings.HasPrefix(d, "tauri_command_duplicate app/src/main.rs:")
		}
		if duplicate != c.unreachable {
			t.Errorf("%s / %s: diagnostics %v", c.first, c.second, l.diagnostics)
		}
	}
}

// M2: another Tauri app in the repository blocks the single-app proof.
func TestOtherAppEvidence(t *testing.T) {
	base := link(t, app(proven))
	if len(base.edges) != 1 {
		t.Fatalf("proven app: %v %v", base.edges, base.diagnostics)
	}
	for name, extra := range map[string]map[string]string{
		"aliased root":    {"other/src/main.rs": "use tauri::Builder as B;\nfn main() { B::new().run(tauri::generate_context!()).unwrap(); }\n"},
		"second context":  {"other/src/lib.rs": "pub fn context() { let _ = tauri::generate_context!(); }\n"},
		"closure handler": {"other/src/main.rs": "fn main() { make().invoke_handler(|i| true).run(c()); }\n"},
		"two configs":     {"app/tauri.conf.json": "{}", "other/Tauri.toml": ""},
	} {
		files := app(proven)
		files["other/Cargo.toml"] = strings.Replace(cargo, `"app"`, `"other"`, 1)
		for p, s := range extra {
			files[p] = s
		}
		l := link(t, files)
		if got := status(t, l, "a.INVOKE%20%27greet%27")["status"]; len(l.edges) != 0 || got != "ambiguous" && got != "registry_unproven" {
			t.Errorf("%s: %v %v", name, got, l.diagnostics)
		}
	}
	// Overlays, test-scope builders, cfg(test) modules and mock_builder
	// neither prove nor block.
	for name, extra := range map[string]map[string]string{
		"platform overlay": {"app/tauri.conf.json": "{}", "app/tauri.linux.conf.json": "{}", "app/Tauri.ios.toml": ""},
		"test file":        {"app/tests/ipc.rs": "fn t() { tauri::Builder::default().run(tauri::generate_context!()).unwrap(); }\n"},
		"cfg(test) module": {"app/src/lib.rs": "#[cfg(test)]\nmod tests {\n    fn t() { tauri::Builder::default().invoke_handler(|i| true).run(tauri::generate_context!()).unwrap(); }\n}\n"},
		"cfg(all(test))":   {"app/src/lib.rs": "#[cfg(all(unix, test))]\nfn t() { tauri::Builder::default().invoke_handler(tauri::generate_handler![crate::greet]).build(tauri::generate_context!()).unwrap(); }\n"},
		"mock_builder":     {"app/src/mock.rs": "fn t() { tauri::test::mock_builder().invoke_handler(tauri::generate_handler![crate::greet]).build(tauri::generate_context!()).unwrap(); }\n"},
	} {
		files := app(proven)
		for p, s := range extra {
			files[p] = s
		}
		if name == "mock_builder" {
			// Not test scope and not cfg(test): mock_builder alone keeps it
			// from blocking; its generate_context! is still a second context.
			files["app/src/mock.rs"] = "fn t() { tauri::test::mock_builder().invoke_handler(tauri::generate_handler![crate::greet]).build(ctx()).unwrap(); }\n"
		}
		if l := link(t, files); !reflect.DeepEqual(l.edges, base.edges) {
			t.Errorf("%s blocked the app: %v", name, l.diagnostics)
		}
	}
}

// Revision 3, MA2 (review 2 probes b0-bl): a second app in the same crate
// (src/bin/tray.rs) sharing the library's generate_context! is seen through
// any reference to Tauri's Builder. Each form is checked alone, without the
// probes' common `pub use` (itself evidence, bg).
func TestSecondAppForms(t *testing.T) {
	const lib = "pub fn context() -> tauri::Context<tauri::Wry> { tauri::generate_context!() }\n"
	const main = `#[tauri::command]
fn greet() {}
fn main() {
    tauri::Builder::default()
        .invoke_handler(tauri::generate_handler![greet])
        .run(probe_app::context())
        .expect("run");
}
`
	files := func(extra map[string]string) map[string]string {
		out := map[string]string{
			"app/Cargo.toml":      strings.Replace(cargo, `"app"`, `"probe_app"`, 1),
			"app/tauri.conf.json": `{"identifier":"x"}`,
			"app/src/lib.rs":      lib,
			"app/src/main.rs":     main,
			"web/main.ts":         web,
		}
		for p, s := range extra {
			out[p] = s
		}
		return out
	}
	base := link(t, files(nil))
	if len(base.edges) != 1 {
		t.Fatalf("proven app: %v %v", base.edges, base.diagnostics)
	}
	const run = ".run(probe_app::context()).expect(\"run\"); }\n"
	for name, tray := range map[string]map[string]string{
		"b0 plain":            {"app/src/bin/tray.rs": "fn main() { tauri::Builder::default()" + run},
		"ba type alias":       {"app/src/bin/tray.rs": "type B = tauri::Builder<tauri::Wry>;\nfn main() { B::default()" + run},
		"bb Default::default": {"app/src/bin/tray.rs": "fn main() { let b: tauri::Builder<tauri::Wry> = Default::default(); b" + run},
		"bc use rename":       {"app/src/bin/tray.rs": "use tauri::Builder as B;\nfn main() { B::default()" + run},
		"bd turbofish":        {"app/src/bin/tray.rs": "fn main() { tauri::Builder::<tauri::Wry>::new()" + run},
		"be qualified path":   {"app/src/bin/tray.rs": "fn main() { <tauri::Builder<tauri::Wry>>::default()" + run},
		"bg re-export":        {"app/src/lib.rs": "pub use tauri::Builder as AppBuilder;\n" + lib, "app/src/bin/tray.rs": "fn main() { probe_app::AppBuilder::default()" + run},
		"bg re-export alone":  {"app/src/lib.rs": "pub use tauri::Builder as AppBuilder;\n" + lib},
		"glob re-export":      {"app/src/lib.rs": "pub use tauri::*;\n" + lib},
		"bi function value":   {"app/src/bin/tray.rs": "fn main() { let mk = tauri::Builder::<tauri::Wry>::default; mk()" + run},
		"bj macro_rules":      {"app/src/bin/tray.rs": "macro_rules! app { () => { tauri::Builder::default() } }\nfn main() { app!()" + run},
		"macro invocation":    {"app/src/bin/tray.rs": "fn main() { let b = vec![tauri::Builder::default()]; b" + run},
		"bk glob":             {"app/src/bin/tray.rs": "use tauri::*;\nfn main() { Builder::default()" + run},
		"bl rename turbofish": {"app/src/bin/tray.rs": "use tauri::{Builder as B, Wry};\nfn main() { B::<Wry>::default()" + run},
		"app::Builder":        {"app/src/bin/tray.rs": "fn main() { tauri::app::Builder::default()" + run},
		"returned default":    {"app/src/bin/tray.rs": "fn make() -> tauri::Builder<tauri::Wry> { Default::default() }\nfn main() { make()" + run},
		"struct field":        {"app/src/bin/tray.rs": "#[derive(Default)]\nstruct S { b: tauri::Builder<tauri::Wry> }\nfn main() { S::default().b" + run},
		"use chain":           {"app/src/bin/tray.rs": "use tauri::Builder as A;\nuse A as B;\nfn main() { B::new()" + run},
		"module use chain":    {"app/src/bin/tray.rs": "mod inner;\nuse tauri::Builder as B;\nfn main() { inner::make(); }\n", "app/src/bin/inner.rs": "use super::B as C;\npub fn make() { C::new()" + run},
	} {
		l := link(t, files(tray))
		if got := status(t, l, "a.INVOKE%20%27greet%27")["status"]; len(l.edges) != 0 || got != "ambiguous" {
			t.Errorf("%s: %v %v", name, got, l.diagnostics)
		}
	}
	// Not app evidence: other crates' Builder types, signatures, private
	// uses, Builder-named locals and strings.
	for name, extra := range map[string]map[string]string{
		"other Builder types": {"app/src/util.rs": "fn f() { tempfile::Builder::new(); tokio::runtime::Builder::new_multi_thread(); std::thread::Builder::new(); }\n"},
		"signatures":          {"app/src/util.rs": "use tauri::Builder;\nfn f(b: Builder<tauri::Wry>) -> Option<tauri::Builder<tauri::Wry>> { let x: Option<Builder<tauri::Wry>> = None; x }\n"},
		"private use":         {"app/src/util.rs": "use tauri::Builder as B;\n"},
		"plugin builder":      {"app/src/util.rs": "fn f() { tauri::plugin::Builder::new(\"x\"); tauri_plugin_updater::Builder::new(); }\n"},
		"string":              {"app/src/util.rs": "fn f() { log::info!(\"tauri::Builder::default()\"); }\n"},
		"local glob":          {"app/src/lib.rs": "pub use util::*;\nmod util;\n" + lib, "app/src/util.rs": "pub fn helper() {}\n"},
	} {
		if l := link(t, files(extra)); !reflect.DeepEqual(l.edges, base.edges) {
			t.Errorf("%s blocked the app: %v", name, l.diagnostics)
		}
	}
}

// m1: a callable that writes its parameter is not a wrapper.
func TestWrapperWrites(t *testing.T) {
	for source, wrapper := range map[string]bool{
		"export function w(cmd: string) { return invoke(cmd) }":                                     true,
		"export function w(cmd: string) { log(cmd, `x${cmd}`); return invoke(cmd) }":                true,
		"export function w(cmd: string) { const o = { cmd }; x[cmd] = 1; return invoke(cmd) }":      true,
		"export function w(cmd: string) { [cmd] = ['other']; return invoke(cmd) }":                  false,
		"export function w(cmd: string) { ({ cmd } = { cmd: 'o' }); return invoke(cmd) }":           false,
		"export function w(cmd: string) { ({ a: cmd } = { a: 'o' }); return invoke(cmd) }":          false,
		"export function w(cmd: any) { cmd++; return invoke(cmd) }":                                 false,
		"export function w(cmd: any) { --cmd; return invoke(cmd) }":                                 false,
		"export function w(cmd: string) { cmd ??= 'o'; return invoke(cmd) }":                        false,
		"export function w(cmd: string) { const f = () => { cmd = 'o' }; f(); return invoke(cmd) }": false,
		"export function w(cmd: string) { for (cmd of list) {} return invoke(cmd) }":                false,
		"export function w(cmd: string) { arguments[0] = 'o'; return invoke(cmd) }":                 false,
	} {
		files := app(proven)
		files["web/main.ts"] = "import { invoke } from '@tauri-apps/api/core'\ndeclare const list: string[]\ndeclare const x: any\ndeclare function log(...a: any[]): void\n" + source + "\nexport const a = () => w('greet')\n"
		l := link(t, files)
		_, recognized := l.nodes["ipc_call:web/main.ts#a.INVOKE%20%27greet%27"]
		if recognized != wrapper {
			t.Errorf("%s: wrapper %v, want %v", source, recognized, wrapper)
		}
	}
	if writesParameter([]byte("function w(cmd) { const s = '[cmd] = 1'; /* cmd++ */ return f(cmd) }"), "cmd", false) {
		t.Error("strings and comments are not writes")
	}
	if !writesParameter([]byte("function w(cmd) { const s = `${cmd = 'o'}`; return f(cmd) }"), "cmd", false) {
		t.Error("a template substitution is code")
	}
}

// Revision 3, MA1 (review 2 probes q1 and q2): the write check fails
// closed. Every construct the lexer may read differently from JavaScript
// rejects the wrapper, so none of these links its caller (Tauri would
// receive 'other' where a write is hidden). The controls keep linking.
func TestWrapperLexerFailsClosed(t *testing.T) {
	prelude := "import { invoke } from '@tauri-apps/api/core'\ndeclare const ok: boolean\ndeclare const s: string\ndeclare let n: number | undefined\n"
	for _, c := range []struct {
		file, source string
		wrapper      bool
	}{
		// q1 v1, v2: a regular expression after `)` was read as division,
		// its quote then opened a string that hid the write.
		{"web/main.ts", "export function w(cmd: string) { if (ok) /'/.test(s); [cmd] = ['other']; return invoke(cmd) }", false},
		{"web/main.ts", "export function w(cmd: string) { if (ok) /\"/.test(s); ({ cmd } = { cmd: 'other' }); return invoke(cmd) }", false},
		// q1 v3: a plain destructuring write (found before as well).
		{"web/main.ts", "export function w(cmd: string) { [cmd] = ['other']; return invoke(cmd) }", false},
		// q1 v4: a division after `++` was read as a regular expression.
		{"web/main.ts", "export function w(cmd: string) { let n = 1; n++ / 2, [cmd] = ['other'], n / 1; return invoke(cmd) }", false},
		// q1 v5: a backtick in a misread regular expression opened a template.
		{"web/main.ts", "export function w(cmd: string) { while (ok) /`/.test(s), [cmd] = ['other']; return invoke(cmd) }", false},
		// q1 v6: an identifier escape (c\u{6D}d is cmd).
		{"web/main.ts", "export function w(cmd: string) { c\\u{6D}d++; return invoke(cmd) }", false},
		// q1 V7: an apostrophe in JSX text.
		{"web/view.tsx", "export function w(cmd: string) { const el = <p>Don't {([cmd] = ['other'])}</p>; return invoke(cmd) }", false},
		// q2 u1: a regular expression after `}`.
		{"web/main.ts", "export function w(cmd: string) { if (ok) {} /'/.test(s) && ([cmd] = ['other']); return invoke(cmd) }", false},
		// q2 u3: the same across a line break.
		{"web/main.ts", "export function w(cmd: string) { if (ok) /'/.test(s)\n  [cmd] = ['other']; return invoke(cmd) }", false},
		// Review 3 MA3 (probes q3, q4): a TypeScript non-null assertion
		// hid the write from both layers (a6, a7, d1, d2, d4) or made the
		// next `/` read as a regular expression that swallowed it (a5).
		{"web/main.ts", "export function w(cmd: string) { const x = (n! / 2, [cmd] = ['other'], n! / 1); return invoke(cmd) }", false},
		{"web/main.ts", "export function w(cmd: string) { cmd! = 'other'; return invoke(cmd) }", false},
		{"web/main.ts", "export function w(cmd: string) { cmd! += 'x'; return invoke(cmd) }", false},
		{"web/main.ts", "export function w(cmd: string) { (cmd)! = 'other'; return invoke(cmd) }", false},
		{"web/main.ts", "export function w(cmd?: string) { cmd!++; return invoke(cmd!) }", false},
		{"web/main.ts", "export function w(cmd?: string) { if (!cmd) cmd! ??= 'other'; return invoke(cmd!) }", false},
		{"web/main.ts", "export function w(cmd: string) { const v = [s][0]!; return invoke(cmd) }", false},
		// Controls: `!==`, `!=` and the prefix `!` keep the wrapper.
		{"web/main.ts", "export function w(cmd: string) { if (cmd !== s && cmd!=s && !ok && (!cmd)) return; return invoke(cmd) }", true},
		{"web/main.ts", "export function w(cmd: string) { if (ok) return !ok; return invoke(cmd) }", true},
		// Other unsure input: an unterminated string, template or comment,
		// a non-ASCII identifier, a keyword before `/`, unbalanced brackets.
		{"web/main.ts", "export function w(cmd: string) { const t = 'a\n'; return invoke(cmd) }", false},
		{"web/main.ts", "export function w(cmd: string) { const v = typeof /x/; return invoke(cmd) }", false},
		{"web/main.ts", "export function w(cmd: string) { const \u00e9 = 1; return invoke(cmd) }", false},
		{"web/main.ts", "export function w(cmd: string) { const r = (1) / 2; return invoke(cmd) }", false},
		// Controls: q2 u2 (an apostrophe inside a template), a regular
		// expression after `(` and a division after an identifier.
		{"web/main.ts", "export function w(cmd: string) { const o = { k: `'` }; return invoke(cmd) }", true},
		{"web/main.ts", "export function w(cmd: string) { const m = s.match(/'/); const h = s.length / 2; return invoke(cmd) }", true},
		{"web/main.ts", "export function w(cmd: string) { const t: Array<string> = []; return invoke<string>(cmd) }", true},
		// In a JSX file any `<` before an identifier rejects the wrapper.
		{"web/view.tsx", "export function w(cmd: string) { return invoke<string>(cmd) }", false},
		{"web/view.tsx", "export function w(cmd: string) { return invoke(cmd) }", true},
	} {
		files := app(proven)
		delete(files, "web/main.ts")
		files[c.file] = prelude + c.source + "\nexport const a = () => w('greet')\n"
		l := link(t, files)
		var edges []string
		recognized := false
		for id := range l.nodes {
			if strings.HasPrefix(id, "ipc_call:"+c.file+"#a.") {
				recognized = true
			}
		}
		for _, e := range l.edges {
			if strings.HasPrefix(e, "ipc_call:"+c.file+"#a.") {
				edges = append(edges, e)
			}
		}
		if recognized != c.wrapper || c.wrapper != (len(edges) == 1) {
			t.Errorf("%s %s: wrapper %v (edges %v), want %v", c.file, c.source, recognized, edges, c.wrapper)
		}
	}
	for source, unsure := range map[string]bool{
		"f(cmd) /* open":         true,
		"`a${b}":                 true,
		"x = /abc":               true,
		"a ) b":                  true,
		"g(cmd, 'x', `y${z}`)":   false,
		"a.b / c, d = /e/g":      false,
		"n = 1 // c\n/x/.t(s)":   false,
		"a! = b":                 true,
		"f(a)!.b":                true,
		"a[0]!":                  true,
		"a !== b, !c, return !d": false,
	} {
		_, got := lexJS([]byte(source), false)
		if strings.HasPrefix(source, "a )") {
			got = writesParameter([]byte(source), "cmd", false)
		}
		if got != unsure {
			t.Errorf("%q: unsure %v, want %v", source, got, unsure)
		}
	}
}

// m2: a cfg(test) command is test_only for production calls; test calls
// still link to it.
func TestTestOnlyCommands(t *testing.T) {
	main := `#[tauri::command]
fn greet() {}
#[cfg(test)]
mod testing {
    #[tauri::command]
    pub fn in_module() {}
}
#[cfg(all(test, unix))]
#[tauri::command]
fn own_cfg() {}
#[cfg_attr(test, tauri::command)]
fn cfg_attr_cmd() {}
#[cfg(any(test, unix))]
#[tauri::command]
fn any_cfg() {}
#[tauri::command]
fn entry_cfg() {}
fn main() {
    tauri::Builder::default()
        .invoke_handler(tauri::generate_handler![greet, testing::in_module, own_cfg, cfg_attr_cmd, any_cfg, #[cfg(test)] entry_cfg])
        .run(tauri::generate_context!())
        .expect("run");
}
`
	calls := "import { invoke } from '@tauri-apps/api/core'\nexport const a = () => [invoke('in_module'), invoke('own_cfg'), invoke('cfg_attr_cmd'), invoke('any_cfg'), invoke('entry_cfg')]\n"
	files := app(main)
	files["web/main.ts"] = calls
	files["web/main.test.ts"] = calls
	l := link(t, files)
	for name, want := range map[string]string{"in_module": "test_only", "own_cfg": "test_only", "cfg_attr_cmd": "test_only", "any_cfg": "linked", "entry_cfg": "test_only"} {
		n := l.nodes["ipc_call:web/main.ts#a.INVOKE%20%27"+name+"%27"]
		if n.Attributes["status"] != want {
			t.Errorf("production %s: %v", name, n.Attributes)
		}
		if got := l.nodes["ipc_call:web/main.test.ts#a.INVOKE%20%27"+name+"%27"].Attributes["status"]; got != "linked" {
			t.Errorf("test call %s: %v", name, got)
		}
	}
}

// m3: only verified attributes may sit on a handler's function.
func TestFunctionAttributes(t *testing.T) {
	body := `#[tauri::command]
fn greet() {}
%s
fn main() {
    let app = tauri::Builder::default();
    app.invoke_handler(tauri::generate_handler![greet])
        .run(tauri::generate_context!())
        .expect("run");
}
`
	for attrs, ok := range map[string]bool{
		"":               true,
		"#[tokio::main]": true,
		"#[cfg_attr(mobile, tauri::mobile_entry_point)]\n#[allow(dead_code)]\n#[inline]\n#[doc = \"x\"]": true,
		"#[deny(warnings)]\n#[cfg(not(test))]": true,
		"#[my_macros::rewrite]":                false,
		"#[cfg_attr(x, my_macros::rewrite)]":   false,
		"#[rewrite]":                           false,
		"#[test]":                              false,
	} {
		l := link(t, app(strings.Replace(body, "%s", attrs, 1)))
		if got := len(l.edges) == 1; got != ok {
			t.Errorf("%q: linked %v, want %v (%v)", attrs, got, ok, l.diagnostics)
		}
	}
}

func TestCfgPredicates(t *testing.T) {
	for p, want := range map[string]bool{
		"test": true, "all(test, unix)": true, "all(unix, all(test))": true, "#[cfg(all(feature = \"a,b\", test))]": true,
		"any(test, unix)": false, "not(test)": false, "all(not(test))": false, "feature = \"test\"": false, "#[cfg_attr(test, x)]": false,
	} {
		if got := facts.CfgTestOnly(p); got != want {
			t.Errorf("CfgTestOnly(%q) = %v", p, got)
		}
	}
	for a, want := range map[string]bool{"#[cfg(x)]": true, "#[cfg_attr(x, cfg(y))]": true, "#[cfg_attr(x, allow(y))]": false, "#[cfg_attr(x, cfg_attr(y, cfg(z)))]": true, "#[allow(x)]": false} {
		if got := facts.CfgApplies(a); got != want {
			t.Errorf("CfgApplies(%q) = %v", a, got)
		}
	}
}
