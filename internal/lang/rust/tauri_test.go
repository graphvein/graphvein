package rust

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/parse"
)

// extractEach parses all sources in one batch, so the grammar compiles once.
func extractEach(t *testing.T, sources []string) []facts.Facts {
	t.Helper()
	jobs := make([]parse.Job, len(sources))
	for i, s := range sources {
		jobs[i] = parse.Job{Language: "rust", Source: []byte(s)}
	}
	results, err := parse.Batch(context.Background(), jobs, parse.Options{CacheDir: filepath.Join(t.TempDir(), "cache")})
	if err != nil {
		t.Fatal(err)
	}
	out := make([]facts.Facts, len(sources))
	for i, r := range results {
		if r.Err != nil || r.SyntaxError {
			t.Fatal(sources[i], r.Err)
		}
		out[i] = Extract("src/lib.rs", []byte(sources[i]), r.Root)
	}
	return out
}

func TestTauriAbsentWithoutSyntax(t *testing.T) {
	f := extract(t, "src/lib.rs", "#[derive(Debug)]\n#[command(name = \"x\")]\nstruct Cli;\nfn command() { let b = tauri::Thing::default(); b.run(); }\n")
	if f.Tauri != nil {
		t.Fatal("unexpected Tauri payload", f.Tauri)
	}
	// Version 3: a Builder constructor alone records only an app site.
	f = extract(t, "src/lib.rs", "fn command() { let b = tauri::Builder::default(); b.run(); }\n")
	if f.Tauri == nil || len(f.Tauri.Commands)+len(f.Tauri.Handlers)+len(f.Tauri.Flows) != 0 || len(f.Tauri.Apps) != 1 || f.Tauri.Apps[0].Kind != "root" {
		t.Fatal("builder app site", f.Tauri)
	}
}

func TestTauriHandlerGrammar(t *testing.T) {
	cases := map[string]bool{
		"a, b,":                       false,
		"#![plugin(p)] #![other] a":   false,
		"":                            false,
		", a":                         true,
		"a,, b":                       true,
		"a b":                         true,
		"a::<T>":                      true,
		"#![plugin(\"p\")] a":         true,
		"#![plugin(p, q)] a":          true,
		"#[cfg(x)] #[allow(y)] a::b":  false,
		"#![plugin(p)] #![plugin(q)]": false,
	}
	var keys, sources []string
	for source := range cases {
		keys = append(keys, source)
		sources = append(sources, "fn main() { let h = tauri::generate_handler!["+source+"]; }\n")
	}
	for i, f := range extractEach(t, sources) {
		source, invalid := keys[i], cases[keys[i]]
		if f.Tauri == nil || len(f.Tauri.Handlers) != 1 {
			t.Fatal("handler not captured", source)
		}
		h := f.Tauri.Handlers[0]
		if (h.Invalid != "") != invalid || invalid && (len(h.Entries) != 0 || h.Plugin != "") {
			t.Error("grammar", source, h)
		}
		if strings.Contains(source, "plugin(p)") && !invalid && h.Plugin != "p" {
			t.Error("plugin", source, h.Plugin)
		}
	}
}

func TestTauriAttributeArguments(t *testing.T) {
	cases := map[string]string{
		"()":                               "",
		"(async,)":                         "",
		"(rename = \"a\", rename = \"b\")": "",
		"(rename = 1)":                     "",
		"(rename = b\"x\")":                "",
		"(,)":                              "empty argument",
		"(rename(\"x\"))":                  "list argument rename",
		"(rename = concat!(\"a\"))":        "non-literal value for rename",
		"(= \"x\")":                        "unrecognized argument",
		"[rename = \"x\"]":                 "unsupported attribute form",
	}
	var keys, sources []string
	for args := range cases {
		keys = append(keys, args)
		sources = append(sources, "#[tauri::command"+args+"]\nfn c() {}\n")
	}
	sources = append(sources, "#[tauri::command(rename = 1, rename = b\"x\", rename = \"last\")]\nfn c() {}\n")
	all := extractEach(t, sources)
	for i, f := range all[:len(keys)] {
		args, invalid := keys[i], cases[keys[i]]
		if f.Tauri == nil || len(f.Tauri.Commands) != 1 {
			t.Fatal("command not captured", args)
		}
		if got := f.Tauri.Commands[0].Attributes[0].Invalid; got != invalid {
			t.Errorf("%s: invalid %q, want %q", args, got, invalid)
		}
	}
	a := all[len(keys)].Tauri.Commands[0].Attributes[0].Arguments
	if len(a) != 3 || a[0].Kind != "literal" || a[0].Value != nil || a[1].Kind != "literal" || a[1].Value != nil || *a[2].Value != "last" {
		t.Error("argument kinds", a)
	}
}

