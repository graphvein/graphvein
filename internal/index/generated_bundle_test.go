package index_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/graphvein/graphvein/internal/extract/clients"
	"github.com/graphvein/graphvein/internal/golden"
)

const bundleFixture = "../../testdata/fixtures/generated-bundle-output"

// TestGeneratedBundleOutput checks ADR 0085 on its fixture: the client
// calls of committed bundler output (a closed list of path markers plus a
// minified line) are recorded with status generated, link to no route
// and get one diagnostic per file; a file that matches a path marker but
// is not minified, or is minified outside the markers, keeps every rule;
// clients.link_generated_output: true links them all.
func TestGeneratedBundleOutput(t *testing.T) {
	index := func(optIn bool) (map[string]string, map[string]string, map[string]int, []any) {
		t.Helper()
		root := t.TempDir()
		if err := os.CopyFS(root, os.DirFS(bundleFixture)); err != nil {
			t.Fatal(err)
		}
		if optIn {
			if err := os.WriteFile(filepath.Join(root, "graphvein.yaml"), []byte("version: 1\nclients:\n  link_generated_output: true\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		s, opt := open(t), options(t)
		emit(t, root, s, opt)
		snapshot, err := s.Dump(context.Background(), "fixture")
		if err != nil {
			t.Fatal(err)
		}
		status, marker, diagnostics := map[string]string{}, map[string]string{}, map[string]int{}
		var projected []any
		linked := map[string]bool{}
		for _, e := range snapshot.Edges {
			if e.Kind == "calls_route" {
				linked[e.Source] = true
				projected = append(projected, e)
			}
		}
		for _, n := range snapshot.Nodes {
			if n.Kind != "client_call" || n.Attributes["status"] == "wrapper" {
				continue
			}
			projected = append(projected, n)
			s, _ := n.Attributes["status"].(string)
			if linked[n.Identity] {
				s += "+edge"
			}
			status[n.File] = s
			if m, ok := n.Attributes["generated_output"].(string); ok {
				marker[n.File] = m
			}
		}
		for _, d := range snapshot.Diagnostics {
			if d.Code == clients.GeneratedOutputDiagnostic {
				diagnostics[d.File]++
				projected = append(projected, d)
			}
		}
		return status, marker, diagnostics, projected
	}
	const (
		next    = "static/web/_next/static/chunks/0t035gbxq9z1.js"
		vite    = "public/assets/index-Bx7k_2Qa.js"
		webpack = "www/static/js/main.1a2b3c4d.chunk.js"
	)
	status, marker, diagnostics, projected := index(false)
	want := map[string]string{
		"src/api.ts": "linked+edge",
		next:         "generated", vite: "generated", webpack: "generated",
		// GB-U: a marker path without a minified line, a minified-looking
		// name outside every marker.
		"static/web/_next/static/chunks/t035-loader.js": "linked+edge",
		"src/assets/helper-abcdefgh.js":                 "linked+edge",
		// Hand-written files with one long data: URI: a word
		// is no Vite hash, and a long line that is not dense code does not
		// make the file minified.
		"src/assets/user-settings.js": "linked+edge",
		"src/assets/api-requests.mjs": "linked+edge",
		"src/assets/icon-Ab12Cd34.js": "linked+edge",
	}
	wantMarker := map[string]string{next: "GB-1", vite: "GB-2", webpack: "GB-3"}
	for file, s := range want {
		if status[file] != s {
			t.Errorf("%s: status %q, want %q", file, status[file], s)
		}
		if marker[file] != wantMarker[file] {
			t.Errorf("%s: generated_output %q, want %q", file, marker[file], wantMarker[file])
		}
		if want := map[bool]int{true: 1}[wantMarker[file] != ""]; diagnostics[file] != want {
			t.Errorf("%s: %d diagnostics %s, want %d", file, diagnostics[file], clients.GeneratedOutputDiagnostic, want)
		}
	}
	if len(status) != len(want) {
		t.Errorf("client calls %v, want %v", status, want)
	}
	reached := map[string]bool{}
	for _, m := range marker {
		reached[m] = true
	}
	for _, m := range clients.BundleMarkers {
		if !reached[m.ID] {
			t.Errorf("the fixture never reaches marker %s", m.ID)
		}
	}
	golden.Check(t, "../../testdata/fixtures/generated-bundle-output.golden.json", projected)

	status, marker, diagnostics, _ = index(true)
	for file := range want {
		if status[file] != "linked+edge" || marker[file] != "" || diagnostics[file] != 0 {
			t.Errorf("opt-in: %s status %q, marker %q, %d diagnostics", file, status[file], marker[file], diagnostics[file])
		}
	}
}
