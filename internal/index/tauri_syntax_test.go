package index_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/store"
)

// tauriPayloads reads every cached Rust fact entry, so tests see exactly what
// replay will reuse.
func tauriPayloads(t *testing.T, s store.Store) map[string]facts.Facts {
	t.Helper()
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]facts.Facts{}
	for _, f := range snapshot.Files {
		if f.Language != "rust" {
			continue
		}
		data, err := s.FactCache(context.Background(), "fixture", f.Path, f.ContentHash, fmt.Sprintf("%s/max=%d", index.Version, 2<<20))
		if err != nil {
			t.Fatal(err)
		}
		var cached facts.Facts
		if err := json.Unmarshal(data, &cached); err != nil || !cached.Tauri.Valid() {
			t.Fatal(f.Path, err)
		}
		if cached.Tauri == nil && bytes.Contains(data, []byte(`"Tauri"`)) {
			t.Fatal("absent Tauri payload must be omitted", f.Path)
		}
		out[f.Path] = cached
	}
	return out
}

func tauriProjection(all map[string]facts.Facts) map[string]facts.TauriSyntax {
	out := map[string]facts.TauriSyntax{}
	for p, f := range all {
		if f.Tauri != nil {
			out[p] = *f.Tauri
		}
	}
	return out
}

func TestTauriCommandSyntax(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/tauri-command-syntax")); err != nil {
		t.Fatal(err)
	}
	s, opt := open(t), options(t)
	first, _, _ := emit(t, root, s, opt)
	all := tauriPayloads(t, s)
	projection := tauriProjection(all)
	golden.Check(t, "../../testdata/fixtures/tauri-command-syntax.golden.json", projection)

	second, summary, writes := emit(t, root, s, opt)
	if !bytes.Equal(first, second) || summary.Parsed != 0 || writes.Files+writes.Nodes+writes.Edges+writes.Deleted != 0 {
		t.Fatal("Tauri replay", summary, writes)
	}
	if !reflect.DeepEqual(projection, tauriProjection(tauriPayloads(t, s))) {
		t.Fatal("replayed Tauri payload changed")
	}

	// Files without Tauri syntax carry no payload.
	for _, p := range []string{"app/build.rs", "app/src/main.rs"} {
		if _, ok := all[p]; !ok {
			t.Fatal("missing cached facts", p)
		}
		if _, ok := projection[p]; ok {
			t.Fatal("unexpected Tauri payload", p)
		}
	}
	// A5: build.rs ACL inputs stay in the existing Rust calls only.
	calls := map[string]bool{}
	for _, c := range all["app/build.rs"].Calls {
		calls[c.Name+"|"+c.StaticName] = true
	}
	for _, want := range []string{"tauri_build::try_build|", "tauri_build::Attributes::new|", "tauri_build::AppManifest::new|", "|app_manifest", "|commands"} {
		if !calls[want] {
			t.Error("build.rs call", want, calls)
		}
	}

	host := func(file string) []byte {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(root, file))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	commands := map[string]facts.TauriCommand{}
	for file, p := range projection {
		for _, c := range p.Commands {
			commands[file+"#"+c.Name] = c
			// A function inside a function body is no module item (row
			// RS-D10 of docs/lang/rust-structure.md): no node, no identity.
			if (c.Identity == "") != (c.Name == "nested_fn") {
				t.Error("command identity", file, c.Name, c.Identity)
			}
			// The function span excludes the attribute, like the function node.
			if text := string(atSpan(host(file), c.Span)); strings.HasPrefix(text, "#") {
				t.Error("function span includes attribute", c.Name)
			}
			if text := string(atSpan(host(file), c.Attributes[0].Span)); !strings.HasPrefix(text, "#[") || !strings.HasSuffix(text, "]") {
				t.Error("attribute span", c.Name, text)
			}
		}
	}
	names := []string{}
	for k := range commands {
		names = append(names, k)
	}
	want := []string{
		"app/src/commands/aliased.rs#aliased", "app/src/commands/arguments.rs#bare_path", "app/src/commands/arguments.rs#non_literal",
		"app/src/commands/arguments.rs#r#type", "app/src/commands/arguments.rs#raw_literal", "app/src/commands/arguments.rs#with_rename",
		"app/src/commands/arguments.rs#with_root", "app/src/commands/grouped.rs#grouped_bare", "app/src/commands/mod.rs#debug_only",
		"app/src/commands/mod.rs#greet", "app/src/commands/mod.rs#member", "app/src/commands/mod.rs#nested_fn",
		"app/src/commands/mod.rs#multiline", "app/src/commands/mod.rs#nested_cfg_attr", "app/src/commands/mod.rs#both_attributes",
		"app/src/commands/mod.rs#in_cfg_module", "app/src/commands/mod.rs#cfg_through_cfg_attr",
		"app/src/commands/mod.rs#cfg_through_nested_cfg_attr", "app/src/commands/mod.rs#cfg_attr_without_cfg",
		"app/src/commands/mod.rs#nested_module", "app/src/commands/mod.rs#optional", "app/src/commands/mod.rs#rooted",
		"app/src/commands/plain.rs#plain_bare", "app/src/negatives.rs#poise_cmd", "app/src/plugin.rs#ping",
	}
	if !sameSet(names, want) {
		t.Fatal("command filter", names)
	}
	check := func(key string, ok bool) {
		t.Helper()
		if !ok {
			t.Error("command", key, commands[key])
		}
	}
	c := commands["app/src/commands/mod.rs#optional"]
	check("optional", c.Attributes[0].Form == "cfg_attr" && c.Attributes[0].Predicate == `feature = "desktop"` && c.Attributes[0].Path == "tauri::command" && len(c.Attributes[0].Arguments) == 1 && *c.Attributes[0].Arguments[0].Value == "optional_cmd")
	c = commands["app/src/commands/mod.rs#debug_only"]
	check("debug_only", c.Cfg && c.Visibility == "pub(crate)")
	c = commands["app/src/commands/mod.rs#rooted"]
	check("rooted", c.Attributes[0].Path == "::tauri::command" && c.AsyncFn)
	c = commands["app/src/commands/mod.rs#member"]
	check("member", c.Identity == "method:app/src/commands/mod.rs#Service.member")
	c = commands["app/src/commands/mod.rs#nested_module"]
	check("nested_module", c.Identity == "function:app/src/commands/mod.rs#inline.nested_module" && c.Attributes[0].Scope != 0)
	c = commands["app/src/commands/mod.rs#nested_fn"]
	check("nested_fn", c.Identity == "" && c.Attributes[0].Scope != 0 && c.Visibility == "")
	c = commands["app/src/commands/aliased.rs#aliased"]
	check("aliased", c.Attributes[0].Path == "cmd" && c.Attributes[0].Invalid == "")
	c = commands["app/src/commands/grouped.rs#grouped_bare"]
	check("grouped_bare", c.Attributes[0].Path == "command" && c.AsyncFn)
	c = commands["app/src/commands/arguments.rs#with_rename"]
	check("with_rename", len(c.Attributes[0].Arguments) == 2 && c.Attributes[0].Arguments[1].Key == "rename_all" && *c.Attributes[0].Arguments[1].Value == "snake_case")
	c = commands["app/src/commands/arguments.rs#with_root"]
	check("with_root", c.Attributes[0].Async && !c.AsyncFn && *c.Attributes[0].Arguments[0].Value == "crate")
	c = commands["app/src/commands/arguments.rs#raw_literal"]
	check("raw_literal", c.Attributes[0].Invalid == "" && *c.Attributes[0].Arguments[0].Value == "raw" && c.AsyncFn)
	c = commands["app/src/commands/arguments.rs#non_literal"]
	check("non_literal", c.Attributes[0].Invalid != "" && c.Attributes[0].Arguments[0].Value == nil && c.Attributes[0].Arguments[0].Kind == "expression")
	c = commands["app/src/commands/arguments.rs#bare_path"]
	check("bare_path", c.Attributes[0].Invalid != "" && c.Attributes[0].Arguments[0].Kind == "path")
	c = commands["app/src/commands/arguments.rs#r#type"]
	check("r#type", c.Identity == "function:app/src/commands/arguments.rs#r%23type")
	c = commands["app/src/negatives.rs#poise_cmd"]
	check("poise_cmd", c.Attributes[0].Path == "poise::command")
	// Review 1 (m2, m3, m4, m5).
	c = commands["app/src/commands/mod.rs#multiline"]
	check("multiline", len(c.Attributes) == 1 && c.Attributes[0].Form == "cfg_attr" && c.Attributes[0].Predicate == "all(feature = \"a\",\n        feature = \"b\")")
	c = commands["app/src/commands/mod.rs#nested_cfg_attr"]
	check("nested_cfg_attr", len(c.Attributes) == 1 && c.Attributes[0].Path == "tauri::command" && c.Attributes[0].Predicate == `all(feature = "outer", feature = "inner")`)
	c = commands["app/src/commands/mod.rs#both_attributes"]
	check("both_attributes", len(c.Attributes) == 2 && c.Attributes[0].Path == "poise::command" && c.Attributes[1].Path == "tauri::command" && c.Attributes[1].Span.StartLine == c.Attributes[0].Span.StartLine+1)
	c = commands["app/src/commands/mod.rs#in_cfg_module"]
	check("in_cfg_module", c.Identity == "function:app/src/commands/mod.rs#gated.in_cfg_module" && !c.Cfg)
	// Review 2 (m4): cfg applied through cfg_attr, at any nesting depth.
	check("cfg_through_cfg_attr", commands["app/src/commands/mod.rs#cfg_through_cfg_attr"].Cfg)
	check("cfg_through_nested_cfg_attr", commands["app/src/commands/mod.rs#cfg_through_nested_cfg_attr"].Cfg)
	check("cfg_attr_without_cfg", !commands["app/src/commands/mod.rs#cfg_attr_without_cfg"].Cfg)
	for key, c := range commands {
		if len(c.Attributes) != 1 && key != "app/src/commands/mod.rs#both_attributes" {
			t.Error("attribute count", key, c.Attributes)
		}
	}

	// Handlers.
	handlers := func(file string) []facts.TauriHandler { return projection[file].Handlers }
	if len(handlers("app/src/negatives.rs")) != 0 || len(handlers("app/src/chains.rs")) != 6 || len(handlers("app/src/handlers.rs")) != 3 || len(handlers("app/src/plugin.rs")) != 2 || len(handlers("app/src/shapes.rs")) != 17 || len(handlers("app/src/rebinding.rs")) != 8 {
		t.Fatal("handler filter")
	}
	for file, p := range projection {
		for _, h := range p.Handlers {
			if text := string(atSpan(host(file), h.Span)); !strings.HasPrefix(text, h.Path+"!") {
				t.Error("handler span", file, text)
			}
			for _, e := range h.Entries {
				text := atSpan(host(file), e.Span)
				if string(text) != e.Path || e.Hash != contract.ContentHash(text) || strings.Join(e.Segments, "::") != e.Path {
					t.Error("entry location", file, e)
				}
			}
		}
	}
	chains := handlers("app/src/chains.rs")
	delimiters := ""
	for _, h := range chains {
		delimiters += strings.TrimSpace(string(atSpan(host("app/src/chains.rs"), h.Span)[len(h.Path)+1:]))[:1]
	}
	if delimiters != "[({[[[" {
		t.Error("delimiters", delimiters)
	}
	twice := chains[2]
	if len(twice.Entries) != 2 || twice.Context.Chain == nil || len(twice.Context.Chain.Methods) != 4 || *twice.Context.Chain.Methods[0].Handler != 1 || *twice.Context.Chain.Methods[1].Handler != 2 {
		t.Error("two invoke_handler calls on one chain", twice.Context)
	}
	direct := chains[0].Context
	if direct.Kind != "argument" || direct.Method != "invoke_handler" || direct.Chain.Root.Kind != "call" || direct.Chain.Root.Callee != "tauri::Builder::default" || len(direct.Chain.Methods) != 3 || direct.Chain.Methods[2].Name != "expect" {
		t.Error("direct chain", direct)
	}
	rebound := chains[3]
	if len(rebound.Entries) != 3 || !reflect.DeepEqual(rebound.Entries[1].Attributes, []string{"#[cfg(debug_assertions)]"}) || rebound.Entries[2].Path != "commands::arguments::r#type" || rebound.Context.Chain.Root.Name != "app" {
		t.Error("rebound handler", rebound)
	}
	empty, malformed, bound := handlers("app/src/handlers.rs")[0], handlers("app/src/handlers.rs")[1], handlers("app/src/handlers.rs")[2]
	if len(empty.Entries) != 0 || empty.Invalid != "" || len(malformed.Entries) != 0 || malformed.Invalid == "" || bound.Context.Kind != "let" || bound.Context.Binding != "handler" || bound.Context.Chain != nil {
		t.Error("handler forms", empty, malformed, bound)
	}
	plugin, scoped := handlers("app/src/plugin.rs")[0], handlers("app/src/plugin.rs")[1]
	if plugin.Plugin != "my_plugin" || plugin.Context.Chain.Root.Callee != "Builder::new" || !plugin.Context.Chain.Root.Generic || *plugin.Context.Chain.Root.Argument != "my-plugin" || plugin.Owner != "function:app/src/plugin.rs#init" {
		t.Error("plugin handler", plugin)
	}
	if scoped.Plugin != "" || scoped.Context.Chain.Root.Callee != "tauri::plugin::Builder::new" || scoped.Context.Chain.Root.Generic || *scoped.Context.Chain.Root.Argument != "x" {
		t.Error("scoped plugin handler", scoped)
	}

	// Flows.
	flows := map[string]facts.TauriFlow{}
	for _, f := range projection["app/src/chains.rs"].Flows {
		flows[f.Function] = f
	}
	if len(flows) != 3 {
		t.Fatal("flows", flows)
	}
	shape := func(f facts.TauriFlow) string {
		var parts []string
		for _, b := range f.Bindings {
			roots := []string{}
			for _, a := range b.Alternatives {
				roots = append(roots, a.Root.Kind+":"+a.Root.Name+a.Root.Callee)
			}
			parts = append(parts, fmt.Sprintf("%s %s %s cfg=%v complete=%v %v", b.Kind, b.Name, b.Value, b.Cfg, b.Complete, roots))
		}
		return strings.Join(parts, "; ")
	}
	for fn, want := range map[string]string{
		"function:app/src/chains.rs#rebound": "let builder plain cfg=false complete=true [call:tauri::Builder::default]; let builder if cfg=true complete=true [identifier:builder identifier:builder]; let app plain cfg=false complete=true [identifier:builder]; let app plain cfg=false complete=true [identifier:app]",
		"function:app/src/chains.rs#mutable": "let b plain cfg=false complete=true [call:tauri::Builder::default]; assign b plain cfg=false complete=true [identifier:b]; assign b plain cfg=false complete=true [identifier:b]",
		"function:app/src/chains.rs#matched": "let base plain cfg=false complete=true [call:tauri::Builder::default]; let chosen match cfg=false complete=false [identifier:base identifier:base]",
	} {
		if got := shape(flows[fn]); got != want {
			t.Errorf("flow %s:\n got %s\nwant %s", fn, got, want)
		}
	}
	if b := flows["function:app/src/chains.rs#mutable"].Bindings[0]; !b.Alternatives[0].Root.Generic {
		t.Error("turbofish root", b)
	}
	for fn, f := range flows {
		if f.Shadows == nil || len(f.Shadows) != 0 {
			t.Error("chains.rs flows have no shadows", fn, f.Shadows)
		}
	}

	// Review 1 (M1, M3): chain wrappers and how the chain's value is used.
	uses := []string{}
	for _, h := range chains {
		uses = append(uses, h.Context.Use)
	}
	if strings.Join(uses, " ") != "statement statement statement let assign statement" {
		t.Error("chains.rs uses", uses)
	}
	for _, h := range append(append([]facts.TauriHandler{}, chains...), handlers("app/src/plugin.rs")...) {
		if h.Context.Chain != nil && h.Context.Chain.Incomplete {
			t.Error("unexpected incomplete chain", h.Context)
		}
	}
	type shapeWant struct {
		use, root, methods string
		incomplete         bool
	}
	shapeOf := func(h facts.TauriHandler) shapeWant {
		ch := h.Context.Chain
		names := []string{}
		for _, m := range ch.Methods {
			name := m.Name
			if m.Handler != nil {
				name += fmt.Sprint("#", *m.Handler)
			}
			names = append(names, name)
		}
		return shapeWant{h.Context.Use, ch.Root.Kind + ":" + ch.Root.Callee + ch.Root.Name, strings.Join(names, " "), ch.Incomplete}
	}
	for i, want := range []shapeWant{
		{"statement", "call:tauri::Builder::default", "invoke_handler#0 invoke_handler#1 run unwrap", false},
		{"statement", "call:tauri::Builder::default", "invoke_handler#0 invoke_handler#1 run unwrap", false},
		{"statement", "call:tauri::Builder::default", "invoke_handler#2 checked invoke_handler#3 run", true},
		{"statement", "call:tauri::Builder::default", "invoke_handler#2 checked invoke_handler#3 run", true},
		{"statement", "call:make_async", "invoke_handler#4 run unwrap", true},
		{"tail", "call:tauri::Builder::default", "invoke_handler#5", false},
		{"statement", "call:make", "invoke_handler#6 run unwrap", false},
		{"return", "call:tauri::Builder::default", "invoke_handler#7", false},
		{"argument", "call:tauri::Builder::default", "invoke_handler#8", false},
		{"other", "call:tauri::Builder::default", "invoke_handler#9", true},
		{"statement", "identifier:builder", "invoke_handler#10 run unwrap", false},
		{"tail", "identifier:builder", "invoke_handler#11", false},
		{"other", "identifier:b", "invoke_handler#12", true},
		{"statement", "identifier:builder", "invoke_handler#13", false},
		{"statement", "identifier:builder", "invoke_handler#14", false},
		{"statement", "identifier:app", "invoke_handler#15", false},
		{"statement", "identifier:app", "invoke_handler#16", false},
	} {
		if got := shapeOf(handlers("app/src/shapes.rs")[i]); got != want {
			t.Errorf("shapes.rs handler %d: got %+v, want %+v", i, got, want)
		}
	}

	// Review 1 (M2): binders other than recorded lets that enclose a root site.
	shadows := map[string]string{}
	for _, f := range projection["app/src/shapes.rs"].Flows {
		shadows[f.Function] = strings.Join(f.Shadows, ",")
	}
	if !reflect.DeepEqual(shadows, map[string]string{
		"function:app/src/shapes.rs#if_let_shadow":       "builder",
		"function:app/src/shapes.rs#closure_shadow":      "builder",
		"function:app/src/shapes.rs#match_shadow":        "b",
		"function:app/src/shapes.rs#for_shadow":          "builder",
		"function:app/src/shapes.rs#destructured_shadow": "builder",
		"function:app/src/shapes.rs#param_shadow":        "builder",
		"function:app/src/shapes.rs#unrelated_closure":   "",
	}) {
		t.Error("shapes.rs shadows", shadows)
	}
	// Review 2 (M1, M2, m1, m2, m3): assignment targets, uncaptured
	// replacements, guard and macro shadows, cfg blocks and Conditional.
	rebinding := handlers("app/src/rebinding.rs")
	for i := 0; i < 2; i++ {
		if c := rebinding[i].Context; c.Use != "other" || !c.Chain.Incomplete {
			t.Error("non-local assignment target", i, c)
		}
	}
	review2 := map[string]facts.TauriFlow{}
	for _, f := range projection["app/src/rebinding.rs"].Flows {
		review2[strings.TrimPrefix(f.Function, "function:app/src/rebinding.rs#")] = f
	}
	for _, fn := range []string{"replaced_by_binding", "replaced_by_closure"} {
		f := review2[fn]
		if len(f.Bindings) != 1 || f.Bindings[0].Name != "app" || len(f.Calls) != 1 || f.Calls[0].Use != "statement" || f.Calls[0].Chain.Root.Name != "app" || !f.Calls[0].Chain.Methods[0].Uncaptured || f.Calls[0].Chain.Methods[0].Handler != nil {
			t.Error("uncaptured replacement", fn, f)
		}
	}
	for _, fn := range []string{"guard_shadow", "macro_shadow"} {
		if strings.Join(review2[fn].Shadows, ",") != "builder" {
			t.Error("shadows", fn, review2[fn].Shadows)
		}
	}
	gated := []string{}
	for _, b := range review2["gated_blocks"].Bindings {
		gated = append(gated, fmt.Sprintf("%s cfg=%v conditional=%v", b.Kind, b.Cfg, b.Conditional))
	}
	if strings.Join(gated, "; ") != "let cfg=false conditional=false; assign cfg=true conditional=true; let cfg=true conditional=false; let cfg=false conditional=false; assign cfg=false conditional=true" {
		t.Error("gated_blocks bindings", gated)
	}
	for fn, f := range flows {
		for _, b := range f.Bindings {
			if b.Conditional {
				t.Error("chains.rs bindings are top-level", fn, b)
			}
		}
		if len(f.Calls) != 1 {
			t.Error("chains.rs flow calls", fn, f.Calls)
		}
	}
	for file, p := range projection {
		if !p.Valid() {
			t.Error("invalid payload", file)
		}
	}

	// A CRLF copy produces identical payloads.
	crlf := t.TempDir()
	if err := os.CopyFS(crlf, os.DirFS("../../testdata/fixtures/tauri-command-syntax")); err != nil {
		t.Fatal(err)
	}
	if err := filepath.WalkDir(crlf, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".rs") {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(p, bytes.ReplaceAll(data, []byte("\n"), []byte("\r\n")), 0644)
	}); err != nil {
		t.Fatal(err)
	}
	other := open(t)
	emit(t, crlf, other, opt)
	if !reflect.DeepEqual(projection, tauriProjection(tauriPayloads(t, other))) {
		t.Fatal("CRLF changed Tauri payloads")
	}
}

