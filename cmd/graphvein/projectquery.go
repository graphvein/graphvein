package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/projectlink"
	"github.com/graphvein/graphvein/internal/query"
	"github.com/graphvein/graphvein/internal/traverse"
)

// projectQueryCommand is graphvein callers and impact with --project (ADR
// 0075, T022 B7): the term is looked up in the named member (or in every
// member) of the project store, and the traversal crosses repositories
// through the project's stored links.
func projectQueryCommand(command, term, manifest, repository string, depth int, jsonOutput bool, stdout, stderr io.Writer) int {
	dir, err := projectDir(manifest)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	project, err := config.LoadProject(dir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	ctx := context.Background()
	s, err := openReadStore(ctx, project.Dir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer s.Close()
	out, err := query.ExecuteProject(ctx, s, query.ProjectOptions{Command: command, Term: term, Repository: repository, Depth: depth, Project: project})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if jsonOutput {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(out); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	} else {
		printProjectQuery(stdout, command, out)
	}
	if out.Error != "" {
		return 1
	}
	return 0
}

// printProjectQuery is the text form: member freshness, the link pass
// state, then one line per reached node with its repository and path.
// Project link steps show their assumption and, when stale, why.
func printProjectQuery(w io.Writer, command string, out query.ProjectResponse) {
	fmt.Fprintf(w, "Project %s: %d members\n", out.Project, len(out.Members))
	for _, m := range out.Members {
		switch {
		case !m.Indexed:
			fmt.Fprintf(w, "  %s: not indexed\n", m.Repository)
		case !m.Checkout:
			fmt.Fprintf(w, "  %s: indexed; no checkout here\n", m.Repository)
		case len(m.Changed) > 0:
			fmt.Fprintf(w, "  %s: %d files changed since last index (index is stale)\n", m.Repository, len(m.Changed))
		default:
			fmt.Fprintf(w, "  %s: 0 files changed since last index\n", m.Repository)
		}
	}
	switch {
	case !out.LinkPass.Stored:
		fmt.Fprintln(w, "Link pass: none yet; run graphvein index --project (no cross-repository links followed)")
	case len(out.LinkPass.StaleReasons) == 0:
		fmt.Fprintf(w, "Link pass %d: fresh, %d links\n", out.LinkPass.Pass, out.LinkPass.Links)
	default:
		fmt.Fprintf(w, "Link pass %d: stale (%s), %d links; stale links are followed and marked\n", out.LinkPass.Pass, strings.Join(out.LinkPass.StaleReasons, "; "), out.LinkPass.Links)
	}
	if out.Target != nil {
		fmt.Fprintf(w, "Target %s %s", out.Target.Repository, out.Target.Identity)
		if out.Target.IDE != nil {
			fmt.Fprintf(w, " %s", out.Target.IDE.Link)
		}
		fmt.Fprintln(w)
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
		if r.Stale {
			label += " stale"
		}
		fmt.Fprintf(w, "%d %s %s%s%s\n", r.Distance, r.Node.Repository, r.Node.Identity, link, label)
		for _, e := range r.Path {
			kind := e.Kind
			if e.ProjectLink {
				kind += " (project link)"
			}
			fmt.Fprintf(w, "  %s %s %s -> %s %s %s confidence=%s\n", kind, e.SourceRepository, e.Source, e.TargetRepository, e.Target, e.Provenance, contract.ConfidenceText(e.Confidence))
			if e.ProjectLink {
				fmt.Fprintf(w, "    assumption: %s\n", e.Assumption)
			}
			if e.Stale {
				fmt.Fprintf(w, "    stale: %s\n", strings.Join(e.StaleReasons, "; "))
			}
			for _, v := range e.Evidence {
				fmt.Fprintf(w, "    %s %d:%d-%d:%d\n", v.File, v.Span.StartLine, v.Span.StartColumn, v.Span.EndLine, v.Span.EndColumn)
			}
		}
	}
	for _, b := range out.Bounds {
		fmt.Fprintln(w, traverse.BoundText(b))
	}
	for _, e := range out.Ends {
		fmt.Fprintf(w, "%s %s %s\n", e.Repository, e.Node, e.Text)
	}
	if out.Error != "" {
		fmt.Fprintln(w, out.Error)
		if out.Error == "no matching target" && len(out.Candidates) > 0 {
			fmt.Fprintln(w, partialHeader)
		}
		for _, c := range out.Candidates {
			fmt.Fprintf(w, "%s %s\n", c.Repository, c.Identity)
		}
	}
	across := fmt.Sprintf("%d possible callers in other repositories (ambiguous or incomplete, not linked)", len(out.PossibleCallers))
	switch {
	case command != "impact":
		if out.Target != nil {
			fmt.Fprintf(w, "Blind spot: %s\n", across)
		}
	case !out.BlindSpotData:
		fmt.Fprintf(w, "Blind spot: name-matched call sites unavailable; %s\n", across)
	default:
		fmt.Fprintf(w, "Blind spot: %d possibly missing callers (name-matched call sites), %s\n", out.PossiblyMissingCallers, across)
		for i, site := range out.PossiblyMissingCallSites {
			if i == 20 {
				fmt.Fprintf(w, "  %d more call sites; use --json\n", len(out.PossiblyMissingCallSites)-20)
				break
			}
			fmt.Fprintf(w, "  %s %s:%d %s\n", site.Repository, site.File, site.Span.StartLine, *site.Name)
		}
	}
	for i, pc := range out.PossibleCallers {
		if i == 20 {
			fmt.Fprintf(w, "  %d more possible callers; use --json\n", len(out.PossibleCallers)-20)
			break
		}
		fmt.Fprintf(w, "  %s %s may be called by %s %s (%s)\n", pc.Route.Repository, pc.Route.Identity, pc.Source.Repository, pc.Source.Identity, possibleCallerNote(pc.PossibleCaller))
	}
}

// possibleCallerNote words why a possible caller is not a link: the
// ambiguity with the repositories of its listed candidates, or the
// outcome's own reason (the providers without a stored index).
func possibleCallerNote(pc projectlink.PossibleCaller) string {
	if pc.Status == projectlink.StatusAmbiguous {
		return fmt.Sprintf("%s; %d candidates in %s", pc.Wording, pc.CandidateCount, strings.Join(pc.CandidateRepositories, ", "))
	}
	if pc.Reason != "" {
		return pc.Wording + "; " + pc.Reason
	}
	return pc.Wording
}