// chainShape summarizes a handler's context chain for review 1 regressions.
func chainShape(h facts.TauriHandler) string {
	ch := h.Context.Chain
	if ch == nil {
		return h.Context.Kind
	}
	parts := []string{h.Context.Use, ch.Root.Kind + ":" + ch.Root.Callee + ch.Root.Name + ch.Root.Syntax}
	for _, m := range ch.Methods {
		parts = append(parts, methodShape(m))
	}
	if ch.Incomplete {
		parts = append(parts, "incomplete")
	}
	return strings.Join(parts, " ")
}

// methodShape writes a captured handler reference as name#index and an
// uncaptured invoke_handler as name?.
func methodShape(m facts.TauriMethod) string {
	switch {
	case m.Handler != nil:
		return fmt.Sprintf("%s#%d", m.Name, *m.Handler)
	case m.Uncaptured:
		return m.Name + "?"
	}
	return m.Name
}

// Review 1 M1: a chain never drops methods silently at parentheses, `?` or
// `.await`; `?` and `.await` mark it incomplete.
func TestTauriChainWrappers(t *testing.T) {
	cases := map[string][]string{
		"fn main() { (tauri::Builder::default().invoke_handler(tauri::generate_handler![a])).invoke_handler(tauri::generate_handler![b]).run(); }\n": {
			"statement call:tauri::Builder::default invoke_handler#0 invoke_handler#1 run",
			"statement call:tauri::Builder::default invoke_handler#0 invoke_handler#1 run",
		},
		"fn main() -> R { tauri::Builder::default().invoke_handler(tauri::generate_handler![a]).setup(f)?.invoke_handler(tauri::generate_handler![b]); Ok(()) }\n": {
			"statement call:tauri::Builder::default invoke_handler#0 setup invoke_handler#1 incomplete",
			"statement call:tauri::Builder::default invoke_handler#0 setup invoke_handler#1 incomplete",
		},
		"async fn main() { tauri::Builder::default().invoke_handler(tauri::generate_handler![a]).ready().await.invoke_handler(tauri::generate_handler![b]); }\n": {
			"statement call:tauri::Builder::default invoke_handler#0 ready invoke_handler#1 incomplete",
			"statement call:tauri::Builder::default invoke_handler#0 ready invoke_handler#1 incomplete",
		},
		// A trailing `?` after the outermost method hides no method.
		"fn main() -> R { tauri::Builder::default().invoke_handler(tauri::generate_handler![a]).run(c)?; Ok(()) }\n": {
			"statement call:tauri::Builder::default invoke_handler#0 run",
		},
		// A root that is neither a call nor an identifier is incomplete.
		"fn main(s: S) { s.app.invoke_handler(tauri::generate_handler![a]); }\n": {
			"statement other:field_expression invoke_handler#0 incomplete",
		},
	}
	checkShapes(t, cases)
}

// Review 1 M3: Use records what consumes the chain's value.
func TestTauriChainUse(t *testing.T) {
	cases := map[string][]string{
		"fn make() -> B { tauri::Builder::default().invoke_handler(tauri::generate_handler![a]) }\nfn main() { make().invoke_handler(tauri::generate_handler![b]).run(); }\n": {
			"tail call:tauri::Builder::default invoke_handler#0",
			"statement call:make invoke_handler#1 run",
		},
		"fn main() { let x = tauri::Builder::default().invoke_handler(tauri::generate_handler![a]); helper(x); }\n": {
			"let call:tauri::Builder::default invoke_handler#0",
		},
		"fn main() { let mut x = b(); x = x.invoke_handler(tauri::generate_handler![a]); }\n": {
			"assign identifier:x invoke_handler#0",
		},
		"fn make() -> B { return tauri::Builder::default().invoke_handler(tauri::generate_handler![a]); }\n": {
			"return call:tauri::Builder::default invoke_handler#0",
		},
		"fn main() { helper(tauri::Builder::default().invoke_handler(tauri::generate_handler![a])); }\n": {
			"argument call:tauri::Builder::default invoke_handler#0",
		},
		"fn main() { let f = |b: B| b.invoke_handler(tauri::generate_handler![a]); }\n": {
			"tail identifier:b invoke_handler#0",
		},
		"fn main() { let x = Some(tauri::Builder::default().invoke_handler(tauri::generate_handler![a]).build()?); }\n": {
			"argument call:tauri::Builder::default invoke_handler#0 build",
		},
		"fn main() { let x = [tauri::Builder::default().invoke_handler(tauri::generate_handler![a])]; }\n": {
			"other call:tauri::Builder::default invoke_handler#0 incomplete",
		},
	}
	checkShapes(t, cases)
}

// Review 2 M1: only a plain local assignment is an `assign` use; a field,
// deref or index target is `other` and incomplete.
func TestTauriAssignTarget(t *testing.T) {
	checkShapes(t, map[string][]string{
		"impl S { fn f(&mut self) { self.b = tauri::Builder::default().invoke_handler(tauri::generate_handler![a]); } }\n": {
			"other call:tauri::Builder::default invoke_handler#0 incomplete",
		},
		"fn f(b: &mut B) { *b = tauri::Builder::default().invoke_handler(tauri::generate_handler![a]); }\n": {
			"other call:tauri::Builder::default invoke_handler#0 incomplete",
		},
		"fn f(v: &mut [B]) { v[0] = tauri::Builder::default().invoke_handler(tauri::generate_handler![a]); }\n": {
			"other call:tauri::Builder::default invoke_handler#0 incomplete",
		},
		"fn f() { let b; b = tauri::Builder::default().invoke_handler(tauri::generate_handler![a]); }\n": {
			"assign call:tauri::Builder::default invoke_handler#0",
		},
	})
}

