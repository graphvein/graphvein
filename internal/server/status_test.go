package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/store"
)

// finishedAtPlaceholder replaces the real finish time in goldens.
const finishedAtPlaceholder = "2026-11-03T14:32:05.123Z"

// checkStatusGolden compares a status body with a golden after replacing
// the run's finish time, the only value the test cannot fix.
func checkStatusGolden(t *testing.T, name string, body []byte) {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatal(err)
	}
	if idx, ok := v["index"].(map[string]any); ok {
		if run, ok := idx["last_run"].(map[string]any); ok {
			run["finished_at"] = finishedAtPlaceholder
		}
	}
	golden.Check(t, filepath.Join("testdata", name+".golden.json"), v)
}

func TestStatusStates(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		ctx := context.Background()
		root := fixtureRoot(t)
		// The handler reads through the read-only store, as serve does;
		// indexing writes through w while that store stays open, as a
		// graphvein index run next to a running serve does.
		opt := testOptions(root, s)

		// A migrated store without runs: not indexed, every file listed.
		rec := get(t, opt, http.MethodGet, "/api/v0/status", nil)
		got := decodeStatus(t, rec)
		if got.Freshness.State != stateNotIndexed || got.Index.Indexed || got.Index.LastRun != nil || got.Extractor.Indexed != nil || got.Extractor.Matches != nil {
			t.Fatalf("empty store: %+v", got)
		}
		want, err := index.Freshness(ctx, root, "fixture", s)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got.Freshness.ChangedFiles, want) || *got.Freshness.ChangedCount != len(want) || len(want) == 0 {
			t.Fatalf("not indexed lists %v, status lists %v", got.Freshness.ChangedFiles, want)
		}
		for _, h := range [][2]string{{"Content-Type", "application/json; charset=utf-8"}, {"Cache-Control", "no-store"}} {
			if rec.Header().Get(h[0]) != h[1] {
				t.Errorf("%s = %q", h[0], rec.Header().Get(h[0]))
			}
		}
		checkStatusGolden(t, "status-not-indexed", rec.Body.Bytes())

		indexRoot(t, root, w)
		rec = get(t, opt, http.MethodGet, "/api/v0/status", nil)
		got = decodeStatus(t, rec)
		if got.Freshness.State != stateFresh || *got.Freshness.ChangedCount != 0 || len(got.Freshness.ChangedFiles) != 0 || !got.Index.Indexed {
			t.Fatalf("after index: %+v", got)
		}
		if got.Index.LastRun == nil || got.Index.LastRun.Indexer.Name != "graphvein" || got.Extractor.Matches == nil || !*got.Extractor.Matches {
			t.Fatalf("last run or extractor: %+v", got)
		}
		checkStatusGolden(t, "status-fresh", rec.Body.Bytes())

		// Edit, add and delete one file each.
		if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "helper", "added.go"), []byte("package helper\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(root, "extra.go")); err != nil {
			t.Fatal(err)
		}
		rec = get(t, opt, http.MethodGet, "/api/v0/status", nil)
		got = decodeStatus(t, rec)
		stale := []string{"extra.go", "helper/added.go", "main.go"}
		if got.Freshness.State != stateStale || !reflect.DeepEqual(got.Freshness.ChangedFiles, stale) || *got.Freshness.ChangedCount != 3 {
			t.Fatalf("after edits: %+v", got.Freshness)
		}
		want, err = index.Freshness(ctx, root, "fixture", s)
		if err != nil || !reflect.DeepEqual(want, stale) {
			t.Fatalf("status lists %v, %v", want, err)
		}
		checkStatusGolden(t, "status-stale", rec.Body.Bytes())

		// A failed freshness read is unknown, never fresh, and keeps the
		// run information.
		rec = get(t, testOptions(root, failingStore{Store: s, hashes: true}), http.MethodGet, "/api/v0/status", nil)
		got = decodeStatus(t, rec)
		if got.Freshness.State != stateUnknown || got.Freshness.ChangedCount != nil || got.Freshness.ChangedFiles == nil || got.Freshness.Error != errInjected.Error() {
			t.Fatalf("store read failure: %+v", got.Freshness)
		}
		checkStatusGolden(t, "status-unknown", rec.Body.Bytes())

		// An incomplete scan (an unreadable .graphveinignore; a directory
		// fails to read on every OS) is unknown as well.
		if err := os.Mkdir(filepath.Join(root, ".graphveinignore"), 0o755); err != nil {
			t.Fatal(err)
		}
		got = decodeStatus(t, get(t, opt, http.MethodGet, "/api/v0/status", nil))
		if got.Freshness.State != stateUnknown || !strings.HasPrefix(got.Freshness.Error, "freshness scan incomplete:") {
			t.Fatalf("incomplete scan: %+v", got.Freshness)
		}
	})
}

