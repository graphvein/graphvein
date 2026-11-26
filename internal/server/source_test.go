package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/query"
	"github.com/graphvein/graphvein/internal/store"
)

// pad returns n comment lines.
func pad(n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "// pad %d\n", i)
	}
	return b.String()
}

// peekFiles are the TypeScript files of the code peek repository. Each
// state case edits only its own file, so one index serves every case.
func peekFiles() map[string]string {
	long := "export function longTarget() {\n" + strings.Repeat("  x();\n", 450) + "}\n"
	wide := `export function wideTarget() { return "` + strings.Repeat("a", 4095-39) + "\u00e9" + strings.Repeat("b", 900) + `"; }` + "\n"
	return map[string]string{
		"exact.ts":        "export function exactTarget() {\n  return fetch(\"/x\");\n}\n" + pad(20),
		"tail.ts":         pad(20) + "export function tailTarget() {\n  return 1;\n}\n",
		"inserted.ts":     pad(2) + "export function insertedTarget() {\n  return 2;\n}\n",
		"elsewhere.ts":    "export function elsewhereTarget() {\n  return 3;\n}\n// tail\n",
		"dup.ts":          "export function dupTarget() {\n  return 4;\n}\n",
		"truncated.ts":    pad(4) + "export function truncatedTarget() {\n  return 5;\n}\n",
		"empty.ts":        "",
		"deleted.ts":      "export function deletedTarget() {}\n",
		"link-in.ts":      "export function linkInTarget() {}\n",
		"link-copy.ts":    "export function linkInTarget() {}\n",
		"link-out.ts":     "export function linkOutTarget() {}\n",
		"linkdir/in.ts":   "export function linkDirTarget() {}\n",
		"compfile/in.ts":  "export function compFileTarget() {}\n",
		"dir.ts":          "export function dirTarget() {}\n",
		"nul.ts":          "export function nulTarget() {}\n",
		"badutf8.ts":      "export function badTarget() {}\n",
		"big.ts":          "export function bigTarget() {}\n",
		"locked.ts":       "export function lockedTarget() {}\n",
		"long.ts":         long,
		"wide.ts":         wide,
		"crlf.ts":         "export function crlfTarget() {\r\n  return 6;\r\n}\r\n",
		"bom.ts":          "\xef\xbb\xbfexport function bomTarget() {}\n",
		"utf16.ts":        "/*\u00e9\U0001F600*/ export function utf16Target() {}\n",
		"config/keep.txt": "",
	}
}

// peekRoot writes the peek repository and indexes it into w.
func peekRoot(t *testing.T, w store.Store) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range peekFiles() {
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

// sourceBody mirrors the documented response; decoding disallows unknown
// fields, so documentation and code cannot drift apart.
type sourceBody struct {
	APIVersion int        `json:"api_version"`
	Repository string     `json:"repository"`
	Index      *lastRun   `json:"index"`
	IDE        ideStatus  `json:"ide"`
	Node       nodeBody   `json:"node"`
	Source     sourceView `json:"source"`
	// CodeCallsPrecision is the code calls label of a function or method.
	CodeCallsPrecision *scopeLabel `json:"code_calls_precision"`
}

type nodeBody struct {
	contract.Node
	IDE       *query.IDELink `json:"ide"`
	FileState *string        `json:"file_state"`
}

func getSource(t *testing.T, opt Options, id string, extra string) sourceBody {
	t.Helper()
	rec := get(t, opt, http.MethodGet, "/api/v0/source?id="+url.QueryEscape(id)+extra, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s: status %d %s", id, rec.Code, rec.Body.String())
	}
	var out sourceBody
	dec := json.NewDecoder(bytes.NewReader(rec.Body.Bytes()))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		t.Fatal(err, rec.Body.String())
	}
	return out
}

