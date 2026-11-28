package server

import (
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/clients"
	"github.com/graphvein/graphvein/internal/store"
)

// This file holds the component links (rows CE1..CE6, CED) and their
// annotation cells (rows AP, AA, AT, AE) of the architecture view (T026,
// ADR 0079). Links bundle stored edges per (source component, target
// component, family); nothing here creates a link from anything but a
// stored edge.

// linkRows are the component-link rows in table order.
var linkRows = []archRow{{ID: "CE1"}, {ID: "CE2"}, {ID: "CE3"}, {ID: "CE4"}, {ID: "CE5"}, {ID: "CE6"}, {ID: "CE7"}, {ID: "CE8"}, {ID: "CED"}}

// annotationRows are the annotation rows in table order. Pending rows read
// facts a later implementation step adds; until then their cells stay on the
// default branch (AAD, ATD, AED) with its reason.
var annotationRows = []archRow{
	{ID: "AP1"}, {ID: "AP2"}, {ID: "AP3"}, {ID: "AP4"}, {ID: "AP5"}, {ID: "APD"},
	{ID: "AA1"}, {ID: "AA2"}, {ID: "AA3"}, {ID: "AA4"}, {ID: "AA5"}, {ID: "AA6"}, {ID: "AAD"},
	{ID: "AT1"}, {ID: "AT2"}, {ID: "AT3"}, {ID: "ATD"},
	{ID: "AE1"}, {ID: "AE2"}, {ID: "AE3"}, {ID: "AED"},
}

// Link families.
const (
	familyHTTP      = "http"
	familyIPC       = "tauri_ipc"
	familySQL       = "sql"
	familyInProcess = "in_process"
	// familyServerAction is a Next.js server action reached from another
	// component (row CE7): a POST to the page that renders the action.
	familyServerAction = "server_action"
)

// Cell states. A cell is "proven" only for a proven source; "as_written",
// "documented" and "as_configured" never render as proven (gate: glyph).
const (
	cellProven        = "proven"
	cellAsWritten     = "as_written"
	cellDocumented    = "documented"
	cellNotApplicable = "not_applicable"
	cellUnknown       = "unknown"
	cellMixed         = "mixed"
)

// archCell is one annotation cell: its value, the label of its source,
// the row that produced it and, when mixed, the members per value.
type archCell struct {
	Value  string         `json:"value"`
	State  string         `json:"state"`
	Source string         `json:"source"`
	Rows   []string       `json:"rows"`
	Counts map[string]int `json:"counts,omitempty"`
}

// archCells are the three cells of a link's glyph.
type archCells struct {
	Protocol archCell `json:"protocol"`
	Auth     archCell `json:"auth"`
	TLS      archCell `json:"tls"`
}

// archMember is one stored edge of an HTTP or IPC link.
type archMember struct {
	Source string  `json:"source"`
	Target string  `json:"target"`
	Method *string `json:"method"`
	Path   *string `json:"path"`
	// FullPath is an HTTP member's route path as served (its stored
	// paths, router prefixes and FastAPI's trailing slash included), as
	// the chain's full_path; absent when the route has none.
	FullPath *string `json:"full_path,omitempty"`
	// PrefixPath is an HTTP member's route path whose prefix the code
	// does not fix ("…" marks it; T033 row UP-33), set only when FullPath
	// is absent.
	PrefixPath *string `json:"prefix_path,omitempty"`
	// Assumptions are the assumptions an HTTP member's link rests on
	// (origin, UP-V1, UP-V2), read from its client call; absent when
	// there is none.
	Assumptions []clients.LinkAssumption `json:"assumptions,omitempty"`
	Tier        string                   `json:"tier"`
	Auth        string                   `json:"auth,omitempty"`
	// SourceRepository and TargetRepository name the other project member
	// of a CE8 member (serve --project): its source call, or its target
	// route, is a node of that repository. Absent otherwise.
	SourceRepository string `json:"source_repository,omitempty"`
	TargetRepository string `json:"target_repository,omitempty"`
}

// archOrigin is an absolute origin named by a client call or a spec
// (AT3): shown as documented, never as the link's TLS.
type archOrigin struct {
	Origin string `json:"origin"`
	Source string `json:"source"`
	Row    string `json:"row"`
}

// archLink is one bundled component link.
type archLink struct {
	ID        string         `json:"id"`
	Family    string         `json:"family"`
	Row       string         `json:"row"`
	Source    string         `json:"source"`
	Target    string         `json:"target"`
	Count     int            `json:"count"`
	BackCount int            `json:"back_count"`
	ByKind    map[string]int `json:"by_kind"`
	Tiers     map[string]int `json:"tiers"`
	Weakest   string         `json:"weakest"`
	Methods   []string       `json:"methods"`
	Members   []archMember   `json:"members"`
	Origins   []archOrigin   `json:"origins"`
	Cells     archCells      `json:"cells"`
	// Repository, Assumptions, Assumes and StaleReasons belong to a CE8
	// link (serve --project): the other member, the distinct assumption
	// texts its stored project links carry, their row assumptions and why
	// some are stale. Absent on every other link.
	Repository   string   `json:"repository,omitempty"`
	Assumptions  []string `json:"assumptions,omitempty"`
	Assumes      []string `json:"assumes,omitempty"`
	StaleReasons []string `json:"stale_reasons,omitempty"`

	weakest float64
	tools   []string
	// serverCalls marks a CE7 link with a direct call not proven to cross
	// the client boundary (it may run in process on the server).
	serverCalls bool
}

// archDatabase is one database card per schema set (CE3).
type archDatabase struct {
	ID        string          `json:"id"`
	Set       string          `json:"set"`
	Tool      *string         `json:"tool"`
	Tables    int             `json:"tables"`
	Engine    archCell        `json:"engine"`
	Evidence  []string        `json:"evidence"`
	Accessors []archAccessors `json:"accessors"`
}

// archAccessors are one component's table accesses to a database card.
type archAccessors struct {
	Component string `json:"component"`
	Link      string `json:"link"`
	Reads     int    `json:"reads"`
	Writes    int    `json:"writes"`
	// Builds are the table edges whose owner's table_access entry has
	// role builds (a query builder's entry): worded "builds a query on"
	// as on the chain (CH6) and the Flow tab, and not counted
	// in Reads or Writes.
	Builds     int `json:"builds"`
	MayAlsoUse int `json:"may_also_use"`
}

// archStub is a component's single muted SDK stub (CE6, O2 (a)).
type archStub struct {
	Component string   `json:"component"`
	Row       string   `json:"row"`
	Packages  int      `json:"packages"`
	Text      string   `json:"text"`
	Protocol  archCell `json:"protocol"`
}

// archGhost is a component's "not linked (n)" ghost (CE1, CE2).
type archGhost struct {
	Component string         `json:"component"`
	Family    string         `json:"family"`
	Row       string         `json:"row"`
	Total     int            `json:"total"`
	ByStatus  map[string]int `json:"by_status"`
}

