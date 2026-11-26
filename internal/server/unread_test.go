package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/contract"
	routepaths "github.com/graphvein/graphvein/internal/extract/routes"
	"github.com/graphvein/graphvein/internal/extract/routeshape"
	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/projectlink"
	"github.com/graphvein/graphvein/internal/store"
)

// contractTableRows reads the rows of a claim contract table: "| ID | Topic |".
func contractTableRows(t *testing.T, file, prefix string) []string {
	t.Helper()
	doc, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, m := range regexp.MustCompile(`(?m)^\| (`+prefix+`-[0-9]+|D) \| ([^|]+) \|`).FindAllSubmatch(doc, -1) {
		out = append(out, string(m[1])+" "+strings.TrimSpace(string(m[2])))
	}
	return out
}

// checkRowTests fails when a row names a test that is not defined in the
// named test files.
func checkRowTests(t *testing.T, tests map[string]string, files ...string) {
	t.Helper()
	var sources strings.Builder
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		sources.Write(data)
	}
	for row, test := range tests {
		if !strings.Contains(sources.String(), "func "+test+"(t *testing.T)") {
			t.Errorf("row %s names test %s, which does not exist", row, test)
		}
	}
}

func TestUnreadContractTable(t *testing.T) {
	documented := contractTableRows(t, "../../docs/contract/claims/couldnt-read.md", "CR")
	var code []string
	tests := map[string]string{}
	for _, r := range unreadRows {
		code = append(code, r.ID+" "+r.Topic)
		tests[r.ID] = r.Test
	}
	if strings.Join(documented, ",") != strings.Join(code, ",") {
		t.Fatalf("couldnt-read.md rows %v\ncode rows %v", documented, code)
	}
	checkRowTests(t, tests, "unread_test.go")
}

// Synthetic snapshot (rows CR-5, CR-9..CR-27, CR-33).

// unreadGraph is a crafted snapshot whose header names indexer.
func unreadGraph(t *testing.T, repository, indexer string) []byte {
	t.Helper()
	return unreadGraphWith(t, repository, indexer, nil)
}

