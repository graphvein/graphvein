package openapi

import (
	"context"
	"fmt"
	"time"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/store"
)

// Input is everything Generate reads. Read fills it from a store; tests
// build it by hand.
type Input struct {
	Repository string
	// Run is the latest index run, or nil when the repository is not
	// indexed.
	Run *store.Run
	// Routes are every stored route node, test scope included (Generate
	// counts and leaves those out).
	Routes []contract.Node
	// Diagnostics are the stored route composition diagnostics
	// (UnknownDiagnosticCodes), read to name the unknown part of a route
	// without a path.
	Diagnostics []contract.Diagnostic
	// Documents are the documents_route edges (imported operation ->
	// route).
	Documents []contract.Edge
	// Specs and SpecOperations are the imported api_spec and
	// api_operation nodes.
	Specs          []contract.Node
	SpecOperations []contract.Node
	// PathValues are the user's explorer.path_values from graphvein.yaml.
	PathValues []config.PathValue
	// Servers are the configured try-it environments (row OG-5, built by
	// Servers); a base URL inferred from code is never one.
	Servers []Server
}

// Servers lists the try-it environments configured for repository (row
// OG-5): first the project environments that give the member a base URL
// (graphvein-project.yaml, in file order), then the repository's own
// (graphvein.yaml), the order the try-it endpoints list them. Only the
// base URL, name and label are written; an environment's auth header is
// never in the document.
func Servers(project []config.ProjectEnvironment, own []config.Environment, repository string) []Server {
	var out []Server
	for _, pe := range project {
		if b, ok := pe.For(repository); ok {
			out = append(out, Server{URL: b.Text, Description: "configured try-it environment " + pe.Name + " (" + pe.Label + ")",
				X: ServerExt{Environment: pe.Name, Label: pe.Label, File: config.ProjectFile, Line: pe.Line}})
		}
	}
	for _, e := range own {
		out = append(out, Server{URL: e.BaseURL.Text, Description: "configured try-it environment " + e.Name + " (" + e.Label + ")",
			X: ServerExt{Environment: e.Name, Label: e.Label, File: config.File, Line: e.Line}})
	}
	return out
}

// UnknownDiagnosticCodes are the diagnostics that may name why a route has
// no path (docs/extract/routes.md "Composition and diagnostics").
var UnknownDiagnosticCodes = []string{"route_unmounted", "route_mount_unresolved", "route_prefix_unresolved"}

// Read reads the input of repository's document from s. Every read goes
// through the store interface; the caller checks consistency of the runs
// when it needs one (the server repeats the read when an index run
// commits in between).
func Read(ctx context.Context, s store.Store, repository string, pathValues []config.PathValue) (Input, error) {
	in := Input{Repository: repository, PathValues: pathValues}
	runs, err := s.Runs(ctx, repository)
	if err != nil {
		return in, fmt.Errorf("reading runs: %w", err)
	}
	in.Run = LatestRun(runs)
	if in.Routes, err = s.Nodes(ctx, repository, store.Query{Kind: "route"}); err != nil {
		return in, fmt.Errorf("reading route nodes: %w", err)
	}
	if in.Diagnostics, err = s.DiagnosticsWithCodes(ctx, repository, UnknownDiagnosticCodes, nil); err != nil {
		return in, fmt.Errorf("reading route diagnostics: %w", err)
	}
	if in.Documents, err = s.EdgesOfKind(ctx, repository, "documents_route"); err != nil {
		return in, fmt.Errorf("reading documents_route edges: %w", err)
	}
	if in.Specs, err = s.Nodes(ctx, repository, store.Query{Kind: "api_spec"}); err != nil {
		return in, fmt.Errorf("reading api_spec nodes: %w", err)
	}
	if in.SpecOperations, err = s.Nodes(ctx, repository, store.Query{Kind: "api_operation"}); err != nil {
		return in, fmt.Errorf("reading api_operation nodes: %w", err)
	}
	return in, nil
}

// LatestRun is the run with the greatest parsed finish time (the run the
// server's status reports), or nil.
func LatestRun(runs []store.Run) *store.Run {
	var best *store.Run
	var bestAt time.Time
	for i := range runs {
		at, err := time.Parse(time.RFC3339Nano, runs[i].FinishedAt)
		if err != nil {
			continue
		}
		if best == nil || at.After(bestAt) {
			best, bestAt = &runs[i], at
		}
	}
	return best
}
