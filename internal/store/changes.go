package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
)

// Defaults of the change journal limits (ADR 0056, changes.* in
// graphvein.yaml).
const (
	DefaultJournalRuns     = 30
	DefaultJournalBudget   = 1.0
	DefaultMaxPins         = 5
	DefaultMaxRefSnapshots = 4
	// MaxGapPaths caps the knowledge-gap paths a run records; beyond it the
	// run records GapsTruncated and the whole run counts as partial.
	MaxGapPaths = 10000
)

// GapWarningCodes are the warning codes that also make a file a knowledge
// gap: syntax_error is the built-in indexer's report that tree-sitter
// recovered from invalid syntax, so the facts extracted from the file come
// from a recovered tree and the absence of a fact there is not proven
// (T019a2 decision).
var GapWarningCodes = []string{"syntax_error"}

// GapDiagnostic reports whether d makes its path (or, without a file, the
// whole run) a knowledge gap: error severity, or a GapWarningCodes warning.
func GapDiagnostic(d contract.Diagnostic) bool {
	if d.Severity == "error" {
		return true
	}
	for _, code := range GapWarningCodes {
		if d.Code == code {
			return true
		}
	}
	return false
}

// RefMarker separates a repository identifier from the commit in a ref
// namespace. Configured repository identifiers may not contain it, so a
// namespace can never collide with a repository (ADR 0056).
const RefMarker = "@ref:"

// Record names of journal entries and change sets.
const (
	RecordFile = "file"
	RecordNode = "node"
	RecordEdge = "edge"
)

// Errors of the journal, pin and ref methods. Callers test them with
// errors.Is to answer the API's error codes.
var (
	// ErrBaseExpired: the journal or overlay no longer covers the base.
	ErrBaseExpired = errors.New("comparison base is no longer covered by the change journal")
	// ErrBaseUnknown: no run, pin or ref snapshot of that name exists.
	ErrBaseUnknown = errors.New("comparison base is unknown")
	// ErrNoJournal: the repository has no journal run yet (indexed only
	// before migration 0004); one re-index starts the journal.
	ErrNoJournal  = errors.New("repository has no change journal yet")
	ErrPinExists  = errors.New("a pin of that name exists")
	ErrPinLimit   = errors.New("pin limit reached")
	ErrPinUnknown = errors.New("pin is unknown")
	ErrRefUnknown = errors.New("ref snapshot is unknown")
	// ErrReservedRepository: a repository identifier contains RefMarker
	// outside a ref run.
	ErrReservedRepository = errors.New("repository identifiers containing " + RefMarker + " are reserved for ref snapshots")
)

// JournalLimits bounds the change journal of one repository. Retention keeps
// the newest Runs journal runs and at most Budget times the current graph's
// payload bytes of entries, whichever keeps fewer runs, but always the newest
// run.
type JournalLimits struct {
	Runs   int
	Budget float64
}

// WithDefaults fills zero fields with the defaults.
func (l JournalLimits) WithDefaults() JournalLimits {
	if l.Runs <= 0 {
		l.Runs = DefaultJournalRuns
	}
	if l.Budget <= 0 {
		l.Budget = DefaultJournalBudget
	}
	return l
}

// Namespace describes a ref run. The run's header repository must be
// RefNamespace(Repository, Commit).
type Namespace struct {
	// Repository is the main repository the ref belongs to.
	Repository string
	// Commit is the full commit id that was materialised.
	Commit string
	// ConfigSource names where graphvein.yaml came from: "ref" (committed in
	// the ref), "working_tree" (the ref had none) or "none".
	ConfigSource string
	// MaxSnapshots bounds the ref snapshots of Repository; the least
	// recently used beyond it are dropped at commit. Zero means
	// DefaultMaxRefSnapshots.
	MaxSnapshots int
}

// RefNamespace is the reserved repository identifier holding the graph of
// commit in repository.
func RefNamespace(repository, commit string) string {
	return repository + RefMarker + commit
}

// ReservedRepository reports whether id is reserved for ref namespaces.
func ReservedRepository(id string) bool { return strings.Contains(id, RefMarker) }

// EdgeKey is the journal key of an edge: the JSON array [kind, source,
// target]. ADR 0056 sketched NUL separators, which Postgres text cannot
// hold; a JSON array is unambiguous on both backends.
func EdgeKey(kind, source, target string) string {
	data, _ := json.Marshal([]string{kind, source, target})
	return string(data)
}

// ParseEdgeKey splits an EdgeKey.
func ParseEdgeKey(key string) (kind, source, target string, err error) {
	var parts []string
	if err := json.Unmarshal([]byte(key), &parts); err != nil || len(parts) != 3 {
		return "", "", "", fmt.Errorf("invalid edge key %q", key)
	}
	return parts[0], parts[1], parts[2], nil
}

// ChangeCounts counts one record type's net changes in a run.
type ChangeCounts struct {
	Added   int `json:"added"`
	Changed int `json:"changed"`
	Removed int `json:"removed"`
}