// Review 2 (nit): a damaged or foreign Tauri cache entry is a cache miss; the
// file is re-extracted and its payload restored.
func TestTauriDamagedCacheEntry(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/tauri-command-syntax")); err != nil {
		t.Fatal(err)
	}
	s, opt := open(t), options(t)
	emit(t, root, s, opt)
	want := tauriProjection(tauriPayloads(t, s))
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	path, hash := "app/src/chains.rs", ""
	for _, f := range snapshot.Files {
		if f.Path == path {
			hash = f.ContentHash
		}
	}
	version := fmt.Sprintf("%s/max=%d", index.Version, 2<<20)
	damage := map[string]func(*facts.TauriSyntax){
		"version":         func(p *facts.TauriSyntax) { p.Version = facts.TauriSyntaxVersion + 1 },
		"handler index":   func(p *facts.TauriSyntax) { i := len(p.Handlers); p.Handlers[0].Context.Chain.Methods[0].Handler = &i },
		"missing shadows": func(p *facts.TauriSyntax) { p.Flows[0].Shadows = nil },
		"unmarked call":   func(p *facts.TauriSyntax) { p.Flows[0].Calls[0].Chain.Methods[1].Handler = nil },
	}
	for name, mutate := range damage {
		data, err := s.FactCache(context.Background(), "fixture", path, hash, version)
		if err != nil || len(data) == 0 {
			t.Fatal(name, err)
		}
		var cached facts.Facts
		if err := json.Unmarshal(data, &cached); err != nil || cached.Tauri == nil {
			t.Fatal(name, err)
		}
		mutate(cached.Tauri)
		broken, err := json.Marshal(cached)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.SaveFactCache(context.Background(), "fixture", path, hash, version, broken); err != nil {
			t.Fatal(err)
		}
		_, summary, _ := emit(t, root, s, opt)
		if summary.Parsed != 1 {
			t.Fatal("damaged Tauri payload replayed", name, summary)
		}
		if got := tauriProjection(tauriPayloads(t, s)); !reflect.DeepEqual(got, want) {
			t.Fatal("re-extracted payload differs", name)
		}
	}
}

func sameSet(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	seen := map[string]bool{}
	for _, g := range got {
		seen[g] = true
	}
	for _, w := range want {
		if !seen[w] {
			return false
		}
	}
	return true
}
