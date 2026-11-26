package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

// commentedSettings is the commented graphvein.yaml of the config
// package's edit goldens: four-space indentation, comments, keys outside
// the form.
const commentedSettings = "../config/testdata/settings/commented.yaml"

// settingsServer serves one repository whose root holds graphvein.yaml.
type settingsServer struct {
	*explorerServer
	root string
	// store is the repository's own SQLite store (no runs until a test
	// indexes into it).
	store store.Store
}

func newSettingsServer(t *testing.T, file string) *settingsServer {
	t.Helper()
	root := t.TempDir()
	if file != "" {
		if err := os.WriteFile(filepath.Join(root, config.File), []byte(file), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	ss := &settingsServer{explorerServer: &explorerServer{t: t, h: NewHandler(testOptions(root, s), testHosts)}, root: root, store: s}
	ss.token = ss.get("").Token
	return ss
}

func (ss *settingsServer) get(query string) configResponse {
	ss.t.Helper()
	rec := ss.do(http.MethodGet, configPath+query, "", nil)
	var out configResponse
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &out) != nil {
		ss.t.Fatalf("GET %s: %d %s", configPath+query, rec.Code, rec.Body)
	}
	return out
}

func (ss *settingsServer) file() string {
	data, _ := os.ReadFile(filepath.Join(ss.root, config.File))
	return string(data)
}

// edit posts a body built from the current hash: ops (a JSON array) or
// text (a JSON string), as a preview or an apply of resultHash.
func editBody(hash, field, value string, resultHash string) string {
	body := `{"base_hash":"` + hash + `","` + field + `":` + value
	if resultHash != "" {
		body += `,"apply":true,"result_hash":"` + resultHash + `"`
	}
	return body + "}"
}

func (ss *settingsServer) post(query, body string) (int, configEditResponse, string) {
	ss.t.Helper()
	rec := ss.do(http.MethodPost, configPath+query, body, nil)
	var out configEditResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	var e struct{ Error struct{ Code string } }
	_ = json.Unmarshal(rec.Body.Bytes(), &e)
	return rec.Code, out, e.Error.Code + " " + rec.Body.String()
}

// previewApply previews body's edit and applies it, failing on any
// refusal.
func (ss *settingsServer) previewApply(field, value string) (configEditResponse, configEditResponse) {
	ss.t.Helper()
	hash := *ss.get("").Hash
	code, preview, msg := ss.post("", editBody(hash, field, value, ""))
	if code != http.StatusOK {
		ss.t.Fatalf("preview %s: %d %s", value, code, msg)
	}
	code, applied, msg := ss.post("", editBody(hash, field, value, preview.ResultHash))
	if code != http.StatusOK || !applied.Applied {
		ss.t.Fatalf("apply %s: %d %s", value, code, msg)
	}
	return preview, applied
}

func keyByName(t *testing.T, doc configResponse, key string) configKeyDoc {
	t.Helper()
	for _, k := range doc.Keys {
		if k.Key == key {
			return k
		}
	}
	t.Fatalf("no key %s in %+v", key, doc.Keys)
	return configKeyDoc{}
}

// TestSettingsGet checks rows ST-12..ST-14, ST-16 and ST-19: each form
// key with its value and origin (the file with its line, the built-in
// default, not set), the keys outside the form with the guarded edit
// that edits them, the raw text with its hash and the root
// .graphveinignore read-only.
func TestSettingsGet(t *testing.T) {
	data, err := os.ReadFile(commentedSettings)
	if err != nil {
		t.Fatal(err)
	}
	ss := newSettingsServer(t, string(data))
	if err := os.WriteFile(filepath.Join(ss.root, ignoreFile), []byte("generated/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	doc := ss.get("")
	if doc.Repository != "fixture" || doc.File != config.File || !*doc.Exists || *doc.Text != string(data) || *doc.Hash != manifestHash(data) || !doc.Editable || doc.Token == "" || doc.ConfigError != nil || doc.Project != nil {
		t.Fatalf("document: %+v", doc)
	}
	if doc.Reindex != (configReindexDoc{Path: indexRunPath, Scope: "repository"}) {
		t.Errorf("reindex %+v", doc.Reindex)
	}
	if len(doc.Keys) != len(config.SettingKeys()) {
		t.Fatalf("%d keys", len(doc.Keys))
	}
	labels := keyByName(t, doc, "ui.labels")
	if labels.Value != "technical" || labels.Origin != config.OriginFile || *labels.Line != 7 || labels.Default != "plain" || labels.Applies != config.AppliesAtOnce || !labels.Editable || strings.Join(labels.Choices, ",") != "plain,technical" {
		t.Errorf("ui.labels %+v", labels)
	}
	depth := keyByName(t, doc, "index.shape_depth")
	if depth.Value != 6.0 || *depth.Line != 12 || *depth.Min != 1 || *depth.Max != 10 || depth.Applies != config.AppliesNextIndexRun {
		t.Errorf("index.shape_depth %+v", depth)
	}
	generated := keyByName(t, doc, "clients.link_generated_output")
	if generated.Value != false || generated.Origin != config.OriginBuiltin || generated.Line != nil {
		t.Errorf("clients.link_generated_output %+v", generated)
	}
	hide := keyByName(t, doc, "architecture.hide")
	if hide.Value != nil || hide.Default != nil || hide.Origin != config.OriginNotSet {
		t.Errorf("architecture.hide %+v", hide)
	}
	var other []string
	for _, o := range doc.OtherKeys {
		s := o.Key + "@" + string(rune('0'+o.Line/10)) + string(rune('0'+o.Line%10))
		if o.EditedBy != nil {
			s += "->" + *o.EditedBy
		}
		other = append(other, s)
	}
	if got := strings.Join(other, " "); got != "version@02 repository@03 changes.journal_runs@20 explorer.path_values@23->"+explorerValuesPath {
		t.Errorf("other keys %s", got)
	}
	if doc.Ignore.File != ".graphveinignore" || !*doc.Ignore.Exists || *doc.Ignore.Text != "generated/\n" || doc.Ignore.Editable {
		t.Errorf("ignore %+v", doc.Ignore)
	}
	// No file: absent, built-in defaults, nothing set.
	empty := newSettingsServer(t, "").get("")
	if *empty.Exists || *empty.Hash != hashAbsent || *empty.Text != "" || *empty.Ignore.Exists || len(empty.OtherKeys) != 0 || keyByName(t, empty, "ui.labels").Origin != config.OriginBuiltin {
		t.Errorf("no file: %+v", empty)
	}
}

// TestSettingsInvalidConfig checks row ST-15: a file that does not load
// is reported with the strict decoder's message and no value; typed edits
// are refused; a whole-text edit that loads fixes it.
func TestSettingsInvalidConfig(t *testing.T) {
	broken := "version: 1\nui:\n  labels: fancy\n"
	ss := newSettingsServer(t, broken)
	doc := ss.get("")
	_, want := config.Parse(config.File, []byte(broken))
	if doc.ConfigError == nil || *doc.ConfigError != want.Error() || !strings.Contains(*doc.ConfigError, "line 3") || len(doc.OtherKeys) != 0 {
		t.Fatalf("config error %v, want %v", doc.ConfigError, want)
	}
	for _, k := range doc.Keys {
		if k.Value != nil || k.Origin != "unknown" || k.Editable || k.Reason == nil {
			t.Errorf("%s reported from a broken file: %+v", k.Key, k)
		}
	}
	code, _, msg := ss.post("", editBody(*doc.Hash, "operations", `[{"op":"set","key":"ui.labels","value":"plain"}]`, ""))
	if code != http.StatusConflict || !strings.HasPrefix(msg, "invalid_config ") {
		t.Errorf("typed edit of a broken file: %d %s", code, msg)
	}
	preview, _ := ss.previewApply("text", `"version: 1\nui:\n  labels: plain\n"`)
	if !preview.Valid || !preview.OtherKeysChanged || len(preview.ChangedKeys) != len(config.SettingKeys()) {
		t.Errorf("fixing preview %+v", preview)
	}
	if ss.file() != "version: 1\nui:\n  labels: plain\n" || ss.get("").ConfigError != nil {
		t.Errorf("fixed file %q", ss.file())
	}
}

// TestSettingsEditGuards checks row ST-22: every guard of the path value
// edit, and that a refused edit writes nothing.
func TestSettingsEditGuards(t *testing.T) {
	ss := newSettingsServer(t, "version: 1\n")
	hash := *ss.get("").Hash
	set := editBody(hash, "operations", `[{"op":"set","key":"ui.labels","value":"technical"}]`, "")
	for _, c := range []struct {
		name, body string
		header     map[string]string
		status     int
		code       string
	}{
		{"no token", set, map[string]string{tokenHeader: ""}, 403, "token_required"},
		{"wrong token", set, map[string]string{tokenHeader: "guess"}, 403, "token_required"},
		{"remote peer", set, map[string]string{"RemoteAddr": "192.0.2.1:4000"}, 403, "not_loopback"},
		{"foreign origin", set, map[string]string{"Origin": "http://evil.example"}, 403, "cross_site_request"},
		{"cross site", set, map[string]string{"Sec-Fetch-Site": "cross-site"}, 403, "cross_site_request"},
		{"same site", set, map[string]string{"Sec-Fetch-Site": "same-site"}, 403, "cross_site_request"},
		{"other host", set, map[string]string{"Host": "evil.example:80"}, 403, "host_not_allowed"},
		{"form post", set, map[string]string{"Content-Type": "text/plain"}, 415, "unsupported_media_type"},
		{"unknown field", strings.Replace(set, `"base_hash"`, `"path":"/etc/x","base_hash"`, 1), nil, 400, "bad_request"},
		{"duplicate field", strings.Replace(set, `"base_hash"`, `"apply":false,"apply":false,"base_hash"`, 1), nil, 400, "bad_request"},
		{"operations and text", strings.Replace(set, `"base_hash"`, `"text":"version: 1\n","base_hash"`, 1), nil, 400, "bad_request"},
		{"neither", `{"base_hash":"` + hash + `"}`, nil, 400, "bad_request"},
		{"apply without hash", strings.Replace(set, `"base_hash"`, `"apply":true,"base_hash"`, 1), nil, 400, "bad_request"},
		{"too large", editBody(hash, "text", `"`+strings.Repeat("#", maxManifestSize+1)+`"`, ""), nil, 413, "too_large"},
	} {
		var rec *httptest.ResponseRecorder
		if host := c.header["Host"]; host != "" {
			req := httptest.NewRequest(http.MethodPost, configPath, strings.NewReader(c.body))
			req.Host, req.RemoteAddr = host, "127.0.0.1:50000"
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set(tokenHeader, ss.token)
			rec = httptest.NewRecorder()
			ss.h.ServeHTTP(rec, req)
		} else {
			rec = ss.do(http.MethodPost, configPath, c.body, c.header)
		}
		var e struct{ Error struct{ Code string } }
		_ = json.Unmarshal(rec.Body.Bytes(), &e)
		if rec.Code != c.status || e.Error.Code != c.code {
			t.Errorf("%s: %d %s", c.name, rec.Code, rec.Body)
		}
	}
	for _, method := range []string{http.MethodPut, http.MethodDelete, http.MethodOptions} {
		if rec := ss.do(method, configPath, "", nil); rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "GET, HEAD, POST" {
			t.Errorf("%s: %d %q", method, rec.Code, rec.Header().Get("Allow"))
		}
	}
	if ss.file() != "version: 1\n" {
		t.Errorf("a refused edit wrote %q", ss.file())
	}
}

// TestSettingsConcurrentEdit checks rows ST-23 and ST-30: two edits
// prepared on the same file are serialised; the second is refused because
// the file changed under it, and a stale preview is never applied.
func TestSettingsConcurrentEdit(t *testing.T) {
	ss := newSettingsServer(t, "version: 1\n")
	hash := *ss.get("").Hash
	a := `[{"op":"set","key":"ui.labels","value":"technical"}]`
	b := `[{"op":"set","key":"index.shape_depth","value":8}]`
	_, pa, _ := ss.post("", editBody(hash, "operations", a, ""))
	_, pb, _ := ss.post("", editBody(hash, "operations", b, ""))
	var wg sync.WaitGroup
	codes := make([]int, 2)
	for i, body := range []string{editBody(hash, "operations", a, pa.ResultHash), editBody(hash, "operations", b, pb.ResultHash)} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i], _, _ = ss.post("", body)
		}()
	}
	wg.Wait()
	ok, conflict := 0, 0
	for _, c := range codes {
		switch c {
		case http.StatusOK:
			ok++
		case http.StatusConflict:
			conflict++
		}
	}
	if ok != 1 || conflict != 1 {
		t.Fatalf("concurrent applies: %v", codes)
	}
	written := ss.file()
	if written != "version: 1\nui:\n  labels: technical\n" && written != "version: 1\nindex:\n  shape_depth: 8\n" {
		t.Errorf("written %q", written)
	}
	// An edit made on disk after the preview: the stale base is refused.
	if err := os.WriteFile(filepath.Join(ss.root, config.File), []byte("version: 1\n# by hand\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, msg := ss.post("", editBody(hash, "operations", a, pa.ResultHash)); code != http.StatusConflict || !strings.HasPrefix(msg, "config_changed ") {
		t.Errorf("stale base: %d %s", code, msg)
	}
	if ss.file() != "version: 1\n# by hand\n" {
		t.Errorf("stale edit wrote %q", ss.file())
	}
}

// TestSettingsPreviewApply checks rows ST-18, ST-24, ST-25 and ST-29: a
// preview writes nothing and says what changes and when it applies; the
// apply writes exactly the preview; a remove brings the built-in default
// back; an apply that changes nothing is refused.
func TestSettingsPreviewApply(t *testing.T) {
	data, err := os.ReadFile(commentedSettings)
	if err != nil {
		t.Fatal(err)
	}
	ss := newSettingsServer(t, string(data))
	hash := *ss.get("").Hash
	ops := `[{"op":"set","key":"ui.labels","value":"plain"},{"op":"set","key":"index.shape_depth","value":8},{"op":"set","key":"clients.base_paths","value":[{"key":"web","value":"/api"}]}]`
	code, preview, msg := ss.post("", editBody(hash, "operations", ops, ""))
	if code != http.StatusOK || preview.Applied || !preview.Valid || preview.Reformatted || preview.BaseHash != hash || !preview.ReindexNeeded || preview.OtherKeysChanged {
		t.Fatalf("preview: %d %s", code, msg)
	}
	if ss.file() != string(data) {
		t.Fatal("a preview wrote the file")
	}
	var changed []string
	for _, c := range preview.ChangedKeys {
		changed = append(changed, c.Key+"="+c.Applies)
	}
	if got := strings.Join(changed, " "); got != "ui.labels=at_once index.shape_depth=next_index_run clients.base_paths=next_index_run" {
		t.Errorf("changed keys %s", got)
	}
	// Lists and maps are worded item by item as `key: value`, never as
	// JSON, scalars with the value they replace.
	if strings.Join(preview.Changes, "; ") != "sets ui.labels to `plain` (was `technical`); sets index.shape_depth to 8 (was 6); sets clients.base_paths (not set before): adds `web: /api`" || len(preview.LostComments) != 0 {
		t.Errorf("changes %q", preview.Changes)
	}
	// Only the edited lines change (row ST-27 through the endpoint).
	if !strings.Contains(preview.Diff, "-    labels: technical   # decided 2026-10\n+    labels: plain   # decided 2026-10\n") || strings.Count(preview.Diff, "\n-") != 2 {
		t.Errorf("diff:\n%s", preview.Diff)
	}
	code, applied, msg := ss.post("", editBody(hash, "operations", ops, preview.ResultHash))
	if code != http.StatusOK || !applied.Applied || manifestHash([]byte(ss.file())) != preview.ResultHash || unifiedDiff(config.File, string(data), ss.file()) != preview.Diff {
		t.Fatalf("apply: %d %s", code, msg)
	}
	doc := ss.get("")
	if k := keyByName(t, doc, "clients.base_paths"); k.Origin != config.OriginFile {
		t.Errorf("after apply: %+v", k)
	}
	// A map edited entry by entry: added, changed (with the value it
	// replaces) and removed entries in words; nothing else changes.
	preview, _ = ss.previewApply("operations", `[{"op":"set","key":"clients.base_paths","value":[{"key":"web","value":"/api/v2"},{"key":"admin","value":"/admin"}]}]`)
	if strings.Join(preview.Changes, "; ") != "sets clients.base_paths: adds `admin: /admin`; changes `web: /api/v2` (was `/api`)" || preview.Reformatted {
		t.Errorf("map words: %q %v", preview.Changes, preview.Reformatted)
	}
	preview, _ = ss.previewApply("operations", `[{"op":"set","key":"clients.base_paths","value":[{"key":"admin","value":"/admin"}]}]`)
	if strings.Join(preview.Changes, "; ") != "sets clients.base_paths: removes `web: /api/v2`" {
		t.Errorf("map removal words: %q", preview.Changes)
	}
	// Remove: the key and its emptied section go, the default is back,
	// and an at-once key needs no re-index. Its comments go with it, and
	// the preview names each one.
	preview, _ = ss.previewApply("operations", `[{"op":"remove","key":"ui.labels"}]`)
	if preview.ReindexNeeded || len(preview.ChangedKeys) != 0 || strings.Contains(ss.file(), "ui:") || strings.Join(preview.Changes, "; ") != "removes ui.labels (the built-in `plain` applies); drops the comment # Labels: the team reads ids. (line 5); drops the comment # decided 2026-10 (line 7)" {
		t.Errorf("remove: %+v\n%s", preview, ss.file())
	}
	if !reflect.DeepEqual(preview.LostComments, []config.Comment{{Line: 5, Text: "# Labels: the team reads ids."}, {Line: 7, Text: "# decided 2026-10"}}) {
		t.Errorf("lost comments %+v", preview.LostComments)
	}
	if k := keyByName(t, ss.get(""), "ui.labels"); k.Origin != config.OriginBuiltin || k.Value != "plain" {
		t.Errorf("after remove: %+v", k)
	}
	// Setting the value the file already has changes nothing.
	hash = *ss.get("").Hash
	same := `[{"op":"set","key":"index.shape_depth","value":8}]`
	_, p, _ := ss.post("", editBody(hash, "operations", same, ""))
	if code, _, msg := ss.post("", editBody(hash, "operations", same, p.ResultHash)); code != http.StatusBadRequest || !strings.HasPrefix(msg, "no_change ") || p.Diff != "" {
		t.Errorf("no change: %d %s", code, msg)
	}
}

// TestSettingsTypedValues checks row ST-26: a key outside the form, an
// unknown operation and a value of the wrong JSON type are refused before
// anything is read; a value of the right type the decoder refuses is
// previewed as invalid with the decoder's message and never written; a
// locked key is refused.
func TestSettingsTypedValues(t *testing.T) {
	ss := newSettingsServer(t, "version: 1\nide:\n  link_template: 'subl://open?url={abs_path}'\n")
	hash := *ss.get("").Hash
	for _, c := range []struct {
		ops    string
		status int
		code   string
	}{
		{`[{"op":"set","key":"changes.max_pins","value":3}]`, 400, "unknown_key"},
		{`[{"op":"set","key":"repository","value":"x"}]`, 400, "unknown_key"},
		{`[{"op":"rename","key":"ui.labels","value":"x"}]`, 400, "bad_request"},
		{`[{"op":"remove","key":"ui.labels","value":"x"}]`, 400, "bad_request"},
		{`[{"op":"set","key":"ui.labels"}]`, 400, "bad_request"},
		{`[{"op":"set","key":"index.shape_depth","value":"6"}]`, 400, "invalid_value"},
		{`[{"op":"set","key":"index.shape_depth","value":6.5}]`, 400, "invalid_value"},
		{`[{"op":"set","key":"clients.link_generated_output","value":"yes"}]`, 400, "invalid_value"},
		{`[{"op":"set","key":"tests.include","value":"a"}]`, 400, "invalid_value"},
		{`[{"op":"set","key":"tests.include","value":[1]}]`, 400, "invalid_value"},
		{`[{"op":"set","key":"openapi.base_paths","value":{"a":"/b"}}]`, 400, "invalid_value"},
		{`[{"op":"set","key":"openapi.base_paths","value":[{"key":"a","value":"/b","x":1}]}]`, 400, "invalid_value"},
		{`[{"op":"set","key":"ide.editor","value":"zed"}]`, 400, "edit_refused"},
		{`[{"op":"remove","key":"ui.labels"}]`, 400, "edit_refused"},
	} {
		if code, _, msg := ss.post("", editBody(hash, "operations", c.ops, "")); code != c.status || !strings.HasPrefix(msg, c.code+" ") {
			t.Errorf("%s: %d %s", c.ops, code, msg)
		}
	}
	if k := keyByName(t, ss.get(""), "ide.editor"); k.Editable || k.Reason == nil || !strings.Contains(*k.Reason, "ide.link_template") {
		t.Errorf("locked editor %+v", k)
	}
	out := `[{"op":"set","key":"index.shape_depth","value":11}]`
	code, preview, _ := ss.post("", editBody(hash, "operations", out, ""))
	_, want := config.Parse(config.File, []byte("version: 1\nide:\n  link_template: 'subl://open?url={abs_path}'\nindex:\n  shape_depth: 11\n"))
	if code != http.StatusOK || preview.Valid || len(preview.Problems) != 1 || preview.Problems[0] != want.Error() {
		t.Fatalf("out of range preview: %d %+v (want %v)", code, preview, want)
	}
	if code, _, msg := ss.post("", editBody(hash, "operations", out, preview.ResultHash)); code != http.StatusUnprocessableEntity || !strings.HasPrefix(msg, "invalid_config ") || !strings.Contains(msg, "index.shape_depth must be an integer from 1 to 10") {
		t.Errorf("out of range apply: %d %s", code, msg)
	}
	if ss.file() != "version: 1\nide:\n  link_template: 'subl://open?url={abs_path}'\n" {
		t.Errorf("invalid edit wrote %q", ss.file())
	}
}

// TestSettingsRawText checks row ST-28: the whole text is validated like a
// typed edit; invalid YAML is refused with the decoder's message and never
// written; a change outside the form is reported as such.
func TestSettingsRawText(t *testing.T) {
	ss := newSettingsServer(t, "version: 1\n")
	hash := *ss.get("").Hash
	broken := `"version: 1\nui: [plain\n"`
	code, preview, _ := ss.post("", editBody(hash, "text", broken, ""))
	_, want := config.Parse(config.File, []byte("version: 1\nui: [plain\n"))
	if code != http.StatusOK || preview.Valid || len(preview.Problems) != 1 || preview.Problems[0] != want.Error() || !strings.HasPrefix(want.Error(), "graphvein.yaml: yaml: ") {
		t.Fatalf("invalid YAML preview: %d %+v (want %v)", code, preview, want)
	}
	if code, _, msg := ss.post("", editBody(hash, "text", broken, preview.ResultHash)); code != http.StatusUnprocessableEntity || !strings.HasPrefix(msg, "invalid_config ") {
		t.Errorf("invalid YAML apply: %d %s", code, msg)
	}
	unknown := `"version: 1\nfrobnicate: true\n"`
	_, preview, _ = ss.post("", editBody(hash, "text", unknown, ""))
	if preview.Valid || !strings.Contains(preview.Problems[0], "unknown key frobnicate") {
		t.Errorf("unknown key: %+v", preview)
	}
	if ss.file() != "version: 1\n" {
		t.Fatalf("a refused text wrote %q", ss.file())
	}
	preview, _ = ss.previewApply("text", `"# kept\nversion: 1\nchanges:\n  max_pins: 3\ntests:\n  include: ['checks/**']\n"`)
	if !preview.OtherKeysChanged || !preview.ReindexNeeded || len(preview.ChangedKeys) != 1 || preview.ChangedKeys[0].Key != "tests.include" || preview.Changes[0] != "replaces graphvein.yaml with the text given" {
		t.Errorf("text edit: %+v", preview)
	}
	if !strings.HasPrefix(ss.file(), "# kept\n") {
		t.Errorf("written %q", ss.file())
	}
}

// TestSettingsAppliesAtOnce checks row ST-17: an at-once key written
// through the edit is what the next request of its endpoint reads, with
// no re-index.
func TestSettingsAppliesAtOnce(t *testing.T) {
	ss := newSettingsServer(t, "")
	preview, _ := ss.previewApply("operations", `[{"op":"set","key":"ui.labels","value":"technical"},{"op":"set","key":"ide.editor","value":"zed"}]`)
	if preview.ReindexNeeded {
		t.Errorf("at-once keys offered a re-index: %+v", preview)
	}
	var ui uiResponse
	_ = json.Unmarshal(ss.do(http.MethodGet, "/api/v0/ui", "", nil).Body.Bytes(), &ui)
	var ide ideResponse
	_ = json.Unmarshal(ss.do(http.MethodGet, "/api/v0/ide", "", nil).Body.Bytes(), &ide)
	if ui.Labels == nil || *ui.Labels != "technical" || ide.Repository.Editor == nil || *ide.Repository.Editor != "zed" {
		t.Errorf("ui %+v, ide %+v", ui, ide.Repository)
	}
	if ss.file() != "version: 1\nui:\n  labels: technical\nide: zed\n" {
		t.Errorf("written %q", ss.file())
	}
	// The walk depths (rows ST-31, ST-32) are read by GET /api/v0/ui too.
	preview, _ = ss.previewApply("operations", `[{"op":"set","key":"ui.impact_depth","value":4},{"op":"set","key":"ui.chain_depth","value":8}]`)
	if preview.ReindexNeeded {
		t.Errorf("depth keys offered a re-index: %+v", preview)
	}
	ui = uiResponse{}
	_ = json.Unmarshal(ss.do(http.MethodGet, "/api/v0/ui", "", nil).Body.Bytes(), &ui)
	if ui.ImpactDepth == nil || *ui.ImpactDepth != 4 || ui.ChainDepth == nil || *ui.ChainDepth != 8 {
		t.Errorf("depths after the edit: %+v", ui)
	}
	for _, k := range config.SettingKeys() {
		if k.Applies == config.AppliesAtOnce && !map[string]bool{"ui.labels": true, "ide.editor": true, "architecture.hide": true, "ui.impact_depth": true, "ui.chain_depth": true}[k.Key] {
			t.Errorf("%s is at once but no endpoint is known to read it per request", k.Key)
		}
	}
}

// TestSettingsProject checks rows ST-18, ST-20 and ST-21 in project mode:
// each member's own file, the manifest's environments read-only with the
// edit that changes them, the project re-index, and a member without a
// checkout that is neither read nor written.
func TestSettingsProject(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		root := copyProjectFixture(t)
		manifest := usersManifest + "tryit:\n  environments:\n    - name: shared\n      label: staging\n      base_urls:\n        backend: http://127.0.0.1:8000\n"
		if err := os.WriteFile(filepath.Join(root, config.ProjectFile), []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "backend", config.File), []byte("version: 1\nindex:\n  shape_depth: 4\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		project, err := config.LoadProject(root)
		if err != nil {
			t.Fatal(err)
		}
		project.Members = append(project.Members, config.Member{Repository: "ghost", RepositorySource: config.RepositoryFromManifest})
		ss := &settingsServer{explorerServer: &explorerServer{t: t, h: NewHandler(projectOptions(project, s), testHosts)}, root: filepath.Join(root, "backend")}
		if rec := ss.do(http.MethodGet, configPath, "", nil); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "repository_required") {
			t.Fatalf("no member named: %d %s", rec.Code, rec.Body)
		}
		backend := ss.get("?repository=backend")
		if backend.Repository != "backend" || backend.Reindex.Scope != "project" || keyByName(t, backend, "index.shape_depth").Value != 4.0 {
			t.Fatalf("backend %+v", backend)
		}
		p := backend.Project
		if p == nil || p.File != config.ProjectFile || p.Error != nil || p.EditedBy != tryItEnvironmentsPath || len(p.Environments) != 1 {
			t.Fatalf("project part %+v", p)
		}
		if e := p.Environments[0]; e.Name != "shared" || e.Label != "staging" || e.Line != 10 || !e.ForMember || len(e.BaseURLs) != 1 || e.BaseURLs[0] != (configBaseURLDoc{"backend", "http://127.0.0.1:8000"}) {
			t.Errorf("environment %+v", e)
		}
		frontend := ss.get("?repository=frontend")
		if frontend.Repository != "frontend" || frontend.Project.Environments[0].ForMember || keyByName(t, frontend, "index.shape_depth").Origin == config.OriginFile {
			t.Errorf("frontend %+v", frontend.Project)
		}
		// The member's own file is edited, through its own token.
		ss.token = backend.Token
		code, _, msg := ss.post("?repository=backend", editBody(*backend.Hash, "operations", `[{"op":"set","key":"index.shape_depth","value":5}]`, ""))
		if code != http.StatusOK {
			t.Fatalf("backend preview: %d %s", code, msg)
		}
		ghost := ss.get("?repository=ghost")
		if ghost.Editable || ghost.Reason == nil || ghost.Exists != nil || ghost.Hash != nil || ghost.Text != nil || ghost.Ignore.Exists != nil || ghost.Project == nil {
			t.Fatalf("ghost %+v", ghost)
		}
		for _, k := range ghost.Keys {
			if k.Value != nil || k.Origin != "unknown" || k.Editable {
				t.Errorf("ghost key %+v", k)
			}
		}
		ss.token = ghost.Token
		if code, _, msg := ss.post("?repository=ghost", `{"base_hash":"absent","operations":[{"op":"remove","key":"ui.labels"}]}`); code != http.StatusConflict || !strings.HasPrefix(msg, "no_checkout ") {
			t.Errorf("ghost edit: %d %s", code, msg)
		}
	})
}