// Review 2 M2: every identifier-rooted invoke_handler chain in a function with
// a captured handler is recorded in its flow, captured or not, so a later
// replacement is visible.
func TestTauriUncapturedCalls(t *testing.T) {
	cases := map[string]string{
		// let-bound handler: the later call replaces handler 0.
		"fn main() { let app = tauri::Builder::default().invoke_handler(tauri::generate_handler![a]); let h = tauri::generate_handler![b]; app.invoke_handler(h).run(ctx); }\n": "statement identifier:app invoke_handler? run",
		// a closure handler.
		"fn main() { let app = tauri::Builder::default().invoke_handler(tauri::generate_handler![a]); app.invoke_handler(|i| true).run(ctx); }\n": "statement identifier:app invoke_handler? run",
		// captured and uncaptured calls on one chain are one record.
		"fn main() { let b = tauri::Builder::default(); b.invoke_handler(tauri::generate_handler![a]).invoke_handler(h); }\n": "statement identifier:b invoke_handler#0 invoke_handler?",
		// two chains on one binding, in source order.
		"fn main() { let b = tauri::Builder::default(); b.invoke_handler(h).run(c); b.invoke_handler(tauri::generate_handler![a]).run(c); }\n": "statement identifier:b invoke_handler? run | statement identifier:b invoke_handler#0 run",
		// inside a closure of the function.
		"fn main() { let b = tauri::Builder::default().invoke_handler(tauri::generate_handler![a]); let f = || b.invoke_handler(h); }\n": "tail identifier:b invoke_handler?",
	}
	var keys []string
	for source := range cases {
		keys = append(keys, source)
	}
	for i, f := range extractEach(t, keys) {
		if f.Tauri == nil || len(f.Tauri.Flows) != 1 || !f.Tauri.Valid() {
			t.Fatal("flow", keys[i], f.Tauri)
		}
		var got []string
		for _, call := range f.Tauri.Flows[0].Calls {
			parts := []string{call.Use, call.Chain.Root.Kind + ":" + call.Chain.Root.Name}
			for _, m := range call.Chain.Methods {
				parts = append(parts, methodShape(m))
			}
			got = append(got, strings.Join(parts, " "))
		}
		if strings.Join(got, " | ") != cases[keys[i]] {
			t.Errorf("%s:\n got %s\nwant %s", keys[i], strings.Join(got, " | "), cases[keys[i]])
		}
	}
	// A chain inside a handler context marks an uncaptured call as well.
	f := extractEach(t, []string{"fn main() { tauri::Builder::default().invoke_handler(tauri::generate_handler![a]).invoke_handler(h).run(c); }\n"})[0]
	if got := chainShape(f.Tauri.Handlers[0]); got != "statement call:tauri::Builder::default invoke_handler#0 invoke_handler? run" {
		t.Error("uncaptured method in a handler chain", got)
	}
	// Functions without a captured handler record nothing.
	// Version 3 keeps the call as an app site only.
	if f := extractEach(t, []string{"fn finish(b: B) { let c = b; c.invoke_handler(h); }\n"})[0]; f.Tauri == nil || len(f.Tauri.Flows)+len(f.Tauri.Handlers) != 0 || len(f.Tauri.Apps) != 1 || f.Tauri.Apps[0].Kind != "invoke_handler" {
		t.Error("uncaptured call without a handler", f.Tauri)
	}
}