func TestStatusRunsFailure(t *testing.T) {
	backends(t, func(t *testing.T, _, s store.Store) {
		rec := get(t, testOptions(t.TempDir(), failingStore{Store: s, runs: true}), http.MethodGet, "/api/v0/status", nil)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status %d", rec.Code)
		}
		var env errorEnvelope
		if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
			t.Fatal(err)
		}
		if env.Error.Code != "store_error" || env.Error.Message != "reading runs: injected failure" {
			t.Fatalf("envelope %+v", env)
		}
		if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("Content-Security-Policy") == "" {
			t.Fatalf("headers %v", rec.Header())
		}
	})
}

// TestCheckedAtIsScanStart proves checked_at is taken before the walk: a
// file added at the instant the clock is read is seen by the same check, so
// "fresh as of checked_at" never names a time after the scan began.
func TestCheckedAtIsScanStart(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		root := fixtureRoot(t)
		indexRoot(t, root, w)
		opt := testOptions(root, s)
		calls := 0
		opt.Now = func() time.Time {
			calls++
			if err := os.WriteFile(filepath.Join(root, "late.go"), []byte("package main\n"), 0o644); err != nil {
				t.Error(err)
			}
			return fixedNow()
		}
		got := decodeStatus(t, get(t, opt, http.MethodGet, "/api/v0/status", nil))
		if calls != 1 {
			t.Fatalf("clock read %d times, want once", calls)
		}
		if got.Freshness.State != stateStale || !reflect.DeepEqual(got.Freshness.ChangedFiles, []string{"late.go"}) {
			t.Fatalf("a file written when checked_at was taken is missing: %+v", got.Freshness)
		}
		if got.Freshness.CheckedAt != "2026-11-03T14:40:00Z" {
			t.Fatalf("checked_at %q", got.Freshness.CheckedAt)
		}
	})
}

func TestLatestRun(t *testing.T) {
	run := func(id, finished string) store.Run {
		return store.Run{ID: id, FinishedAt: finished, Header: contract.Header{ContractVersion: 1, Indexer: contract.Indexer{Name: "graphvein", Version: id}}}
	}
	// IDs are random, so store order says nothing; ".1Z" sorts after
	// ".12Z" as text but is the earlier time.
	runs := []store.Run{
		run("b", "2026-11-03T14:32:05.12Z"),
		run("a", "2026-11-03T14:32:05.1Z"),
		run("c", "not a time"),
	}
	got := latestRun(runs)
	if got == nil || got.Indexer.Version != "b" || got.FinishedAt != "2026-11-03T14:32:05.12Z" {
		t.Fatalf("latest = %+v", got)
	}
	if got := latestRun([]store.Run{run("c", "")}); got != nil {
		t.Fatalf("unparsable only = %+v", got)
	}
	if got := latestRun(nil); got != nil {
		t.Fatalf("none = %+v", got)
	}
	withCommit := run("d", "2026-11-03T15:00:00+01:00")
	withCommit.Header.Commit = "abc123"
	got = latestRun(append(runs, withCommit))
	if got.Indexer.Version != "b" {
		t.Fatalf("offset time compared as UTC: %+v", got)
	}
	got = latestRun([]store.Run{withCommit})
	if got.Commit != "abc123" || got.FinishedAt != "2026-11-03T14:00:00Z" {
		t.Fatalf("commit run = %+v", got)
	}
}

func TestFreshnessTruncation(t *testing.T) {
	changed := make([]string, maxChangedFiles+5)
	for i := range changed {
		changed[i] = fmt.Sprintf("f%05d.go", i)
	}
	got := freshnessFor(changed, nil, true)
	if got.State != stateStale || *got.ChangedCount != maxChangedFiles+5 || len(got.ChangedFiles) != maxChangedFiles || !got.ChangedFilesTruncated || got.ChangedFiles[maxChangedFiles-1] != changed[maxChangedFiles-1] {
		t.Fatalf("truncated: state %s count %d listed %d truncated %v", got.State, *got.ChangedCount, len(got.ChangedFiles), got.ChangedFilesTruncated)
	}
	got = freshnessFor(changed[:maxChangedFiles], nil, true)
	if got.ChangedFilesTruncated || len(got.ChangedFiles) != maxChangedFiles {
		t.Fatal("exactly the limit is not truncated")
	}
	if got := freshnessFor(nil, nil, false); got.State != stateNotIndexed || got.ChangedFiles == nil {
		t.Fatalf("not indexed, empty: %+v", got)
	}
	if got := freshnessFor(nil, nil, true); got.State != stateFresh || got.ChangedFiles == nil {
		t.Fatalf("fresh: %+v", got)
	}
}

