package tauri

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
)

// appMethods are the by-value inherent methods of tauri::Builder<R> that
// return Self (T11; app.rs:1694-2620 at tauri-v2.12.5, cfg-gated ones
// included). Inherent methods win over trait methods for a by-value
// receiver, so these names on a proven builder are Tauri's.
var appMethods = names("any_thread invoke_handler invoke_system channel_interceptor append_invoke_initialization_script setup on_page_load on_permission_request on_web_content_process_terminate plugin plugin_boxed manage menu on_menu_event on_tray_icon_event enable_macos_default_menu activate_ignoring_other_apps on_window_event on_webview_event register_uri_scheme_protocol register_asynchronous_uri_scheme_protocol device_event_filter")

// pluginMethods are the plugin builder's by-value setters returning Self
// (plugin.rs:279-757 at tauri-v2.12.5).
var pluginMethods = names("invoke_handler js_init_script js_init_script_on_all_frames setup on_navigation on_page_load on_window_ready on_webview_ready on_event on_drop register_uri_scheme_protocol register_asynchronous_uri_scheme_protocol")

// anyMethods is used while a root's kind is still unknown.
var anyMethods = union(appMethods, pluginMethods)

// consumers take the builder by value (T10): no later invoke_handler can
// reach it, so the handler installed last before them is final.
var appConsumers, pluginConsumers = names("build run"), names("build try_build")

// Macros a flow body may invoke without hiding a rebinding (G11, R3),
// verified at the locked versions in the T015c2 census. Unbound single
// segments are std/core/alloc prelude macros and builtins.
var (
	stdMacros     = names("format print println eprint eprintln write writeln format_args concat env option_env stringify include_str include_bytes line column file module_path cfg vec assert assert_eq assert_ne debug_assert debug_assert_eq debug_assert_ne panic unreachable todo unimplemented matches dbg")
	logMacros     = names("log error warn info debug trace")
	tracingMacros = names("error warn info debug trace event span trace_span debug_span info_span warn_span error_span")
	tauriMacros   = names("generate_handler generate_context")
	// Builtin attributes that never rewrite a function body (m3); other
	// attributes on a handler's function must resolve to a verified macro.
	builtinAttributes = names("cfg cfg_attr allow deny doc inline")
)

var plainBinding = regexp.MustCompile(`^(mut\s+)?[A-Za-z_][A-Za-z0-9_]*$`)

func names(list string) map[string]bool {
	out := map[string]bool{}
	for _, n := range strings.Fields(list) {
		out[n] = true
	}
	return out
}

func union(a, b map[string]bool) map[string]bool {
	out := map[string]bool{}
	for k := range a {
		out[k] = true
	}
	for k := range b {
		out[k] = true
	}
	return out
}

// registry is one G1-valid generate_handler! with its proof result. test is
// set for a test-scope file (ADR 0021) or a cfg(test)-only region; such a
// registry neither proves nor blocks the production app (Revision 2, M2).
// roots holds the constructor call spans its proof went through, so those
// calls are not counted as another app.
type registry struct {
	file, owner, identity string
	line                  int
	kind                  string // app, plugin or "" when unproven
	plugin, reason        string
	conditional           bool
	test                  bool
	entries               []entry
	acl                   bool
	roots                 []contract.Span
}

// mock reports a registry built on tauri::test::mock_builder, which hosts no
// webview: it neither proves nor blocks an app.
func (r *registry) mock() bool { return r.reason == "mock_builder" }

// entry is one ipc_command of a registry, for matching in declaration order.
// gated is set when the entry's own attributes apply a cfg (the match arm
// exists only in some builds); testOnly when the arm exists only in test
// builds (m2).
type entry struct {
	identity        string
	name            *string
	gated, testOnly bool
	cfgGated        bool
	// cfg is the normalized predicate P of an arm gated only by one
	// `#[cfg(P)]` entry attribute, for complementary pairs (Revision 3, mi1).
	cfg string
}