// checkAgreement asserts D4: the presented node and the source were built
// from the same read.
func checkAgreement(t *testing.T, name string, b sourceBody) {
	t.Helper()
	state := b.Source.State
	fileState := ""
	if b.Node.FileState != nil {
		fileState = *b.Node.FileState
	}
	// A link with a caveat (a line-only link after a tab, a column unit
	// only assumed) is not exact although the source is: the conversion
	// read the same unchanged bytes, the editor's column is not vouched for.
	exact := b.Node.IDE != nil && (b.Node.IDE.Exact || b.Node.IDE.Caveat != "")
	want := map[string]string{
		sourceExact: "unchanged", sourceSpanInvalid: "unchanged",
		sourceTextMatch: "changed", sourceChanged: "changed", sourceBinary: "changed",
		sourceMissing: "missing", sourceNotRegular: "missing", sourceTooLarge: "missing",
		sourceUnreadable: "missing", sourceNotIndexed: "missing",
	}
	if state == sourceNoLocation {
		if b.Node.File == "." && (b.Node.FileState != nil || b.Node.IDE != nil) {
			t.Errorf("%s: synthetic node presented with %v %v", name, b.Node.FileState, b.Node.IDE)
		}
		return
	}
	if fileState != want[state] || exact != (state == sourceExact) {
		t.Errorf("%s: state %s, file_state %q, ide.exact %v", name, state, fileState, exact)
	}
	trusted := state == sourceExact || state == sourceTextMatch
	if trusted != (b.Source.Highlight != nil) {
		t.Errorf("%s: state %s with highlight %+v", name, state, b.Source.Highlight)
	}
	read := state == sourceExact || state == sourceTextMatch || state == sourceChanged || state == sourceSpanInvalid
	if read != (b.Source.FirstLine != nil) || read != (len(b.Source.Lines) > 0) || read != (b.Source.File.LineCount != nil) {
		t.Errorf("%s: state %s, first_line %v, %d lines", name, state, b.Source.FirstLine, len(b.Source.Lines))
	}
}

func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable here (Windows without developer mode): %v", err)
	}
}

