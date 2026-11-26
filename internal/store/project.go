package store

import (
	"context"
	"errors"
	"time"

	"github.com/graphvein/graphvein/internal/contract"
)

// Errors of the project methods (ADR 0075). Callers test them with
// errors.Is.
var (
	// ErrProjectUnknown: no link pass of that project is stored yet.
	ErrProjectUnknown = errors.New("project is unknown")
	// ErrProjectStale: a member was indexed again after the link pass read
	// it, or a link names a node that is no longer stored with the content
	// hash the pass saw. The pass must read the members again; nothing was
	// written.
	ErrProjectStale = errors.New("project members changed since the link pass read them")
	// ErrRepositoryShared: a repository to retire from one project is a
	// member of another stored project, whose links still read its index;
	// nothing was deleted.
	ErrRepositoryShared = errors.New("repository is a member of another project")
)

// DefaultProjectLinkRuns is how many link passes the project link journal
// keeps per project, newest first. It matches the change journal's default
// run count.
const DefaultProjectLinkRuns = DefaultJournalRuns

// ProjectStore holds a multi-repository project's cross-repository links
// and their journal (ADR 0075). Links belong to the project, not to either
// repository, so every member's own snapshot stays exactly as a
// single-repository index stores it.
type ProjectStore interface {
	// Repositories lists every repository indexed into the store, by ID,
	// with its latest run. Ref snapshot namespaces are not listed. A link
	// pass records these runs; a link is fresh only while they are still
	// the latest.
	Repositories(ctx context.Context) ([]Repository, error)
	// ProjectLinks reads, in one read transaction, the project as the
	// latest link pass stored it: members with the runs it read, the
	// manifest hash, and every link in key order. It returns an error
	// wrapping ErrProjectUnknown when no pass is stored.
	ProjectLinks(ctx context.Context, project string) (ProjectLinkSet, error)
	// ReplaceProjectLinks stores the result of one link pass in one
	// transaction: the project record, its members, the complete link set
	// (links absent from set are deleted) and a journal entry. It refuses,
	// writing nothing, a set whose member runs are not the members' latest
	// runs or whose link endpoints are not stored with the recorded content
	// hash (ErrProjectStale), and an invalid set (ErrInvalid). The caller
	// holds the project lock.
	ReplaceProjectLinks(ctx context.Context, set ProjectLinkSet) (ProjectLinkRun, error)
	// ProjectLinkRuns lists the link passes the journal still keeps for
	// project, oldest first. An unknown project has none.
	ProjectLinkRuns(ctx context.Context, project string) ([]ProjectLinkRun, error)
	// RetireProjectMember removes a repository renamed away from project
	// (T022 B11): in one transaction, every project row naming it (its
	// links, the outcomes whose source, target or candidates name it, its
	// member record) and its stored index (runs, files, nodes, edges,
	// diagnostics, fact cache, change journal, pins and ref snapshots).
	// Without it the old name's rows stay in the store as orphans that no
	// manifest names. The link journal keeps its past passes. It refuses,
	// deleting nothing, a repository that is a member of another stored
	// project (ErrRepositoryShared) or a reserved ref namespace
	// (ErrReservedRepository). A repository with nothing stored is not an
	// error. The caller holds the project lock.
	RetireProjectMember(ctx context.Context, project, repository string) error
	// RepositoryFootprint reports what retiring repository would delete:
	// whether a run of it is stored, its node count, and the projects
	// whose last stored link pass lists it as a member (T022 review:
	// a rename preview names them and asks for an explicit confirm).
	RepositoryFootprint(ctx context.Context, repository string) (RepositoryFootprint, error)
}

// RepositoryFootprint is a repository's stored index as a retirement would
// delete it.
type RepositoryFootprint struct {
	Repository string `json:"repository"`
	// Stored is true when at least one run of the repository is stored.
	Stored bool `json:"stored"`
	Nodes  int  `json:"nodes"`
	// Projects are the projects whose stored link pass lists the
	// repository as a member, sorted. None: the repository was indexed on
	// its own (or only by a project whose pass never ran in this store).
	Projects []string `json:"projects"`
}

// Repository is one repository stored in the index, with the run whose
// snapshot is stored now.
type Repository struct {
	ID string `json:"repository"`
	// Latest is the run with the greatest finish time (LatestRun), nil when
	// no run of the repository has a usable finish time.
	Latest *Run `json:"latest_run"`
}

// LatestRun picks the run with the greatest finish time. Run IDs are random
// and RFC3339Nano trims trailing zeros, so neither store order nor string
// comparison finds the latest run; parsed times do. Runs whose finish time
// does not parse are skipped; nil means no usable run. On equal times the
// first run in the given order wins.
func LatestRun(runs []Run) *Run {
	var best *Run
	var bestAt time.Time
	for i := range runs {
		at, err := time.Parse(time.RFC3339Nano, runs[i].FinishedAt)
		if err != nil {
			continue
		}
		if best == nil || at.After(bestAt) {
			best, bestAt = &runs[i], at
		}
	}
	return best
}

// ProjectMember is one member repository as a link pass read it.
type ProjectMember struct {
	Repository string `json:"repository"`
	// Run is the member's latest run when the pass read it, empty when the
	// member had no stored index then.
	Run string `json:"run"`
	// IndexError is set when graphvein index --project failed to index the
	// member's checkout before this pass: the pass linked the member's
	// previous stored index (Run), so views show its links as stale even
	// though Run is still its latest run.
	IndexError string `json:"index_error,omitempty"`
}