// archDocuments counts documents_route and calls_operation edges per
// component pair (CE5): no map link, a "documents" row of the Links tab.
type archDocuments struct {
	Source string         `json:"source"`
	Target string         `json:"target"`
	Row    string         `json:"row"`
	ByKind map[string]int `json:"by_kind"`
}

// archGraph is everything buildLinks derives.
type archGraph struct {
	links     []*archLink
	databases []*archDatabase
	stubs     []archStub
	ghosts    []archGhost
	documents []archDocuments
	testEdges map[string]int
	notDrawn  map[string]int
	notProven []archNotProven
}

// unlinkedStatuses are the client-call statuses of CE1's ghost.
var unlinkedStatuses = map[string]bool{"dynamic": true, "unmatched": true, "ambiguous": true, "test_only": true}

func unlinkedStatus(n contract.Node) bool {
	if n.Kind == "ipc_call" {
		// A wrapper's forwarding call is no request of its own in either
		// family: its requests are the wrapper's call sites.
		return statusOf(n) != "linked" && statusOf(n) != "wrapper"
	}
	return unlinkedStatuses[statusOf(n)]
}

// notDrawnKinds are the edge kinds CED counts besides the families' own
// kinds (edges inside one component). Other stored kinds (contains,
// exposes_route, exposes_command, ...) are structure, not links, and are
// not read by this view.
var notDrawnKinds = []string{"renders", "injects", "routes_to", "loads_children", "passes_function", "uses_config", "foreign_key", "alters"}

// tierOf names an edge's tier: "fact" or "inferred <confidence>", the
// confidence written by contract.ConfidenceText.
func tierOf(e contract.Edge) (string, float64) {
	if e.Provenance == "fact" {
		return "fact", 1
	}
	return e.Provenance + " " + contract.ConfidenceText(e.Confidence), e.Confidence
}

func linkKey(family, source, target string) string {
	return family + ":" + source + "->" + target
}

