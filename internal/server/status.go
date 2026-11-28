package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/store"
)

// maxChangedFiles caps the listed paths so a repository with every file
// changed (or never indexed) still answers with a small body; the count
// stays exact.
const maxChangedFiles = 1000

// Freshness states of HTTP API v0. Clients treat any other value as unknown,
// never as fresh.
const (
	stateFresh      = "fresh"
	stateStale      = "stale"
	stateNotIndexed = "not_indexed"
	stateUnknown    = "unknown"
)

// statusResponse is the body of GET /api/v0/status. Field order is the
// documented order (docs/cli/serve.md).
type statusResponse struct {
	APIVersion    int             `json:"api_version"`
	Repository    string          `json:"repository"`
	ServerVersion string          `json:"server_version"`
	Extractor     extractorStatus `json:"extractor"`
	Index         indexStatus     `json:"index"`
	Freshness     freshnessStatus `json:"freshness"`
}

type extractorStatus struct {
	Current string  `json:"current"`
	Indexed *string `json:"indexed"`
	Matches *bool   `json:"matches"`
}

type indexStatus struct {
	Indexed bool     `json:"indexed"`
	LastRun *lastRun `json:"last_run"`
	// ParseFailed counts the files the stored run could not parse
	// (parse_failed diagnostics, T074): every count of the index is
	// incomplete while it is nonzero. Omitted when zero.
	ParseFailed int `json:"parse_failed,omitempty"`
}

// codeParseFailed is the diagnostic of a file whose parse failed, a timeout
// included once the serial retry also timed out.
const codeParseFailed = "parse_failed"

// parseFailedFiles counts the distinct files of repo's stored parse_failed
// diagnostics (T074), the files that make a run's counts incomplete.
func parseFailedFiles(ctx context.Context, s store.Store, repo string) (int, error) {
	diagnostics, err := s.DiagnosticsWithCodes(ctx, repo, []string{codeParseFailed}, nil)
	if err != nil {
		return 0, fmt.Errorf("reading parse_failed diagnostics: %w", err)
	}
	seen := map[string]bool{}
	for _, d := range diagnostics {
		seen[d.File] = true
	}
	return len(seen), nil
}

type lastRun struct {
	FinishedAt      string           `json:"finished_at"`
	Indexer         contract.Indexer `json:"indexer"`
	ContractVersion int              `json:"contract_version"`
	Commit          string           `json:"commit,omitempty"`
}

type freshnessStatus struct {
	State                 string   `json:"state"`
	CheckedAt             string   `json:"checked_at"`
	ChangedCount          *int     `json:"changed_count"`
	ChangedFiles          []string `json:"changed_files"`
	ChangedFilesTruncated bool     `json:"changed_files_truncated"`
	Error                 string   `json:"error,omitempty"`
	// Settings compares the settings the last run read with the current
	// graphvein.yaml (rows ST-43..ST-46); set by /api/v0/status and the
	// project members, omitted by the change endpoints.
	Settings *settingsStatus `json:"settings,omitempty"`
}

// statusHandler answers GET /api/v0/status. Every request runs its own
// freshness calculation; nothing is cached, so "fresh" always describes the
// files as they were after the request arrived and its check started.
type statusHandler struct {
	opt Options
	// slot serializes calculations: polls from several tabs queue instead
	// of walking the repository in parallel.
	slot chan struct{}
}

func newStatusHandler(opt Options) *statusHandler {
	return &statusHandler{opt: opt, slot: make(chan struct{}, 1)}
}

func (h *statusHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	select {
	case h.slot <- struct{}{}:
	case <-ctx.Done():
		cancelled(w, ctx)
		return
	}
	body, err := h.compute(ctx)
	<-h.slot
	if ctx.Err() != nil {
		// The walk stopped early, so its result says nothing.
		cancelled(w, ctx)
		return
	}
	if err != nil {
		logFailure(h.opt.logger(), r, err)
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	if h.opt.onStatus != nil {
		h.opt.onStatus(h.opt.Repository, h.opt.checkedPath(), body.Freshness)
	}
	writeJSON(w, http.StatusOK, body)
}

// cancelled answers a request whose context ended. A stopping server says
// so with 503; a client that left gets nothing, because nobody reads it.
func cancelled(w http.ResponseWriter, ctx context.Context) {
	if errors.Is(context.Cause(ctx), errStopping) {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "the server is stopping; the request was cancelled")
	}
}

