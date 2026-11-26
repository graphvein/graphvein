package index_test

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/store"
)

// routesProjection keeps the routing records of a snapshot: ui_route nodes,
// routing edges and angular_route_* diagnostics.
type routesProjection struct {
	Nodes       []contract.Node
	Edges       []contract.Edge
	Diagnostics []contract.Diagnostic
}

func projectRoutes(snapshot store.Snapshot) routesProjection {
	p := routesProjection{Nodes: []contract.Node{}, Edges: []contract.Edge{}, Diagnostics: []contract.Diagnostic{}}
	for _, n := range snapshot.Nodes {
		if n.Kind == "ui_route" {
			p.Nodes = append(p.Nodes, n)
		}
	}
	for _, e := range snapshot.Edges {
		if e.Kind == "routes_to" || e.Kind == "loads_children" || e.Kind == "contains" && strings.HasPrefix(e.Target, "ui_route:") {
			p.Edges = append(p.Edges, e)
		}
	}
	for _, d := range snapshot.Diagnostics {
		if strings.HasPrefix(d.Code, "angular_route_") {
			p.Diagnostics = append(p.Diagnostics, d)
		}
	}
	return p
}

func dumpRoutes(t *testing.T, s store.Store) routesProjection {
	t.Helper()
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	return projectRoutes(snapshot)
}

