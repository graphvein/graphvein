// Package store is the persistence boundary for graph facts and run metadata.
package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/graphvein/graphvein/internal/contract"
)

// ErrInvalid identifies a recoverable record error rather than a storage failure.
var ErrInvalid = errors.New("invalid graph record")

// Direction keeps incoming and outgoing neighbour queries unambiguous.
type Direction string

// Incoming and Outgoing select the edge endpoint being queried.
const (
	Incoming Direction = "incoming"
	Outgoing Direction = "outgoing"
)

// Query composes the minimal filters needed by M1; nil Level means all levels.
type Query struct {
	Identity string
	File     string
	Kind     string
	Level    *int
	// Term matches exact names or a case-sensitive qualified-name suffix.
	Term string
	// Limit is applied after name matching; zero means unlimited.
	Limit int
	// Attribute, when set, keeps only nodes whose attributes hold this key
	// (any value, null included). A view that needs the few nodes carrying
	// a rare attribute (the architecture overview's table_access_unknown
	// owners) would otherwise decode every node of a kind; backends filter
	// the payload text in SQL before decoding and check the key exactly in
	// Go.
	Attribute string
	// Contains, when set, keeps the nodes whose name or qualified name
	// holds it as a substring under ASCII case folding (FoldASCII): the
	// jump-to search matches names this way (T040, ADR 0089). Backends
	// prefilter the name columns in SQL and apply ContainsFolded in Go.
	Contains string
}

// FoldASCII maps the ASCII letters A-Z to a-z and keeps every other byte.
// Search folds only ASCII so that SQLite's and Postgres's lower() on the
// name columns are both supersets of the Go test: Unicode folding (the
// Kelvin sign folds to k) would match names the SQL prefilter skips.
func FoldASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if 'A' <= c && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}

// ContainsFolded reports whether text holds query under FoldASCII.
func ContainsFolded(text, query string) bool {
	return strings.Contains(FoldASCII(text), FoldASCII(query))
}

// Run preserves producer and source metadata without putting commits in identity.
type Run struct {
	ID         string          `json:"id"`
	Header     contract.Header `json:"header"`
	StartedAt  string          `json:"started_at"`
	FinishedAt string          `json:"finished_at"`
	// Settings is the record of the graphvein.yaml keys the run read (the
	// index inputs, ADR 0088), stored in its own column beside the
	// payload; empty when the run recorded none (a run from before schema
	// 6, an external indexer, an ingest): those settings are unknown.
	// Runs fills it; the store never interprets it.
	Settings string `json:"-"`
}

// Snapshot provides a deterministic projection suitable for backend parity tests.
type Snapshot struct {
	Files       []contract.File       `json:"files"`
	Nodes       []contract.Node       `json:"nodes"`
	Edges       []contract.Edge       `json:"edges"`
	Diagnostics []contract.Diagnostic `json:"diagnostics"`
}

// Transaction stages one repository snapshot so invalid framing cannot erase facts.
type Transaction interface {
	UpsertFile(context.Context, contract.File) error
	UpsertNode(context.Context, contract.Node) error
	UpsertEdge(context.Context, contract.Edge) error
	DeleteFile(context.Context, string) error
	Diagnostic(context.Context, contract.Diagnostic) error
	Commit(context.Context) error
	Rollback() error
	Writes() Writes
	// Run is the run this transaction records. After a successful Commit it
	// holds the same ID and finish time as the committed index_runs row, so
	// a caller can name the run it just published without reading it back.
	Run() Run
}

// RunOptions configures one run transaction. The zero value is a normal run
// of the built-in indexer with the default journal limits.
type RunOptions struct {
	// PreserveFactCache keeps external snapshots from changing the built-in
	// fact cache.
	PreserveFactCache bool
	// Commit and Dirty describe the tree the run indexed, when the caller
	// knows it (graphvein index and the server re-index read them from git).
	// They are journal metadata only: nothing attaches to the commit, and
	// Header.Commit, when an indexer set it, takes precedence over Commit.
	// A nil Dirty means unknown.
	Commit string
	Dirty  *bool
	// Journal bounds the change journal (ADR 0056); zero fields take the
	// defaults.
	Journal JournalLimits
	// Namespace marks a ref run: the snapshot of one commit indexed into
	// its own reserved repository identifier. A ref run writes no journal
	// and registers itself as a ref snapshot of Namespace.Repository.
	Namespace *Namespace
	// Settings is the record of the index inputs the run read (see
	// Run.Settings); empty means unknown.
	Settings string
}