// ProjectLinkSet is a project with the complete link set of one link pass.
type ProjectLinkSet struct {
	Project string `json:"project"`
	// ManifestHash identifies the manifest content the pass used; a
	// different hash of the current manifest makes every link stale.
	ManifestHash string `json:"manifest_hash"`
	// Members are in manifest order.
	Members []ProjectMember `json:"members"`
	// Links are stored and returned in key order (ProjectLinkKey.Less).
	Links []ProjectLink `json:"links"`
	// Outcomes are the pass's result for every source (a client call
	// alternative or an OpenAPI path entry), linked or not, in the pass's
	// order. They are stored with the links in the same transaction, so a
	// view can say why a call has no project link (ambiguous, unmatched,
	// incomplete, ...) without running the pass again.
	Outcomes []ProjectOutcome `json:"outcomes,omitempty"`
	// Pass is the journal sequence number of the pass that stored the set;
	// ReplaceProjectLinks ignores it on input.
	Pass int64 `json:"pass"`
}

// ProjectEndpoint is one end of a project link: a node named by the pair
// (repository, identity), never by one concatenated string (ADR 0075
// identity rule 3), with the run and content hash the link was computed
// from.
type ProjectEndpoint struct {
	Repository  string `json:"repository"`
	Identity    string `json:"identity"`
	Run         string `json:"run"`
	ContentHash string `json:"content_hash"`
}

// ProjectLink is one link between nodes of project members, owned by the
// project. Its payload is produced by the project link pass; the store
// checks only what keeps it honest (endpoints stored as recorded, a
// confidence in (0, 1], an assumption shown).
type ProjectLink struct {
	Kind       string              `json:"kind"`
	Source     ProjectEndpoint     `json:"source"`
	Target     ProjectEndpoint     `json:"target"`
	Provenance string              `json:"provenance"`
	Confidence float64             `json:"confidence"`
	Evidence   []contract.Evidence `json:"evidence"`
	// Assumption is the text every view shows with the link, for example
	// "matched across project repositories; base URL not proven".
	Assumption string `json:"assumption"`
	// Assumes lists the row assumptions the link rests on besides the
	// cross-repository one, for example ["NX-V1"] for a link to a Next.js
	// route (TOP-1 of 2026-11-18). Additive: links without any omit it, and
	// the backends store it in the payload like every other field.
	Assumes []string `json:"assumes,omitempty"`
	// Attributes carry pass-specific details (the declaration used,
	// candidates) the store does not interpret.
	Attributes map[string]any `json:"attributes,omitempty"`
}

// Key returns the link's primary key.
func (l ProjectLink) Key() ProjectLinkKey {
	return ProjectLinkKey{Kind: l.Kind, SourceRepository: l.Source.Repository, SourceIdentity: l.Source.Identity, TargetRepository: l.Target.Repository, TargetIdentity: l.Target.Identity}
}

// ProjectLinkKey identifies one link within a project.
type ProjectLinkKey struct {
	Kind             string `json:"kind"`
	SourceRepository string `json:"source_repository"`
	SourceIdentity   string `json:"source_identity"`
	TargetRepository string `json:"target_repository"`
	TargetIdentity   string `json:"target_identity"`
}

// Less orders keys field by field in Go byte order, the order both
// backends return links in.
func (k ProjectLinkKey) Less(o ProjectLinkKey) bool {
	a := [5]string{k.Kind, k.SourceRepository, k.SourceIdentity, k.TargetRepository, k.TargetIdentity}
	b := [5]string{o.Kind, o.SourceRepository, o.SourceIdentity, o.TargetRepository, o.TargetIdentity}
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

// ProjectLinkRun is one link pass in the project link journal. It keeps the
// link set as keys with payload hashes, not the payloads, so a comparison
// can report links added, removed or changed between two passes.
type ProjectLinkRun struct {
	Seq          int64           `json:"seq"`
	ID           string          `json:"id"`
	FinishedAt   string          `json:"finished_at"`
	ManifestHash string          `json:"manifest_hash"`
	Members      []ProjectMember `json:"members"`
	// Added, Removed and Changed compare this pass's links with the
	// previous pass's stored links. Changed counts a link whose payload
	// differs other than in its endpoints' runs: a member indexed again
	// with the same content gets a new run, and its links are rewritten
	// with it but are not counted as changed.
	Added   int `json:"added"`
	Removed int `json:"removed"`
	Changed int `json:"changed"`
	// Links is the pass's link set in key order.
	Links []ProjectLinkEntry `json:"links"`
}

// ProjectLinkEntry is one link of a journal pass.
type ProjectLinkEntry struct {
	ProjectLinkKey
	// PayloadHash is contract.ContentHash of the stored link payload with
	// both endpoint runs blanked, so two passes agree on it exactly when
	// the journal counts the link unchanged.
	PayloadHash string `json:"payload_hash"`
}

// ProjectNodeRef names a node of a project member by the pair
// (repository, identity) (ADR 0075 identity rule 3).
type ProjectNodeRef struct {
	Repository string `json:"repository"`
	Identity   string `json:"identity"`
}

// ProjectOutcome is the link pass's result for one source: a client call
// alternative, or one path entry of an OpenAPI operation. Its row is a row
// of the claim contract in docs/extract/project-links.md; a status other
// than "linked" claims nothing.
type ProjectOutcome struct {
	// Kind is the link kind the source could make.
	Kind     string         `json:"kind"`
	Source   ProjectNodeRef `json:"source"`
	Template string         `json:"template,omitempty"`
	Row      string         `json:"row"`
	Status   string         `json:"status"`
	// Reason names the providers without a stored index (PL-R7).
	Reason string          `json:"reason,omitempty"`
	Target *ProjectNodeRef `json:"target,omitempty"`
	// Candidates are at most ten, ordered by repository and identity;
	// CandidateCount is the full number when there are two or more.
	Candidates     []ProjectNodeRef `json:"candidates"`
	CandidateCount int              `json:"candidate_count"`
}
