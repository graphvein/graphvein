package index_test

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/store"
)

// ipcProjection keeps the Tauri IPC records of a snapshot: ipc_command and
// ipc_call nodes, exposes_command and invokes_command edges, contains edges
// into IPC nodes and tauri_* diagnostics.
type ipcProjection struct {
	Nodes       []contract.Node
	Edges       []contract.Edge
	Diagnostics []contract.Diagnostic
}

func projectIPC(snapshot store.Snapshot) ipcProjection {
	p := ipcProjection{Nodes: []contract.Node{}, Edges: []contract.Edge{}, Diagnostics: []contract.Diagnostic{}}
	for _, n := range snapshot.Nodes {
		if n.Kind == "ipc_command" || n.Kind == "ipc_call" {
			p.Nodes = append(p.Nodes, n)
		}
	}
	for _, e := range snapshot.Edges {
		if e.Kind == "exposes_command" || e.Kind == "invokes_command" || e.Kind == "contains" && (strings.HasPrefix(e.Target, "ipc_command:") || strings.HasPrefix(e.Target, "ipc_call:")) {
			p.Edges = append(p.Edges, e)
		}
	}
	for _, d := range snapshot.Diagnostics {
		if strings.HasPrefix(d.Code, "tauri_") {
			p.Diagnostics = append(p.Diagnostics, d)
		}
	}
	return p
}

func dumpIPC(t *testing.T, s store.Store) ipcProjection {
	t.Helper()
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	return projectIPC(snapshot)
}

