package server

import (
	"context"
	"fmt"
	"sort"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/projectlink"
	"github.com/graphvein/graphvein/internal/store"
)

// This file holds the architecture view of a project member (serve
// --project; TOP-2 of 2026-11-18, row CE8): the stored link pass's
// cross-repository calls_route links, drawn as links to a muted card for
// the other member, never as calls without a link. Nothing here creates a
// link: every CE8 link is a stored project link, shown with the
// cross-repository assumption it carries. A single-repository server has
// no project, and its answers do not change.

// remoteCardPrefix names the card of another project member. A component
// is a directory and a database card is "database:<set>", so the prefix
// cannot collide with either.
const remoteCardPrefix = "repository:"

// remoteCard is the card id of member repository.
func remoteCard(repository string) string { return remoteCardPrefix + repository }

// archProjectDoc is the "project" part of an architecture answer in
// project mode; absent on a single-repository server.
type archProjectDoc struct {
	Project string `json:"project"`
	// LinkPass is the pass the links come from (state none without one).
	LinkPass projectPassBrief `json:"link_pass"`
	// LinksPrecision is the scope label of the project links: never
	// precise (docs/extract/precision.md "Cross-repository links").
	LinksPrecision scopeLabel `json:"links_precision"`
	// Members are the cards of the other members the links reach.
	Members []archRemoteMember `json:"members"`
	// Across counts, per component, the calls the pass links into another
	// member, with the repository's own count of calls without a link
	// (secondary: what the repository alone says).
	Across []archAcross `json:"across"`
	// Assumptions are the distinct assumption texts of the drawn links.
	Assumptions []string `json:"assumptions"`
}

// archRemoteMember is the muted card of another member.
type archRemoteMember struct {
	ID         string `json:"id"`
	Repository string `json:"repository"`
	Name       string `json:"name"`
	Row        string `json:"row"`
	// CallsOut are this repository's calls linked to the member's routes;
	// CallsIn are the member's calls linked to this repository's routes.
	CallsOut int `json:"calls_out"`
	CallsIn  int `json:"calls_in"`
	// OperationsOut are this repository's OpenAPI operations a stored
	// documents_route project link connects to the member's routes;
	// OperationsIn are the member's operations documenting this
	// repository's routes (row CE8 (R1)). Each operation counts once per
	// card, however many routes it documents. Without them the card would
	// show fewer cross-repository links than the pass stored.
	OperationsOut int `json:"operations_out"`
	OperationsIn  int `json:"operations_in"`
}

// archAcross is one component's calls linked across repositories.
type archAcross struct {
	Component string `json:"component"`
	// LinkedAcross are the component's calls without a link in the
	// repository that a stored project link connects to another member.
	LinkedAcross int `json:"linked_across"`
	// RepositoryNotLinked is the component's calls without a link in the
	// repository alone (the single-repository ghost's count).
	RepositoryNotLinked int `json:"repository_not_linked"`
	// OperationsAcross are the component's OpenAPI operations without a
	// route in the repository that a stored documents_route project link
	// connects to another member's route; RepositoryNoRoute is the
	// repository's own "documented, no route" count (TOP-2 R1 of 2026-11-18).
	OperationsAcross  int `json:"operations_across"`
	RepositoryNoRoute int `json:"repository_no_route"`
	// Repositories are the members the calls and operations link to.
	Repositories []string `json:"repositories"`
}

// archProjectIn is what the link pass says about one member, read before
// the links are built.
type archProjectIn struct {
	doc *archProjectDoc
	// out are the member's calls_route links into another member, in the
	// pass's key order; in are the other members' links into its routes.
	out, in []store.ProjectLink
	stale   map[store.ProjectLinkKey][]string
	// remote holds the other members' nodes the links name.
	remote map[store.ProjectNodeRef]contract.Node
	// across marks the member's calls with an outgoing link.
	across map[string]bool
	// targets are the members each call links to.
	targets map[string][]string
	// docsOut and docsIn are the documents_route project links from this
	// member's operations and into its routes; opsAcross marks the
	// operations with an outgoing one, opsTo the members per component;
	// opsAcrossBy and opsAlone count per component (filled by
	// documentedSecurity).
	docsOut, docsIn       []store.ProjectLink
	opsAcross             map[string]bool
	opsTo                 map[string][]string
	opsAcrossBy, opsAlone map[string]int
}

