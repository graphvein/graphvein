package index_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/query"
	"github.com/graphvein/graphvein/internal/store"
)

// Opt-in measurements use a fresh child process/store for every run. Only the
// compilation cache survives between cold and warm; fact reuse cannot skew it.
func TestCorpusMeasurements(t *testing.T) {
	corpus := os.Getenv("GRAPHVEIN_BENCH_CORPUS")
	if corpus == "" {
		t.Skip("set GRAPHVEIN_BENCH_CORPUS to a pinned local React checkout")
	}
	corpus, err := filepath.Abs(corpus)
	if err != nil {
		t.Fatal(err)
	}
	for _, workers := range []int{1, 4} {
		cache := filepath.Join(t.TempDir(), "cache")
		for _, phase := range []string{"cold", "warm"} {
			cmd := exec.Command(os.Args[0], "-test.run=^TestCorpusWorker$", "-test.v")
			cmd.Env = append(os.Environ(), "GRAPHVEIN_BENCH_CHILD="+corpus, "GRAPHVEIN_BENCH_WORKERS="+strconv.Itoa(workers), "GRAPHVEIN_BENCH_CACHE="+cache)
			started := time.Now()
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("%s/%d: %v\n%s", phase, workers, err, output)
			}
			t.Logf("phase=%s workers=%d launch_to_exit=%s\n%s", phase, workers, time.Since(started), output)
		}
	}
}
func TestCorpusWorker(t *testing.T) {
	corpus := os.Getenv("GRAPHVEIN_BENCH_CHILD")
	if corpus == "" {
		t.Skip("measurement subprocess only")
	}
	workers, _ := strconv.Atoi(os.Getenv("GRAPHVEIN_BENCH_WORKERS"))
	parserOptions := parse.Options{Workers: workers, CacheDir: os.Getenv("GRAPHVEIN_BENCH_CACHE")}
	startupAt := time.Now()
	first, err := parse.Batch(context.Background(), []parse.Job{{Language: "typescript", Source: []byte("const x=1;")}, {Language: "tsx", Source: []byte("const X=()=> <div/>;")}, {Language: "javascript", Source: []byte("const x=1;")}, {Language: "go", Source: []byte("package main\nfunc main() {}")}, {Language: "python", Source: []byte("def f():\n    pass\n")}, {Language: "rust", Source: []byte("fn main() {}")}}, parserOptions)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range first {
		if r.Err != nil {
			t.Fatal(r.Err)
		}
	}
	startup := time.Since(startupAt)
	s := open(t)
	var stream bytes.Buffer
	summary, err := index.Emit(context.Background(), corpus, s, &stream, index.Options{Repository: "realworld-react", Parser: parserOptions})
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	writes, err := ingest.StreamWithWrites(context.Background(), bytes.NewReader(stream.Bytes()), s)
	if err != nil {
		t.Fatal(err)
	}
	ingestTime := time.Since(started)
	snapshot, err := s.Dump(context.Background(), "realworld-react")
	if err != nil {
		t.Fatal(err)
	}
	codes := map[string]int{}
	unresolved := 0
	for _, d := range snapshot.Diagnostics {
		codes[d.Code]++
		if d.Code == "parse_failed" || d.Code == "extract_failed" || d.Code == "invalid_record" || d.Code == "invalid_reference" {
			t.Fatal("corpus defect", d)
		}
		if d.Code == "unresolved_calls" {
			var count int
			fmt.Sscanf(d.Message, "%d", &count)
			unresolved += count
		}
	}
	peak, commit, method := peakMemory()
	external, unknown := 0, 0
	for _, n := range snapshot.Nodes {
		if n.Kind == "file" {
			e, _ := strconv.Atoi(fmt.Sprint(n.Attributes["unresolved_calls_external"]))
			u, _ := strconv.Atoi(fmt.Sprint(n.Attributes["unresolved_calls_unknown"]))
			external += e
			unknown += u
		}
	}
	impactTime := ""
	impactResults := 0
	missing := 0
	if target := os.Getenv("GRAPHVEIN_BENCH_IMPACT"); target != "" {
		start := time.Now()
		out, err := query.Execute(context.Background(), s, query.Options{Command: "impact", Term: target, Root: corpus, Repository: "realworld-react", Depth: 3})
		if err != nil || out.Error != "" {
			t.Fatal("impact measurement", out.Error, err)
		}
		impactTime = time.Since(start).String()
		impactResults = len(out.Results)
		missing = out.PossiblyMissingCallers
	}
	data, _ := json.Marshal(struct {
		Startup                               string
		Summary                               index.Summary
		Writes                                store.Writes
		Ingest                                string
		PeakBytes, PeakCommit                 uint64
		Method                                string
		DiagnosticCodes                       map[string]int
		UnresolvedCalls                       int
		ExternalCalls, UnknownCalls           int
		Impact                                string
		ImpactResults, PossiblyMissingCallers int
	}{startup.String(), summary, writes, ingestTime.String(), peak, commit, method, codes, unresolved, external, unknown, impactTime, impactResults, missing})
	fmt.Println("MEASUREMENT", string(data))
}
