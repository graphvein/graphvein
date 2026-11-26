package changes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/gitref"
	"github.com/graphvein/graphvein/internal/query"
	"github.com/graphvein/graphvein/internal/store"
)

// Base kinds of a comparison request.
const (
	BasePrevious = "previous"
	BaseRun      = "run"
	BasePin      = "pin"
	BaseRef      = "ref"
)

// Base is a parsed comparison base: "previous", "run:<id>", "pin:<name>"
// or "ref:<base>[..<head>]" (ADR 0056).
type Base struct {
	Kind string
	// Value is the run id or pin name.
	Value string
	// Ref and Head are git ref names of a ref base; an empty Head means
	// the current index (open point O1's default).
	Ref, Head string
}

// ParseBase reads the base parameter of the API and the CLI.
func ParseBase(s string) (Base, error) {
	if s == "" || s == BasePrevious {
		return Base{Kind: BasePrevious}, nil
	}
	kind, value, ok := strings.Cut(s, ":")
	if !ok || value == "" {
		return Base{}, fmt.Errorf("base %q: want previous, run:ID, pin:NAME or ref:BASE[..HEAD]", s)
	}
	switch kind {
	case BaseRun, BasePin:
		return Base{Kind: kind, Value: value}, nil
	case BaseRef:
		ref, headRef, two := strings.Cut(value, "..")
		// "a...b" (git's symmetric difference) is refused: the base is
		// always the merge base of the two (R3-10).
		if ref == "" || two && headRef == "" || strings.HasPrefix(headRef, ".") || strings.Contains(headRef, "..") {
			return Base{}, fmt.Errorf("base %q: want ref:BASE or ref:BASE..HEAD", s)
		}
		return Base{Kind: BaseRef, Ref: ref, Head: headRef}, nil
	}
	return Base{}, fmt.Errorf("base %q: want previous, run:ID, pin:NAME or ref:BASE[..HEAD]", s)
}

// String is the canonical spelling of b.
func (b Base) String() string {
	switch b.Kind {
	case BaseRun, BasePin:
		return b.Kind + ":" + b.Value
	case BaseRef:
		if b.Head != "" {
			return "ref:" + b.Ref + ".." + b.Head
		}
		return "ref:" + b.Ref
	}
	return BasePrevious
}

// Errors Compare returns besides the store's (store.ErrBaseExpired,
// ErrBaseUnknown, ErrNoJournal, ErrRefUnknown) and gitref's
// (ErrGitUnavailable, ErrRefUnknown, ErrObjectsMissing).
var (
	// ErrReindexNeeded: a ref comparison's head is the current index, made
	// by another extractor version than this binary's (G4).
	ErrReindexNeeded = errors.New("the current index was made by another extractor version; re-index first")
	// ErrNoPrevious: the newest journal run is the repository's first, so
	// there is no previous run to compare with.
	ErrNoPrevious = errors.New("the index has only one run: there is no previous run to compare with")
	// ErrCursorInvalid: a cursor that does not decode at all; a client
	// error, not a changed index (Revision 3, R3-10).
	ErrCursorInvalid = errors.New("the cursor does not decode")
	// ErrCursorMismatch: a cursor from another comparison or filter (G6).
	ErrCursorMismatch = errors.New("the cursor belongs to another comparison; the index changed")
)

// ErrNoIndexedCommit: a ref base without a head ref, while the current
// index recorded no commit to take the merge base with (R3-5). It matches
// ErrReindexNeeded.
var ErrNoIndexedCommit error = noIndexedCommit{}

type noIndexedCommit struct{}

func (noIndexedCommit) Error() string {
	return "the current index recorded no git commit; re-index inside the git repository first"
}

// Is lets callers test for ErrReindexNeeded.
func (noIndexedCommit) Is(target error) bool { return target == ErrReindexNeeded }

// RefNotIndexedError reports the commits of a ref base whose snapshots are
// not stored yet; the server's POST refs or the CLI index them first.
type RefNotIndexedError struct {
	Commits []string
}