// ChangeRun is one journal run: what a comparison needs to know about the
// run itself. For a ref snapshot the same shape describes the ref run (Seq 0,
// no counts).
type ChangeRun struct {
	Seq   int64  `json:"seq"`
	RunID string `json:"run_id"`
	// PreviousRun is the index run whose state this run's entries start
	// from; empty for the repository's first run (Initial), whose entries
	// are not written because no earlier state exists to compare with.
	PreviousRun string  `json:"previous_run,omitempty"`
	Initial     bool    `json:"initial,omitempty"`
	Indexer     Indexer `json:"indexer"`
	Commit      string  `json:"commit,omitempty"`
	Dirty       *bool   `json:"dirty,omitempty"`
	FinishedAt  string  `json:"finished_at"`
	// Counts holds net changes per record name (RecordFile, ...).
	Counts map[string]ChangeCounts `json:"counts,omitempty"`
	// EntryBytes is what the run's entries count against the journal
	// budget: record, key, before payload and after hash bytes.
	EntryBytes int64 `json:"entry_bytes"`
	// Gaps are the knowledge gaps: paths (files or directories) with an
	// error-severity diagnostic, sorted, at most MaxGapPaths.
	Gaps          []string `json:"gaps"`
	GapsTruncated bool     `json:"gaps_truncated"`
	// GapsFileless is set when an error diagnostic named no file: the whole
	// side is partial.
	GapsFileless bool `json:"gaps_fileless"`
	// GapsUnknown is set for a base run older than the journal (the run the
	// oldest kept run started from): its gaps were never recorded, so the
	// whole side is partial.
	GapsUnknown bool `json:"gaps_unknown,omitempty"`
}

// Indexer is the producer of a run (contract.Indexer), repeated here so the
// journal payload does not depend on the contract package's JSON shape.
type Indexer struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Pin is a pinned baseline: a journal run kept comparable by an overlay of
// the payloads its records had at the pin.
type Pin struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Seq       int64     `json:"seq"`
	CreatedAt string    `json:"created_at"`
	Run       ChangeRun `json:"run"`
}

// RefSnapshot is one indexed commit in its namespace. It is a comparison
// input named by commit; nothing attaches to it (ADR 0056, AGENTS.md).
type RefSnapshot struct {
	Namespace    string    `json:"namespace"`
	Commit       string    `json:"commit"`
	Extractor    string    `json:"extractor"`
	CreatedAt    string    `json:"created_at"`
	LastUsedAt   string    `json:"last_used_at"`
	ConfigSource string    `json:"config_source"`
	Run          ChangeRun `json:"run"`
}

// ChangeBase selects the base of a comparison: exactly one of Run, Pin or
// Ref. Run and Pin bases compare with the repository's current graph; a Ref
// base compares with Head, another namespace, or with the current graph when
// Head is empty.
type ChangeBase struct {
	// Run is an index run id the journal still covers.
	Run string
	// Pin is a pin name.
	Pin string
	// Ref and Head are ref namespaces (RefNamespace) of the repository.
	Ref  string
	Head string
}

// Side kinds of a ChangeSet.
const (
	SideRun     = "run"
	SidePin     = "pin"
	SideRef     = "ref"
	SideCurrent = "current"
)

// ChangeSide is one side of a comparison.
type ChangeSide struct {
	Kind string `json:"kind"`
	// Repository holds the side's graph: the repository itself for run, pin
	// and current sides, a namespace for ref sides. A run or pin base has no
	// graph of its own; its state is the head graph plus the journal.
	Repository string `json:"repository"`
	// Pin names the pin of a pin side.
	Pin string `json:"pin,omitempty"`
	// Run describes the run the side's state is from.
	Run ChangeRun `json:"run"`
}

// RecordChange is one key whose raw stored payload differs between the
// sides. Nil Before or After means the record is absent on that side.
type RecordChange struct {
	Record string          `json:"record"`
	Key    string          `json:"key"`
	Before json.RawMessage `json:"before"`
	After  json.RawMessage `json:"after"`
}

// ChangeSet is the raw difference between two sides, sorted by record and
// key.
type ChangeSet struct {
	Base    ChangeSide     `json:"base"`
	Head    ChangeSide     `json:"head"`
	Changes []RecordChange `json:"changes"`
}

// WithRunOptions wraps s so every BeginRun also applies extra: its set
// fields (Commit, Dirty, Journal limits, Namespace) override the caller's,
// and PreserveFactCache is kept when either asks for it. indexrun uses it to
// add run metadata to the transaction ingest begins, which knows nothing of
// git or journal settings.
func WithRunOptions(s Store, extra RunOptions) Store {
	return &optionStore{Store: s, extra: extra}
}

type optionStore struct {
	Store
	extra RunOptions
}

func (s *optionStore) BeginRun(ctx context.Context, h contract.Header, options ...RunOptions) (Transaction, error) {
	var o RunOptions
	if len(options) > 0 {
		o = options[0]
	}
	o.PreserveFactCache = o.PreserveFactCache || s.extra.PreserveFactCache
	if s.extra.Commit != "" {
		o.Commit = s.extra.Commit
	}
	if s.extra.Dirty != nil {
		o.Dirty = s.extra.Dirty
	}
	if s.extra.Journal.Runs != 0 {
		o.Journal.Runs = s.extra.Journal.Runs
	}
	if s.extra.Journal.Budget != 0 {
		o.Journal.Budget = s.extra.Journal.Budget
	}
	if s.extra.Namespace != nil {
		o.Namespace = s.extra.Namespace
	}
	if s.extra.Settings != "" {
		o.Settings = s.extra.Settings
	}
	return s.Store.BeginRun(ctx, h, o)
}
