// Package benchmark measures real files with one independent parser set per worker.
// It is spike code and emits no application/indexer facts.
package benchmark

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"graphvein/spike/parsing/internal/parse"
)

// File keeps private filenames internal so published statistics cannot expose them.
type File struct {
	Corpus, Language, Path, Label string
	Private                       bool
}

// Observation is one attempt, including failures, without source contents.
type Observation struct {
	File        File
	Bytes       int64
	ParseNS     int64
	ErrorNodes  int
	SyntaxError bool
	Error       string
	Panic       bool
	Cleanup     bool
}

// Detail records only public paths; private extrema carry a redacted label.
type Detail struct {
	Path       string `json:"path"`
	ParseNS    int64  `json:"parse_ns"`
	ErrorNodes int    `json:"error_nodes"`
	Error      string `json:"error,omitempty"`
}

// Stats separates successful parses, syntax recovery and runtime/read failures.
type Stats struct {
	Corpus      string   `json:"corpus"`
	Language    string   `json:"language"`
	Files       int      `json:"files"`
	Bytes       int64    `json:"bytes"`
	Parsed      int      `json:"parsed"`
	SumNS       int64    `json:"sum_parse_ns"`
	MedianNS    int64    `json:"median_parse_ns"`
	SyntaxFiles int      `json:"syntax_error_files"`
	Failures    int      `json:"failures"`
	Panics      int      `json:"panics"`
	Slowest     []Detail `json:"slowest_five"`
	Worst       []Detail `json:"worst_five"`
	Diagnostics []Detail `json:"diagnostics,omitempty"`
}

// Skipped makes unavailable grammar coverage visible instead of dropping files.
type Skipped struct {
	Corpus   string `json:"corpus"`
	Language string `json:"language"`
	Files    int    `json:"files"`
	Bytes    int64  `json:"bytes"`
}

// Run includes lifetime process peaks, never Go heap values masquerading as RSS.
type Run struct {
	Workers             int       `json:"workers"`
	WallNS              int64     `json:"wall_ns"`
	PeakWorkingSetBytes uint64    `json:"peak_working_set_bytes,omitempty"`
	PeakCommitBytes     uint64    `json:"peak_commit_bytes,omitempty"`
	MemoryMethod        string    `json:"memory_method"`
	ParseClock          string    `json:"parse_clock"`
	Stats               []Stats   `json:"stats"`
	Languages           []Stats   `json:"languages"`
	Skipped             []Skipped `json:"skipped"`
}

func language(path string) (name string, supported bool) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".js", ".jsx", ".mjs", ".cjs":
		return "javascript", true
	case ".ts", ".mts", ".cts":
		return "typescript", true
	case ".tsx":
		return "tsx", true
	case ".rs":
		return "rust", true
	case ".go":
		return "go", true
	case ".py":
		return "python", true
	case ".java":
		return "java", true
	case ".html":
		return "html", true
	case ".cs":
		return "c_sharp", true
	case ".dart":
		return "dart", true
	default:
		return "", false
	}
}

func ignored(name string) bool {
	switch name {
	case ".git", "node_modules", "target", "dist", "build", "vendor", ".venv", "venv", "__pycache__", ".codex", ".claude", ".opencode", ".kilo":
		return true
	}
	return false
}

// Scan uses source-only directory ignores; it never follows symlinks or installs
// corpus dependencies. Public labels are relative to the corpus checkout.
func Scan(root, corpus string, private bool) ([]File, []Skipped, error) {
	var files []File
	counts := map[string]*Skipped{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("walking corpus %s: %w", corpus, err)
		}
		if entry.IsDir() {
			if path != root && ignored(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		lang, supported := language(path)
		if lang == "html" && corpus == "angular" {
			lang = "angular"
		}
		if lang == "" {
			return nil
		}
		if !supported {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if counts[lang] == nil {
				counts[lang] = &Skipped{Corpus: corpus, Language: lang}
			}
			counts[lang].Files++
			counts[lang].Bytes += info.Size()
			return nil
		}
		label, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if private {
			label = "[private]"
		}
		files = append(files, File{corpus, lang, path, filepath.ToSlash(label), private})
		return nil
	})
	var skipped []Skipped
	for _, count := range counts {
		skipped = append(skipped, *count)
	}
	sort.Slice(skipped, func(i, j int) bool { return skipped[i].Language < skipped[j].Language })
	return files, skipped, err
}