// Writes counts changed graph rows, excluding run metadata and diagnostics.
type Writes struct{ Files, Nodes, Edges, Deleted int }

// Store keeps application code independent of SQL dialects and driver internals.
type Store interface {
	BeginRun(context.Context, contract.Header, ...RunOptions) (Transaction, error)
	Nodes(context.Context, string, Query) ([]contract.Node, error)
	// NodesWithoutModelClaims reads the nodes a Query selects, in the same
	// order as Nodes, but a model record (attribute source "model") comes
	// without its claims attribute, removed by the backend before decoding.
	// A list view that never shows claims (/schema/sets) would otherwise
	// spend most of its time decoding them (ADR 0057).
	NodesWithoutModelClaims(ctx context.Context, repository string, q Query) ([]contract.Node, error)
	Neighbours(context.Context, string, string, string, Direction) ([]contract.Node, error)
	Edges(context.Context, string, []string, []string, Direction) ([]contract.Edge, error)
	// EdgesOfKind reads every edge of kind in the repository, ordered as
	// Edges orders them. A view that counts one edge kind across the whole
	// index (/api/v0/precision counts code calls, ADR 0073) would otherwise
	// read every node to ask Edges for their outgoing edges.
	EdgesOfKind(ctx context.Context, repository, kind string) ([]contract.Edge, error)
	Dump(context.Context, string) (Snapshot, error)
	// FileHashes maps each stored file path to its content hash. Freshness
	// needs only these hashes, so it avoids decoding the whole graph.
	FileHashes(ctx context.Context, repository string) (map[string]string, error)
	// NodesByIdentity reads the given nodes in identity order, once each,
	// omitting identities that are not stored. Traversals use it to fetch
	// each level's endpoints instead of loading every node.
	NodesByIdentity(ctx context.Context, repository string, identities []string) ([]contract.Node, error)
	// Files reads every stored file record in path order. A view that
	// counts files by language and precision label (/api/v0/precision,
	// ADR 0066) needs the records without decoding the graph through Dump.
	Files(ctx context.Context, repository string) ([]contract.File, error)
	// FilesByPath reads the stored file records of the given paths in path
	// order, once each, omitting paths that are not stored. A view of one
	// node's source needs its file's language and label (ADR 0066).
	FilesByPath(ctx context.Context, repository string, paths []string) ([]contract.File, error)
	Runs(context.Context, string) ([]Run, error)
	// Diagnostics returns the stored diagnostics whose file is one of files,
	// in stored (ordinal) order. Views that explain one file's facts need its
	// diagnostics without decoding the whole repository through Dump.
	Diagnostics(ctx context.Context, repository string, files []string) ([]contract.Diagnostic, error)
	// DiagnosticsWithoutFile returns, in stored order, the diagnostics whose
	// file is not a stored file path: the scanner's diagnostics on paths it
	// did not read (symlinks, unreadable files and directories, unreadable
	// ignore files) and diagnostics with an empty file. A view that states
	// that something is absent from the index needs them to prove the scan
	// missed nothing, and cannot name their paths in advance (ADR 0048).
	DiagnosticsWithoutFile(ctx context.Context, repository string) ([]contract.Diagnostic, error)
	// DiagnosticsWithCodes returns, in stored order, the diagnostics whose
	// code is one of codes or starts with one of prefixes, on any file. A
	// view that needs a few codes across thousands of files (the schema
	// model rules read the Python files' schema_ and unread diagnostics)
	// would otherwise decode every diagnostic of those files per request.
	DiagnosticsWithCodes(ctx context.Context, repository string, codes, prefixes []string) ([]contract.Diagnostic, error)
	// DiagnosticsWithMessage returns, in stored order, the diagnostics of
	// code whose message contains text, on any file. A view that reads one
	// kind of marker among a code many linkers use (the GORM v1 markers
	// among the schema_model_unread diagnostics) would otherwise decode
	// them all per request.
	DiagnosticsWithMessage(ctx context.Context, repository, code, text string) ([]contract.Diagnostic, error)
	// FactFiles returns the paths a stored fact names: the file of every
	// node that is not a file node, and every file an edge's evidence
	// names. The couldn't-read page (ADR 0089) proves "no fact stored from
	// this file" with it, without decoding every node through Dump.
	FactFiles(ctx context.Context, repository string) (map[string]bool, error)
	Close() error
	FactCache(context.Context, string, string, string, string) ([]byte, error)
	SaveFactCache(context.Context, string, string, string, string, []byte) error

	// ChangeRuns lists the journal runs still kept for repository, oldest
	// first (ADR 0056).
	ChangeRuns(ctx context.Context, repository string) ([]ChangeRun, error)
	// ChangeSet composes, in one read transaction, every record whose raw
	// stored payload differs between base and head, with both payloads. It
	// is the raw layer: internal/changes applies the claim contract to it.
	ChangeSet(ctx context.Context, repository string, base ChangeBase) (ChangeSet, error)
	// Pins lists the pinned baselines of repository by name.
	Pins(ctx context.Context, repository string) ([]Pin, error)
	// Pin pins the journal run seq under name, keeping at most limit pins
	// (zero means DefaultMaxPins). The caller holds the index lock.
	Pin(ctx context.Context, repository string, seq int64, name string, limit int) (Pin, error)
	// Unpin removes the pin id and its overlay. The caller holds the index
	// lock.
	Unpin(ctx context.Context, repository, id string) error
	// RefSnapshots lists the ref snapshots of repository by namespace.
	RefSnapshots(ctx context.Context, repository string) ([]RefSnapshot, error)
	// DropRefSnapshot deletes one ref snapshot and everything stored under
	// its namespace. The caller holds the index lock.
	DropRefSnapshot(ctx context.Context, repository, namespace string) error

	// ProjectStore keeps multi-repository project links (ADR 0075).
	ProjectStore
}

