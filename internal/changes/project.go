package changes

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/projectlink"
	"github.com/graphvein/graphvein/internal/query"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/traverse"
)

// ErrProjectRef: a project-scoped traversal needs the compared member's
// current graph, because project links name the members' current
// indexes; a ref comparison reads snapshots the links never saw.
var ErrProjectRef = errors.New("project-scoped impact needs a previous, run or pin base: project links name the members' current indexes, not ref snapshots")

// ProjectLinks is what a project-scoped traversal reads of a project's
// last link pass. projectlink.View implements it.
type ProjectLinks interface {
	// LinksTo returns the stored links whose target is the node
	// (repository, identity).
	LinksTo(repository, identity string) []store.ProjectLink
	// StaleReasons says why a link is stale, nil when it is fresh.
	StaleReasons(l store.ProjectLink) []string
	// PossibleCallers lists the sources of other members the pass left
	// ambiguous or incomplete that may reach the route (repository,
	// identity) without a link (T022 review).
	PossibleCallers(repository, identity string) []projectlink.PossibleCaller
}

// PossibleDependent is a source of another member that may depend on a
// reached route although no project link joins them (T022 review):
// the last link pass left it ambiguous with the route among its
// candidates, or incomplete because a candidate provider had no stored
// index. Its claim is always "may depend on" (ADR 0056) and it is never a
// path: the traversal does not continue from it.
type PossibleDependent struct {
	Node       string `json:"node"`
	Repository string `json:"repository"`
	Kind       string `json:"kind"`
	Seed       string `json:"seed"`
	Distance   int    `json:"distance"`
	// Relation is I12 or I13, as a link of the source's kind would be;
	// Wording is that relation's.
	Relation        string `json:"relation"`
	Claim           string `json:"claim"`
	Wording         string `json:"wording"`
	Provenance      string `json:"provenance"`
	Route           string `json:"route"`
	RouteRepository string `json:"route_repository"`
	Row             string `json:"row"`
	Status          string `json:"status"`
	// Reason says why it is not a link: the possible-caller wording and
	// the outcome's own reason or candidate repositories.
	Reason string `json:"reason"`
}

// ProjectScope continues an impact traversal through a project's stored
// links (ADR 0075 "Query, impact and change journal", T022 B7): from a
// reached route of a member, every project link into it leads to the
// client call (I12) or OpenAPI operation (I13) of another member, and the
// traversal goes on in that member's current graph with the same
// relations. A link is inferred and shows its assumption; a stale link is
// followed and its steps say why it is stale.
type ProjectScope struct {
	Project string
	// Members are the project's member repositories in manifest order;
	// a link to a repository outside them is not followed.
	Members []string
	Links   ProjectLinks
}

// projectHops are the project link kinds a traversal follows, with their
// relation. Every other link kind is counted in untraversed as
// "project:" plus its kind.
var projectHops = map[string]string{"calls_route": "I12", "documents_route": "I13"}

// MemberImpact is what a project-scoped traversal found in one member
// other than the compared one: its unknowns and table uses, and the link
// sources it could not continue from because the member no longer stores
// them (a stale link whose call was removed).
type MemberImpact struct {
	Repository         string     `json:"repository"`
	Unknowns           Unknowns   `json:"unknowns"`
	TableUses          []TableUse `json:"table_uses"`
	LinkSourcesMissing []string   `json:"link_sources_missing"`
}

// newTraversal starts a traversal on walk w, or on a new walk when w is
// nil. Member traversals at project scope share the compared member's
// walk, so its reached count and caps cover the whole project.
func newTraversal(ctx context.Context, s store.Store, c *Comparison, depth int, w *traverse.Walk) *traversal {
	if w == nil {
		w = &traverse.Walk{Depth: depth}
	}
	return &traversal{ctx: ctx, s: s, c: c, depth: depth, visited: map[string]*reached{},
		untraversed: map[string]int{}, notExtracted: map[string]map[string]bool{},
		useTables: map[string]string{}, uses: map[string]map[string]bool{}, graphs: map[string]string{},
		walker: w, hopped: map[string]bool{}}
}