// compute builds the status body with the same freshness calculation as
// graphvein status. Only a failure to read runs is an error; a failed
// freshness check is reported as state unknown.
//
// checked_at is the instant the check starts, taken before the walk: a file
// edited while the walk runs may be missed, so "fresh as of checked_at" is
// only true for an instant no later than the scan's start.
func (h *statusHandler) compute(ctx context.Context) (statusResponse, error) {
	opt := h.opt
	checkedAt := opt.now()
	changed, freshErr := opt.freshness(ctx)
	runs, err := opt.Store.Runs(ctx, opt.Repository)
	if err != nil {
		return statusResponse{}, fmt.Errorf("reading runs: %w", err)
	}
	last := latestRun(runs)
	out := statusResponse{
		APIVersion:    0,
		Repository:    opt.Repository,
		ServerVersion: opt.Version,
		Extractor:     extractorFor(index.Version, last),
		Index:         indexStatus{Indexed: len(runs) > 0, LastRun: last},
		Freshness:     freshnessFor(changed, freshErr, len(runs) > 0),
	}
	if len(runs) > 0 {
		if out.Index.ParseFailed, err = parseFailedFiles(ctx, opt.Store, opt.Repository); err != nil {
			return statusResponse{}, err
		}
	}
	out.Freshness.CheckedAt = checkedAt.UTC().Format(time.RFC3339Nano)
	out.Freshness.Settings = opt.statusSettings(runs)
	return out, nil
}

// freshnessFor applies the state table of docs/cli/serve.md. Fresh is
// produced only when runs exist, the check succeeded and nothing changed.
func freshnessFor(changed []string, err error, indexed bool) freshnessStatus {
	if err != nil {
		return freshnessStatus{State: stateUnknown, ChangedFiles: []string{}, Error: err.Error()}
	}
	count := len(changed)
	out := freshnessStatus{ChangedCount: &count, ChangedFiles: changed}
	if out.ChangedFiles == nil {
		out.ChangedFiles = []string{}
	}
	if count > maxChangedFiles {
		out.ChangedFiles = changed[:maxChangedFiles]
		out.ChangedFilesTruncated = true
	}
	switch {
	case !indexed:
		out.State = stateNotIndexed
	case count > 0:
		out.State = stateStale
	default:
		out.State = stateFresh
	}
	return out
}

// latestRun picks the run with the greatest finish time. Run IDs are random
// and RFC3339Nano trims trailing zeros, so neither store order nor string
// comparison finds the latest run; parsed times do. Runs whose finish time
// does not parse are skipped; nil means no usable run.
func latestRun(runs []store.Run) *lastRun {
	best := latestStoreRun(runs)
	if best == nil {
		return nil
	}
	bestAt, _ := time.Parse(time.RFC3339Nano, best.FinishedAt)
	return &lastRun{
		FinishedAt:      bestAt.UTC().Format(time.RFC3339Nano),
		Indexer:         best.Header.Indexer,
		ContractVersion: best.Header.ContractVersion,
		Commit:          best.Header.Commit,
	}
}

// extractorFor compares this binary's extractor version with the one that
// produced the last run. Runs from an external indexer carry that
// indexer's own version, so they are not comparable: indexed and matches
// are null then.
func extractorFor(current string, last *lastRun) extractorStatus {
	out := extractorStatus{Current: current}
	if last == nil || last.Indexer.Name != "graphvein" {
		return out
	}
	indexed := last.Indexer.Version
	matches := indexed == current
	out.Indexed, out.Matches = &indexed, &matches
	return out
}
