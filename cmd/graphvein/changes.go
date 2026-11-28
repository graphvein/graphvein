package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/graphvein/graphvein/internal/changes"
	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/indexrun"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/server"
	"github.com/graphvein/graphvein/internal/store"
)

const changesUsage = `usage:
  graphvein changes [path] [--base previous|run:ID|pin:NAME|ref:BASE[..HEAD]] [--group G] [--change C] [--cause C] [--kind K] [--tests] [--raw] [--json]
  graphvein changes impact ID [path] [--base B] [--depth N] [--json]
  graphvein changes pin NAME [path] [--run ID]
  graphvein changes unpin NAME [path]
  graphvein changes --project DIR [--base previous|pass:N] [--json]
  graphvein changes impact ID --project DIR [--repository MEMBER] [--base B] [--depth N] [--json]`

// changesCommand is graphvein changes (ADR 0056 "CLI"). Reads open the
// store like the other query commands; pin, unpin and indexing a ref
// snapshot take the index lock.
func changesCommand(args []string, stdout, stderr io.Writer) int {
	sub := ""
	if len(args) > 0 && (args[0] == "impact" || args[0] == "pin" || args[0] == "unpin") {
		sub, args = args[0], args[1:]
	}
	f := flag.NewFlagSet("changes", flag.ContinueOnError)
	f.SetOutput(stderr)
	repo := f.String("repository", "", "logical repository ID (defaults to directory name)")
	baseFlag := f.String("base", "previous", "comparison base: previous, run:ID, pin:NAME or ref:BASE[..HEAD]")
	jsonOutput := f.Bool("json", false, "emit stable JSON")
	var filter changes.Filter
	f.StringVar(&filter.Group, "group", "", "only items of this group")
	f.StringVar(&filter.Change, "change", "", "only items of this change kind")
	f.StringVar(&filter.Cause, "cause", "", "only items of this cause")
	f.StringVar(&filter.Kind, "kind", "", "only items of this record kind")
	f.BoolVar(&filter.IncludeTests, "tests", false, "include test-scope items")
	f.BoolVar(&filter.Raw, "raw", false, "include both stored payloads of every item")
	depth := f.Int("depth", changes.DefaultDepth, fmt.Sprintf("impact traversal depth (1 to %d)", changes.MaxDepth))
	run := f.String("run", "", "journal run to pin (default: the newest)")
	projectArg := f.String("project", "", "project manifest (graphvein-project.yaml) or its directory")
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
			boolean := name == "json" || name == "tests" || name == "raw" || name == "h" || name == "help"
			if !boolean && !strings.Contains(a, "=") && i+1 < len(args) {
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
	if *projectArg != "" {
		explicit := map[string]bool{}
		f.Visit(func(fl *flag.Flag) { explicit[fl.Name] = true })
		return projectChangesCommand(sub, positional, *projectArg, *repo, *baseFlag, *depth, *jsonOutput, explicit, stdout, stderr)
	}
	want := 0
	if sub != "" {
		want = 1
	}
	if len(positional) < want || len(positional) > want+1 {
		fmt.Fprintln(stderr, changesUsage)
		return 2
	}
	root := "."
	if len(positional) == want+1 {
		root = positional[want]
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	var override *string
	f.Visit(func(fl *flag.Flag) {
		if fl.Name == "repository" {
			override = repo
		}
	})
	if *repo, err = config.ResolveRepository(abs, override); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	ctx := context.Background()
	switch sub {
	case "pin", "unpin":
		cfg, err := config.Load(abs)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		err = withLock(ctx, abs, *repo, func(lease store.IndexLease) error {
			s, err := openStore(ctx, abs)
			if err != nil {
				return err
			}
			defer s.Close()
			if sub == "unpin" {
				return changes.UnpinName(ctx, s, *repo, positional[0])
			}
			p, err := changes.PinRun(ctx, s, *repo, *run, positional[0], cfg.Changes.MaxPins)
			if err == nil {
				fmt.Fprintf(stdout, "Pinned run %s as %s\n", p.Run.RunID, p.Name)
			}
			return err
		})
		if err != nil {
			return reportChangesError(stderr, err)
		}
		if sub == "unpin" {
			fmt.Fprintf(stdout, "Unpinned %s\n", positional[0])
		}
		return 0
	}
	base, err := changes.ParseBase(*baseFlag)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if base.Kind == changes.BaseRef {
		if code := indexRefs(ctx, abs, *repo, base, stderr); code != 0 {
			return code
		}
	}
	s, err := openReadStore(ctx, abs)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer s.Close()
	cmp, err := changes.Compare(ctx, s, *repo, base, changes.Options{Root: abs, ExtractorVersion: index.Version, Schema: server.ChangeSchema{}})
	if err != nil {
		return reportChangesError(stderr, err)
	}
	if *depth < 1 || *depth > changes.MaxDepth {
		fmt.Fprintf(stderr, "--depth must be 1 to %d\n", changes.MaxDepth)
		return 2
	}
	report := changes.Report{Repository: cmp.Repository, Base: cmp.Base.String(), Summary: cmp.Summary}
	if sub == "impact" {
		r, err := changes.Impact(ctx, s, cmp, changes.ImpactRequest{ID: positional[0], Depth: *depth})
		if err != nil {
			return reportChangesError(stderr, err)
		}
		report.Items = []changes.Item{}
		report.Impacts = []changes.NamedImpact{{Seed: positional[0], Impact: r}}
	} else if report, err = changes.BuildReport(ctx, s, cmp, filter, nil); err != nil {
		return reportChangesError(stderr, err)
	}
	if *jsonOutput {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	}
	printChanges(stdout, report)
	return 0
}

// withLock runs fn under the index lock shared with graphvein index and
// serve.
func withLock(ctx context.Context, root, repo string, fn func(store.IndexLease) error) error {
	locker, lockName := openLocker(root, repo)
	defer locker.Close()
	lease, err := locker.Acquire(ctx, indexrun.LockWait)
	if errors.Is(err, store.ErrIndexLocked) {
		return fmt.Errorf("another index run holds the lock for this store (%s): %w", lockName, err)
	}
	if err != nil {
		return fmt.Errorf("taking the index lock (%s): %w", lockName, err)
	}
	defer lease.Release()
	return fn(lease)
}

// indexRefs indexes the commits of a ref base that have no snapshot yet.
func indexRefs(ctx context.Context, root, repo string, base changes.Base, stderr io.Writer) int {
	s, err := openStore(ctx, root)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	indexed, err := changes.IndexedCommit(ctx, s, repo)
	if err != nil {
		s.Close()
		return reportChangesError(stderr, err)
	}
	refs, err := changes.ResolveRefs(ctx, root, base, indexed)
	if err != nil {
		s.Close()
		return reportChangesError(stderr, err)
	}
	snapshots, err := s.RefSnapshots(ctx, repo)
	s.Close()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	have := map[string]bool{}
	for _, r := range snapshots {
		if changes.SnapshotUsable(r, index.Version) {
			have[r.Commit] = true
		}
	}
	var missing []string
	for _, c := range []string{refs.Base, refs.Head} {
		if c != "" && !have[c] {
			missing = append(missing, c)
		}
	}
	if len(missing) == 0 {
		return 0
	}
	err = withLock(ctx, root, repo, func(lease store.IndexLease) error {
		for _, commit := range missing {
			fmt.Fprintf(stderr, "Indexing ref snapshot %s\n", commit)
			_, err := indexrun.RunRef(ctx, indexrun.RefOptions{Commit: commit, Options: indexrun.Options{
				Root: root, Repository: repo, Lease: lease,
				OpenStore:   func(ctx context.Context) (store.Store, error) { return openStore(ctx, root) },
				SnapshotDir: storeDir(root),
				StoreName:   storeName(root),
				Parser:      parse.Options{Timeout: indexrun.DefaultTimeout},
				MaxFileSize: indexrun.DefaultMaxFileSize,
			}})
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return reportChangesError(stderr, err)
	}
	return 0
}

func reportChangesError(stderr io.Writer, err error) int {
	fmt.Fprintln(stderr, err)
	return 1
}

// printChanges is the text form: both sides, the counts per group and one
// line per item.
func printChanges(w io.Writer, r changes.Report) {
	s := r.Summary
	side := func(label string, info changes.SideInfo) {
		fmt.Fprintf(w, "%s: %s %s", label, info.Kind, info.Run)
		if info.Pin != "" {
			fmt.Fprintf(w, " pin %s", info.Pin)
		}
		if info.Commit != "" {
			fmt.Fprintf(w, " commit %s", info.Commit)
		}
		fmt.Fprintf(w, " %s/%s", info.Extractor.Name, info.Extractor.Version)
		if info.Partial {
			fmt.Fprintf(w, " partial (%d gap paths)", len(info.Gaps))
		}
		fmt.Fprintln(w)
	}
	side("Base", s.Base)
	side("Head", s.Head)
	if s.ExtractorChanged {
		fmt.Fprintln(w, "Extractor changed between the sides: unchanged files may show extractor_or_relink changes")
	}
	for _, g := range s.Groups {
		if g.Items == 0 && g.Moves == 0 && g.OtherChanges == 0 {
			continue
		}
		fmt.Fprintf(w, "%s: %d items, %d moves, %d other changes\n", g.Group, g.Items, g.Moves, g.OtherChanges)
	}
	for _, name := range s.NotExtracted {
		fmt.Fprintf(w, "%s: not extracted yet\n", name)
	}
	for _, it := range r.Items {
		cause := ""
		if it.Cause != nil {
			cause = " (" + *it.Cause + ")"
		}
		reason := ""
		if it.Reason != nil {
			reason = " [" + *it.Reason + "]"
		}
		fmt.Fprintf(w, "%s %s %s %s%s%s\n", it.Row, it.Change, it.Kind, it.ID(), reason, cause)
	}
	if r.Hidden > 0 {
		fmt.Fprintf(w, "%d test-scope items hidden; use --tests\n", r.Hidden)
	}
	for _, im := range r.Impacts {
		fmt.Fprintf(w, "Impact of %s (depth %d)\n", im.Seed, im.Impact.Depth)
		for _, p := range im.Impact.Paths {
			conf := "none (inferred by its relation; every step is a fact)"
			if p.Confidence != nil {
				conf = contract.ConfidenceText(*p.Confidence)
			}
			node := p.Node
			if p.Repository != "" {
				node = p.Repository + " " + p.Node
			}
			stale := ""
			if p.Stale {
				stale = " stale"
			}
			fmt.Fprintf(w, "  %d %s %s %s: %s, %s confidence=%s%s%s\n", p.Distance, p.Relation, node, p.Claim, p.Wording, p.Provenance, conf, accessNote(p), stale)
			for _, s := range p.Steps {
				if s.Assumption == "" {
					continue
				}
				fmt.Fprintf(w, "    via %s %s %s -> %s %s: %s\n", s.Kind, s.SourceRepository, s.Source, s.TargetRepository, s.Target, s.Assumption)
				if len(s.StaleReasons) > 0 {
					fmt.Fprintf(w, "    stale: %s\n", strings.Join(s.StaleReasons, "; "))
				}
			}
		}
		for _, pd := range im.Impact.PossibleDependents {
			fmt.Fprintf(w, "  %d %s %s %s %s: %s, %s (not a link: %s; via route %s %s)\n", pd.Distance, pd.Relation, pd.Repository, pd.Node, pd.Claim, pd.Wording, pd.Provenance, pd.Reason, pd.RouteRepository, pd.Route)
		}
		for _, u := range im.Impact.TableUses {
			fmt.Fprintf(w, "  %s: %d %s (a lower bound)\n", u.Table, len(u.Found), u.Wording)
			for _, m := range u.MayAlsoBeUsedBy {
				fmt.Fprintf(w, "    %s %s (%s line %d: %s)\n", u.MayAlsoWording, m.Owner, m.Row, m.Line, m.Reason)
			}
		}
		u := im.Impact.Unknowns
		across := ""
		if im.Impact.Project != "" {
			across = fmt.Sprintf(", %d possible dependents in other repositories (ambiguous or incomplete, not links)", len(im.Impact.PossibleDependents))
		}
		fmt.Fprintf(w, "  Unknowns: %d blind spots, %d near misses, %d nodes in gap files%s\n", len(u.BlindSpots), len(u.NearMisses), len(u.GapNodes), across)
		for _, ne := range u.NotExtracted {
			fmt.Fprintf(w, "  %s: not extracted yet\n", ne.Relation)
		}
		for _, m := range im.Impact.Members {
			u := m.Unknowns
			fmt.Fprintf(w, "  %s unknowns: %d blind spots, %d near misses, %d nodes in gap files\n", m.Repository, len(u.BlindSpots), len(u.NearMisses), len(u.GapNodes))
			if len(m.LinkSourcesMissing) > 0 {
				fmt.Fprintf(w, "  %s: %d project link sources no longer stored (stale links, not followed)\n", m.Repository, len(m.LinkSourcesMissing))
			}
		}
		if len(im.Impact.Caps) > 0 {
			fmt.Fprintf(w, "  Caps reached: %s\n", strings.Join(im.Impact.Caps, ", "))
		}
	}
}

// accessNote shows an I8 path's table_access entry (T019 Revision 4): its
// verification, assumptions, and that a column's own use is not extracted.
func accessNote(p changes.Path) string {
	n := len(p.Steps)
	if n == 0 || p.Steps[n-1].Access == nil {
		return ""
	}
	a := p.Steps[n-1].Access
	parts := []string{a.VerificationWording}
	if len(a.Assumes) > 0 {
		parts = append(parts, "assumes "+strings.Join(a.Assumes, ", "))
	}
	if a.ColumnUse != nil {
		parts = append(parts, "column use "+strings.ReplaceAll(*a.ColumnUse, "_", " "))
	}
	return " [" + strings.Join(parts, "; ") + "]"
}