func TestAngularRoutes(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/angular-routes")); err != nil {
		t.Fatal(err)
	}
	s, opt := open(t), options(t)
	first, summary, _ := emit(t, root, s, opt)
	routes := dumpRoutes(t, s)
	golden.Check(t, "../../testdata/fixtures/angular-routes.golden.json", routes)

	const app = "ui_route:standalone/src/app/app.routes.ts#routes.ROUTE%20"
	nodes := map[string]contract.Node{}
	for _, n := range routes.Nodes {
		nodes[n.Identity] = n
		if n.Level != 4 || n.Span == nil || !strings.HasPrefix(n.ContentHash, "sha256:") || n.Attributes["framework"] != "angular" {
			t.Errorf("node shape %s", n.Identity)
		}
		checkNoControl(t, n.Identity, n.Attributes)
	}
	paths := func(identity string) []string {
		t.Helper()
		n, ok := nodes[identity]
		if !ok {
			t.Fatalf("missing node %s", identity)
		}
		out := []string{}
		for _, raw := range n.Attributes["paths"].([]any) {
			entry := raw.(map[string]any)
			p := entry["path"].(string)
			if entry["scope"] == "test" {
				p += " (test)"
			}
			out = append(out, p)
		}
		return out
	}
	for identity, want := range map[string][]string{
		app + "%22%22":              {"/"},
		app + "docs.ROUTE%20%22%22": {"/docs"},
		app + "docs.ROUTE%20%2A%2A": {"/docs/**"},
		app + "%28matcher%29":       {},
		app + "aside":               {},
		app + "aside.ROUTE%20inner": {},
		app + "%28dynamic%29":       {"/dyn"},
		app + "dup%402":             {"/dup"},
		app + "partial":             {},
		app + "route-providers":     {},
		"ui_route:standalone/src/app/shared.routes.ts#shared.ROUTE%20help":                                        {"/help", "/sync-children/help"},
		"ui_route:standalone/src/app/profile.routes.ts#ROUTE%20%22%22.ROUTE%20%3Ausername.ROUTE%20favorites":      {"/profile/:username/favorites"},
		"ui_route:standalone/src/app/reports.routes.ts#REPORT_ROUTES.ROUTE%20%3Ayear":                             {"/harness/:year (test)", "/reports/:year"},
		"ui_route:standalone/src/app/admin.routes.ts#ROUTE%20%22%22":                                              {"/admin"},
		"ui_route:standalone/src/app/lists.ts#ROUTE%20spread-head":                                                {"/spread-head"},
		"ui_route:standalone/src/app/forms.ts#Shell.ROUTE%20component-providers":                                  {},
		"ui_route:standalone/src/testing/router.spec.ts#testProviders.ROUTE%20test-only":                          {"/test-only (test)"},
		"ui_route:ngmodule/src/app/home.module.ts#HomeModule.ROUTE%20%22%22":                                      {"/"},
		"ui_route:ngmodule/src/app/help.module.ts#ROUTE%20help":                                                   {"/help", "/settings/help"},
		"ui_route:ngmodule/src/app/profile/profile.module.ts#ProfileModule.ROUTE%20%3Ausername.ROUTE%20favorites": {"/settings/profile/:username/favorites"},
		"ui_route:ngmodule/src/app/provided/provided.module.ts#ProvidedModule.ROUTE%20provided":                   {"/provided"},
		"ui_route:ngmodule/src/app/lazy-root/lazy-root.module.ts#LazyRootModule.ROUTE%20again":                    {},
		"ui_route:ngmodule/src/app/orphan/orphan.module.ts#OrphanModule.ROUTE%20orphan":                           {},
		"ui_route:ngmodule/src/app/cycle/cycle-b.module.ts#CycleBModule.ROUTE%20a":                                {"/cycle/b/a"},
	} {
		if got := paths(identity); !reflect.DeepEqual(got, want) {
			t.Errorf("%s paths %v, want %v", identity, got, want)
		}
	}
	// Revision 1 (review 1). F1: production provideRouter is a root only inside
	// proven application bootstrap providers; F2: closures follow exports; F4:
	// a named method keeps composition; F6, F7, F10 as named.
	const standalone = "ui_route:standalone/src/"
	for identity, want := range map[string][]string{
		standalone + "forms.main.ts#ROUTE%20inline-options":                          {"/inline-options"},
		standalone + "app/forms.ts#ROUTE%20local-const":                              {"/local-const"},
		standalone + "app/lists.ts#satisfied.ROUTE%20satisfied":                      {"/satisfied"},
		standalone + "app/lists.ts#spreadRoutes.ROUTE%20spread-tail":                 {"/spread-tail"},
		standalone + "app/app.config.ts#unusedConfig.ROUTE%20unbootstrapped":         {},
		standalone + "app/app.config.ts#mergedConfig.ROUTE%20merged":                 {},
		standalone + "app/app.config.ts#specOnlyProviders.ROUTE%20spec-bootstrapped": {"/spec-bootstrapped (test)"},
		standalone + "app/route-providers.ts#ROUTE_PROVIDERS.ROUTE%20route-scoped":   {},
		standalone + "app/factories.ts#FeatureModule.forRoot.ROUTE%20from-static":    {},
		standalone + "app/factories.ts#neverCalled.ROUTE%20never-called":             {},
		standalone + "app/twins.routes.ts#ROUTE%20twin%402":                          {"/twins/twin"},
		standalone + "app/shared.routes.ts#shared.ROUTE%20legacy-shared":             {"/legacy-shared", "/sync-children/legacy-shared"},
		app + "method":        {"/method"},
		app + "paren":         {"/paren"},
		app + "cast":          {"/cast"},
		app + "both-children": {"/both-children"},
		"ui_route:ngmodule/src/app/lazy-providers/lazy-providers.module.ts#ROUTE%20x":                  {},
		"ui_route:ngmodule/src/app/exported/exported.module.ts#ExportedRoutingModule.ROUTE%20exported": {"/exported"},
	} {
		if got := paths(identity); !reflect.DeepEqual(got, want) {
			t.Errorf("revision 1: %s paths %v, want %v", identity, got, want)
		}
	}
	// F3: a default-export array mutated through a default import; F5: no
	// traversal through rejected children/loadChildren; F7: the unreached first
	// twin keeps the name ROUTE twin.
	for _, identity := range []string{
		standalone + "app/anon.routes.ts#ROUTE%20anon",
		app + "both-children.ROUTE%20kid",
		standalone + "app/twins.routes.ts#ROUTE%20twin",
	} {
		if _, ok := nodes[identity]; ok {
			t.Errorf("revision 1: unexpected node %s", identity)
		}
	}
	// Rejected registration forms and unresolved lists emit no nodes.
	for _, text := range []string{"typed", "other", "shadowed-callee", "local-factory", "mutable", "pushed"} {
		for identity := range nodes {
			if strings.HasSuffix(identity, "ROUTE%20"+text) {
				t.Errorf("rejected form %s emitted %s", text, identity)
			}
		}
	}
	if nodes["ui_route:standalone/src/testing/router.spec.ts#testProviders.ROUTE%20test-only"].Attributes["scope"] != "test" {
		t.Error("test-file route lost scope")
	}
	status := func(identity, key string) string {
		t.Helper()
		v, _ := nodes[identity].Attributes[key].(map[string]any)
		s, _ := v["status"].(string)
		return s
	}
	for identity, want := range map[string]string{
		app + "lazy-default":          "linked",
		app + "lazy-member":           "linked",
		app + "lazy-sync":             "linked",
		app + "lazy-legacy":           "invalid",
		app + "lazy-module-component": "invalid",
		app + "wrong-role":            "invalid",
		app + "plain":                 "invalid",
		app + "standalone-false":      "linked",
		"ui_route:standalone/src/app/admin.routes.ts#ROUTE%20legacy":              "invalid",
		"ui_route:standalone/src/app/forms.ts#inlineShadow.ROUTE%20inline-shadow": "unresolved",
		"ui_route:ngmodule/src/app/settings/settings.module.ts#ROUTE%20%22%22":    "linked",
		standalone + "app/shared.routes.ts#shared.ROUTE%20legacy-shared":          "linked",
		app + "method":      "unresolved",
		app + "spread-lazy": "unresolved",
	} {
		if got := status(identity, "component"); got != want {
			t.Errorf("%s component %s, want %s", identity, got, want)
		}
	}
	for identity, want := range map[string]string{
		app + "admin":            "linked",
		app + "reports":          "linked",
		app + "sync-children":    "linked",
		app + "invalid-children": "invalid",
		app + "async-children":   "unresolved",
		"ui_route:ngmodule/src/app/app-routing.module.ts#ROUTE%20component-children": "invalid",
		"ui_route:ngmodule/src/app/app-routing.module.ts#ROUTE%20partial":            "unresolved",
		"ui_route:ngmodule/src/app/app-routing.module.ts#ROUTE%20lazy-providers":     "linked",
		"ui_route:standalone/src/app/invalid.routes.ts#invalidConfig.ROUTE%20r-load": "invalid",
		app + "anon":          "unresolved",
		app + "both-children": "invalid",
		app + "spread-lazy":   "unresolved",
	} {
		if got := status(identity, "children_load"); got != want {
			t.Errorf("%s children_load %s, want %s", identity, got, want)
		}
	}
	if nodes[app+"lazy-default"].Attributes["lazy"] != true || nodes[app+"%22%22"].Attributes["lazy"] != false {
		t.Error("lazy attribute")
	}
	if nodes[app+"partial"].Attributes["partial"] != true || nodes[app+"method"].Attributes["partial"] != true {
		t.Error("partial objects")
	}
	// F4: keys a spread may supply are unknown, a named method is present.
	spread := nodes[app+"spread-lazy"].Attributes
	if v, ok := spread["lazy"]; !ok || v != nil || nodes[app+"method"].Attributes["lazy"] != true {
		t.Error("lazy behind a spread must be unknown, a loadComponent method lazy", spread["lazy"])
	}
	if g, ok := spread["guards"].(map[string]any); !ok || g["can_activate"] != nil || len(g) != 5 {
		t.Error("guards behind a spread must be unknown", spread["guards"])
	}
	for _, key := range []string{"matcher", "outlet", "redirect_to", "resolve", "title"} {
		if v, ok := spread[key]; !ok || v != nil {
			t.Errorf("%s behind a spread must be null, got %v (present %t)", key, v, ok)
		}
	}
	if _, ok := nodes[app+"method"].Attributes["outlet"]; ok {
		t.Error("a named method must not make absent keys unknown")
	}
	invalid := map[string]bool{}
	for _, n := range routes.Nodes {
		codes, _ := n.Attributes["invalid"].([]any)
		for _, c := range codes {
			invalid[c.(string)] = true
		}
	}
	for _, code := range []string{"leading_slash", "component_with_load_component", "missing_target", "missing_path", "empty_path_redirect_without_path_match", "path_with_matcher", "redirect_with_children", "redirect_with_guard", "named_outlet_without_target", "redirect_with_load_children"} {
		if !invalid[code] {
			t.Errorf("R6 code %s not raised", code)
		}
	}
	if _, ok := nodes["ui_route:standalone/src/app/invalid.routes.ts#invalidConfig.ROUTE%20%22%22%402"].Attributes["invalid"]; ok {
		t.Error("empty path with matcher is valid under Angular truthiness")
	}
	guards := nodes[app+"guarded"].Attributes["guards"].(map[string]any)["can_activate"].([]any)
	var guardStatus []string
	for _, g := range guards {
		guardStatus = append(guardStatus, g.(map[string]any)["status"].(string))
	}
	if !reflect.DeepEqual(guardStatus, []string{"linked", "linked", "inline", "external", "unresolved"}) {
		t.Error("guard statuses", guardStatus)
	}
	edges := map[string]int{}
	for _, e := range routes.Edges {
		edges[e.Kind]++
		if e.Kind == "loads_children" && (e.Source == app+"both-children" || strings.HasSuffix(e.Source, "#invalidConfig.ROUTE%20r-load")) {
			t.Error("F5: loads_children through a rejected configuration", e.Source)
		}
		if e.Provenance != "fact" || e.Confidence != 1 || len(e.Evidence) == 0 {
			t.Error("edge shape", e)
		}
	}
	if edges["contains"] != len(routes.Nodes) {
		t.Errorf("contains %d for %d nodes", edges["contains"], len(routes.Nodes))
	}
	codes := map[string]int{}
	lazyOnly := 0
	located := regexp.MustCompile(`^route at [^ ]+:[0-9]+:[0-9]+: `)
	for _, d := range routes.Diagnostics {
		codes[d.Code]++
		if strings.HasSuffix(d.Message, "(lazy reach only)") {
			lazyOnly++
		}
		if !located.MatchString(d.Message) {
			t.Error("F12: message lacks line:column", d.Message)
		}
	}
	if lazyOnly != 1 {
		t.Errorf("F6: %d lazy-reach-only warnings, want 1", lazyOnly)
	}
	for _, code := range []string{"angular_route_invalid", "angular_route_target_invalid", "angular_route_for_root_lazy", "angular_route_root_unproven", "angular_route_entry_unresolved", "angular_route_cycle"} {
		if codes[code] == 0 {
			t.Errorf("diagnostic %s not raised", code)
		}
	}

	again, replay, writes := emit(t, root, s, opt)
	if !bytes.Equal(first, again) || replay.Parsed != 0 || replay.Reused != summary.Parsed || writes.Files+writes.Nodes+writes.Edges+writes.Deleted != 0 {
		t.Fatal("routes replay", replay, writes)
	}

	// Editing only an imported routes file relinks other files' routes.
	rewrite(t, filepath.Join(root, "standalone/src/app/app.routes.ts"), "path: 'profile'", "path: 'people'")
	_, edited, _ := emit(t, root, s, opt)
	if edited.Parsed != 1 {
		t.Fatal("routes edit reparsed other files", edited)
	}
	relinked := map[string]contract.Node{}
	for _, n := range dumpRoutes(t, s).Nodes {
		relinked[n.Identity] = n
	}
	profile := relinked["ui_route:standalone/src/app/profile.routes.ts#ROUTE%20%22%22"]
	if got := profile.Attributes["paths"].([]any); len(got) != 1 || got[0].(map[string]any)["path"] != "/people" {
		t.Error("lazy routes were not relinked", got)
	}
	rewrite(t, filepath.Join(root, "standalone/src/app/segments.ts"), "'dyn'", "'renamed'")
	if _, edited, _ = emit(t, root, s, opt); edited.Parsed != 1 {
		t.Fatal("segment edit reparsed other files", edited)
	}
	for _, n := range dumpRoutes(t, s).Nodes {
		if n.Identity == app+"%28dynamic%29" && n.Attributes["path"] != "renamed" {
			t.Error("evaluated path was not relinked", n.Attributes)
		}
	}

	// F7: reaching the first twin from another file does not rename the second.
	rewrite(t, filepath.Join(root, "standalone/src/app/app.routes.ts"), "import { ROUTE_PROVIDERS } from './route-providers';", "import { ROUTE_PROVIDERS } from './route-providers';\nimport { twinFirst } from './twins.routes';")
	rewrite(t, filepath.Join(root, "standalone/src/app/app.routes.ts"), "  { path: 'cast' as const", "  { path: 'twin-first', children: twinFirst },\n  { path: 'cast' as const")
	if _, edited, _ = emit(t, root, s, opt); edited.Parsed != 1 {
		t.Fatal("twin edit reparsed other files", edited)
	}
	twins := map[string]string{}
	for _, e := range dumpRoutes(t, s).Edges {
		if e.Kind == "routes_to" && strings.HasPrefix(e.Source, standalone+"app/twins.routes.ts#") {
			twins[e.Source] = e.Target
		}
	}
	if twins[standalone+"app/twins.routes.ts#ROUTE%20twin"] != "class:standalone/src/app/pages/home.ts#HomePage" || twins[standalone+"app/twins.routes.ts#ROUTE%20twin%402"] != "class:standalone/src/app/pages/docs.ts#DocsPage" {
		t.Error("F7: twin identities depend on reachability", twins)
	}

	// A CRLF copy produces identical facts.
	crlf := t.TempDir()
	if err := os.CopyFS(crlf, os.DirFS("../../testdata/fixtures/angular-routes")); err != nil {
		t.Fatal(err)
	}
	if err := filepath.WalkDir(crlf, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".ts") {
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
	if !reflect.DeepEqual(routes, dumpRoutes(t, other)) {
		t.Fatal("CRLF changed routing facts")
	}
}

func rewrite(t *testing.T, path, old, replacement string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(old)) {
		t.Fatalf("%s lacks %q", path, old)
	}
	if err := os.WriteFile(path, bytes.Replace(data, []byte(old), []byte(replacement), 1), 0644); err != nil {
		t.Fatal(err)
	}
}

// checkNoControl rejects U+0000-U+001F in every attribute string value.
func checkNoControl(t *testing.T, identity string, v any) {
	t.Helper()
	switch x := v.(type) {
	case string:
		for _, r := range x {
			if r < 0x20 {
				t.Errorf("%s attribute contains a control character: %q", identity, x)
			}
		}
	case map[string]any:
		keys := []string{}
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			checkNoControl(t, identity, x[k])
		}
	case []any:
		for _, e := range x {
			checkNoControl(t, identity, e)
		}
	}
}