// proof is the outcome of a root or flow proof: a builder kind with its
// plugin name, or the reason it is unproven.
type proof struct {
	kind, plugin, reason string
}

func unproven(reason string) proof { return proof{reason: reason} }

// collectRegistries proves every handler (G1-G11) and emits its entries as
// ipc_command nodes.
func (l *linker) collectRegistries() {
	for _, file := range facts.Paths(l.in.Rust) {
		syntax := l.in.Rust[file].Tauri
		if syntax == nil {
			continue
		}
		duplicates := map[string]int{}
		for index, h := range syntax.Handlers {
			b := l.in.Resolver.Resolve(file, h.Scope, h.Path)
			if b.Ambiguous || !tauriPath(b.External, b.Name, "generate_handler") {
				// Another crate's macro passed to invoke_handler may build any
				// handler: an unproven handler of some builder (M2).
				if h.Context.Kind == "argument" && h.Context.Method == "invoke_handler" && !h.Test && !l.test(file) {
					l.foreign = append(l.foreign, appSite{kind: "invoke_handler", file: file, line: h.Span.StartLine, span: h.Span})
				}
				continue
			}
			r := &registry{file: file, owner: h.Owner, line: h.Span.StartLine, test: l.test(file)}
			l.registries = append(l.registries, r)
			if h.Invalid != "" {
				r.reason = "invalid"
				l.diagnostic(file, "warning", "tauri_handler_invalid", h.Span.StartLine, "generate_handler! is not the macro's input grammar: "+h.Invalid)
				continue
			}
			r.test = r.test || h.Test
			l.roots = nil
			p := l.proveHandler(file, syntax, index, h)
			r.kind, r.plugin, r.reason, r.roots = p.kind, p.plugin, p.reason, l.roots
			if r.kind == "" {
				l.diagnostic(file, "info", "tauri_registry_unproven", h.Span.StartLine, "registry not proven ["+p.reason+"]")
			} else {
				r.conditional = l.registryConditional(file, syntax, h)
			}
			l.emitCommands(file, h, r, duplicates)
		}
	}
}

// proveHandler applies G2-G11 and the plugin name rule to one handler.
func (l *linker) proveHandler(file string, syntax *facts.TauriSyntax, index int, h facts.TauriHandler) proof {
	c := h.Context
	if c.Kind != "argument" || c.Method != "invoke_handler" || c.Chain == nil {
		return unproven("context")
	}
	chain := *c.Chain
	if chain.Incomplete {
		return unproven("incomplete")
	}
	at := -1
	for i, m := range chain.Methods {
		if m.Handler != nil && *m.Handler == index {
			at = i
		}
	}
	if at < 0 {
		return unproven("context")
	}
	for _, m := range chain.Methods[at+1:] {
		if m.Name == "invoke_handler" {
			return unproven("not_last")
		}
	}
	root := l.root(file, h.Owner, syntax, chain.Root, map[string]bool{})
	methods, consumers := anyMethods, union(appConsumers, pluginConsumers)
	switch root.kind {
	case "app":
		methods, consumers = appMethods, appConsumers
	case "plugin":
		methods, consumers = pluginMethods, pluginConsumers
	}
	consumed := false
	for _, m := range chain.Methods[at+1:] {
		if methods[m.Name] {
			continue
		}
		consumed = consumers[m.Name]
		break
	}
	if !consumed {
		return unproven("not_consumed")
	}
	for _, m := range chain.Methods[:at] {
		if !methods[m.Name] {
			return unproven("method")
		}
	}
	if root.reason != "" {
		return root
	}
	if !l.attributesAllowed(file, h.Attributes) {
		return unproven("macro")
	}
	if h.Plugin != "" {
		if root.kind != "plugin" || strings.ReplaceAll(h.Plugin, "_", "-") != root.plugin {
			return unproven("plugin_name")
		}
	}
	return root
}