// archProject reads the stored link pass for repository's architecture
// view; nil without a project.
func (ps *projectState) archProject(ctx context.Context, repository string) (*archProjectIn, error) {
	if ps == nil {
		return nil, nil
	}
	view, err := ps.view(ctx)
	if err != nil {
		return nil, err
	}
	label, err := ps.linksLabel(ctx, repository)
	if err != nil {
		return nil, err
	}
	in := &archProjectIn{doc: &archProjectDoc{Project: ps.project.Name, LinkPass: projectPassBrief{State: "none", StaleReasons: []string{}}, LinksPrecision: label,
		Members: []archRemoteMember{}, Across: []archAcross{}, Assumptions: []string{}},
		stale: map[store.ProjectLinkKey][]string{}, remote: map[store.ProjectNodeRef]contract.Node{}, across: map[string]bool{}, targets: map[string][]string{},
		opsAcross: map[string]bool{}, opsTo: map[string][]string{}, opsAcrossBy: map[string]int{}, opsAlone: map[string]int{}}
	if !view.Stored {
		return in, nil
	}
	in.doc.LinkPass.State, in.doc.LinkPass.Pass = stateFresh, view.Set.Pass
	in.doc.LinkPass.StaleReasons = append(in.doc.LinkPass.StaleReasons, view.PassReasons()...)
	if len(in.doc.LinkPass.StaleReasons) > 0 {
		in.doc.LinkPass.State = stateStale
	}
	want := map[string]map[string]bool{}
	need := func(r store.ProjectEndpoint) {
		if want[r.Repository] == nil {
			want[r.Repository] = map[string]bool{}
		}
		want[r.Repository][r.Identity] = true
	}
	for _, l := range view.Set.Links {
		if l.Source.Repository == l.Target.Repository {
			continue
		}
		if l.Kind == projectlink.KindDocumentsRoute {
			_, src := ps.members[l.Source.Repository]
			_, dst := ps.members[l.Target.Repository]
			switch {
			case !src || !dst:
			case l.Source.Repository == repository:
				in.docsOut = append(in.docsOut, l)
				in.opsAcross[l.Source.Identity] = true
				in.targets[l.Source.Identity] = append(in.targets[l.Source.Identity], l.Target.Repository)
			case l.Target.Repository == repository:
				in.docsIn = append(in.docsIn, l)
			}
			continue
		}
		if l.Kind != projectlink.KindCallsRoute {
			continue
		}
		if _, member := ps.members[l.Source.Repository]; !member {
			continue
		}
		if _, member := ps.members[l.Target.Repository]; !member {
			continue
		}
		switch repository {
		case l.Source.Repository:
			in.out = append(in.out, l)
			in.across[l.Source.Identity] = true
			in.targets[l.Source.Identity] = append(in.targets[l.Source.Identity], l.Target.Repository)
			need(l.Target)
		case l.Target.Repository:
			in.in = append(in.in, l)
			need(l.Source)
		default:
			continue
		}
		if reasons := view.StaleReasons(l); len(reasons) > 0 {
			in.stale[l.Key()] = reasons
		}
	}
	repos := make([]string, 0, len(want))
	for repo := range want {
		repos = append(repos, repo)
	}
	sort.Strings(repos)
	for _, repo := range repos {
		ids := make([]string, 0, len(want[repo]))
		for id := range want[repo] {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		nodes, err := ps.opt.Store.NodesByIdentity(ctx, repo, ids)
		if err != nil {
			return nil, fmt.Errorf("reading nodes of %s: %w", repo, err)
		}
		for _, n := range nodes {
			in.remote[store.ProjectNodeRef{Repository: repo, Identity: n.Identity}] = n
		}
	}
	return in, nil
}

// projectLinks is row CE8: one HTTP link per (component, other member)
// for the stored calls_route project links, outgoing (this repository's
// calls) and incoming (the other member's calls to its routes), with the
// assumption the pass stored. The protocol is AP1's, auth the AA rows'
// reading of the route's stored facts (documented security only for this
// repository's routes), TLS unknown (ATD).
func projectLinks(px *archProjectIn, m *archModel, g *archGraph, nodes map[string]contract.Node, documented *documentation, testSource func(string) bool, scope string) []*archLink {
	if px == nil {
		return nil
	}
	links := map[string]*archLink{}
	var order []string
	auths := map[string][]memberAuth{}
	cards := map[string]*archRemoteMember{}
	card := func(repo string) *archRemoteMember {
		c, ok := cards[repo]
		if !ok {
			c = &archRemoteMember{ID: remoteCard(repo), Repository: repo, Name: repo + " (other repository)", Row: "CE8"}
			cards[repo] = c
		}
		return c
	}
	add := func(l store.ProjectLink, outgoing bool) {
		local, remote := l.Source, l.Target
		if !outgoing {
			local, remote = l.Target, l.Source
		}
		component := m.componentOf(local.Identity)
		if component == "" {
			return
		}
		remoteNode, remoteOK := px.remote[store.ProjectNodeRef{Repository: remote.Repository, Identity: remote.Identity}]
		if scope == scopeProduction {
			test := outgoing && testSource(local.Identity) || !outgoing && remoteOK && remoteNode.Attributes["scope"] == "test"
			if test {
				g.testEdges[familyHTTP]++
				return
			}
		}
		tagRow("CE8")
		source, target := component, remoteCard(remote.Repository)
		if !outgoing {
			source, target = target, source
		}
		key := linkKey(familyHTTP, source, target)
		al, ok := links[key]
		if !ok {
			al = &archLink{ID: key, Family: familyHTTP, Row: "CE8", Source: source, Target: target, Repository: remote.Repository, ByKind: map[string]int{}, Tiers: map[string]int{},
				Methods: []string{}, Members: []archMember{}, Origins: []archOrigin{}, Assumptions: []string{}, Assumes: []string{}, weakest: 2}
			links[key] = al
			order = append(order, key)
		}
		al.Count++
		al.ByKind[l.Kind]++
		tier, confidence := tierOf(contract.Edge{Provenance: l.Provenance, Confidence: l.Confidence})
		al.Tiers[tier]++
		if confidence < al.weakest {
			al.weakest, al.Weakest = confidence, tier
		}
		if l.Assumption != "" {
			al.Assumptions = distinct(append(al.Assumptions, l.Assumption))
		}
		al.Assumes = distinct(append(al.Assumes, l.Assumes...))
		al.StaleReasons = distinct(append(al.StaleReasons, px.stale[l.Key()]...))
		member := archMember{Source: l.Source.Identity, Target: l.Target.Identity, Tier: tier}
		var route, call contract.Node
		var routeOK, callOK bool
		var docs [][]string
		if outgoing {
			member.TargetRepository = remote.Repository
			route, routeOK = remoteNode, remoteOK
			call, callOK = nodes[local.Identity]
			card(remote.Repository).CallsOut++
		} else {
			member.SourceRepository = remote.Repository
			route, routeOK = nodes[local.Identity]
			call, callOK = remoteNode, remoteOK
			docs = documented.security[local.Identity]
			card(remote.Repository).CallsIn++
		}
		if routeOK {
			member.Method, member.Path = stringAttr(route, "method"), stringAttr(route, "pattern")
			member.FullPath = routePath(route, scope)
			member.PrefixPath = prefixPath(route, scope)
			if member.Method != nil {
				al.Methods = append(al.Methods, *member.Method)
			}
		}
		if callOK && outgoing {
			al.Origins = append(al.Origins, callOrigins(call)...)
		}
		a := routeAuth(route, routeOK, docs, call, callOK)
		member.Auth = a.value
		al.Members = append(al.Members, member)
		auths[key] = append(auths[key], a)
	}
	for _, l := range px.out {
		add(l, true)
	}
	for _, l := range px.in {
		add(l, false)
	}
	// CE8 (R1): the documents_route project links count on the card too,
	// as the documents rows do (documentedSecurity): one per distinct
	// operation, only when this repository's end is in a component.
	seenOps := map[string]bool{}
	countOp := func(l store.ProjectLink, outgoing bool) {
		local, remote := l.Source, l.Target
		if !outgoing {
			local, remote = l.Target, l.Source
		}
		if m.componentOf(local.Identity) == "" {
			return
		}
		key := fmt.Sprintf("%t|%s|%s|%s", outgoing, remote.Repository, l.Source.Repository, l.Source.Identity)
		if seenOps[key] {
			return
		}
		seenOps[key] = true
		if outgoing {
			card(remote.Repository).OperationsOut++
		} else {
			card(remote.Repository).OperationsIn++
		}
	}
	for _, l := range px.docsOut {
		countOp(l, true)
	}
	for _, l := range px.docsIn {
		countOp(l, false)
	}
	var out []*archLink
	assumptions := []string{}
	for _, key := range order {
		l := links[key]
		l.Methods = distinct(l.Methods)
		l.Origins = distinctOrigins(l.Origins)
		tagRow("AP1")
		l.Cells.Protocol = archCell{Value: "HTTP", State: cellProven, Source: "proven: route and client-call facts", Rows: []string{"AP1"}}
		l.Cells.Auth = aggregateAuth(auths[key])
		tagRow("ATD")
		l.Cells.TLS = archCell{Value: "TLS ?", State: cellUnknown, Source: "unknown: transport security comes from deployment; no deploy config proves this hop", Rows: []string{"ATD"}}
		assumptions = append(assumptions, l.Assumptions...)
		out = append(out, l)
	}
	repos := make([]string, 0, len(cards))
	for repo := range cards {
		repos = append(repos, repo)
	}
	sort.Strings(repos)
	for _, repo := range repos {
		px.doc.Members = append(px.doc.Members, *cards[repo])
	}
	px.doc.Assumptions = distinct(assumptions)
	return out
}

// remoteMemberID is the chain member id of a node of another member: the
// card prefix keeps it apart from this repository's identities.
func remoteMemberID(r store.ProjectEndpoint) string {
	return remoteCardPrefix + r.Repository + "/" + r.Identity
}

// continuesIn is row CH13's end line under a member of another repository.
func continuesIn(repo, assumption string) string {
	text := "continues in " + repo + ": the chain stops at the stored project link; " + repo + " is not walked from here"
	if assumption != "" {
		text += " (" + assumption + ")"
	}
	return text
}

// projectChainSteps is row CH13 for a node chain in project mode: a
// client call member with a stored project link gets the other member's
// route as the next step downstream, a route member the other member's
// linked calls upstream, each in that member's muted card, with an end
// line "continues in <repo>". The other repository is not walked: the
// chain goes exactly as far as the stored link.
func projectChainSteps(side *chainSide, dir string, depth int, scope string, px *archProjectIn) {
	if side == nil || px == nil {
		return
	}
	type hop struct {
		link   store.ProjectLink
		remote store.ProjectEndpoint
	}
	from := map[string][]hop{}
	fromKind := "client_call"
	if dir == chainDirDown {
		for _, l := range px.out {
			from[l.Source.Identity] = append(from[l.Source.Identity], hop{l, l.Target})
		}
	} else {
		fromKind = "route"
		for _, l := range px.in {
			from[l.Target.Identity] = append(from[l.Target.Identity], hop{l, l.Source})
		}
	}
	if len(from) == 0 {
		return
	}
	added := map[string]*chainMember{}
	var newOrder []string
	newStep := map[string]int{}
	for i := range side.Steps {
		step := side.Steps[i]
		if step.Step+1 > depth {
			continue
		}
		for _, parent := range step.Members {
			if parent.Kind != fromKind {
				continue
			}
			for _, h := range from[parent.ID] {
				remoteNode, remoteOK := px.remote[store.ProjectNodeRef{Repository: h.remote.Repository, Identity: h.remote.Identity}]
				if scope == scopeProduction && remoteOK && remoteNode.Attributes["scope"] == "test" {
					// CH10 as in this repository: the other member's
					// test-scope calls stay out of the default scope.
					side.HiddenTest++
					continue
				}
				id := remoteMemberID(h.remote)
				tier, confidence := tierOf(contract.Edge{Provenance: h.link.Provenance, Confidence: h.link.Confidence})
				cm, ok := added[id]
				if !ok {
					tagRow("CH13")
					repo := h.remote.Repository
					cm = &chainMember{ID: id, Kind: fromKindOther(fromKind), Name: shortName(h.remote.Identity), QualifiedName: shortName(h.remote.Identity), Row: "CH13",
						Tier: parent.Tier, TierStep: parent.TierStep, Parents: []string{}, step: step.Step + 1, confidence: parent.confidence, Repository: &repo}
					if n, ok := remoteNode, remoteOK; ok {
						cm.Kind, cm.Name, cm.QualifiedName = n.Kind, n.Name, n.QualifiedName
						if n.File != "" {
							f := n.File
							cm.File = &f
						}
						if n.Span != nil {
							line := n.Span.StartLine
							cm.Line = &line
						}
						switch n.Kind {
						case "route":
							cm.Method, cm.Path = stringAttr(n, "method"), stringAttr(n, "pattern")
							cm.FullPath = routePath(n, scopeProduction)
							cm.PrefixPath = prefixPath(n, scopeProduction)
						case "client_call":
							cm.Method, cm.Status = stringAttr(n, "method"), stringAttr(n, "status")
						}
					}
					if confidence < cm.confidence {
						cm.Tier, cm.TierStep, cm.confidence = tier, step.Step+1, confidence
					}
					added[id] = cm
					newOrder = append(newOrder, id)
					newStep[id] = step.Step + 1
				}
				cm.Parents = distinct(append(cm.Parents, parent.ID))
				wording := "calls route in " + h.remote.Repository + " (project link)"
				if dir == chainDirUp {
					wording = "called by client call in " + h.remote.Repository + " (project link)"
				}
				evidence := h.link.Evidence
				if evidence == nil {
					evidence = []contract.Evidence{}
				}
				side.Edges = append(side.Edges, chainEdge{From: parent.ID, To: id, Kind: h.link.Kind, Row: "CH13", Wording: wording, Tier: tier, Evidence: evidence})
				end := chainEnd{Member: id, Row: "CH13", Text: continuesIn(h.remote.Repository, h.link.Assumption)}
				if !containsEnd(side.Ends, end) {
					side.Ends = append(side.Ends, end)
				}
			}
		}
	}
	for _, id := range newOrder {
		at := newStep[id]
		for len(side.Steps) <= at {
			side.Steps = append(side.Steps, chainStep{Step: len(side.Steps), Members: []chainMember{}, Bounds: []chainBound{}, Map: chainStepMap{Shown: []string{}}})
		}
		side.Steps[at].Members = append(side.Steps[at].Members, *added[id])
		side.Members++
	}
}

// fromKindOther is the kind at the other end of a CH13 hop when the other
// member no longer stores the node.
func fromKindOther(kind string) string {
	if kind == "client_call" {
		return "route"
	}
	return "client_call"
}

func containsEnd(list []chainEnd, e chainEnd) bool {
	for _, x := range list {
		if x == e {
			return true
		}
	}
	return false
}
