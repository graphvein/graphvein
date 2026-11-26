package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/graphvein/graphvein/internal/changes"
	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/projectlink"
	"github.com/graphvein/graphvein/internal/server"
	"github.com/graphvein/graphvein/internal/store"
)

// projectChangesReport is graphvein changes --project: which project
// links the link journal says were added, removed or changed between two
// link passes, with each member's own comparison against its previous
// index (ADR 0075 "Query, impact and change journal", T022 B7).
type projectChangesReport struct {
	Project string `json:"project"`
	Base    string `json:"base"`
	// State is what the API's GET /api/v0/project/links/changes answers
	// for the same base: "compared" when Links holds a diff, "same_pass"
	// when the base is the newest pass (nothing compared, which is never
	// "no change") and "no_comparison" when fewer than two passes are kept.
	State string                   `json:"state"`
	Links *changes.ProjectLinkDiff `json:"links"`
	// LinksNote says why Links is null (state same_pass or no_comparison).
	LinksNote string          `json:"links_note,omitempty"`
	Members   []memberChanges `json:"members"`
}

// memberChanges is one member's comparison against its previous index,
// as counts per group; graphvein changes --repository MEMBER lists the
// items.
type memberChanges struct {
	Repository string               `json:"repository"`
	Head       string               `json:"head_run,omitempty"`
	Base       string               `json:"base_run,omitempty"`
	Groups     []changes.GroupCount `json:"groups"`
	Error      string               `json:"error,omitempty"`
}

// projectChangesCommand is graphvein changes and changes impact with
// --project. Pin, unpin and ref bases stay per repository: project links
// name the members' current indexes.
func projectChangesCommand(sub string, positional []string, manifest, repository, baseFlag string, depth int, jsonOutput bool, explicit map[string]bool, stdout, stderr io.Writer) int {
	if sub == "pin" || sub == "unpin" {
		fmt.Fprintf(stderr, "graphvein changes %s works on one repository; run it in the member's checkout without --project\n", sub)
		return 2
	}
	want := 0
	if sub == "impact" {
		want = 1
	}
	if len(positional) != want {
		fmt.Fprintln(stderr, changesUsage+"\nwith --project, members come from the manifest, so no path")
		return 2
	}
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
	view, err := projectlink.ReadView(ctx, s, project)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	var names []string
	for _, m := range project.Members {
		names = append(names, m.Repository)
	}
	if sub == "" {
		if explicit["repository"] || explicit["depth"] || explicit["group"] || explicit["change"] || explicit["cause"] || explicit["kind"] || explicit["tests"] || explicit["raw"] {
			fmt.Fprintln(stderr, "graphvein changes --project reports links and member counts only; list a member's items with graphvein changes in its checkout")
			return 2
		}
		return projectLinkChanges(ctx, s, project, baseFlag, jsonOutput, stdout, stderr)
	}

	member, ok := config.Member{}, false
	switch {
	case repository != "":
		for _, m := range project.Members {
			if m.Repository == repository {
				member, ok = m, true
			}
		}
		if !ok {
			fmt.Fprintf(stderr, "repository %s is not a member of project %s (members: %s)\n", repository, project.Name, strings.Join(names, ", "))
			return 2
		}
	case len(project.Members) == 1:
		member = project.Members[0]
	default:
		fmt.Fprintf(stderr, "name the member whose change ID this is with --repository (members: %s)\n", strings.Join(names, ", "))
		return 2
	}
	base, err := changes.ParseBase(baseFlag)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if base.Kind == changes.BaseRef {
		fmt.Fprintln(stderr, changes.ErrProjectRef)
		return 2
	}
	if depth < 1 || depth > changes.MaxDepth {
		fmt.Fprintf(stderr, "--depth must be 1 to %d\n", changes.MaxDepth)
		return 2
	}
	root := ""
	if member.Checkout {
		root = member.Path
	}
	cmp, err := changes.Compare(ctx, s, member.Repository, base, changes.Options{Root: root, ExtractorVersion: index.Version, Schema: server.ChangeSchema{}})
	if err != nil {
		return reportChangesError(stderr, err)
	}
	scope := &changes.ProjectScope{Project: project.Name, Members: names, Links: view}
	r, err := changes.Impact(ctx, s, cmp, changes.ImpactRequest{ID: positional[0], Depth: depth, Project: scope})
	if err != nil {
		return reportChangesError(stderr, err)
	}
	report := changes.Report{Repository: cmp.Repository, Base: cmp.Base.String(), Summary: cmp.Summary, Items: []changes.Item{},
		Impacts: []changes.NamedImpact{{Seed: positional[0], Impact: r}}}
	if jsonOutput {
		return encodeJSON(stdout, stderr, report)
	}
	printLinkPass(stdout, view)
	printChanges(stdout, report)
	return 0
}

