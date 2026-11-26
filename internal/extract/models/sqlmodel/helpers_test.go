package sqlmodel

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/lang/python"
	"github.com/graphvein/graphvein/internal/parse"
)

// linkSources parses Python sources, captures their facts and links them, as
// the index does at link time.
func linkSources(t *testing.T, sources map[string]string) ([]contract.Node, []contract.Edge, []contract.Diagnostic) {
	t.Helper()
	files := map[string]facts.Facts{}
	raw := map[string][]byte{}
	var paths []string
	for file, source := range sources {
		raw[file] = []byte(source)
		if strings.HasSuffix(file, ".py") {
			paths = append(paths, file)
		}
	}
	sort.Strings(paths)
	jobs := make([]parse.Job, len(paths))
	for i, file := range paths {
		jobs[i] = parse.Job{Language: "python", Source: raw[file]}
	}
	err := parse.Consume(context.Background(), jobs, parse.Options{Workers: 1, CacheDir: filepath.Join(t.TempDir(), "cache")}, func(i int, r parse.Result) {
		if r.Err != nil || r.Root == nil {
			t.Fatalf("%s: %v", paths[i], r.Err)
		}
		files[paths[i]] = python.Extract(paths[i], raw[paths[i]], r.Root)
	})
	if err != nil {
		t.Fatal(err)
	}
	return Link(Input{Python: files, Resolver: python.NewModuleResolver(files, raw), Sources: raw})
}