func TestExtractorStatus(t *testing.T) {
	for _, tc := range []struct {
		name, indexer, version string
		indexed                *string
		matches                *bool
	}{
		{"current graphvein", "graphvein", "extractors-27", ptr("extractors-27"), ptr(true)},
		{"older graphvein", "graphvein", "extractors-26", ptr("extractors-26"), ptr(false)},
		{"external indexer", "scip-go", "extractors-27", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := extractorFor("extractors-27", &lastRun{Indexer: contract.Indexer{Name: tc.indexer, Version: tc.version}})
			if got.Current != "extractors-27" || !reflect.DeepEqual(got.Indexed, tc.indexed) || !reflect.DeepEqual(got.Matches, tc.matches) {
				t.Fatalf("got %+v", got)
			}
		})
	}
	if got := extractorFor("extractors-27", nil); got.Indexed != nil || got.Matches != nil {
		t.Fatalf("no run: %+v", got)
	}
}

func ptr[T any](v T) *T { return &v }

// TestStatusSettings checks row ST-46 (and ST-43, ST-44 at the API):
// freshness.settings of /api/v0/status and index_settings of
// /api/v0/config compare the settings the last run read with the current
// graphvein.yaml; neither changes the file state.
func TestStatusSettings(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		ctx := context.Background()
		root := fixtureRoot(t)
		opt := testOptions(root, s)
		status := func() *settingsStatus {
			t.Helper()
			got := decodeStatus(t, get(t, opt, http.MethodGet, "/api/v0/status", nil))
			if got.Freshness.Settings == nil {
				t.Fatal("status without settings")
			}
			var cfg configResponse
			decodeInto(t, get(t, opt, http.MethodGet, configPath, nil), &cfg)
			if !reflect.DeepEqual(cfg.IndexSettings, got.Freshness.Settings) {
				t.Fatalf("config GET index_settings %+v, status %+v", cfg.IndexSettings, got.Freshness.Settings)
			}
			return got.Freshness.Settings
		}
		write := func(text string) {
			t.Helper()
			if err := os.WriteFile(filepath.Join(root, "graphvein.yaml"), []byte(text), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if got := status(); got.State != settingsNotIndexed || got.IndexHash != nil || got.CurrentHash != nil {
			t.Fatalf("not indexed: %+v", got)
		}
		write("version: 1\nindex:\n  shape_depth: 4\n")
		indexRoot(t, root, w)
		got := status()
		if got.State != config.IndexSettingsUnchanged || got.IndexHash == nil || got.CurrentHash == nil || *got.IndexHash != *got.CurrentHash || got.Reason != nil {
			t.Fatalf("after index: %+v", got)
		}
		// Comments, layout and at-once keys never make the index stale.
		write("# team file\nversion: 1\nui:\n  labels: technical\nide: zed\nindex:\n    shape_depth: 4   # slow\n")
		if got := status(); got.State != config.IndexSettingsUnchanged {
			t.Fatalf("comment and at-once edit: %+v", got)
		}
		write("version: 1\ntests:\n  include: ['checks/**']\n")
		got = status()
		if got.State != config.IndexSettingsChanged || !reflect.DeepEqual(got.ChangedKeys, []string{"index.shape_depth", "tests.include"}) || *got.IndexHash == *got.CurrentHash {
			t.Fatalf("changed: %+v", got)
		}
		// The file state is the files' alone.
		if st := decodeStatus(t, get(t, opt, http.MethodGet, "/api/v0/status", nil)); st.Freshness.State != stateStale || !reflect.DeepEqual(st.Freshness.ChangedFiles, []string{"graphvein.yaml"}) {
			t.Fatalf("file freshness: %+v", st.Freshness)
		}
		write("version: 1\nindex:\n  shape_depth: 99\n")
		got = status()
		if got.State != config.IndexSettingsUnknown || got.Reason == nil || !strings.Contains(*got.Reason, "graphvein.yaml: line 3") || strings.Contains(*got.Reason, root) || got.CurrentHash != nil || got.IndexHash == nil {
			t.Fatalf("file does not load: %+v", got)
		}
		// A run that recorded nothing (an external indexer, an ingest)
		// is unknown, never unchanged.
		var stream bytes.Buffer
		write("version: 1\n")
		if _, err := index.Emit(ctx, root, w, &stream, index.Options{Repository: "fixture"}); err != nil {
			t.Fatal(err)
		}
		if err := ingest.Stream(ctx, &stream, w); err != nil {
			t.Fatal(err)
		}
		got = status()
		if got.State != config.IndexSettingsUnknown || got.Reason == nil || !strings.Contains(*got.Reason, "did not record") || got.IndexHash != nil {
			t.Fatalf("run without settings: %+v", got)
		}
	})
}
