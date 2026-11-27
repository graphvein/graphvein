package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/indexrun"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/projectlink"
	"github.com/graphvein/graphvein/internal/store"
)

// staleLinksShown bounds the stale links graphvein status --project lists
// one by one; the rest are counted.
const staleLinksShown = 20

// passAttempts is how often index --project runs the link pass when a
// member is published while the pass reads (Postgres only: there the
// project lock does not exclude member index runs).
const passAttempts = 3

// outcomeStatuses is the order outcome counts are printed in: the claim
// contract's statuses, linked first.
var outcomeStatuses = []string{
	projectlink.StatusLinked, projectlink.StatusAmbiguous, projectlink.StatusUnmatched,
	projectlink.StatusIncomplete, projectlink.StatusUnproven, projectlink.StatusDynamic,
	projectlink.StatusTestOnly, projectlink.StatusInRepository, projectlink.StatusUnknown,
}

// hasProjectFlag reports whether args select project mode (--project).
func hasProjectFlag(args []string) bool {
	for _, a := range args {
		if a == "--" {
			return false
		}
		name := strings.SplitN(strings.TrimLeft(a, "-"), "=", 2)[0]
		if strings.HasPrefix(a, "-") && name == "project" {
			return true
		}
	}
	return false
}

// projectDir resolves the --project argument: the manifest file itself or
// the directory holding it.
func projectDir(arg string) (string, error) {
	abs, err := filepath.Abs(arg)
	if err != nil {
		return "", fmt.Errorf("resolving %s: %w", arg, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("project manifest %s: %w", arg, err)
	}
	if info.IsDir() {
		return abs, nil
	}
	if filepath.Base(abs) != config.ProjectFile {
		return "", fmt.Errorf("--project names %s; give the directory holding %s or that file", arg, config.ProjectFile)
	}
	return filepath.Dir(abs), nil
}

// openProjectLocker returns the project lock of the project store (ADR
// 0075 "Locks"): a Postgres advisory lock keyed by schema and project, or
// for SQLite the store's own index lock.
func openProjectLocker(dir, project string) (store.IndexLocker, string) {
	return commandLine(dir).projectLocker(project)
}

// projectIndexCommand is graphvein index --project and status --project
// (ADR 0075, T022 B4). Single-repository
// flags (--repository, --emit, --indexer) are refused: every member keeps
// the repository the manifest names and is indexed by the built-in indexer.
func projectIndexCommand(command string, args []string, stdout, stderr io.Writer, emit indexrun.EmitFunc) int {
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(stderr)
	manifest := flags.String("project", "", "project manifest (graphvein-project.yaml) or its directory")
	workers := flags.Int("workers", 0, "parser workers (default min(4, CPUs))")
	timeout := flags.Duration("timeout", indexrun.DefaultTimeout, "base per-file parsing timeout, scaled with file size up to 12 times (ADR 0090)")
	maxSize := flags.Int64("max-file-size", indexrun.DefaultMaxFileSize, "maximum file size to parse")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *manifest == "" {
		fmt.Fprintf(stderr, "usage: graphvein %s --project <manifest or its directory>; members come from the manifest, so no path, --repository, --emit or --indexer\n", command)
		return 2
	}
	if *workers < 0 || *timeout <= 0 || *maxSize <= 0 {
		fmt.Fprintln(stderr, "workers must be nonnegative; timeout and max-file-size must be positive")
		return 2
	}
	dir, err := projectDir(*manifest)
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
	if command == "status" {
		return projectStatus(ctx, project, stdout, stderr)
	}
	return projectIndex(ctx, project, parse.Options{Workers: *workers, Timeout: *timeout}, *maxSize, emit, stdout, stderr)
}

// memberIndex is what index --project did with one member.
type memberIndex struct {
	summary string
	run     *store.Run
	err     error
}

// projectIndexOutcome is what one project index did: each member checkout
// indexed (results), then the link pass.
type projectIndexOutcome struct {
	results     map[string]memberIndex
	indexErrors map[string]string
	set         store.ProjectLinkSet
	run         store.ProjectLinkRun
	passErr     error
}

// runProjectIndex indexes every member with a checkout into the project
// store, each under its own index lock (released before the next member
// and before the project lock is taken), then runs the link pass under the
// project lock. A member that fails is recorded and the others go on; the
// pass then links it from its previous stored index, if any, and stores
// the failure so its links show as stale. progress, when set, receives
// each member's phases and, with an empty repository, the link pass.
// graphvein index --project and a re-index from serve --project share it.
func runProjectIndex(ctx context.Context, at storeAt, project config.Project, parser parse.Options, maxSize int64, emit indexrun.EmitFunc, progress func(string, indexrun.Progress)) projectIndexOutcome {
	out := projectIndexOutcome{results: map[string]memberIndex{}, indexErrors: map[string]string{}}
	for _, m := range project.Members {
		if !m.Checkout {
			continue
		}
		var report func(indexrun.Progress)
		if progress != nil {
			report = func(p indexrun.Progress) { progress(m.Repository, p) }
		}
		r := indexProjectMember(ctx, at, m, parser, maxSize, emit, report)
		out.results[m.Repository] = r
		if r.err != nil {
			out.indexErrors[m.Repository] = r.err.Error()
		}
	}
	if progress != nil {
		progress("", indexrun.Progress{})
	}
	out.set, out.run, out.passErr = linkProject(ctx, at, project, out.indexErrors)
	return out
}

// runProjectLinkPass runs only the link pass over the members' stored
// indexes: what a manifest edit that changed no member needs (T022 B10).
// No member is indexed, so none is in the outcome's results.
func runProjectLinkPass(ctx context.Context, at storeAt, project config.Project, progress func(string, indexrun.Progress)) projectIndexOutcome {
	out := projectIndexOutcome{results: map[string]memberIndex{}, indexErrors: map[string]string{}}
	if progress != nil {
		progress("", indexrun.Progress{})
	}
	out.set, out.run, out.passErr = linkProject(ctx, at, project, out.indexErrors)
	return out
}

// projectIndex is graphvein index --project: runProjectIndex, then its
// report.
func projectIndex(ctx context.Context, project config.Project, parser parse.Options, maxSize int64, emit indexrun.EmitFunc, stdout, stderr io.Writer) int {
	// The project store is located like a repository's, keyed by the
	// manifest folder (ADR 0093).
	loc, err := locate(project.Dir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintln(stdout, indexLine(loc, "project", project.Name))
	defer printLines(stdout, indexTrailer(loc, "graphvein serve --project "+quoteArg(loc.Folder)))
	outcome := runProjectIndex(ctx, commandLine(project.Dir).pinned(loc), project, parser, maxSize, emit, nil)
	results, set, run, passErr := outcome.results, outcome.set, outcome.run, outcome.passErr
	failed := len(outcome.indexErrors) > 0 || passErr != nil
	stored := map[string]string{}
	for _, m := range set.Members {
		stored[m.Repository] = m.Run
	}

	fmt.Fprintf(stdout, "Project %s: %d members\n", project.Name, len(project.Members))
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	for _, m := range project.Members {
		r, indexed := results[m.Repository]
		run, inStore := stored[m.Repository]
		var line string
		switch {
		case indexed && r.err == nil:
			line = "indexed: " + r.summary
		case indexed && passErr == nil && run != "":
			line = fmt.Sprintf("index failed: %v; linked from its previous index, its links are stale", r.err)
		case indexed:
			line = fmt.Sprintf("index failed: %v", r.err)
		case passErr != nil:
			line = "no checkout here"
		case inStore && run != "":
			line = "no checkout here; linked from the store"
		default:
			line = "no checkout here and not indexed: not linked; calls that could reach it are incomplete"
		}
		fmt.Fprintf(w, "  %s\t%s\t%s\n", m.Repository, displayPath(project.Dir, m), line)
	}
	w.Flush()
	if passErr != nil {
		fmt.Fprintf(stdout, "Link pass failed: %v\n", passErr)
		fmt.Fprintf(stderr, "link pass of project %s failed: %v\n", project.Name, passErr)
		return 1
	}
	unchanged := len(run.Links) - run.Added - run.Changed
	fmt.Fprintf(stdout, "Link pass %d: %d links (added %d, removed %d, changed %d, unchanged %d)\n", run.Seq, len(run.Links), run.Added, run.Removed, run.Changed, unchanged)
	fmt.Fprintf(stdout, "Outcomes: %s\n", outcomeCounts(set.Outcomes))
	if failed {
		fmt.Fprintln(stderr, "some members failed to index; see above")
		return 1
	}
	return 0
}

// indexProjectMember indexes one member checkout into the project store
// under the member's own index lock, as graphvein index would; the fact
// cache is the member repository's own in the project store.
func indexProjectMember(ctx context.Context, at storeAt, m config.Member, parser parse.Options, maxSize int64, emit indexrun.EmitFunc, progress func(indexrun.Progress)) memberIndex {
	locker, lockName := at.locker(m.Repository)
	defer locker.Close()
	lease, err := locker.Acquire(ctx, indexrun.LockWait)
	if errors.Is(err, store.ErrIndexLocked) {
		return memberIndex{err: fmt.Errorf("another index run holds the lock for this store (%s)", lockName)}
	}
	if err != nil {
		return memberIndex{err: fmt.Errorf("taking the index lock (%s): %w", lockName, err)}
	}
	defer lease.Release()
	result, err := indexrun.Run(ctx, indexrun.Options{
		Root:        m.Path,
		Repository:  m.Repository,
		Lease:       lease,
		OpenStore:   at.openWriter,
		Parser:      parser,
		MaxFileSize: maxSize,
		Emit:        emit,
		Progress:    progress,
		SnapshotDir: at.dir(),
		StoreName:   at.name(),
	})
	if err != nil {
		return memberIndex{err: err}
	}
	s := result.Summary
	line := fmt.Sprintf("files %d, nodes %d, edges %d, diagnostics %d (parsed %d, reused %d)", s.Files, s.Nodes, s.Edges, s.Diagnostics, s.Parsed, s.Reused)
	for _, l := range parseLines(s) {
		line += "; " + l
	}
	return memberIndex{summary: line, run: &result.Run}
}

// linkProject runs the link pass under the project lock and returns the
// stored set and journal entry.
func linkProject(ctx context.Context, at storeAt, project config.Project, indexErrors map[string]string) (store.ProjectLinkSet, store.ProjectLinkRun, error) {
	locker, lockName := at.projectLocker(project.Name)
	defer locker.Close()
	lease, err := locker.Acquire(ctx, indexrun.LockWait)
	if errors.Is(err, store.ErrIndexLocked) {
		return store.ProjectLinkSet{}, store.ProjectLinkRun{}, fmt.Errorf("another run holds the project lock (%s); run graphvein index --project again when it has finished", lockName)
	}
	if err != nil {
		return store.ProjectLinkSet{}, store.ProjectLinkRun{}, fmt.Errorf("taking the project lock (%s): %w", lockName, err)
	}
	defer lease.Release()
	s, err := at.openWriter(ctx)
	if err != nil {
		return store.ProjectLinkSet{}, store.ProjectLinkRun{}, err
	}
	defer s.Close()
	hash := projectlink.ManifestHash(project)
	for attempt := 1; ; attempt++ {
		if err := lease.Check(ctx); err != nil {
			return store.ProjectLinkSet{}, store.ProjectLinkRun{}, fmt.Errorf("project lock lost: %w", err)
		}
		result, run, err := projectlink.RunWithIndexErrors(ctx, s, project, hash, indexErrors)
		if errors.Is(err, store.ErrProjectStale) && attempt < passAttempts {
			continue
		}
		return result.Set, run, err
	}
}

// outcomeCounts prints the number of sources per outcome status, in
// outcomeStatuses order, leaving out statuses no source has.
func outcomeCounts(outcomes []store.ProjectOutcome) string {
	counts := map[string]int{}
	for _, o := range outcomes {
		counts[o.Status]++
	}
	parts := []string{}
	for _, status := range outcomeStatuses {
		if counts[status] > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", strings.ReplaceAll(status, "_", " "), counts[status]))
		}
		delete(counts, status)
	}
	for status, n := range counts {
		parts = append(parts, fmt.Sprintf("%s %d", status, n))
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ", ")
}

// memberSettingsWords words a member's settings comparison for its status
// row (row ST-47): whether the graphvein.yaml settings its last index run
// recorded still match its checkout's file. Like graphvein status, it never
// says unchanged without the run's record.
func memberSettingsWords(ctx context.Context, s store.Store, m config.Member) (string, error) {
	runs, err := s.Runs(ctx, m.Repository)
	if err != nil {
		return "", fmt.Errorf("reading the runs of %s: %w", m.Repository, err)
	}
	record := ""
	if last := store.LatestRun(runs); last != nil {
		record = last.Settings
	}
	c := config.IndexSettingsAt(record, m.Path)
	switch c.State {
	case config.IndexSettingsUnchanged:
		return "settings unchanged since its last index", nil
	case config.IndexSettingsChanged:
		return "settings changed since its last index: " + strings.Join(c.ChangedKeys, ", ") + " (re-index to apply)", nil
	}
	return "settings since its last index unknown: " + c.Reason, nil
}

// projectStatus prints graphvein status --project: each member's index
// freshness and its state against the last link pass, the link pass state
// and every stale link. Freshness is computed from stored runs (ADR 0075
// "Storage"); nothing is written.
func projectStatus(ctx context.Context, project config.Project, stdout, stderr io.Writer) int {
	s, err := openStore(ctx, project.Dir)
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
	latest, set, passed := view.Latest, view.Set, view.Stored
	inPass := map[string]store.ProjectMember{}
	for _, m := range set.Members {
		inPass[m.Repository] = m
	}

	fmt.Fprintf(stdout, "Project %s: %d members\n", project.Name, len(project.Members))
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	for _, m := range project.Members {
		current := latest[m.Repository]
		var state []string
		switch {
		case current == "" && !m.Checkout:
			state = append(state, "not indexed and no checkout here: not linked; calls that could reach it are incomplete")
		case current == "":
			state = append(state, "not indexed: not linked; calls that could reach it are incomplete")
		case !m.Checkout:
			state = append(state, "indexed; no checkout here, linked from the store")
		default:
			changed, err := index.Freshness(ctx, m.Path, m.Repository, s)
			if err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
			if len(changed) == 0 {
				state = append(state, "indexed; up to date")
			} else {
				state = append(state, fmt.Sprintf("indexed; %d files changed since its last index", len(changed)))
			}
			words, err := memberSettingsWords(ctx, s, m)
			if err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
			state = append(state, words)
		}
		if current != "" && !m.Checkout {
			// The comparison needs the member's graphvein.yaml (row ST-47).
			state = append(state, "settings since its last index unknown: no checkout here")
		}
		if passed {
			pm, ok := inPass[m.Repository]
			switch {
			case !ok:
				state = append(state, "not in the last link pass")
			case pm.IndexError != "":
				state = append(state, "failed to index before the last link pass: "+pm.IndexError)
			case pm.Run != current && pm.Run == "":
				state = append(state, "indexed since the last link pass")
			case pm.Run != current:
				state = append(state, "indexed again since the last link pass")
			}
		}
		fmt.Fprintf(w, "  %s\t%s\t%s\n", m.Repository, displayPath(project.Dir, m), strings.Join(state, "; "))
	}
	w.Flush()
	if err := printOrphans(ctx, s, project, latest, stdout); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if !passed {
		fmt.Fprintln(stdout, "Link pass: none yet; run graphvein index --project")
		return 0
	}
	passReasons := view.PassReasons()
	if len(passReasons) == 0 {
		fmt.Fprintf(stdout, "Link pass %d: fresh\n", set.Pass)
	} else {
		fmt.Fprintf(stdout, "Link pass %d: stale (%s); run graphvein index --project\n", set.Pass, strings.Join(passReasons, "; "))
	}

	var staleLinks []string
	for _, l := range set.Links {
		if why := view.StaleReasons(l); len(why) > 0 {
			staleLinks = append(staleLinks, fmt.Sprintf("  %s %s %s -> %s %s: %s", l.Kind, l.Source.Repository, l.Source.Identity, l.Target.Repository, l.Target.Identity, strings.Join(why, "; ")))
		}
	}
	fmt.Fprintf(stdout, "Links: %d (%d fresh, %d stale)\n", len(set.Links), len(set.Links)-len(staleLinks), len(staleLinks))
	fmt.Fprintf(stdout, "Outcomes: %s\n", outcomeCounts(set.Outcomes))
	if len(staleLinks) > 0 {
		fmt.Fprintln(stdout, "Stale links:")
		for i, line := range staleLinks {
			if i == staleLinksShown {
				fmt.Fprintf(stdout, "  ... and %d more\n", len(staleLinks)-staleLinksShown)
				break
			}
			fmt.Fprintln(stdout, line)
		}
	}
	return 0
}

// printOrphans lists the stored repositories a kept link pass of the
// project named as members but the manifest no longer does (a member
// renamed or removed on disk, T022 review): their stored index stays
// until graphvein project forget removes it.
func printOrphans(ctx context.Context, s store.Store, project config.Project, latest map[string]string, stdout io.Writer) error {
	runs, err := s.ProjectLinkRuns(ctx, project.Name)
	if err != nil {
		return fmt.Errorf("reading the link journal of project %s: %w", project.Name, err)
	}
	current := map[string]bool{}
	for _, m := range project.Members {
		current[m.Repository] = true
	}
	seen := map[string]bool{}
	var orphans []string
	for i := len(runs) - 1; i >= 0; i-- {
		for _, m := range runs[i].Members {
			if !current[m.Repository] && !seen[m.Repository] && latest[m.Repository] != "" {
				seen[m.Repository] = true
				orphans = append(orphans, m.Repository)
			}
		}
	}
	sort.Strings(orphans)
	for _, name := range orphans {
		fmt.Fprintf(stdout, "Stored index of %s: a past link pass named it as a member, the manifest no longer does (renamed or removed); remove it with graphvein project forget %s\n", name, name)
	}
	return nil
}