// buildLinks applies rows CE1..CE8 and CED and the annotation rows; px is
// the link pass of a project member (nil on a single-repository server).
func buildLinks(rd *reader, m *archModel, scope string, px *archProjectIn) (*archGraph, error) {
	g := &archGraph{links: []*archLink{}, databases: []*archDatabase{}, stubs: []archStub{}, ghosts: []archGhost{}, documents: []archDocuments{},
		testEdges: map[string]int{}, notDrawn: map[string]int{}, notProven: []archNotProven{}}
	links := map[string]*archLink{}
	nodes := map[string]contract.Node{}
	for _, kind := range []string{"route", "client_call", "ipc_command", "ipc_call", "table"} {
		for _, n := range m.nodesByKind[kind] {
			nodes[n.Identity] = n
		}
	}
	// testSource is the source side of the default scope: a node with
	// scope test or a file test scope classifies.
	testSource := func(id string) bool {
		if n, ok := nodes[id]; ok && n.Attributes["scope"] == "test" {
			return true
		}
		file, ok := identityFile(id)
		return ok && m.testFile[file]
	}
	link := func(family, row, source, target string) *archLink {
		key := linkKey(family, source, target)
		l, ok := links[key]
		if !ok {
			l = &archLink{ID: key, Family: family, Row: row, Source: source, Target: target, ByKind: map[string]int{}, Tiers: map[string]int{}, Methods: []string{}, Members: []archMember{}, Origins: []archOrigin{}, weakest: 2}
			links[key] = l
		}
		return l
	}
	addTier := func(l *archLink, e contract.Edge) string {
		tier, confidence := tierOf(e)
		l.Tiers[tier]++
		if confidence < l.weakest {
			l.weakest, l.Weakest = confidence, tier
		}
		return tier
	}
	// documented holds each route's operations' security names (AA2).
	documented, err := documentedSecurity(rd, m, g, px)
	if err != nil {
		return nil, err
	}
	// CE1 and CE2: calls_route and invokes_command.
	linkedCalls := map[string]bool{}
	for _, f := range []struct{ kind, family, row string }{{"calls_route", familyHTTP, "CE1"}, {"invokes_command", familyIPC, "CE2"}} {
		edges, err := rd.s.EdgesOfKind(rd.ctx, rd.repo, f.kind)
		if err != nil {
			return nil, fmt.Errorf("reading %s edges: %w", f.kind, err)
		}
		for _, e := range edges {
			linkedCalls[e.Source] = true
			a, b := m.componentOf(e.Source), m.componentOf(e.Target)
			if a == "" || b == "" {
				continue
			}
			if a == b {
				tagRow("CED/inside")
				g.notDrawn[e.Kind]++
				continue
			}
			if scope == scopeProduction && testSource(e.Source) {
				g.testEdges[f.family]++
				continue
			}
			tagRow(f.row)
			l := link(f.family, f.row, a, b)
			l.Count++
			l.ByKind[e.Kind]++
			tier := addTier(l, e)
			member := archMember{Source: e.Source, Target: e.Target, Tier: tier}
			if target, ok := nodes[e.Target]; ok {
				if f.family == familyHTTP {
					member.Method, member.Path = stringAttr(target, "method"), stringAttr(target, "pattern")
					member.FullPath = routePath(target, scope)
					member.PrefixPath = prefixPath(target, scope)
				} else {
					member.Path = stringAttr(target, "command")
				}
				if member.Method != nil {
					l.Methods = append(l.Methods, *member.Method)
				}
			}
			if call, ok := nodes[e.Source]; ok && f.family == familyHTTP {
				member.Assumptions = linkAssumptions(call, e.Target)
			}
			l.Members = append(l.Members, member)
			if call, ok := nodes[e.Source]; ok {
				l.Origins = append(l.Origins, callOrigins(call)...)
			}
			if f.family == familyHTTP {
				for _, o := range documented.origins[e.Target] {
					tagRow("AT3/server")
					l.Origins = append(l.Origins, o)
				}
			}
		}
	}
	// CE7: calls_action from an action call in A to a server action of a
	// Next.js application in B (TOP-2 of 2026-11-18). An action call is
	// stored only when linked (docs/extract/server-actions.md), so there
	// is no ghost; a call inside the action's own component stays inside.
	if err := actionLinks(rd, m, g, link, addTier, testSource, scope); err != nil {
		return nil, err
	}
	// CE1/CE2 ghosts: unlinked calls per component, never a link.
	ghosts := map[string]*archGhost{}
	// In project mode (CE8) a call the pass links into another member is
	// no ghost: it is counted as linked across repositories, beside the
	// repository's own count of calls without a link.
	across, alone := map[string]int{}, map[string]int{}
	acrossTo := map[string][]string{}
	for _, kind := range []string{"client_call", "ipc_call"} {
		family, row := familyHTTP, "CE1"
		if kind == "ipc_call" {
			family, row = familyIPC, "CE2"
		}
		for _, n := range m.nodesByKind[kind] {
			if !unlinkedStatus(n) || linkedCalls[n.Identity] && kind == "ipc_call" {
				continue
			}
			if scope == scopeProduction && testSource(n.Identity) {
				continue
			}
			c := m.componentOfFile(n.File)
			if px != nil && kind == "client_call" {
				alone[c]++
				if px.across[n.Identity] {
					across[c]++
					acrossTo[c] = append(acrossTo[c], px.targets[n.Identity]...)
					continue
				}
			}
			key := family + ":" + c
			gh, ok := ghosts[key]
			if !ok {
				tagRow(row + "/ghost")
				gh = &archGhost{Component: c, Family: family, Row: row, ByStatus: map[string]int{}}
				ghosts[key] = gh
			}
			gh.Total++
			gh.ByStatus[statusOf(n)]++
		}
	}
	for _, gh := range ghosts {
		g.ghosts = append(g.ghosts, *gh)
	}
	if px != nil {
		for _, dir := range m.order {
			if across[dir] > 0 || px.opsAcrossBy[dir] > 0 {
				px.doc.Across = append(px.doc.Across, archAcross{Component: dir, LinkedAcross: across[dir], RepositoryNotLinked: alone[dir], Repositories: distinct(append(acrossTo[dir], px.opsTo[dir]...)),
					OperationsAcross: px.opsAcrossBy[dir], RepositoryNoRoute: px.opsAlone[dir]})
			}
		}
	}
	sort.Slice(g.ghosts, func(i, j int) bool {
		if g.ghosts[i].Component != g.ghosts[j].Component {
			return lessRoot(g.ghosts[i].Component, g.ghosts[j].Component)
		}
		return g.ghosts[i].Family < g.ghosts[j].Family
	})
	// The Not proven entries follow the sorted ghosts (component, then
	// family), never the map's order, so two requests return one body.
	for _, gh := range g.ghosts {
		g.notProven = append(g.notProven, archNotProven{Kind: "calls_not_linked", Row: gh.Row, Component: gh.Component, Text: "not linked (" + strconv.Itoa(gh.Total) + "): " + gh.Family + " calls without a stored link", Count: gh.Total})
	}
	// CE3: table access to one database card per schema set.
	if err := sqlLinks(rd, m, g, links, nodes, testSource, scope, addTier); err != nil {
		return nil, err
	}
	// CE4: in-process calls and imports, both directions bundled per pair;
	// CE6: imports of external packages as one stub per component.
	var imports []contract.Edge
	for _, kind := range []string{"calls", "imports"} {
		edges, err := rd.s.EdgesOfKind(rd.ctx, rd.repo, kind)
		if err != nil {
			return nil, fmt.Errorf("reading %s edges: %w", kind, err)
		}
		if kind == "imports" {
			imports = edges
		}
		for _, e := range edges {
			if strings.HasPrefix(e.Target, "external:") {
				continue
			}
			a, b := m.componentOf(e.Source), m.componentOf(e.Target)
			if a == "" || b == "" {
				continue
			}
			if a == b {
				tagRow("CED/inside")
				g.notDrawn[e.Kind]++
				continue
			}
			if scope == scopeProduction && testSource(e.Source) {
				g.testEdges[familyInProcess]++
				continue
			}
			tagRow("CE4")
			source, target, back := a, b, false
			if b < a {
				source, target, back = b, a, true
			}
			l := link(familyInProcess, "CE4", source, target)
			if back {
				l.BackCount++
			} else {
				l.Count++
			}
			l.ByKind[e.Kind]++
			addTier(l, e)
		}
	}
	ext, err := externalImports(rd, m, scope, imports)
	if err != nil {
		return nil, err
	}
	for _, dir := range m.order {
		if list := ext[dir]; len(list) > 0 {
			tagRow("CE6")
			text := "imports " + strconv.Itoa(len(list)) + " external packages"
			if len(list) == 1 {
				text = "imports 1 external package"
			}
			// A stub is never a protocol (APD).
			tagRow("APD")
			g.stubs = append(g.stubs, archStub{Component: dir, Row: "CE6", Packages: len(list), Text: text,
				Protocol: archCell{Value: "protocol ?", State: cellUnknown, Source: "unknown: not modelled", Rows: []string{"APD"}}})
		}
	}
	// CED: the other edge kinds the view reads are counted, never drawn.
	for _, kind := range notDrawnKinds {
		edges, err := rd.s.EdgesOfKind(rd.ctx, rd.repo, kind)
		if err != nil {
			return nil, fmt.Errorf("reading %s edges: %w", kind, err)
		}
		if len(edges) > 0 {
			tagRow("CED")
			g.notDrawn[kind] += len(edges)
		}
	}
	for _, l := range links {
		// An in-process bundle points from the side with more edges.
		if l.Family == familyInProcess && l.BackCount > l.Count {
			l.Source, l.Target, l.Count, l.BackCount = l.Target, l.Source, l.BackCount, l.Count
			l.ID = linkKey(l.Family, l.Source, l.Target)
		}
		l.Methods = distinct(l.Methods)
		l.Origins = distinctOrigins(l.Origins)
		annotate(l, documented, nodes)
		g.links = append(g.links, l)
	}
	g.links = append(g.links, projectLinks(px, m, g, nodes, documented, testSource, scope)...)
	ingressPointers(m, g.links)
	sort.Slice(g.links, func(i, j int) bool { return g.links[i].ID < g.links[j].ID })
	for _, l := range g.links {
		sort.Slice(l.Members, func(i, j int) bool {
			if l.Members[i].Source != l.Members[j].Source {
				return l.Members[i].Source < l.Members[j].Source
			}
			return l.Members[i].Target < l.Members[j].Target
		})
	}
	untiedDatabases(m, g)
	return g, nil
}

// actionLinks is row CE7: one server action link per component pair,
// members named by the action (its export or bound name).
func actionLinks(rd *reader, m *archModel, g *archGraph, link func(family, row, source, target string) *archLink, addTier func(*archLink, contract.Edge) string, testSource func(string) bool, scope string) error {
	edges, err := rd.s.EdgesOfKind(rd.ctx, rd.repo, "calls_action")
	if err != nil {
		return fmt.Errorf("reading calls_action edges: %w", err)
	}
	if len(edges) == 0 {
		return nil
	}
	actions, err := rd.nodes("server_action")
	if err != nil {
		return err
	}
	calls, err := rd.nodes("action_call")
	if err != nil {
		return err
	}
	byID := map[string]contract.Node{}
	for _, n := range append(actions, calls...) {
		byID[n.Identity] = n
	}
	for _, e := range edges {
		a, b := m.componentOf(e.Source), m.componentOf(e.Target)
		if a == "" || b == "" {
			continue
		}
		if a == b {
			tagRow("CED/inside")
			g.notDrawn[e.Kind]++
			continue
		}
		if scope == scopeProduction && testSource(e.Source) {
			g.testEdges[familyServerAction]++
			continue
		}
		tagRow("CE7")
		l := link(familyServerAction, "CE7", a, b)
		l.Count++
		l.ByKind[e.Kind]++
		member := archMember{Source: e.Source, Target: e.Target, Tier: addTier(l, e)}
		if call, ok := byID[e.Source]; !ok || call.Attributes["form"] == "direct" && call.Attributes["boundary"] != "client" {
			// SA-C3: a direct call is a request only from client code.
			l.serverCalls = true
		}
		if n, ok := byID[e.Target]; ok {
			name := "anonymous inline action"
			if action := stringAttr(n, "action"); action != nil {
				name = *action
			}
			member.Path = &name
		}
		l.Members = append(l.Members, member)
	}
	return nil
}