// TestSourceStates covers every source state on both backends.
func TestSourceStates(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		root := peekRoot(t, w)
		opt := testOptions(root, s)
		at := func(name string) string { return filepath.Join(root, filepath.FromSlash(name)) }
		write := func(name, content string) {
			t.Helper()
			if err := os.WriteFile(at(name), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		remove := func(name string) {
			t.Helper()
			if err := os.RemoveAll(at(name)); err != nil {
				t.Fatal(err)
			}
		}
		files := peekFiles()
		outside := filepath.Join(t.TempDir(), "outside.ts")
		if err := os.WriteFile(outside, []byte(files["link-out.ts"]), 0o644); err != nil {
			t.Fatal(err)
		}
		dupText := strings.TrimSuffix(files["dup.ts"], "\n")
		for _, tc := range []struct {
			name, id, state, reason string
			edit                    func(t *testing.T)
		}{
			{"unchanged", "function:exact.ts#exactTarget", sourceExact, "", nil},
			{"inserted above", "function:inserted.ts#insertedTarget", sourceChanged, reasonTextDiffers, func(t *testing.T) { write("inserted.ts", "// new\n"+files["inserted.ts"]) }},
			{"edit elsewhere", "function:elsewhere.ts#elsewhereTarget", sourceTextMatch, "", func(t *testing.T) {
				write("elsewhere.ts", strings.Replace(files["elsewhere.ts"], "// tail", "// edited tail", 1))
			}},
			{"duplicated", "function:dup.ts#dupTarget", sourceChanged, reasonNotUnique, func(t *testing.T) { write("dup.ts", files["dup.ts"]+"/*\n"+dupText+"\n*/\n") }},
			{"truncated", "function:truncated.ts#truncatedTarget", sourceChanged, reasonOutOfRange, func(t *testing.T) { write("truncated.ts", "x\n") }},
			{"empty span", "file:empty.ts#empty.ts", sourceChanged, reasonEmptySpan, func(t *testing.T) { write("empty.ts", "let a = 1;\n") }},
			{"deleted", "function:deleted.ts#deletedTarget", sourceMissing, "", func(t *testing.T) { remove("deleted.ts") }},
			{"component is a file", "function:compfile/in.ts#compFileTarget", sourceMissing, "", func(t *testing.T) { remove("compfile"); write("compfile", "x") }},
			{"symlink in root", "function:link-in.ts#linkInTarget", sourceNotRegular, "", func(t *testing.T) { remove("link-in.ts"); symlinkOrSkip(t, "link-copy.ts", at("link-in.ts")) }},
			{"symlink outside", "function:link-out.ts#linkOutTarget", sourceNotRegular, "", func(t *testing.T) { remove("link-out.ts"); symlinkOrSkip(t, outside, at("link-out.ts")) }},
			{"directory symlink", "function:linkdir/in.ts#linkDirTarget", sourceNotRegular, "", func(t *testing.T) {
				if err := os.Rename(at("linkdir"), at("realdir")); err != nil {
					t.Fatal(err)
				}
				symlinkOrSkip(t, "realdir", at("linkdir"))
			}},
			{"directory", "function:dir.ts#dirTarget", sourceNotRegular, "", func(t *testing.T) {
				remove("dir.ts")
				if err := os.Mkdir(at("dir.ts"), 0o755); err != nil {
					t.Fatal(err)
				}
			}},
			{"nul byte", "function:nul.ts#nulTarget", sourceBinary, "", func(t *testing.T) { write("nul.ts", files["nul.ts"]+"\x00") }},
			{"invalid utf-8", "function:badutf8.ts#badTarget", sourceBinary, "", func(t *testing.T) { write("badutf8.ts", files["badutf8.ts"]+"\xff") }},
			{"too large", "function:big.ts#bigTarget", sourceTooLarge, "", func(t *testing.T) {
				write("big.ts", files["big.ts"]+strings.Repeat("a", maxSourceBytes))
			}},
			{"unreadable", "function:locked.ts#lockedTarget", sourceUnreadable, "", func(t *testing.T) {
				if runtime.GOOS == "windows" {
					t.Skip("file modes do not deny reads on Windows")
				}
				if os.Geteuid() == 0 {
					t.Skip("running as root, which reads files whatever their mode")
				}
				if err := os.Chmod(at("locked.ts"), 0); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { os.Chmod(at("locked.ts"), 0o644) })
			}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				if tc.edit != nil {
					tc.edit(t)
				}
				b := getSource(t, opt, tc.id, "")
				reason := ""
				if b.Source.Reason != nil {
					reason = *b.Source.Reason
				}
				if b.Source.State != tc.state || (tc.state != sourceUnreadable && reason != tc.reason) || tc.state == sourceUnreadable && reason == "" {
					t.Fatalf("state %s reason %q, want %s %q", b.Source.State, reason, tc.state, tc.reason)
				}
				checkAgreement(t, tc.name, b)
				if b.Source.File.Path != b.Node.File || b.Node.Identity != tc.id {
					t.Errorf("file %+v node %s", b.Source.File, b.Node.Identity)
				}
				switch tc.state {
				case sourceExact:
					span := b.Node.Span
					if *b.Source.Highlight != (highlight{span.StartLine, span.StartColumn, span.EndLine, span.EndColumn}) {
						t.Errorf("highlight %+v, span %+v", b.Source.Highlight, span)
					}
				case sourceTextMatch:
					if b.Source.Highlight.StartLine != 1 || b.Source.Highlight.EndLine != 3 {
						t.Errorf("highlight %+v", b.Source.Highlight)
					}
				case sourceTooLarge:
					if b.Source.File.Size == nil || *b.Source.File.Size <= maxSourceBytes {
						t.Errorf("size %v", b.Source.File.Size)
					}
				case sourceChanged:
					// Centred on the stored start line, clamped to the file.
					if tc.reason == reasonOutOfRange && (*b.Source.FirstLine != 1 || len(b.Source.Lines) != 1) {
						t.Errorf("window %d %v", *b.Source.FirstLine, b.Source.Lines)
					}
				}
			})
		}
	})
}

// hidingStore drops one file from FileHashes, as an inconsistent store
// would.
type hidingStore struct {
	store.Store
	file string
}

func (h hidingStore) FileHashes(ctx context.Context, repo string) (map[string]string, error) {
	hashes, err := h.Store.FileHashes(ctx, repo)
	delete(hashes, h.file)
	return hashes, err
}