// CorpusFiles validates every public pin before measuring and optionally scans
// a local repository in place. Private configuration is kept under .corpus.
func CorpusFiles(manifest, root, local string) ([]File, []Skipped, error) {
	f, err := os.Open(manifest)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	var files []File
	var skipped []Skipped
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 3 {
			return nil, nil, fmt.Errorf("invalid corpus manifest line")
		}
		checkout, err := filepath.Abs(filepath.Join(root, fields[0]))
		if err != nil {
			return nil, nil, err
		}
		pin, err := exec.Command("git", "-c", "safe.directory="+filepath.ToSlash(checkout), "-C", checkout, "rev-parse", "HEAD").Output()
		if err != nil || strings.TrimSpace(string(pin)) != fields[2] {
			return nil, nil, fmt.Errorf("corpus %s does not match pinned commit; run fetch-corpus.sh", fields[0])
		}
		status, err := exec.Command("git", "-c", "safe.directory="+filepath.ToSlash(checkout), "-C", checkout, "status", "--porcelain").Output()
		if err != nil || len(status) > 0 {
			return nil, nil, fmt.Errorf("corpus %s is not clean", fields[0])
		}
		more, gaps, err := Scan(filepath.Join(root, fields[0]), fields[0], false)
		if err != nil {
			return nil, nil, err
		}
		files, skipped = append(files, more...), append(skipped, gaps...)
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, err
	}
	if local != "" {
		more, gaps, err := Scan(local, "private", true)
		if err != nil {
			return nil, nil, fmt.Errorf("scanning private corpus (details withheld)")
		}
		files, skipped = append(files, more...), append(skipped, gaps...)
	}
	return files, skipped, nil
}

type factory func(string) (parse.Parser, error)

func closeParser(p parse.Parser) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("closing parser: panic: %v", recovered)
		}
	}()
	p.Close()
	return
}

func attempt(file File, parsers map[string]parse.Parser, create factory) (result Observation) {
	result.File = file
	defer func() {
		if recovered := recover(); recovered != nil {
			result.Panic = true
			result.Error = fmt.Sprintf("parser panic: %v", recovered)
			// A panic may leave the guest or parser unusable. The next file gets
			// an independent replacement rather than reusing damaged state.
			if p := parsers[file.Language]; p != nil {
				if closeErr := closeParser(p); closeErr != nil {
					result.Error += "; " + closeErr.Error()
				}
				delete(parsers, file.Language)
			}
		}
		if result.Error != "" && file.Private {
			result.Error = "private file failed (details withheld)"
		}
	}()
	source, err := os.ReadFile(file.Path)
	if err != nil {
		result.Error = fmt.Sprintf("reading %s: %v", file.Label, err)
		return
	}
	result.Bytes = int64(len(source))
	p := parsers[file.Language]
	if p == nil {
		p, err = create(file.Language)
		if err != nil {
			result.Error = fmt.Sprintf("loading %s: %v", file.Language, err)
			return
		}
		parsers[file.Language] = p
	}
	start, err := timestamp()
	if err != nil {
		result.Error = err.Error()
		return
	}
	tree, err := p.Parse(source)
	end, clockErr := timestamp()
	result.ParseNS = end - start
	if clockErr != nil {
		if tree != nil {
			tree.Close()
		}
		result.Error = clockErr.Error()
		return
	}
	if err != nil {
		result.Error = fmt.Sprintf("parsing %s: %v", file.Label, err)
		if closeErr := closeParser(p); closeErr != nil {
			result.Panic = true
			result.Error += "; " + closeErr.Error()
		}
		delete(parsers, file.Language)
		return
	}
	defer tree.Close()
	result.SyntaxError = tree.Root().HasError()
	if result.SyntaxError {
		result.ErrorNodes = parse.ErrorCount(tree)
	}
	return
}

