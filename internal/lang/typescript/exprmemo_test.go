package typescript

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/scan"
)

// plainRouteFacts is RouteFacts without its JSON methods: the layout every
// RouteFacts was cached in before T069.
type plainRouteFacts facts.RouteFacts

// TestSharedExpressionsKeepRouteFacts extracts every TypeScript and
// JavaScript file of the repository's fixtures with and without the
// expression memo and checks the route facts are equal, and that the
// cache layout of RouteFacts decodes to what the plain layout decodes to
// (T069: sharing changes memory, never a fact).
func TestSharedExpressionsKeepRouteFacts(t *testing.T) {
	var paths []string
	var jobs []parse.Job
	root := "../../../testdata"
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		switch language := scan.Language(path, source); language {
		case "typescript", "tsx", "javascript":
			if len(source) < 1<<20 {
				paths = append(paths, path)
				jobs = append(jobs, parse.Job{Language: language, Source: source})
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) < 500 {
		t.Fatalf("found %d fixture files, want the repository's fixtures", len(jobs))
	}
	results, err := parse.Batch(context.Background(), jobs, parse.Options{CacheDir: filepath.Join(t.TempDir(), "cache")})
	if err != nil {
		t.Fatal(err)
	}
	shared, compared := 0, 0
	for i, res := range results {
		if res.Err != nil || res.Root == nil {
			continue
		}
		file := filepath.ToSlash(paths[i])
		f := Extract(file, jobs[i].Source, res.Root)
		with, without := f, f
		with.Routes, with.Actions, without.Routes, without.Actions = nil, nil, nil, nil
		m := memoFor(res.Root)
		if m != nil {
			shared++
		}
		extractRoutesWith(m, file, jobs[i].Source, res.Root, &with)
		extractRoutesWith(nil, file, jobs[i].Source, res.Root, &without)
		if !reflect.DeepEqual(with.Routes, without.Routes) || !reflect.DeepEqual(with.Actions, without.Actions) {
			t.Errorf("%s: route facts differ with shared expressions", file)
			continue
		}
		if with.Routes == nil {
			continue
		}
		compared++
		plain, err := json.Marshal((*plainRouteFacts)(with.Routes))
		if err != nil {
			t.Fatal(err)
		}
		var want plainRouteFacts
		if err := json.Unmarshal(plain, &want); err != nil {
			t.Fatal(err)
		}
		layout, err := json.Marshal(with.Routes)
		if err != nil {
			t.Fatal(err)
		}
		if again, _ := json.Marshal(with.Routes); string(again) != string(layout) {
			t.Errorf("%s: cached route facts differ between two encodings", file)
		}
		var got facts.RouteFacts
		if err := json.Unmarshal(layout, &got); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		got.SetSource("", 0)
		if !reflect.DeepEqual(plainRouteFacts(got), want) {
			t.Errorf("%s: cached route facts decode differently from the plain layout", file)
		}
	}
	if shared < 100 || compared < 300 {
		t.Fatalf("compared %d route facts, %d with shared expressions", compared, shared)
	}
}

// TestNestedCallFactsAreLinear checks that the facts of nested calls grow
// with the source, not with the source times the nesting: in memory and
// in the cache (T069). Doubling the nesting may at most about double them.
func TestNestedCallFactsAreLinear(t *testing.T) {
	nested := func(depth int) string {
		var b strings.Builder
		b.WriteString("export const v = pipe(")
		for i := range depth {
			b.WriteString("Schema.Struct({ a: f" + string(rune('a'+i%26)) + "(x).y(")
		}
		b.WriteString("1")
		for range depth {
			b.WriteString(") }), ")
		}
		b.WriteString("0);\n")
		return b.String()
	}
	measure := func(depth int) (retained uint64, cached int) {
		source := []byte(nested(depth))
		results, err := parse.Batch(context.Background(), []parse.Job{{Language: "typescript", Source: source}}, parse.Options{CacheDir: filepath.Join(t.TempDir(), "cache")})
		if err != nil || results[0].Err != nil {
			t.Fatal(results, err)
		}
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		f := Extract("a.ts", source, results[0].Root)
		runtime.GC()
		runtime.ReadMemStats(&after)
		if f.Routes == nil || len(f.Routes.Clients) < 2*depth {
			t.Fatalf("depth %d: %d client calls", depth, len(f.Routes.Clients))
		}
		data, err := json.Marshal(f.Routes)
		if err != nil {
			t.Fatal(err)
		}
		runtime.KeepAlive(results)
		return after.HeapAlloc - before.HeapAlloc, len(data)
	}
	smallAlloc, smallCache := measure(150)
	largeAlloc, largeCache := measure(300)
	if r := float64(largeCache) / float64(smallCache); r > 2.3 {
		t.Errorf("cached route facts grew %.1fx for twice the nesting (%d -> %d bytes)", r, smallCache, largeCache)
	}
	if r := float64(largeAlloc) / float64(smallAlloc); r > 2.5 {
		t.Errorf("extracted facts hold %.1fx the heap for twice the nesting (%d -> %d bytes)", r, smallAlloc, largeAlloc)
	}
	t.Logf("cache %d -> %d bytes, heap %d -> %d bytes", smallCache, largeCache, smallAlloc, largeAlloc)
}
