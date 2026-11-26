package typescript

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/graphvein/graphvein/internal/parse"
)

func TestRouterFallbackAndAdditivity(t *testing.T) {
	source, err := os.ReadFile("../../../testdata/fixtures/angular-router-syntax/calls.ts")
	if err != nil {
		t.Fatal(err)
	}
	source = append(source, []byte(`
function factoryShadow(platformBrowserDynamic: unknown, routes: unknown) {
  platformBrowserDynamic().bootstrapModule(routes['x']);
}
const typedLoader = { loadChildren: () => import('./targets').then((m: typeof import('./targets')) => m.M) };
function unshadowed() { configure(routes); }
function nestedArgument(Home: unknown, guard: unknown, extra: unknown, key: unknown, member: unknown, Type: unknown) {
  configure([{ key: Home, canActivate: [guard], extra, data: target.member } as Type]);
}
`)...)
	results, err := parse.Batch(context.Background(), []parse.Job{{Language: "typescript", Source: source}}, parse.Options{CacheDir: filepath.Join(t.TempDir(), "cache")})
	if err != nil || results[0].Err != nil {
		t.Fatal(err)
	}
	root := results[0].Root
	f := Extract("calls.ts", source, root)
	templates := CaptureTemplates(root, f.Nodes)
	declarations := CaptureDeclarations(root, f, templates)
	injections := CaptureInjections(root, f)
	before, _ := json.Marshal(CachedFacts{Facts: f, TemplateSyntax: &templates, Declarations: &declarations, Injections: &injections})
	full := CaptureRouter(root, f, declarations)
	after, _ := json.Marshal(CachedFacts{Facts: f, TemplateSyntax: &templates, Declarations: &declarations, Injections: &injections})
	if string(before) != string(after) {
		t.Fatal("router capture changed an existing payload")
	}
	// Force precisely F5's missing-client-record case without altering bindings.
	copyFacts := f
	copyRoutes := *f.Routes
	copyRoutes.Clients = nil
	copyFacts.Routes = &copyRoutes
	fallback := CaptureRouter(root, copyFacts, declarations)
	if !full.Valid() || !fallback.Valid() || len(full.Calls) != len(fallback.Calls) {
		t.Fatal("invalid source syntax")
	}
	shadowCounts := map[string]int{}
	for _, c := range fallback.Calls {
		if c.Scope != 0 {
			t.Fatal("fallback scope", c)
		}
		for _, name := range c.Shadows {
			shadowCounts[name]++
		}
		if (c.Span.StartLine == 28 || c.Span.StartLine == 29) && len(c.Shadows) > 0 {
			t.Fatal("default expression became a binding", c)
		}
	}
	if shadowCounts["routes"] < 5 || shadowCounts["RouterModule"] != 1 {
		t.Fatal("source-only shadows", shadowCounts)
	}
	last := fallback.Calls[len(fallback.Calls)-3]
	if !reflect.DeepEqual(last.Shadows, []string{"platformBrowserDynamic", "routes"}) {
		t.Fatal("receiver/subscript roots lost source shadows", last)
	}
	for _, syntax := range []RouterSyntax{full, fallback} {
		call := syntax.Calls[len(syntax.Calls)-1]
		if call.Scope != 0 || !reflect.DeepEqual(call.Shadows, []string{"Home", "extra", "guard"}) {
			t.Fatal("nested identifiers/shorthand or excluded keys/properties/types", call)
		}
	}
	if len(full.Loaders) != 1 || full.Loaders[0].Form != "import_member" || full.Loaders[0].Member != "M" {
		t.Fatal("typed identifier callback", full.Loaders)
	}
	if reflect.DeepEqual(full.Calls, fallback.Calls) {
		t.Fatal("existing lexical scopes were not reused")
	}
	for _, mutate := range []func(*RouterSyntax){
		func(s *RouterSyntax) { s.Version = 0 }, func(s *RouterSyntax) { s.Version = 1 }, func(s *RouterSyntax) { s.Calls = nil }, func(s *RouterSyntax) { s.Loaders = nil }, func(s *RouterSyntax) { s.Arrays = nil },
		func(s *RouterSyntax) { s.Configs = nil }, func(s *RouterSyntax) { s.Methods = nil },
		func(s *RouterSyntax) { s.Calls[0].First = nil }, func(s *RouterSyntax) { s.Calls[0].Shadows = nil }, func(s *RouterSyntax) { s.Calls[0].Callee.Kind = "" },
	} {
		data, _ := json.Marshal(full)
		var broken RouterSyntax
		json.Unmarshal(data, &broken)
		mutate(&broken)
		if broken.Valid() {
			t.Fatal("incomplete router syntax replayed")
		}
	}
}

