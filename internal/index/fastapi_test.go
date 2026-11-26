package index_test

import (
	"context"
	"encoding/json"
	"fmt"
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

func fastapiSnapshot(t *testing.T, s store.Snapshot) map[string][]string {
	t.Helper()
	out := map[string][]string{"routes": {}, "exposes_route": {}, "diagnostics": {}}
	owners := map[string]string{}
	for _, e := range s.Edges {
		if e.Kind == "contains" && strings.HasPrefix(e.Target, "route:") {
			owners[e.Target] = e.Source
		}
		if e.Kind == "exposes_route" {
			out["exposes_route"] = append(out["exposes_route"], e.Source+" -> "+e.Target)
		}
	}
	for _, n := range s.Nodes {
		if n.Kind != "route" {
			continue
		}
		var paths []string
		for _, p := range routePaths(t, n) {
			path := fmt.Sprintf("%s:%v:%s", p["template"], p["conditional"], p["resolution"])
			if p["trailing_slash"] == true {
				path += ":trailing_slash=true"
			}
			paths = append(paths, path)
		}
		handler, _ := n.Attributes["handler"].(string)
		if n.Attributes["handler_inline"] == true {
			handler = "inline"
		}
		if v, ok := n.Attributes["handler_expression"]; ok {
			handler = "expression:" + v.(string)
		}
		out["routes"] = append(out["routes"], fmt.Sprintf("%s | %d | %s | %s | %s", n.Identity, n.Span.StartLine, handler, owners[n.Identity], strings.Join(paths, ",")))
	}
	for _, d := range s.Diagnostics {
		if strings.HasPrefix(d.Code, "route_") {
			out["diagnostics"] = append(out["diagnostics"], d.File+": "+d.Code+": "+d.Message)
		}
		if d.Code == "extract_invalid_record" || d.Code == "parse_failed" || d.Code == "extract_failed" || d.Code == "syntax_error" {
			t.Fatal(d)
		}
	}
	for key := range out {
		sort.Strings(out[key])
	}
	return out
}

func TestFastAPIGoldenAndSettingsReplay(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/fastapi")); err != nil {
		t.Fatal(err)
	}
	s, opt := open(t), options(t)
	data, first, _ := emit(t, root, s, opt)
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	got := fastapiSnapshot(t, snapshot)
	if len(got["routes"]) != 19 {
		for _, row := range got["routes"] {
			t.Log(row)
		}
		for _, d := range got["diagnostics"] {
			t.Log(d)
		}
		t.Fatalf("route count %d", len(got["routes"]))
	}
	golden.Check(t, "../../testdata/fixtures/fastapi.golden.json", got)
	for _, n := range snapshot.Nodes {
		if n.Kind != "route" {
			continue
		}
		source, err := os.ReadFile(filepath.Join(root, n.File))
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(string(source), "\n")
		span := n.Span
		if span.StartLine != span.EndLine {
			t.Fatal("fixture calls should occupy one line", n)
		}
		text := lines[span.StartLine-1][span.StartColumn:span.EndColumn]
		if strings.HasPrefix(text, "@") || !strings.HasSuffix(text, ")") || n.ContentHash != contract.ContentHash([]byte(text)) {
			t.Fatal("registration span/hash", n, text)
		}
		if strings.HasPrefix(strings.TrimSpace(lines[span.StartLine-1]), "@") && span.StartColumn != len(lines[span.StartLine-1])-len(strings.TrimLeft(lines[span.StartLine-1], " \t"))+1 {
			t.Fatal("decorator span includes trivia", n)
		}
	}
	again, warm, writes := emit(t, root, s, opt)
	if string(data) != string(again) || warm.Parsed != 0 || warm.Reused != first.Parsed || writes.Files+writes.Nodes+writes.Edges+writes.Deleted != 0 {
		t.Fatalf("warm replay: %+v %+v", warm, writes)
	}
	config := filepath.Join(root, "app/config.py")
	source, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, []byte(strings.ReplaceAll(string(source), `"/api"`, `"/changed"`)), 0644); err != nil {
		t.Fatal(err)
	}
	_, changed, _ := emit(t, root, s, opt)
	if changed.Parsed != 1 || changed.Reused != first.Parsed-1 {
		t.Fatal(changed)
	}
	after, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, n := range after.Nodes {
		if n.Kind != "route" {
			continue
		}
		for _, p := range routePaths(t, n) {
			if strings.HasPrefix(p["template"].(string), "/api/") {
				t.Fatal("stale settings path", n)
			}
			if strings.HasPrefix(p["template"].(string), "/changed/") {
				seen++
				if p["resolution"] != "settings_default" {
					t.Fatal(p)
				}
			}
		}
	}
	if seen != 4 {
		t.Fatal("settings paths changed", seen)
	}
	fresh := open(t)
	emit(t, root, fresh, opt)
	complete, err := fresh.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, complete) {
		t.Fatal("settings replay differs from fresh index")
	}
	// Handler body edits change the function, but never the route's registration hash.
	main := filepath.Join(root, "app/main.py")
	source, err = os.ReadFile(main)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(main, []byte(strings.ReplaceAll(string(source), "return 1", "return 999")), 0644); err != nil {
		t.Fatal(err)
	}
	emit(t, root, s, opt)
	edited, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	beforeHashes := map[string]string{}
	for _, n := range after.Nodes {
		if n.Kind == "route" {
			beforeHashes[n.Identity] = n.ContentHash
		}
	}
	for _, n := range edited.Nodes {
		if n.Kind == "route" && beforeHashes[n.Identity] != n.ContentHash {
			t.Fatal("handler edit changed route hash", n)
		}
	}
	// Marshal the snapshot as a guard against nonserializable cached attributes.
	if _, err := json.Marshal(edited); err != nil {
		t.Fatal(err)
	}
}

