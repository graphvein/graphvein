//go:build graphveinprofile

package index

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/graphvein/graphvein/internal/lang/typescript"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

func TestPipelineDetachedTreeBound(t *testing.T) {
	root := pipelineFiles(t, 160)
	for _, workers := range []int{1, 4} {
		t.Run(fmt.Sprint(workers), func(t *testing.T) {
			s, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "index.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			var mu sync.Mutex
			live, peak := 0, 0
			parse.ProfileDetachedTree = func(delta int) {
				mu.Lock()
				defer mu.Unlock()
				live += delta
				peak = max(peak, live)
			}
			defer func() { parse.ProfileDetachedTree = nil }()
			extract := func(file string, source []byte, tree *parse.Node, language string) (typescript.Facts, error) {
				// Backpressure must fill the queue rather than grow retained trees.
				time.Sleep(time.Millisecond)
				return extractFile(file, source, tree, language)
			}
			opt := Options{Repository: "bound", Parser: parse.Options{Workers: workers, CacheDir: filepath.Join(t.TempDir(), "cache")}}
			got, err := emit(context.Background(), root, s, io.Discard, opt, extract)
			if err != nil {
				t.Fatal(err)
			}
			if live != 0 || peak > 2*workers+1 || peak < workers+1 || got.Parsed != 160 {
				t.Fatalf("live=%d peak=%d bound=%d parsed=%d", live, peak, 2*workers+1, got.Parsed)
			}
			t.Logf("live detached tree maximum %d; bound %d", peak, 2*workers+1)
		})
	}
}
