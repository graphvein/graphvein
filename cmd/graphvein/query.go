package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/query"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/store/postgres"
	"github.com/graphvein/graphvein/internal/store/sqlite"
	"github.com/graphvein/graphvein/internal/traverse"
)

// openReadStore opens root's store read-only, located as openStore
// locates it (ADR 0093); it never creates one.
func openReadStore(ctx context.Context, root string) (store.Store, error) {
	if url := os.Getenv("GRAPHVEIN_POSTGRES_URL"); url != "" {
		return postgres.OpenRead(ctx, url)
	}
	p, err := sqlitePath(root)
	if err != nil {
		return nil, err
	}
	return sqlite.OpenRead(ctx, p)
}

func queryCommand(command string, args []string, stdout, stderr io.Writer) int {
	f := flag.NewFlagSet(command, flag.ContinueOnError)
	f.SetOutput(stderr)
	repo := f.String("repository", "", "logical repository ID (defaults to directory name)")
	jsonOutput := f.Bool("json", false, "emit stable JSON")
	kind, file, projectArg := "", "", ""
	limit, depth := 50, 1
	if command == "query" {
		f.StringVar(&kind, "kind", "", "node kind")
		f.StringVar(&file, "file", "", "exact repository-relative file")
		f.IntVar(&limit, "limit", 50, "maximum nodes")
	} else {
		if command == "impact" {
			depth = 3
		}
		f.IntVar(&depth, "depth", depth, "reverse traversal depth")
		f.StringVar(&projectArg, "project", "", "project manifest (graphvein-project.yaml) or its directory: traverse every member through the project's links")
	}
	var positional, options []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if strings.HasPrefix(a, "-") {
			options = append(options, a)
			name := strings.TrimLeft(strings.SplitN(a, "=", 2)[0], "-")
			if name != "json" && !strings.Contains(a, "=") && name != "h" && name != "help" && i+1 < len(args) {
				i++
				options = append(options, args[i])
			}
		} else {
			positional = append(positional, a)
		}
	}
	if err := f.Parse(options); err != nil {
		return 2
	}
	if len(positional) < 1 || len(positional) > 2 || limit <= 0 || depth <= 0 {
		fmt.Fprintf(stderr, "usage: graphvein %s <term-or-target> [path] [flags]; limit and depth must be positive\n", command)
		return 2
	}
	if projectArg == "" && depth > query.MaxDepth || depth > query.ProjectMaxDepth {
		max := query.MaxDepth
		if projectArg != "" {
			max = query.ProjectMaxDepth
		}
		fmt.Fprintf(stderr, "graphvein %s: depth %d is above the maximum %d\n", command, depth, max)
		return 2
	}
	if projectArg != "" {
		if len(positional) != 1 {
			fmt.Fprintf(stderr, "usage: graphvein %s <term-or-target> --project <manifest or its directory> [--repository MEMBER] [--depth N] [--json]; members come from the manifest, so no path\n", command)
			return 2
		}
		return projectQueryCommand(command, positional[0], projectArg, *repo, depth, *jsonOutput, stdout, stderr)
	}
	root := "."
	if len(positional) == 2 {
		root = positional[1]
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	var override *string
	f.Visit(func(flag *flag.Flag) {
		if flag.Name == "repository" {
			override = repo
		}
	})
	*repo, err = config.ResolveRepository(abs, override)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	ctx := context.Background()
	s, err := openReadStore(ctx, abs)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer s.Close()
	out, err := query.Execute(ctx, s, query.Options{Command: command, Term: positional[0], Root: abs, Repository: *repo, Kind: kind, File: file, Limit: limit, Depth: depth})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if *jsonOutput {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(out); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	} else {
		if !out.Freshness.Indexed {
			fmt.Fprintln(stdout, "Not indexed")
		} else {
			fmt.Fprintf(stdout, "%d files changed since last index\n", len(out.Freshness.Changed))
		}
		if len(out.Freshness.Changed) > 0 {
			fmt.Fprintln(stdout, "Warning: index is stale")
		}
		for _, p := range out.Freshness.Changed {
			fmt.Fprintln(stdout, p)
		}
		for _, n := range out.Nodes {
			fmt.Fprintf(stdout, "%s %s %s", n.Kind, n.Identity, n.File)
			if n.Span != nil {
				fmt.Fprintf(stdout, " %d:%d-%d:%d", n.Span.StartLine, n.Span.StartColumn, n.Span.EndLine, n.Span.EndColumn)
			}
			if n.IDE != nil {
				fmt.Fprintf(stdout, " %s", n.IDE.Link)
			}
			fmt.Fprintln(stdout)
		}
		if out.Total != nil && *out.Total > len(out.Nodes) {
			fmt.Fprintf(stdout, "%d more not shown; raise --limit\n", *out.Total-len(out.Nodes))
		}
		if out.Target != nil {
			fmt.Fprintf(stdout, "Target %s", out.Target.Identity)
			if out.Target.IDE != nil {
				fmt.Fprintf(stdout, " %s", out.Target.IDE.Link)
			}
			fmt.Fprintln(stdout)
		}
		for _, r := range out.Results {
			link := ""
			if r.Node.IDE != nil {
				link = " " + r.Node.IDE.Link
			}
			label := ""
			if r.Provenance == "inferred" {
				label = " inferred confidence=" + contract.ConfidenceText(*r.Confidence)
			}
			fmt.Fprintf(stdout, "%d %s%s%s\n", r.Distance, r.Node.Identity, link, label)
			for _, e := range r.Path {
				fmt.Fprintf(stdout, "  %s %s -> %s %s confidence=%s\n", e.Kind, e.Source, e.Target, e.Provenance, contract.ConfidenceText(e.Confidence))
				for _, v := range e.Evidence {
					fmt.Fprintf(stdout, "    %s %d:%d-%d:%d\n", v.File, v.Span.StartLine, v.Span.StartColumn, v.Span.EndLine, v.Span.EndColumn)
				}
			}
		}
		printWalkLimits(stdout, out.Bounds, out.Ends)
		if out.Error != "" {
			fmt.Fprintln(stdout, out.Error)
			if out.Error == "no matching target" && len(out.Candidates) > 0 {
				fmt.Fprintln(stdout, partialHeader)
			}
			for _, id := range out.Candidates {
				fmt.Fprintln(stdout, id)
			}
		}
		if command == "impact" {
			if !out.BlindSpotData {
				fmt.Fprintln(stdout, "Blind spot: data unavailable")
			} else {
				fmt.Fprintf(stdout, "Blind spot: %d possibly missing callers (name-matched call sites)\n", out.PossiblyMissingCallers)
				for i, site := range out.PossiblyMissingCallSites {
					if i == 20 {
						fmt.Fprintf(stdout, "  %d more call sites; use --json\n", len(out.PossiblyMissingCallSites)-20)
						break
					}
					fmt.Fprintf(stdout, "  %s:%d %s\n", site.File, site.Span.StartLine, *site.Name)
				}
			}
		}
	}
	if out.Error != "" {
		return 1
	}
	return 0
}

// printWalkLimits prints the caps that bound a callers or impact walk and
// its "? not modelled" ends, so text output never reads a cut as "nothing
// beyond" (T030 D2, G10).
func printWalkLimits(w io.Writer, bounds []traverse.Bound, ends []query.End) {
	for _, b := range bounds {
		fmt.Fprintln(w, traverse.BoundText(b))
	}
	for _, e := range ends {
		fmt.Fprintf(w, "%s %s\n", e.Node, e.Text)
	}
}

// partialHeader introduces the candidates of a term that names no node but
// ends a qualified name inside a name ("Items" in "listItems"): they are
// offered, never chosen (T030 review).
const partialHeader = "names that end with this term inside a name (not resolved; pass one identity or the whole name):"