// followProject follows the project links into every route the compared
// member's traversal reached (seeds included), walks each consumer's
// graph from the link sources with the remaining depth, and repeats for
// routes the consumers reach, until no link leads anywhere new. It
// returns the consumers' traversals in member order; t itself is never
// among them (a link back into the compared member continues t).
func (t *traversal) followProject(scope *ProjectScope) ([]*traversal, error) {
	member := map[string]bool{}
	order := map[string]int{}
	for i, m := range scope.Members {
		member[m], order[m] = true, i
	}
	byRepo := map[string]*traversal{t.label: t}
	missing := map[string][]string{}
	pending := []*traversal{t}
	for len(pending) > 0 {
		tr := pending[0]
		pending = pending[1:]
		var routes []string
		for id, r := range tr.visited {
			if r.path.Kind == "route" && tr.graphs[id] == tr.label && !tr.hopped[id] {
				routes = append(routes, id)
			}
		}
		sort.Strings(routes)
		injected := map[*traversal]map[int][]string{}
		for _, id := range routes {
			tr.hopped[id] = true
			parent := tr.visited[id].path
			for _, pc := range scope.Links.PossibleCallers(tr.label, id) {
				relation, ok := projectHops[pc.Kind]
				if !ok || !member[pc.Source.Repository] {
					continue
				}
				t.possible = append(t.possible, PossibleDependent{Node: pc.Source.Identity, Repository: pc.Source.Repository, Kind: identityKind(pc.Source.Identity),
					Seed: parent.Seed, Distance: parent.Distance + 1, Relation: relation, Claim: "may depend on", Wording: relationText(relation),
					Provenance: "inferred", Route: id, RouteRepository: tr.label, Row: pc.Row, Status: pc.Status, Reason: possibleReason(pc)})
			}
			fan := 0
			for _, l := range scope.Links.LinksTo(tr.label, id) {
				relation, ok := projectHops[l.Kind]
				if !ok || !member[l.Source.Repository] {
					t.untraversed["project:"+l.Kind]++
					continue
				}
				d := parent.Distance + 1
				if d > t.depth {
					t.walker.Bind(CapDepth, d, 1)
					continue
				}
				repo := l.Source.Repository
				tc := byRepo[repo]
				if tc == nil {
					know, err := t.memberKnowledge(repo)
					if err != nil {
						return nil, err
					}
					tc = newTraversal(t.ctx, t.s, t.c, t.depth, t.walker)
					tc.label, tc.know = repo, &know
					byRepo[repo] = tc
				}
				src := l.Source.Identity
				if tc.visited[src] != nil {
					continue
				}
				nodes, err := t.s.NodesByIdentity(t.ctx, repo, []string{src})
				if err != nil {
					return nil, fmt.Errorf("reading the source of a project link in %s: %w", repo, err)
				}
				if len(nodes) == 0 {
					if !inList(missing[repo], src) {
						missing[repo] = append(missing[repo], src)
					}
					continue
				}
				if fan >= MaxFanOut {
					t.walker.Bind(CapFanOut, d, 1)
					continue
				}
				if !t.walker.Admit(d) {
					continue
				}
				fan++
				evidence := l.Evidence
				if evidence == nil {
					evidence = []contract.Evidence{}
				}
				step := Step{Kind: l.Kind, Source: src, Target: id, Relation: relation, Provenance: l.Provenance, Confidence: l.Confidence,
					Evidence: evidence, SourceRepository: repo, TargetRepository: tr.label, Assumption: l.Assumption, StaleReasons: scope.Links.StaleReasons(l)}
				p := Path{Node: src, Kind: identityKind(src), Seed: parent.Seed, Distance: d, Relation: relation,
					Steps: append(append([]Step{}, parent.Steps...), step)}
				tc.visited[src] = &reached{path: p}
				tc.graphs[src] = repo
				if injected[tc] == nil {
					injected[tc] = map[int][]string{}
				}
				injected[tc][d] = append(injected[tc][d], src)
			}
		}
		walked := make([]*traversal, 0, len(injected))
		for tc := range injected {
			walked = append(walked, tc)
		}
		sort.Slice(walked, func(i, j int) bool { return order[walked[i].label] < order[walked[j].label] })
		for _, tc := range walked {
			if err := tc.walkLevels(injected[tc], tc.label); err != nil {
				return nil, err
			}
			pending = append(pending, tc)
		}
	}
	var out []*traversal
	for _, m := range scope.Members {
		if tc := byRepo[m]; tc != nil && tc != t {
			out = append(out, tc)
		}
	}
	for _, tc := range out {
		tc.missing = missing[tc.label]
	}
	t.missing = missing[t.label]
	return out, nil
}