// TestSourceCrafted covers the states only an external snapshot or an
// inconsistent store produce, and that a stored path never leaves the
// root.
func TestSourceCrafted(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		root := t.TempDir()
		real := []byte("const a = 1;\n")
		if err := os.WriteFile(filepath.Join(root, "real.ts"), real, 0o644); err != nil {
			t.Fatal(err)
		}
		synthetic, err := contract.SyntheticHash(map[string]any{})
		if err != nil {
			t.Fatal(err)
		}
		hash := contract.ContentHash(real)
		lines := []string{
			`{"type":"header","contract_version":1,"indexer":{"name":"crafted","version":"t016e1"},"repository":"crafted","generated_at":"2026-11-03T00:00:00Z"}`,
			fmt.Sprintf(`{"type":"file","path":"real.ts","language":"typescript","precision":"precise","size":13,"content_hash":%q}`, hash),
			fmt.Sprintf(`{"type":"file","path":"../outside.ts","language":"typescript","precision":"precise","size":13,"content_hash":%q}`, hash),
			fmt.Sprintf(`{"type":"node","identity":"function:real.ts#oob","kind":"function","level":3,"name":"oob","qualified_name":"oob","file":"real.ts","span":{"start_line":5,"start_column":0,"end_line":5,"end_column":1},"content_hash":%q,"attributes":{}}`, hash),
			fmt.Sprintf(`{"type":"node","identity":"function:real.ts#col","kind":"function","level":3,"name":"col","qualified_name":"col","file":"real.ts","span":{"start_line":1,"start_column":0,"end_line":1,"end_column":40},"content_hash":%q,"attributes":{}}`, hash),
			fmt.Sprintf(`{"type":"node","identity":"function:real.ts#ok","kind":"function","level":3,"name":"ok","qualified_name":"ok","file":"real.ts","span":{"start_line":1,"start_column":6,"end_line":1,"end_column":7},"content_hash":%q,"attributes":{}}`, contract.ContentHash([]byte("a"))),
			fmt.Sprintf(`{"type":"node","identity":"external:.#npm%%3Areact","kind":"external","level":1,"name":"react","qualified_name":"npm:react","file":".","content_hash":%q,"attributes":{}}`, synthetic),
			fmt.Sprintf(`{"type":"node","identity":"function:../outside.ts#x","kind":"function","level":3,"name":"x","qualified_name":"x","file":"../outside.ts","span":{"start_line":1,"start_column":0,"end_line":1,"end_column":1},"content_hash":%q,"attributes":{}}`, hash),
			`{"type":"footer","counts":{"records":7,"file":2,"node":5,"edge":0,"diagnostic":0}}`,
		}
		if err := ingest.Stream(context.Background(), strings.NewReader(strings.Join(lines, "\n")+"\n"), w); err != nil {
			t.Fatal(err)
		}
		snapshot, err := s.Dump(context.Background(), "crafted")
		if err != nil {
			t.Fatal(err)
		}
		// Contract validation rejects the ../outside.ts file and node, so
		// no request can make the endpoint open them.
		if len(snapshot.Files) != 1 || len(snapshot.Nodes) != 4 || len(snapshot.Diagnostics) != 2 {
			t.Fatalf("crafted snapshot: %d files, %d nodes, diagnostics %v", len(snapshot.Files), len(snapshot.Nodes), snapshot.Diagnostics)
		}
		opt := testOptions(root, s)
		opt.Repository = "crafted"
		for _, tc := range []struct{ id, state string }{
			{"function:real.ts#oob", sourceSpanInvalid},
			{"function:real.ts#col", sourceSpanInvalid},
			{"function:real.ts#ok", sourceExact},
			{"external:.#npm%3Areact", sourceNoLocation},
		} {
			b := getSource(t, opt, tc.id, "")
			if b.Source.State != tc.state {
				t.Errorf("%s: state %s, want %s", tc.id, b.Source.State, tc.state)
			}
			checkAgreement(t, tc.id, b)
		}
		if b := getSource(t, opt, "function:real.ts#oob", ""); *b.Source.FirstLine != 1 || len(b.Source.Lines) != 1 || b.Source.Lines[0] != "const a = 1;" {
			t.Errorf("span_invalid window %v %v", b.Source.FirstLine, b.Source.Lines)
		}
		// A stored span that is not valid in its unchanged file is a
		// producer error: one server logs it once per node and file
		// content, however often it is requested.
		var logged bytes.Buffer
		warnOpt := opt
		warnOpt.Logger = slog.New(slog.NewTextHandler(&logged, nil))
		handler := NewHandler(warnOpt, testHosts)
		for _, id := range []string{"function:real.ts#oob", "function:real.ts#oob", "function:real.ts#col", "function:real.ts#ok", "function:real.ts#col"} {
			req := httptest.NewRequest(http.MethodGet, "/api/v0/source?id="+url.QueryEscape(id), nil)
			req.Host = testHost
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("%s: status %d", id, rec.Code)
			}
		}
		warnings := strings.Count(logged.String(), "stored span is not valid in its unchanged file")
		if warnings != 2 || strings.Count(logged.String(), "function:real.ts#oob") != 1 || strings.Count(logged.String(), "function:real.ts#col") != 1 {
			t.Errorf("span warnings: want one per invalid node, got %d:\n%s", warnings, logged.String())
		}
		// A store that lost the file's hash: nothing is read.
		hiding := opt
		hiding.Store = hidingStore{Store: s, file: "real.ts"}
		b := getSource(t, hiding, "function:real.ts#ok", "")
		if b.Source.State != sourceNotIndexed || b.Source.File.Size != nil {
			t.Errorf("not indexed: %+v", b.Source)
		}
		checkAgreement(t, "not indexed", b)
		for _, path := range []string{
			"/api/v0/source?id=" + url.QueryEscape("function:../outside.ts#x"),
			"/api/v0/source?id=function%3A..%2F..%2Fetc%2Fpasswd%23x",
			"/api/v0/source?id=%2E%2E%2F%2E%2E%2Fetc%2Fpasswd",
			"/api/v0/source?id=" + url.QueryEscape("function:"+filepath.ToSlash(filepath.Join(root, "real.ts"))+"#ok"),
			"/api/v0/source/..%2Freal.ts",
		} {
			rec := get(t, opt, http.MethodGet, path, nil)
			if rec.Code != http.StatusNotFound && rec.Code != http.StatusBadRequest {
				t.Errorf("%s: status %d %s", path, rec.Code, rec.Body.String())
			}
		}
	})
}

