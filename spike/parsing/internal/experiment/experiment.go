// Package experiment holds fixture checks and a reproducible parse-only workload.
package experiment

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"time"

	"graphvein/spike/parsing/internal/parse"
)

// SampleResult records failures independently so one file cannot hide another.
type SampleResult struct {
	Path    string        `json:"path"`
	Entries []parse.Entry `json:"entries"`
	Correct bool          `json:"correct"`
	Error   string        `json:"error,omitempty"`
}

// Measurement distinguishes parse time from initialization and AST extraction.
type Measurement struct {
	Backend    string         `json:"backend"`
	Samples    []SampleResult `json:"samples"`
	ParseNS    []int64        `json:"parse_ns,omitempty"`
	MedianNS   int64          `json:"median_ns,omitempty"`
	LargeBytes int            `json:"large_bytes,omitempty"`
	LargeLines int            `json:"large_lines,omitempty"`
	Error      string         `json:"error,omitempty"`
}

// CheckGolden compares ordered names, node kinds and one-based source lines.
func CheckGolden(entries []parse.Entry, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading golden %s: %w", path, err)
	}
	var expected []parse.Entry
	if err := json.Unmarshal(data, &expected); err != nil {
		return fmt.Errorf("decoding golden %s: %w", path, err)
	}
	if !reflect.DeepEqual(entries, expected) {
		actual, _ := json.MarshalIndent(entries, "", "  ")
		return fmt.Errorf("golden mismatch %s\nexpected:\n%s\nactual:\n%s", path, data, actual)
	}
	return nil
}

// LargeSource generates 6,000 valid lines with distinct names and varied syntax.
// It is synthetic TypeScript, as explicitly permitted by T001.
func LargeSource() []byte {
	var b strings.Builder
	for i := 0; i < 1000; i++ {
		fmt.Fprintf(&b, "export interface Item%d { id: number; name: string }\n", i)
		fmt.Fprintf(&b, "export type ID%d = number | string;\n", i)
		fmt.Fprintf(&b, "export function item%d(id: number): Item%d {\n", i, i)
		fmt.Fprintf(&b, "  return { id, name: 'item-%d' }; }\n", i)
		fmt.Fprintf(&b, "export const card%d = (item: Item%d) => <article>{item.name}</article>;\n", i, i)
		fmt.Fprintf(&b, "export class Store%d { get(id: number) { return item%d(id); } }\n", i, i)
	}
	return []byte(b.String())
}

// Run checks both samples before taking five fresh-tree parses with a reused parser.
func Run(backend, largePath string) Measurement {
	result := Measurement{Backend: backend}
	for _, sample := range []struct{ path, language string }{{"samples/items.tsx", "tsx"}, {"samples/items.rs", "rust"}} {
		r := SampleResult{Path: sample.path}
		entries, err := extractFile(backend, sample.language, sample.path)
		r.Entries = entries
		if err == nil {
			err = CheckGolden(entries, sample.path+".golden.json")
		}
		if err != nil {
			r.Error = err.Error()
		} else {
			r.Correct = true
		}
		result.Samples = append(result.Samples, r)
	}
	if largePath != "" {
		if err := measure(&result, largePath); err != nil {
			result.Error = err.Error()
		}
	}
	return result
}

func extractFile(backend, language, path string) ([]parse.Entry, error) {
	source, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	p, err := parse.New(backend, language)
	if err != nil {
		return nil, fmt.Errorf("initializing for %s: %w", path, err)
	}
	defer p.Close()
	t, err := p.Parse(source)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	defer t.Close()
	entries, err := parse.Extract(t, source)
	if err != nil {
		return nil, fmt.Errorf("extracting %s: %w", path, err)
	}
	return entries, nil
}

func measure(result *Measurement, path string) error {
	source, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	result.LargeBytes, result.LargeLines = len(source), strings.Count(string(source), "\n")
	p, err := parse.New(result.Backend, "tsx")
	if err != nil {
		return err
	}
	defer p.Close()
	for i := 0; i < 5; i++ {
		start := time.Now()
		tree, err := p.Parse(source)
		elapsed := time.Since(start).Nanoseconds()
		if err != nil {
			return fmt.Errorf("parsing %s run %d: %w", path, i+1, err)
		}
		hasError := tree.Root().HasError()
		tree.Close()
		if hasError {
			return fmt.Errorf("syntax errors in %s run %d", path, i+1)
		}
		result.ParseNS = append(result.ParseNS, elapsed)
	}
	sorted := append([]int64(nil), result.ParseNS...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	result.MedianNS = sorted[2]
	return nil
}

// OK rejects any failed fixture or requested benchmark in the runner.
func (m Measurement) OK() bool {
	if m.Error != "" || len(m.Samples) != 2 {
		return false
	}
	for _, s := range m.Samples {
		if !s.Correct {
			return false
		}
	}
	return true
}