func (e *RefNotIndexedError) Error() string {
	return fmt.Sprintf("ref snapshot of %s is not indexed yet", strings.Join(e.Commits, ", "))
}

// Unwrap lets callers test for store.ErrRefUnknown.
func (e *RefNotIndexedError) Unwrap() error { return store.ErrRefUnknown }

// SnapshotUsable reports whether a stored ref snapshot can serve a
// comparison made by extractor version (empty: any). A snapshot from
// another extractor version is indexed again before use, so both sides of
// a ref comparison have equal versions by construction (G4, ADR 0056,
// Revision 3 R3-6).
func SnapshotUsable(r store.RefSnapshot, version string) bool {
	return version == "" || r.Run.Indexer.Version == version
}

// RefCommits is a resolved ref base: the base commit (the merge base of
// base and head, open point O2) and the head commit ("" for the current
// index).
type RefCommits struct {
	Base, Head string
	// BaseTip is the commit the base ref names, before the merge base.
	BaseTip string
	// Indexed is the commit the current index recorded, the merge base's
	// other side when there is no head ref; HeadCommitDiffers reports that
	// the working tree's HEAD has moved away from it since (R3-5).
	Indexed           string
	HeadCommitDiffers bool
}

// IndexedCommit is the commit the newest journal run of repository
// recorded: the commit the current index was made from ("" when the run
// recorded none or there is no journal).
func IndexedCommit(ctx context.Context, s store.Store, repository string) (string, error) {
	runs, err := s.ChangeRuns(ctx, repository)
	if err != nil {
		return "", fmt.Errorf("reading the journal of %s: %w", repository, err)
	}
	if len(runs) == 0 {
		return "", nil
	}
	return runs[len(runs)-1].Commit, nil
}

// ResolveRefs resolves a ref base in the git repository at root. With no
// head ref the merge base is taken with indexed, the commit the current
// index recorded (IndexedCommit), not with the live HEAD, which may have
// moved since the index was made (Revision 3, R3-5); an index without a
// recorded commit must be re-indexed first.
func ResolveRefs(ctx context.Context, root string, b Base, indexed string) (RefCommits, error) {
	var out RefCommits
	tip, err := gitref.Resolve(ctx, root, b.Ref)
	if err != nil {
		return out, err
	}
	out.BaseTip = tip
	other := indexed
	if b.Head != "" {
		if out.Head, err = gitref.Resolve(ctx, root, b.Head); err != nil {
			return out, err
		}
		other = out.Head
	} else {
		if indexed == "" {
			return out, fmt.Errorf("resolving %s: %w", b, ErrNoIndexedCommit)
		}
		out.Indexed = indexed
		live, err := gitref.Resolve(ctx, root, "HEAD")
		if err != nil {
			return out, err
		}
		out.HeadCommitDiffers = live != indexed
	}
	if out.Base, err = gitref.MergeBase(ctx, root, tip, other); err != nil {
		return out, err
	}
	return out, nil
}

// Schema supplies the schema rules the classification depends on but does
// not own: the table_list state of each schema group (ADR 0048, ADR 0055)
// and a table's completeness. The server implements it with the same code
// its schema endpoints use, so a change item never disagrees with the
// Schema view. A nil Schema leaves every table list and completeness
// unknown, which only downgrades claims (C8, C9, C11).
type Schema interface {
	// TableLists maps each group key (a migration set's directory or a
	// model group's set) of the graph stored under repository to its
	// table_list value ("complete", "may_be_incomplete", "not_modeled").
	TableLists(ctx context.Context, s store.Store, repository string) (map[string]string, error)
	// TableCompleteness is the completeness of one stored table record
	// ("complete", "complete_in_source", "partial", "unknown").
	TableCompleteness(n contract.Node) string
}

// Options configures Compare.
type Options struct {
	// Root is the repository directory; ref bases resolve their names
	// there.
	Root string
	// ExtractorVersion is this binary's extractor version (index.Version);
	// a ref comparison against a current index made by another version is
	// refused (G4). Empty skips the check.
	ExtractorVersion string
	// Schema supplies table lists and completeness; nil leaves them unknown.
	Schema Schema
}