// TestSourceWindow covers the window, caps and column encoding.
func TestSourceWindow(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		root := peekRoot(t, w)
		opt := testOptions(root, s)
		lineCount := func(b sourceBody) int { return *b.Source.File.LineCount }
		// Clamped at the file start; context 0 and 50.
		b := getSource(t, opt, "function:exact.ts#exactTarget", "")
		if *b.Source.FirstLine != 1 || len(b.Source.Lines) != 11 || lineCount(b) != 23 || b.Source.Truncated {
			t.Errorf("default window: first %d, %d lines of %d", *b.Source.FirstLine, len(b.Source.Lines), lineCount(b))
		}
		if b := getSource(t, opt, "function:exact.ts#exactTarget", "&context=0"); len(b.Source.Lines) != 3 || b.Source.Lines[1] != `  return fetch("/x");` {
			t.Errorf("context 0: %q", b.Source.Lines)
		}
		if b := getSource(t, opt, "function:exact.ts#exactTarget", "&context=50"); len(b.Source.Lines) != 23 {
			t.Errorf("context 50: %d lines", len(b.Source.Lines))
		}
		// Clamped at the file end.
		b = getSource(t, opt, "function:tail.ts#tailTarget", "&context=5")
		if *b.Source.FirstLine != 16 || len(b.Source.Lines) != 8 || lineCount(b) != 23 {
			t.Errorf("end window: first %d, %d lines", *b.Source.FirstLine, len(b.Source.Lines))
		}
		// A file node ends after the final line feed: the highlight ends at
		// the end of the last line.
		b = getSource(t, opt, "file:tail.ts#tail.ts", "&context=0")
		if b.Source.State != sourceExact || *b.Source.Highlight != (highlight{1, 0, 23, 1}) || len(b.Source.Lines) != 23 {
			t.Errorf("file node: %s %+v %d lines", b.Source.State, b.Source.Highlight, len(b.Source.Lines))
		}
		// A span longer than the line cap.
		b = getSource(t, opt, "function:long.ts#longTarget", "")
		if !b.Source.Truncated || len(b.Source.Lines) != MaxSourceLines || *b.Source.Highlight != (highlight{1, 0, 400, 6}) {
			t.Errorf("long span: truncated %v, %d lines, highlight %+v", b.Source.Truncated, len(b.Source.Lines), b.Source.Highlight)
		}
		// A line over 4,096 bytes is cut before the two-byte character
		// that straddles the cap, and the highlight is clipped to it.
		b = getSource(t, opt, "function:wide.ts#wideTarget", "")
		if len(b.Source.Lines[0]) != 4095 || len(b.Source.CutLines) != 1 || b.Source.CutLines[0] != 1 || *b.Source.Highlight != (highlight{1, 0, 1, 4095}) {
			t.Errorf("wide line: %d bytes, cut %v, highlight %+v", len(b.Source.Lines[0]), b.Source.CutLines, b.Source.Highlight)
		}
		// CRLF: lines without terminators, positions on the normalised text.
		b = getSource(t, opt, "function:crlf.ts#crlfTarget", "")
		if b.Source.State != sourceExact || strings.Join(b.Source.Lines, "|") != "export function crlfTarget() {|  return 6;|}" || *b.Source.Highlight != (highlight{1, 0, 3, 1}) {
			t.Errorf("crlf: %s %q %+v", b.Source.State, b.Source.Lines, b.Source.Highlight)
		}
		// BOM: dropped from line 1, columns shifted.
		b = getSource(t, opt, "function:bom.ts#bomTarget", "")
		if b.Node.Span.StartColumn != 3 || b.Source.Lines[0] != "export function bomTarget() {}" || *b.Source.Highlight != (highlight{1, 0, 1, 30}) {
			t.Errorf("bom: span %+v %q %+v", b.Node.Span, b.Source.Lines[0], b.Source.Highlight)
		}
		// UTF-16 columns: é is one code unit, the emoji two.
		b = getSource(t, opt, "function:utf16.ts#utf16Target", "")
		if b.Node.Span.StartColumn != 11 || *b.Source.Highlight != (highlight{1, 8, 1, 8 + len("export function utf16Target() {}")}) {
			t.Errorf("utf16: span %+v highlight %+v", b.Node.Span, b.Source.Highlight)
		}
		checkAgreement(t, "utf16", b)
	})
}

