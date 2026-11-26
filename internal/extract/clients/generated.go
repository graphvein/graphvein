package clients

import (
	"bytes"
	"fmt"
	"regexp"

	"github.com/graphvein/graphvein/internal/contract"
)

// BundleMarker is one row of the closed list of committed bundler output
// markers (ADR 0085, docs/extract/clients.md "Committed bundler output"):
// a file whose repository path matches Path and whose content is minified
// is a bundler's build output. Such a file is still indexed, but its
// client calls are not linked to routes: a bundle repeats requests its
// sources already make, and minified code defeats the rules that keep
// links precise (T034: 11 of owncast's 12 false links came from it).
type BundleMarker struct {
	ID, Bundler, Layout string
	Path                *regexp.Regexp
	// Hash, when set, must also match: the hash part is not a word.
	Hash *regexp.Regexp
}

// BundleMarkers is the closed list; a file no row matches keeps every
// client rule (row GB-U).
var BundleMarkers = []BundleMarker{
	{ID: "GB-1", Bundler: "Next.js", Layout: "_next/static/", Path: regexp.MustCompile(`(^|/)_next/static/.+\.m?js$`)},
	// Vite's default hash is 8 characters of [A-Za-z0-9_-]; one with no
	// digit and no capital letter reads as a word (user-settings.js), so it
	// is no hash here (a real Vite hash is lower case only about once in a
	// thousand names).
	{ID: "GB-2", Bundler: "Vite", Layout: "assets/<name>-<hash>.js", Path: regexp.MustCompile(`(^|/)assets/[^/]+-[A-Za-z0-9_-]{8}\.m?js$`), Hash: regexp.MustCompile(`-[A-Za-z0-9_-]*[0-9A-Z][A-Za-z0-9_-]*\.m?js$`)},
	// Create React App names chunks [name].[contenthash:8].chunk.js.
	{ID: "GB-3", Bundler: "webpack (Create React App)", Layout: "static/js/<name>.<hash>[.chunk].js", Path: regexp.MustCompile(`(^|/)static/js/[^/]+\.[0-9a-f]{8}(\.chunk)?\.js$`)},
}

// MinifiedLine is the length (bytes) from which a line can count as
// minified (see minified).
const MinifiedLine = 1000

// bundleMarker returns the marker of a committed bundler output file, or
// nil when the path matches no marker or the file is not minified (a
// non-minified file in a build directory keeps every rule).
func bundleMarker(file string, source []byte) *BundleMarker {
	for i := range BundleMarkers {
		m := &BundleMarkers[i]
		if m.Path.MatchString(file) && (m.Hash == nil || m.Hash.MatchString(file)) && minified(source) {
			return m
		}
	}
	return nil
}

// minified decides for the whole file: it is minified when its dense
// long lines hold at least half of its bytes. A dense long line has at
// least MinifiedLine bytes and at least one ';' or '}' per 100 bytes, as
// minified code has (owncast's chunks with client calls: 96% or more of
// their bytes); a long data: URI, a base64 string or other long text in a
// hand-written file is not dense, and one long line in a longer file does
// not hold half of it.
func minified(source []byte) bool {
	dense := 0
	rest := source
	for len(rest) > 0 {
		line := rest
		if i := bytes.IndexByte(rest, '\n'); i >= 0 {
			line, rest = rest[:i], rest[i+1:]
		} else {
			rest = nil
		}
		if len(line) >= MinifiedLine && (bytes.Count(line, []byte(";"))+bytes.Count(line, []byte("}")))*100 >= len(line) {
			dense += len(line) + 1
		}
	}
	return len(source) > 0 && dense*2 >= len(source)
}

// GeneratedOutputDiagnostic is the code of the per-file diagnostic that
// says why a bundle's client calls are not linked.
const GeneratedOutputDiagnostic = "client_calls_generated_output"

func generatedDiagnostic(file string, m *BundleMarker, calls int) contract.Diagnostic {
	noun := "client calls are"
	if calls == 1 {
		noun = "client call is"
	}
	return contract.Diagnostic{Type: "diagnostic", File: file, Severity: "info", Code: GeneratedOutputDiagnostic,
		Message: fmt.Sprintf("committed %s build output (%s: path %s, minified: most bytes in lines of at least %d bytes dense with ; or }): its %d %s not linked to routes; set clients.link_generated_output: true in graphvein.yaml to link them", m.Bundler, m.ID, m.Layout, MinifiedLine, calls, noun)}
}