// callOrigins is row AT3 for a client call: absolute https origins it
// names, shown as documented origins.
func callOrigins(n contract.Node) []archOrigin {
	var out []archOrigin
	for _, p := range objects(n.Attributes["paths"]) {
		if origin, ok := p["origin"].(string); ok && strings.HasPrefix(origin, "https://") {
			tagRow("AT3")
			out = append(out, archOrigin{Origin: origin, Source: "documented origin in " + n.File + ", not this link's TLS", Row: "AT3"})
		}
	}
	return out
}

func distinctOrigins(list []archOrigin) []archOrigin {
	seen := map[string]bool{}
	out := []archOrigin{}
	for _, o := range list {
		key := o.Origin + "\x00" + o.Source
		if !seen[key] {
			seen[key] = true
			out = append(out, o)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Origin != out[j].Origin {
			return out[i].Origin < out[j].Origin
		}
		return out[i].Source < out[j].Source
	})
	return out
}

// documentation is what documents_route edges say about routes: the
// security scheme names of each documenting operation (AA2) and the https
// servers of their specs (AT3).
type documentation struct {
	security map[string][][]string
	origins  map[string][]archOrigin
}

// plural is "1 thing" or "n things".
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}

// documentedSecurity reads every documents_route edge (CE5): the security
// scheme names of each route's documenting operations (AA2), the
// "documents" rows per component pair, operations with no route, and
// https spec servers (AT3, attached by annotate to the documented links).
func documentedSecurity(rd *reader, m *archModel, g *archGraph, px *archProjectIn) (*documentation, error) {
	out := &documentation{security: map[string][][]string{}, origins: map[string][]archOrigin{}}
	ops, err := rd.nodes("api_operation")
	if err != nil {
		return nil, err
	}
	specs, err := rd.nodes("api_spec")
	if err != nil {
		return nil, err
	}
	servers := map[string][]archOrigin{}
	for _, spec := range specs {
		for _, server := range objects(spec.Attributes["servers"]) {
			if u, ok := server["url"].(string); ok && strings.HasPrefix(u, "https://") {
				servers[spec.File] = append(servers[spec.File], archOrigin{Origin: u, Source: "documented server in " + spec.File + ", not this link's TLS", Row: "AT3"})
			}
		}
	}
	byID := map[string]contract.Node{}
	for _, op := range ops {
		byID[op.Identity] = op
	}
	pairs := map[string]*archDocuments{}
	documentedOps := map[string]bool{}
	for _, kind := range []string{"documents_route", "calls_operation"} {
		edges, err := rd.s.EdgesOfKind(rd.ctx, rd.repo, kind)
		if err != nil {
			return nil, fmt.Errorf("reading %s edges: %w", kind, err)
		}
		for _, e := range edges {
			a, b := m.componentOf(e.Source), m.componentOf(e.Target)
			if kind == "documents_route" {
				documentedOps[e.Source] = true
				if op, ok := byID[e.Source]; ok {
					out.security[e.Target] = append(out.security[e.Target], stringList(op.Attributes["security"]))
					out.origins[e.Target] = append(out.origins[e.Target], servers[op.File]...)
				}
			}
			if a == "" || b == "" {
				continue
			}
			tagRow("CE5")
			key := a + "->" + b
			d, ok := pairs[key]
			if !ok {
				d = &archDocuments{Source: a, Target: b, Row: "CE5", ByKind: map[string]int{}}
				pairs[key] = d
			}
			d.ByKind[kind]++
		}
	}
	// CE8: operations a stored project link connects to another member's
	// routes (outgoing), and another member's operations documenting this
	// repository's routes (incoming), as "documents" rows with that
	// member's card.
	if px != nil {
		for _, l := range px.docsOut {
			if c := m.componentOf(l.Source.Identity); c != "" {
				tagRow("CE8/documents")
				key := c + "->" + remoteCard(l.Target.Repository)
				d, ok := pairs[key]
				if !ok {
					d = &archDocuments{Source: c, Target: remoteCard(l.Target.Repository), Row: "CE8", ByKind: map[string]int{}}
					pairs[key] = d
				}
				d.ByKind[l.Kind]++
			}
		}
		for _, l := range px.docsIn {
			if c := m.componentOf(l.Target.Identity); c != "" {
				tagRow("CE8/documents")
				key := remoteCard(l.Source.Repository) + "->" + c
				d, ok := pairs[key]
				if !ok {
					d = &archDocuments{Source: remoteCard(l.Source.Repository), Target: c, Row: "CE8", ByKind: map[string]int{}}
					pairs[key] = d
				}
				d.ByKind[l.Kind]++
			}
		}
	}
	for _, d := range pairs {
		g.documents = append(g.documents, *d)
	}
	sort.Slice(g.documents, func(i, j int) bool {
		if g.documents[i].Source != g.documents[j].Source {
			return g.documents[i].Source < g.documents[j].Source
		}
		return g.documents[i].Target < g.documents[j].Target
	})
	noRoute := map[string]int{}
	for _, op := range ops {
		if !documentedOps[op.Identity] {
			c := m.componentOfFile(op.File)
			if px != nil {
				// In project mode an operation a stored project link
				// connects to another member's route is documented
				// there: counted as linked across repositories, beside
				// the repository's own "documented, no route" count.
				px.opsAlone[c]++
				if px.opsAcross[op.Identity] {
					px.opsAcrossBy[c]++
					px.opsTo[c] = append(px.opsTo[c], px.targets[op.Identity]...)
					continue
				}
			}
			noRoute[c]++
		}
	}
	for _, dir := range m.order {
		if n := noRoute[dir]; n > 0 {
			tagRow("CE5/no-route")
			g.notProven = append(g.notProven, archNotProven{Kind: "documented_no_route", Row: "CE5", Component: dir, Text: "documented, no route: " + plural(n, "operation"), Count: n})
		}
	}
	return out, nil
}

