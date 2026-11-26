package index_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/store"
)

func TestAngularHttpClientGolden(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/angular-httpclient")); err != nil {
		t.Fatal(err)
	}
	// Revision 3 runs as a separate repository: an opaque registration would
	// otherwise deliberately change every unowned call, including loose/app.ts.
	if err := os.RemoveAll(filepath.Join(root, "revision3")); err != nil {
		t.Fatal(err)
	}
	s, opt := open(t), options(t)
	data, first, _ := emit(t, root, s, opt)
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	golden.Check(t, "../../testdata/fixtures/angular-httpclient.golden.json", store.Snapshot{Nodes: snapshot.Nodes, Edges: snapshot.Edges, Diagnostics: snapshot.Diagnostics})
	counts := map[string]int{}
	for _, n := range snapshot.Nodes {
		if n.Kind != "client_call" {
			continue
		}
		counts[n.File]++
		a := n.Attributes
		if n.File == "classic/app.ts" && a["base"] != "literal" {
			t.Fatal("HttpClientModule interceptor base", n)
		}
		if n.File == "classic-di/app.ts" && a["base"] != "literal" {
			t.Fatal("withInterceptorsFromDi base", n)
		}
		if n.File == "ignored/app.ts" && a["base"] != "none" {
			t.Fatal("non-running/spec interceptor supplied base", n)
		}
		if (n.File == "double/app.ts" || n.File == "conditional/app.ts" || n.File == "loose/app.ts") && a["base"] != "unresolved" {
			t.Fatal("unproven interceptor base", n)
		}
		if n.File == "functional/app.ts" && n.Span.StartLine == 14 && (a["base"] != "constant" || a["status"] != "linked") {
			t.Fatal("environment prefix/header-only interceptor", n)
		}
		if n.File == "functional/app.ts" && n.Name == "GET 'one'" && a["status"] != "linked" {
			t.Fatal("DI wrapper not linked", n)
		}
		if n.File == "transports.ts" && (n.Span.StartLine == 2 || n.Span.StartLine == 3 || n.Span.StartLine == 4) {
			p := a["paths"].([]any)[0].(map[string]any)
			if p["template"] != nil || p["origin"] != nil || a["base"] != "unresolved" {
				t.Fatal("dynamic authority", n)
			}
		}
		if n.File == "transports.ts" && (n.Span.StartLine == 5 || n.Span.StartLine == 9) && a["status"] != "linked" {
			t.Fatal("equivalent Express registration", n)
		}
		if n.File == "transports.ts" && n.Span.StartLine > 5 && n.Span.StartLine != 9 && a["status"] != "ambiguous" {
			t.Fatal("non-equivalent Express registration", n)
		}
	}
	if counts["functional/app.ts"] != 21 || counts["functional/barrel-local.ts"] != 2 {
		t.Fatalf("functional calls=%d, want 21; barrel calls=%d, want 2", counts["functional/app.ts"], counts["functional/barrel-local.ts"])
	}
	if counts["classic/app.ts"] != 1 || counts["classic-di/app.ts"] != 1 || counts["overridden/app.ts"] != 1 {
		t.Fatal("class interceptors/provider override counts", counts)
	}
	warmData, warm, _ := emit(t, root, s, opt)
	if string(data) != string(warmData) || warm.Parsed != 0 || warm.Reused != first.Parsed {
		t.Fatal("Angular cache replay differs", warm)
	}
	if err := os.Remove(filepath.Join(root, "angular.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "graphvein.yaml"), []byte("version: 1\nclients:\n  base_paths:\n    functional: /t012b-api\n"), 0644); err != nil {
		t.Fatal(err)
	}
	_, configured, _ := emit(t, root, s, opt)
	if configured.Parsed != 0 {
		t.Fatal("configuration reparsed Angular source")
	}
	configuredSnapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range configuredSnapshot.Nodes {
		if n.Kind == "client_call" && n.File == "functional/app.ts" && n.Span.StartLine == 14 {
			p := n.Attributes["paths"].([]any)[0].(map[string]any)
			if n.Attributes["base"] != "configured" || p["template"] != "/t012b-api/t012b-items" || p["origin"] != nil {
				t.Fatal("missing angular.json/configured unresolved base", n)
			}
		}
	}
	if err := os.WriteFile(filepath.Join(root, "angular.json"), []byte("{"), 0644); err != nil {
		t.Fatal(err)
	}
	emit(t, root, s, opt)
	invalid, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	warning := false
	for _, d := range invalid.Diagnostics {
		warning = warning || d.Code == "angular_config_parse_error"
	}
	if !warning {
		t.Fatal("malformed angular.json missing warning")
	}
	if err := os.Rename(filepath.Join(root, "shadow", "_package.json"), filepath.Join(root, "shadow", "package.json")); err != nil {
		t.Fatal(err)
	}
	emit(t, root, s, opt)
	shadow, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range shadow.Nodes {
		if n.Kind == "client_call" && n.Attributes["library"] == "angular" {
			t.Fatal("scanned Angular package must shadow external HttpClient", n)
		}
	}
}