// TestSettingsContractTable keeps docs/contract/claims/settings.md, the
// row list and the key table 1:1, and checks that every row names a test
// that exists.
func TestSettingsContractTable(t *testing.T) {
	doc, err := os.ReadFile("../../docs/contract/claims/settings.md")
	if err != nil {
		t.Fatal(err)
	}
	var documented, code []string
	keys := map[string]string{}
	for _, m := range regexp.MustCompile("(?m)^\\| (ST-[0-9]+) \\| (`([a-z_.]+)`)?").FindAllSubmatch(doc, -1) {
		documented = append(documented, string(m[1]))
		if len(m[3]) > 0 {
			keys[string(m[1])] = string(m[3])
		}
	}
	for _, r := range settingsRows {
		code = append(code, r.ID)
	}
	if strings.Join(documented, ",") != strings.Join(code, ",") {
		t.Fatalf("settings.md rows %v\ncode rows %v", documented, code)
	}
	table := map[string]string{}
	for _, k := range config.SettingKeys() {
		table[k.Row] = k.Key
	}
	if len(table) != len(keys) {
		t.Errorf("key rows in settings.md %v, key table %v", keys, table)
	}
	for row, key := range table {
		if keys[row] != key {
			t.Errorf("row %s: settings.md names %q, the key table %q", row, keys[row], key)
		}
	}
	var sources bytes.Buffer
	for _, pattern := range []string{"*_test.go", "../config/*_test.go", "../store/*_test.go", "../../cmd/graphvein/*_test.go"} {
		files, _ := filepath.Glob(pattern)
		for _, file := range files {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			sources.Write(data)
		}
	}
	for _, r := range settingsRows {
		if file, name, ok := strings.Cut(r.Test, ": "); ok && strings.HasPrefix(file, "web/") {
			// A web UI row: the test file must hold a test of that name.
			data, err := os.ReadFile(filepath.Join("../..", file))
			if err != nil || !strings.Contains(string(data), `("`+name+`"`) {
				t.Errorf("row %s names web test %q in %s, which does not exist (%v)", r.ID, name, file, err)
			}
			continue
		}
		if !strings.Contains(sources.String(), "func "+r.Test+"(t *testing.T)") {
			t.Errorf("row %s names test %s, which does not exist", r.ID, r.Test)
		}
	}
}