// sqlLinks is row CE3: reads_table and writes_table from a symbol of A to
// a table of schema set S make one SQL link A -> database card S.
func sqlLinks(rd *reader, m *archModel, g *archGraph, links map[string]*archLink, nodes map[string]contract.Node, testSource func(string) bool, scope string, addTier func(*archLink, contract.Edge) string) error {
	migrations, err := rd.nodes("migration")
	if err != nil {
		return err
	}
	setTools := map[string][]string{}
	for _, mg := range migrations {
		if set, tool := stringAttr(mg, "set"), stringAttr(mg, "tool"); set != nil && tool != nil {
			setTools[*set] = append(setTools[*set], *tool)
		}
	}
	cards := map[string]*archDatabase{}
	dialects := map[string][]string{}
	dialectFrom := map[string][]string{}
	for _, t := range m.nodesByKind["table"] {
		set := stringAttr(t, "set")
		if set == nil {
			continue
		}
		card, ok := cards[*set]
		if !ok {
			card = &archDatabase{ID: "database:" + *set, Set: *set, Evidence: []string{}, Accessors: []archAccessors{}}
			tools := setTools[*set]
			if tool := stringAttr(t, "tool"); tool != nil {
				tools = append(tools, *tool)
			}
			if tools = distinct(tools); len(tools) == 1 {
				card.Tool = &tools[0]
			}
			cards[*set] = card
		}
		card.Tables++
		if d := stringAttr(t, "dialect"); d != nil {
			dialects[*set] = append(dialects[*set], *d)
			// AE1's source names where the dialect is stored:
			// a migration set, or ORM model tables (source "model").
			from := "migration"
			if t.Attributes["source"] == "model" {
				from = "model"
				if tool := stringAttr(t, "tool"); tool != nil {
					from = "model " + *tool
				}
			}
			dialectFrom[*set] = append(dialectFrom[*set], from)
		}
	}
	tableSet := func(id string) string {
		if t, ok := nodes[id]; ok {
			if set := stringAttr(t, "set"); set != nil {
				return *set
			}
		}
		return ""
	}
	type accessKey struct{ component, set string }
	access := map[accessKey]*archAccessors{}
	accessor := func(c, set string) *archAccessors {
		k := accessKey{c, set}
		a, ok := access[k]
		if !ok {
			a = &archAccessors{Component: c, Link: linkKey(familySQL, c, "database:"+set)}
			access[k] = a
		}
		return a
	}
	// builds holds the (owner, table) pairs of table_access entries with
	// role builds (I8, CH6 "builds a query on").
	builds := map[[3]string]bool{}
	for _, kind := range []string{"function", "method", "file"} {
		owners, err := rd.s.Nodes(rd.ctx, rd.repo, store.Query{Kind: kind, Attribute: "table_access"})
		if err != nil {
			return fmt.Errorf("reading %s nodes: %w", kind, err)
		}
		for _, n := range owners {
			for _, entry := range objects(n.Attributes["table_access"]) {
				k, _ := entry["kind"].(string)
				if t, ok := entry["target"].(string); ok && entry["role"] == "builds" {
					builds[[3]string{n.Identity, t, k}] = true
				}
			}
		}
	}
	for _, kind := range []string{"reads_table", "writes_table"} {
		edges, err := rd.s.EdgesOfKind(rd.ctx, rd.repo, kind)
		if err != nil {
			return fmt.Errorf("reading %s edges: %w", kind, err)
		}
		for _, e := range edges {
			c, set := m.componentOf(e.Source), tableSet(e.Target)
			if c == "" || set == "" {
				continue
			}
			if scope == scopeProduction && testSource(e.Source) {
				g.testEdges[familySQL]++
				continue
			}
			tagRow("CE3")
			key := linkKey(familySQL, c, "database:"+set)
			l, ok := links[key]
			if !ok {
				l = &archLink{ID: key, Family: familySQL, Row: "CE3", Source: c, Target: "database:" + set, ByKind: map[string]int{}, Tiers: map[string]int{}, Methods: []string{}, Members: []archMember{}, Origins: []archOrigin{}, weakest: 2}
				links[key] = l
			}
			l.Count++
			l.ByKind[kind]++
			addTier(l, e)
			if card := cards[set]; card != nil && card.Tool != nil {
				l.tools = append(l.tools, *card.Tool)
			}
			a := accessor(c, set)
			switch {
			case builds[[3]string{e.Source, e.Target, kind}]:
				tagRow("CE3/builds")
				a.Builds++
			case kind == "reads_table":
				a.Reads++
			default:
				a.Writes++
			}
		}
	}
	// "may also use": table_access_unknown sites of a component's symbols,
	// never a link.
	for _, kind := range []string{"function", "method", "file"} {
		// Only the owners holding the attribute: reading every symbol and
		// file node took about 1.1 s on langflow.
		owners, err := rd.s.Nodes(rd.ctx, rd.repo, store.Query{Kind: kind, Attribute: "table_access_unknown"})
		if err != nil {
			return fmt.Errorf("reading %s nodes: %w", kind, err)
		}
		for _, n := range owners {
			entries := objects(n.Attributes["table_access_unknown"])
			if len(entries) == 0 {
				continue
			}
			if scope == scopeProduction && testScoped(n, m.testFile[n.File]) {
				continue
			}
			c := m.componentOfFile(n.File)
			for _, entry := range entries {
				for _, set := range unknownSets(entry, nodes, cards) {
					tagRow("CE3/may-also-use")
					accessor(c, set).MayAlsoUse++
				}
			}
		}
	}
	for _, a := range access {
		set := a.Link[strings.Index(a.Link, "->database:")+len("->database:"):]
		if card := cards[set]; card != nil {
			card.Accessors = append(card.Accessors, *a)
		}
	}
	for _, set := range keysOf(cards) {
		card := cards[set]
		sort.Slice(card.Accessors, func(i, j int) bool { return lessRoot(card.Accessors[i].Component, card.Accessors[j].Component) })
		card.Engine, card.Evidence = engineCell(m, card, dialects[set], dialectFrom[set])
		card.Engine, card.Evidence = engineFromDeploy(m, card, card.Engine, card.Evidence, len(dialects[set]) > 0)
		g.databases = append(g.databases, card)
		for _, a := range card.Accessors {
			if a.MayAlsoUse > 0 {
				g.notProven = append(g.notProven, archNotProven{Kind: "may_also_use", Row: "CE3", Component: a.Component, Text: "may also use " + card.ID + ": " + plural(a.MayAlsoUse, "site") + " not bound to a table", Count: a.MayAlsoUse, Database: card.ID})
			}
		}
	}
	return nil
}

// unknownSets are the schema sets a table_access_unknown entry may touch:
// its candidates' sets, or every set of its tool when candidates is null.
func unknownSets(entry map[string]any, nodes map[string]contract.Node, cards map[string]*archDatabase) []string {
	var out []string
	if list, ok := entry["candidates"].([]any); ok {
		for _, c := range list {
			if id, ok := c.(string); ok {
				if t, ok := nodes[id]; ok {
					if set := stringAttr(t, "set"); set != nil {
						out = append(out, *set)
					}
				}
			}
		}
		return distinct(out)
	}
	tool, _ := entry["tool"].(string)
	for set, card := range cards {
		if card.Tool != nil && *card.Tool == tool {
			out = append(out, set)
		}
	}
	sort.Strings(out)
	return out
}