// root proves a chain root: a Tauri builder constructor call (G7) or an
// identifier traced through the owner's flow (G8-G11).
func (l *linker) root(file, owner string, syntax *facts.TauriSyntax, r facts.TauriRoot, visiting map[string]bool) proof {
	switch r.Kind {
	case "call":
		if r.Generic {
			return unproven("generic")
		}
		if r.Span == nil {
			return unproven("root")
		}
		b := l.in.Resolver.Resolve(file, l.in.Resolver.ScopeAt(file, *r.Span), r.Callee)
		if b.Ambiguous || b.External != "tauri" {
			return unproven("root")
		}
		switch b.Name {
		case "test::mock_builder":
			return unproven("mock_builder")
		case "Builder::default", "Builder::new":
			l.roots = append(l.roots, *r.Span)
			return proof{kind: "app"}
		case "plugin::Builder::new":
			if r.Argument != nil {
				return proof{kind: "plugin", plugin: *r.Argument}
			}
		}
		return unproven("root")
	case "identifier":
		var flow *facts.TauriFlow
		for i := range syntax.Flows {
			if syntax.Flows[i].Function == owner {
				flow = &syntax.Flows[i]
			}
		}
		if flow == nil {
			return unproven("flow_binding")
		}
		p := l.flowRoot(file, owner, syntax, flow, r.Name, r.Offset, visiting)
		if p.reason == "" && !l.macrosAllowed(file, flow) {
			return unproven("macro")
		}
		return p
	}
	return unproven("root")
}

// flowRoot resolves an identifier root through the flow bindings: the let in
// scope at the site (R1: only after its own statement), a cfg-gated let's
// shadowed binding, and every assignment between the let and the site. Each
// possible value must be a complete chain of builder methods with a proven
// root of one kind.
func (l *linker) flowRoot(file, owner string, syntax *facts.TauriSyntax, flow *facts.TauriFlow, name string, offset uint32, visiting map[string]bool) proof {
	key := fmt.Sprintf("%s@%d", name, offset)
	if visiting[key] {
		return unproven("flow_binding")
	}
	visiting[key] = true
	defer delete(visiting, key)
	for _, s := range flow.Shadows {
		if s == name {
			return unproven("shadowed")
		}
	}
	values, reason := l.values(file, flow, name, offset)
	if reason != "" {
		return unproven(reason)
	}
	var result proof
	mocks, set := 0, false
	for _, v := range values {
		if !plainBinding.MatchString(v.binding.Name) {
			return unproven("use_let_destructure")
		}
		if !v.binding.Complete || v.chain.Incomplete {
			return unproven("flow_binding")
		}
		p := l.root(file, owner, syntax, v.chain.Root, visiting)
		if p.reason == "mock_builder" {
			// Only a value that is a mock builder in every branch is a mock.
			mocks++
			continue
		}
		if p.reason != "" {
			return p
		}
		methods := appMethods
		if p.kind == "plugin" {
			methods = pluginMethods
		}
		for _, m := range v.chain.Methods {
			if !methods[m.Name] {
				return unproven("method")
			}
		}
		if set && p != result {
			return unproven("root")
		}
		result, set = p, true
	}
	switch {
	case mocks > 0 && !set:
		return unproven("mock_builder")
	case mocks > 0:
		return unproven("root")
	}
	return result
}

// value is one possible value of a name at a site, with its binding.
type value struct {
	binding facts.TauriBinding
	chain   facts.TauriChain
}

// values lists the possible values of name at offset (G8).
func (l *linker) values(file string, flow *facts.TauriFlow, name string, offset uint32) ([]value, string) {
	var let *facts.TauriBinding
	for i := range flow.Bindings {
		b := &flow.Bindings[i]
		if b.Name == name && b.Kind == "let" && b.End <= offset && l.contains(file, b.Block, offset) && (let == nil || b.Offset > let.Offset) {
			let = b
		}
	}
	if let == nil {
		return nil, "flow_binding"
	}
	var out []value
	add := func(b facts.TauriBinding) {
		if len(b.Alternatives) == 0 {
			out = append(out, value{binding: b, chain: facts.TauriChain{Incomplete: true}})
		}
		for _, a := range b.Alternatives {
			out = append(out, value{binding: b, chain: a})
		}
	}
	add(*let)
	if let.Cfg {
		// Without the cfg, the binding it shadows is the value.
		shadowed, reason := l.values(file, flow, name, let.Offset)
		if reason != "" {
			return nil, reason
		}
		out = append(out, shadowed...)
	}
	for _, b := range flow.Bindings {
		// An assignment's own right-hand side still sees the earlier value.
		if b.Name == name && b.Kind == "assign" && b.Offset > let.Offset && b.End <= offset {
			add(b)
		}
	}
	return out, ""
}

