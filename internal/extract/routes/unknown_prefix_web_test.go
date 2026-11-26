package routes_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/graphvein/graphvein/internal/extract/routes"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

// TestUnknownPrefixWebContractTags is the tag half of the unknown-prefix
// route contract for Express, FastAPI and Axum (T033):
// indexing the unknown-prefix-mounts fixture reaches rows UP-5..UP-7 and
// their listed forms, the shared rows UP-8..UP-10 and their forms, and
// every D form of the step; every tag names a row, a listed form or a D
// form. The golden is internal/index's TestUnknownPrefixMountsGolden.
func TestUnknownPrefixWebContractTags(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../../testdata/fixtures/unknown-prefix-mounts")); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	s, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	var mu sync.Mutex
	tags := map[string]int{}
	routes.SetUnknownPrefixTag(func(row string) {
		mu.Lock()
		tags[row]++
		mu.Unlock()
	})
	t.Cleanup(func() { routes.SetUnknownPrefixTag(nil) })
	opt := index.Options{Repository: "fixture", GeneratedAt: "2026-11-23T00:00:00Z", Parser: parse.Options{Workers: 2, CacheDir: filepath.Join(t.TempDir(), "cache")}}
	var stream bytes.Buffer
	if _, err := index.Emit(ctx, root, s, &stream, opt); err != nil {
		t.Fatal(err)
	}
	rows := map[string]routes.UnknownPrefixRow{}
	for _, r := range routes.UnknownPrefixRows {
		rows[r.ID] = r
	}
	defaults := map[string]bool{}
	for _, f := range routes.UnknownPrefixWebDefaultForms {
		defaults[f] = true
	}
	var got []string
	for tag := range tags {
		got = append(got, tag)
		row, form, hasForm := strings.Cut(tag, "/")
		r, ok := rows[row]
		switch {
		case !ok:
			t.Errorf("tag %s is not a contract row", tag)
		case !hasForm:
		case row == "D":
			if !defaults[form] {
				t.Errorf("tag %s is not a D form of UP-A2", tag)
			}
		default:
			listed := false
			for _, f := range r.Forms {
				listed = listed || f == form
			}
			if !listed {
				t.Errorf("tag %s is not a listed form of %s", tag, row)
			}
		}
	}
	sort.Strings(got)
	for _, r := range routes.UnknownPrefixRows {
		shared := r.ID == "UP-8" || r.ID == "UP-9" || r.ID == "UP-10"
		if r.Unit != "UP-A2" && !shared {
			continue
		}
		if tags[r.ID] == 0 {
			t.Errorf("the fixture does not reach row %s", r.ID)
		}
		for _, f := range r.Forms {
			if tags[r.ID+"/"+f] == 0 {
				t.Errorf("the fixture does not reach form %s/%s", r.ID, f)
			}
		}
	}
	for f := range defaults {
		if tags["D/"+f] == 0 {
			t.Errorf("the fixture does not reach D form %s", f)
		}
	}
	if t.Failed() {
		t.Logf("tags: %v", got)
	}
}