// unreadGraphWith is unreadGraph with extra records (nodes, diagnostics)
// before the footer.
func unreadGraphWith(t *testing.T, repository, indexer string, extra []any) []byte {
	t.Helper()
	hash := contract.ContentHash(nil)
	span := func(line int) *contract.Span {
		return &contract.Span{StartLine: line, StartColumn: 0, EndLine: line, EndColumn: 1}
	}
	var lines []any
	lines = append(lines, contract.Header{Type: "header", ContractVersion: 1, Indexer: contract.Indexer{Name: indexer, Version: "t040"}, Repository: repository, GeneratedAt: "2026-11-24T00:00:00Z"})
	files := map[string]string{
		"src/app.go": "go", "src/zero.go": "go", "src/broken.py": "python", "src/big.ts": "typescript", "src/syntax.ts": "typescript",
		"src/odd.ts": "typescript", "src/nonode.rs": "rust", "src/Main.java": "java", "Dockerfile": "unknown", "web/App.vue": "unknown",
		"docs/notes.md": "markdown", "config/app.json": "json", "tests/fixture.txt": "unknown",
	}
	var paths []string
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		lines = append(lines, contract.File{Type: "file", Path: p, Language: files[p], Precision: "precise", ContentHash: hash})
	}
	id := func(kind, file, qualified string) string {
		s, err := contract.Identity(kind, file, qualified)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	fileAttrs := map[string]map[string]any{
		"src/app.go":    {"unresolved_calls_external": 2, "unresolved_calls_unknown": 1, "go_route_forms_outside_contract": map[string]any{"wrapped_router": 2, "dynamic_method": 1}},
		"src/zero.go":   {"unresolved_calls_external": 0, "unresolved_calls_unknown": 0, "go_route_forms_outside_contract": map[string]any{"x": 0}},
		"src/broken.py": {"unresolved_calls_external": 0, "unresolved_calls_unknown": 0},
		"src/big.ts":    {"unresolved_calls_external": 0, "unresolved_calls_unknown": 0},
		"src/syntax.ts": {"unresolved_calls_external": 5, "unresolved_calls_unknown": 5},
		"src/odd.ts":    {"unresolved_calls_external": "x", "unresolved_calls_unknown": 1},
		"src/Main.java": {"unresolved_calls_external": 3, "unresolved_calls_unknown": 0},
		"web/App.vue":   {},
		"Dockerfile":    {},
	}
	for _, p := range paths {
		attrs, ok := fileAttrs[p]
		if !ok {
			continue
		}
		lines = append(lines, contract.Node{Type: "node", Identity: id("file", p, p), Kind: "file", Level: 3, Name: filepath.Base(p), QualifiedName: p, File: p, Span: span(1), ContentHash: hash, Attributes: attrs})
	}
	node := func(kind, file, qualified, name string, attrs map[string]any) contract.Node {
		if attrs == nil {
			attrs = map[string]any{}
		}
		return contract.Node{Type: "node", Identity: id(kind, file, qualified), Kind: kind, Level: 4, Name: name, QualifiedName: qualified, File: file, Span: span(3), ContentHash: hash, Attributes: attrs}
	}
	shape, err := routeshape.ToAttribute(routeshape.New(4))
	if err != nil {
		t.Fatal(err)
	}
	run := node("function", "src/app.go", "run", "run", nil)
	nodes := []contract.Node{
		run,
		node("method", "src/app.go", "UserService.getUserName", "getUserName", nil),
		node("class", "src/app.go", "UserService", "UserService", nil),
		node("table", "src/app.go", "users", "users", map[string]any{"source": "migration"}),
		node("column", "src/app.go", "users.user_id", "user_id", nil),
		node("dockerfile", "Dockerfile", "Dockerfile", "Dockerfile", nil),
		node("route", "src/app.go", "GET /users", "GET /users", map[string]any{"method": "GET", "paths": []any{map[string]any{"template": "/users", "resolution": "constant"}}}),
		node("route", "src/app.go", "GET /broken", "GET /broken", map[string]any{"method": "GET", "shape": "garbage"}),
		node("route", "src/app.go", "GET /user-names", "GET /user-names", map[string]any{"method": "GET", "shape": shape}),
		node("route", "src/app.go", "GET /only-tests", "GET /only-tests", map[string]any{"method": "GET", "scope": "test"}),
		node("client_call", "src/app.go", "run.GET dyn", "GET dyn", map[string]any{"status": "dynamic"}),
		node("client_call", "src/app.go", "run.GET wrap", "GET wrap", map[string]any{"status": "wrapper"}),
		node("client_call", "src/app.go", "run.GET linked", "GET linked", map[string]any{"status": "linked"}),
		node("client_call", "src/app.go", "run.GET testdyn", "GET testdyn", map[string]any{"status": "dynamic", "scope": "test"}),
	}
	for _, n := range nodes {
		lines = append(lines, n)
	}
	// Extra records go after the nodes: a node or a diagnostic, in the
	// snapshot's record order.
	for _, x := range extra {
		if n, ok := x.(contract.Node); ok {
			lines = append(lines, n)
		}
	}
	edge := func(kind, source, target, file string) contract.Edge {
		return contract.Edge{Type: "edge", Kind: kind, Source: source, Target: target, Provenance: "fact", Confidence: 1, Evidence: []contract.Evidence{{File: file, Span: *span(3)}}}
	}
	lines = append(lines,
		edge("contains", run.Identity, id("client_call", "src/app.go", "run.GET dyn"), "src/app.go"),
		edge("references", run.Identity, id("dockerfile", "Dockerfile", "Dockerfile"), "docs/notes.md"),
	)
	diagnostic := func(file, severity, code, message string) contract.Diagnostic {
		return contract.Diagnostic{Type: "diagnostic", File: file, Severity: severity, Code: code, Message: message}
	}
	lines = append(lines,
		diagnostic("src/broken.py", "error", "parse_failed", "parsing src/broken.py: boom"),
		diagnostic("src/big.ts", "warning", "file_too_large", "file exceeds 1 byte parsing limit"),
		diagnostic("secret.bin", "warning", "file_read", "permission denied"),
		diagnostic("src/syntax.ts", "warning", "syntax_error", "tree-sitter recovered from invalid syntax"),
		diagnostic("vendor", "warning", "scan_read", "permission denied"),
		diagnostic("link", "info", "symlink", "symlink is not followed"),
		diagnostic(".gitignore", "warning", "ignore_read", "permission denied"),
		diagnostic("src/app.go", "warning", "route_dynamic_pattern", "prefix + \"/x\" at 12:4-12:30"),
		diagnostic("src/app.go", "warning", "route_dynamic_pattern", "no span in this message"),
		diagnostic("src/app.go", "warning", "route_dynamic_methods", "methods at 20:1-20:9"),
		diagnostic("src/app.go", "info", "go_routes_unread", "30 Go router forms are outside the route claim contract and claim nothing (x=30)"),
		diagnostic("src/zero.go", "info", "go_routes_unread", "7 Go router forms are outside the route claim contract and claim nothing (y=7)"),
		diagnostic("src/app.go", "info", "unresolved_calls", "99 calls could not be resolved without a type checker (external=90, unknown=9)"),
		diagnostic("src/app.go", "info", "route_unmounted", "not on this page"),
	)
	for _, x := range extra {
		if _, ok := x.(contract.Node); !ok {
			lines = append(lines, x)
		}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	counts := contract.Counts{}
	for _, l := range lines {
		switch l.(type) {
		case contract.File:
			counts.File++
		case contract.Node:
			counts.Node++
		case contract.Edge:
			counts.Edge++
		case contract.Diagnostic:
			counts.Diagnostic++
		}
		if err := enc.Encode(l); err != nil {
			t.Fatal(err)
		}
	}
	counts.Records = counts.File + counts.Node + counts.Edge + counts.Diagnostic
	if err := enc.Encode(contract.Footer{Type: "footer", Counts: counts}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// ingestGraph stores a crafted snapshot.
func ingestGraph(t *testing.T, s store.Store, data []byte) {
	t.Helper()
	if err := ingest.Stream(context.Background(), bytes.NewReader(data), s); err != nil {
		t.Fatal(err)
	}
}

// decodeUnread decodes a one-repository page strictly.
func decodeUnread(t *testing.T, rec interface{ Bytes() []byte }) unreadResponse {
	t.Helper()
	var out unreadResponse
	dec := json.NewDecoder(bytes.NewReader(rec.Bytes()))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		t.Fatal(err, string(rec.Bytes()))
	}
	return out
}

func unreadGet(t *testing.T, opt Options, path string) unreadResponse {
	t.Helper()
	rec := get(t, opt, http.MethodGet, path, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s: %d %s", path, rec.Code, rec.Body.String())
	}
	return decodeUnread(t, rec.Body)
}

func count(t *testing.T, r unreadResponse, name string) int {
	t.Helper()
	v, ok := r.Counts[name]
	if !ok || v == nil {
		t.Fatalf("count %s = %v", name, v)
	}
	return *v
}

func fileList[T any](items []T, file func(T) string) string {
	var out []string
	for _, it := range items {
		out = append(out, file(it))
	}
	return strings.Join(out, ",")
}

func TestUnreadSynthetic(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		ingestGraph(t, w, unreadGraph(t, "fixture", builtinIndexer))
		root := t.TempDir()
		opt := testOptions(root, s)
		r := unreadGet(t, opt, "/api/v0/unread")
		// CR-19: stored files not read plus the unreadable path.
		if got := fileList(r.Files.NotRead.Items, func(f unreadFile) string { return f.File + ":" + f.Code }); got != "secret.bin:file_read,src/big.ts:file_too_large,src/broken.py:parse_failed" {
			t.Errorf("not read %s", got)
		}
		for _, f := range r.Files.NotRead.Items {
			if f.Stored != (f.File != "secret.bin") {
				t.Errorf("stored %v for %s", f.Stored, f.File)
			}
		}
		// CR-20, CR-21.
		if got := fileList(r.Files.SyntaxErrors.Items, func(f unreadFile) string { return f.File }); got != "src/syntax.ts" {
			t.Errorf("syntax %s", got)
		}
		if got := fileList(r.Files.ScannerGaps.Items, func(f unreadScanGap) string { return f.Path + ":" + f.Code }); got != "vendor:scan_read,link:symlink,.gitignore:ignore_read" {
			t.Errorf("scanner %s", got)
		}
		// CR-22, CR-23: no reader and no fact; a fact from a node
		// (Dockerfile) or an edge's evidence (docs/notes.md) keeps a
		// file off the list, and a test file is out of production scope.
		if got := fileList(r.Files.WithoutFacts.Files.Items, func(f unreadWithoutFact) string { return f.File }); got != "config/app.json,web/App.vue" {
			t.Errorf("without facts %s", got)
		}
		if len(r.Files.WithoutFacts.ByExtension) != 2 || r.Files.WithoutFacts.ByExtension[0].Count != 1 {
			t.Errorf("by extension %v", r.Files.WithoutFacts.ByExtension)
		}
		// CR-24..CR-27: only counted languages, only stored integers,
		// never the diagnostic message (99 there).
		cc := r.CodeCalls
		if !cc.Available || *cc.External != 2 || *cc.Unknown != 1 || *cc.FilesCounted != 2 || *cc.FilesNotCounted != 5 {
			t.Errorf("code calls %+v ext %v unk %v counted %v not %v", cc.unreadPart, *cc.External, *cc.Unknown, *cc.FilesCounted, *cc.FilesNotCounted)
		}
		if got := fileList(cc.Files.Items, func(f unresolvedFile) string { return f.File }); got != "src/app.go" {
			t.Errorf("unresolved files %s", got)
		}
		// CR-9, CR-10: spans from the documented message form only.
		rp := r.RoutePaths
		if rp.Dynamic.Total != 3 || rp.RouterForms.Total != 2 || rp.RouterForms.Items[0].Message == "" {
			t.Errorf("route diagnostics %+v", rp)
		}
		// CR-38: the file node's structured counts, never the message (30
		// and 7 there); a zero count is not a valid attribute.
		if rp.Forms == nil || *rp.Forms != 3 || rp.FormFilesNotCounted == nil || *rp.FormFilesNotCounted != 1 || count(t, r, countRouterForms) != 3 || count(t, r, countFormFilesNotCount) != 1 {
			t.Errorf("router forms %v not counted %v", rp.Forms, rp.FormFilesNotCounted)
		}
		app, zero := rp.RouterForms.Items[0], rp.RouterForms.Items[1]
		if app.File != "src/app.go" || app.Forms == nil || *app.Forms != 3 || !equalJSON(app.ByForm, map[string]int{"dynamic_method": 1, "wrapped_router": 2}) || zero.Forms != nil || zero.ByForm != nil {
			t.Errorf("router form items %+v %+v", app, zero)
		}
		lines := map[string]any{}
		for _, d := range rp.Dynamic.Items {
			if d.Line != nil {
				lines[d.Code+":"+*d.Expression] = *d.Line
			} else {
				lines[d.Code+":"+d.Message] = nil
			}
		}
		want := map[string]any{"route_dynamic_pattern:prefix + \"/x\"": 12, "route_dynamic_pattern:no span in this message": nil, "route_dynamic_methods:methods": 20}
		if !equalJSON(lines, want) {
			t.Errorf("dynamic lines %v", lines)
		}
		// CR-11..CR-13: the calls endpoint's status, in scope.
		if r.ClientCalls.Dynamic.Total != 1 || r.ClientCalls.Wrapper.Total != 1 || r.ClientCalls.Dynamic.Items[0].Owner == nil {
			t.Errorf("calls %+v", r.ClientCalls)
		}
		// CR-14..CR-16.
		sh := r.Shapes
		if sh.Routes != 3 || sh.WithoutShape.Total != 1 || sh.ShapeInvalid.Total != 1 || sh.SectionsNotRead.Total != 1 || sh.Sections["security"] != 1 || len(sh.SectionsNotRead.Items[0].Sections) != 7 {
			t.Errorf("shapes %+v", sh)
		}
		if r.DepthCaps.Routes.Total != 0 {
			t.Errorf("depth caps %+v", r.DepthCaps)
		}
		// CR-5: items name a node or a file and line.
		if r.ClientCalls.Dynamic.Items[0].ID == "" || r.Shapes.WithoutShape.Items[0].Line == nil {
			t.Error("items without identity or line")
		}
		// CR-33: other diagnostics are not on the page.
		data, _ := json.Marshal(r)
		for _, absent := range []string{"route_unmounted", "not on this page", "external=90"} {
			if bytes.Contains(data, []byte(absent)) {
				t.Errorf("page shows %s", absent)
			}
		}
	})
}

