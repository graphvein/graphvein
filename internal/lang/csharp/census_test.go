package csharp

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/graphvein/graphvein/internal/lang/besteffort"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/scan"
)

// censusCount is one inventory cell: sites, and the number of files holding
// them (-1 where the inventory gives no file count, or for link rows, whose
// tracer does not know the file).
type censusCount struct{ sites, files int }

// csharpCensus holds the per-row counts of the T021 task record (internal)
// ("C# census with the extractor", with the T021-B5 BE-8 rule for
// block-namespace files, and the T021-F1 row CS-I4) for eShop, by checkout
// name (scripts/cloud-corpus.sh), plus the number of scanned .cs files. Rows
// not listed are not checked. BE-8 4: the alias usings the refused gRPC file
// Basket.cs (a block namespace) may declare. Of the other 495, 95 are CS-I4
// (names under a repository namespace of two or more segments, such as the
// Razor folder namespace eShop.WebApp.Components and the Microsoft.Extensions
// and Microsoft.AspNetCore names next to the repository's own extension
// namespaces) and 400 CS-I2.
var csharpCensus = map[string]struct {
	files int
	rows  map[string]censusCount
}{
	"eshop": {547, map[string]censusCount{
		"CS-T1": {433, 397}, "CS-T2": {0, 0}, "CS-T3": {43, 42}, "CS-T4": {81, 70},
		"CS-T5": {10, 9}, "CS-F1": {925, 325}, "CS-F2": {208, 169}, "CS-F3": {1, 1},
		"CS-F4": {6, 4}, "CS-F5": {2, 1}, "CS-P1": {749, 184}, "CS-P2": {0, 0},
		"CS-G1": {0, 0}, "CS-I1": {402, -1}, "CS-I3": {10, -1}, "CS-I2": {400, -1},
		"CS-I4": {95, -1}, "BE-2": {2, 2}, "BE-8": {4, -1},
	}},
}

// TestCSharpCensus extracts and links every .cs file the scanner keeps in
// the pinned C# corpus with the row tracer and checks the per-row
// counts against the inventory (T021-B5). It only parses; set
// GRAPHVEIN_BESTEFFORT_CENSUS_CORPUS to the directory holding the checkouts
// (scripts/cloud-corpus.sh eshop) to run it.
func TestCSharpCensus(t *testing.T) {
	dir := os.Getenv("GRAPHVEIN_BESTEFFORT_CENSUS_CORPUS")
	if dir == "" {
		t.Skip("GRAPHVEIN_BESTEFFORT_CENSUS_CORPUS is not set")
	}
	for name, want := range csharpCensus {
		t.Run(name, func(t *testing.T) {
			root := filepath.Join(dir, name)
			if _, err := os.Stat(root); err != nil {
				t.Fatalf("%v (run scripts/cloud-corpus.sh %s)", err, name)
			}
			sites, files, scanned := csharpCensusRun(t, root)
			if scanned != want.files {
				t.Errorf("scanned .cs files %d, want %d", scanned, want.files)
			}
			for row, c := range want.rows {
				if sites[row] != c.sites || (c.files >= 0 && len(files[row]) != c.files) {
					t.Errorf("%s: %d sites in %d files, want %d in %d", row, sites[row], len(files[row]), c.sites, c.files)
				}
			}
		})
	}
}

// csharpCensusRun returns the sites per row, the files per extraction row and
// the number of scanned .cs files. A file the parser flags is refused as
// the indexer refuses it (row BE-2) and counted once under BE-2.
func csharpCensusRun(t *testing.T, root string) (map[string]int, map[string]map[string]bool, int) {
	t.Helper()
	scanned, err := scan.Walk(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	var jobs []parse.Job
	for _, f := range scanned.Files {
		if f.Record.Language == "c_sharp" {
			paths = append(paths, f.Record.Path)
			jobs = append(jobs, parse.Job{Language: "c_sharp", Source: f.Source})
		}
	}
	sites := map[string]int{}
	files := map[string]map[string]bool{}
	current := ""
	trace := tracer(func(row string) {
		sites[row]++
		if current != "" {
			if files[row] == nil {
				files[row] = map[string]bool{}
			}
			files[row][current] = true
		}
	})
	extracted := map[string]facts.Facts{}
	// Consume calls back one result at a time, so the tracer's file is
	// never shared between two extractions.
	err = parse.Consume(context.Background(), jobs, parse.Options{Workers: 2, CacheDir: filepath.Join(t.TempDir(), "cache")}, func(i int, result parse.Result) {
		if result.Err != nil {
			t.Errorf("%s: %v", paths[i], result.Err)
			return
		}
		current = paths[i]
		f := extract(paths[i], jobs[i].Source, result.Root, trace)
		if result.SyntaxError && (f.BestEffort == nil || !f.BestEffort.Refused) {
			trace("BE-2")
			f = besteffort.Refuse(f, RefusedScope(jobs[i].Source))
		}
		current = ""
		extracted[paths[i]] = f
	})
	if err != nil {
		t.Fatal(err)
	}
	link(extracted, trace)
	return sites, files, len(paths)
}