func keysOf[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// dbDrivers is row AE3's closed list of database driver packages by
// manifest ecosystem (SQL drivers only: a card is a SQL schema set):
// evidence lines, never the engine value.
var dbDrivers = map[string][]string{
	"npm":    {"pg", "postgres", "mysql", "mysql2", "better-sqlite3", "sqlite3", "mssql", "tedious", "oracledb"},
	"python": {"psycopg", "psycopg2", "psycopg2-binary", "psycopg-binary", "asyncpg", "aiosqlite", "pymysql", "mysqlclient", "aiomysql", "oracledb", "cx_oracle"},
	"cargo":  {"tokio-postgres", "postgres", "rusqlite", "mysql", "mysql_async", "sqlx"},
	"go":     {},
}

// engineCell applies rows AE1, AE3 and AED to one database card.
func engineCell(m *archModel, card *archDatabase, dialects, from []string) (archCell, []string) {
	evidence := []string{}
	for _, a := range card.Accessors {
		c := m.components[a.Component]
		if c == nil {
			continue
		}
		for _, p := range c.packages {
			eco, _ := p.Attributes["ecosystem"].(string)
			deps := stringList(p.Attributes["dependencies"])
			for _, d := range dbDrivers[eco] {
				if contains(deps, d) {
					tagRow("AE3")
					evidence = append(evidence, "driver "+d+" declared in "+p.File)
				}
			}
		}
	}
	evidence = distinct(evidence)
	ds := distinct(dialects)
	if len(ds) == 1 && ds[0] == "postgres" && len(dialects) == card.Tables {
		tagRow("AE1")
		return archCell{Value: "PostgreSQL", State: cellProven, Source: "proven: " + dialectSource(from), Rows: []string{"AE1"}}, evidence
	}
	tagRow("AED")
	reason := "unknown: no stored fact names the engine"
	if len(ds) > 1 {
		reason = "unknown: stored dialects disagree (" + strings.Join(ds, ", ") + ")"
	}
	return archCell{Value: "engine ?", State: cellUnknown, Source: reason, Rows: []string{"AED"}}, evidence
}

// dialectSource names where a card's stored dialect comes from (AE1):
// "migration dialect", "ORM model dialect (<tool>)", or "stored table
// dialect (<sources>)" when the tables mix them.
func dialectSource(from []string) string {
	ds := distinct(from)
	switch {
	case len(ds) == 1 && ds[0] == "migration":
		return "migration dialect"
	case len(ds) == 1 && strings.HasPrefix(ds[0], "model "):
		return "ORM model dialect (" + strings.TrimPrefix(ds[0], "model ") + ")"
	case len(ds) == 1:
		return "ORM model dialect"
	}
	sort.Strings(ds)
	return "stored table dialect (" + strings.Join(ds, ", ") + ")"
}

// annotate fills a link's three cells.
func annotate(l *archLink, documented *documentation, nodes map[string]contract.Node) {
	switch l.Family {
	case familyHTTP:
		tagRow("AP1")
		l.Cells.Protocol = archCell{Value: "HTTP", State: cellProven, Source: "proven: route and client-call facts", Rows: []string{"AP1"}}
		l.Cells.Auth = httpAuth(l, documented, nodes)
		tagRow("ATD")
		l.Cells.TLS = archCell{Value: "TLS ?", State: cellUnknown, Source: "unknown: transport security comes from deployment; no deploy config proves this hop", Rows: []string{"ATD"}}
	case familyIPC:
		tagRow("AP2")
		l.Cells.Protocol = archCell{Value: "Tauri IPC", State: cellProven, Source: "proven: invokes_command", Rows: []string{"AP2"}}
		tagRow("AAD/tauri")
		l.Cells.Auth = archCell{Value: "auth ?", State: cellUnknown, Source: "unknown: capabilities not read", Rows: []string{"AAD"}}
		tagRow("AT1")
		l.Cells.TLS = archCell{Value: "n/a, in-app", State: cellNotApplicable, Source: "in-app IPC", Rows: []string{"AT1"}}
	case familySQL:
		tagRow("AP3")
		value := "SQL"
		if tools := distinct(l.tools); len(tools) == 1 {
			value = "SQL via " + tools[0]
		}
		l.Cells.Protocol = archCell{Value: value, State: cellProven, Source: "proven: schema set tool", Rows: []string{"AP3"}}
		tagRow("AAD/sql")
		l.Cells.Auth = archCell{Value: "auth ?", State: cellUnknown, Source: "unknown: database credentials are not read", Rows: []string{"AAD"}}
		tagRow("ATD")
		l.Cells.TLS = archCell{Value: "TLS ?", State: cellUnknown, Source: "unknown: transport security comes from deployment; no deploy config proves this hop", Rows: []string{"ATD"}}
	case familyServerAction:
		// AP5: Next.js sends a server action as a POST to the page that
		// renders it (an action ID header names the action), so the hop is
		// HTTP and its TLS comes from deployment; a direct call from
		// server code runs in process, which no row tells apart here, so
		// TLS stays unknown either way (never "n/a").
		tagRow("AP5")
		source := "proven: calls_action (a POST to the page that renders the action)"
		if l.serverCalls {
			tagRow("AP5/direct")
			source = "proven: calls_action (a POST to the page that renders the action when called from the client; a direct call from server code runs in process)"
		}
		l.Cells.Protocol = archCell{Value: "server action", State: cellProven, Source: source, Rows: []string{"AP5"}}
		tagRow("AAD/server-action")
		l.Cells.Auth = archCell{Value: "auth ?", State: cellUnknown, Source: "unknown: a server action is reachable by a POST to its page; checks inside the action are not read", Rows: []string{"AAD"}}
		tagRow("ATD")
		l.Cells.TLS = archCell{Value: "TLS ?", State: cellUnknown, Source: "unknown: transport security comes from deployment; no deploy config proves this hop", Rows: []string{"ATD"}}
	case familyInProcess:
		tagRow("AP4")
		l.Cells.Protocol = archCell{Value: "in-process", State: cellProven, Source: "proven: calls and imports", Rows: []string{"AP4"}}
		tagRow("AAD/in-process")
		l.Cells.Auth = archCell{Value: "auth ?", State: cellUnknown, Source: "unknown: no auth model for in-process calls", Rows: []string{"AAD"}}
		tagRow("AT1")
		l.Cells.TLS = archCell{Value: "n/a, in-app", State: cellNotApplicable, Source: "in-process", Rows: []string{"AT1"}}
	}
}

// memberAuth is one HTTP member's auth statement and the rows behind it;
// labels are the AA3/AA4 source labels (empty for AA1/AA2 alone).
type memberAuth struct {
	value, state string
	rows         []string
	labels       []string
}

// declaredSchemes is row AA3: the route's FastAPI security_dependencies
// (DA-F3) worded "declares <class> (<scheme name>)", with "optional
// (auto_error=False)" when written so. Never "protected": a scheme with
// auto_error=False lets requests without credentials through.
func declaredSchemes(route contract.Node) []string {
	var out []string
	for _, s := range objects(route.Attributes["security_dependencies"]) {
		class, _ := s["class"].(string)
		if class == "" {
			continue
		}
		name, _ := s["scheme_name"].(string)
		if name == "" || name == "unknown" {
			name = "scheme name ?"
		}
		text := "declares " + class + " (" + name + ")"
		switch s["auto_error"] {
		case false:
			text += ", optional (auto_error=False)"
		case true:
		default:
			text += ", auto_error ?"
		}
		out = append(out, text)
	}
	return distinct(out)
}

// clientSecurity is row AA4: a hey-api call's security array (DA-H1) as
// the generated client documents it, each entry as written (type, scheme,
// name, in). The client adds credentials only when the application
// configures auth, which is not read, so the wording never says it sends
// them.
func clientSecurity(call contract.Node) []string {
	var out []string
	for _, entry := range objects(call.Attributes["security"]) {
		var words []string
		for _, key := range []string{"type", "scheme", "name"} {
			if v, _ := entry[key].(string); v != "" {
				words = append(words, v)
			}
		}
		if v, _ := entry["in"].(string); v != "" {
			words = append(words, "in "+v)
		}
		if len(words) > 0 {
			out = append(out, strings.Join(words, " "))
		}
	}
	return out
}

// routeAuth applies AA1 to AA4 to one member (its route and client call);
// AAD when none applies.
func routeAuth(route contract.Node, ok bool, documented [][]string, call contract.Node, callOK bool) memberAuth {
	var parts, rows, labels []string
	state := ""
	if ok && route.Attributes["framework"] == "fastapi" {
		if declared := declaredSchemes(route); len(declared) > 0 {
			tagRow("AA3")
			if len(objects(route.Attributes["security_dependencies_unknown"])) > 0 {
				tagRow("AA3/unknown-steps")
			}
			rows = append(rows, "AA3")
			labels = append(labels, "declared")
			state = cellAsWritten
			parts = append(parts, strings.Join(declared, "; "))
		}
	}
	if callOK {
		if schemes := clientSecurity(call); len(schemes) > 0 {
			tagRow("AA4")
			rows = append(rows, "AA4")
			labels = append(labels, "documented by the generated client, as written in "+call.File)
			if state == "" {
				state = cellDocumented
			}
			parts = append(parts, "generated client documents "+strings.Join(schemes, " or "))
		}
	}
	if ok && route.Attributes["framework"] == "express" {
		if list, isList := route.Attributes["middleware"].([]any); isList {
			names := stringList(list)
			tagRow("AA1")
			rows = append(rows, "AA1")
			state = cellAsWritten
			if len(names) == 0 {
				parts = append(parts, "no route middleware (as written)")
			} else {
				text := "middleware " + strings.Join(names, ", ") + " (as written)"
				for _, d := range objects(route.Attributes["middleware_declares"]) {
					tagRow("AA1/declares")
					text += ", " + declaresText(d)
				}
				parts = append(parts, text)
			}
		}
		if text := mountedMiddleware(route); text != "" {
			rows = append(rows, "AA5")
			state = cellAsWritten
			parts = append(parts, text)
		}
	}
	if ok && route.Attributes["framework"] == "axum" {
		if text := axumLayers(route); text != "" {
			rows = append(rows, "AA6")
			state = cellAsWritten
			parts = append(parts, text)
		}
	}
	if len(documented) > 0 {
		tagRow("AA2")
		rows = append(rows, "AA2")
		if state == "" {
			state = cellDocumented
		}
		var texts []string
		for _, names := range documented {
			if len(names) == 0 {
				texts = append(texts, "documented: none")
			} else {
				texts = append(texts, "documented: "+strings.Join(names, ", "))
			}
		}
		texts = distinct(texts)
		if len(texts) > 1 {
			tagRow("AA2/mixed")
			parts = append(parts, "documented: mixed ("+strings.Join(texts, "; ")+")")
		} else {
			parts = append(parts, texts[0])
		}
	}
	if len(parts) == 0 {
		tagRow("AAD/http")
		return memberAuth{value: "auth ?", state: cellUnknown, rows: []string{"AAD"}}
	}
	return memberAuth{value: strings.Join(parts, "; "), state: state, rows: rows, labels: labels}
}

// httpAuth aggregates the members' auth into one cell, "mixed" with
// counts when they differ (AG15).
func httpAuth(l *archLink, documented *documentation, nodes map[string]contract.Node) archCell {
	auths := make([]memberAuth, 0, len(l.Members))
	for i, member := range l.Members {
		route, ok := nodes[member.Target]
		call, callOK := nodes[member.Source]
		a := routeAuth(route, ok, documented.security[member.Target], call, callOK)
		l.Members[i].Auth = a.value
		auths = append(auths, a)
	}
	return aggregateAuth(auths)
}

// aggregateAuth is httpAuth's cell for the members' auth statements.
func aggregateAuth(auths []memberAuth) archCell {
	counts := map[string]int{}
	states := map[string]bool{}
	var rows, labels []string
	for _, a := range auths {
		counts[a.value]++
		states[a.state] = true
		rows = append(rows, a.rows...)
		labels = append(labels, a.labels...)
	}
	rows = distinct(rows)
	if len(counts) == 1 {
		for value := range counts {
			if value == "auth ?" {
				return archCell{Value: value, State: cellUnknown, Source: "unknown: no stored auth form covers these routes", Rows: rows}
			}
			state := cellDocumented
			source := "documented"
			if states[cellAsWritten] {
				state, source = cellAsWritten, "as written"
			}
			if len(labels) > 0 {
				// AA3/AA4 name their own sources; AA1/AA2 keep theirs.
				all := distinct(labels)
				if contains(rows, "AA1") || contains(rows, "AA2") {
					all = append(all, source)
				}
				source = strings.Join(all, "; ")
			}
			return archCell{Value: value, State: state, Source: source, Rows: rows}
		}
	}
	return archCell{Value: "mixed", State: cellMixed, Source: "members differ; counts per value", Rows: rows, Counts: counts}
}

// externalImports is row CE6: per component, the external packages its
// non-test files import (standard libraries and unresolved names
// excluded).
// imports are the stored imports edges when the caller read them already
// (nil: read them here).
func externalImports(rd *reader, m *archModel, scope string, imports []contract.Edge) (map[string][]componentExtern, error) {
	externals, err := rd.nodes("external")
	if err != nil {
		return nil, err
	}
	byID := map[string]contract.Node{}
	for _, n := range externals {
		byID[n.Identity] = n
	}
	edges := imports
	if edges == nil {
		var err error
		if edges, err = rd.s.EdgesOfKind(rd.ctx, rd.repo, "imports"); err != nil {
			return nil, fmt.Errorf("reading imports edges: %w", err)
		}
	}
	counts := map[string]map[string]int{}
	for _, e := range edges {
		n, ok := byID[e.Target]
		if !ok || boolAttr(n.Attributes, "unresolved") || standardLibrary(n) {
			continue
		}
		file, ok := identityFile(e.Source)
		if !ok || scope == scopeProduction && m.testFile[file] {
			continue
		}
		c := m.componentOfFile(file)
		if c == "" {
			continue
		}
		if counts[c] == nil {
			counts[c] = map[string]int{}
		}
		counts[c][e.Target]++
	}
	out := map[string][]componentExtern{}
	for c, targets := range counts {
		for _, id := range keysOf(targets) {
			n := byID[id]
			eco, _ := n.Attributes["ecosystem"].(string)
			name, _ := n.Attributes["name"].(string)
			out[c] = append(out[c], componentExtern{Identity: id, Ecosystem: eco, Name: name, Imports: targets[id]})
		}
	}
	return out, nil
}

// nodeBuiltins are Node.js core modules (without the node: prefix).
var nodeBuiltins = setOf("assert", "async_hooks", "buffer", "child_process", "cluster", "console", "constants", "crypto", "dgram", "diagnostics_channel", "dns", "domain", "events", "fs", "fs/promises", "http", "http2", "https", "inspector", "module", "net", "os", "path", "perf_hooks", "process", "punycode", "querystring", "readline", "repl", "stream", "string_decoder", "sys", "timers", "tls", "trace_events", "tty", "url", "util", "v8", "vm", "wasi", "worker_threads", "zlib")

// standardLibrary is CE6's closed exclusion list: a language's standard
// library is not an SDK.
func standardLibrary(n contract.Node) bool {
	eco, _ := n.Attributes["ecosystem"].(string)
	name, _ := n.Attributes["name"].(string)
	switch eco {
	case "python-stdlib":
		return true
	case "npm":
		return strings.HasPrefix(name, "node:") || nodeBuiltins[name]
	case "go":
		first, _, _ := strings.Cut(name, "/")
		return !strings.Contains(first, ".")
	case "cargo":
		return name == "std" || name == "core" || name == "alloc" || name == "proc_macro" || name == "test"
	case "dart":
		return strings.HasPrefix(name, "dart:")
	case "java":
		return strings.HasPrefix(name, "java.") || strings.HasPrefix(name, "javax.")
	case "dotnet":
		return name == "System" || strings.HasPrefix(name, "System.")
	}
	return path.IsAbs(name)
}

// declaresText words a DA-E2 / DA-X2 "declares" object: "declares
// <library> (<detail>)", with ", optional (<option>: false)" when the
// option is written false and ", <option> ?" when it is not a literal.
// Never "protected": an optional check lets requests without credentials
// through.
func declaresText(d map[string]any) string {
	library, _ := d["library"].(string)
	detail, _ := d["detail"].(string)
	text := "declares " + library + " (" + detail + ")"
	option, _ := d["option"].(string)
	switch d["optional"] {
	case true:
		text += ", optional (" + option + ": false)"
	case "unknown":
		text += ", " + option + " ?"
	}
	return text
}

// unreadSuffix words the unknown entries of an AA5/AA6 attribute.
func unreadSuffix(what string, n int) string {
	return what + " ? (" + plural(n, "unread form") + ")"
}

// mountedMiddleware is row AA5: the route's mounted_middleware (DA-E1) as
// written, "middleware <name>, mounted at <prefix>", root router first,
// and a count of the unread forms that may add more. Empty when the route
// has neither.
func mountedMiddleware(route contract.Node) string {
	var names []string
	for _, m := range objects(route.Attributes["mounted_middleware"]) {
		name, _ := m["name"].(string)
		prefix, _ := m["prefix"].(string)
		text := "middleware " + name + ", mounted at " + prefix
		if d, isMap := m["declares"].(map[string]any); isMap {
			tagRow("AA5/declares")
			text += ", " + declaresText(d)
		}
		names = append(names, text)
	}
	unknown, earlier := 0, 0
	for _, u := range objects(route.Attributes["mounted_middleware_unknown"]) {
		reason, _ := u["reason"].(string)
		if earlierRouteReason(reason) {
			earlier++
		} else {
			unknown++
		}
	}
	var parts []string
	if len(names) > 0 {
		tagRow("AA5")
		parts = append(parts, strings.Join(names, "; ")+" (as written)")
	}
	if unknown > 0 {
		tagRow("AA5/unknown")
		parts = append(parts, unreadSuffix("mounted middleware", unknown))
	}
	if earlier > 0 {
		// DA-E1: an earlier route layer may run first; its
		// handlers act as middleware when they call next().
		tagRow("AA5/earlier-route")
		parts = append(parts, "earlier route handlers ? ("+plural(earlier, "route layer")+" may run first)")
	}
	return strings.Join(parts, "; ")
}

// earlierRouteReason reports a DA-E1 unknown entry for an earlier route
// layer (the route's router or an earlier sibling router).
func earlierRouteReason(reason string) bool {
	return strings.HasPrefix(reason, "earlier route handler ") || strings.HasPrefix(reason, "router ") && strings.Contains(reason, " has route handler ")
}

// maxLayerText caps a layer expression in the cell; the attribute keeps
// it whole.
const maxLayerText = 60

// layerSuffix names a layer by DA-X2 where the closed list applies.
func layerSuffix(l map[string]any) string {
	text := ""
	if d, isMap := l["declares"].(map[string]any); isMap {
		tagRow("AA6/declares")
		text += ", " + declaresText(d)
	}
	if f, _ := l["middleware_function"].(string); f != "" {
		tagRow("AA6/function")
		text += ", middleware function " + f
	}
	return text
}

// layerName is one stack entry, its expression capped.
func layerName(s map[string]any) string {
	expression, _ := s["expression"].(string)
	if runes := []rune(expression); len(runes) > maxLayerText {
		expression = string(runes[:maxLayerText]) + "…"
	}
	return expression + layerSuffix(s)
}

// axumLayers is row AA6: the route's layers (DA-X1) as written,
// innermost first, with a DA-X2 name where the closed list applies, and a
// count of the unread forms. Empty when the route has neither.
func axumLayers(route contract.Node) string {
	var names []string
	for _, l := range objects(route.Attributes["layers"]) {
		expression, _ := l["expression"].(string)
		if runes := []rune(expression); len(runes) > maxLayerText {
			expression = string(runes[:maxLayerText]) + "…"
		}
		method, _ := l["method"].(string)
		text := method + " " + expression
		if stack := objects(l["stack"]); len(stack) > 0 {
			// DA-X4: the stack's layers in written order (outermost first).
			tagRow("AA6/stack")
			inner := make([]string, 0, len(stack))
			for _, s := range stack {
				inner = append(inner, layerName(s))
			}
			text = method + " ServiceBuilder [" + strings.Join(inner, " · ") + "]"
		}
		text += layerSuffix(l)
		if on, _ := l["on"].(string); on == "method_router" {
			tagRow("AA6/method_router")
			text = "method router " + text
		}
		names = append(names, text)
	}
	unknown := len(objects(route.Attributes["layers_unknown"]))
	var parts []string
	if len(names) > 0 {
		tagRow("AA6")
		parts = append(parts, strings.Join(names, "; ")+" (as written)")
	}
	if unknown > 0 {
		tagRow("AA6/unknown")
		parts = append(parts, unreadSuffix("layers", unknown))
	}
	return strings.Join(parts, "; ")
}
