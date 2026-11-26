package dart

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

// dartCensus holds the per-row counts of the T021 task record (internal)
// ("Dart census with the extractor") for localsend, by checkout name
// (scripts/cloud-corpus.sh), plus the number of scanned .dart files. Rows
// not listed are not checked.
var dartCensus = map[string]struct {
	files int
	rows  map[string]censusCount
}{
	"localsend": {396, map[string]censusCount{
		"DA-T1": {5270, 308}, "DA-T2": {43, 34}, "DA-T3": {101, 70}, "DA-T4": {1, 1},
		"DA-T5": {30, 27}, "DA-F1": {205, 104}, "DA-F2": {4872, 279}, "DA-F3": {18390, 150},
		"DA-F4": {0, 0}, "DA-F5": {106, 41}, "DA-F6": {22, 11}, "DA-C1": {5181, 293},
		"DA-C2": {0, 0}, "DA-C3": {0, 0}, "DA-G1": {0, 0}, "DA-G2": {0, 0},
		"DA-I1": {129, -1}, "DA-I2": {230, -1}, "DA-I3": {878, -1}, "DA-I4": {180, -1},
		"DA-I5": {805, -1}, "DA-I6": {1, -1}, "DA-I7": {42, -1}, "BE-2": {0, 0},
	}},
}

// TestDartCensus extracts and links every .dart file the scanner keeps in
// the pinned Dart corpus with the row tracer and checks the per-row
// counts against the inventory (T021-B5). It only parses; set
// GRAPHVEIN_BESTEFFORT_CENSUS_CORPUS to the directory holding the checkouts
// (scripts/cloud-corpus.sh localsend) to run it.
func TestDartCensus(t *testing.T) {
	dir := os.Getenv("GRAPHVEIN_BESTEFFORT_CENSUS_CORPUS")
	if dir == "" {
		t.Skip("GRAPHVEIN_BESTEFFORT_CENSUS_CORPUS is not set")
	}
	for name, want := range dartCensus {
		t.Run(name, func(t *testing.T) {
			root := filepath.Join(dir, name)
			if _, err := os.Stat(root); err != nil {
				t.Fatalf("%v (run scripts/cloud-corpus.sh %s)", err, name)
			}
			sites, files, scanned := dartCensusRun(t, root)
			if scanned != want.files {
				t.Errorf("scanned .dart files %d, want %d", scanned, want.files)
			}
			for row, c := range want.rows {
				if sites[row] != c.sites || (c.files >= 0 && len(files[row]) != c.files) {
					t.Errorf("%s: %d sites in %d files, want %d in %d", row, sites[row], len(files[row]), c.sites, c.files)
				}
			}
		})
	}
}

// dartCensusRun returns the sites per row, the files per extraction row and
// the number of scanned .dart files. A file the parser flags is refused as
// the indexer refuses it (row BE-2; Dart has no BE-8 scope) and counted
// once under BE-2.
func dartCensusRun(t *testing.T, root string) (map[string]int, map[string]map[string]bool, int) {
	t.Helper()
	scanned, err := scan.Walk(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	var jobs []parse.Job
	sources := map[string][]byte{}
	for _, f := range scanned.Files {
		sources[f.Record.Path] = f.Source
		if f.Record.Language == "dart" {
			paths = append(paths, f.Record.Path)
			jobs = append(jobs, parse.Job{Language: "dart", Source: f.Source})
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
			f = besteffort.Refuse(f, besteffort.Scope{})
		}
		current = ""
		extracted[paths[i]] = f
	})
	if err != nil {
		t.Fatal(err)
	}
	link(extracted, PackageNames(sources), trace)
	return sites, files, len(paths)
}