// Review 2 m3: cfg on an enclosing statement reaches the bindings inside it,
// and a binding outside the body's top-level statement list is Conditional.
func TestTauriBindingCfgConditional(t *testing.T) {
	cases := map[string]string{
		"fn main() { let mut b = tauri::Builder::default();\n#[cfg(feature = \"x\")]\n{ b = b.manage(1); }\nb.invoke_handler(tauri::generate_handler![a]); }\n": "let - -; assign cfg conditional",
		"fn main() { let mut b = tauri::Builder::default();\n#[cfg(x)]\nif c { b = b.manage(1); }\nb.invoke_handler(tauri::generate_handler![a]); }\n":          "let - -; assign cfg conditional",
		"fn main() { let b = tauri::Builder::default();\n#[cfg_attr(x, cfg(y))]\nlet b = b.manage(1);\nb.invoke_handler(tauri::generate_handler![a]); }\n":      "let - -; let cfg -",
		"fn main() { let b = tauri::Builder::default();\n#[cfg_attr(x, allow(y))]\nlet b = b.manage(1);\nb.invoke_handler(tauri::generate_handler![a]); }\n":    "let - -; let - -",
		"fn main() { let mut b = tauri::Builder::default(); if c { b = b.manage(1); } b.invoke_handler(tauri::generate_handler![a]); }\n":                       "let - -; assign - conditional",
		"fn main() { let mut b = tauri::Builder::default(); match c { _ => b = b.manage(1) } b.invoke_handler(tauri::generate_handler![a]); }\n":                "let - -; assign - conditional",
		"fn main() { let mut b = tauri::Builder::default(); for _ in x { b = b.manage(1); } b.invoke_handler(tauri::generate_handler![a]); }\n":                 "let - -; assign - conditional",
		"fn main() { let mut b = tauri::Builder::default(); let f = || b = b.manage(1); b.invoke_handler(tauri::generate_handler![a]); }\n":                     "let - -; assign - conditional",
		"fn main() { let mut b = tauri::Builder::default(); let g = async { b = b.manage(1); }; b.invoke_handler(tauri::generate_handler![a]); }\n":             "let - -; assign - conditional",
		"fn main() { let b = tauri::Builder::default(); { let b = b.manage(1); } b.invoke_handler(tauri::generate_handler![a]); }\n":                            "let - -; let - conditional",
		"fn main() { let b = tauri::Builder::default(); let b = if c { b.manage(1) } else { b }; b.invoke_handler(tauri::generate_handler![a]); }\n":            "let - -; let - -",
	}
	var keys []string
	for source := range cases {
		keys = append(keys, source)
	}
	for i, f := range extractEach(t, keys) {
		if f.Tauri == nil || len(f.Tauri.Flows) != 1 {
			t.Fatal("flow", keys[i], f.Tauri)
		}
		var got []string
		for _, b := range f.Tauri.Flows[0].Bindings {
			cfg, conditional := "-", "-"
			if b.Cfg {
				cfg = "cfg"
			}
			if b.Conditional {
				conditional = "conditional"
			}
			got = append(got, b.Kind+" "+cfg+" "+conditional)
		}
		if strings.Join(got, "; ") != cases[keys[i]] {
			t.Errorf("%s:\n got %s\nwant %s", keys[i], strings.Join(got, "; "), cases[keys[i]])
		}
	}
}

// Review 2 m4: a command's Cfg includes cfg applied through cfg_attr at any
// nesting depth.
func TestTauriCommandCfgAttr(t *testing.T) {
	cases := map[string]bool{
		"#[cfg_attr(not(x), cfg(y))]\n#[tauri::command]\nfn f() {}\n":           true,
		"#[cfg_attr(a, cfg_attr(b, cfg(c)))]\n#[tauri::command]\nfn f() {}\n":   true,
		"#[cfg_attr(a, allow(x), cfg(y))]\n#[tauri::command]\nfn f() {}\n":      true,
		"#[cfg_attr(a, tauri::command, cfg(y))]\nfn f() {}\n":                   true,
		"#[cfg_attr(a, allow(x))]\n#[tauri::command]\nfn f() {}\n":              false,
		"#[cfg_attr(a, cfg_attr(b, allow(x)))]\n#[tauri::command]\nfn f() {}\n": false,
		"#[tauri::command]\n#[cfg(x)]\nfn f() {}\n":                             true,
		"#[tauri::command]\nfn f() {}\n":                                        false,
	}
	var keys []string
	for source := range cases {
		keys = append(keys, source)
	}
	for i, f := range extractEach(t, keys) {
		if f.Tauri == nil || len(f.Tauri.Commands) != 1 || f.Tauri.Commands[0].Cfg != cases[keys[i]] {
			t.Error("command cfg", keys[i], f.Tauri)
		}
	}
}

func checkShapes(t *testing.T, cases map[string][]string) {
	t.Helper()
	var keys []string
	for source := range cases {
		keys = append(keys, source)
	}
	for i, f := range extractEach(t, keys) {
		want := cases[keys[i]]
		if f.Tauri == nil || len(f.Tauri.Handlers) != len(want) {
			t.Fatal("handlers", keys[i], f.Tauri)
		}
		if !f.Tauri.Valid() {
			t.Error("invalid payload", keys[i])
		}
		for j, h := range f.Tauri.Handlers {
			if got := chainShape(h); got != want[j] {
				t.Errorf("%s handler %d:\n got %s\nwant %s", keys[i], j, got, want[j])
			}
		}
	}
}