// SideInfo describes one side of a comparison.
type SideInfo struct {
	Kind       string        `json:"kind"`
	Repository string        `json:"repository"`
	Pin        string        `json:"pin,omitempty"`
	Run        string        `json:"run"`
	FinishedAt string        `json:"finished_at"`
	Commit     string        `json:"commit,omitempty"`
	Dirty      *bool         `json:"dirty"`
	Extractor  store.Indexer `json:"extractor"`
	// Partial is set when the whole side is partial (PartialReasons) or
	// it has knowledge gaps (Gaps).
	Partial        bool     `json:"partial"`
	PartialReasons []string `json:"partial_reasons"`
	Gaps           []string `json:"gaps"`
	GapsTruncated  bool     `json:"gaps_truncated"`
}

// Field is one changed user-level field of an item.
type Field struct {
	Name string `json:"name"`
	// Change is changed, unknown_now (a value became null or unknown) or
	// known_now (the reverse).
	Change string `json:"change"`
	Before any    `json:"before"`
	After  any    `json:"after"`
}

// State is one side of an item: the record's file and span on that side
// and the stored values of the fields the item's row compares.
type State struct {
	File   string         `json:"file,omitempty"`
	Span   *contract.Span `json:"span,omitempty"`
	Fields map[string]any `json:"fields"`
	// IDE is the editor link of the head side, set by the server only when
	// the head is the current index and the file is unchanged since it was
	// indexed (ADR 0050: links are server-built).
	IDE *query.IDELink `json:"ide,omitempty"`
	// PathWithheld marks the side of a route item that is a registration
	// whose path the Go linker withholds (couldn't-read row CR-44): the
	// route is found, another pattern or route may answer its path. Set
	// only from the diagnostics of a side's graph (the head, or a ref
	// base); a reader says "path withheld", else "path not known" for a
	// route without a full path, never "not mounted".
	PathWithheld bool `json:"path_withheld,omitempty"`
}

// Hint is an inferred move hint (C14) pairing a removed and an added node.
type Hint struct {
	Row        string `json:"row"`
	Kind       string `json:"kind"`
	Other      string `json:"other"`
	Basis      string `json:"basis"`
	Provenance string `json:"provenance"`
}

// Item is one classified change. Its JSON shape is the HTTP items shape
// and graphvein changes --json's.
type Item struct {
	Group    string  `json:"group"`
	Change   string  `json:"change"`
	Row      string  `json:"row"`
	Record   string  `json:"record"`
	Identity string  `json:"identity"`
	Kind     string  `json:"kind"`
	Name     string  `json:"name"`
	Before   *State  `json:"before"`
	After    *State  `json:"after"`
	Fields   []Field `json:"fields"`
	// Labels refine a change: edited, verdict_changed and alters_changed
	// (C10), handler_changed (C3).
	Labels []string `json:"labels"`
	// Reason explains a link change or why a claim was downgraded.
	Reason *string `json:"reason"`
	// Wording is G8's wording of a removal.
	Wording    *string  `json:"wording"`
	Cause      *string  `json:"cause"`
	Provenance *string  `json:"provenance"`
	Confidence *float64 `json:"confidence"`
	Test       bool     `json:"test"`
	Hints      []Hint   `json:"hints"`
	// ImpactCount is the number of dependents once Impact ran for this
	// item; null before.
	ImpactCount *int `json:"impact_count"`
	// Raw are both stored payloads (the raw view); filled only on request.
	Raw *Raw `json:"raw,omitempty"`

	footprint []string
	seed      bool
	raw       Raw
	// undecodable marks a D item whose payload did not decode.
	undecodable bool
}

// Raw holds both stored payloads of an item (null = absent).
type Raw struct {
	Before any `json:"before"`
	After  any `json:"after"`
	// Related are the changed records reported with this item rather than
	// as items of their own (Revision 1): the exposes_route edges of a
	// route, the alters edges of a migration.
	Related []RelatedRaw `json:"related,omitempty"`
}

// RelatedRaw is one changed record reported with another item.
type RelatedRaw struct {
	Record string `json:"record"`
	Key    string `json:"key"`
	Before any    `json:"before"`
	After  any    `json:"after"`
}

