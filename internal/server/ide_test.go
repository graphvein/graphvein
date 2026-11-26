package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/query"
	"github.com/graphvein/graphvein/internal/store"
)

// The editor repository has one route and one call whose line holds an
// emoji (two UTF-16 units, one code point, four bytes) and an é (one unit,
// one code point, two bytes) before the call, so every column unit and
// base gives a different column: byte 22, code point 18, UTF-16 19.
const (
	editorRoute = "route:server/app.ts#GET%20%2Femoji"
	editorCall  = "client_call:client.ts#load.GET%20%27%2Femoji%27"
)

func editorRoot(t *testing.T, w store.Store) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"server/package.json": "{\"name\": \"editor-server\", \"private\": true, \"dependencies\": {\"express\": \"4.21.0\"}}\n",
		"server/app.ts":       "import express from 'express';\n\nconst app = express();\n\nexport function emoji() {}\n\napp.get('/emoji', emoji);\n",
		"client.ts":           "export function load() {\n  /* \U0001F600\u00e9 */ return fetch('/emoji');\n}\n",
	}
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	indexRoot(t, root, w)
	return root
}

// editorPaths are the four API and source endpoints that present nodes
// (TestSchemaEditorParameter covers the schema ones), each asked for a
// body that presents editorCall.
var editorPaths = []string{
	"/api/v0/api/endpoint?id=" + url.QueryEscape(editorRoute),
	"/api/v0/api/calls?transport=http&status=linked",
	"/api/v0/api/flow?id=" + url.QueryEscape(editorRoute),
	"/api/v0/source?id=" + url.QueryEscape(editorCall),
}

// editorBody is the part of a node-presenting response these tests read:
// the ide status and the link of editorCall.
type editorBody struct {
	IDE  ideStatus
	Link map[string]any // nil when the call has no link
	Raw  string
}

func getEditorBody(t *testing.T, opt Options, path string) editorBody {
	t.Helper()
	rec := get(t, opt, http.MethodGet, path, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s: status %d %s", path, rec.Code, rec.Body.String())
	}
	var top struct {
		IDE ideStatus `json:"ide"`
	}
	dec := json.NewDecoder(bytes.NewReader(rec.Body.Bytes()))
	if err := dec.Decode(&top); err != nil {
		t.Fatal(err)
	}
	var all any
	if err := json.Unmarshal(rec.Body.Bytes(), &all); err != nil {
		t.Fatal(err)
	}
	out := editorBody{IDE: top.IDE, Raw: rec.Body.String()}
	found := false
	var walk func(v any)
	walk = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			if v["identity"] == editorCall {
				if _, ok := v["ide"]; ok {
					found = true
					out.Link, _ = v["ide"].(map[string]any)
				}
			}
			for _, c := range v {
				walk(c)
			}
		case []any:
			for _, c := range v {
				walk(c)
			}
		}
	}
	walk(all)
	if !found {
		t.Fatalf("%s: no presented %s in %s", path, editorCall, rec.Body.String())
	}
	return out
}

