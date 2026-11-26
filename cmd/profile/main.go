//go:build graphveinprofile

// The opt-in profiler is absent from ordinary builds and test package discovery.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/metrics"
	"runtime/pprof"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

type memory struct {
	Heap, Total, WorkingSet, Private uint64
	HighestPhase                     string
}
type sample struct {
	Target, Run                                                     string
	Workers                                                         int
	Profiled                                                        bool
	Instrumented                                                    bool
	Details                                                         *detailStats `json:",omitempty"`
	Summary                                                         index.Summary
	IngestMS                                                        float64
	Writes                                                          store.Writes
	Memory                                                          memory
	CompilationCache                                                string
	DiagnosticCodes                                                 map[string]int
	InvalidRecords                                                  []string
	DeadlineDiagnostics, AgentsFiles, CursorFiles, FactCacheEntries int
}
type peakSampler struct {
	mu             sync.Mutex
	peak           memory
	phase, profile string
	err            error
}

func (s *peakSampler) read() {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := []metrics.Sample{{Name: "/memory/classes/heap/objects:bytes"}, {Name: "/memory/classes/total:bytes"}}
	metrics.Read(v)
	h, total := v[0].Value.Uint64(), v[1].Value.Uint64()
	if total > s.peak.Total {
		s.peak.Total = total
	}
	if h > s.peak.Heap {
		s.peak.Heap = h
		s.peak.HighestPhase = s.phase
		if s.profile != "" {
			f, err := os.Create(s.profile)
			if err == nil {
				err = pprof.WriteHeapProfile(f)
				closeErr := f.Close()
				if err == nil {
					err = closeErr
				}
			}
			if err != nil {
				s.err = err
			}
		}
	}
	w, p, err := processMemory()
	if err != nil {
		s.err = err
	}
	if w > s.peak.WorkingSet {
		s.peak.WorkingSet = w
	}
	if p > s.peak.Private {
		s.peak.Private = p
	}
}
func (s *peakSampler) boundary(p string) { s.mu.Lock(); s.phase = p; s.mu.Unlock(); s.read() }
func child(args []string) error {
	if len(args) != 7 {
		return fmt.Errorf("child requires target, root, db, run, workers, snapshot, profile")
	}
	target, root, db, run := args[0], args[1], args[2], args[3]
	workers, err := strconv.Atoi(args[4])
	if err != nil {
		return err
	}
	var details *detailStats
	if strings.HasPrefix(run, "detail-") {
		details = enableDetails()
	}
	ctx := context.Background()
	s, err := sqlite.Open(ctx, db)
	if err != nil {
		return err
	}
	defer s.Close()
	f, err := os.Create(args[5])
	if err != nil {
		return err
	}
	defer f.Close()
	probe := &peakSampler{phase: "index", profile: args[6]}
	index.ProfileBoundary = probe.boundary
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		tick := time.NewTicker(50 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-tick.C:
				probe.read()
			case <-stop:
				return
			}
		}
	}()
	probe.boundary("index")
	summary, indexErr := index.Emit(ctx, root, s, f, index.Options{Repository: target, Parser: parse.Options{Workers: workers, Timeout: 5 * time.Second}, MaxFileSize: 2 << 20})
	probe.boundary("index-end")
	if indexErr != nil {
		close(stop)
		<-done
		return indexErr
	}
	if _, err := f.Seek(0, 0); err != nil {
		close(stop)
		<-done
		return err
	}
	probe.boundary("ingest")
	started := time.Now()
	writes, ingestErr := ingest.StreamWithWrites(ctx, f, s)
	elapsed := time.Since(started)
	probe.boundary("ingest-end")
	close(stop)
	<-done
	if ingestErr != nil {
		return ingestErr
	}
	if probe.err != nil {
		return probe.err
	}
	result := sample{Target: target, Run: run, Workers: workers, Profiled: args[6] != "", Summary: summary, IngestMS: float64(elapsed) / float64(time.Millisecond), Writes: writes, Memory: probe.peak, DiagnosticCodes: map[string]int{}, InvalidRecords: []string{}, CompilationCache: "see child stderr; no compilation on fully replayed run"}
	result.Details, result.Instrumented = details, details != nil
	// Post-measurement inventory uses the existing store boundary only.
	snapshot, err := s.Dump(ctx, target)
	if err != nil {
		return err
	}
	for _, d := range snapshot.Diagnostics {
		result.DiagnosticCodes[d.Code]++
		if d.Code == "extract_invalid_record" {
			result.InvalidRecords = append(result.InvalidRecords, d.File+": "+d.Message)
		}
		if strings.Contains(d.Message, "deadline") {
			result.DeadlineDiagnostics++
		}
	}
	for _, file := range snapshot.Files {
		if strings.HasPrefix(file.Path, ".agents/") {
			result.AgentsFiles++
		}
		if strings.HasPrefix(file.Path, ".cursor/") {
			result.CursorFiles++
		}
		data, err := s.FactCache(ctx, target, file.Path, file.ContentHash, fmt.Sprintf("%s/max=%d", index.Version, 2<<20))
		if err != nil {
			return err
		}
		if len(data) > 0 {
			result.FactCacheEntries++
		}
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}
func command(binary string, args []string, db string) ([]byte, []byte, error) {
	c := exec.Command(binary, args...)
	for _, e := range os.Environ() {
		key := strings.ToUpper(strings.SplitN(e, "=", 2)[0])
		if key != "GRAPHVEIN_SQLITE_PATH" && key != "GRAPHVEIN_POSTGRES_URL" {
			c.Env = append(c.Env, e)
		}
	}
	if db != "" {
		c.Env = append(c.Env, "GRAPHVEIN_SQLITE_PATH="+db)
	}
	var out, stderr bytes.Buffer
	c.Stdout = &out
	c.Stderr = &stderr
	err := c.Run()
	return out.Bytes(), stderr.Bytes(), err
}
func save(path string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0644)
}
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if entry.IsDir() && (entry.Name() == ".git" || entry.Name() == ".graphvein") {
			return filepath.SkipDir
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("copy refuses symlink %s", rel)
		}
		dest := filepath.Join(dst, rel)
		if entry.IsDir() {
			return os.MkdirAll(dest, 0755)
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.Create(dest)
		if err != nil {
			return err
		}
		_, err = io.Copy(out, in)
		closeErr := out.Close()
		if err == nil {
			err = closeErr
		}
		return err
	})
}
func normalize(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	split := bytes.IndexByte(b, '\n')
	if split < 0 {
		return nil, fmt.Errorf("missing header")
	}
	var h map[string]any
	if err = json.Unmarshal(b[:split], &h); err != nil {
		return nil, err
	}
	stamp, ok := h["generated_at"].(string)
	if !ok {
		return nil, fmt.Errorf("missing generated_at string")
	}
	encoded, err := json.Marshal(stamp)
	if err != nil {
		return nil, err
	}
	token := append([]byte(`"generated_at":`), encoded...)
	if bytes.Count(b[:split], token) != 1 {
		return nil, fmt.Errorf("expected one compact generated_at field")
	}
	head := bytes.Replace(b[:split], token, []byte(`"generated_at":""`), 1)
	return append(head, b[split:]...), nil
}

