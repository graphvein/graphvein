package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/graphvein/graphvein/internal/query"
)

// changedShown is how many changed files the envelope lists; the count is
// always exact and the status tool pages the whole list (D6 default).
const changedShown = DefaultLimit

// statusView is the part of /api/v0/status (and of a project member in
// /api/v0/project) the envelope reads.
type statusView struct {
	Extractor struct {
		Current string  `json:"current"`
		Indexed *string `json:"indexed"`
		Matches *bool   `json:"matches"`
	} `json:"extractor"`
	Index struct {
		Indexed bool     `json:"indexed"`
		LastRun *lastRun `json:"last_run"`
	} `json:"index"`
	Freshness freshnessView `json:"freshness"`
}

type lastRun struct {
	FinishedAt string `json:"finished_at"`
	Indexer    struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"indexer"`
	ContractVersion int    `json:"contract_version"`
	Commit          string `json:"commit"`
}

type freshnessView struct {
	State                 string   `json:"state"`
	CheckedAt             string   `json:"checked_at"`
	ChangedCount          *int     `json:"changed_count"`
	ChangedFiles          []string `json:"changed_files"`
	ChangedFilesTruncated bool     `json:"changed_files_truncated"`
	Error                 string   `json:"error"`
	// Settings is whether graphvein.yaml's index inputs changed since the
	// last run (row MC-33, the server's ST-43..ST-46); nil when the view
	// did not send it.
	Settings *settingsView `json:"settings"`
}

// settingsView is freshness.settings of /api/v0/status and of a project
// member (docs/cli/serve.md).
type settingsView struct {
	State       string   `json:"state"`
	ChangedKeys []string `json:"changed_keys"`
	UnknownKeys []string `json:"unknown_keys"`
	Reason      *string  `json:"reason"`
}

type projectView struct {
	Members []struct {
		Repository string `json:"repository"`
		Checkout   bool   `json:"checkout"`
		Extractor  struct {
			Current string `json:"current"`
			Matches *bool  `json:"matches"`
		} `json:"extractor"`
		Index     *lastRun      `json:"index"`
		Freshness freshnessView `json:"freshness"`
	} `json:"members"`
	LinkPass LinkPass `json:"link_pass"`
}

// freshness fills the envelope's freshness for this call (row MC-1, D4):
// the /api/v0/status calculation, or the project view's per-member
// freshness and link pass. force skips the reuse of a recent check (the
// status tool's question is whether the index is current, so it always
// checks, T030 review).
func (s *Server) freshness(ctx context.Context, e *Envelope, force bool) error {
	tagRow("MC-1")
	if s.opt.Project != nil {
		return s.projectFreshness(ctx, e, force)
	}
	raw, reused, err := s.freshView(ctx, "/api/v0/status", force)
	if err != nil {
		return err
	}
	var v statusView
	if err := json.Unmarshal(raw, &v); err != nil {
		return fmt.Errorf("decoding status: %w", err)
	}
	f := freshnessOf(e, v.Freshness)
	f.Index = indexInfo(v.Index.LastRun, v.Extractor.Current, v.Extractor.Matches)
	markReused(&f, reused, s.reuseWindow())
	e.Freshness = f
	e.status = &v
	// The whole list the view served (at most 1,000 files; the count is
	// exact), for queryFresh.
	e.changed = append([]string{}, v.Freshness.ChangedFiles...)
	e.checked = f.State == "fresh" || f.State == "stale"
	return nil
}

// recheck makes sure an answer never calls an index fresh while one of
// its own nodes says its file changed (R1): a node's file_state is read
// from the file as it is now, the envelope's freshness may come from a
// check reused from a moment before. A reused fresh check is replaced by
// a new one; a mismatch that remains (the file changed during this very
// call) is stated as an MC-1 unknown.
func (s *Server) recheck(ctx context.Context, e *Envelope) error {
	if e.Freshness.State != "fresh" || len(e.changedNodes) == 0 {
		return nil
	}
	files := sortedStrings(e.changedNodes)
	if e.Freshness.Reused {
		// The new check's labels replace the reused one's.
		for k := range e.labelSet {
			if strings.HasPrefix(k, "freshness:") || strings.HasPrefix(k, "link_pass:") {
				delete(e.labelSet, k)
			}
		}
		if err := s.freshness(ctx, e, true); err != nil {
			return err
		}
		e.unknown("MC-1", "freshness_rechecked", "a served node's file changed after the reused freshness check: freshness was checked again for this answer", files)
		if e.Freshness.State != "fresh" {
			return nil
		}
	}
	e.unknown("MC-1", "freshness_mismatch", "a served node's file_state says its file changed or is missing while the freshness check says fresh (the file changed during this call): treat these files as stale", files)
	return nil
}

// queryFresh is the call's freshness check as the query package takes it
// (query.Options.Fresh), so find, callers and impact do not scan the
// checkout a second time. A failed check returns nil: the query then runs
// its own scan and reports its own error.
func queryFresh(e *Envelope) *query.Freshness {
	if !e.checked {
		return nil
	}
	return &query.Freshness{Indexed: e.Freshness.State != "not_indexed", Changed: e.changed}
}

// FreshnessReuse is how long a freshness check serves the calls that
// follow it (D4: one check per call batch). A full scan of a large
// repository takes about a second (langflow: 1.1 s), and an agent often
// sends several calls at once; a reused check keeps its own checked_at
// and says reused, so the age is visible.
const FreshnessReuse = 2 * time.Second

// freshView reads the freshness view at path (/api/v0/status or
// /api/v0/project), reusing the last answer when that check started less
// than reuse ago and the latest stored run of every repository it covers
// is still the one it saw (R1: a re-index inside the window is never
// labelled with the old run's freshness). reuse 0 means FreshnessReuse;
// negative never reuses; force checks again in any case.
func (s *Server) freshView(ctx context.Context, path string, force bool) (json.RawMessage, bool, error) {
	reuse := s.reuseWindow()
	s.freshMu.Lock()
	defer s.freshMu.Unlock()
	runs, err := s.latestRuns(ctx)
	if err != nil {
		return nil, false, err
	}
	if !force && reuse > 0 && s.freshRaw != nil && time.Since(s.freshAt) < reuse && runs == s.freshRuns {
		return s.freshRaw, true, nil
	}
	started := time.Now()
	raw, err := s.reader.Read(ctx, path, nil)
	if err != nil {
		s.freshRaw = nil
		return nil, false, err
	}
	s.freshRaw, s.freshAt, s.freshRuns = raw, started, runs
	return raw, false, nil
}

// latestRuns names the latest stored run of the repository (or of every
// project member): its ID and finish time. Reading the runs is cheap next
// to a freshness scan, so a reused check is compared with it every call.
func (s *Server) latestRuns(ctx context.Context) (string, error) {
	repos := []string{s.opt.Repository}
	if s.opt.Project != nil {
		repos = repos[:0]
		for _, m := range s.opt.Project.Members {
			repos = append(repos, m.Repository)
		}
	}
	var b strings.Builder
	for _, repo := range repos {
		runs, err := s.opt.Store.Runs(ctx, repo)
		if err != nil {
			return "", fmt.Errorf("reading the runs of %s: %w", repo, err)
		}
		b.WriteString(repo)
		if len(runs) > 0 {
			last := runs[len(runs)-1]
			fmt.Fprintf(&b, "\x00%d\x00%s\x00%s", len(runs), last.ID, last.FinishedAt)
		}
		b.WriteString("\n")
	}
	return b.String(), nil
}

// reuseWindow is the configured reuse of a freshness check.
func (s *Server) reuseWindow() time.Duration {
	if s.opt.FreshnessReuse == 0 {
		return FreshnessReuse
	}
	return s.opt.FreshnessReuse
}

func markReused(f *Freshness, reused bool, window time.Duration) {
	if !reused {
		return
	}
	f.Reused = true
	note := "reused from a check started less than " + window.String() + " before this call with the same latest run (one check per call batch); checked_at is that check's time"
	if f.Text != "" {
		note = f.Text + "; " + note
	}
	f.Text = note
}

func (s *Server) projectFreshness(ctx context.Context, e *Envelope, force bool) error {
	raw, reused, err := s.freshView(ctx, "/api/v0/project", force)
	if err != nil {
		return err
	}
	var v projectView
	if err := json.Unmarshal(raw, &v); err != nil {
		return fmt.Errorf("decoding project: %w", err)
	}
	f := Freshness{State: "fresh", ChangedFiles: []string{}, Members: []MemberFreshness{}}
	// The project's state is its weakest member's: one stale member makes
	// the project stale, one unknown makes it unknown.
	rank := map[string]int{"fresh": 0, "stale": 1, "not_indexed": 2, "unknown": 3}
	for _, m := range v.Members {
		mf := freshnessOf(e, m.Freshness)
		if mf.ChangedMore > 0 {
			mf.Text = memberChangedText(m.Repository, mf.ChangedMore)
		}
		f.Members = append(f.Members, MemberFreshness{Text: mf.Text, Repository: m.Repository, Checkout: m.Checkout, State: mf.State, ChangedCount: mf.ChangedCount,
			ChangedFiles: mf.ChangedFiles, ChangedMore: mf.ChangedMore, Error: mf.Error, Index: indexInfo(m.Index, m.Extractor.Current, m.Extractor.Matches),
			Settings: m.Freshness.Settings})
		if rank[mf.State] > rank[f.State] {
			f.State = mf.State
		}
		if f.CheckedAt == "" || mf.CheckedAt < f.CheckedAt {
			f.CheckedAt = mf.CheckedAt
		}
	}
	lp := v.LinkPass
	if lp.StaleReasons == nil {
		lp.StaleReasons = []string{}
	}
	f.LinkPass = &lp
	e.label("link_pass", lp.State)
	if lp.State != "fresh" {
		tagRow("MC-18")
	}
	f.Text = "project freshness is the weakest member's; see members and link_pass"
	markReused(&f, reused, s.reuseWindow())
	e.label("freshness", f.State)
	e.Freshness = f
	return nil
}

// freshnessOf maps one freshness block: a state the legend does not know
// is reported as unknown (MC-D), and the changed files are cut to
// changedShown with the exact count kept.
func freshnessOf(e *Envelope, v freshnessView) Freshness {
	f := Freshness{State: v.State, CheckedAt: v.CheckedAt, ChangedCount: v.ChangedCount, ChangedFiles: v.ChangedFiles, Error: v.Error}
	if f.ChangedFiles == nil {
		f.ChangedFiles = []string{}
	}
	if _, ok := legend["freshness"][f.State]; !ok {
		e.notModelled("freshness.state", v.State)
		f.State = "unknown"
	}
	e.label("freshness", f.State)
	if len(f.ChangedFiles) > changedShown {
		f.ChangedFiles = f.ChangedFiles[:changedShown]
	}
	if f.ChangedCount != nil {
		f.ChangedMore = *f.ChangedCount - len(f.ChangedFiles)
	}
	if f.ChangedMore > 0 {
		f.Text = fmt.Sprintf("%d more changed files not shown; the status tool lists them", f.ChangedMore)
	}
	return f
}

// memberChangedText is a project member's note on changed files not
// shown: the project status tool lists members, not their files (R12).
func memberChangedText(repo string, more int) string {
	return fmt.Sprintf("%d more changed files not shown; graphvein status in member %s lists them", more, repo)
}

func indexInfo(r *lastRun, current string, matches *bool) *IndexInfo {
	if r == nil {
		return nil
	}
	out := &IndexInfo{FinishedAt: r.FinishedAt, Indexer: r.Indexer.Name, IndexerVersion: r.Indexer.Version, ContractVersion: r.ContractVersion,
		ExtractorCurrent: current, ExtractorMatches: matches}
	if r.Commit != "" {
		c := r.Commit
		out.Commit = &c
	}
	return out
}
