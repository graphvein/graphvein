// Package query implements deterministic graph reads through the store boundary.
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
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/traverse"
)

// Options keeps CLI and backend fixture behavior on the same query path.
type Options struct {
	Command, Term, Root, Repository, Kind, File string
	Limit, Depth                                int
	// Fresh, when set, is a freshness check the caller already made for
	// this repository (the MCP server checks once per call batch, T030):
	// Execute reports it as given and does not scan the checkout again.
	// Stored file hashes for presentation are still read from the store.
	Fresh *Freshness
}

// Freshness uses the same changed-file calculation as status.
type Freshness struct {
	Indexed bool     `json:"indexed"`
	Changed []string `json:"changed_files"`
}

// Result retains the shortest traversal path and its supporting evidence.
type Result struct {
	Node       Node            `json:"node"`
	Distance   int             `json:"distance"`
	Path       []contract.Edge `json:"path"`
	Provenance string          `json:"provenance,omitempty"`
	Confidence *float64        `json:"confidence,omitempty"`
}

// Response is the stable CLI JSON envelope, including unsuccessful target lookup.
type Response struct {
	Freshness                 Freshness     `json:"freshness"`
	Command                   string        `json:"command"`
	Target                    *Node         `json:"target,omitempty"`
	Nodes                     []Node        `json:"nodes"`
	Results                   []Result      `json:"results"`
	PossiblyMissingCallers    int           `json:"possibly_missing_callers"`
	PossiblyMissingCallSites  []MissingSite `json:"possibly_missing_call_sites"`
	UnknownCallsInResultFiles int           `json:"unknown_calls_in_result_files"`
	BlindSpotData             bool          `json:"blind_spot_data"`
	Candidates                []string      `json:"candidates"`
	// Total is the number of stored nodes matching a query term (query
	// only, T030 G1); Nodes holds at most Limit of them, so Total minus
	// len(Nodes) were not shown.
	Total *int `json:"total,omitempty"`
	// Caps names the traversal caps that bound (callers and impact; empty
	// when none did), in traverse.CapOrder; Bounds says where each bound
	// and how many nodes it kept out (T030 D2). A bound cap means the
	// results stop at the cap, not that nothing lies beyond.
	Caps   []string         `json:"caps"`
	Bounds []traverse.Bound `json:"bounds"`
	// Ends are the reached functions and methods (the target included)
	// with no stored edge to follow: the walk ends there because nothing
	// is modelled, not because nothing calls them (T030 G10).
	Ends  []End  `json:"ends"`
	Error string `json:"error,omitempty"`
}

// End is one "? not modelled" end of a callers or impact walk: Node has
// no stored edge of the kinds the command follows, so the walk cannot
// say what lies beyond it.
type End struct {
	Node string `json:"node"`
	Text string `json:"text"`
}

// End lines of callers and impact (T030 G10). They state what the walk
// does not model, in the words of the chain's row CH11, narrowed to what
// callers and impact follow (they follow no JSX render, injection or
// passed-callback bridge at all).
const (
	CallersEndText = "? not modelled: no stored caller; dependency injection, framework callbacks, JSX renders, passed callbacks and dynamic calls are not followed by callers"
	ImpactEndText  = "? not modelled: no stored caller, import, reference or route; dependency injection, framework callbacks, JSX renders, passed callbacks and dynamic calls are not followed by impact"
)

// MaxDepth is the deepest callers or impact walk of one repository: the
// shared traversal cap. ProjectMaxDepth is the project walk's maximum,
// kept at the HTTP project endpoints' value (T030 D2: not lowered).
const (
	MaxDepth        = traverse.MaxDepth
	ProjectMaxDepth = 10
)

// walkEnds names the node an edge leads to and the frontier node it is
// followed from: callers and impact walk edges backwards, except a
// handler's exposes_route, which leads forward to its route.
func walkEnds(e contract.Edge) (next, previous string) {
	if e.Kind == "exposes_route" {
		return e.Target, e.Source
	}
	return e.Source, e.Target
}

// markEnds records each function or method of frontier that no edge of
// the step leads on from (led), whether or not the edge's other end was
// already visited: a function whose only caller is already in the result
// has a stored caller and is no end.
func markEnds(frontier []string, led map[string]bool, kindOf func(string) string, ended map[string]bool) {
	for _, id := range frontier {
		if k := kindOf(id); (k == "function" || k == "method") && !led[id] {
			ended[id] = true
		}
	}
}

// endsOf lists the ends among ids (the target, then the results in their
// order) with the command's end line.
func endsOf(command string, ids []string, ended map[string]bool) []End {
	text := CallersEndText
	if command == "impact" {
		text = ImpactEndText
	}
	out := []End{}
	for _, id := range ids {
		if ended[id] {
			out = append(out, End{Node: id, Text: text})
		}
	}
	return out
}