func writeConfig(t *testing.T, root, content string) {
	t.Helper()
	path := filepath.Join(root, "graphvein.yaml")
	if content == "" {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		return
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func strp(s string) *string { return &s }

// TestEditorParameter checks the editor parameter on the four endpoints:
// each preset's link format, unit and base; the parameter beats a custom
// repository template and label; the precedence fields; a load error with
// a requested editor still builds links (K3 A).
func TestEditorParameter(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		root := editorRoot(t, w)
		opt := testOptions(root, s)
		abs := absLinkPath(root)
		plain := filepath.ToSlash(root)
		// The emoji before the call is two UTF-16 units and one code point:
		// vscode and jetbrains count UTF-16 (bases 1 and 0), zed code points.
		links := map[string]string{
			"vscode":    "vscode://file" + abs + "/client.ts:2:20",
			"jetbrains": "idea://open?file=" + plain + "/client.ts&line=2&column=19",
			"zed":       "zed://file" + plain + "/client.ts:2:19",
		}
		for _, tc := range []struct {
			name, config, editor string
			want                 ideStatus
			link                 string
		}{
			{"builtin", "", "", ideStatus{Available: true, Label: strp("Open in VS Code"), Editor: strp("vscode"), Source: ideSourceBuiltin}, links["vscode"]},
			{"builtin-empty-mapping", "version: 1\nide: {}\n", "", ideStatus{Available: true, Label: strp("Open in VS Code"), Editor: strp("vscode"), Source: ideSourceBuiltin}, links["vscode"]},
			{"repository", "version: 1\nide: zed\n", "", ideStatus{Available: true, Label: strp("Open in Zed"), Editor: strp("zed"), Source: ideSourceRepository}, links["zed"]},
			{"repository-custom", "version: 1\nide: {link_template: 'x://{rel_path}:{line}:{column}', label: Open in X}\n", "", ideStatus{Available: true, Label: strp("Open in X"), Source: ideSourceRepository}, "x://client.ts:2:20"},
			{"request-zed", "", "zed", ideStatus{Available: true, Label: strp("Open in Zed"), Editor: strp("zed"), Source: ideSourceRequest}, links["zed"]},
			{"request-jetbrains", "", "jetbrains", ideStatus{Available: true, Label: strp("Open in IntelliJ IDEA"), Editor: strp("jetbrains"), Source: ideSourceRequest}, links["jetbrains"]},
			{"request-vscode", "version: 1\nide: zed\n", "vscode", ideStatus{Available: true, Label: strp("Open in VS Code"), Editor: strp("vscode"), Source: ideSourceRequest}, links["vscode"]},
			{"request-beats-custom", "version: 1\nide: {link_template: 'x://{rel_path}:{line}:{column}', label: Open in X, column_unit: byte, column_base: 0}\n", "zed", ideStatus{Available: true, Label: strp("Open in Zed"), Editor: strp("zed"), Source: ideSourceRequest}, links["zed"]},
		} {
			writeConfig(t, root, tc.config)
			for _, path := range editorPaths {
				if tc.editor != "" {
					path += "&editor=" + tc.editor
				}
				b := getEditorBody(t, opt, path)
				if !reflect.DeepEqual(b.IDE, tc.want) {
					t.Errorf("%s %s: ide %+v, want %+v", tc.name, path, b.IDE, tc.want)
				}
				if b.Link == nil || b.Link["link"] != tc.link {
					t.Errorf("%s %s: link %v, want %s", tc.name, path, b.Link, tc.link)
				}
			}
		}

		// A configuration that fails to load: no links without a request
		// (the error is reported), links of the requested editor with the
		// error kept (K3 A).
		writeConfig(t, root, "version: 1\nide: emacs\n")
		for _, path := range editorPaths {
			b := getEditorBody(t, opt, path)
			if b.IDE.Available || b.IDE.Error == nil || b.IDE.Editor != nil || b.IDE.Label != nil || b.IDE.Source != ideSourceRepository || b.Link != nil {
				t.Errorf("load error %s: ide %+v link %v", path, b.IDE, b.Link)
			}
			b = getEditorBody(t, opt, path+"&editor=zed")
			if !b.IDE.Available || b.IDE.Error == nil || !strings.Contains(*b.IDE.Error, `unknown ide editor "emacs"`) || b.IDE.Source != ideSourceRequest || b.Link == nil || b.Link["link"] != links["zed"] {
				t.Errorf("load error with editor %s: ide %+v link %v", path, b.IDE, b.Link)
			}
		}
	})
}

// TestEditorParameterErrors checks that an unknown, empty or repeated
// editor is a bad request on every endpoint, never a fallback.
func TestEditorParameterErrors(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		root := editorRoot(t, w)
		opt := testOptions(root, s)
		for _, path := range editorPaths {
			for _, tc := range []struct{ extra, message string }{
				{"&editor=sublime", `editor must be one of vscode, vscode-insiders, cursor, vscodium, jetbrains, zed; not "sublime"`},
				{"&editor=", `editor must be one of vscode, vscode-insiders, cursor, vscodium, jetbrains, zed; not ""`},
				{"&editor=Zed", `editor must be one of vscode, vscode-insiders, cursor, vscodium, jetbrains, zed; not "Zed"`},
				{"&editor=zed&editor=zed", "parameter editor is given more than once"},
			} {
				rec := get(t, opt, http.MethodGet, path+tc.extra, nil)
				var env errorEnvelope
				if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil || rec.Code != http.StatusBadRequest || env.Error.Code != "bad_request" || env.Error.Message != tc.message {
					t.Errorf("%s%s: status %d %s", path, tc.extra, rec.Code, rec.Body.String())
				}
			}
		}
	})
}

