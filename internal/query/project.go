package query

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/projectlink"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/traverse"
)

// ProjectOptions asks for callers or impact at project scope (ADR 0075
// "Query, impact and change journal", T022 B7): the traversal reads each
// member's own graph and crosses repositories only through the project's
// stored links.
type ProjectOptions struct {
	Command string
	Term    string
	// Repository names the member whose nodes the term is looked up in;
	// empty looks in every member, and a term found in several is
	// ambiguous.
	Repository string
	Depth      int
	Project    config.Project
}

// ProjectNode is a node of one project member. Identities are only unique
// within a repository, so every node carries its repository (ADR 0075
// identity rule 3).
type ProjectNode struct {
	Repository string `json:"repository"`
	Node
}

// ProjectStep is one edge of a project path. An edge of a member's own
// graph has the same repository on both ends; a project link (ProjectLink
// true) joins two repositories and carries the assumption it rests on and,
// when the link is stale, why.
type ProjectStep struct {
	contract.Edge
	SourceRepository string   `json:"source_repository"`
	TargetRepository string   `json:"target_repository"`
	ProjectLink      bool     `json:"project_link,omitempty"`
	Assumption       string   `json:"assumption,omitempty"`
	Stale            bool     `json:"stale,omitempty"`
	StaleReasons     []string `json:"stale_reasons,omitempty"`
}

// ProjectResult is one reached node with its shortest path. A path through
// a project link is inferred and at most as confident as the link; its
// assumptions are listed, and it is stale when one of its links is.
type ProjectResult struct {
	Node        ProjectNode   `json:"node"`
	Distance    int           `json:"distance"`
	Path        []ProjectStep `json:"path"`
	Provenance  string        `json:"provenance,omitempty"`
	Confidence  *float64      `json:"confidence,omitempty"`
	Assumptions []string      `json:"assumptions,omitempty"`
	Stale       bool          `json:"stale,omitempty"`
}

// MemberFreshness is one member's index state for a project query.
type MemberFreshness struct {
	Repository string `json:"repository"`
	Indexed    bool   `json:"indexed"`
	// Checkout is false when the member has no checkout here: its facts
	// come from the store, without IDE links or changed files.
	Checkout bool     `json:"checkout"`
	Changed  []string `json:"changed_files"`
}

// LinkPassState is the project's last link pass as the query read it.
type LinkPassState struct {
	Stored bool  `json:"stored"`
	Pass   int64 `json:"pass"`
	Links  int   `json:"links"`
	// StaleReasons is empty when the pass is fresh.
	StaleReasons []string `json:"stale_reasons"`
}

// ProjectMissingSite is a blind spot (ADR 0009) in one member.
type ProjectMissingSite struct {
	Repository string `json:"repository"`
	MissingSite
}

// ProjectPossibleCaller is a client call of another member that may call
// a reached route without a project link to it (T022 review): the last
// link pass left it ambiguous with the route among its candidates, or
// incomplete with the route's member among its candidate providers. It is
// an unknown, never a result or a link: "may be called by".
type ProjectPossibleCaller struct {
	Route store.ProjectNodeRef `json:"route"`
	projectlink.PossibleCaller
}

// ProjectResponse is the JSON envelope of graphvein callers and impact
// with --project.
type ProjectResponse struct {
	Project                   string               `json:"project"`
	Command                   string               `json:"command"`
	Members                   []MemberFreshness    `json:"members"`
	LinkPass                  LinkPassState        `json:"link_pass"`
	Target                    *ProjectNode         `json:"target,omitempty"`
	Results                   []ProjectResult      `json:"results"`
	PossiblyMissingCallers    int                  `json:"possibly_missing_callers"`
	PossiblyMissingCallSites  []ProjectMissingSite `json:"possibly_missing_call_sites"`
	UnknownCallsInResultFiles int                  `json:"unknown_calls_in_result_files"`
	BlindSpotData             bool                 `json:"blind_spot_data"`
	// PossibleCallers are the reached routes' possible callers in other
	// members (callers and impact alike), counted in the blind-spot line.
	PossibleCallers []ProjectPossibleCaller `json:"possible_callers"`
	Candidates      []store.ProjectNodeRef  `json:"candidates"`
	// Caps, Bounds and Ends are as in Response (T030 D2, G10); the
	// project walk has the same reached-node and fan-out caps, and its
	// depth maximum is ProjectMaxDepth.
	Caps   []string         `json:"caps"`
	Bounds []traverse.Bound `json:"bounds"`
	Ends   []ProjectEnd     `json:"ends"`
	Error  string           `json:"error,omitempty"`
}