func TestSourceParameters(t *testing.T) {
	opt := handlerOptions(t)
	id := "&id=" + url.QueryEscape("function:a.ts#f")
	for _, tc := range []struct {
		query  string
		status int
	}{
		{"", 400},
		{"id=", 400},
		{"id=nope", 400},
		{"id=a&id=b", 400},
		{"context=51" + id, 400},
		{"context=-1" + id, 400},
		{"context=%2B5" + id, 400},
		{"context=x" + id, 400},
		{"context=" + id, 400},
		{"context=1.5" + id, 400},
		{"context=1&context=2" + id, 400},
		{"context=0" + id, 404},
		{"context=50" + id, 404},
		{"context=050" + id, 404},
		{"unknown=1" + id, 404},
	} {
		rec := get(t, opt, http.MethodGet, "/api/v0/source?"+tc.query, nil)
		want := map[int]string{400: "bad_request", 404: "not_found"}[tc.status]
		if rec.Code != tc.status || errorCode(t, rec) != want {
			t.Errorf("%s: status %d %s, want %d", tc.query, rec.Code, rec.Body.String(), tc.status)
		}
	}
}

// TestSourceLabel reports the preset label, an explicit label, null for a
// user template, and null with the error for an invalid configuration.
func TestSourceLabel(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		root := peekRoot(t, w)
		opt := testOptions(root, s)
		id := "function:exact.ts#exactTarget"
		for _, tc := range []struct {
			config, label, link string
			available           bool
		}{
			{"", "Open in VS Code", "vscode://file" + absLinkPath(root) + "/exact.ts:1:1", true},
			{"version: 1\nide: vscode-insiders\n", "Open in VS Code Insiders", "vscode-insiders://file" + absLinkPath(root) + "/exact.ts:1:1", true},
			{"version: 1\nide: {editor: jetbrains, label: Open in IDEA}\n", "Open in IDEA", "", true},
			{"version: 1\nide: {link_template: 'x://{rel_path}'}\n", "", "x://exact.ts", true},
			{"version: 1\nide: emacs\n", "", "", false},
		} {
			path := filepath.Join(root, "graphvein.yaml")
			os.Remove(path)
			if tc.config != "" {
				if err := os.WriteFile(path, []byte(tc.config), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			b := getSource(t, opt, id, "")
			label := ""
			if b.IDE.Label != nil {
				label = *b.IDE.Label
			}
			if label != tc.label || b.IDE.Available != tc.available || (b.IDE.Error == nil) != tc.available {
				t.Errorf("%q: ide %+v", tc.config, b.IDE)
			}
			if tc.link != "" && (b.Node.IDE == nil || b.Node.IDE.Link != tc.link) {
				t.Errorf("%q: link %+v, want %s", tc.config, b.Node.IDE, tc.link)
			}
			if !tc.available && b.Node.IDE != nil {
				t.Errorf("%q: link despite the configuration error", tc.config)
			}
			if b.Source.State != sourceExact {
				t.Errorf("%q: state %s", tc.config, b.Source.State)
			}
		}
	})
}