// Review 1 M2: names bound by patterns, closure and function parameters that
// enclose a root site are recorded as Shadows; others are not.
func TestTauriFlowShadows(t *testing.T) {
	cases := map[string]string{
		"fn main() { let builder = tauri::Builder::default(); if let Some(builder) = other() { builder.invoke_handler(tauri::generate_handler![a]); } }\n":         "builder",
		"fn main() { let builder = tauri::Builder::default(); while let Some(builder) = other() { builder.invoke_handler(tauri::generate_handler![a]); } }\n":      "builder",
		"fn main() { let builder = tauri::Builder::default(); let f = |builder| builder.invoke_handler(tauri::generate_handler![a]); }\n":                          "builder",
		"fn main() { let b = tauri::Builder::default(); match o { Some(b) => b.invoke_handler(tauri::generate_handler![a]), None => b }; }\n":                      "b",
		"fn main() { let b = tauri::Builder::default(); for (b, _) in all { b.invoke_handler(tauri::generate_handler![a]); } }\n":                                  "b",
		"fn main(builder: B) { let app = builder; app.invoke_handler(tauri::generate_handler![a]); }\n":                                                            "builder",
		"fn main() { let b = tauri::Builder::default(); let S { b, .. } = s; b.invoke_handler(tauri::generate_handler![a]); }\n":                                   "b",
		"fn main() { let b = tauri::Builder::default(); let f = |b| b; b.invoke_handler(tauri::generate_handler![a]); }\n":                                         "",
		"fn main() { let app = tauri::Builder::default().setup(|app| Ok(())); if let Some(x) = y { } app.invoke_handler(tauri::generate_handler![a]); }\n":         "",
		"fn main() { let b = tauri::Builder::default(); fn inner(b: B) {} let Some(other) = o else { return }; b.invoke_handler(tauri::generate_handler![a]); }\n": "",
		// Review 2 m1: match guard patterns (if let guards, let chains).
		"fn main() { let builder = tauri::Builder::default(); match x { Some(y) if let Some(builder) = y.get() => { builder.invoke_handler(tauri::generate_handler![a]); } _ => {} } }\n":  "builder",
		"fn main() { let b = tauri::Builder::default(); match x { Some(y) if let Some(z) = y && let Some(b) = z.get() => { b.invoke_handler(tauri::generate_handler![a]); } _ => {} } }\n": "b",
		// Review 2 m2: a macro invocation that mentions a flow name.
		"macro_rules! rebind { ($n:ident) => { let $n = other(); } }\nfn main() { let builder = tauri::Builder::default(); rebind!(builder); builder.invoke_handler(tauri::generate_handler![a]); }\n": "builder",
		"fn main() { let builder = tauri::Builder::default(); builder.invoke_handler(tauri::generate_handler![a]); log!(info, { inner!(builder) }); }\n":                                               "builder",
		"fn main() { let builder = tauri::Builder::default(); println!(\"x\"); builder.invoke_handler(tauri::generate_handler![a]); }\n":                                                               "",
		"fn main() { let builder = tauri::Builder::default(); fn inner() { m!(builder); } builder.invoke_handler(tauri::generate_handler![a]); }\n":                                                    "",
	}
	var keys []string
	for source := range cases {
		keys = append(keys, source)
	}
	for i, f := range extractEach(t, keys) {
		if f.Tauri == nil || len(f.Tauri.Flows) != 1 {
			t.Fatal("flow", keys[i], f.Tauri)
		}
		flow := f.Tauri.Flows[0]
		if flow.Shadows == nil || strings.Join(flow.Shadows, ",") != cases[keys[i]] {
			t.Errorf("%s: shadows %v, want %q", keys[i], flow.Shadows, cases[keys[i]])
		}
	}
}

// Review 1 m2: predicates and string values are line-ending independent even
// for direct Extract callers (the index already normalizes line endings in
// scan before extraction). Byte offsets differ by construction, so the
// source-text fields are compared.
func TestTauriNewlineNormalization(t *testing.T) {
	lf := "#[cfg_attr(\n  all(a,\n b), tauri::command(rename = \"x\ny\"))]\nfn a() {}\nfn main() { tauri::plugin::Builder::new(\"p\nq\").invoke_handler(tauri::generate_handler![a]); }\n"
	all := extractEach(t, []string{lf, strings.ReplaceAll(lf, "\n", "\r\n")})
	for i, f := range all {
		a := f.Tauri.Commands[0].Attributes[0]
		root := f.Tauri.Handlers[0].Context.Chain.Root
		if a.Predicate != "all(a,\n b)" || *a.Arguments[0].Value != "x\ny" || *root.Argument != "p\nq" {
			t.Errorf("copy %d: predicate %q, rename %q, root argument %q", i, a.Predicate, *a.Arguments[0].Value, *root.Argument)
		}
		if !reflect.DeepEqual(f.Tauri.Commands, all[0].Tauri.Commands) {
			t.Error("CRLF changed the commands")
		}
	}
}