// macrosAllowed is G11: the flow body defines no macro_rules! and invokes
// only macros on the verified list, matched by resolved path (R3).
func (l *linker) macrosAllowed(file string, flow *facts.TauriFlow) bool {
	for _, m := range flow.Macros {
		if m.Kind != "invocation" {
			return false
		}
		b := l.in.Resolver.Resolve(file, m.Scope, m.Path)
		if b.Ambiguous || b.External == "" {
			return false
		}
		allowed := false
		switch {
		case b.Name == "":
			allowed = !strings.Contains(m.Path, "::") && b.External == m.Path && stdMacros[m.Path]
		case b.External == "std" || b.External == "core" || b.External == "alloc":
			allowed = stdMacros[b.Name]
		case b.External == "log":
			allowed = logMacros[b.Name]
		case b.External == "tracing":
			allowed = tracingMacros[b.Name]
		case b.External == "tauri" || b.External == "tauri_macros":
			allowed = tauriMacros[b.Name]
		}
		if !allowed {
			return false
		}
	}
	return true
}

// attributesAllowed is G11 for the handler's function attributes (m3): a
// builtin attribute that does not rewrite the body, tokio::main or
// tauri::mobile_entry_point (both emit the body unchanged at the locked
// versions: tokio-macros 2.7.2 entry.rs wraps it in `async`, tauri-macros
// 2.7.4 mobile.rs re-emits the item). Any other attribute may be a macro
// that rewrites the body, so the registry is unproven.
func (l *linker) attributesAllowed(file string, attrs []facts.TauriMacro) bool {
	for _, a := range attrs {
		b := l.in.Resolver.Resolve(file, a.Scope, a.Path)
		switch {
		case b.Ambiguous:
			return false
		case b.Name == "" && b.External == a.Path && builtinAttributes[a.Path]:
		case (b.External == "tokio" || b.External == "tokio_macros") && b.Name == "main":
		case (b.External == "tauri" || b.External == "tauri_macros") && b.Name == "mobile_entry_point":
		default:
			return false
		}
	}
	return true
}

// registryConditional: the owner function is cfg-dependent, or the consumed
// chain's own let carries a cfg.
func (l *linker) registryConditional(file string, syntax *facts.TauriSyntax, h facts.TauriHandler) bool {
	if h.Owner != facts.ID("file", file, file) && l.functionConditional(file, h.Owner) {
		return true
	}
	c := h.Context
	if c.Chain == nil || c.Use != "let" && c.Use != "assign" {
		return false
	}
	for _, f := range syntax.Flows {
		if f.Function != h.Owner {
			continue
		}
		for _, b := range f.Bindings {
			if b.Kind == c.Use && b.Offset <= c.Chain.Root.Offset && c.Chain.Root.Offset < b.End && b.Cfg {
				return true
			}
		}
	}
	return false
}