type queryTiming struct {
	Category, Term, Command                            string
	Frequency, Examined, Matched, Returned, Candidates int
	Decoded, FilterReturned                            int
	Milliseconds                                       []float64
	MedianMS, MinMS, MaxMS                             float64
	Error                                              string
}
type report struct {
	Environment     map[string]any
	Samples         []sample
	Equivalence     map[string]any
	Queries         []queryTiming
	External        map[string]any
	NotMeasured     map[string]string
	Medians         map[string]map[string][]float64
	DetailedMedians map[string]map[string][]float64
}

func run(args []string) error {
	if len(args) != 4 {
		return fmt.Errorf("driver requires output directory, CLI binary, native replay binary, sample count")
	}
	out, cli, replay := args[0], args[1], args[2]
	n, err := strconv.Atoi(args[3])
	if err != nil || n < 1 {
		return fmt.Errorf("sample count must be positive")
	}
	abs, err := filepath.Abs(out)
	if err != nil {
		return err
	}
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	if strings.EqualFold(root, abs) || strings.HasPrefix(strings.ToLower(abs), strings.ToLower(root)+string(filepath.Separator)) {
		return fmt.Errorf("profile output must be outside repository")
	}
	if err = os.MkdirAll(abs, 0755); err != nil {
		return err
	}
	work, err := os.MkdirTemp("", "graphvein-profile-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	r := report{Environment: map[string]any{"os": runtime.GOOS, "arch": runtime.GOARCH, "go": runtime.Version(), "logical_cpus": runtime.NumCPU(), "samples_per_configuration": n, "GOGC": "default (unset)", "GOMEMLIMIT": "default (unset)", "compilation_cache": "inferred individually from stderr; default user cache, no override", "time": time.Now().UTC().Format(time.RFC3339Nano), "default_workers": min(4, runtime.NumCPU()), "memory_method": "Windows GetProcessMemoryInfo PeakWorkingSetSize and PeakPagefileUsage; runtime/metrics every 50ms and index/ingest boundaries"}, Equivalence: map[string]any{}, External: map[string]any{}, NotMeasured: map[string]string{"Q2 attribution": "working set and Go total high-water marks occur at different instants; their difference is a descriptive residual, not a reliable allocation attribution. Wazero compiled mappings and guest memory cannot be separated by this sampler.", "Q4 shared output validation": "Local replay cache-entry validation measured in separate instrumented runs. Common synthesized-output and wire-writer validation is not attributed to cached entries.", "Q5 map buckets": "Distinct referenced string byte lower bounds measured, excluding map bucket capacity, allocator overhead and unreachable garbage awaiting GC."}, Medians: map[string]map[string][]float64{}}
	commit, _, err := command("git", []string{"rev-parse", "HEAD"}, "")
	if err != nil {
		return err
	}
	r.Environment["graphvein_commit"] = strings.TrimSpace(string(commit))
	corpusCommits := map[string]string{}
	for _, name := range []string{"langflow", "react", "express", "fastapi", "axum", "axum-ddd", "angular"} {
		b, _, err := command("git", []string{"-C", filepath.Join("spike", "parsing", ".corpus", name), "rev-parse", "HEAD"}, "")
		if err != nil {
			return err
		}
		corpusCommits[name] = strings.TrimSpace(string(b))
	}
	r.Environment["corpus_commits"] = corpusCommits
	r.Environment["host"] = hostEnvironment()
	launch := func(target, path, db, phase string, workers int, profile bool) (sample, string, error) {
		id := fmt.Sprintf("%s-w%d-%s-%d", target, workers, phase, len(r.Samples))
		snap := filepath.Join(work, id+".ndjson")
		heap := ""
		if profile {
			heap = filepath.Join(abs, id+".heap")
		}
		b, stderr, err := command(os.Args[0], []string{"child", target, path, db, phase, strconv.Itoa(workers), snap, heap}, "")
		if err != nil {
			return sample{}, "", fmt.Errorf("%s: %w: %s", id, err, stderr)
		}
		var s sample
		if err = json.Unmarshal(b, &s); err != nil {
			return s, "", err
		}
		if strings.Contains(string(stderr), "using memory") {
			s.CompilationCache = "memory fallback (cache unavailable warning)"
		} else if strings.Contains(string(stderr), "rebuilding WASM") {
			s.CompilationCache = "cache rebuild warning; disk versus memory not established"
		} else if s.Summary.Parsed > 0 {
			s.CompilationCache = "default disk cache; no fallback warning"
		}
		r.Samples = append(r.Samples, s)
		if err = os.WriteFile(filepath.Join(abs, id+".stderr.txt"), stderr, 0644); err != nil {
			return s, "", err
		}
		if err = save(filepath.Join(abs, id+".json"), s); err != nil {
			return s, "", err
		}
		fmt.Fprintf(os.Stderr, "%s: files=%d parsed=%d replayed=%d index=%.1fms ingest=%.1fms peak=%.1fMiB\n", id, s.Summary.Files, s.Summary.Parsed, s.Summary.Reused, float64(s.Summary.Total)/1e6, s.IngestMS, float64(s.Memory.WorkingSet)/(1<<20))
		if profile {
			b, stderr, err = command("go", []string{"tool", "pprof", "-top", "-inuse_space", heap}, "")
			if err != nil {
				return s, "", fmt.Errorf("pprof: %w %s", err, stderr)
			}
			if err = os.WriteFile(filepath.Join(abs, id+".pprof.txt"), b, 0644); err != nil {
				return s, "", err
			}
		}
		return s, snap, nil
	}
	targets := []struct{ name, path string }{{"react", "spike/parsing/.corpus/react"}, {"express", "spike/parsing/.corpus/express"}, {"fastapi", "spike/parsing/.corpus/fastapi"}, {"self", "."}, {"langflow-base", "spike/parsing/.corpus/langflow/src/backend/base"}, {"langflow-frontend", "spike/parsing/.corpus/langflow/src/frontend"}, {"langflow-src", "spike/parsing/.corpus/langflow/src"}, {"langflow", "spike/parsing/.corpus/langflow"}}
	for _, t := range targets {
		for i := 0; i < n; i++ {
			db := filepath.Join(work, fmt.Sprintf("scale-%s-%d.db", t.name, i))
			if _, _, err = launch(t.name, t.path, db, "cold", 0, false); err != nil {
				return err
			}
		}
	}
	// Equivalence is checked on a temporary copy so CLI scratch never touches a corpus.
	copyRoot := filepath.Join(work, "react-copy")
	if err = copyTree(targets[0].path, copyRoot); err != nil {
		return err
	}
	// Heterogeneous evidence mirrors the existing JSON report without changing CLI output.
	pairs := []map[string]any{}
	for i := 0; i < 3; i++ {
		h, snap, err := launch("equivalence", copyRoot, filepath.Join(work, fmt.Sprintf("equivalence-%d.db", i)), "cold", 0, false)
		if err != nil {
			return err
		}
		cliSnap := filepath.Join(work, fmt.Sprintf("cli-%d.ndjson", i))
		b, stderr, err := command(cli, []string{"index", copyRoot, "--repository", "equivalence", "--emit", cliSnap}, filepath.Join(work, fmt.Sprintf("cli-%d.db", i)))
		if err != nil {
			return fmt.Errorf("equivalence CLI: %w %s", err, stderr)
		}
		a, err := normalize(snap)
		if err != nil {
			return err
		}
		c, err := normalize(cliSnap)
		if err != nil {
			return err
		}
		if !bytes.Equal(a, c) {
			return fmt.Errorf("equivalence snapshots differ")
		}
		pairs = append(pairs, map[string]any{"harness": h, "cli_totals": string(b), "equal_except_generated_at": true})
	}
	r.Equivalence["pairs"] = pairs
	r.Equivalence["snapshot_equal_except_generated_at"] = true
	r.Equivalence["target"] = "temporary React copy"
	langRoot := targets[len(targets)-1].path
	var queryDB string
	for _, workers := range []int{1, 4} {
		for i := 0; i < n; i++ {
			copyRoot := filepath.Join(work, fmt.Sprintf("edit-w%d-%d", workers, i))
			if err = copyTree(langRoot, copyRoot); err != nil {
				return err
			}
			db := filepath.Join(work, fmt.Sprintf("incremental-w%d-%d.db", workers, i))
			if _, _, err = launch("langflow", copyRoot, db, "cold", workers, false); err != nil {
				return err
			}
			if _, _, err = launch("langflow", copyRoot, db, "warm", workers, false); err != nil {
				return err
			}
			if queryDB == "" {
				queryDB = filepath.Join(work, "query.db")
				in, err := os.Open(db)
				if err != nil {
					return err
				}
				dest, err := os.Create(queryDB)
				if err != nil {
					in.Close()
					return err
				}
				_, err = io.Copy(dest, in)
				in.Close()
				closeErr := dest.Close()
				if err != nil {
					return err
				}
				if closeErr != nil {
					return closeErr
				}
			}
			edit := filepath.Join(copyRoot, "src/backend/base/langflow/api/v1/endpoints.py")
			contents, err := os.ReadFile(edit)
			if err != nil {
				return err
			}
			suffix := "# graphvein-profile edit\n"
			if len(contents) > 0 && contents[len(contents)-1] != '\n' {
				suffix = "\n" + suffix
			}
			f, err := os.OpenFile(edit, os.O_APPEND|os.O_WRONLY, 0)
			if err != nil {
				return err
			}
			_, err = f.WriteString(suffix)
			closeErr := f.Close()
			if err != nil {
				return err
			}
			if closeErr != nil {
				return closeErr
			}
			if _, _, err = launch("langflow", copyRoot, db, "edit", workers, false); err != nil {
				return err
			}
		}
	}
	for _, workers := range []int{1, 4} {
		if _, _, err = launch("langflow", langRoot, filepath.Join(work, fmt.Sprintf("profile-%d.db", workers)), "cold", workers, true); err != nil {
			return err
		}
	}
	for i := 0; i < n; i++ {
		db := filepath.Join(work, fmt.Sprintf("detail-%d.db", i))
		for _, phase := range []string{"detail-cold", "detail-warm", "detail-warm-w1"} {
			workers := 4
			if phase == "detail-warm-w1" {
				workers = 1
			}
			if _, _, err = launch("langflow", langRoot, db, phase, workers, false); err != nil {
				return err
			}
		}
	}
	// Built-in -> native external replay -> built-in, all on the same store.
	db := filepath.Join(work, "external.db")
	first, snap, err := launch("langflow-base", targets[4].path, db, "before-external", 0, false)
	if err != nil {
		return err
	}
	os.Setenv("GV_REPLAY_STREAM", snap)
	b, stderr, err := command(cli, []string{"index", targets[4].path, "--repository", "langflow-base", "--indexer", replay}, db)
	os.Unsetenv("GV_REPLAY_STREAM")
	if err != nil {
		return fmt.Errorf("external replay: %w %s", err, stderr)
	}
	// Count matching entries through FactCache before the final built-in index.
	s, err := sqlite.Open(context.Background(), db)
	if err != nil {
		return err
	}
	snapshot, err := s.Dump(context.Background(), "langflow-base")
	if err != nil {
		s.Close()
		return err
	}
	count := 0
	for _, f := range snapshot.Files {
		data, err := s.FactCache(context.Background(), "langflow-base", f.Path, f.ContentHash, fmt.Sprintf("%s/max=%d", index.Version, 2<<20))
		if err != nil {
			s.Close()
			return err
		}
		if len(data) > 0 {
			count++
		}
	}
	s.Close()
	last, _, err := launch("langflow-base", targets[4].path, db, "after-external", 0, false)
	if err != nil {
		return err
	}
	r.External = map[string]any{"before": first.FactCacheEntries, "after_external": count, "after_builtin": last.FactCacheEntries, "reparsed_after": last.Summary.Parsed, "cli_output": string(b), "scope": "unchanged inventory; deleted-file retention not exercised"}
	// Deterministic names: sort distinct L4 simple names by (frequency, name).
	s, err = sqlite.OpenRead(context.Background(), queryDB)
	if err != nil {
		return err
	}
	nodes, err := s.Nodes(context.Background(), "langflow", store.Query{})
	s.Close()
	if err != nil {
		return err
	}
	freq := map[string]int{}
	for _, node := range nodes {
		if node.Level == 4 {
			freq[node.Name]++
		}
	}
	names := []string{}
	for name := range freq {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		if freq[names[i]] != freq[names[j]] {
			return freq[names[i]] < freq[names[j]]
		}
		return names[i] < names[j]
	})
	if len(names) == 0 {
		return fmt.Errorf("no L4 query names")
	}
	most := names[len(names)-1]
	for _, name := range names {
		if freq[name] == freq[most] {
			most = name
			break
		}
	}
	chosen := []struct{ category, name string }{{"most-frequent", most}, {"median-frequency", names[(len(names)-1)/2]}, {"unique", names[0]}}
	for _, term := range chosen {
		for _, cmd := range []string{"query", "callers", "impact"} {
			q := queryTiming{Category: term.category, Term: term.name, Command: cmd, Frequency: freq[term.name], Examined: len(nodes)}
			for i := 0; i < 5; i++ {
				started := time.Now()
				b, stderr, runErr := command(cli, []string{cmd, term.name, langRoot, "--repository", "langflow", "--json"}, queryDB)
				ms := float64(time.Since(started)) / 1e6
				var response struct {
					Nodes, Results, Candidates []json.RawMessage
					Error                      string
				}
				if err = json.Unmarshal(b, &response); err != nil {
					return fmt.Errorf("query JSON: %w %s", err, stderr)
				}
				if runErr != nil && response.Error == "" {
					return fmt.Errorf("query CLI: %w %s", runErr, stderr)
				}
				q.Milliseconds = append(q.Milliseconds, ms)
				q.Returned = len(response.Nodes) + len(response.Results)
				q.Candidates = len(response.Candidates)
				q.Error = response.Error
			}
			for _, node := range nodes {
				if node.Name == term.name || node.QualifiedName == term.name || strings.HasSuffix(node.QualifiedName, term.name) {
					q.Matched++
				}
			}
			if cmd == "query" && q.Matched > 50 {
				q.Examined = 0
				for _, node := range nodes {
					q.Examined++
					if node.Name == term.name || node.QualifiedName == term.name || strings.HasSuffix(node.QualifiedName, term.name) {
						q.Returned--
						if q.Returned == 0 {
							break
						}
					}
				}
				q.Returned = 50
			}
			ordered := append([]float64{}, q.Milliseconds...)
			q.Decoded, q.FilterReturned = len(nodes), q.Matched
			if cmd == "query" && q.FilterReturned > 50 {
				q.FilterReturned = 50
			}
			sort.Float64s(ordered)
			q.MedianMS, q.MinMS, q.MaxMS = ordered[2], ordered[0], ordered[4]
			fmt.Fprintf(os.Stderr, "Q8 %s/%s %q: median %.1fms returned=%d candidates=%d error=%q\n", q.Category, q.Command, q.Term, q.MedianMS, q.Returned, q.Candidates, q.Error)
			r.Queries = append(r.Queries, q)
		}
	}
	for _, s := range r.Samples {
		if s.Profiled || s.Instrumented || s.Target == "equivalence" || s.Run == "before-external" || s.Run == "after-external" {
			continue
		}
		key := fmt.Sprintf("%s/w%d/%s", s.Target, s.Workers, s.Run)
		if r.Medians[key] == nil {
			r.Medians[key] = map[string][]float64{}
		}
		for field, v := range map[string]float64{"index_ms": float64(s.Summary.Total) / 1e6, "scan_ms": float64(s.Summary.Scan) / 1e6, "parse_ms": float64(s.Summary.Parse) / 1e6, "other_index_ms": float64(s.Summary.Total-s.Summary.Scan-s.Summary.Parse) / 1e6, "go_heap_mib": float64(s.Memory.Heap) / (1 << 20), "go_total_mib": float64(s.Memory.Total) / (1 << 20), "ingest_ms": s.IngestMS, "peak_ws_mib": float64(s.Memory.WorkingSet) / (1 << 20), "peak_private_mib": float64(s.Memory.Private) / (1 << 20)} {
			r.Medians[key][field] = append(r.Medians[key][field], v)
		}
	}
	for _, fields := range r.Medians {
		for field, v := range fields {
			sort.Float64s(v)
			median := v[len(v)/2]
			if len(v)%2 == 0 {
				median = (v[len(v)/2-1] + median) / 2
			}
			fields[field] = []float64{median, v[0], v[len(v)-1]}
		}
	}
	r.DetailedMedians = aggregateDetails(r.Samples)
	if err = save(filepath.Join(abs, "report.json"), r); err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(abs, "summary.txt"))
	if err != nil {
		return err
	}
	w := bufio.NewWriter(io.MultiWriter(os.Stdout, f))
	fmt.Fprintln(w, "Q1/Q3/Q6: configuration | supported/files/records | index ms median [min,max] | ingest ms | WS/private/Go heap/Go total MiB [median,min,max]")
	keys := []string{}
	for key := range r.Medians {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		m := r.Medians[key]
		for _, s := range r.Samples {
			if s.Profiled || fmt.Sprintf("%s/w%d/%s", s.Target, s.Workers, s.Run) != key {
				continue
			}
			supported := 0
			for _, language := range []string{"javascript", "typescript", "tsx", "go", "python", "rust"} {
				supported += s.Summary.Languages[language]
			}
			records := s.Summary.Files + s.Summary.Nodes + s.Summary.Edges + s.Summary.Diagnostics
			fmt.Fprintf(w, "%s | %d/%d/%d | %v | %v | %v/%v/%v/%v\n", key, supported, s.Summary.Files, records, m["index_ms"], m["ingest_ms"], m["peak_ws_mib"], m["peak_private_mib"], m["go_heap_mib"], m["go_total_mib"])
			fmt.Fprintf(w, "  index/1000 supported files=%.1fms index/record=%.1fus ingest/record=%.1fus parsed=%d replayed=%d writes=%+v\n", m["index_ms"][0]*1000/float64(supported), m["index_ms"][0]*1000/float64(records), m["ingest_ms"][0]*1000/float64(records), s.Summary.Parsed, s.Summary.Reused, s.Writes)
			break
		}
	}
	for _, s := range r.Samples {
		if s.Profiled {
			fmt.Fprintf(w, "Q2 profiled w%d: memory=%+v (bytes); highest sampled heap phase=%s; see pprof text\n", s.Workers, s.Memory, s.Memory.HighestPhase)
		}
	}
	fmt.Fprintln(w, "Q2/Q6 native attribution: not measured:", r.NotMeasured["Q2 attribution"])
	for _, phase := range []string{"detail-cold", "detail-warm", "detail-warm-w1"} {
		if values := r.DetailedMedians[phase]; values != nil {
			fmt.Fprintf(w, "Q4/Q5 %s: median/min/max %v\n", phase, values)
		}
	}
	fmt.Fprintln(w, "Q4 scope: local replayed-entry validation; common final output validation not attributed to individual cached entries.")
	fmt.Fprintln(w, "Q5 map memory: distinct referenced string bytes are a lower bound; map buckets and allocator overhead not measured.")
	fmt.Fprintf(w, "Q7: %v\n", r.External)
	for _, q := range r.Queries {
		sort.Float64s(q.Milliseconds)
		fmt.Fprintf(w, "Q8 %s/%s %q: median %.1fms CLI rows=%d candidates=%d examined=%d filter returned=%d decoded=%d potential matches=%d error=%q\n", q.Category, q.Command, q.Term, q.Milliseconds[2], q.Returned, q.Candidates, q.Examined, q.FilterReturned, q.Decoded, q.Matched, q.Error)
	}
	for _, s := range r.Samples {
		if s.Target == "langflow" && s.Run == "cold" && !s.Profiled && s.Workers != 0 {
			fmt.Fprintf(w, "Q9 w%d codes=%v deadline=%d size-limit=%d agents=%d cursor=%d invalid-record-details=%v\n", s.Workers, s.DiagnosticCodes, s.DeadlineDiagnostics, s.DiagnosticCodes["file_too_large"], s.AgentsFiles, s.CursorFiles, s.InvalidRecords)
		}
	}
	fmt.Fprintln(w, "Equivalence: snapshot PASS; CLI/harness timing totals in report (assess noise separately).")
	if err = w.Flush(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
func main() {
	var err error
	if len(os.Args) > 1 && os.Args[1] == "child" {
		err = child(os.Args[2:])
	} else if len(os.Args) > 1 && os.Args[1] == "details" {
		err = runDetails(os.Args[2:])
	} else {
		err = run(os.Args[1:])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
