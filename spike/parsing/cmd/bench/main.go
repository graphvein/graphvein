// Command bench isolates each worker count and cold grammar in a fresh process.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"graphvein/spike/parsing/grammars"
	"graphvein/spike/parsing/internal/benchmark"
	"graphvein/spike/parsing/internal/coverage"
	"graphvein/spike/parsing/internal/parse"
)

type coldResult struct {
	Language              string `json:"language"`
	ProcessToFirstParseNS int64  `json:"process_to_first_parse_ns"`
	Error                 string `json:"error,omitempty"`
}

type report struct {
	GoVersion  string          `json:"go_version"`
	OS         string          `json:"os"`
	Arch       string          `json:"arch"`
	CPUs       int             `json:"logical_cpus"`
	CPU        string          `json:"cpu"`
	GOMAXPROCS int             `json:"gomaxprocs"`
	Cold       []coldResult    `json:"cold_start"`
	Runs       []benchmark.Run `json:"runs"`
	Error      string          `json:"error,omitempty"`
}

func cold(language string) (result coldResult) {
	result.Language = language
	defer func() {
		if value := recover(); value != nil {
			result.Error = fmt.Sprintf("panic: %v", value)
		}
	}()
	source, err := os.ReadFile(coverage.SamplePath(language))
	if err != nil {
		result.Error = err.Error()
		return
	}
	p, err := parse.New("wasm", language)
	if err != nil {
		result.Error = err.Error()
		return
	}
	defer p.Close()
	tree, err := p.Parse(source)
	if err != nil {
		result.Error = err.Error()
		return
	}
	defer tree.Close()
	if tree.Root().HasError() {
		result.Error = "sample contains syntax errors"
		return
	}
	// Signal immediately after the first successful parse, before cleanup.
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		result.Error = err.Error()
	}
	return
}

func subprocess(args []string, result any) (int64, error) {
	self, err := os.Executable()
	if err != nil {
		return 0, err
	}
	cmd := exec.Command(self, args...)
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return 0, err
	}
	start := time.Now()
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	decodeErr := json.NewDecoder(stdout).Decode(result)
	elapsed := time.Since(start).Nanoseconds()
	waitErr := cmd.Wait()
	if decodeErr != nil {
		return elapsed, fmt.Errorf("reading child result: %w", decodeErr)
	}
	return elapsed, waitErr
}

func main() {
	child := flag.Bool("child", false, "internal: one worker-scaling subprocess")
	coldLanguage := flag.String("cold", "", "internal: cold-start grammar subprocess")
	workers := flag.Int("workers", 1, "internal: worker count")
	local := flag.String("local", "", "optional private corpus path; output uses aggregate/redacted labels")
	out := flag.String("out", "out/results-t002.json", "benchmark JSON output")
	flag.Parse()
	if *coldLanguage != "" {
		result := cold(*coldLanguage)
		if result.Error != "" {
			_ = json.NewEncoder(os.Stdout).Encode(result)
			os.Exit(1)
		}
		return
	}
	if *local == "" {
		data, err := os.ReadFile(".corpus/local-path.txt")
		if err != nil && !os.IsNotExist(err) {
			fmt.Fprintln(os.Stderr, "reading optional local corpus configuration failed")
			os.Exit(1)
		}
		*local = strings.TrimSpace(string(data))
	}
	if *child {
		files, skipped, err := benchmark.CorpusFiles("corpus.tsv", ".corpus", *local)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		run, err := benchmark.Measure(files, skipped, *workers)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if err := json.NewEncoder(os.Stdout).Encode(run); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		for _, stats := range run.Stats {
			if stats.Failures > 0 || stats.Panics > 0 {
				os.Exit(1)
			}
		}
		return
	}
	result := report{GoVersion: runtime.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH, CPUs: runtime.NumCPU(), CPU: os.Getenv("PROCESSOR_IDENTIFIER"), GOMAXPROCS: runtime.GOMAXPROCS(0)}
	for _, language := range grammars.Available() {
		fmt.Fprintln(os.Stderr, "Cold start:", language)
		var measurement coldResult
		ns, err := subprocess([]string{"-cold", language}, &measurement)
		measurement.ProcessToFirstParseNS = ns
		if err != nil {
			measurement.Error = fmt.Sprintf("%v; %s", err, measurement.Error)
			result.Error = "cold-start failure"
		}
		result.Cold = append(result.Cold, measurement)
	}
	for _, count := range []int{1, 4, 8} {
		fmt.Fprintln(os.Stderr, "Corpus workers:", count)
		var run benchmark.Run
		_, err := subprocess([]string{"-child", "-workers", fmt.Sprint(count), "-local", *local}, &run)
		result.Runs = append(result.Runs, run)
		if err != nil {
			result.Error = fmt.Sprintf("worker %d run failed: %v", count, err)
			break
		}
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err == nil {
		err = os.WriteFile(*out, append(data, '\n'), 0644)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if result.Error != "" {
		fmt.Fprintln(os.Stderr, result.Error)
		os.Exit(1)
	}
	fmt.Println("Results:", *out)
}