// TestEditorParameterUnchangedBodies checks that the parameter changes
// nothing but the links and the ide status: naming the preset that already
// applies changes only ide.source.
func TestEditorParameterUnchangedBodies(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		root := editorRoot(t, w)
		opt := testOptions(root, s)
		for _, path := range editorPaths {
			without := get(t, opt, http.MethodGet, path, nil).Body.String()
			with := get(t, opt, http.MethodGet, path+"&editor=vscode", nil).Body.String()
			if strings.Replace(with, `"source":"request"`, `"source":"builtin"`, 1) != without {
				t.Errorf("%s: bodies differ beyond ide.source:\n%s\n%s", path, without, with)
			}
		}
	})
}

// TestIDEEndpoint checks GET /api/v0/ide: the golden body for the built-in
// default, a repository preset, a custom template and a load error, plus
// the security headers and the Host and Origin checks.
func TestIDEEndpoint(t *testing.T) {
	root := t.TempDir()
	opt := testOptions(root, nil)
	decode := func(t *testing.T) ideResponse {
		t.Helper()
		rec := get(t, opt, http.MethodGet, "/api/v0/ide", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d %s", rec.Code, rec.Body.String())
		}
		for _, h := range securityHeaders {
			if got := rec.Header().Get(h[0]); got != h[1] {
				t.Errorf("header %s = %q", h[0], got)
			}
		}
		if rec.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("Cache-Control %q", rec.Header().Get("Cache-Control"))
		}
		var out ideResponse
		dec := json.NewDecoder(bytes.NewReader(rec.Body.Bytes()))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&out); err != nil {
			t.Fatal(err, rec.Body.String())
		}
		return out
	}
	golden.Check(t, filepath.Join("testdata", "ide-builtin.golden.json"), decode(t))

	for _, tc := range []struct {
		config string
		want   repositoryEntry
	}{
		{"version: 1\nide: cursor\n", repositoryEntry{Available: true, Editor: strp("cursor"), Label: strp("Open in Cursor"), Source: ideSourceRepository}},
		{"version: 1\nide: {link_template: 'x://{rel_path}'}\n", repositoryEntry{Available: true, Custom: true, Source: ideSourceRepository}},
		{"version: 1\nide: {link_template: 'x://{rel_path}', label: Open in X}\n", repositoryEntry{Available: true, Label: strp("Open in X"), Custom: true, Source: ideSourceRepository}},
	} {
		writeConfig(t, root, tc.config)
		if got := decode(t).Repository; !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%q: %+v, want %+v", tc.config, got, tc.want)
		}
	}
	writeConfig(t, root, "version: 1\nide: none\n")
	got := decode(t)
	if r := got.Repository; r.Available || r.Error == nil || !strings.Contains(*r.Error, `unknown ide editor "none"`) || r.Editor != nil || r.Label != nil || r.Custom || r.Source != ideSourceRepository {
		t.Errorf("load error: %+v", r)
	}
	if len(got.Presets) != 6 {
		t.Errorf("load error: %d presets", len(got.Presets))
	}

	for _, tc := range []struct {
		header map[string]string
		status int
		code   string
	}{
		{map[string]string{"Host": "evil.example:7420"}, http.StatusForbidden, "host_not_allowed"},
		{map[string]string{"Origin": "http://evil.example"}, http.StatusForbidden, "cross_site_request"},
		{map[string]string{"Sec-Fetch-Site": "cross-site"}, http.StatusForbidden, "cross_site_request"},
	} {
		rec := get(t, opt, http.MethodGet, "/api/v0/ide", tc.header)
		if rec.Code != tc.status || errorCode(t, rec) != tc.code {
			t.Errorf("%v: status %d %s", tc.header, rec.Code, rec.Body.String())
		}
	}
	if rec := get(t, opt, http.MethodPost, "/api/v0/ide", nil); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST: status %d", rec.Code)
	}
}

