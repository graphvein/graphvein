package index

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/graphvein/graphvein/internal/lang/typescript"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

func pipelineFiles(t *testing.T, count int) string {
	t.Helper()
	root := t.TempDir()
	for i := 0; i < count; i++ {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("%03d.ts", i)), []byte(fmt.Sprintf("export function f%d() { return %d; }\n", i, i)), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestPipelineMixedFailures(t *testing.T) {
	root := pipelineFiles(t, 15)
	if err := os.WriteFile(filepath.Join(root, "003.ts"), []byte("function broken( {"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, workers := range []int{1, 4} {
		t.Run(fmt.Sprint(workers), func(t *testing.T) {
			s, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "index.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			opt := Options{Repository: "mixed", GeneratedAt: "2026-10-08T00:00:00Z", Parser: parse.Options{Workers: workers, CacheDir: filepath.Join(t.TempDir(), "cache")}}
			seen := map[string]bool{}
			extract := func(file string, source []byte, tree *parse.Node, language string) (typescript.Facts, error) {
				seen[file] = true
				if file == "007.ts" {
					panic("injected extraction panic")
				}
				if file == "008.ts" {
					return typescript.Facts{}, fmt.Errorf("injected extraction failure")
				}
				return extractFile(file, source, tree, language)
			}
			pipeline := func(ctx context.Context, jobs []parse.Job, options parse.Options, consume func(int, parse.Result)) error {
				return parse.Consume(ctx, jobs, options, func(i int, result parse.Result) {
					// Inject the existing parser deadline result without a timing race.
					// Real WASM interruption/worker retirement is covered in parse tests.
					if i == 5 {
						result.Err = context.DeadlineExceeded
					}
					consume(i, result)
				})
			}
			var stream bytes.Buffer
			summary, err := emitWithPipeline(context.Background(), root, s, &stream, opt, extract, pipeline)
			if err != nil {
				t.Fatal(err)
			}
			if summary.Parsed != 15 || len(seen) != 14 || seen["005.ts"] {
				t.Fatal(summary, seen)
			}
			for _, want := range []string{`"code":"syntax_error"`, `"code":"parse_failed"`, `parsing 005.ts: context deadline exceeded`, `extracting 007.ts: injected extraction panic`, `injected extraction failure`, `function:014.ts#f14`} {
				if !strings.Contains(stream.String(), want) {
					t.Fatalf("missing %s", want)
				}
			}
			if strings.Count(stream.String(), `"code":"extract_failed"`) != 2 {
				t.Fatal("wrong extraction diagnostic count")
			}
		})
	}
}

func TestPipelineCancellationJoinsWorkers(t *testing.T) {
	root := pipelineFiles(t, 120)
	s, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// Prime compilation before measuring goroutine lifetime.
	opt := Options{Repository: "cancel", Parser: parse.Options{Workers: 4, CacheDir: filepath.Join(t.TempDir(), "cache")}}
	if err := parse.Consume(context.Background(), []parse.Job{{Language: "typescript", Source: []byte("function f() {}")}}, opt.Parser, func(int, parse.Result) {}); err != nil {
		t.Fatal(err)
	}
	before := runtime.NumGoroutine()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	extract := func(file string, source []byte, tree *parse.Node, language string) (typescript.Facts, error) {
		calls++
		if calls == 5 {
			cancel()
		}
		return extractFile(file, source, tree, language)
	}
	_, err = emit(ctx, root, s, io.Discard, opt, extract)
	if !errors.Is(err, context.Canceled) || calls != 5 {
		t.Fatal(err, calls)
	}
	deadline := time.Now().Add(3 * time.Second)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if got := runtime.NumGoroutine(); got > before {
		t.Fatalf("goroutines before=%d after=%d", before, got)
	}
}
