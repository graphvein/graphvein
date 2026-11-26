package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/graphvein/graphvein/internal/changes"
	"github.com/graphvein/graphvein/internal/indexrun"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/precision"
	"github.com/graphvein/graphvein/internal/store"
)

const changeFixture = "../../testdata/fixtures/change-impact"

// replaceTree replaces dst's files (except .git) with src's.
func replaceTree(t *testing.T, src, dst string) {
	t.Helper()
	entries, _ := os.ReadDir(dst)
	for _, e := range entries {
		if e.Name() != ".git" {
			if err := os.RemoveAll(filepath.Join(dst, e.Name())); err != nil {
				t.Fatal(err)
			}
		}
	}
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// decodeMap decodes a 200-class body generically.
func decodeMap(t *testing.T, _ any, body []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("%v: %s", err, body)
	}
	return out
}

func bodyCode(t *testing.T, body []byte) string {
	t.Helper()
	var env errorEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("%v: %s", err, body)
	}
	return env.Error.Code
}

// flipStore answers every Runs call with another latest run, as if a run
// committed during every request.
type flipStore struct {
	store.Store
	calls int
}

func (f *flipStore) Runs(ctx context.Context, repo string) ([]store.Run, error) {
	runs, err := f.Store.Runs(ctx, repo)
	f.calls++
	at := time.Date(2030, 1, 1, 0, 0, f.calls, 0, time.UTC).Format(time.RFC3339Nano)
	return append(runs, store.Run{ID: "concurrent", FinishedAt: at}), err
}