// TestRouterApplicationSyntax covers router payload version 2 (ADR 0036
// Revision 1): application bootstrap calls keep their options argument,
// module const objects with an own providers key are kept as configs, and
// method keys of route-shaped objects are named.
func TestRouterApplicationSyntax(t *testing.T) {
	source := []byte(`import { bootstrapApplication as boot, createApplication } from '@angular/platform-browser';
import * as browser from '@angular/platform-browser';
import { App } from './app';
export const appConfig = { providers: [routes] } satisfies object;
const plain = { other: 1 };
let mutable = { providers: [] };
boot(App, appConfig);
createApplication({ providers: [] });
browser.bootstrapApplication(App);
function shadowed(appConfig: unknown) { boot(App, appConfig); }
const route = { path: 'a', loadComponent() { return import('./a'); }, get title() { return 't'; }, [key]() {} };
const other = { name() {} };
`)
	results, err := parse.Batch(context.Background(), []parse.Job{{Language: "typescript", Source: source}}, parse.Options{CacheDir: filepath.Join(t.TempDir(), "cache")})
	if err != nil || results[0].Err != nil {
		t.Fatal(err)
	}
	root := results[0].Root
	f := Extract("main.ts", source, root)
	templates := CaptureTemplates(root, f.Nodes)
	s := CaptureRouter(root, f, CaptureDeclarations(root, f, templates))
	if !s.Valid() || s.Version != 2 {
		t.Fatal("invalid version 2 payload", s.Version)
	}
	var kinds []string
	for _, c := range s.Calls {
		kinds = append(kinds, c.Kind)
	}
	if !reflect.DeepEqual(kinds, []string{"bootstrap_application", "create_application", "bootstrap_application", "bootstrap_application"}) {
		t.Fatal("call kinds", kinds)
	}
	if o := s.Calls[0].Options; o == nil || o.Kind != "identifier" || s.Calls[0].First == nil || s.Calls[0].First.Kind != "identifier" {
		t.Fatal("bootstrapApplication options", s.Calls[0])
	}
	if o := s.Calls[1].Options; o == nil || o.Kind != "object" {
		t.Fatal("createApplication options", s.Calls[1])
	}
	if s.Calls[2].Options != nil {
		t.Fatal("options without a second argument", s.Calls[2])
	}
	if !reflect.DeepEqual(s.Calls[3].Shadows, []string{"appConfig"}) {
		t.Fatal("options names must count for shadows", s.Calls[3].Shadows)
	}
	if len(s.Configs) != 1 || s.Configs[0].Name != "appConfig" || s.Configs[0].Value.Kind != "object" {
		t.Fatal("configs", s.Configs)
	}
	var keys []string
	for _, m := range s.Methods {
		keys = append(keys, m.Key)
		if m.Object.StartLine != 11 {
			t.Fatal("method outside a route-shaped object", m)
		}
	}
	if !reflect.DeepEqual(keys, []string{"loadComponent", "title"}) || s.Methods[0].Index != 1 || s.Methods[1].Index != 2 {
		t.Fatal("method keys", s.Methods)
	}
}