// GroupCount counts one group's items and what was counted but not listed.
type GroupCount struct {
	Group   string         `json:"group"`
	Items   int            `json:"items"`
	Changes map[string]int `json:"changes"`
	Causes  map[string]int `json:"causes"`
	// Moves are records equal under projection (G2).
	Moves int `json:"moves"`
	// OtherChanges are differences outside the row's user-level fields.
	OtherChanges int `json:"other_changes"`
	// Context counts changed records that are no item of their own but
	// context of others (Revision 1): file nodes (the cause context of the
	// items in their file, C1), and the exposes_route and alters edges of
	// a route or migration that is itself added or removed (C3, C10).
	Context int `json:"context"`
	// HiddenTests are the group's test-scope items (G7).
	HiddenTests int `json:"hidden_tests"`
	// Kinds counts the group's items per record kind (D: per kind).
	Kinds map[string]int `json:"kinds"`
}

// Summary is the summary of a comparison.
type Summary struct {
	ContractVersion  string   `json:"contract_version"`
	Base             SideInfo `json:"base"`
	Head             SideInfo `json:"head"`
	ExtractorChanged bool     `json:"extractor_changed"`
	// HeadCommitDiffers: a ref base without a head ref took its merge base
	// with the commit the current index recorded, and the working tree's
	// HEAD is another commit now (Revision 3, R3-5).
	HeadCommitDiffers bool         `json:"head_commit_differs"`
	Partial           bool         `json:"partial"`
	Groups            []GroupCount `json:"groups"`
	// Locations counts changed L5 location nodes (G2).
	Locations int `json:"locations"`
	// FileMetadataOnly counts file records whose size or git blob id
	// differ with an equal content hash (line-ending conversion).
	FileMetadataOnly int `json:"file_metadata_only"`
	// NotExtracted names groups whose facts are not extracted yet (C13).
	NotExtracted []string `json:"not_extracted"`
	// ExtractorRecheck is O7's offer: a ref comparison of both runs'
	// commits with the current extractor, when versions differ and both
	// commits are known and clean.
	ExtractorRecheck *string `json:"extractor_recheck"`
	Total            int     `json:"total"`
}

// Comparison is one classified comparison, bound to its head run (G6).
type Comparison struct {
	Repository string
	Base       Base
	// Refs is the resolved ref base (ref comparisons only).
	Refs    RefCommits
	Summary Summary
	items   []Item
	byID    map[string]int
	set     store.ChangeSet
	know    [2]knowledge
	// graph is the repository holding each side's dependents: the head
	// graph, and for a ref base the base namespace.
	graph [2]string
	// mu guards impacts, files and unknowns: the server shares a cached comparison
	// between concurrent requests.
	mu      sync.Mutex
	impacts map[string]*ImpactResult
	// files indexes each graph's file nodes (unresolved call sites by
	// callee name, the file nodes' table_access_unknown entries), built
	// once per comparison (the graph is fixed by G6), by Prepare in the
	// background or by the first traversal that needs it.
	files map[string]*indexBuild[*fileIndex]
	// unknowns indexes each graph's table_access_unknown entries, named
	// and unbound (I8, Revision 4; UnknownUses), built once per comparison by
	// Prepare when an item is a table or column, or by the first traversal
	// that reaches a table.
	unknowns map[string]*indexBuild[*UnknownUses]
}

// ID identifies the comparison for cursors: the base and both runs.
func (c *Comparison) ID() string {
	sum := sha256.Sum256([]byte(c.Repository + "\x00" + c.Base.String() + "\x00" + c.set.Base.Run.RunID + "\x00" + c.set.Head.Run.RunID + "\x00" + c.set.Base.Repository + "\x00" + c.set.Head.Repository))
	return hex.EncodeToString(sum[:12])
}

// HeadRun is the run the comparison's head side is bound to.
func (c *Comparison) HeadRun() string { return c.set.Head.Run.RunID }

// ChangeSet is the raw change set the comparison classified.
func (c *Comparison) ChangeSet() store.ChangeSet { return c.set }

