package sequelize

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/typescript"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/scan"
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
	raw := map[string][]byte{}
	for f, s := range sources {
		raw[f] = []byte(s)
	}
	return build(t, raw)
}

// build parses every script file of sources and fills the input.
func build(t *testing.T, sources map[string][]byte) Input {
	t.Helper()
	in := Input{TypeScript: map[string]typescript.Facts{}, Models: map[string]*typescript.ModelSyntax{}, Sources: sources}
	var files []string
	var jobs []parse.Job
	for f, s := range sources {
		in.TypeScript[f] = typescript.Facts{Exports: map[string]string{}}
		if lang := tsLanguage(f); lang != "" {
			files = append(files, f)
			jobs = append(jobs, parse.Job{Language: lang, Source: s})
		}
	}
	options := parse.Options{Workers: 2, CacheDir: filepath.Join(t.TempDir(), "cache")}
	err := parse.Consume(context.Background(), jobs, options, func(i int, r parse.Result) {
		if r.Err != nil {
			t.Errorf("%s: %v", files[i], r.Err)
			return
		}
		f := typescript.Extract(files[i], sources[files[i]], r.Root)
		in.TypeScript[files[i]] = f
		if m := typescript.CaptureModels(files[i], r.Root, f); m != nil {
			in.Models[files[i]] = m
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	in.Resolver = typescript.NewModuleResolver(in.TypeScript, in.Sources)
	return in
}

// corpusInput scans a corpus checkout (no indexing, nothing written) and
// builds the input.
func corpusInput(t *testing.T, root string) Input {
	t.Helper()
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("%v (run scripts/cloud-corpus.sh)", err)
	}
	scanned, err := scan.Walk(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	sources := map[string][]byte{}
	for _, f := range scanned.Files {
		sources[f.Record.Path] = f.Source
	}
	return build(t, sources)
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