// absLinkPath is the {abs_url_path} form of root as the tests expect it.
func absLinkPath(root string) string {
	p := filepath.ToSlash(root)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return p
}

func TestClassifyReadError(t *testing.T) {
	if got := classifyReadError(&os.PathError{Op: "open", Path: "a", Err: errors.New("boom")}); got.state != sourceUnreadable || got.reason != "open a: boom" {
		t.Errorf("%+v", got)
	}
	if got := classifyReadError(&os.PathError{Op: "openat", Path: "a", Err: os.ErrNotExist}); got.state != sourceMissing {
		t.Errorf("%+v", got)
	}
	if got := classifyReadError(&os.PathError{Op: "openat", Path: "a", Err: errors.New("path escapes from parent")}); got.state != sourceNotRegular {
		t.Errorf("%+v", got)
	}
	// A path that is not a valid contract path is never read, even when
	// the store holds it.
	c := &confinedReader{root: t.TempDir(), hashes: map[string]string{"../x": "h", "a//b": "h"}, reads: map[string]fileRead{}}
	for _, p := range []string{"../x", "a//b", "missing.ts"} {
		if got := c.read(p); got.state != sourceNotIndexed {
			t.Errorf("%s: %+v", p, got)
		}
	}
}

// TestTextMatchOverlap checks that overlapping occurrences count: "aba"
// occurs twice in "ababa" (at bytes 0 and 2), so the span is not unique and
// the file, which changed since indexing, gets no highlight.
func TestTextMatchOverlap(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.ts"), []byte("ababa\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	span := contract.Span{StartLine: 1, StartColumn: 0, EndLine: 1, EndColumn: 3}
	n := contract.Node{File: "a.ts", Span: &span, ContentHash: contract.ContentHash([]byte("aba"))}
	hashes := map[string]string{"a.ts": contract.ContentHash([]byte("indexed before the edit"))}
	files := &confinedReader{root: root, hashes: hashes, reads: map[string]fileRead{}}
	got := sourceOf(n, hashes, files, 3)
	if got.State != sourceChanged || got.Reason == nil || *got.Reason != reasonNotUnique || got.Highlight != nil {
		t.Fatalf("state %s, reason %v, highlight %+v", got.State, got.Reason, got.Highlight)
	}
}
