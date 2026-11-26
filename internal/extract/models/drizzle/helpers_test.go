package drizzle

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/typescript"
	"github.com/graphvein/graphvein/internal/parse"
)

// tsLanguage is the parser language of a TypeScript or JavaScript file, or
// "" for any other file.
func tsLanguage(file string) string {
	switch filepath.Ext(file) {
	case ".ts", ".mts", ".cts":
		return "typescript"
	case ".tsx":
		return "tsx"
	case ".js", ".mjs", ".cjs", ".jsx":
		return "javascript"
	}
	return ""
}

// input builds the link-time input from in-memory sources as the indexer
// does: every file in the TypeScript map (non-script files with empty
// facts), facts and model payloads from a real parse, and the shared
// resolver.
func input(t *testing.T, sources map[string]string) Input {
	t.Helper()
	in := Input{TypeScript: map[string]typescript.Facts{}, Models: map[string]*typescript.ModelSyntax{}, Sources: map[string][]byte{}}
	var files []string
	var jobs []parse.Job
	for f, s := range sources {
		in.Sources[f] = []byte(s)
		in.TypeScript[f] = typescript.Facts{Exports: map[string]string{}}
		if lang := tsLanguage(f); lang != "" {
			files = append(files, f)
			jobs = append(jobs, parse.Job{Language: lang, Source: []byte(s)})
		}
	}
	if len(jobs) > 0 {
		results, err := parse.Batch(context.Background(), jobs, parse.Options{CacheDir: filepath.Join(t.TempDir(), "cache")})
		if err != nil {
			t.Fatal(err)
		}
		for i, r := range results {
			if r.Err != nil {
				t.Fatalf("%s: %v", files[i], r.Err)
			}
			f := typescript.Extract(files[i], []byte(sources[files[i]]), r.Root)
			in.TypeScript[files[i]] = f
			if m := typescript.CaptureModels(files[i], r.Root, f); m != nil {
				in.Models[files[i]] = m
			}
		}
	}
	in.Resolver = typescript.NewModuleResolver(in.TypeScript, in.Sources)
	return in
}

// diagLines renders diagnostics as "code file: message" lines, sorted.
func diagLines(diags []contract.Diagnostic) []string {
	var out []string
	for _, d := range diags {
		out = append(out, d.Code+" "+d.File+": "+d.Message)
	}
	sort.Strings(out)
	return out
}

// hasDiag reports a diagnostic with code on file whose message contains
// every part.
func hasDiag(diags []contract.Diagnostic, code, file string, parts ...string) bool {
	for _, d := range diags {
		if d.Code != code || d.File != file {
			continue
		}
		ok := true
		for _, p := range parts {
			ok = ok && strings.Contains(d.Message, p)
		}
		if ok {
			return true
		}
	}
	return false
}

// moduleFiles lists a config's module files in discovery order.
func moduleFiles(c *Config) []string {
	var out []string
	for _, m := range c.Modules {
		out = append(out, m.File)
	}
	return out
}