// emitCommands writes one ipc_command per entry with its contains and
// exposes_command edges. Tauri emits each entry's attributes on its own match
// arm (tauri-macros handler.rs), so a cfg-gated arm does not hide a later arm
// of the same name: an entry is unreachable only when an earlier entry of
// that name exists in every build that has this one (not cfg-gated, not
// test-only; Revision 2, M1).
func (l *linker) emitCommands(file string, h facts.TauriHandler, r *registry, duplicates map[string]int) {
	ownerName := ""
	for _, n := range l.in.Rust[file].Nodes {
		if n.Identity == h.Owner {
			ownerName = n.QualifiedName
		}
	}
	closed := map[string]bool{}
	predicates := map[string][]string{}
	for _, e := range h.Entries {
		segment := e.Segments[len(e.Segments)-1]
		name := "COMMAND " + segment
		qualified := facts.Join(ownerName, name)
		duplicates[qualified]++
		if n := duplicates[qualified]; n > 1 {
			qualified += fmt.Sprintf("@%d", n)
		}
		identity := facts.ID("ipc_command", file, qualified)
		var invoke *string
		handler := ""
		b := l.in.Resolver.Resolve(file, h.Scope, e.Path)
		cmd, ok := l.commands[b.Identity]
		if b.Ambiguous || b.Identity == "" || !ok {
			l.diagnostic(file, "info", "tauri_entry_unresolved", e.Span.StartLine, "entry "+e.Path+" does not resolve to a Tauri command")
		} else {
			invoke, handler = cmd.name, b.Identity
		}
		gated, testOnly := false, cmd.test || h.Test
		for _, a := range e.Attributes {
			gated = gated || facts.CfgApplies(a)
			testOnly = testOnly || facts.CfgTestOnly(a)
		}
		cfg := gated || cmd.conditional || r.conditional
		attrs := map[string]any{"framework": "tauri", "transport": "tauri_ipc", "command": nullable(invoke), "handler": nil, "registry": "unproven", "cfg_gated": cfg}
		if handler != "" {
			attrs["handler"] = handler
		}
		if r.kind != "" {
			attrs["registry"] = r.kind
		} else {
			attrs["reason"] = r.reason
		}
		if r.kind == "plugin" {
			attrs["plugin"] = r.plugin
		}
		if testOnly {
			attrs["test_only"] = true
		}
		predicate := ""
		if !cmd.conditional && !cmd.test {
			predicate = armPredicate(e.Attributes)
		}
		if invoke != nil {
			if closed[*invoke] {
				attrs["unreachable"] = true
				l.diagnostic(file, "info", "tauri_command_duplicate", e.Span.StartLine, "entry "+e.Path+" repeats command "+*invoke+"; the first entry wins")
			} else if !gated && !testOnly || complements(predicates[*invoke], predicate) {
				// An arm without a cfg, or the second arm of a P / not(P)
				// pair, exists in every build (mi1).
				closed[*invoke] = true
			}
			if predicate != "" {
				predicates[*invoke] = append(predicates[*invoke], predicate)
			}
		}
		if l.test(file) {
			attrs["scope"] = "test"
		}
		span := e.Span
		l.nodes = append(l.nodes, contract.Node{Type: "node", Kind: "ipc_command", Level: 4, Identity: identity, Name: name, QualifiedName: qualified, File: file, Span: &span, ContentHash: e.Hash, Attributes: attrs})
		l.edges = append(l.edges, facts.Edge("contains", h.Owner, identity, file, span))
		if handler != "" {
			l.edges = append(l.edges, facts.Edge("exposes_command", handler, identity, file, span))
		}
		_, unreachable := attrs["unreachable"]
		if !unreachable {
			r.entries = append(r.entries, entry{identity: identity, name: invoke, gated: gated, testOnly: testOnly, cfgGated: cfg, cfg: predicate})
		}
	}
}

// armPredicate returns the predicate P of an arm whose only cfg is one
// `#[cfg(P)]` entry attribute, with whitespace outside string literals
// removed, or "" (no cfg, several, or one applied through cfg_attr).
func armPredicate(attributes []string) string {
	predicate := ""
	for _, a := range attributes {
		if !facts.CfgApplies(a) {
			continue
		}
		inner, ok := strings.CutPrefix(stripSpaces(a), "#[cfg(")
		if predicate != "" || !ok || !strings.HasSuffix(inner, ")]") {
			return ""
		}
		predicate = strings.TrimSuffix(inner, ")]")
	}
	return predicate
}