// Measure retains parser/runtime pairs sequentially per worker and releases
// every tree before another file uses that parser. Wall time includes cold setup,
// reading, error inspection and cleanup; per-file timings cover Parse only.
func Measure(files []File, skipped []Skipped, workers int) (Run, error) {
	if workers < 1 {
		return Run{}, fmt.Errorf("workers must be positive")
	}
	start := time.Now()
	jobs := make(chan File)
	results := make(chan Observation)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			parsers := map[string]parse.Parser{}
			lastFiles := map[string]File{}
			defer func() {
				for lang, p := range parsers {
					if err := closeParser(p); err != nil {
						file := lastFiles[lang]
						message := err.Error()
						if file.Private {
							message = "closing private parser failed (details withheld)"
						}
						results <- Observation{File: file, Error: message, Panic: true, Cleanup: true}
					}
				}
			}()
			for file := range jobs {
				lastFiles[file.Language] = file
				result := attempt(file, parsers, func(lang string) (parse.Parser, error) { return parse.New("wasm", lang) })
				results <- result
			}
		}()
	}
	go func() {
		for _, file := range files {
			jobs <- file
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()
	var observations []Observation
	for result := range results {
		observations = append(observations, result)
	}
	run := Run{Workers: workers, WallNS: time.Since(start).Nanoseconds(), Skipped: skipped, ParseClock: timingMethod}
	run.Stats = summarize(observations)
	combined := make([]Observation, len(observations))
	copy(combined, observations)
	for i := range combined {
		if !combined[i].File.Private {
			combined[i].File.Label = combined[i].File.Corpus + "/" + combined[i].File.Label
		}
		combined[i].File.Corpus = "all"
	}
	run.Languages = summarize(combined)
	run.PeakWorkingSetBytes, run.PeakCommitBytes, run.MemoryMethod = peakMemory()
	return run, nil
}

func summarize(results []Observation) []Stats {
	groups := map[string][]Observation{}
	for _, result := range results {
		key := result.File.Corpus + "/" + result.File.Language
		groups[key] = append(groups[key], result)
	}
	keys := []string{}
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	stats := []Stats{}
	for _, key := range keys {
		group := groups[key]
		s := Stats{Corpus: group[0].File.Corpus, Language: group[0].File.Language, Slowest: []Detail{}, Worst: []Detail{}}
		times := []int64{}
		for _, r := range group {
			if !r.Cleanup {
				s.Files++
				s.Bytes += r.Bytes
			}
			if r.Panic {
				s.Panics++
			}
			if r.Error != "" {
				s.Failures++
				s.Diagnostics = append(s.Diagnostics, Detail{Path: r.File.Label, Error: r.Error})
				continue
			}
			s.Parsed++
			s.SumNS += r.ParseNS
			times = append(times, r.ParseNS)
			if r.SyntaxError {
				s.SyntaxFiles++
			}
			detail := Detail{Path: r.File.Label, ParseNS: r.ParseNS, ErrorNodes: r.ErrorNodes}
			s.Slowest = append(s.Slowest, detail)
			if r.SyntaxError {
				s.Worst = append(s.Worst, detail)
			}
		}
		sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
		if len(times) > 0 {
			s.MedianNS = times[len(times)/2]
			if len(times)%2 == 0 {
				s.MedianNS = (times[len(times)/2-1] + s.MedianNS) / 2
			}
		}
		sort.Slice(s.Slowest, func(i, j int) bool {
			a, b := s.Slowest[i], s.Slowest[j]
			if a.ParseNS == b.ParseNS {
				return a.Path < b.Path
			}
			return a.ParseNS > b.ParseNS
		})
		sort.Slice(s.Worst, func(i, j int) bool {
			a, b := s.Worst[i], s.Worst[j]
			if a.ErrorNodes == b.ErrorNodes {
				return a.Path < b.Path
			}
			return a.ErrorNodes > b.ErrorNodes
		})
		sort.Slice(s.Diagnostics, func(i, j int) bool { return s.Diagnostics[i].Path < s.Diagnostics[j].Path })
		if len(s.Slowest) > 5 {
			s.Slowest = s.Slowest[:5]
		}
		if len(s.Worst) > 5 {
			s.Worst = s.Worst[:5]
		}
		stats = append(stats, s)
	}
	return stats
}