func equalJSON(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}

func TestUnreadScope(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		ingestGraph(t, w, unreadGraph(t, "fixture", builtinIndexer))
		opt := testOptions(t.TempDir(), s)
		prod := unreadGet(t, opt, "/api/v0/unread?scope=production")
		test := unreadGet(t, opt, "/api/v0/unread?scope=test")
		all := unreadGet(t, opt, "/api/v0/unread?scope=all")
		if count(t, test, countDynamicCalls) != 1 || count(t, all, countDynamicCalls) != 2 || count(t, prod, countDynamicCalls) != 1 {
			t.Errorf("call scope %v %v %v", prod.Counts[countDynamicCalls], test.Counts[countDynamicCalls], all.Counts[countDynamicCalls])
		}
		if count(t, test, countRoutesWithoutShape) != 1 || count(t, all, countRoutesWithoutShape) != 2 {
			t.Error("route scope")
		}
		if count(t, test, countWithoutFacts) != 1 || count(t, all, countWithoutFacts) != 3 {
			t.Error("file scope")
		}
		for _, f := range all.Files.WithoutFacts.Files.Items {
			if f.Test == nil || *f.Test != (f.File == "tests/fixture.txt") {
				t.Errorf("test flag of %s: %v", f.File, f.Test)
			}
		}
		if !all.Files.ScopeApplied || all.Config != configRepository {
			t.Error("scope applied")
		}
		// CR-18: unknown rules keep every file and say so.
		if err := os.WriteFile(filepath.Join(opt.Root, config.File), []byte("version: 1\ntests: [\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		bad := unreadGet(t, opt, "/api/v0/unread?scope=test")
		if bad.Files.ScopeApplied || bad.Config != configError || bad.ConfigError == nil || count(t, bad, countWithoutFacts) != 3 || bad.Files.WithoutFacts.Files.Items[0].Test != nil {
			t.Errorf("unknown rules %+v %v", bad.Files, bad.Counts[countWithoutFacts])
		}
		if rec := get(t, opt, http.MethodGet, "/api/v0/unread?scope=nope", nil); rec.Code != http.StatusBadRequest {
			t.Errorf("bad scope %d", rec.Code)
		}
	})
}