// ipcScenario copies the fixture and keeps only the named top-level
// directories (README). Each scenario is a separate repository because app
// scope is decided per repository (A1).
func ipcScenario(t *testing.T, dirs ...string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/tauri-invoke")); err != nil {
		t.Fatal(err)
	}
	keep := map[string]bool{}
	for _, d := range dirs {
		keep[d] = true
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !keep[e.Name()] {
			if err := os.RemoveAll(filepath.Join(root, e.Name())); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Inert name (README): the shadowing package manifest is stored as
	// _package.json, so it never shadows @tauri-apps/api for the Graphvein
	// repository itself, and is restored only in the shadowed scenario.
	if keep["shadowed"] {
		dir := filepath.Join(root, "shadowed/packages/tauri-api")
		if err := os.Rename(filepath.Join(dir, "_package.json"), filepath.Join(dir, "package.json")); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// wantPath maps a short "file:line" to its path under second-bin/src.
func wantPath(short string) string {
	if strings.HasPrefix(short, "lib.rs") {
		return short
	}
	return "bin/" + short
}

func ipcIndex(t *testing.T, root string) (ipcProjection, store.Store, []byte) {
	t.Helper()
	s := open(t)
	data, _, _ := emit(t, root, s, options(t))
	return dumpIPC(t, s), s, data
}

func ipcNodes(p ipcProjection) map[string]contract.Node {
	out := map[string]contract.Node{}
	for _, n := range p.Nodes {
		out[n.Identity] = n
	}
	return out
}

func ipcEdges(p ipcProjection, kind string) []string {
	out := []string{}
	for _, e := range p.Edges {
		if e.Kind == kind {
			out = append(out, e.Source+" -> "+e.Target)
		}
	}
	sort.Strings(out)
	return out
}

func ipcDiagnostics(p ipcProjection) []string {
	out := []string{}
	for _, d := range p.Diagnostics {
		out = append(out, d.Code+" "+d.Message)
	}
	sort.Strings(out)
	return out
}

func ipcStatuses(p ipcProjection) map[string]string {
	out := map[string]string{}
	for _, n := range p.Nodes {
		if n.Kind == "ipc_call" {
			out[strings.TrimPrefix(n.Identity, "ipc_call:")] = n.Attributes["status"].(string)
		}
	}
	return out
}

const (
	appMain    = "ipc_command:app/src/main.rs#main.COMMAND%20"
	auditCmd   = "ipc_command:app/src/plugins.rs#audit.COMMAND%20"
	namedCmd   = "ipc_command:app/src/plugins.rs#named.COMMAND%20"
	commandsRs = "function:app/src/commands.rs#"
)

func TestTauriInvoke(t *testing.T) {
	linkedRoot := ipcScenario(t, "app", "frontend")
	linked, s, first := ipcIndex(t, linkedRoot)
	registries, _, _ := ipcIndex(t, ipcScenario(t, "registries"))
	testOnly, _, _ := ipcIndex(t, ipcScenario(t, "test-only"))
	// Revision 3 (review 2 probes): wrapper lexer limits (q1, q2),
	// complementary cfg arms (e1) and second apps in one crate (b0-bl).
	unsure, _, _ := ipcIndex(t, ipcScenario(t, "unsure"))
	pairs, _, _ := ipcIndex(t, ipcScenario(t, "pairs"))
	secondBin, _, _ := ipcIndex(t, ipcScenario(t, "second-bin"))
	golden.Check(t, "../../testdata/fixtures/tauri-invoke.golden.json", map[string]ipcProjection{"linked": linked, "registries": registries, "test-only": testOnly, "unsure": unsure, "pairs": pairs, "second-bin": secondBin})
	if got := ipcEdges(unsure, "invokes_command"); !reflect.DeepEqual(got, []string{"ipc_call:unsure/web/main.ts#c2.INVOKE%20%27greet%27 -> ipc_command:unsure/app/src/main.rs#main.COMMAND%20greet"}) {
		t.Errorf("wrapper lexer: only u2 is a wrapper: %v", got)
	}
	for site, want := range ipcStatuses(unsure) {
		if want != "dynamic" && site != "unsure/web/main.ts#c2.INVOKE%20%27greet%27" && site != "unsure/web/main.ts#u2.INVOKE%20cmd" {
			t.Errorf("wrapper lexer site %s: %s", site, want)
		}
	}
	if got := ipcEdges(pairs, "invokes_command"); len(got) != 2 || ipcNodes(pairs)["ipc_command:pairs/app/src/main.rs#main.COMMAND%20greet"].Attributes["unreachable"] != true {
		t.Errorf("complementary arms: %v", got)
	}
	if got := ipcStatuses(secondBin)["second-bin/web/main.ts#a.INVOKE%20%27greet%27"]; got != "ambiguous" || len(ipcEdges(secondBin, "invokes_command")) != 0 {
		t.Errorf("second app in the crate: %s", got)
	}
	secondSites := strings.Join(ipcDiagnostics(secondBin), "\n")
	for _, want := range []string{"b0.rs:2", "ba.rs:2", "bb.rs:2", "bc.rs:3", "bd.rs:2", "be.rs:2", "bg.rs:2", "bi.rs:2", "bj.rs:2", "bk.rs:3", "bl.rs:3", "lib.rs:4"} {
		if !strings.Contains(secondSites, "second-bin/src/"+wantPath(want)) {
			t.Errorf("second app site %s missing in\n%s", want, secondSites)
		}
	}

	for _, p := range []ipcProjection{linked, registries, testOnly} {
		for _, n := range p.Nodes {
			if n.Level != 4 || n.Span == nil || !strings.HasPrefix(n.ContentHash, "sha256:") || n.Attributes["framework"] != "tauri" || n.Attributes["transport"] != "tauri_ipc" {
				t.Errorf("node shape %s", n.Identity)
			}
			checkNoControl(t, n.Identity, n.Attributes)
		}
		for _, e := range p.Edges {
			if e.Provenance != "fact" || e.Confidence != 1 || len(e.Evidence) != 1 {
				t.Errorf("edge shape %+v", e)
			}
		}
	}

	host := func(root, file string) []byte {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(root, file))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}

	// Commands: names, cfg marks and the registries that expose them.
	nodes := ipcNodes(linked)
	type commandWant struct {
		command, handler, registry string
		cfg, unreachable, testOnly bool
	}
	for identity, want := range map[string]commandWant{
		appMain + "greet":         {"greet", commandsRs + "greet", "app", false, false, false},
		appMain + "bare_cmd":      {"bare_cmd", "function:app/src/bare.rs#bare_cmd", "app", false, false, false},
		appMain + "aliased":       {"aliased", commandsRs + "aliased", "app", false, false, false},
		appMain + "desktop_only":  {"desktop_only", commandsRs + "desktop_only", "app", true, false, false},
		appMain + "rename_me":     {"renamed", commandsRs + "rename_me", "app", false, false, false},
		appMain + "debug_cmd":     {"debug_cmd", commandsRs + "debug_cmd", "app", true, false, false},
		appMain + "greet%402":     {"greet", commandsRs + "greet", "app", false, true, false},
		appMain + "test_cmd":      {"test_cmd", "function:app/src/commands.rs#testing.test_cmd", "app", true, false, true},
		appMain + "legacy_status": {"status", commandsRs + "legacy_status", "app", true, false, false},
		appMain + "status":        {"status", commandsRs + "status", "app", false, false, false},
		auditCmd + "greet":        {"greet", commandsRs + "greet", "plugin", false, false, false},
		auditCmd + "record":       {"record", "function:app/src/plugins.rs#record", "plugin", false, false, false},
		namedCmd + "dynamic_name": {"", commandsRs + "dynamic_name", "plugin", false, false, false},
		namedCmd + "twice":        {"", "", "plugin", false, false, false},
		namedCmd + "poise_cmd":    {"", "", "plugin", false, false, false},
		namedCmd + "clap_like":    {"", "", "plugin", false, false, false},
	} {
		n, ok := nodes[identity]
		if !ok {
			t.Errorf("missing %s", identity)
			continue
		}
		a := n.Attributes
		command, _ := a["command"].(string)
		handler, _ := a["handler"].(string)
		unreachable, _ := a["unreachable"].(bool)
		testOnly, _ := a["test_only"].(bool)
		if command != want.command || handler != want.handler || a["registry"] != want.registry || a["cfg_gated"] != want.cfg || unreachable != want.unreachable || testOnly != want.testOnly {
			t.Errorf("command %s: %v", identity, a)
		}
		if _, has := a["command"]; !has {
			t.Errorf("command attribute must be present (null when unknown): %s", identity)
		}
		segment := strings.Split(strings.TrimPrefix(n.Name, "COMMAND "), "@")[0]
		if text := atSpan(host(linkedRoot, n.File), *n.Span); n.ContentHash != contract.ContentHash(text) || !strings.HasSuffix(string(text), "::"+segment) && string(text) != segment {
			t.Errorf("entry span %s: %q", identity, text)
		}
	}
	for _, n := range linked.Nodes {
		if n.Kind == "ipc_command" && strings.HasPrefix(n.Identity, "ipc_command:app/src/plugins.rs#") {
			want := map[string]bool{"audit": true, "a-b": true}
			if !want[n.Attributes["plugin"].(string)] {
				t.Errorf("plugin name %s: %v", n.Identity, n.Attributes)
			}
		}
	}
	if got := ipcEdges(linked, "exposes_command"); len(got) != 13 {
		t.Errorf("exposes_command edges %d: %v", len(got), got)
	}

	// Invokes: forms, statuses and targets.
	statuses := ipcStatuses(linked)
	for site, want := range map[string]string{
		"frontend/src/app.ts#named.INVOKE%20%27greet%27":                              "linked",
		"frontend/src/app.ts#aliased.INVOKE%20%27bare_cmd%27":                         "linked",
		"frontend/src/app.ts#namespaced.INVOKE%20%27aliased%27":                       "linked",
		"frontend/src/app.ts#coreMember.INVOKE%20%27renamed%27":                       "linked",
		"frontend/src/app.ts#viaWrapper.INVOKE%20%27desktop_only%27":                  "linked",
		"frontend/src/app.ts#viaWrapper.INVOKE%20%27debug_cmd%27":                     "linked",
		"frontend/src/app.ts#twoLevel.INVOKE%20%27test_cmd%27":                        "test_only",
		"frontend/src/app.ts#status.INVOKE%20%27status%27":                            "linked",
		"frontend/src/app.test.ts#testCommand.INVOKE%20%27test_cmd%27":                "linked",
		"frontend/src/writes.ts#destructured.INVOKE%20command":                        "dynamic",
		"frontend/src/writes.ts#objectPattern.INVOKE%20command":                       "dynamic",
		"frontend/src/writes.ts#updated.INVOKE%20command":                             "dynamic",
		"frontend/src/writes.ts#closure.INVOKE%20command":                             "dynamic",
		"frontend/src/writes.ts#forOf.INVOKE%20command":                               "dynamic",
		"frontend/src/writes.ts#logged.INVOKE%20command":                              "wrapper",
		"frontend/src/writes.ts#useWrites.INVOKE%20%27greet%27":                       "linked",
		"frontend/src/app.ts#conditional.INVOKE%20flag%3F%27greet%27%3A%27renamed%27": "linked",
		"frontend/src/app.ts#dynamicName.INVOKE%20names%5B0%5D":                       "dynamic",
		"frontend/src/app.ts#unregistered.INVOKE%20%27missing_cmd%27":                 "unmatched",
		"frontend/src/app.ts#plugins.INVOKE%20%27plugin%3Afs%7Cread_file%27":          "plugin",
		"frontend/src/app.ts#plugins.INVOKE%20%27plugin%3Ax%27":                       "plugin",
		"frontend/src/lib/ipc.ts#call.INVOKE%20command":                               "wrapper",
		"frontend/src/lib/relay.ts#relay.INVOKE%20name":                               "wrapper",
		"frontend/src/local.ts#send.INVOKE%20command":                                 "wrapper",
		"frontend/src/local.ts#INVOKE%20%27greet%27":                                  "linked",
		"frontend/src/local.ts#INVOKE%20%27greet%27%402":                              "linked",
		"frontend/src/local.ts#INVOKE%20%27greet%27%403":                              "linked",
		"frontend/src/global.js#INVOKE%20%27greet%27":                                 "global_unlinked",
		"frontend/src/global.js#INVOKE%20name":                                        "dynamic",
		"frontend/src/app.test.ts#fromTest.INVOKE%20%27greet%27":                      "linked",
	} {
		if statuses[site] != want {
			t.Errorf("status %s: got %q, want %q", site, statuses[site], want)
		}
		delete(statuses, site)
	}
	// Type-only, v1 (@tauri-apps/api/tauri) and shadowed wrapper calls are not invokes.
	if len(statuses) != 0 {
		t.Errorf("unexpected ipc_call nodes: %v", statuses)
	}
	call := func(site string) map[string]any { return nodes["ipc_call:"+site].Attributes }
	if a := call("frontend/src/app.ts#viaWrapper.INVOKE%20%27desktop_only%27"); a["form"] != "wrapper" || a["wrapper_identity"] != "function:frontend/src/lib/ipc.ts#call" || a["command"] != "desktop_only" || a["conditional"] != false {
		t.Errorf("imported wrapper: %v", a)
	}
	if a := call("frontend/src/app.ts#twoLevel.INVOKE%20%27test_cmd%27"); a["wrapper_identity"] != "function:frontend/src/lib/relay.ts#relay" {
		t.Errorf("two-level wrapper: %v", a)
	}
	if a := call("frontend/src/lib/relay.ts#relay.INVOKE%20name"); a["wrapper"] != true || a["form"] != "wrapper" || a["wrapper_identity"] != "function:frontend/src/lib/ipc.ts#call" || a["command"] != nil {
		t.Errorf("inner wrapper site: %v", a)
	}
	if a := call("frontend/src/local.ts#INVOKE%20%27greet%27%403"); a["wrapper_identity"] != "function:frontend/src/local.ts#send" {
		t.Errorf("local wrapper: %v", a)
	}
	if a := call("frontend/src/app.ts#namespaced.INVOKE%20%27aliased%27"); a["form"] != "namespace" {
		t.Errorf("namespace form: %v", a)
	}
	if a := call("frontend/src/app.ts#coreMember.INVOKE%20%27renamed%27"); a["form"] != "namespace" {
		t.Errorf("core member form: %v", a)
	}
	if a := call("frontend/src/global.js#INVOKE%20%27greet%27"); a["form"] != "global" || a["command"] != "greet" {
		t.Errorf("global form: %v", a)
	}
	if a := call("frontend/src/app.ts#plugins.INVOKE%20%27plugin%3Afs%7Cread_file%27"); a["plugin"] != "fs" || a["plugin_command"] != "read_file" {
		t.Errorf("plugin split: %v", a)
	}
	if a := call("frontend/src/app.ts#plugins.INVOKE%20%27plugin%3Ax%27"); a["plugin"] != "x" || a["plugin_command"] != "" {
		t.Errorf("plugin without command: %v", a)
	}
	if a := call("frontend/src/app.test.ts#fromTest.INVOKE%20%27greet%27"); a["scope"] != "test" {
		t.Errorf("test scope: %v", a)
	}
	// R2: a conditional name carries conditional on the call and each alternative.
	conditional := call("frontend/src/app.ts#conditional.INVOKE%20flag%3F%27greet%27%3A%27renamed%27")
	alternatives, _ := conditional["alternatives"].([]any)
	if conditional["conditional"] != true || conditional["command"] != nil || len(alternatives) != 2 {
		t.Errorf("conditional call: %v", conditional)
	}
	for _, alt := range alternatives {
		if m := alt.(map[string]any); m["conditional"] != true || m["status"] != "linked" {
			t.Errorf("alternative: %v", m)
		}
	}
	// M1: two arms may take `status`; the call links to both, marked
	// conditional, with each target and its cfg_gated.
	status := call("frontend/src/app.ts#status.INVOKE%20%27status%27")
	statusAlternatives, _ := status["alternatives"].([]any)
	if status["conditional"] != true || status["command"] != "status" || !reflect.DeepEqual(statusAlternatives, []any{
		map[string]any{"command": "status", "status": "linked", "conditional": true, "target": appMain + "legacy_status", "cfg_gated": true},
		map[string]any{"command": "status", "status": "linked", "conditional": true, "target": appMain + "status", "cfg_gated": false},
	}) {
		t.Errorf("cfg-gated duplicate arms: %v", status)
	}
	// m2: a production call never links to a cfg(test) command.
	if a := call("frontend/src/app.ts#twoLevel.INVOKE%20%27test_cmd%27"); !reflect.DeepEqual(a["candidates"], []any{appMain + "test_cmd"}) {
		t.Errorf("test_only command: %v", a)
	}
	invokes := ipcEdges(linked, "invokes_command")
	for _, want := range []string{
		"ipc_call:frontend/src/app.ts#conditional.INVOKE%20flag%3F%27greet%27%3A%27renamed%27 -> " + appMain + "greet",
		"ipc_call:frontend/src/app.ts#conditional.INVOKE%20flag%3F%27greet%27%3A%27renamed%27 -> " + appMain + "rename_me",
		"ipc_call:frontend/src/app.ts#coreMember.INVOKE%20%27renamed%27 -> " + appMain + "rename_me",
		"ipc_call:frontend/src/app.test.ts#testCommand.INVOKE%20%27test_cmd%27 -> " + appMain + "test_cmd",
		"ipc_call:frontend/src/app.ts#status.INVOKE%20%27status%27 -> " + appMain + "legacy_status",
		"ipc_call:frontend/src/app.ts#status.INVOKE%20%27status%27 -> " + appMain + "status",
		"ipc_call:frontend/src/writes.ts#useWrites.INVOKE%20%27greet%27 -> " + appMain + "greet",
		"ipc_call:frontend/src/local.ts#INVOKE%20%27greet%27%402 -> " + appMain + "greet",
	} {
		if !contains(invokes, want) {
			t.Errorf("missing invokes_command %s", want)
		}
	}
	// 14 linked calls, the conditional and the status ones with two targets
	// each; the first greet entry wins over the duplicate.
	if len(invokes) != 16 {
		t.Errorf("invokes_command edges %d: %v", len(invokes), invokes)
	}
	for _, e := range invokes {
		if strings.HasSuffix(e, "greet%402") {
			t.Errorf("edge to an unreachable entry: %s", e)
		}
	}
	diagnostics := strings.Join(ipcDiagnostics(linked), "\n")
	for _, want := range []string{
		"tauri_command_duplicate app/src/main.rs:33: entry commands::greet repeats command greet; the first entry wins",
		"tauri_command_invalid app/src/bare.rs:8: command twice has more than one Tauri command attribute",
		"tauri_command_invalid app/src/commands.rs:19: command dynamic_name: non-literal value for rename",
		"tauri_entry_unresolved app/src/plugins.rs:19: entry crate::bare::twice does not resolve to a Tauri command",
		"tauri_entry_unresolved app/src/plugins.rs:20: entry crate::negatives::poise_cmd does not resolve to a Tauri command",
		"tauri_entry_unresolved app/src/plugins.rs:21: entry crate::negatives::clap_like does not resolve to a Tauri command",
	} {
		if !strings.Contains(diagnostics, want) {
			t.Errorf("missing diagnostic %q in\n%s", want, diagnostics)
		}
	}
	if len(linked.Diagnostics) != 6 {
		t.Errorf("linked diagnostics:\n%s", diagnostics)
	}

	// Registries the proof rejects, with the first failing rule.
	reasons := map[string]string{}
	for _, n := range registries.Nodes {
		if r, ok := n.Attributes["reason"].(string); ok {
			reasons[n.QualifiedName] = r
		} else {
			reasons[n.QualifiedName] = n.Attributes["registry"].(string)
		}
	}
	if want := map[string]string{
		"path_form.COMMAND ping":       "context",
		"let_bound.COMMAND ping":       "context",
		"incomplete.COMMAND ping":      "incomplete",
		"later_handler.COMMAND ping":   "not_last",
		"later_handler.COMMAND ping@2": "not_consumed",
		"escapes.COMMAND ping":         "not_consumed",
		"extension.COMMAND ping":       "method",
		"generic_root.COMMAND ping":    "generic",
		"helper_root.COMMAND ping":     "root",
		"partial_if.COMMAND ping":      "flow_binding",
		"shadowed.COMMAND ping":        "shadowed",
		"destructured.COMMAND ping":    "shadowed",
		"body_macro.COMMAND ping":      "macro",
		"opaque_macro.COMMAND ping":    "macro",
		"plugin_mismatch.COMMAND ping": "plugin_name",
		"attribute_macro.COMMAND ping": "macro",
		"mock_root.COMMAND ping":       "mock_builder",
	}; !reflect.DeepEqual(reasons, want) {
		t.Errorf("registry reasons:\n got %v\nwant %v", reasons, want)
	}
	registryDiagnostics := ipcDiagnostics(registries)
	invalid, unproven, handlers := 0, 0, 0
	for _, d := range registryDiagnostics {
		switch {
		case strings.HasPrefix(d, "tauri_handler_invalid registries/src/main.rs:"):
			invalid++
		case strings.HasSuffix(d, "[handler]"):
			// The foreign generate_handler and the let-bound handler's call.
			handlers++
		case strings.HasPrefix(d, "tauri_registry_unproven"):
			unproven++
		}
	}
	if invalid != 1 || unproven != 17 || handlers != 2 || len(registryDiagnostics) != 20 {
		t.Errorf("registry diagnostics: %v", registryDiagnostics)
	}

	// Test-scope registry: production calls are test_only or not proven.
	if got, want := ipcStatuses(testOnly), map[string]string{
		"test-only/web/main.ts#production.INVOKE%20%27ping%27":    "test_only",
		"test-only/web/main.ts#production.INVOKE%20%27pong%27":    "shadow_possible",
		"test-only/web/main.ts#production.INVOKE%20%27nope%27":    "unknown_command",
		"test-only/web/main.test.ts#fromTest.INVOKE%20%27ping%27": "linked",
	}; !reflect.DeepEqual(got, want) {
		t.Errorf("test-only statuses: %v", got)
	}
	if a := ipcNodes(testOnly)["ipc_call:test-only/web/main.ts#production.INVOKE%20%27ping%27"].Attributes; !reflect.DeepEqual(a["candidates"], []any{"ipc_command:test-only/tests/ipc.rs#main.COMMAND%20ping"}) {
		t.Errorf("test_only candidates: %v", a)
	}
	if got := ipcEdges(testOnly, "invokes_command"); !reflect.DeepEqual(got, []string{"ipc_call:test-only/web/main.test.ts#fromTest.INVOKE%20%27ping%27 -> ipc_command:test-only/tests/ipc.rs#main.COMMAND%20ping"}) {
		t.Errorf("test-only invokes: %v", got)
	}

	// A second proven app makes every app match ambiguous.
	ambiguous, _, _ := ipcIndex(t, ipcScenario(t, "app", "frontend", "second-app"))
	if a := ipcNodes(ambiguous)["ipc_call:frontend/src/app.ts#named.INVOKE%20%27greet%27"].Attributes; a["status"] != "ambiguous" || !reflect.DeepEqual(a["candidates"], []any{appMain + "greet", "ipc_command:second-app/src/main.rs#main.COMMAND%20greet"}) {
		t.Errorf("ambiguous: %v", a)
	}
	if len(ipcEdges(ambiguous, "invokes_command")) != 0 || countCodes(ambiguous, "tauri_app_ambiguous") != 2 {
		t.Errorf("ambiguous edges or diagnostics: %v", ipcDiagnostics(ambiguous))
	}
	// A permissions/ file in the app crate makes app matches acl_possible.
	aclRoot := ipcScenario(t, "app", "frontend")
	if err := os.Rename(filepath.Join(aclRoot, "app/_permissions"), filepath.Join(aclRoot, "app/permissions")); err != nil {
		t.Fatal(err)
	}
	acl, _, _ := ipcIndex(t, aclRoot)
	if got := ipcStatuses(acl)["frontend/src/app.ts#named.INVOKE%20%27greet%27"]; got != "acl_possible" || len(ipcEdges(acl, "invokes_command")) != 0 || countCodes(acl, "tauri_acl_possible") != 1 {
		t.Errorf("acl_possible: %s %v", got, ipcDiagnostics(acl))
	}
	// Any unproven handler in the repository blocks app matches (A1).
	blocked, _, _ := ipcIndex(t, ipcScenario(t, "app", "frontend", "registries"))
	if got := ipcStatuses(blocked)["frontend/src/app.ts#named.INVOKE%20%27greet%27"]; got != "registry_unproven" || len(ipcEdges(blocked, "invokes_command")) != 0 {
		t.Errorf("registry_unproven: %s", got)
	}
	// Revision 2, M2: other app evidence blocks the single-app proof. A
	// closure handler is an unproven handler; a constructor, a second
	// generate_context! and a second config directory are another app.
	otherApp, _, _ := ipcIndex(t, ipcScenario(t, "app", "frontend", "other-app"))
	if got := ipcStatuses(otherApp)["frontend/src/app.ts#named.INVOKE%20%27greet%27"]; got != "registry_unproven" || len(ipcEdges(otherApp, "invokes_command")) != 0 || !strings.Contains(strings.Join(ipcDiagnostics(otherApp), "\n"), "tauri_registry_unproven other-app/src/main.rs:4: invoke_handler argument is not Tauri's generate_handler! [handler]") {
		t.Errorf("closure handler app: %s %v", got, ipcDiagnostics(otherApp))
	}
	bareApp, _, _ := ipcIndex(t, ipcScenario(t, "app", "frontend", "bare-app"))
	if a := ipcNodes(bareApp)["ipc_call:frontend/src/app.ts#named.INVOKE%20%27greet%27"].Attributes; a["status"] != "ambiguous" || !reflect.DeepEqual(a["candidates"], []any{appMain + "greet"}) || len(ipcEdges(bareApp, "invokes_command")) != 0 {
		t.Errorf("bare app: %v", a)
	}
	if got, want := ipcDiagnostics(bareApp), []string{
		"tauri_app_ambiguous app/src/main.rs:25: 5 other Tauri app sites in the repository; app invokes are not linked",
		"tauri_app_ambiguous app/src/main.rs:40: one of several Tauri app sites [context]",
		"tauri_app_ambiguous app/tauri.conf.json:1: one of several Tauri app sites [config]",
		"tauri_app_ambiguous bare-app/src/main.rs:7: one of several Tauri app sites [root]",
		"tauri_app_ambiguous bare-app/src/main.rs:8: one of several Tauri app sites [context]",
		"tauri_app_ambiguous bare-app/tauri.conf.json:1: one of several Tauri app sites [config]",
	}; !reflect.DeepEqual(got[:len(want)], want) || countCodes(bareApp, "tauri_app_ambiguous") != len(want) {
		t.Errorf("bare app diagnostics:\n%s", strings.Join(got, "\n"))
	}
	// Test-only builders (a #[cfg(test)] module, mock_builder in tests/)
	// neither prove nor block the production app (m5).
	mockTests, _, _ := ipcIndex(t, ipcScenario(t, "app", "frontend", "mock-tests"))
	if got := ipcStatuses(mockTests); !reflect.DeepEqual(got, ipcStatuses(linked)) || !reflect.DeepEqual(ipcEdges(mockTests, "invokes_command"), ipcEdges(linked, "invokes_command")) {
		t.Errorf("test-only builders changed app matches: %v", got)
	}
	mockReasons := map[string]any{}
	for _, n := range mockTests.Nodes {
		if strings.HasPrefix(n.File, "mock-tests/") {
			mockReasons[n.Identity] = n.Attributes["reason"]
			if n.File == "mock-tests/src/lib.rs" && n.Attributes["test_only"] != true {
				t.Errorf("cfg(test) registry command: %v", n.Attributes)
			}
		}
	}
	if want := map[string]any{
		// #[test] is not a verified attribute; the registry is unproven but
		// in a cfg(test) module, so it does not block either.
		"ipc_command:mock-tests/src/lib.rs#tests.builds.COMMAND%20ping":     "macro",
		"ipc_command:mock-tests/tests/ipc.rs#mocked.COMMAND%20pong":         "mock_builder",
		"ipc_command:mock-tests/tests/ipc.rs#mocked_binding.COMMAND%20pong": "mock_builder",
	}; !reflect.DeepEqual(mockReasons, want) {
		t.Errorf("mock-tests registries: %v", mockReasons)
	}
	// A scanned package named @tauri-apps/api shadows the real one.
	shadowed, _, _ := ipcIndex(t, ipcScenario(t, "shadowed"))
	if len(shadowed.Nodes) != 0 {
		t.Errorf("shadowed package: %v", shadowed.Nodes)
	}

	// Replay: a warm run parses nothing; editing only TypeScript relinks
	// without reparsing Rust.
	second, summary, writes := emit(t, linkedRoot, s, options(t))
	if !bytes.Equal(first, second) || summary.Parsed != 0 || writes.Files+writes.Nodes+writes.Edges+writes.Deleted != 0 {
		t.Fatal("Tauri invoke replay", summary, writes)
	}
	app := filepath.Join(linkedRoot, "frontend/src/app.ts")
	if err := os.WriteFile(app, bytes.Replace(host(linkedRoot, "frontend/src/app.ts"), []byte("invoke('missing_cmd')"), []byte("invoke('aliased')"), 1), 0644); err != nil {
		t.Fatal(err)
	}
	_, summary, _ = emit(t, linkedRoot, s, options(t))
	if summary.Parsed != 1 {
		t.Fatal("TypeScript edit reparsed", summary)
	}
	relinked := dumpIPC(t, s)
	if got := ipcStatuses(relinked)["frontend/src/app.ts#unregistered.INVOKE%20%27aliased%27"]; got != "linked" {
		t.Errorf("relinked status %q", got)
	}

	// A CRLF copy produces identical facts.
	crlf := ipcScenario(t, "app", "frontend")
	if err := filepath.WalkDir(crlf, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
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
	if got, _, _ := ipcIndex(t, crlf); !reflect.DeepEqual(got, linked) {
		t.Fatal("CRLF changed Tauri IPC records")
	}
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func countCodes(p ipcProjection, code string) int {
	n := 0
	for _, d := range p.Diagnostics {
		if d.Code == code {
			n++
		}
	}
	return n
}