// Review 1 m3, m4, m5, n2: nested cfg_attr, every qualifying attribute, the
// enclosing-module cfg gap, and commands inside macro bodies.
func TestTauriAttributeCoverage(t *testing.T) {
	all := extractEach(t, []string{
		"#[cfg_attr(x, cfg_attr(y, tauri::command(rename = \"n\")))]\nfn n() {}\n",
		"#[cfg_attr(x, cfg_attr(y, cfg_attr(z, poise::command), tauri::command))]\nfn m() {}\n",
		"#[poise::command]\n#[tauri::command]\nfn both() {}\n#[cfg_attr(x, tauri::command, poise::command)]\nfn list() {}\n",
		"#[cfg(test)]\nmod t { #[tauri::command] fn q() {} }\nmod u { #![cfg(x)] #[command] pub fn w() {} }\n",
		"macro_rules! m { () => { #[tauri::command] fn in_macro() {} }; }\nm!();\nm! { #[tauri::command] fn in_call() {} }\n",
	})
	attrs := func(c facts.TauriCommand) string {
		var parts []string
		for _, a := range c.Attributes {
			parts = append(parts, a.Path+"|"+a.Form+"|"+a.Predicate)
		}
		return strings.Join(parts, "; ")
	}
	if c := all[0].Tauri.Commands[0]; attrs(c) != "tauri::command|cfg_attr|all(x, y)" || *c.Attributes[0].Arguments[0].Value != "n" {
		t.Error("nested cfg_attr", attrs(c))
	}
	if c := all[1].Tauri.Commands[0]; attrs(c) != "poise::command|cfg_attr|all(x, y, z); tauri::command|cfg_attr|all(x, y)" {
		t.Error("deeply nested cfg_attr", attrs(c))
	}
	if cs := all[2].Tauri.Commands; len(cs) != 2 || attrs(cs[0]) != "poise::command|attribute|; tauri::command|attribute|" || attrs(cs[1]) != "tauri::command|cfg_attr|x; poise::command|cfg_attr|x" {
		t.Error("every qualifying attribute", all[2].Tauri.Commands)
	}
	// Known gap (m5): the enclosing module's cfg is not reflected in Cfg.
	if cs := all[3].Tauri.Commands; len(cs) != 2 || cs[0].Cfg || cs[1].Cfg {
		t.Error("enclosing module cfg", cs)
	}
	if all[4].Tauri != nil {
		t.Error("command inside a macro body captured", all[4].Tauri)
	}
}

// Review 1 m1: Valid rejects structurally incomplete payloads, so a damaged
// cache entry is re-extracted instead of trusted.
func TestTauriSyntaxValid(t *testing.T) {
	source := "#[tauri::command(rename = \"x\")]\nfn a() {}\nfn main() { let b = tauri::Builder::default(); b.invoke_handler(tauri::generate_handler![a]); b.invoke_handler(h); }\n"
	f := extractEach(t, []string{source})[0]
	if f.Tauri == nil || !f.Tauri.Valid() || len(f.Tauri.Commands) != 1 || len(f.Tauri.Handlers) != 1 || len(f.Tauri.Flows) != 1 || len(f.Tauri.Flows[0].Calls) != 2 {
		t.Fatal("baseline", f.Tauri)
	}
	var nilSyntax *facts.TauriSyntax
	if !nilSyntax.Valid() {
		t.Fatal("absent payload must be valid")
	}
	mutations := map[string]func(*facts.TauriSyntax){
		"version":           func(s *facts.TauriSyntax) { s.Version = 0 },
		"command name":      func(s *facts.TauriSyntax) { s.Commands[0].Name = "" },
		"no attributes":     func(s *facts.TauriSyntax) { s.Commands[0].Attributes = nil },
		"attribute form":    func(s *facts.TauriSyntax) { s.Commands[0].Attributes[0].Form = "" },
		"attribute scope":   func(s *facts.TauriSyntax) { s.Commands[0].Attributes[0].Scope = -1 },
		"cfg_attr no pred":  func(s *facts.TauriSyntax) { s.Commands[0].Attributes[0].Form = "cfg_attr" },
		"argument value":    func(s *facts.TauriSyntax) { s.Commands[0].Attributes[0].Arguments[0].Value = nil },
		"argument kind":     func(s *facts.TauriSyntax) { s.Commands[0].Attributes[0].Arguments[0].Kind = "" },
		"handler owner":     func(s *facts.TauriSyntax) { s.Handlers[0].Owner = "" },
		"entry segments":    func(s *facts.TauriSyntax) { s.Handlers[0].Entries[0].Segments = nil },
		"entry hash":        func(s *facts.TauriSyntax) { s.Handlers[0].Entries[0].Hash = "" },
		"invalid entries":   func(s *facts.TauriSyntax) { s.Handlers[0].Invalid = "x" },
		"context kind":      func(s *facts.TauriSyntax) { s.Handlers[0].Context.Kind = "" },
		"context use":       func(s *facts.TauriSyntax) { s.Handlers[0].Context.Use = "" },
		"context chain":     func(s *facts.TauriSyntax) { s.Handlers[0].Context.Chain = nil },
		"root kind":         func(s *facts.TauriSyntax) { s.Handlers[0].Context.Chain.Root.Kind = "" },
		"handler index":     func(s *facts.TauriSyntax) { i := 1; s.Handlers[0].Context.Chain.Methods[0].Handler = &i },
		"negative index":    func(s *facts.TauriSyntax) { i := -1; s.Handlers[0].Context.Chain.Methods[0].Handler = &i },
		"method name":       func(s *facts.TauriSyntax) { s.Handlers[0].Context.Chain.Methods[0].Name = "" },
		"flow shadows":      func(s *facts.TauriSyntax) { s.Flows[0].Shadows = nil },
		"flow bindings":     func(s *facts.TauriSyntax) { s.Flows[0].Bindings = nil },
		"binding kind":      func(s *facts.TauriSyntax) { s.Flows[0].Bindings[0].Kind = "" },
		"binding value":     func(s *facts.TauriSyntax) { s.Flows[0].Bindings[0].Value = "" },
		"plain alternative": func(s *facts.TauriSyntax) { s.Flows[0].Bindings[0].Alternatives = nil },
		"call root span":    func(s *facts.TauriSyntax) { s.Flows[0].Bindings[0].Alternatives[0].Root.Span = nil },
		// Review 2: invoke_handler marking and flow calls.
		"captured and uncaptured": func(s *facts.TauriSyntax) { s.Handlers[0].Context.Chain.Methods[0].Uncaptured = true },
		"unmarked invoke_handler": func(s *facts.TauriSyntax) { s.Flows[0].Calls[1].Chain.Methods[0].Uncaptured = false },
		"uncaptured other method": func(s *facts.TauriSyntax) { s.Flows[0].Calls[1].Chain.Methods[0].Name = "setup" },
		"call root kind": func(s *facts.TauriSyntax) {
			s.Flows[0].Calls[0].Chain.Root = s.Flows[0].Bindings[0].Alternatives[0].Root
		},
		"call use":            func(s *facts.TauriSyntax) { s.Flows[0].Calls[0].Use = "" },
		"call other complete": func(s *facts.TauriSyntax) { s.Flows[0].Calls[0].Use = "other" },
		"call without invoke": func(s *facts.TauriSyntax) { s.Flows[0].Calls[0].Chain.Methods = nil },
	}
	data, err := json.Marshal(f.Tauri)
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range mutations {
		var s facts.TauriSyntax
		if err := json.Unmarshal(data, &s); err != nil {
			t.Fatal(err)
		}
		if !s.Valid() {
			t.Fatal("round trip", name)
		}
		mutate(&s)
		if s.Valid() {
			t.Error("mutation accepted:", name)
		}
	}
}