// TestUnreadInvalidConfig: CR-8.
func TestUnreadInvalidConfig(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		ingestGraph(t, w, unreadGraph(t, "fixture", builtinIndexer))
		opt := testOptions(t.TempDir(), s)
		if err := os.WriteFile(filepath.Join(opt.Root, config.File), []byte("version: 1\nnope: [\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		r := unreadGet(t, opt, "/api/v0/unread")
		if r.RoutePaths.Document.Available || r.RoutePaths.Document.Error == nil || r.Counts[countUnplacedRoutes] != nil || r.Counts[countMounts] != nil {
			t.Errorf("document %+v", r.RoutePaths.Document)
		}
		if count(t, r, countDynamicPatterns) != 3 || count(t, r, countFilesNotRead) != 3 || count(t, r, countRouterForms) != 3 {
			t.Error("other parts must still be answered")
		}
	})
}

// TestUnreadExternalRun: CR-2.
func TestUnreadExternalRun(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		ingestGraph(t, w, unreadGraph(t, "fixture", "crafted"))
		r := unreadGet(t, testOptions(t.TempDir(), s), "/api/v0/unread")
		if r.BuiltinRun || r.Files.WithoutFacts.Available || r.CodeCalls.Available || *r.CodeCalls.Reason != reasonExternalIndexer || r.CodeCalls.External != nil {
			t.Errorf("external run %+v %+v", r.Files.WithoutFacts.unreadPart, r.CodeCalls)
		}
		for _, name := range []string{countWithoutFacts, countExternalCalls, countUnknownCalls, countFilesCounted, countFilesNotCounted} {
			if v, ok := r.Counts[name]; !ok || v != nil {
				t.Errorf("count %s = %v, want null", name, v)
			}
		}
		if count(t, r, countFilesNotRead) != 3 {
			t.Error("diagnostic parts still answered")
		}
	})
}

// TestUnreadNotIndexed: CR-3.
func TestUnreadNotIndexed(t *testing.T) {
	backends(t, func(t *testing.T, _, s store.Store) {
		r := unreadGet(t, testOptions(t.TempDir(), s), "/api/v0/unread")
		if r.Index != nil || r.RoutePaths.Document.Available || *r.RoutePaths.Document.Reason != reasonNotIndexed || r.CodeCalls.Available {
			t.Errorf("not indexed %+v", r)
		}
		if len(r.Counts) == 0 {
			t.Fatal("no counts named")
		}
		for name, v := range r.Counts {
			if v != nil {
				t.Errorf("count %s = %d without an index", name, *v)
			}
		}
	})
}

// TestUnreadLimit: CR-4.
func TestUnreadLimit(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		ingestGraph(t, w, unreadGraph(t, "fixture", builtinIndexer))
		opt := testOptions(t.TempDir(), s)
		r := unreadGet(t, opt, "/api/v0/unread?limit=1&scope=all")
		if len(r.Files.NotRead.Items) != 1 || r.Files.NotRead.Total != 3 || !r.Files.NotRead.Truncated || count(t, r, countFilesNotRead) != 3 {
			t.Errorf("limit %+v", r.Files.NotRead)
		}
		for _, bad := range []string{"limit=0", "limit=2001", "limit=x", "limit=1&limit=2", "editor=nope"} {
			if rec := get(t, opt, http.MethodGet, "/api/v0/unread?"+bad, nil); rec.Code != http.StatusBadRequest {
				t.Errorf("%s: %d", bad, rec.Code)
			}
		}
	})
}

// TestUnreadWording: CR-31 (no count adds units) and CR-32 (no "unread"
// wording outside the files not read).
func TestUnreadWording(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		ingestGraph(t, w, unreadGraph(t, "fixture", builtinIndexer))
		rec := get(t, testOptions(t.TempDir(), s), http.MethodGet, "/api/v0/unread?scope=all", nil)
		var v map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"total", "count"} {
			if _, ok := v[key]; ok {
				t.Errorf("page has a top-level %s", key)
			}
		}
		for name := range v["counts"].(map[string]any) {
			if strings.Contains(name, "total") || name == "all" {
				t.Errorf("count %s mixes units", name)
			}
		}
		var keys []string
		var walk func(string, any)
		walk = func(at string, x any) {
			switch x := x.(type) {
			case map[string]any:
				for k, val := range x {
					keys = append(keys, at+"."+k)
					walk(at+"."+k, val)
				}
			case []any:
				for _, val := range x {
					walk(at, val)
				}
			}
		}
		walk("", v)
		for _, k := range keys {
			if strings.Contains(strings.ToLower(k), "unread") && !strings.HasPrefix(k, ".files.not_read") {
				t.Errorf("key %s says unread", k)
			}
		}
	})
}

// Fixtures: the generated document's lists (CR-6, CR-7), calls and
// shapes with depth caps on real index runs.
func TestUnreadFixtureGolden(t *testing.T) {
	var mu sync.Mutex
	bodies := map[string]map[string]string{}
	backends(t, func(t *testing.T, w, s store.Store) {
		got := map[string]any{}
		raw := map[string]string{}
		for _, fixture := range []string{"api-section", "unknown-prefix", "express-shape"} {
			var root string
			if fixture == "api-section" {
				root = apiFixtureRoot(t)
			} else {
				root = t.TempDir()
				if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/"+fixture)); err != nil {
					t.Fatal(err)
				}
			}
			indexRepository(t, root, w, fixture)
			opt := testOptions(root, s)
			opt.Repository = fixture
			rec := get(t, opt, http.MethodGet, "/api/v0/unread?scope=all", nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("%s: %d %s", fixture, rec.Code, rec.Body.String())
			}
			decodeUnread(t, rec.Body)
			v := normalizeAPI(t, rec.Body.Bytes())
			data, _ := json.Marshal(v)
			text := golden.ReplaceRoot(string(data), filepath.ToSlash(root))
			raw[fixture] = text
			var norm any
			if err := json.Unmarshal([]byte(text), &norm); err != nil {
				t.Fatal(err)
			}
			got[fixture] = norm
		}
		golden.Check(t, filepath.Join("testdata", "unread-fixtures.golden.json"), got)
		mu.Lock()
		bodies[t.Name()] = raw
		mu.Unlock()
	})
	if a, b := bodies["TestUnreadFixtureGolden/sqlite"], bodies["TestUnreadFixtureGolden/postgres"]; a != nil && b != nil && !equalJSON(a, b) {
		t.Error("SQLite and Postgres answers differ")
	}
}

