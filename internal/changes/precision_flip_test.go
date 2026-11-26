package changes_test

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"github.com/graphvein/graphvein/internal/changes"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/server"
	"github.com/graphvein/graphvein/internal/store"
)

// TestPrecisionFlipCause is the change-impact gate of T020's S steps (ADR
// 0066, open points O7 and O14): comparing an index from before a file-row
// flip (PL-L1 Go; PL-L2 Python; PL-L3 TypeScript, TSX
// and JavaScript; PL-L4 Rust) with one after it lists
// each flipped file as a changed file item whose only field is
// `precision`, with cause extractor_or_relink, never code or a bare
// relink. The post-flip stream is this binary's index of the language's
// test repository; the pre-flip stream is the same stream under an older
// extractor version, with every file labelled best_effort (the label is
// the only difference the flip makes).
func TestPrecisionFlipCause(t *testing.T) {
	// testdata/repos/go: six Go files go/parser accepts (broken.go stays
	// best_effort). testdata/repos/python (row PL-L2): four Python
	// files CPython 3.14 accepts (pkg/broken.py stays best_effort).
	// testdata/repos/typescript (row PL-L3): ten of its eleven
	// TypeScript, TSX and JavaScript files as stored (the inert ignore names
	// are not restored here); src/broken.ts, which tree-sitter rejects, stays
	// best_effort. testdata/repos/rust (row PL-L4): the four Rust
	// files the crate root reaches (broken.rs, with a syntax error, and
	// blind.rs, which no `mod` item names, stay best_effort).
	for _, c := range []struct {
		repo, older string
		flipped     int
	}{
		{"go", "extractors-37", 6},
		{"python", "extractors-37", 4},
		{"typescript", "extractors-37", 10},
		{"rust", "extractors-39", 4},
	} {
		t.Run(c.repo, func(t *testing.T) { checkFlipCause(t, c.repo, c.older, c.flipped) })
	}
}

func checkFlipCause(t *testing.T, repo, older string, want int) {
	ctx := context.Background()
	backends(t, func(t *testing.T, b backend) {
		s, err := b.open(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		var after bytes.Buffer
		opt := index.Options{Repository: "flip", GeneratedAt: "2026-11-16T00:00:00Z", Parser: parse.Options{Workers: 1, CacheDir: filepath.Join(t.TempDir(), "cache")}}
		if _, err := index.Emit(ctx, filepath.Join("..", "..", "testdata", "repos", repo), s, &after, opt); err != nil {
			t.Fatal(err)
		}
		before := bytes.ReplaceAll(after.Bytes(), []byte(`"precision":"precise"`), []byte(`"precision":"best_effort"`))
		before = bytes.Replace(before, []byte(`"version":"`+index.Version+`"`), []byte(`"version":"`+older+`"`), 1)
		if bytes.Equal(before, after.Bytes()) {
			t.Fatalf("the index of testdata/repos/%s labels no file precise", repo)
		}
		for _, stream := range [][]byte{before, after.Bytes()} {
			if err := ingest.Stream(ctx, bytes.NewReader(stream), s); err != nil {
				t.Fatal(err)
			}
		}
		base, err := changes.ParseBase("previous")
		if err != nil {
			t.Fatal(err)
		}
		c, err := changes.Compare(ctx, s, "flip", base, changes.Options{ExtractorVersion: index.Version, Schema: server.ChangeSchema{}})
		if err != nil {
			t.Fatal(err)
		}
		flipped := 0
		for _, it := range c.AllItems() {
			if it.Record != store.RecordFile {
				t.Errorf("%s item %s %s: the flip changes file records only", it.Record, it.Kind, it.Identity)
				continue
			}
			flipped++
			if len(it.Fields) != 1 || it.Fields[0].Name != "precision" || it.Fields[0].Before != "best_effort" || it.Fields[0].After != "precise" {
				t.Errorf("%s: fields %+v, want precision best_effort -> precise only", it.Identity, it.Fields)
			}
			if str(it.Cause) != changes.CauseExtractor {
				t.Errorf("%s: cause %s, want %s", it.Identity, str(it.Cause), changes.CauseExtractor)
			}
		}
		if flipped != want {
			t.Errorf("%d flipped file items, want %d", flipped, want)
		}
	})
}
