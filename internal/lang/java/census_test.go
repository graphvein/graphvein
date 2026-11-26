package java

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

// javaCensus holds the per-row counts of the T021 task record (internal)
// ("Counts per contract row", Java) for the two Java corpora, by checkout
// name (scripts/cloud-corpus.sh), plus the number of scanned .java files.
// Rows not listed are not checked.
var javaCensus = map[string]struct {
	files int
	rows  map[string]censusCount
}{
	"spring-modulith": {682, map[string]censusCount{
		"JV-T1": {865, 499}, "JV-T2": {78, 71}, "JV-T3": {26, 22}, "JV-T4": {58, 48},
		"JV-T5": {30, 18}, "JV-T6": {463, 156}, "JV-F1": {2920, 449}, "JV-F2": {266, 170},
		"JV-F3": {36, 10}, "JV-F4": {0, 0}, "JV-I2": {933, -1}, "JV-I3": {32, -1},
		"JV-I4": {0, -1}, "JV-I5": {46, -1}, "JV-I6": {0, -1}, "JV-I7": {0, -1},
		"JV-I8": {3680, -1}, "BE-2": {0, 0}, "BE-8": {0, -1},
	}},
	// commons-lang has 646 raw .java files; the scanner skips
	// ClassUtilsOssFuzzTest.java (raw NUL bytes: not text), the one file
	// The investigation counted as BE-2.
	"commons-lang": {645, map[string]censusCount{
		"JV-T1": {1082, 548}, "JV-T2": {106, 82}, "JV-T3": {25, 17}, "JV-T4": {0, 0},
		"JV-T5": {18, 9}, "JV-T6": {583, 138}, "JV-F1": {10846, 581}, "JV-F2": {586, 237},
		"JV-F3": {56, 2}, "JV-F4": {0, 0}, "JV-I2": {647, -1}, "JV-I3": {142, -1},
		"JV-I4": {0, -1}, "JV-I5": {0, -1}, "JV-I6": {0, -1}, "JV-I7": {0, -1},
		"JV-I8": {3835, -1}, "BE-2": {0, 0}, "BE-8": {0, -1},
	}},
}

// TestJavaCensus extracts and links every .java file the scanner keeps in
// the two pinned Java corpora with the row tracer and checks the per-row
// counts against the inventory (T021-B5). It only parses; set
// GRAPHVEIN_BESTEFFORT_CENSUS_CORPUS to the directory holding the checkouts
// (scripts/cloud-corpus.sh spring-modulith commons-lang) to run it.
func TestJavaCensus(t *testing.T) {
	dir := os.Getenv("GRAPHVEIN_BESTEFFORT_CENSUS_CORPUS")
	if dir == "" {
		t.Skip("GRAPHVEIN_BESTEFFORT_CENSUS_CORPUS is not set")
	}
	for name, want := range javaCensus {
		t.Run(name, func(t *testing.T) {
			root := filepath.Join(dir, name)
			if _, err := os.Stat(root); err != nil {
				t.Fatalf("%v (run scripts/cloud-corpus.sh %s)", err, name)
			}
			sites, files, scanned := javaCensusRun(t, root)
			if scanned != want.files {
				t.Errorf("scanned .java files %d, want %d", scanned, want.files)
			}
			for row, c := range want.rows {
				if sites[row] != c.sites || (c.files >= 0 && len(files[row]) != c.files) {
					t.Errorf("%s: %d sites in %d files, want %d in %d", row, sites[row], len(files[row]), c.sites, c.files)
				}
			}
		})
	}
}

// javaCensusRun returns the sites per row, the files per extraction row and
// the number of scanned .java files. A file the parser flags is refused as
// the indexer refuses it (row BE-2) and counted once under BE-2.
func javaCensusRun(t *testing.T, root string) (map[string]int, map[string]map[string]bool, int) {
	t.Helper()
	scanned, err := scan.Walk(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	var jobs []parse.Job
	for _, f := range scanned.Files {
		if f.Record.Language == "java" {
			paths = append(paths, f.Record.Path)
			jobs = append(jobs, parse.Job{Language: "java", Source: f.Source})
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