// TestUnreadProject: CR-28..CR-30.
func TestUnreadProject(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		ctx := context.Background()
		root := copyProjectFixture(t)
		project, err := config.LoadProject(root)
		if err != nil {
			t.Fatal(err)
		}
		indexRepository(t, filepath.Join(root, "frontend"), w, "frontend")
		indexRepository(t, filepath.Join(root, "backend"), w, "backend")
		if _, _, err := projectlink.Run(ctx, w, project, projectlink.ManifestHash(project)); err != nil {
			t.Fatal(err)
		}
		opt := projectOptions(project, s)
		rec := get(t, opt, http.MethodGet, "/api/v0/unread?scope=all", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("%d %s", rec.Code, rec.Body.String())
		}
		var p unreadProjectResponse
		dec := json.NewDecoder(bytes.NewReader(rec.Body.Bytes()))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&p); err != nil {
			t.Fatal(err)
		}
		if p.Project != "users" || len(p.Members) != 2 || p.Members[0].Repository != "frontend" || len(p.NotAvailable) != 0 {
			t.Fatalf("project page %+v", p)
		}
		for name, total := range p.Totals {
			sum := 0
			for _, m := range p.Members {
				sum += *m.Counts[name]
			}
			if total == nil || *total != sum {
				t.Errorf("project total %s = %v, members sum %d", name, total, sum)
			}
		}
		// CR-28: one member, byte for byte its page.
		member := unreadGet(t, opt, "/api/v0/unread?scope=all&repository=backend")
		if !equalJSON(member, p.Members[1]) {
			t.Error("member page differs from its part of the project page")
		}
		// CR-29: a member without an index makes its counts unknown at
		// project scope, never zero.
		withEmpty := project
		withEmpty.Members = append(append([]config.Member(nil), project.Members...), config.Member{Repository: "empty", Checkout: false})
		e := unreadProjectGet(t, projectOptions(withEmpty, s))
		if e.Totals[countFilesNotRead] != nil || len(e.NotAvailable[countFilesNotRead]) != 1 || e.NotAvailable[countFilesNotRead][0] != "empty" {
			t.Errorf("not available %v %v", e.Totals[countFilesNotRead], e.NotAvailable)
		}
		// CR-30: no checkout: store answers, default rules, no links.
		noCheckout := project
		noCheckout.Members = append([]config.Member(nil), project.Members...)
		noCheckout.Members[1].Checkout = false
		nc := unreadProjectGet(t, projectOptions(noCheckout, s)).Members[1]
		if nc.Config != configDefaults || nc.IDE.Available && hasLink(nc) {
			t.Errorf("no checkout %s %+v", nc.Config, nc.IDE)
		}
	})
}