// TestJetBrainsTabLineOnly checks rule 14 for IntelliJ IDEA's
// column (ADR 0044, ADR 0050): on a tab-indented Go line the server cannot
// know the IDE's tab size, so the jetbrains link names the line only
// (IntelliJ opens a missing column at the start of the line), exact is
// false and line_only true; every other preset keeps its exact column, and
// a jetbrains link on a line without a tab keeps its column.
func TestJetBrainsTabLineOnly(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		root := t.TempDir()
		files := map[string]string{
			"go.mod": "module example.com/tab\n\ngo 1.22\n",
			"tab.go": "package tab\n\ntype (\n\tWidget struct{}\n)\n\ntype Plain struct{}\n",
		}
		for name, content := range files {
			if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		indexRoot(t, root, w)
		opt := testOptions(root, s)
		plain := filepath.ToSlash(root)
		const widget, flat = "struct:tab.go#Widget", "struct:tab.go#Plain"
		lineOnly := "idea://open?file=" + plain + "/tab.go&line=4"
		for _, tc := range []struct {
			name, config, extra, id string
			want                    query.IDELink
		}{
			{"request", "", "&editor=jetbrains", widget, query.IDELink{Link: lineOnly, Line: 4, Column: 0, LineOnly: true, Caveat: config.CaveatTabSize}},
			{"repository", "version: 1\nide: jetbrains\n", "", widget, query.IDELink{Link: lineOnly, Line: 4, Column: 0, LineOnly: true, Caveat: config.CaveatTabSize}},
			{"repository-base-1", "version: 1\nide: {editor: jetbrains, column_base: 1}\n", "", widget, query.IDELink{Link: lineOnly, Line: 4, Column: 1, LineOnly: true, Caveat: config.CaveatTabSize}},
			{"same-template", "version: 1\nide: {link_template: 'idea://open?file={abs_path}&line={line}&column={column}', column_unit: codepoint, column_base: 0}\n", "", widget, query.IDELink{Link: lineOnly, Line: 4, Column: 0, LineOnly: true, Caveat: config.CaveatTabSize}},
			{"no-tab", "", "&editor=jetbrains", flat, query.IDELink{Link: "idea://open?file=" + plain + "/tab.go&line=7&column=5", Line: 7, Column: 5, Exact: true}},
			{"zed", "", "&editor=zed", widget, query.IDELink{Link: "zed://file" + plain + "/tab.go:4:2", Line: 4, Column: 2, Exact: true}},
			{"vscode", "", "&editor=vscode", widget, query.IDELink{Link: "vscode://file" + absLinkPath(root) + "/tab.go:4:2", Line: 4, Column: 2, Exact: true}},
		} {
			writeConfig(t, root, tc.config)
			b := getSource(t, opt, tc.id, tc.extra)
			checkAgreement(t, tc.name, b)
			if b.Source.State != sourceExact || b.Node.IDE == nil || *b.Node.IDE != tc.want {
				t.Errorf("%s: state %s, ide %+v, want %+v", tc.name, b.Source.State, b.Node.IDE, tc.want)
			}
		}
		rec := get(t, opt, http.MethodGet, "/api/v0/source?id="+url.QueryEscape(widget)+"&editor=jetbrains", nil)
		if !strings.Contains(rec.Body.String(), `"line_only":true`) || strings.Contains(rec.Body.String(), "&column=") {
			t.Errorf("line-only body: %s", rec.Body.String())
		}
		rec = get(t, opt, http.MethodGet, "/api/v0/source?id="+url.QueryEscape(widget)+"&editor=zed", nil)
		if strings.Contains(rec.Body.String(), "line_only") {
			t.Errorf("line_only must be omitted when false: %s", rec.Body.String())
		}
	})
}