// TestChangesAPI covers the change endpoints on both backends: shapes and
// freshness, paging stability, the cursor refused after a new run, 409
// when the index changes during a request, error codes, and the POST
// rules (content type, cross-origin, lock) of pins and refs.
func TestChangesAPI(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	replaceTree(t, filepath.Join(changeFixture, "rev1"), root)
	runBackends(t, root, func(t *testing.T, rb runBackend) {
		ctx := context.Background()
		// The next backend indexes rev1 again: restore it afterwards.
		defer func() {
			os.RemoveAll(filepath.Join(root, ".git"))
			replaceTree(t, filepath.Join(changeFixture, "rev1"), root)
		}()
		w, err := rb.open(ctx)
		if err != nil {
			t.Fatal(err)
		}
		replaceTree(t, filepath.Join(changeFixture, "rev2"), root)
		indexRoot(t, root, w)
		w.Close()
		opt := testOptions(root, rb.read)
		opt.Locker, opt.OpenWriter = rb.locker(), rb.open
		defer opt.Locker.Close()
		h, runs, refs := newServerHandler(opt, testHosts)
		defer runs.stop()
		defer refs.shutdown()
		do := func(method, path, body string, header map[string]string) (int, []byte) {
			t.Helper()
			req, _ := http.NewRequest(method, "http://"+testHost+path, strings.NewReader(body))
			req.Host = testHost
			for k, v := range header {
				req.Header.Set(k, v)
			}
			rec := &recorder{header: http.Header{}}
			h.ServeHTTP(rec, req)
			return rec.status, rec.body.Bytes()
		}

		// bases
		code, body := do("GET", changesBasesPath, "", nil)
		if code != 200 {
			t.Fatalf("bases: %d %s", code, body)
		}
		bases := decodeMap(t, nil, body)
		if len(bases["runs"].([]any)) != 2 || bases["freshness"] == nil || bases["retention"] == nil {
			t.Errorf("bases = %s", body)
		}

		// summary
		code, body = do("GET", changesSummaryPath+"?base=previous", "", nil)
		if code != 200 {
			t.Fatalf("summary: %d %s", code, body)
		}
		sum := decodeMap(t, nil, body)
		if sum["freshness"] == nil || sum["summary"].(map[string]any)["contract_version"] != changes.ContractVersion {
			t.Errorf("summary = %s", body)
		}

		// items: pages of 7 concatenate to the full list.
		code, body = do("GET", changesItemsPath+"?base=previous&tests=true&limit=500", "", nil)
		if code != 200 {
			t.Fatalf("items: %d %s", code, body)
		}
		full := decodeMap(t, nil, body)["items"].([]any)
		checkItemPrecision(t, full)
		var paged []any
		cursor := ""
		first := ""
		for {
			q := "?base=previous&tests=true&limit=7"
			if cursor != "" {
				q += "&cursor=" + url.QueryEscape(cursor)
			}
			code, body = do("GET", changesItemsPath+q, "", nil)
			if code != 200 {
				t.Fatalf("items page: %d %s", code, body)
			}
			page := decodeMap(t, nil, body)
			paged = append(paged, page["items"].([]any)...)
			next, _ := page["next_cursor"].(string)
			if first == "" {
				first = next
			}
			if next == "" {
				break
			}
			cursor = next
		}
		a, _ := json.Marshal(full)
		b, _ := json.Marshal(paged)
		if !bytes.Equal(a, b) || len(full) == 0 {
			t.Errorf("paged items (%d) differ from one page (%d)", len(paged), len(full))
		}

		// impact
		code, body = do("GET", changesImpactPath+"?base=previous&id="+url.QueryEscape("function:server/handlers.ts#listItems"), "", nil)
		if code != 200 || len(decodeMap(t, nil, body)["paths"].([]any)) == 0 {
			t.Errorf("impact: %d %s", code, body)
		}
		for _, c := range []struct {
			path   string
			status int
			code   string
		}{
			{changesImpactPath + "?base=previous&id=nope", 404, "not_found"},
			{changesImpactPath + "?base=previous&id=x&group=code", 400, "bad_request"},
			{changesItemsPath + "?base=bogus", 400, "bad_request"},
			{changesItemsPath + "?base=pin:none", 404, "base_unknown"},
			{changesItemsPath + "?base=run:none", 404, "base_unknown"},
			{changesItemsPath + "?base=previous&limit=501", 400, "bad_request"},
			{changesItemsPath + "?base=previous&cursor=garbage", 400, "bad_request"},
			{changesItemsPath + "?base=ref:a...b", 400, "bad_request"},
			{changesSummaryPath + "?base=ref:main", 503, "git_unavailable"},
		} {
			code, body := do("GET", c.path, "", nil)
			if code != c.status || bodyCode(t, body) != c.code {
				t.Errorf("GET %s = %d %s, want %d %s", c.path, code, body, c.status, c.code)
			}
		}

		// POST rules.
		jsonType := map[string]string{"Content-Type": "application/json"}
		if code, body := do("POST", changesPinsPath, `{"name":"rev2"}`, nil); code != 415 {
			t.Errorf("pin without content type = %d %s", code, body)
		}
		if code, body := do("POST", changesPinsPath, `{"name":"rev2"}`, map[string]string{"Content-Type": "application/json", "Sec-Fetch-Site": "cross-site"}); code != 403 {
			t.Errorf("cross-site pin = %d %s", code, body)
		}
		if code, body := do("PUT", changesPinsPath, `{}`, jsonType); code != 405 {
			t.Errorf("PUT pins = %d %s", code, body)
		}
		if code, body := do("POST", changesSummaryPath, `{}`, jsonType); code != 405 || !strings.Contains(string(body), "allowed: GET, HEAD") {
			t.Errorf("POST summary = %d %s", code, body)
		}
		if code, body := do("GET", changesPinsPath, "", nil); code != 405 || !strings.Contains(string(body), "allowed: POST") {
			t.Errorf("GET pins = %d %s", code, body)
		}
		if code, body := do("POST", changesPinsPath, `{"name":"a","name":"rev2"}`, jsonType); code != 400 || !strings.Contains(string(body), "duplicate member") {
			t.Errorf("pin with a duplicate member = %d %s", code, body)
		}
		if code, body := do("POST", changesPinsPath, `{"name":"rev2","extra":1}`, jsonType); code != 400 {
			t.Errorf("pin with an unknown member = %d %s", code, body)
		}
		if code, body := do("POST", changesPinsPath, `{"name":"rev2"}`, jsonType); code != 201 {
			t.Fatalf("pin = %d %s", code, body)
		}
		if code, body := do("POST", changesPinsPath, `{"name":"rev2"}`, jsonType); code != 409 || bodyCode(t, body) != "pin_exists" {
			t.Errorf("duplicate pin = %d %s", code, body)
		}
		holder := rb.locker()
		lease, err := holder.Acquire(ctx, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if code, body := do("POST", changesPinsDeletePath, `{"name":"rev2"}`, jsonType); code != 409 || bodyCode(t, body) != "index_locked" {
			t.Errorf("unpin under a held lock = %d %s", code, body)
		}
		lease.Release()
		holder.Close()
		code, pinned := do("GET", changesSummaryPath+"?base=pin:rev2", "", nil)
		if code != 200 {
			t.Errorf("summary against the pin = %d %s", code, pinned)
		}
		if code, body := do("POST", changesPinsDeletePath, `{"name":"rev2"}`, jsonType); code != 200 {
			t.Errorf("unpin = %d %s", code, body)
		}
		// R3-1: a deleted pin never answers from the cached comparison,
		// and a pin re-pointed under the same name compares with its new run.
		if code, body := do("GET", changesSummaryPath+"?base=pin:rev2", "", nil); code != 404 || bodyCode(t, body) != "base_unknown" {
			t.Errorf("summary against a deleted pin = %d %s", code, body)
		}
		oldest := bases["runs"].([]any)[1].(map[string]any)["run_id"].(string)
		if code, body := do("POST", changesPinsPath, `{"name":"rev2","run":"`+oldest+`"}`, jsonType); code != 201 {
			t.Fatalf("re-pin = %d %s", code, body)
		}
		code, repinned := do("GET", changesSummaryPath+"?base=pin:rev2", "", nil)
		if code != 200 {
			t.Errorf("summary against the re-pointed pin = %d %s", code, repinned)
		}
		baseRun := func(body []byte) any {
			return decodeMap(t, nil, body)["summary"].(map[string]any)["base"].(map[string]any)["run"]
		}
		if baseRun(pinned) == baseRun(repinned) || baseRun(repinned) != oldest {
			t.Errorf("re-pointed pin compares with run %v, want %s (was %v)", baseRun(repinned), oldest, baseRun(pinned))
		}
		if code, body := do("POST", changesPinsDeletePath, `{"name":"rev2"}`, jsonType); code != 200 {
			t.Errorf("unpin = %d %s", code, body)
		}
		if code, body := do("POST", changesPinsDeletePath, `{"name":"rev2"}`, jsonType); code != 404 || bodyCode(t, body) != "pin_unknown" {
			t.Errorf("second unpin = %d %s", code, body)
		}

		// refs: GET without a job, POST outside a git repository.
		if code, body := do("GET", changesRefsPath, "", nil); code != 200 || decodeMap(t, nil, body)["job"] != nil {
			t.Errorf("GET refs = %d %s", code, body)
		}
		if code, body := do("POST", changesRefsPath, `{"base":"main"}`, jsonType); code != 503 || bodyCode(t, body) != "git_unavailable" {
			t.Errorf("POST refs without git = %d %s", code, body)
		}

		// A ref job in a git repository, then a ref comparison.
		gitIn(t, root, "init", "-q")
		gitIn(t, root, "add", "-A")
		gitIn(t, root, "commit", "-q", "-m", "rev2")
		// R3-5: the index was made outside git and recorded no commit.
		if code, body := do("POST", changesRefsPath, `{"base":"HEAD"}`, jsonType); code != 409 || bodyCode(t, body) != "reindex_needed" {
			t.Errorf("POST refs without an indexed commit = %d %s", code, body)
		}
		runIndex(t, ctx, root, rb)
		if code, body := do("POST", changesRefsPath, `{"base":"HEAD"}`, jsonType); code != 202 {
			t.Fatalf("POST refs = %d %s", code, body)
		}
		deadline := time.Now().Add(30 * time.Second)
		for {
			_, body := do("GET", changesRefsPath, "", nil)
			job := decodeMap(t, nil, body)["job"].(map[string]any)
			if job["ended_at"] != nil {
				if job["state"] != "committed" {
					t.Fatalf("ref job = %s", body)
				}
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("ref job did not end")
			}
			time.Sleep(20 * time.Millisecond)
		}
		if code, body := do("GET", changesSummaryPath+"?base=ref:HEAD", "", nil); code != 200 {
			t.Errorf("ref summary = %d %s", code, body)
		}

		// A cursor of the previous head run is refused after a new run.
		w, err = rb.open(ctx)
		if err != nil {
			t.Fatal(err)
		}
		replaceTree(t, filepath.Join(changeFixture, "rev3"), root)
		indexRoot(t, root, w)
		w.Close()
		if code, body := do("GET", changesItemsPath+"?base=previous&tests=true&limit=7&cursor="+url.QueryEscape(first), "", nil); code != 409 || bodyCode(t, body) != "index_changed" {
			t.Errorf("stale cursor = %d %s", code, body)
		}

		// A run committing during every request: 409.
		flip := opt
		flip.Store = &flipStore{Store: rb.read}
		fh, fr, ff := newServerHandler(flip, testHosts)
		defer fr.stop()
		defer ff.shutdown()
		req, _ := http.NewRequest("GET", "http://"+testHost+changesSummaryPath+"?base=previous", nil)
		req.Host = testHost
		rec := &recorder{header: http.Header{}}
		fh.ServeHTTP(rec, req)
		if rec.status != 409 || bodyCode(t, rec.body.Bytes()) != "index_changed" {
			t.Errorf("summary during runs = %d %s", rec.status, rec.body.Bytes())
		}
	})
}

// runIndex indexes root as graphvein index does, recording its commit.
func runIndex(t *testing.T, ctx context.Context, root string, rb runBackend) {
	t.Helper()
	locker := rb.locker()
	defer locker.Close()
	lease, err := locker.Acquire(ctx, indexrun.LockWait)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	if _, err := indexrun.Run(ctx, indexrun.Options{Root: root, Repository: "fixture", Lease: lease, OpenStore: rb.open,
		Parser: parse.Options{Workers: 1, CacheDir: filepath.Join(t.TempDir(), "cache")}, MaxFileSize: indexrun.DefaultMaxFileSize}); err != nil {
		t.Fatal(err)
	}
}

// recorder is a minimal ResponseWriter.
type recorder struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (r *recorder) Header() http.Header { return r.header }
func (r *recorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = 200
	}
	return r.body.Write(b)
}
func (r *recorder) WriteHeader(s int) { r.status = s }

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false", "-c", "core.autocrlf=false", "-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// checkItemPrecision checks the labels of change items (T020): every item
// has a file label and a label per changed field; schema, model, route and
// structural items are labelled from their family, and only a rule row
// yields precise.
func checkItemPrecision(t *testing.T, items []any) {
	t.Helper()
	seen := map[string]int{}
	for _, raw := range items {
		it := raw.(map[string]any)
		p, ok := it["precision"].(map[string]any)
		if !ok {
			t.Fatalf("item without precision: %v", it)
		}
		fields, _ := p["fields"].(map[string]any)
		for _, f := range it["fields"].([]any) {
			name := f.(map[string]any)["name"].(string)
			if _, ok := fields[name]; !ok {
				t.Errorf("%s: field %s has no label", it["identity"], name)
			}
		}
		// A file label is precise only for a language with a file row, and
		// names that row.
		file := p["file"].(map[string]any)
		if language, _ := file["language"].(string); language != "" {
			// A file of a language with a file row may also be best_effort
			// when the indexer refused its structure (web/broken.ts,
			// TS-F2); it still names the row.
			want := precision.FileLabel(language)
			refused := want == string(precision.Precise) && file["precision"] == string(precision.BestEffort) && file["precision_reason"] == reasonStructureRefused
			if file["precision"] != want && !refused || want == string(precision.Precise) && file["precision_row"] != precision.FileRow(language) {
				t.Errorf("%s: file label %v, want %s from the file row of %s", it["identity"], file, want, language)
			}
		}
		scope, _ := p["scope"].(map[string]any)
		key := it["kind"].(string) + " "
		if scope == nil {
			key += "null"
		} else {
			key += scope["label"].(string) + " " + scope["row"].(string)
			if scope["label"] == "precise" && scope["reason"] != nil || scope["label"] == "best_effort" && scope["reason"] == nil {
				t.Errorf("%s: inconsistent scope %v", it["identity"], scope)
			}
		}
		seen[key]++
	}
	t.Logf("item labels: %v", seen)
	for _, want := range []string{"table precise PL-F1", "table precise PL-F2", "route best_effort D", "function null"} {
		if seen[want] == 0 {
			t.Errorf("no item labelled %q; labels seen: %v", want, seen)
		}
	}
}