// memberKnowledge is the knowledge of repo's latest journal run: the gap
// files of the index the traversal reads. Without a journal run the
// whole member is a gap, so every path into it is inferred.
func (t *traversal) memberKnowledge(repo string) (knowledge, error) {
	runs, err := t.s.ChangeRuns(t.ctx, repo)
	if err != nil {
		return knowledge{}, fmt.Errorf("reading the journal of %s: %w", repo, err)
	}
	var latest *store.ChangeRun
	for i := range runs {
		if latest == nil || runs[i].Seq > latest.Seq {
			latest = &runs[i]
		}
	}
	if latest == nil {
		return knowledge{whole: true}, nil
	}
	return newKnowledge(*latest), nil
}

// mergeProject adds the consumers' paths, unknowns and caps to out, the
// compared member's result, and labels every path with its repository.
func (t *traversal) mergeProject(out *ImpactResult, scope *ProjectScope, consumers []*traversal) error {
	order := map[string]int{}
	for i, m := range scope.Members {
		order[m] = i
	}
	out.Project = scope.Project
	out.Members = []MemberImpact{}
	for i := range out.Paths {
		out.Paths[i].Repository = t.label
	}
	caps := map[string]bool{}
	for _, c := range out.Caps {
		caps[c] = true
	}
	for _, tc := range consumers {
		r, err := tc.result(nil)
		if err != nil {
			return fmt.Errorf("member %s: %w", tc.label, err)
		}
		for _, p := range r.Paths {
			p.Repository = tc.label
			out.Paths = append(out.Paths, p)
		}
		for _, c := range r.Caps {
			caps[c] = true
		}
		missing := append([]string{}, tc.missing...)
		sort.Strings(missing)
		out.Members = append(out.Members, MemberImpact{Repository: tc.label, Unknowns: r.Unknowns, TableUses: r.TableUses, LinkSourcesMissing: missing})
	}
	if len(t.missing) > 0 {
		// Links back into the compared member whose source it no longer
		// stores: reported with the member, never as impact.
		missing := append([]string{}, t.missing...)
		sort.Strings(missing)
		out.Members = append(out.Members, MemberImpact{Repository: t.label, Unknowns: Unknowns{BlindSpots: []query.MissingSite{}, NearMisses: []NearMiss{}, GapNodes: []string{}, Untraversed: map[string]int{}, NotExtracted: []NotExtracted{}},
			TableUses: []TableUse{}, LinkSourcesMissing: missing})
	}
	out.Caps = []string{}
	for _, c := range []string{CapDepth, CapReached, CapFanOut} {
		if caps[c] {
			out.Caps = append(out.Caps, c)
		}
	}
	sort.SliceStable(out.Paths, func(i, j int) bool {
		a, b := out.Paths[i], out.Paths[j]
		if a.Distance != b.Distance {
			return a.Distance < b.Distance
		}
		if a.Repository != b.Repository {
			return order[a.Repository] < order[b.Repository]
		}
		return a.Node < b.Node
	})
	// A source already reached through a link (another route's) is a
	// dependent, not a possible one.
	out.PossibleDependents = []PossibleDependent{}
	reached := map[[2]string]bool{}
	for _, p := range out.Paths {
		reached[[2]string{p.Repository, p.Node}] = true
	}
	for _, pd := range t.possible {
		if !reached[[2]string{pd.Repository, pd.Node}] {
			out.PossibleDependents = append(out.PossibleDependents, pd)
		}
	}
	sort.SliceStable(out.PossibleDependents, func(i, j int) bool {
		a, b := out.PossibleDependents[i], out.PossibleDependents[j]
		if a.Distance != b.Distance {
			return a.Distance < b.Distance
		}
		if a.RouteRepository != b.RouteRepository || a.Route != b.Route {
			return order[a.RouteRepository] < order[b.RouteRepository] || a.RouteRepository == b.RouteRepository && a.Route < b.Route
		}
		if a.Repository != b.Repository {
			return order[a.Repository] < order[b.Repository]
		}
		return a.Node < b.Node
	})
	return nil
}