func unreadProjectGet(t *testing.T, opt Options) unreadProjectResponse {
	t.Helper()
	rec := get(t, opt, http.MethodGet, "/api/v0/unread?scope=all", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	var p unreadProjectResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	return p
}

// hasLink reports whether any item of a page carries an editor link.
func hasLink(r unreadResponse) bool {
	data, _ := json.Marshal(r)
	return bytes.Contains(data, []byte(`"link":`))
}

func TestParseMessageSpan(t *testing.T) {
	for msg, want := range map[string]string{
		"prefix + \"/x\" at 12:4-12:30":   "prefix + \"/x\"|12",
		"a at b at 3:0-4:2":               "a at b|3",
		"multi\nline at 2:1-3:0":          "multi\nline|2",
		"no span":                         "",
		"x at 0:0-0:1":                    "",
		"x at 5:0-4:1":                    "",
		"x at 1:2-3:4 trailing":           "",
		"x at 99999999999999999999:1-1:1": "",
	} {
		expr, span, ok := parseMessageSpan(msg)
		got := ""
		if ok {
			got = expr + "|" + strconvItoa(span.StartLine)
		}
		if got != want {
			t.Errorf("parseMessageSpan(%q) = %q, want %q", msg, got, want)
		}
	}
}

func strconvItoa(n int) string {
	data, _ := json.Marshal(n)
	return string(data)
}

// TestUnreadScannerSkips: CR-34 (a binary or non-UTF-8 file the rules
// keep is listed as not read, stored: false) and CR-35 (an ignored path,
// a default name or a nested repository is never listed). The
// non-regular case (a FIFO) is checked by the scanner's Unix test.
func TestUnreadScannerSkips(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		root := t.TempDir()
		for p, body := range map[string]string{
			".gitignore":           "ignored.bin\n",
			".graphveinignore":     "private/\n",
			"main.go":              "package main\n\nfunc main() {}\n",
			"assets/logo.png":      "\x89PNG\r\n\x1a\n\x00\x00",
			"legacy.py":            "x = '\xe9'\n",
			"ignored.bin":          "a\x00b",
			"private/secret.png":   "a\x00b",
			"node_modules/x.png":   "a\x00b",
			"dist/latin.js":        "'\xe9'",
			"nested/.git/HEAD":     "ref: refs/heads/main\n",
			"nested/inner.png":     "a\x00b",
			"nested/inner_utf.txt": "\xff",
		} {
			full := filepath.Join(root, filepath.FromSlash(p))
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		indexRoot(t, root, w)
		r := unreadGet(t, testOptions(root, s), "/api/v0/unread?scope=all")
		got := fileList(r.Files.NotRead.Items, func(f unreadFile) string {
			return f.File + ":" + f.Code + ":" + f.Category + ":" + strconvItoa(map[bool]int{false: 0, true: 1}[f.Stored])
		})
		if got != "assets/logo.png:file_binary:not_code:0,legacy.py:file_not_utf8:code_with_reader:0" {
			t.Errorf("not read %s", got)
		}
		if r.Files.ScannerGaps.Total != 0 {
			t.Errorf("scanner gaps %+v", r.Files.ScannerGaps.Items)
		}
		// CR-35: nothing excluded by rule reaches any list or count.
		data, _ := json.Marshal(r)
		for _, absent := range []string{"ignored.bin", "secret.png", "node_modules", "dist/", "nested"} {
			if bytes.Contains(data, []byte(absent)) {
				t.Errorf("page names %s", absent)
			}
		}
		// The records are stored diagnostics on both backends.
		diagnostics, err := s.DiagnosticsWithCodes(context.Background(), "fixture", []string{"file_binary", "file_not_utf8"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		severities := map[string]string{}
		for _, d := range diagnostics {
			severities[d.File+":"+d.Code] = d.Severity
		}
		if !equalJSON(severities, map[string]string{"assets/logo.png:file_binary": "info", "legacy.py:file_not_utf8": "warning"}) {
			t.Errorf("stored %v", severities)
		}
	})
}

// TestUnreadCategories: CR-36 (the extension table equals the claim
// file's, and each item's category follows it) and CR-37 (every category
// counted, summing to the list total).
func TestUnreadCategories(t *testing.T) {
	doc, err := os.ReadFile("../../docs/contract/claims/couldnt-read.md")
	if err != nil {
		t.Fatal(err)
	}
	documented := map[string]string{}
	rows := regexp.MustCompile(`(?m)^\| (code_no_reader|not_code) \| ([^|]+) \|$`).FindAllSubmatch(doc, -1)
	if len(rows) != 2 {
		t.Fatalf("category rows %d", len(rows))
	}
	for _, m := range rows {
		var exts []string
		for _, e := range strings.Split(string(m[2]), ",") {
			e = strings.TrimSpace(e)
			if _, dup := documented[e]; dup || e != strings.ToLower(e) || !strings.HasPrefix(e, ".") {
				t.Errorf("extension %q", e)
			}
			documented[e] = string(m[1])
			exts = append(exts, e)
		}
		if !sort.StringsAreSorted(exts) {
			t.Errorf("%s extensions not sorted", m[1])
		}
	}
	if !equalJSON(documented, fileCategories) {
		t.Errorf("claim file %v\ncode %v", documented, fileCategories)
	}
	for ext := range fileCategories {
		if index.ReadsPath("x" + ext) {
			t.Errorf("%s has a reader and cannot be %s", ext, fileCategories[ext])
		}
	}
	for file, want := range map[string]string{
		"web/App.VUE": categoryCode, "a/b.py": categoryReader, "x.tsx": categoryReader, "logo.png": categoryNotCode,
		"README.md": categoryNotCode, "Dockerfile": categoryOther, "x.wasm": categoryOther, ".gitignore": categoryOther, "a.d/b": categoryOther,
	} {
		if got := fileCategory(file); got != want {
			t.Errorf("%s: %s, want %s", file, got, want)
		}
	}
	backends(t, func(t *testing.T, w, s store.Store) {
		ingestGraph(t, w, unreadGraph(t, "fixture", builtinIndexer))
		r := unreadGet(t, testOptions(t.TempDir(), s), "/api/v0/unread?scope=all&limit=1")
		check := func(name string, counts []categoryCount, total int, items []string, categories []string) {
			sum := 0
			var names []string
			for _, c := range counts {
				sum += c.Count
				names = append(names, c.Category)
			}
			if sum != total || strings.Join(names, ",") != strings.Join(fileCategoryNames, ",") {
				t.Errorf("%s by category %v, total %d", name, counts, total)
			}
			for i, f := range items {
				if categories[i] != fileCategory(f) {
					t.Errorf("%s %s: %s", name, f, categories[i])
				}
			}
		}
		var files, cats []string
		for _, f := range r.Files.WithoutFacts.Files.Items {
			files, cats = append(files, f.File), append(cats, f.Category)
		}
		check("without facts", r.Files.WithoutFacts.ByCategory, r.Files.WithoutFacts.Files.Total, files, cats)
		if !equalJSON(r.Files.WithoutFacts.ByCategory, []categoryCount{{categoryReader, 0}, {categoryCode, 1}, {categoryNotCode, 2}, {categoryOther, 0}}) {
			t.Errorf("without facts %v", r.Files.WithoutFacts.ByCategory)
		}
		files, cats = nil, nil
		for _, f := range r.Files.NotRead.Items {
			files, cats = append(files, f.File), append(cats, f.Category)
		}
		check("not read", r.Files.NotRead.ByCategory, r.Files.NotRead.Total, files, cats)
		if !equalJSON(r.Files.NotRead.ByCategory, []categoryCount{{categoryReader, 2}, {categoryCode, 0}, {categoryNotCode, 0}, {categoryOther, 1}}) {
			t.Errorf("not read %v", r.Files.NotRead.ByCategory)
		}
	})
}

// TestUnreadReader: CR-39, the in-process reader answers the page with
// the same JSON as HTTP.
func TestUnreadReader(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		root := fixtureRoot(t)
		indexRoot(t, root, w)
		opt := testOptions(root, s)
		r := NewReader(opt)
		defer r.Close()
		got, err := r.Read(context.Background(), unreadPath, nil)
		if err != nil {
			t.Fatal(err)
		}
		want := get(t, opt, http.MethodGet, unreadPath, nil)
		var a, b any
		if json.Unmarshal(got, &a) != nil || json.Unmarshal(want.Body.Bytes(), &b) != nil || !jsonEqual(a, b) {
			t.Errorf("reader %s\nHTTP %s", got, want.Body.String())
		}
	})
}