func TestAngularInterceptorPrecisionGolden(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/angular-httpclient/revision3")); err != nil {
		t.Fatal(err)
	}
	s, opt := open(t), options(t)
	data, _, _ := emit(t, root, s, opt)
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	golden.Check(t, "../../testdata/fixtures/angular-httpclient-revision3.golden.json", store.Snapshot{Nodes: snapshot.Nodes, Edges: snapshot.Edges, Diagnostics: snapshot.Diagnostics})
	check := func(snapshot store.Snapshot, allDynamic bool) {
		t.Helper()
		calls := 0
		for _, n := range snapshot.Nodes {
			if n.Kind != "client_call" {
				continue
			}
			calls++
			dynamic := allDynamic || n.File == "opaque/app.ts"
			if n.Attributes["base"] != "unresolved" {
				t.Fatal("interceptor base must stay unresolved", n)
			}
			for _, entry := range n.Attributes["paths"].([]any) {
				p := entry.(map[string]any)
				if dynamic {
					if p["template"] != nil || p["origin"] != nil || p["status"] != "dynamic" {
						t.Fatal("opaque rewrite must null every alternative", n)
					}
				} else if p["template"] != "/t012b-r3-prefix" || p["status"] != "linked" || p["match"] != "suffix" {
					t.Fatal("unevaluable prefix must retain suffix match", n)
				}
			}
			for _, e := range snapshot.Edges {
				if e.Kind == "calls_route" && e.Source == n.Identity {
					if dynamic || e.Confidence != 0.6 {
						t.Fatal("unexpected interceptor link", e)
					}
				}
			}
		}
		if calls != 2 {
			t.Fatalf("got %d calls, want 2", calls)
		}
	}
	check(snapshot, false)
	warm, summary, _ := emit(t, root, s, opt)
	if string(data) != string(warm) || summary.Parsed != 0 {
		t.Fatal("interceptor precision cache replay differs")
	}
	if err := os.WriteFile(filepath.Join(root, "graphvein.yaml"), []byte("version: 1\nclients:\n  base_paths:\n    opaque: /t012b-r3-api\n"), 0644); err != nil {
		t.Fatal(err)
	}
	opaqueFile := filepath.Join(root, "opaque", "app.ts")
	original, err := os.ReadFile(opaqueFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, variant := range []struct{ old, replacement string }{
		{"rewrite(req.url)", "rewrite(req.url)"}, // Config cannot repair even an owned opaque rewrite.
		{"req.clone({ url: rewrite(req.url) })", "new HttpRequest('GET', rewrite(req.url))"},
		{"req.clone({ url: rewrite(req.url) })", "rewrite(req)"},
		{"withInterceptors([opaque])", "withInterceptors([opaque, (req, next) => next(req.clone({ url: '/prefix' + req.url }))])"},
		{"provideHttpClient(withInterceptors([opaque]));", "if (enabled) { provideHttpClient(withInterceptors([opaque])); }"},
	} {
		source := strings.Replace(string(original), variant.old, variant.replacement, 1)
		source = strings.Replace(source, "HttpClient,", "HttpRequest, HttpClient,", 1)
		if err := os.WriteFile(opaqueFile, []byte(source), 0644); err != nil {
			t.Fatal(err)
		}
		emit(t, root, s, opt)
		snapshot, err = s.Dump(context.Background(), "fixture")
		if err != nil {
			t.Fatal(err)
		}
		check(snapshot, false)
	}
	if err := os.WriteFile(opaqueFile, original, 0644); err != nil {
		t.Fatal(err)
	}
	emit(t, root, s, opt)
	// With no owner, both calls must consider the opaque registration. Explicit
	// base configuration cannot repair an opaque resource rewrite.
	for _, malformed := range []bool{false, true} {
		if malformed {
			err = os.WriteFile(filepath.Join(root, "angular.json"), []byte("{"), 0644)
		} else {
			err = os.Remove(filepath.Join(root, "angular.json"))
		}
		if err != nil {
			t.Fatal(err)
		}
		_, summary, _ = emit(t, root, s, opt)
		if summary.Parsed != 0 {
			t.Fatal("ownership change reparsed source")
		}
		snapshot, err = s.Dump(context.Background(), "fixture")
		if err != nil {
			t.Fatal(err)
		}
		check(snapshot, true)
	}
}