// ErrIndexLocked reports that another holder has the index lock. Callers
// test for it with errors.Is to tell "someone else is indexing" from a lock
// that could not be opened or probed at all (ADR 0052).
var ErrIndexLocked = errors.New("index lock held by another run")

// LockState is the state of the index lock as one locker can prove it.
type LockState string

// Lock states. LockHeldHere means the locker that answered holds a lease;
// LockHeldElsewhere means another locker, in this process or another one,
// holds it.
const (
	LockFree          LockState = "free"
	LockHeldHere      LockState = "held_here"
	LockHeldElsewhere LockState = "held_elsewhere"
)

// IndexLocker serializes index runs on one store across processes, so
// graphvein index and graphvein serve never scan, parse and write the same
// store at the same time (ADR 0052). Implementations live in the backend
// packages; a lock is released by the operating system or the database
// when its holder's process or session ends, so a crash leaves no stale
// lock.
type IndexLocker interface {
	// Acquire takes the lock, waiting at most wait; it returns an error
	// wrapping ErrIndexLocked when another holder kept it for the whole
	// wait. Any other error means the lock could not be taken or probed.
	Acquire(ctx context.Context, wait time.Duration) (IndexLease, error)
	// State reports whether the lock is held, by this locker or another
	// holder, without taking it for longer than a probe needs. An error
	// means the state is unknown.
	State(ctx context.Context) (LockState, error)
	// Close releases resources the locker keeps for probes, and a lease it
	// still holds.
	Close() error
}

// IndexLease is a held index lock.
type IndexLease interface {
	// Check returns an error when the lock can no longer be proven held
	// (Postgres: the lock session is gone; SQLite: the lock file was
	// removed or replaced). A run checks it right before its commit.
	Check(ctx context.Context) error
	// Release gives the lock up. It is safe to call more than once.
	Release() error
}