// ProjectEnd is an End in one member.
type ProjectEnd struct {
	Repository string `json:"repository"`
	End
}

func resultNodes(results []ProjectResult) []ProjectNode {
	out := make([]ProjectNode, 0, len(results))
	for _, r := range results {
		out = append(out, r.Node)
	}
	return out
}

// projectKey names a node of a member.
type projectKey struct{ repository, identity string }

// ExecuteProject runs callers or impact across the members of a project.
// Inside a member it follows exactly the edges Execute follows; from a
// route it also follows the project's stored calls_route links back to the
// client calls of other members (callers of a route include calls in
// consumer repositories; impact of a handler continues into the
// consumer's graph). Stale links are followed and marked stale.
func ExecuteProject(ctx context.Context, s store.Store, opt ProjectOptions) (ProjectResponse, error) {
	out := ProjectResponse{Project: opt.Project.Name, Command: opt.Command, Members: []MemberFreshness{}, Results: []ProjectResult{},
		PossiblyMissingCallSites: []ProjectMissingSite{}, Candidates: []store.ProjectNodeRef{}, LinkPass: LinkPassState{StaleReasons: []string{}},
		PossibleCallers: []ProjectPossibleCaller{}, Caps: []string{}, Bounds: []traverse.Bound{}, Ends: []ProjectEnd{}}
	if opt.Command != "callers" && opt.Command != "impact" {
		return out, fmt.Errorf("unknown project graph command %q", opt.Command)
	}
	if opt.Depth > ProjectMaxDepth {
		return out, fmt.Errorf("depth %d is above the project maximum %d", opt.Depth, ProjectMaxDepth)
	}
	view, err := projectlink.ReadView(ctx, s, opt.Project)
	if err != nil {
		return out, err
	}
	out.LinkPass = LinkPassState{Stored: view.Stored, Pass: view.Set.Pass, Links: len(view.Set.Links), StaleReasons: []string{}}
	out.LinkPass.StaleReasons = append(out.LinkPass.StaleReasons, view.PassReasons()...)

	members := map[string]bool{}
	presenters := map[string]*Presenter{}
	hashes := map[string]map[string]string{}
	for _, m := range opt.Project.Members {
		members[m.Repository] = true
		f := MemberFreshness{Repository: m.Repository, Indexed: view.Latest[m.Repository] != "", Checkout: m.Checkout, Changed: []string{}}
		if m.Checkout {
			cfg, err := config.Load(m.Path)
			if err != nil {
				return out, fmt.Errorf("member %s: %w", m.Repository, err)
			}
			changed, h, err := index.FreshnessWithHashes(ctx, m.Path, m.Repository, s)
			if err != nil {
				return out, fmt.Errorf("member %s: %w", m.Repository, err)
			}
			f.Changed = append(f.Changed, changed...)
			abs, err := filepath.Abs(m.Path)
			if err != nil {
				return out, fmt.Errorf("resolving member %s: %w", m.Repository, err)
			}
			presenters[m.Repository] = NewPresenter(abs, cfg.IDE)
			hashes[m.Repository] = h
		}
		out.Members = append(out.Members, f)
	}
	if opt.Repository != "" && !members[opt.Repository] {
		return out, fmt.Errorf("repository %s is not a member of project %s", opt.Repository, opt.Project.Name)
	}
	present := func(repo string, n contract.Node) ProjectNode {
		if p := presenters[repo]; p != nil {
			return ProjectNode{Repository: repo, Node: p.Node(n, hashes[repo][n.File])}
		}
		return ProjectNode{Repository: repo, Node: Node{Node: n}}
	}

	// The target: the term looked up in the named member, or in every
	// indexed member.
	filter := store.Query{Term: opt.Term}
	if _, _, _, err := contract.ParseIdentity(opt.Term); err == nil {
		filter.Identity, filter.Term = opt.Term, ""
	}
	type found struct {
		repo string
		node contract.Node
	}
	var targets []found
	for _, m := range opt.Project.Members {
		if opt.Repository != "" && m.Repository != opt.Repository || view.Latest[m.Repository] == "" {
			continue
		}
		nodes, err := s.Nodes(ctx, m.Repository, filter)
		if err != nil {
			return out, err
		}
		for _, n := range nodes {
			targets = append(targets, found{m.Repository, n})
		}
	}
	// The rule of Resolve across the members: a partial suffix match is a
	// candidate only (T030 review).
	var resolving, partial []found
	for _, t := range targets {
		switch MatchOf(t.node, opt.Term) {
		case MatchIdentity, MatchName, MatchQualifiedSuffix:
			resolving = append(resolving, t)
		case MatchPartial:
			partial = append(partial, t)
		}
	}
	if len(resolving) != 1 {
		out.Error = "no matching target"
		candidates := partial
		if len(resolving) > 1 {
			out.Error = "ambiguous target"
			candidates = resolving
		}
		for _, t := range candidates {
			out.Candidates = append(out.Candidates, store.ProjectNodeRef{Repository: t.repo, Identity: t.node.Identity})
		}
		return out, nil
	}
	target := resolving[0]
	presented := present(target.repo, target.node)
	out.Target = &presented

	start := projectKey{target.repo, target.node.Identity}
	byKey := map[projectKey]contract.Node{start: target.node}
	fetch := func(keys []projectKey) error {
		missing := map[string][]string{}
		for _, k := range keys {
			if _, ok := byKey[k]; !ok {
				missing[k.repository] = append(missing[k.repository], k.identity)
			}
		}
		for repo, ids := range missing {
			nodes, err := s.NodesByIdentity(ctx, repo, ids)
			if err != nil {
				return err
			}
			for _, n := range nodes {
				byKey[projectKey{repo, n.Identity}] = n
			}
		}
		return nil
	}
	seeds := []projectKey{start}
	visited := map[projectKey]bool{start: true}
	paths := map[projectKey][]ProjectStep{start: {}}
	if opt.Command == "impact" && target.node.Kind == "file" {
		queue := []string{target.node.Identity}
		for len(queue) > 0 {
			edges, err := s.Edges(ctx, target.repo, queue, []string{"contains"}, store.Outgoing)
			if err != nil {
				return out, err
			}
			queue = nil
			for _, e := range edges {
				k := projectKey{target.repo, e.Target}
				if !visited[k] {
					visited[k] = true
					seeds = append(seeds, k)
					queue = append(queue, e.Target)
					paths[k] = []ProjectStep{}
				}
			}
		}
		if err := fetch(seeds); err != nil {
			return out, err
		}
	}
	kinds := []string{"calls"}
	if opt.Command == "impact" {
		kinds = append(kinds, "imports", "references")
	}
	files := map[projectKey]bool{{target.repo, target.node.File}: true}
	depth := opt.Depth
	if opt.Command == "callers" && target.node.Kind == "route" && depth < 2 {
		depth = 2
	}
	order := map[string]int{}
	for i, m := range opt.Project.Members {
		order[m.Repository] = i
	}
	ends := func(st ProjectStep) (next, previous projectKey) {
		if st.Kind == "exposes_route" {
			return projectKey{st.TargetRepository, st.Target}, projectKey{st.SourceRepository, st.Source}
		}
		return projectKey{st.SourceRepository, st.Source}, projectKey{st.TargetRepository, st.Target}
	}
	// The walk keys nodes as "repository\x00identity": identities are
	// unique only within a member.
	keyOf := func(k projectKey) string { return k.repository + "\x00" + k.identity }
	byWalkKey := map[string]projectKey{keyOf(start): start}
	for _, k := range seeds {
		byWalkKey[keyOf(k)] = k
	}
	ended := map[projectKey]bool{}
	walk := &traverse.Walk{
		Depth:   depth,
		Visited: func(id string) bool { return visited[byWalkKey[id]] },
		Candidates: func(walkFrontier []string, _ int, _ bool) ([]traverse.Candidate, error) {
			frontier := make([]projectKey, 0, len(walkFrontier))
			for _, id := range walkFrontier {
				frontier = append(frontier, byWalkKey[id])
			}
			// Steps of this level: each member's own edges (as Execute
			// reads them), then the project links into the level's routes.
			byRepo := map[string][]string{}
			for _, k := range frontier {
				byRepo[k.repository] = append(byRepo[k.repository], k.identity)
			}
			repos := make([]string, 0, len(byRepo))
			for repo := range byRepo {
				repos = append(repos, repo)
			}
			sort.Slice(repos, func(i, j int) bool { return order[repos[i]] < order[repos[j]] })
			var level []ProjectStep
			for _, repo := range repos {
				edges, err := levelEdges(ctx, s, repo, opt.Command, byRepo[repo], kinds, func(id string) string { return byKey[projectKey{repo, id}].Kind })
				if err != nil {
					return nil, err
				}
				for _, e := range edges {
					level = append(level, ProjectStep{Edge: e, SourceRepository: repo, TargetRepository: repo})
				}
			}
			var links []ProjectStep
			for _, k := range frontier {
				if byKey[k].Kind != "route" {
					continue
				}
				for _, l := range view.LinksTo(k.repository, k.identity) {
					if l.Kind != "calls_route" || !members[l.Source.Repository] {
						continue
					}
					why := view.StaleReasons(l)
					links = append(links, ProjectStep{
						Edge:             contract.Edge{Kind: l.Kind, Source: l.Source.Identity, Target: l.Target.Identity, Provenance: l.Provenance, Confidence: l.Confidence, Evidence: l.Evidence},
						SourceRepository: l.Source.Repository, TargetRepository: l.Target.Repository,
						ProjectLink: true, Assumption: l.Assumption, Stale: len(why) > 0, StaleReasons: why,
					})
				}
			}
			sort.SliceStable(links, func(i, j int) bool {
				a, b := links[i], links[j]
				if a.SourceRepository != b.SourceRepository {
					return order[a.SourceRepository] < order[b.SourceRepository]
				}
				if a.Source != b.Source {
					return a.Source < b.Source
				}
				if a.TargetRepository != b.TargetRepository {
					return order[a.TargetRepository] < order[b.TargetRepository]
				}
				return a.Target < b.Target
			})
			level = append(level, links...)
			var reached []projectKey
			led := map[projectKey]bool{}
			for _, st := range level {
				next, previous := ends(st)
				led[previous] = true
				if !visited[next] {
					reached = append(reached, next)
				}
			}
			for _, k := range frontier {
				if kind := byKey[k].Kind; (kind == "function" || kind == "method") && !led[k] {
					ended[k] = true
				}
			}
			if err := fetch(reached); err != nil {
				return nil, err
			}
			var cands []traverse.Candidate
			for _, st := range level {
				next, previous := ends(st)
				if _, ok := byKey[next]; !ok {
					continue
				}
				to := keyOf(next)
				byWalkKey[to] = next
				cands = append(cands, traverse.Candidate{From: keyOf(previous), To: to, Edge: st.Edge, Data: st})
			}
			return cands, nil
		},
		Reach: func(c traverse.Candidate, distance int) {
			next, previous := byWalkKey[c.To], byWalkKey[c.From]
			st := c.Data.(ProjectStep)
			visited[next] = true
			p := append(append([]ProjectStep{}, paths[previous]...), st)
			paths[next] = p
			out.Results = append(out.Results, projectResult(present(next.repository, byKey[next]), distance, p))
			files[projectKey{next.repository, byKey[next].File}] = true
		},
	}
	walkSeeds := make([]string, 0, len(seeds))
	for _, k := range seeds {
		walkSeeds = append(walkSeeds, keyOf(k))
	}
	if err := walk.Run(walkSeeds); err != nil {
		return out, err
	}
	out.Caps, out.Bounds = walk.Caps(), walk.Bounds()
	sort.Slice(out.Results, func(i, j int) bool {
		a, b := out.Results[i], out.Results[j]
		if a.Distance != b.Distance {
			return a.Distance < b.Distance
		}
		if a.Node.Repository != b.Node.Repository {
			return order[a.Node.Repository] < order[b.Node.Repository]
		}
		return a.Node.Identity < b.Node.Identity
	})
	text := CallersEndText
	if opt.Command == "impact" {
		text = ImpactEndText
	}
	for _, n := range append([]ProjectNode{presented}, resultNodes(out.Results)...) {
		if ended[projectKey{n.Repository, n.Identity}] {
			out.Ends = append(out.Ends, ProjectEnd{Repository: n.Repository, End: End{Node: n.Identity, Text: text}})
		}
	}
	// Possible callers of every reached route (the target first, then the
	// results in their order), calls only: callers and impact follow
	// calls_route links, not documents_route.
	routes := []ProjectNode{presented}
	for _, r := range out.Results {
		routes = append(routes, r.Node)
	}
	for _, n := range routes {
		if n.Kind != "route" {
			continue
		}
		for _, pc := range view.PossibleCallers(n.Repository, n.Identity) {
			if pc.Kind == projectlink.KindCallsRoute && members[pc.Source.Repository] {
				out.PossibleCallers = append(out.PossibleCallers, ProjectPossibleCaller{Route: store.ProjectNodeRef{Repository: n.Repository, Identity: n.Identity}, PossibleCaller: pc})
			}
		}
	}
	if opt.Command == "impact" {
		if err := projectUnknowns(ctx, s, opt.Project, files, &out); err != nil {
			return out, err
		}
	}
	return out, nil
}