// Execute warns about stale facts while still returning the stored graph.
func Execute(ctx context.Context, s store.Store, opt Options) (Response, error) {
	out := Response{Command: opt.Command, Nodes: []Node{}, Results: []Result{}, Candidates: []string{}, PossiblyMissingCallSites: []MissingSite{},
		Caps: []string{}, Bounds: []traverse.Bound{}, Ends: []End{}}
	cfg, err := config.Load(opt.Root)
	if err != nil {
		return out, err
	}
	absRoot, err := filepath.Abs(opt.Root)
	if err != nil {
		return out, fmt.Errorf("resolving IDE root: %w", err)
	}
	// Source lines are read lazily, only for files of presented nodes.
	presenter := NewPresenter(absRoot, cfg.IDE)
	var changed []string
	var hashes map[string]string
	if opt.Fresh != nil {
		changed = append([]string{}, opt.Fresh.Changed...)
		if hashes, err = s.FileHashes(ctx, opt.Repository); err != nil {
			return out, err
		}
	} else if changed, hashes, err = index.FreshnessWithHashes(ctx, opt.Root, opt.Repository, s); err != nil {
		return out, err
	}
	runs, err := s.Runs(ctx, opt.Repository)
	if err != nil {
		return out, err
	}
	out.Freshness = Freshness{Indexed: len(runs) > 0, Changed: changed}
	filter := TargetFilter(opt.Term)
	filter.Kind, filter.File = opt.Kind, opt.File
	// Every match is read, so the total is exact (T030 G1); query shows
	// the first Limit of them in store order.
	filter.Limit = 0
	nodes, err := s.Nodes(ctx, opt.Repository, filter)
	if err != nil {
		return out, err
	}
	if opt.Command == "query" {
		total := len(nodes)
		out.Total = &total
		if opt.Limit > 0 && len(nodes) > opt.Limit {
			nodes = nodes[:opt.Limit]
		}
		for _, n := range nodes {
			out.Nodes = append(out.Nodes, presenter.Node(n, hashes[n.File]))
		}
		return out, nil
	}
	if opt.Command != "callers" && opt.Command != "impact" {
		return out, fmt.Errorf("unknown graph command %q", opt.Command)
	}
	if opt.Depth > MaxDepth {
		return out, fmt.Errorf("depth %d is above the traversal cap %d", opt.Depth, MaxDepth)
	}
	// A partial suffix match ("Items" in "listItems") is never the
	// target, only a candidate (T030 review, Resolve).
	resolving, partial := Resolve(opt.Term, nodes)
	if len(resolving) != 1 {
		out.Error = "no matching target"
		candidates := partial
		if len(resolving) > 1 {
			out.Error = "ambiguous target"
			candidates = resolving
		}
		for _, n := range candidates {
			out.Candidates = append(out.Candidates, n.Identity)
		}
		return out, nil
	}
	target := resolving[0]
	presented := presenter.Node(target, hashes[target.File])
	out.Target = &presented
	// byID holds only nodes read so far: the target, each level's new
	// endpoints and, for impact, result files' file nodes. A node that is not
	// stored stays absent and is skipped, as with a whole-graph map.
	byID := map[string]contract.Node{target.Identity: target}
	fetch := func(identities []string) error {
		var missing []string
		for _, id := range identities {
			if _, ok := byID[id]; !ok {
				missing = append(missing, id)
			}
		}
		nodes, err := s.NodesByIdentity(ctx, opt.Repository, missing)
		if err != nil {
			return err
		}
		for _, n := range nodes {
			byID[n.Identity] = n
		}
		return nil
	}
	visited := map[string]bool{target.Identity: true}
	seeds := []string{target.Identity}
	paths := map[string][]contract.Edge{target.Identity: {}}
	if opt.Command == "impact" && target.Kind == "file" {
		queue := []string{target.Identity}
		for len(queue) > 0 {
			edges, err := s.Edges(ctx, opt.Repository, queue, []string{"contains"}, store.Outgoing)
			if err != nil {
				return out, err
			}
			queue = nil
			for _, e := range edges {
				if !visited[e.Target] {
					visited[e.Target] = true
					seeds = append(seeds, e.Target)
					queue = append(queue, e.Target)
					paths[e.Target] = []contract.Edge{}
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
	files := map[string]bool{target.File: true}
	depth := opt.Depth
	if opt.Command == "callers" && target.Kind == "route" && depth < 2 {
		depth = 2
	}
	ended := map[string]bool{}
	// The walk is the shared breadth-first walk (internal/traverse, T030
	// D2): the same depth, reached-node and fan-out caps as change impact
	// and the chain, each reported when it binds.
	walk := &traverse.Walk{
		Depth:   depth,
		Visited: func(id string) bool { return visited[id] },
		Candidates: func(frontier []string, _ int, _ bool) ([]traverse.Candidate, error) {
			edges, err := levelEdges(ctx, s, opt.Repository, opt.Command, frontier, kinds, func(id string) string { return byID[id].Kind })
			if err != nil {
				return nil, err
			}
			var reached []string
			led := map[string]bool{}
			for _, e := range edges {
				next, previous := walkEnds(e)
				led[previous] = true
				if !visited[next] {
					reached = append(reached, next)
				}
			}
			markEnds(frontier, led, func(id string) string { return byID[id].Kind }, ended)
			if err := fetch(reached); err != nil {
				return nil, err
			}
			var cands []traverse.Candidate
			for _, e := range edges {
				next, previous := walkEnds(e)
				// A node that is not stored is skipped, as with a
				// whole-graph map.
				if _, ok := byID[next]; ok {
					cands = append(cands, traverse.Candidate{From: previous, To: next, Edge: e})
				}
			}
			return cands, nil
		},
		Reach: func(c traverse.Candidate, distance int) {
			visited[c.To] = true
			n := byID[c.To]
			p := append(append([]contract.Edge{}, paths[c.From]...), c.Edge)
			paths[c.To] = p
			result := Result{Node: presenter.Node(n, hashes[n.File]), Distance: distance, Path: p}
			confidence := 1.0
			for _, edge := range p {
				if edge.Confidence < confidence {
					confidence = edge.Confidence
				}
				if edge.Provenance == "inferred" {
					result.Provenance = "inferred"
				}
			}
			if result.Provenance != "" {
				result.Confidence = &confidence
			}
			out.Results = append(out.Results, result)
			files[n.File] = true
		},
	}
	if err := walk.Run(seeds); err != nil {
		return out, err
	}
	out.Caps, out.Bounds = walk.Caps(), walk.Bounds()
	sort.Slice(out.Results, func(i, j int) bool {
		a, b := out.Results[i], out.Results[j]
		if a.Distance != b.Distance {
			return a.Distance < b.Distance
		}
		return a.Node.Identity < b.Node.Identity
	})
	ids := []string{target.Identity}
	for _, r := range out.Results {
		ids = append(ids, r.Node.Identity)
	}
	out.Ends = endsOf(opt.Command, ids, ended)
	if opt.Command == "impact" {
		var fileIDs []string
		for file := range files {
			fileIDs = append(fileIDs, fileIdentity(file))
		}
		if err := fetch(fileIDs); err != nil {
			return out, err
		}
		for file := range files {
			n := byID[fileIdentity(file)]
			if v, ok := n.Attributes["unresolved_calls_unknown"]; ok {
				count, err := strconv.Atoi(fmt.Sprint(v))
				if err == nil && count > 0 {
					out.UnknownCallsInResultFiles += count
				}
			}
		}
		// Blind spots span the whole repository, so impact alone reads every
		// file node, and only once its target is known.
		fileNodes, err := s.Nodes(ctx, opt.Repository, store.Query{Kind: "file"})
		if err != nil {
			return out, err
		}
		if err := blindSpots(ctx, s, opt.Repository, fileNodes, &out); err != nil {
			return out, err
		}
	}
	return out, nil
}

// TargetFilter is the lookup of a query term or a callers or impact
// target: a canonical identity matches that node only; anything else is
// an exact name or a case-sensitive qualified-name suffix. A callers or
// impact target is then chosen by Resolve (a partial suffix is a
// candidate only); the MCP server resolves names with the same filter
// and rule (T030 D13).
func TargetFilter(term string) store.Query {
	if IsIdentity(term) {
		return store.Query{Identity: term}
	}
	return store.Query{Term: term}
}

// IsIdentity reports whether term parses as a canonical node identity.
func IsIdentity(term string) bool {
	_, _, _, err := contract.ParseIdentity(term)
	return err == nil
}

// levelEdges reads the edges one level of a callers or impact traversal
// follows from frontier in repository: incoming edges of kinds, a route's
// incoming calls_route edges, a client call's enclosing symbol (incoming
// contains) and, for impact, a handler's routes (outgoing exposes_route).
// kindOf gives a frontier node's kind. Edges are in (kind, source, target)
// order, so traversals are deterministic.
func levelEdges(ctx context.Context, s store.Store, repository, command string, frontier, kinds []string, kindOf func(string) string) ([]contract.Edge, error) {
	edges, err := s.Edges(ctx, repository, frontier, kinds, store.Incoming)
	if err != nil {
		return nil, err
	}
	var routes, clients []string
	for _, identity := range frontier {
		if kindOf(identity) == "route" {
			routes = append(routes, identity)
		}
		if kindOf(identity) == "client_call" {
			clients = append(clients, identity)
		}
	}
	if len(routes) > 0 {
		extra, err := s.Edges(ctx, repository, routes, []string{"calls_route"}, store.Incoming)
		if err != nil {
			return nil, err
		}
		edges = append(edges, extra...)
	}
	if len(clients) > 0 {
		extra, err := s.Edges(ctx, repository, clients, []string{"contains"}, store.Incoming)
		if err != nil {
			return nil, err
		}
		edges = append(edges, extra...)
	}
	if command == "impact" {
		extra, err := s.Edges(ctx, repository, frontier, []string{"exposes_route"}, store.Outgoing)
		if err != nil {
			return nil, err
		}
		edges = append(edges, extra...)
	}
	sort.Slice(edges, func(i, j int) bool {
		a, b := edges[i], edges[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		return a.Target < b.Target
	})
	return edges, nil
}

func fileIdentity(file string) string { id, _ := contract.Identity("file", file, file); return id }