// complements reports whether predicate is not(P) for some earlier P, or P
// for an earlier not(P): together the two arms cover every build. General
// cfg satisfiability is not evaluated; only these exact pairs are recognized.
func complements(earlier []string, predicate string) bool {
	if predicate == "" {
		return false
	}
	for _, p := range earlier {
		if predicate == "not("+p+")" || p == "not("+predicate+")" {
			return true
		}
	}
	return false
}

// stripSpaces removes whitespace outside Rust string literals, so
// `feature = "a"` and `feature="a"` compare equal but `"a b"` and `"ab"` do
// not.
func stripSpaces(s string) string {
	var b strings.Builder
	quoted := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quoted && c == '\\' && i+1 < len(s):
			b.WriteByte(c)
			i++
			c = s[i]
		case c == '"':
			quoted = !quoted
		case !quoted && (c == ' ' || c == '\t' || c == '\n' || c == '\r'):
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// appScope is the repository-level host decision (A1, A2).
type appScope struct {
	app       *registry
	apps      []*registry
	unproven  bool
	ambiguous bool
}

// scopeApp applies A1 and A2. Registries in test scope (ADR 0021 or a
// cfg(test)-only region) and mock_builder registries neither prove nor block
// the production app. The production scope needs exactly one proven app
// registry, no unproven handler, no invoke_handler call with another
// argument, and no other Tauri app evidence (otherBuilds); A2 then checks
// ACL removal in the app crate. Only when the production scope has no Tauri
// app syntax at all are test-scope registries matched, so test calls can
// still link to an app built only in tests (I5).
func (l *linker) scopeApp() {
	var production, test []*registry
	for _, r := range l.registries {
		switch {
		case r.mock():
		case r.test:
			test = append(test, r)
		default:
			production = append(production, r)
		}
	}
	sites := append(l.appSites(), l.foreign...)
	scope := production
	if len(production) == 0 && len(sites) == 0 {
		scope = test
	}
	for _, r := range scope {
		switch r.kind {
		case "":
			l.app.unproven = true
		case "app":
			l.app.apps = append(l.app.apps, r)
			r.acl = l.aclPossible(r.file)
			if r.acl {
				l.diagnostic(r.file, "info", "tauri_acl_possible", r.line, "the app crate has permissions files or an app manifest; build-time command removal is possible")
			}
		}
	}
	for _, s := range sites {
		if s.kind == "invoke_handler" {
			// A handler that is not generate_handler! (a closure, a binding)
			// is an unproven handler of some builder.
			l.app.unproven = true
			l.diagnostic(s.file, "info", "tauri_registry_unproven", s.line, "invoke_handler argument is not Tauri's generate_handler! [handler]")
		}
	}
	if len(l.app.apps) > 1 {
		l.app.ambiguous = true
		for _, r := range l.app.apps {
			l.diagnostic(r.file, "info", "tauri_app_ambiguous", r.line, fmt.Sprintf("%d proven app registries in the repository; app invokes are not linked", len(l.app.apps)))
		}
	}
	if len(l.app.apps) != 1 {
		return
	}
	app := l.app.apps[0]
	if others := l.otherApps(app, sites); len(others) > 0 {
		l.app.ambiguous = true
		l.diagnostic(app.file, "info", "tauri_app_ambiguous", app.line, fmt.Sprintf("%d other Tauri app sites in the repository; app invokes are not linked", len(others)))
		for _, s := range others {
			l.diagnostic(s.file, "info", "tauri_app_ambiguous", s.line, "one of several Tauri app sites ["+s.kind+"]")
		}
		return
	}
	l.app.app = app
}

// appSite is production syntax that may build a Tauri app (M2): a resolved
// tauri Builder constructor, an invoke_handler call without generate_handler!,
// a resolved generate_context! invocation, or a Tauri config file.
type appSite struct {
	kind, file string
	line       int
	span       contract.Span
}

// configFiles are the main Tauri config names (tauri-utils config/parse.rs
// at tauri-utils-v2.10.2: tauri.conf.json, then tauri.conf.json5, then
// Tauri.toml in one directory). Platform files (tauri.linux.conf.json and
// the like) are overlays merged into the main one, not separate apps.
var configFiles = names("tauri.conf.json tauri.conf.json5 Tauri.toml")

// appSites lists the production app sites of the repository.
func (l *linker) appSites() []appSite {
	var out []appSite
	seen := map[string]bool{}
	for _, file := range facts.Paths(l.in.Rust) {
		syntax := l.in.Rust[file].Tauri
		if syntax == nil || l.test(file) {
			continue
		}
		for _, a := range syntax.Apps {
			seen[file+"#"+spanKey(a.Span)] = true
			if a.Test {
				continue
			}
			site := appSite{kind: a.Kind, file: file, line: a.Span.StartLine, span: a.Span}
			switch a.Kind {
			case "invoke_handler":
				out = append(out, site)
			case "root", "typed", "alias", "macro":
				b := l.in.Resolver.Resolve(file, a.Scope, a.Path)
				if b.Ambiguous || tauriBuilder(b.External, b.Name) {
					out = append(out, site)
				}
			case "reexport":
				if base, glob := strings.CutSuffix(a.Path, "::*"); glob {
					b := l.in.Resolver.Resolve(file, a.Scope, base)
					if b.Ambiguous || b.External == "tauri" && (b.Name == "" || b.Name == "app") {
						out = append(out, site)
					}
				} else if b := l.in.Resolver.Resolve(file, a.Scope, a.Path); b.Ambiguous || tauriBuilder(b.External, b.Name) {
					out = append(out, site)
				}
			case "context":
				b := l.in.Resolver.Resolve(file, a.Scope, a.Path)
				if b.Ambiguous || tauriPath(b.External, b.Name, "generate_context") {
					out = append(out, site)
				}
			}
		}
	}
	// Calls the payload filter did not keep (a callee renamed through a use
	// chain across modules, `use super::B as C; C::new()`) still resolve
	// through the Rust call cache. Their cfg(test) regions are not known, so
	// they always count (under-claim only). An ambiguous callee of this kind
	// is not counted (a known gap).
	for _, file := range facts.Paths(l.in.Rust) {
		if l.test(file) {
			continue
		}
		for _, call := range l.in.Rust[file].Calls {
			name := plainPath(call.Name)
			last := lastSegment(name)
			if call.Shadowed || last != "default" && last != "new" || seen[file+"#"+spanKey(call.Span)] {
				continue
			}
			b := l.in.Resolver.Resolve(file, l.in.Resolver.ScopeAt(file, call.Span), name)
			if !b.Ambiguous && tauriBuilder(b.External, strings.TrimSuffix(b.Name, "::"+last)) {
				out = append(out, appSite{kind: "root", file: file, line: call.Span.StartLine, span: call.Span})
			}
		}
	}
	dirs := map[string]bool{}
	var configs []appSite
	for p := range l.in.Sources {
		if configFiles[path.Base(p)] && !l.test(p) {
			configs = append(configs, appSite{kind: "config", file: p, line: 1})
			dirs[path.Dir(p)] = true
		}
	}
	if len(dirs) > 1 {
		sort.Slice(configs, func(i, j int) bool { return configs[i].file < configs[j].file })
		out = append(out, configs...)
	}
	return out
}

// otherApps returns the sites that are not part of the proven app: builder
// references its proof did not go through (Revision 3: every typed, alias,
// re-export and macro site), every generate_context! when there is
// more than one, and config files of more than one directory (appSites keeps
// them only then). invoke_handler sites are handled as unproven handlers.
func (l *linker) otherApps(app *registry, sites []appSite) []appSite {
	var out, contexts []appSite
	for _, s := range sites {
		switch s.kind {
		case "root":
			own := false
			for _, r := range app.roots {
				own = own || s.file == app.file && r == s.span
			}
			if !own {
				out = append(out, s)
			}
		case "context":
			contexts = append(contexts, s)
		case "config", "typed", "alias", "reexport", "macro":
			// Never part of a proof: a proof root is a direct constructor
			// call.
			out = append(out, s)
		}
	}
	if len(contexts) > 1 {
		out = append(out, contexts...)
	}
	return out
}

// aclPossible is A2: the registry's Cargo package (nearest enclosing
// Cargo.toml with [package], R4) has a scanned file under permissions/, or
// its build.rs calls app_manifest or AppManifest::new. A registry outside any
// package is treated as possibly removed.
func (l *linker) aclPossible(file string) bool {
	dir, ok := l.crateDir(file)
	if !ok {
		return true
	}
	prefix := path.Join(dir, "permissions") + "/"
	for p := range l.in.Sources {
		if strings.HasPrefix(p, prefix) {
			return true
		}
	}
	for _, c := range l.in.Rust[path.Join(dir, "build.rs")].Calls {
		if lastSegment(c.Name) == "app_manifest" || c.StaticName == "app_manifest" || strings.HasSuffix(c.Name, "AppManifest::new") {
			return true
		}
	}
	return false
}

func (l *linker) crateDir(file string) (string, bool) {
	for dir := path.Dir(file); ; dir = path.Dir(dir) {
		if source, ok := l.in.Sources[path.Join(dir, "Cargo.toml")]; ok && packageManifest(source) {
			return dir, true
		}
		if dir == "." || dir == "/" || dir == "" {
			return "", false
		}
	}
}

func packageManifest(source []byte) bool {
	for _, line := range strings.Split(string(source), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "[package]") {
			return true
		}
	}
	return false
}