// B0 (T015c2): flows record every macro invocation and macro_rules!
// definition in the body, nested token-tree invocations with the outer
// scope, closures included and nested fn items excluded, and each binding's
// statement end.
func TestTauriFlowMacros(t *testing.T) {
	source := `fn main() {
    let b = tauri::Builder::default();
    macro_rules! noop { () => {}; }
    log::info!("{} {}", env!("X"), concat!("a", stringify!(b)));
    let run = || { eprintln!("x"); };
    fn nested() { println!("skipped"); }
    let app = b.setup(|_a| { dbg!(1); Ok(()) });
    app.invoke_handler(tauri::generate_handler![x]).run(tauri::generate_context!()).unwrap();
}
`
	f := extractEach(t, []string{source})[0]
	if f.Tauri == nil || len(f.Tauri.Flows) != 1 || f.Tauri.Version != facts.TauriSyntaxVersion {
		t.Fatal("flow", f.Tauri)
	}
	flow := f.Tauri.Flows[0]
	var got []string
	scopes := map[string]int{}
	for _, m := range flow.Macros {
		got = append(got, m.Kind+":"+m.Path)
		scopes[m.Path] = m.Scope
	}
	want := []string{"definition:noop", "invocation:log::info", "invocation:env", "invocation:concat", "invocation:stringify", "invocation:eprintln", "invocation:dbg", "invocation:tauri::generate_handler", "invocation:tauri::generate_context"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("macros\n got %v\nwant %v", got, want)
	}
	if scopes["env"] != scopes["log::info"] || scopes["stringify"] != scopes["log::info"] {
		t.Error("nested invocations take the outer scope", scopes)
	}
	for _, b := range flow.Bindings {
		text := source[b.Offset:b.End]
		if !strings.HasPrefix(text, "let "+b.Name) || !strings.HasSuffix(text, ";") {
			t.Errorf("binding statement %q", text)
		}
	}
	if !f.Tauri.Valid() {
		t.Error("payload invalid")
	}
	// Version 1 payloads and a binding end before its start are cache misses.
	old := *f.Tauri
	old.Version = 1
	broken := *f.Tauri
	broken.Flows = []facts.TauriFlow{flow}
	broken.Flows[0].Bindings = append([]facts.TauriBinding(nil), flow.Bindings...)
	broken.Flows[0].Bindings[0].End = broken.Flows[0].Bindings[0].Offset
	if old.Valid() || broken.Valid() {
		t.Error("stale or damaged payload accepted")
	}
}