// projectLinkChanges prints the link journal diff between the newest pass
// and the base pass (previous: the pass before it; pass:N) and each
// member's counts against its previous index.
func projectLinkChanges(ctx context.Context, s store.Store, project config.Project, baseFlag string, jsonOutput bool, stdout, stderr io.Writer) int {
	runs, err := s.ProjectLinkRuns(ctx, project.Name)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	report := projectChangesReport{Project: project.Name, Base: baseFlag, State: "no_comparison", Members: []memberChanges{}}
	var from int64
	switch {
	case baseFlag == "" || baseFlag == changes.BasePrevious:
		report.Base = changes.BasePrevious
		if len(runs) >= 2 {
			from = runs[len(runs)-2].Seq
		}
	case strings.HasPrefix(baseFlag, "pass:"):
		n, err := strconv.ParseInt(strings.TrimPrefix(baseFlag, "pass:"), 10, 64)
		if err != nil || n <= 0 {
			fmt.Fprintf(stderr, "base %q: with --project, want previous or pass:N\n", baseFlag)
			return 2
		}
		from = n
	default:
		fmt.Fprintf(stderr, "base %q: with --project, want previous or pass:N\n", baseFlag)
		return 2
	}
	switch {
	case len(runs) == 0:
		report.LinksNote = "no link pass yet; run graphvein index --project"
	case from == 0:
		report.LinksNote = "only one link pass is kept; nothing to compare it with"
	case from == runs[len(runs)-1].Seq:
		// Comparing the newest pass with itself would list no change,
		// which is not proof that no link changed (the API's same_pass).
		report.State = "same_pass"
		report.LinksNote = fmt.Sprintf("the base is pass %d, the newest kept link pass: same pass, nothing compared, not proof that no link changed; use base previous or an older pass", from)
	default:
		diff, err := changes.DiffProjectLinks(project.Name, runs, from, runs[len(runs)-1].Seq)
		if errors.Is(err, changes.ErrPassUnknown) {
			fmt.Fprintf(stderr, "%v (kept: %d to %d)\n", err, runs[0].Seq, runs[len(runs)-1].Seq)
			return 1
		}
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		report.State, report.Links = "compared", &diff
	}
	for _, m := range project.Members {
		mc := memberChanges{Repository: m.Repository, Groups: []changes.GroupCount{}}
		root := ""
		if m.Checkout {
			root = m.Path
		}
		cmp, err := changes.Compare(ctx, s, m.Repository, changes.Base{Kind: changes.BasePrevious}, changes.Options{Root: root, ExtractorVersion: index.Version, Schema: server.ChangeSchema{}})
		if err != nil {
			// One member's comparison failing (not indexed, a single
			// run) stops none of the others.
			mc.Error = err.Error()
		} else {
			mc.Head, mc.Base = cmp.Summary.Head.Run, cmp.Summary.Base.Run
			for _, g := range cmp.Summary.Groups {
				if g.Items > 0 || g.Moves > 0 || g.OtherChanges > 0 {
					mc.Groups = append(mc.Groups, g)
				}
			}
		}
		report.Members = append(report.Members, mc)
	}
	if jsonOutput {
		return encodeJSON(stdout, stderr, report)
	}
	fmt.Fprintf(stdout, "Project %s\n", report.Project)
	if report.Links == nil {
		fmt.Fprintf(stdout, "Links: %s\n", report.LinksNote)
	} else {
		d := report.Links
		fmt.Fprintf(stdout, "Links between pass %d and pass %d: %d added, %d removed, %d changed", d.From, d.To, len(d.Added), len(d.Removed), len(d.Changed))
		if d.Manifest {
			fmt.Fprint(stdout, " (the manifest's declarations changed)")
		}
		fmt.Fprintln(stdout)
		for _, l := range []struct {
			label string
			keys  []store.ProjectLinkKey
		}{{"added", d.Added}, {"removed", d.Removed}, {"changed", d.Changed}} {
			for _, k := range l.keys {
				fmt.Fprintf(stdout, "  %s %s %s %s -> %s %s\n", l.label, k.Kind, k.SourceRepository, k.SourceIdentity, k.TargetRepository, k.TargetIdentity)
			}
		}
	}
	for _, m := range report.Members {
		if m.Error != "" {
			fmt.Fprintf(stdout, "%s: %s\n", m.Repository, m.Error)
			continue
		}
		fmt.Fprintf(stdout, "%s: run %s against %s\n", m.Repository, m.Head, m.Base)
		for _, g := range m.Groups {
			fmt.Fprintf(stdout, "  %s: %d items, %d moves, %d other changes\n", g.Group, g.Items, g.Moves, g.OtherChanges)
		}
	}
	return 0
}

// printLinkPass states the link pass a project-scoped impact read.
func printLinkPass(w io.Writer, view projectlink.View) {
	switch reasons := view.PassReasons(); {
	case !view.Stored:
		fmt.Fprintln(w, "Link pass: none yet; no cross-repository links followed")
	case len(reasons) == 0:
		fmt.Fprintf(w, "Link pass %d: fresh, %d links\n", view.Set.Pass, len(view.Set.Links))
	default:
		fmt.Fprintf(w, "Link pass %d: stale (%s); stale links are followed and marked\n", view.Set.Pass, strings.Join(reasons, "; "))
	}
}

func encodeJSON(stdout, stderr io.Writer, v any) int {
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