// projectResult labels a reached node's path: inferred when any step is,
// with the minimum confidence, the assumptions of its project links and
// whether one of them is stale.
func projectResult(n ProjectNode, distance int, p []ProjectStep) ProjectResult {
	r := ProjectResult{Node: n, Distance: distance, Path: p}
	confidence := 1.0
	seen := map[string]bool{}
	for _, st := range p {
		if st.Confidence < confidence {
			confidence = st.Confidence
		}
		if st.Provenance == "inferred" || st.ProjectLink {
			r.Provenance = "inferred"
		}
		if st.ProjectLink && !seen[st.Assumption] {
			seen[st.Assumption] = true
			r.Assumptions = append(r.Assumptions, st.Assumption)
		}
		r.Stale = r.Stale || st.Stale
	}
	if r.Provenance != "" {
		r.Confidence = &confidence
	}
	return r
}

// projectUnknowns adds impact's unknowns per member: unresolved calls in
// the result files and blind spots matched by name within the member whose
// nodes the traversal reached (a name in one repository says nothing about
// call sites in another).
func projectUnknowns(ctx context.Context, s store.Store, project config.Project, files map[projectKey]bool, out *ProjectResponse) error {
	nodesOf := map[string][]Node{}
	if out.Target != nil {
		nodesOf[out.Target.Repository] = append(nodesOf[out.Target.Repository], out.Target.Node)
	}
	for _, r := range out.Results {
		nodesOf[r.Node.Repository] = append(nodesOf[r.Node.Repository], r.Node.Node)
	}
	for _, m := range project.Members {
		nodes := nodesOf[m.Repository]
		if len(nodes) == 0 {
			continue
		}
		var ids []string
		for k := range files {
			if k.repository == m.Repository {
				ids = append(ids, fileIdentity(k.identity))
			}
		}
		sort.Strings(ids)
		fileNodes, err := s.NodesByIdentity(ctx, m.Repository, ids)
		if err != nil {
			return err
		}
		for _, n := range fileNodes {
			if v, ok := n.Attributes["unresolved_calls_unknown"]; ok {
				count, err := strconv.Atoi(fmt.Sprint(v))
				if err == nil && count > 0 {
					out.UnknownCallsInResultFiles += count
				}
			}
		}
		all, err := s.Nodes(ctx, m.Repository, store.Query{Kind: "file"})
		if err != nil {
			return err
		}
		member := Response{Target: &nodes[0], PossiblyMissingCallSites: []MissingSite{}}
		for _, n := range nodes[1:] {
			member.Results = append(member.Results, Result{Node: n})
		}
		if err := blindSpots(ctx, s, m.Repository, all, &member); err != nil {
			return fmt.Errorf("member %s: %w", m.Repository, err)
		}
		out.BlindSpotData = out.BlindSpotData || member.BlindSpotData
		out.PossiblyMissingCallers += member.PossiblyMissingCallers
		for _, site := range member.PossiblyMissingCallSites {
			out.PossiblyMissingCallSites = append(out.PossiblyMissingCallSites, ProjectMissingSite{Repository: m.Repository, MissingSite: site})
		}
	}
	return nil
}