// Version 3 (T015c2 Revision 2): app sites, the handler function's
// attributes with cfg_attr flattened, and cfg(test)-only regions.
func TestTauriAppSites(t *testing.T) {
	source := `use tauri::Builder as App;
#[cfg_attr(mobile, tauri::mobile_entry_point)]
#[tokio::main]
async fn main() {
    App::new().invoke_handler(tauri::generate_handler![a]).run(tauri::generate_context!()).unwrap();
    tempfile::Builder::new();
    Builder::invoke_handler(b, h);
    other.invoke_handler(|i| true);
    log::info!("{:?}", tauri::generate_context!());
}
#[cfg(test)]
mod tests {
    #[tauri::command]
    fn in_test() {}
    fn t() { tauri::Builder::default(); }
}
#[cfg(all(unix, test))]
#[tauri::command]
fn own() {}
#[cfg(any(test, unix))]
#[tauri::command]
fn either() {}
`
	f := extractEach(t, []string{source})[0]
	if f.Tauri == nil || !f.Tauri.Valid() {
		t.Fatal("payload", f.Tauri)
	}
	var sites []string
	for _, a := range f.Tauri.Apps {
		s := a.Kind + ":" + a.Path
		if a.Test {
			s += ":test"
		}
		sites = append(sites, s)
	}
	want := []string{"root:App", "context:tauri::generate_context", "root:tempfile::Builder", "invoke_handler:Builder::invoke_handler", "root:Builder", "invoke_handler:invoke_handler", "context:tauri::generate_context", "root:tauri::Builder:test"}
	if !reflect.DeepEqual(sites, want) {
		t.Errorf("app sites\n got %v\nwant %v", sites, want)
	}
	var attrs []string
	for _, a := range f.Tauri.Handlers[0].Attributes {
		attrs = append(attrs, a.Path)
	}
	if !reflect.DeepEqual(attrs, []string{"cfg_attr", "tauri::mobile_entry_point", "tokio::main"}) || f.Tauri.Handlers[0].Test {
		t.Errorf("handler attributes %v", attrs)
	}
	tests := map[string]bool{}
	for _, c := range f.Tauri.Commands {
		tests[c.Name] = c.Test
	}
	if !reflect.DeepEqual(tests, map[string]bool{"in_test": true, "own": true, "either": false}) {
		t.Errorf("command test flags %v", tests)
	}
}

// Revision 3, MA2: every value-position reference through a Builder-like
// name, typed constructors, aliases, re-exports and macro token trees are
// app sites; signatures are not.
func TestTauriAppSiteForms(t *testing.T) {
	source := `use tauri::Builder as App;
use App as Again;
pub use tauri::Builder as AppBuilder;
pub(crate) use tauri::{self, Wry, *};
pub use crate::models::*;
pub use other::{Builder as Ob};
type B = tauri::Builder<tauri::Wry>;
type Plain = Option<tauri::Builder<tauri::Wry>>;
struct S { b: tauri::Builder<Wry> }
struct T(App<Wry>);
fn sig(b: tauri::Builder<Wry>, c: &App<Wry>) -> Option<App<Wry>> { None }
fn made() -> tauri::Builder<Wry> { if x { Default::default() } else { return T::new(); } }
fn helper() -> App<Wry> { let v = Vec::new(); App::default() }
fn main() {
    let b: tauri::Builder<tauri::Wry> = Default::default();
    let c: App<Wry> = tauri::Builder::default();
    <tauri::Builder<tauri::Wry>>::default();
    <tauri::Builder<Wry> as Default>::default();
    let mk = tauri::Builder::<tauri::Wry>::default;
    Again::new();
    f(tauri::app::Builder::default);
    tokio::runtime::Builder::new_multi_thread();
    app!();
    other!(tauri, Builder);
    log::info!("Builder {}", x);
}
macro_rules! app { () => { App::default() } }
macro_rules! plain { () => { 1 } }
`
	f := extractEach(t, []string{source})[0]
	if f.Tauri == nil || !f.Tauri.Valid() {
		t.Fatal("payload", f.Tauri)
	}
	var sites []string
	for _, a := range f.Tauri.Apps {
		sites = append(sites, fmt.Sprintf("%s:%s:%d", a.Kind, a.Path, a.Span.StartLine))
	}
	want := []string{
		"reexport:tauri::Builder:3",
		"reexport:tauri::*:4",
		"reexport:other::Builder:6",
		"alias:tauri::Builder:7",
		"typed:tauri::Builder:9",
		"typed:App:10",
		"typed:tauri::Builder:12",
		"typed:tauri::Builder:12",
		"root:App:13",
		"typed:tauri::Builder:15",
		"root:tauri::Builder:16",
		"root:tauri::Builder:17",
		"root:tauri::Builder:18",
		"root:tauri::Builder:19",
		"root:Again:20",
		"root:tauri::app::Builder:21",
		"root:tokio::runtime::Builder:22",
		"macro:Builder:24",
		"macro:tauri::Builder:24",
		"macro:App:27",
	}
	if !reflect.DeepEqual(sites, want) {
		t.Errorf("app sites\n got %v\nwant %v", sites, want)
	}
}