// Compare composes the raw change set for base and classifies it.
func Compare(ctx context.Context, s store.Store, repository string, b Base, opt Options) (*Comparison, error) {
	cmp := &Comparison{Repository: repository, Base: b}
	var cb store.ChangeBase
	switch b.Kind {
	case BasePrevious:
		runs, err := s.ChangeRuns(ctx, repository)
		if err != nil {
			return nil, fmt.Errorf("comparing %s: reading the journal: %w", repository, err)
		}
		if len(runs) == 0 {
			return nil, fmt.Errorf("comparing %s: %w", repository, store.ErrNoJournal)
		}
		newest := runs[len(runs)-1]
		if newest.Initial || newest.PreviousRun == "" {
			return nil, fmt.Errorf("comparing %s: %w", repository, ErrNoPrevious)
		}
		cb.Run = newest.PreviousRun
	case BaseRun:
		cb.Run = b.Value
	case BasePin:
		cb.Pin = b.Value
	case BaseRef:
		indexed, err := IndexedCommit(ctx, s, repository)
		if err != nil {
			return nil, err
		}
		refs, err := ResolveRefs(ctx, opt.Root, b, indexed)
		if err != nil {
			return nil, err
		}
		cmp.Refs = refs
		cmp.Summary.HeadCommitDiffers = refs.HeadCommitDiffers
		snapshots, err := s.RefSnapshots(ctx, repository)
		if err != nil {
			return nil, fmt.Errorf("comparing %s: reading ref snapshots: %w", repository, err)
		}
		have := map[string]bool{}
		for _, r := range snapshots {
			if SnapshotUsable(r, opt.ExtractorVersion) {
				have[r.Namespace] = true
			}
		}
		var missing []string
		for _, commit := range []string{refs.Base, refs.Head} {
			if commit != "" && !have[store.RefNamespace(repository, commit)] {
				missing = append(missing, commit)
			}
		}
		if len(missing) > 0 {
			return nil, &RefNotIndexedError{Commits: missing}
		}
		cb.Ref = store.RefNamespace(repository, refs.Base)
		if refs.Head != "" {
			cb.Head = store.RefNamespace(repository, refs.Head)
		}
	default:
		return nil, fmt.Errorf("unknown base kind %q", b.Kind)
	}
	set, err := s.ChangeSet(ctx, repository, cb)
	if err != nil {
		return nil, fmt.Errorf("comparing %s with %s: %w", repository, b, err)
	}
	if b.Kind == BaseRef && set.Head.Kind == store.SideCurrent && opt.ExtractorVersion != "" && set.Head.Run.Indexer.Version != opt.ExtractorVersion {
		return nil, fmt.Errorf("head run %s used %s, this binary %s: %w", set.Head.Run.RunID, set.Head.Run.Indexer.Version, opt.ExtractorVersion, ErrReindexNeeded)
	}
	cmp.set = set
	cmp.know = [2]knowledge{newKnowledge(set.Base.Run), newKnowledge(set.Head.Run)}
	cmp.graph = [2]string{set.Head.Repository, set.Head.Repository}
	if set.Base.Kind == store.SideRef {
		cmp.graph[base] = set.Base.Repository
	}
	cl := &classifier{ctx: ctx, s: s, opt: opt, cmp: cmp}
	if err := cl.run(); err != nil {
		return nil, fmt.Errorf("classifying %s against %s: %w", repository, b, err)
	}
	return cmp, nil
}

func sideInfo(side store.ChangeSide) SideInfo {
	r := side.Run
	out := SideInfo{
		Kind: side.Kind, Repository: side.Repository, Pin: side.Pin, Run: r.RunID,
		FinishedAt: r.FinishedAt, Commit: r.Commit, Dirty: r.Dirty, Extractor: r.Indexer,
		PartialReasons: partialReasons(r), Gaps: append([]string{}, r.Gaps...), GapsTruncated: r.GapsTruncated,
	}
	out.Partial = len(out.PartialReasons) > 0 || len(out.Gaps) > 0
	return out
}