// tauriBuilder reports whether a resolved external path is Tauri's Builder
// type (re-exported at the crate root from tauri::app).
func tauriBuilder(external, name string) bool {
	return external == "tauri" && (name == "Builder" || name == "app::Builder")
}

func spanKey(s contract.Span) string {
	return fmt.Sprintf("%d:%d-%d:%d", s.StartLine, s.StartColumn, s.EndLine, s.EndColumn)
}

// plainPath removes generic arguments and qualified-path brackets from a
// callee text: `tauri::Builder::<R>::new` and `<tauri::Builder<R>>::new`
// become `tauri::Builder::new`; `<T as Tr>::f` becomes `T::f`.
func plainPath(text string) string {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "<") {
		depth := 0
		for i := 0; i < len(text); i++ {
			if text[i] == '<' {
				depth++
			} else if text[i] == '>' {
				if depth--; depth == 0 {
					inner := text[1:i]
					if k := topLevelAs(inner); k >= 0 {
						inner = inner[:k]
					}
					text = inner + text[i+1:]
					break
				}
			}
		}
	}
	var b strings.Builder
	depth := 0
	for i := 0; i < len(text); i++ {
		switch c := text[i]; {
		case c == '<':
			depth++
		case c == '>':
			depth--
		case depth == 0 && c != ' ' && c != '\t' && c != '\n' && c != '\r':
			b.WriteByte(c)
		}
	}
	return strings.ReplaceAll(b.String(), "::::", "::")
}

// topLevelAs returns the index of ` as ` at generic depth 0 in the inside of
// a qualified path, or -1.
func topLevelAs(inner string) int {
	depth := 0
	for i := 0; i < len(inner); i++ {
		switch inner[i] {
		case '<':
			depth++
		case '>':
			depth--
		case ' ', '\t', '\n', '\r':
			if depth == 0 && strings.HasPrefix(strings.TrimLeft(inner[i:], " \t\n\r"), "as") {
				rest := strings.TrimLeft(inner[i:], " \t\n\r")[2:]
				if rest != "" && (rest[0] == ' ' || rest[0] == '\t' || rest[0] == '\n' || rest[0] == '\r') {
					return i
				}
			}
		}
	}
	return -1
}

func lastSegment(p string) string {
	parts := strings.Split(p, "::")
	return parts[len(parts)-1]
}