// ProjectLinkDiff is the link journal's answer to "which project links
// changed between two link passes" (ADR 0075: the project keeps the link
// set of every pass as keys with payload hashes). Changed links have the
// same key and a different payload hash (endpoint runs left out, so a
// member indexed again with the same content changes nothing).
type ProjectLinkDiff struct {
	Project  string                   `json:"project"`
	From     int64                    `json:"from_pass"`
	To       int64                    `json:"to_pass"`
	Added    []store.ProjectLinkKey   `json:"added"`
	Removed  []store.ProjectLinkKey   `json:"removed"`
	Changed  []store.ProjectLinkKey   `json:"changed"`
	Members  [2][]store.ProjectMember `json:"members"`
	Manifest bool                     `json:"manifest_changed"`
}

// ErrPassUnknown: the link journal keeps no pass with that number.
var ErrPassUnknown = errors.New("the project link journal keeps no such pass")

// DiffProjectLinks compares pass from with pass to of runs (a project's
// link journal, as ProjectLinkRuns returns it). Keys are listed in key
// order.
func DiffProjectLinks(project string, runs []store.ProjectLinkRun, from, to int64) (ProjectLinkDiff, error) {
	out := ProjectLinkDiff{Project: project, From: from, To: to, Added: []store.ProjectLinkKey{}, Removed: []store.ProjectLinkKey{}, Changed: []store.ProjectLinkKey{}}
	var a, b *store.ProjectLinkRun
	for i := range runs {
		if runs[i].Seq == from {
			a = &runs[i]
		}
		if runs[i].Seq == to {
			b = &runs[i]
		}
	}
	if a == nil || b == nil {
		return out, fmt.Errorf("comparing passes %d and %d of project %s: %w", from, to, project, ErrPassUnknown)
	}
	out.Members = [2][]store.ProjectMember{a.Members, b.Members}
	out.Manifest = a.ManifestHash != b.ManifestHash
	before := map[store.ProjectLinkKey]string{}
	for _, l := range a.Links {
		before[l.ProjectLinkKey] = l.PayloadHash
	}
	after := map[store.ProjectLinkKey]bool{}
	for _, l := range b.Links {
		after[l.ProjectLinkKey] = true
		hash, ok := before[l.ProjectLinkKey]
		switch {
		case !ok:
			out.Added = append(out.Added, l.ProjectLinkKey)
		case hash != l.PayloadHash:
			out.Changed = append(out.Changed, l.ProjectLinkKey)
		}
	}
	for _, l := range a.Links {
		if !after[l.ProjectLinkKey] {
			out.Removed = append(out.Removed, l.ProjectLinkKey)
		}
	}
	for _, list := range [][]store.ProjectLinkKey{out.Added, out.Removed, out.Changed} {
		sort.Slice(list, func(i, j int) bool { return list[i].Less(list[j]) })
	}
	return out, nil
}

// possibleReason words why a possible dependent is not a link.
func possibleReason(pc projectlink.PossibleCaller) string {
	switch {
	case pc.Status == projectlink.StatusAmbiguous:
		return fmt.Sprintf("%s; %d candidates in %s", pc.Wording, pc.CandidateCount, strings.Join(pc.CandidateRepositories, ", "))
	case pc.Reason != "":
		return pc.Wording + "; " + pc.Reason
	}
	return pc.Wording
}

// relationText is the wording of relation id in the relation table.
func relationText(id string) string {
	rel, _ := relationOf(id)
	return rel.Wording
}