func TestFastAPIConditionalFormsAndShadowModule(t *testing.T) {
	root := t.TempDir()
	source := `from fastapi import FastAPI, APIRouter
app = FastAPI()
r = APIRouter()
@r.get('/x')
def endpoint():
    pass
try:
    app.include_router(r, prefix='/try')
except Exception:
    app.include_router(r, prefix='/except')
else:
    app.include_router(r, prefix='/try-else')
finally:
    app.include_router(r, prefix='/finally')
with context:
    app.include_router(r, prefix='/with')
for item in values:
    app.include_router(r, prefix='/for')
else:
    app.include_router(r, prefix='/for-else')
while enabled:
    app.include_router(r, prefix='/while')
else:
    app.include_router(r, prefix='/while-else')
if enabled:
    app.include_router(r, prefix='/if')
elif other:
    app.include_router(r, prefix='/elif')
else:
    app.include_router(r, prefix='/else')
match value:
    case 1:
        app.include_router(r, prefix='/match')
app.include_router(r, prefix='/expression') if enabled else None
@app.api_route('/set', methods={'get', 'POST'})
def methods():
    pass
@app.trace('/trace')
def traced():
    pass
r.include_router(r, prefix='/cycle')
`
	main := filepath.Join(root, "main.py")
	if err := os.WriteFile(main, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	s, opt := open(t), options(t)
	emit(t, root, s, opt)
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"/try/x": false, "/except/x": true, "/try-else/x": true, "/finally/x": false, "/with/x": false, "/for/x": true, "/for-else/x": true, "/while/x": true, "/while-else/x": true, "/if/x": true, "/elif/x": true, "/else/x": true, "/match/x": true, "/expression/x": true}
	got := map[string]bool{}
	methods := map[string]bool{}
	for _, n := range snapshot.Nodes {
		if n.Kind != "route" {
			continue
		}
		if n.Attributes["pattern"] == "/x" {
			for _, p := range routePaths(t, n) {
				got[p["template"].(string)] = p["conditional"].(bool)
			}
		}
		methods[n.QualifiedName] = true
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("conditional paths: got %v want %v", got, want)
	}
	if !methods["GET /set"] || !methods["POST /set"] || !methods["TRACE /trace"] {
		t.Fatal(methods)
	}
	// A newly scanned shadow changes framework recognition on cached importers.
	if err := os.WriteFile(filepath.Join(root, "fastapi.py"), []byte("class FastAPI:\n    pass\nclass APIRouter:\n    pass\n"), 0644); err != nil {
		t.Fatal(err)
	}
	_, changed, _ := emit(t, root, s, opt)
	if changed.Parsed != 1 {
		t.Fatal(changed)
	}
	snapshot, err = s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range snapshot.Nodes {
		if n.Kind == "route" {
			t.Fatal("scanned fastapi shadow still recognized", n)
		}
	}
}