// TestUnreadGapDiagnostics: CR-40..CR-43, the stored diagnostics of
// records dropped, Go route recognition failures, mounts whose router is
// not known and TypeScript structure outside the contract, listed with
// their messages and counted, never parsed for a count.
func TestUnreadGapDiagnostics(t *testing.T) {
	diagnostic := func(file, severity, code, message string) contract.Diagnostic {
		return contract.Diagnostic{Type: "diagnostic", File: file, Severity: severity, Code: code, Message: message}
	}
	extra := []any{
		diagnostic("src/odd.ts", "error", "extract_invalid_record", "src/odd.ts: node function f: invalid identity"),
		diagnostic("", "error", "extract_invalid_record", "diagnostic x file=\"\": invalid code"),
		diagnostic("tests/helper.ts", "error", "extract_invalid_record", "tests/helper.ts: edge calls: bad"),
		diagnostic("src/app.go", "warning", "go_routes_failed", "src/app.go: Go route linking failed: boom"),
		diagnostic("", "warning", "go_routes_failed", "Go generated-server recognition failed: boom"),
		diagnostic("src/app.go", "info", "route_mount_unresolved", "app.use(routerFrom(cfg)) at 7:2-7:30"),
		diagnostic("src/app.go", "info", "route_mount_unresolved", "a message without a span"),
		diagnostic("src/odd.ts", "info", "ts_structure_unread", "3 TypeScript or JavaScript declarations or imports are outside the structural claim contract and claim nothing (namespace=3)"),
		diagnostic("tests/helper.ts", "info", "ts_structure_unread", "1 TypeScript or JavaScript declarations or imports are outside the structural claim contract and claim nothing (enum=1)"),
		// Row CR-44: route registrations whose path the Go linker
		// withholds (owncast's root-router catch-all; the refusedWhy form).
		diagnostic("src/app.go", "info", "route_mount_unresolved", `ANY "/*" (another pattern of the ServeMux, its redirect or the forwarding closure may answer this path; no path is claimed through it) at 90:2-90:40`),
		diagnostic("src/app.go", "info", "route_mount_unresolved", `GET "/users" (a route of the router it is mounted on always takes this path; no path is claimed through it) at 17:1-17:30`),
		// The same text in a file of another language is a mount (the
		// writer is Go only).
		diagnostic("src/odd.ts", "info", "route_mount_unresolved", `GET "/x" (looks like a registration) at 4:1-4:9`),
	}
	backends(t, func(t *testing.T, w, s store.Store) {
		ingestGraph(t, w, unreadGraphWith(t, "fixture", builtinIndexer, extra))
		opt := testOptions(t.TempDir(), s)
		r := unreadGet(t, opt, "/api/v0/unread")
		files := func(l unreadList[unreadFile]) string {
			return fileList(l.Items, func(f unreadFile) string { return f.File + ":" + f.Code })
		}
		// CR-40: one item per diagnostic in scope; a run-level record (no
		// file) is in every scope, the test file only outside production.
		if got := files(r.Files.RecordsDropped); got != ":extract_invalid_record,src/odd.ts:extract_invalid_record" {
			t.Errorf("records dropped %s", got)
		}
		// CR-43.
		if got := files(r.Files.StructureOutside); got != "src/odd.ts:ts_structure_unread" || r.Files.StructureOutside.Items[0].Message != extra[7].(contract.Diagnostic).Message {
			t.Errorf("structure %s", got)
		}
		// CR-41: verbatim, no line, the run-level one included.
		failed := r.RoutePaths.Failed
		if failed.Total != 2 || failed.Items[0].File != "" || failed.Items[1].File != "src/app.go" || failed.Items[1].Line != nil || failed.Items[1].Message != "src/app.go: Go route linking failed: boom" {
			t.Errorf("failed %+v", failed)
		}
		// CR-42: the span form gives the line and expression (CR-9's parser).
		mounts := r.RoutePaths.MountsUnresolved
		if mounts.Total != 3 || mounts.Items[0].Line != nil || mounts.Items[1].Line == nil || *mounts.Items[1].Line != 7 || *mounts.Items[1].Expression != "app.use(routerFrom(cfg))" || mounts.Items[2].File != "src/odd.ts" {
			t.Errorf("mounts unresolved %+v", mounts)
		}
		// CR-44: the registrations, apart from the mounts, by line.
		withheld := r.RoutePaths.RoutesWithheld
		if withheld.Total != 2 || *withheld.Items[0].Line != 17 || *withheld.Items[1].Line != 90 || *withheld.Items[1].Expression != `ANY "/*" (another pattern of the ServeMux, its redirect or the forwarding closure may answer this path; no path is claimed through it)` {
			t.Errorf("routes withheld %+v", withheld)
		}
		for name, want := range map[string]int{countRecordsDropped: 2, countStructureOutside: 1, countRouteFailures: 2, countMountsUnresolved: 3, countRoutesWithheld: 2} {
			if v := r.Counts[name]; v == nil || *v != want {
				t.Errorf("count %s = %v, want %d", name, v, want)
			}
		}
		all := unreadGet(t, opt, "/api/v0/unread?scope=all")
		if got := files(all.Files.RecordsDropped); got != ":extract_invalid_record,src/odd.ts:extract_invalid_record,tests/helper.ts:extract_invalid_record" {
			t.Errorf("records dropped, all %s", got)
		}
		if all.Files.StructureOutside.Total != 2 {
			t.Errorf("structure, all %d", all.Files.StructureOutside.Total)
		}
		// Every count equals the stored diagnostics of its code (scope all).
		for code, got := range map[string]int{"extract_invalid_record": all.Files.RecordsDropped.Total, "go_routes_failed": all.RoutePaths.Failed.Total, "route_mount_unresolved": all.RoutePaths.MountsUnresolved.Total + all.RoutePaths.RoutesWithheld.Total, "ts_structure_unread": all.Files.StructureOutside.Total} {
			stored, err := s.DiagnosticsWithCodes(context.Background(), "fixture", []string{code}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(stored) != got {
				t.Errorf("%s: listed %d, stored %d", code, got, len(stored))
			}
		}
	})
	// Without these diagnostics every list is empty and counted as 0.
	backends(t, func(t *testing.T, w, s store.Store) {
		ingestGraph(t, w, unreadGraph(t, "fixture", builtinIndexer))
		r := unreadGet(t, testOptions(t.TempDir(), s), "/api/v0/unread")
		if r.RoutePaths.Failed.Total != 0 || r.RoutePaths.MountsUnresolved.Total != 0 || r.RoutePaths.RoutesWithheld.Total != 0 || r.Counts[countRoutesWithheld] == nil || r.Files.RecordsDropped.Total != 0 || r.Files.StructureOutside.Total != 0 || r.Counts[countRouteFailures] == nil {
			t.Errorf("empty %+v", r.Counts)
		}
	})
}

// TestWithheldRouteForm: row CR-44 tells route registrations from mounts
// by the message form only the Go registration writer uses. Over every
// route_mount_unresolved message of the fixture goldens, the form is
// found only in Go files, and every mount form (Express, FastAPI, axum
// and the Go mount writers) stays a mount.
func TestWithheldRouteForm(t *testing.T) {
	goldens, err := filepath.Glob("../../testdata/fixtures/*.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	line := regexp.MustCompile(`^"(.*): info: route_mount_unresolved: (.*)",?$`)
	registrations, mounts := 0, 0
	for _, g := range goldens {
		if strings.Contains(g, ".openapi.") {
			continue
		}
		data, err := os.ReadFile(g)
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range strings.Split(string(data), "\n") {
			m := line.FindStringSubmatch(strings.TrimSpace(l))
			if m == nil {
				continue
			}
			var message string
			if err := json.Unmarshal([]byte(`"`+m[2]+`"`), &message); err != nil {
				t.Fatalf("%s: %v", g, err)
			}
			d := contract.Diagnostic{File: m[1], Code: codeMountUnresolved, Message: message}
			if !routepaths.IsWithheldRegistration(d) {
				mounts++
				if routepaths.IsWithheldForm(message) {
					t.Errorf("%s: registration form outside a Go file: %s", g, l)
				}
				continue
			}
			registrations++
			// The writers' reasons: a gate of the ServeMux or a route of
			// the router mounted on.
			if !strings.Contains(message, "; no path is claimed through it)") {
				t.Errorf("%s: unexpected registration message %q", g, message)
			}
		}
	}
	// go-routes and unknown-prefix-go hold both writers' forms.
	if registrations == 0 || mounts == 0 {
		t.Fatalf("registrations %d, mounts %d", registrations, mounts)
	}
	t.Logf("registrations %d, mounts %d", registrations, mounts)
}

// TestRoutePatternOnlyLabel: a route with neither a full path nor an
// unknown-prefix path (owncast's ANY "/*" on the root router) is labelled
// by its pattern marked "(path not known)" in search (JS-4) and in the
// shape lists (CR-16), never by the bare pattern (ADR 0089 Revision 4).
func TestRoutePatternOnlyLabel(t *testing.T) {
	hash := contract.ContentHash(nil)
	route := func(name, pattern string, line int, attrs map[string]any) contract.Node {
		id, err := contract.Identity("route", "src/app.go", name)
		if err != nil {
			t.Fatal(err)
		}
		a := map[string]any{"method": strings.Fields(name)[0], "pattern": pattern}
		for k, v := range attrs {
			a[k] = v
		}
		return contract.Node{Type: "node", Identity: id, Kind: "route", Level: 4, Name: name, QualifiedName: name, File: "src/app.go", Span: &contract.Span{StartLine: line, StartColumn: 2, EndLine: line, EndColumn: 40}, ContentHash: hash, Attributes: a}
	}
	// No path and no stored fact about why: "path not known".
	unknown := route("GET /later", "/later", 70, nil)
	// Row CR-44: owncast's root-router catch-all, whose registration
	// diagnostic names the route's own span: "path withheld".
	withheld := route("ANY /*", "/*", 90, nil)
	// The same message at another span names no route: a route there
	// keeps "path not known".
	elsewhere := route("GET /other", "/other", 95, nil)
	// An earlier mount takes every composed path: "shadowed".
	shadowed := route("GET /taken", "/taken", 99, map[string]any{"shadowed_paths": []any{map[string]any{"path": "/taken", "by": "app.mount"}}})
	extra := []any{unknown, withheld, elsewhere, shadowed,
		contract.Diagnostic{Type: "diagnostic", File: "src/app.go", Severity: "info", Code: "route_mount_unresolved", Message: `ANY "/*" (another pattern of the ServeMux, its redirect or the forwarding closure may answer this path; no path is claimed through it) at 90:2-90:40`},
		contract.Diagnostic{Type: "diagnostic", File: "src/app.go", Severity: "info", Code: "route_mount_unresolved", Message: `GET "/other" (a route of the router it is mounted on always takes this path; no path is claimed through it) at 95:2-95:41`},
	}
	want := map[string]struct{ label, flag string }{
		unknown.Identity:   {"/later (path not known)", "unmounted"},
		withheld.Identity:  {"/* (path withheld)", "path_withheld"},
		elsewhere.Identity: {"/other (path not known)", "unmounted"},
		shadowed.Identity:  {"/taken (shadowed)", "shadowed"},
	}
	backends(t, func(t *testing.T, w, s store.Store) {
		ingestGraph(t, w, unreadGraphWith(t, "fixture", builtinIndexer, extra))
		opt := testOptions(t.TempDir(), s)
		r := searchGet(t, opt, "/api/v0/search?q=/&kinds=route")
		seen := map[string]bool{}
		for _, it := range group(t, r.Groups, searchRoute).Items {
			if w, ok := want[*it.ID]; ok {
				seen[*it.ID] = true
				if it.Label != w.label || it.Path != nil || it.PrefixUnknown != nil {
					t.Errorf("search label %q, want %q", it.Label, w.label)
				}
			}
		}
		u := unreadGet(t, opt, "/api/v0/unread")
		for _, it := range u.Shapes.WithoutShape.Items {
			if w, ok := want[it.ID]; ok {
				seen["shape "+it.ID] = true
				if it.Label != w.label {
					t.Errorf("shape list label %q, want %q", it.Label, w.label)
				}
			}
		}
		var list endpointsResponse
		decodeInto(t, get(t, opt, http.MethodGet, "/api/v0/api/endpoints?scope=all", nil), &list)
		for _, tr := range list.Transports {
			raw, _ := json.Marshal(tr.Endpoints)
			var rows []struct {
				ID    string   `json:"id"`
				Flags []string `json:"flags"`
			}
			if err := json.Unmarshal(raw, &rows); err != nil {
				t.Fatal(err)
			}
			for _, row := range rows {
				if w, ok := want[row.ID]; ok {
					seen["flag "+row.ID] = true
					if !slices.Contains(row.Flags, w.flag) || w.flag != "unmounted" && slices.Contains(row.Flags, "unmounted") {
						t.Errorf("flags %v, want %s", row.Flags, w.flag)
					}
				}
			}
		}
		for id, w := range want {
			var detail struct {
				PathWithheld bool `json:"path_withheld"`
			}
			decodeInto(t, get(t, opt, http.MethodGet, "/api/v0/api/endpoint?scope=all&id="+url.QueryEscape(id), nil), &detail)
			if detail.PathWithheld != (w.flag == "path_withheld") {
				t.Errorf("detail %s path_withheld %t", id, detail.PathWithheld)
			}
		}
		if len(seen) != 3*len(want) {
			t.Errorf("routes seen %v", seen)
		}
	})
}
