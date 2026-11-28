package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/store"
)

// TestParseFailedCounts pins T074's additive fields: a run with files it
// could not parse says how many in GET /api/v0/status (index.parse_failed)
// and GET /api/v0/architecture (parse_failed); a run that read every file
// omits both.
func TestParseFailedCounts(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		root := fixtureRoot(t)
		opt := testOptions(root, s)
		// Every parse outlasts its deadline, the serial retry's too.
		restore := parse.HoldParseUntilDeadline()
		t.Cleanup(restore)
		var stream bytes.Buffer
		summary, err := index.Emit(context.Background(), root, w, &stream, index.Options{Repository: "fixture", Parser: parse.Options{Workers: 2, Timeout: 5 * time.Millisecond, CacheDir: filepath.Join(t.TempDir(), "cache")}})
		restore()
		if err != nil {
			t.Fatal(err)
		}
		if err := ingest.Stream(context.Background(), &stream, w); err != nil {
			t.Fatal(err)
		}
		if summary.ParseFailed == 0 || summary.ParseFailed != summary.Parsed {
			t.Fatalf("summary %+v, want every parsed file failed", summary)
		}
		status := decodeStatus(t, get(t, opt, http.MethodGet, "/api/v0/status", nil))
		if status.Index.ParseFailed != summary.ParseFailed {
			t.Fatalf("status parse_failed %d, want %d", status.Index.ParseFailed, summary.ParseFailed)
		}
		rec := get(t, opt, http.MethodGet, "/api/v0/architecture", nil)
		var arch struct {
			ParseFailed *int `json:"parse_failed"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &arch); err != nil || rec.Code != http.StatusOK {
			t.Fatal(rec.Code, err, rec.Body.String())
		}
		if arch.ParseFailed == nil || *arch.ParseFailed != summary.ParseFailed {
			t.Fatalf("architecture parse_failed %v, want %d", arch.ParseFailed, summary.ParseFailed)
		}
		// A failed parse is not cached: the next run reads every file.
		indexRoot(t, root, w)
		for _, path := range []string{"/api/v0/status", "/api/v0/architecture"} {
			rec := get(t, opt, http.MethodGet, path, nil)
			if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), `"parse_failed"`) {
				t.Fatalf("%s: %d %s", path, rec.Code, rec.Body.String())
			}
		}
	})
}

// TestProjectParseFailed pins T078's member field: GET /api/v0/project
// says, per member, how many files its stored run could not parse, so the
// project-wide Couldn't read view names the members whose counts are
// incomplete; a member that read every file omits it.
func TestProjectParseFailed(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		root := copyProjectFixture(t)
		project, err := config.LoadProject(root)
		if err != nil {
			t.Fatal(err)
		}
		restore := parse.HoldParseUntilDeadline()
		t.Cleanup(restore)
		var stream bytes.Buffer
		summary, err := index.Emit(context.Background(), filepath.Join(root, "frontend"), w, &stream, index.Options{Repository: "frontend", Parser: parse.Options{Workers: 2, Timeout: 5 * time.Millisecond, CacheDir: filepath.Join(t.TempDir(), "cache")}})
		restore()
		if err != nil {
			t.Fatal(err)
		}
		if err := ingest.Stream(context.Background(), &stream, w); err != nil {
			t.Fatal(err)
		}
		if summary.ParseFailed == 0 {
			t.Fatalf("summary %+v, want parse failures", summary)
		}
		indexRepository(t, filepath.Join(root, "backend"), w, "backend")
		opt := projectOptions(project, s)
		rec := get(t, opt, http.MethodGet, "/api/v0/project", nil)
		if rec.Code != http.StatusOK {
			t.Fatal(rec.Code, rec.Body.String())
		}
		var doc struct {
			Members []map[string]json.RawMessage `json:"members"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
			t.Fatal(err)
		}
		got := map[string]string{}
		for _, m := range doc.Members {
			var repo string
			if err := json.Unmarshal(m["repository"], &repo); err != nil {
				t.Fatal(err)
			}
			got[repo] = string(m["parse_failed"])
		}
		want := map[string]string{"frontend": fmt.Sprint(summary.ParseFailed), "backend": ""}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("members parse_failed %v, want %v", got, want)
		}
	})
}
