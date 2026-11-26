// Package projectlink is the project link pass of ADR 0075: it links client
// calls and OpenAPI operations of one project member to routes of another,
// from stored nodes alone, and returns links owned by the project. Member
// snapshots are never changed. The rules are the claim contract table in
// docs/extract/project-links.md; contract.go is its code side.
package projectlink

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/clients"
	"github.com/graphvein/graphvein/internal/store"
)

// Member is one project member as the pass reads it: its latest run, its
// stored nodes (route, client_call and api_operation are used, others are
// ignored) and its manifest declarations.
type Member struct {
	Repository string
	// Run is the member's latest run, empty when the member has no stored
	// index (then Nodes is empty too).
	Run      string
	Nodes    []contract.Node
	Consumes []config.Consume
	// Scopes are the member's declared link scopes: each is a link
	// endpoint of its own, and the rest of the member is its default scope.
	// A source inside a declared scope uses that scope's consumes; any other
	// source uses Consumes.
	Scopes []config.Scope
}

// Input is everything one link pass reads, members in manifest order.
type Input struct {
	Project      string
	ManifestHash string
	Members      []Member
}

// Endpoint names a node by the pair (repository, identity), never as one
// concatenated string (ADR 0075 identity rule 3).
type Endpoint = store.ProjectNodeRef

// Outcome is the pass's result for one source: a client call, or one path
// entry of an OpenAPI operation. Every source gets exactly one row, so a
// view can say why a call has no project link as well as show the link.
// The store keeps them with the links (store.ProjectLinkSet.Outcomes).
type Outcome = store.ProjectOutcome

// Result is one link pass: the link set to store and every source's
// outcome, ordered by kind, repository, identity and template.
type Result struct {
	Set      store.ProjectLinkSet
	Outcomes []Outcome
}

// callPath and the other decode types read the stored attributes the
// clients and OpenAPI extractors write (docs/extract/clients.md,
// docs/extract/openapi.md).
type callPath struct {
	Template         *string `json:"template"`
	Origin           *string `json:"origin"`
	OriginResolution string  `json:"origin_resolution"`
	Conditional      bool    `json:"conditional"`
	Status           string  `json:"status"`
	TrailingSlash    bool    `json:"trailing_slash"`
	// Base and BaseExpression are stored per alternative on calls with
	// several or conditional alternatives (extractors-47); Base is empty in
	// an older index.
	Base           string  `json:"base"`
	BaseExpression *string `json:"base_expression"`
}

type callAttributes struct {
	Method         *string     `json:"method"`
	Base           string      `json:"base"`
	BaseExpression string      `json:"base_expression"`
	Status         string      `json:"status"`
	Scope          string      `json:"scope"`
	Paths          *[]callPath `json:"paths"`
}

type operationPath struct {
	Template      *string  `json:"template"`
	Origins       []string `json:"origins"`
	TrailingSlash bool     `json:"trailing_slash"`
	Status        string   `json:"status"`
}

type operationAttributes struct {
	Method *string          `json:"method"`
	Scope  string           `json:"scope"`
	Paths  *[]operationPath `json:"paths"`
}

func decode(attrs map[string]any, v any) bool {
	data, err := json.Marshal(attrs)
	if err != nil {
		return false
	}
	return json.Unmarshal(data, v) == nil
}

// provider is one link endpoint with its own matcher: a member without
// declared link scopes, one declared scope of a member (the routes in its
// folder), or a member's default scope (its routes outside every declared
// scope). Each is matched separately, because which server a call reaches
// is exactly what is not proven.
type provider struct {
	member *Member
	// scope is the declared link scope's name, empty for a member without
	// scopes or for a member's default scope.
	scope   string
	matcher clients.RouteMatcher
	// cross is the matcher for sources of another member: it also holds the
	// endpoint's Next.js routes (clients.NewProjectRouteMatcher), except
	// those of an application whose routes lie in more than one endpoint.
	cross  clients.RouteMatcher
	routes map[string]contract.Node
}

// newProvider builds an endpoint over routes; split names the Next.js
// applications (the route attribute project) whose routes the member's
// link scopes divide, which the endpoint leaves out of cross.
func newProvider(m *Member, scope string, routes []contract.Node, split map[string]bool) *provider {
	byIdentity := map[string]contract.Node{}
	var cross []contract.Node
	for _, n := range routes {
		byIdentity[n.Identity] = n
		if project, _ := n.Attributes["project"].(string); nextRoute(n) && split[project] {
			continue
		}
		cross = append(cross, n)
	}
	return &provider{member: m, scope: scope, matcher: clients.NewStoredRouteMatcher(routes), cross: clients.NewProjectRouteMatcher(cross), routes: byIdentity}
}

// nextRoute reports a Next.js route handler or Pages Router API route.
func nextRoute(n contract.Node) bool {
	framework, _ := n.Attributes["framework"].(string)
	return framework == "nextjs" || framework == "nextjs_pages"
}

// scopeOf returns m's declared link scope whose folder holds file (a
// slash-separated path relative to the member's root), or nil for its
// default scope. Declared scopes never nest (manifest rule
// scope-overlap), so at most one holds the file.
func scopeOf(m *Member, file string) *config.Scope {
	for i := range m.Scopes {
		if strings.HasPrefix(file, m.Scopes[i].Path+"/") {
			return &m.Scopes[i]
		}
	}
	return nil
}

// source is one call alternative or operation path entry to link.
type source struct {
	kind     string
	member   *Member
	node     contract.Node
	template string
	// call fields
	call clients.CallRequest
	// literalOrigin is the alternative's literal origin, lower case.
	literalOrigin  string
	baseExpression string
	// operation fields
	method        string
	scope         string
	trailingSlash bool
	origins       []string
}

// pass holds the members and their matchers for one Link call.
type pass struct {
	members []*Member
	// whole is each member's matcher over all its routes, which replays its
	// own in-repository result (used to check a scope overlay).
	whole map[string]*provider
	// endpoints are each member's link endpoints: the member itself when it
	// declares no scopes, otherwise its declared scopes in manifest order
	// followed by its default scope.
	endpoints map[string][]*provider
	links     map[store.ProjectLinkKey]store.ProjectLink
	outcomes  []Outcome
}

// Link runs the project link pass over in. It never fails: a source whose
// stored attributes it cannot read gets PL-U and no link.
func Link(in Input) Result {
	p := &pass{whole: map[string]*provider{}, endpoints: map[string][]*provider{}, links: map[store.ProjectLinkKey]store.ProjectLink{}}
	set := store.ProjectLinkSet{Project: in.Project, ManifestHash: in.ManifestHash, Members: []store.ProjectMember{}, Links: []store.ProjectLink{}}
	for i := range in.Members {
		m := &in.Members[i]
		p.members = append(p.members, m)
		set.Members = append(set.Members, store.ProjectMember{Repository: m.Repository, Run: m.Run})
		var routes []contract.Node
		byScope := map[string][]contract.Node{}
		// A Next.js application whose routes two endpoints divide is left
		// out of both: NX-L3 needs all of its routes to tell whether one
		// serves a path before a rewrite applies.
		nextScopes := map[string]map[string]bool{}
		for _, n := range m.Nodes {
			if n.Kind == "route" {
				routes = append(routes, n)
				name := ""
				if s := scopeOf(m, n.File); s != nil {
					name = s.Name
				}
				byScope[name] = append(byScope[name], n)
				if project, _ := n.Attributes["project"].(string); nextRoute(n) {
					if nextScopes[project] == nil {
						nextScopes[project] = map[string]bool{}
					}
					nextScopes[project][name] = true
				}
			}
		}
		split := map[string]bool{}
		for project, names := range nextScopes {
			split[project] = len(m.Scopes) > 0 && len(names) > 1
		}
		p.whole[m.Repository] = newProvider(m, "", routes, nil)
		if len(m.Scopes) == 0 {
			p.endpoints[m.Repository] = []*provider{p.whole[m.Repository]}
			continue
		}
		for _, s := range m.Scopes {
			p.endpoints[m.Repository] = append(p.endpoints[m.Repository], newProvider(m, s.Name, byScope[s.Name], split))
		}
		p.endpoints[m.Repository] = append(p.endpoints[m.Repository], newProvider(m, "", byScope[""], split))
	}
	for _, m := range p.members {
		for _, n := range m.Nodes {
			switch n.Kind {
			case "client_call":
				p.call(m, n)
			case "api_operation":
				p.operation(m, n)
			}
		}
	}
	for _, l := range p.links {
		set.Links = append(set.Links, l)
	}
	sort.Slice(set.Links, func(i, j int) bool { return set.Links[i].Key().Less(set.Links[j].Key()) })
	sort.SliceStable(p.outcomes, func(i, j int) bool {
		a, b := p.outcomes[i], p.outcomes[j]
		x := [4]string{a.Kind, a.Source.Repository, a.Source.Identity, a.Template}
		y := [4]string{b.Kind, b.Source.Repository, b.Source.Identity, b.Template}
		for k := range x {
			if x[k] != y[k] {
				return x[k] < y[k]
			}
		}
		return false
	})
	set.Outcomes = p.outcomes
	return Result{Set: set, Outcomes: p.outcomes}
}

// record adds an outcome without a link.
func (p *pass) record(kind string, m *Member, n contract.Node, template, id string) *Outcome {
	p.outcomes = append(p.outcomes, Outcome{Kind: kind, Source: Endpoint{Repository: m.Repository, Identity: n.Identity}, Template: template, Row: id, Status: row(id).Status, Candidates: []Endpoint{}})
	return &p.outcomes[len(p.outcomes)-1]
}

// call classifies one client_call node (rows PL-R1 to PL-R3, then resolve).
// A call with several or conditional alternatives is classified per
// alternative, as the in-repository linker links each alternative.
func (p *pass) call(m *Member, n contract.Node) {
	var a callAttributes
	if !decode(n.Attributes, &a) || a.Paths == nil {
		p.record(KindCallsRoute, m, n, "", RowUnknown)
		return
	}
	paths := *a.Paths
	several := len(paths) > 1 || len(paths) == 1 && paths[0].Conditional
	switch a.Status {
	case "ambiguous":
		// A call ambiguous inside its repository may be resolved across link
		// scopes (rows PL-S1 to PL-S3), alternative by alternative.
		if len(m.Scopes) == 0 || len(paths) == 0 {
			p.record(KindCallsRoute, m, n, "", RowInRepository)
			return
		}
		if !several {
			path := paths[0]
			if path.Status == "ambiguous" && path.Template != nil && a.Method != nil && p.overlay(callSource(m, n, a, path, a.Base, a.BaseExpression)) {
				return
			}
			p.record(KindCallsRoute, m, n, "", RowInRepository)
			return
		}
	case "wrapper", "linked", "test_only", "generated":
		// generated: committed bundler output, never linked (ADR 0085).
		p.record(KindCallsRoute, m, n, "", RowInRepository)
		return
	case "dynamic":
		if !several {
			p.record(KindCallsRoute, m, n, "", RowDynamic)
			return
		}
	case "unmatched":
	default:
		p.record(KindCallsRoute, m, n, "", RowUnknown)
		return
	}
	if len(paths) == 0 {
		// An Orval call bound to the wrong generated file keeps no paths.
		p.record(KindCallsRoute, m, n, "", RowDynamic)
		return
	}
	if several {
		for _, path := range paths {
			p.alternative(m, n, a, path)
		}
		return
	}
	path := paths[0]
	if path.Status != "unmatched" || path.Template == nil || a.Method == nil {
		p.record(KindCallsRoute, m, n, "", RowUnknown)
		return
	}
	p.resolve(callSource(m, n, a, path, a.Base, a.BaseExpression))
}

// alternative classifies one alternative of a call with several or
// conditional alternatives, from its own status and stored base.
func (p *pass) alternative(m *Member, n contract.Node, a callAttributes, path callPath) {
	template := ""
	if path.Template != nil {
		template = *path.Template
	}
	switch path.Status {
	case "ambiguous":
		if path.Template != nil && a.Method != nil && path.Base != "" {
			expression := ""
			if path.BaseExpression != nil {
				expression = *path.BaseExpression
			}
			if p.overlay(callSource(m, n, a, path, path.Base, expression)) {
				return
			}
		}
		p.record(KindCallsRoute, m, n, template, RowInRepository)
		return
	case "linked", "test_only":
		p.record(KindCallsRoute, m, n, template, RowInRepository)
		return
	case "dynamic":
		p.record(KindCallsRoute, m, n, template, RowDynamic)
		return
	case "unmatched":
	default:
		p.record(KindCallsRoute, m, n, template, RowUnknown)
		return
	}
	switch {
	case path.Template == nil:
		p.record(KindCallsRoute, m, n, template, RowDynamic)
	case a.Method == nil:
		p.record(KindCallsRoute, m, n, template, RowUnknown)
	case path.Base == "":
		// An index before extractors-47 does not store the alternative's base.
		p.record(KindCallsRoute, m, n, template, RowAlternatives)
	default:
		expression := ""
		if path.BaseExpression != nil {
			expression = *path.BaseExpression
		}
		p.resolve(callSource(m, n, a, path, path.Base, expression))
	}
}

// callSource builds the source for one call alternative with the base kind
// and expression the in-repository linker used for it.
func callSource(m *Member, n contract.Node, a callAttributes, path callPath, base, expression string) source {
	s := source{kind: KindCallsRoute, member: m, node: n, template: *path.Template, baseExpression: expression}
	s.call = clients.CallRequest{Method: *a.Method, Template: *path.Template, Scope: a.Scope, Base: base, OriginUnresolved: path.OriginResolution == "unresolved", TrailingSlash: path.TrailingSlash}
	if path.Origin != nil {
		s.call.Origin = *path.Origin
		s.literalOrigin = strings.ToLower(*path.Origin)
	}
	return s
}

// operation classifies each path entry of one api_operation node.
func (p *pass) operation(m *Member, n contract.Node) {
	var a operationAttributes
	if !decode(n.Attributes, &a) || a.Paths == nil || a.Method == nil {
		p.record(KindDocumentsRoute, m, n, "", RowUnknown)
		return
	}
	for _, path := range *a.Paths {
		template := ""
		if path.Template != nil {
			template = *path.Template
		}
		origins := make([]string, 0, len(path.Origins))
		for _, o := range path.Origins {
			origins = append(origins, strings.ToLower(o))
		}
		s := source{kind: KindDocumentsRoute, member: m, node: n, template: template, method: *a.Method, scope: a.Scope, trailingSlash: path.TrailingSlash, origins: origins}
		switch path.Status {
		case "ambiguous":
			if path.Template == nil || !p.overlay(s) {
				p.record(KindDocumentsRoute, m, n, template, RowInRepository)
			}
			continue
		case "linked", "test_only":
			p.record(KindDocumentsRoute, m, n, template, RowInRepository)
			continue
		case "unresolved":
			p.record(KindDocumentsRoute, m, n, template, RowDynamic)
			continue
		case "unmatched":
		default:
			p.record(KindDocumentsRoute, m, n, template, RowUnknown)
			continue
		}
		if path.Template == nil {
			p.record(KindDocumentsRoute, m, n, template, RowDynamic)
			continue
		}
		p.resolve(s)
	}
}

// covers reports whether consume entry c, one of the consumer's entries
// list, covers source s (docs "Candidate providers"). Selectors narrow
// client calls only: an OpenAPI operation has no base, so every entry
// covers it and operations follow the consumer's providers as with entries
// without `select` (T022 B6c R1). The exception is literal evidence: when
// an entry's `origins` names one of the operation's server origins, only
// such entries cover it, as for a call with that literal origin.
func covers(c config.Consume, s source, list []config.Consume) bool {
	if s.kind == KindDocumentsRoute {
		named := false
		for _, e := range list {
			named = named || namesOrigin(e, s.origins)
		}
		return !named || namesOrigin(c, s.origins)
	}
	prefixOK := len(c.Select.PathPrefixes) == 0
	for _, prefix := range c.Select.PathPrefixes {
		trimmed := strings.TrimSuffix(prefix, "/")
		if s.template == prefix || trimmed == "" || s.template == trimmed || strings.HasPrefix(s.template, trimmed+"/") {
			prefixOK = true
		}
	}
	if !prefixOK {
		return false
	}
	noAddress := len(c.Select.Bases) == 0 && len(c.Select.Origins) == 0
	if s.literalOrigin != "" {
		for _, origin := range c.Select.Origins {
			if strings.ToLower(origin) == s.literalOrigin {
				return true
			}
		}
		return false
	}
	if noAddress {
		return true
	}
	if s.baseExpression == "" {
		return false
	}
	for _, b := range c.Select.Bases {
		if s.baseExpression == b || s.baseExpression == "import.meta.env."+b || s.baseExpression == "process.env."+b {
			return true
		}
	}
	return false
}

// namesOrigin reports whether c's `origins` lists one of origins (lower
// case server origins of an operation).
func namesOrigin(c config.Consume, origins []string) bool {
	for _, origin := range c.Select.Origins {
		for _, o := range origins {
			if strings.ToLower(origin) == o {
				return true
			}
		}
	}
	return false
}

// consumer returns the name a declaration shows for the consumer of s and
// the consumes list that applies to it: the declared link scope holding the
// source (repository/scope, with that scope's own consumes) or, for a
// source in the member's default scope, the member and its consumes.
func consumer(s source) (string, []config.Consume) {
	m := s.member
	if sc := scopeOf(m, s.node.File); sc != nil {
		return m.Repository + "/" + sc.Name, sc.Consumes
	}
	return m.Repository, m.Consumes
}

// candidate is an endpoint that may serve a source, with the consumes entry
// that made it one (nil without consumes).
type candidate struct {
	provider *provider
	entry    *config.Consume
}

// declared returns the endpoints named by the entries of list that cover
// s, in manifest member order. An entry naming a scope names that scope's
// endpoint; an entry naming only a repository names every endpoint of it
// (all its declared scopes and its default scope). An endpoint named by
// several entries keeps the first.
func (p *pass) declared(list []config.Consume, s source) []candidate {
	var out []candidate
	for _, member := range p.members {
		for _, e := range p.endpoints[member.Repository] {
			for i := range list {
				c := &list[i]
				if c.Repository == member.Repository && names(*c, e) && covers(*c, s, list) {
					out = append(out, candidate{provider: e, entry: c})
					break
				}
			}
		}
	}
	return out
}

// names reports whether entry c names endpoint e of c's repository: an
// entry with scope names that scope, one with default_scope the member's
// default scope (manifest validation requires the member to declare scopes,
// so e.scope is empty only for it), and any other entry every endpoint.
func names(c config.Consume, e *provider) bool {
	switch {
	case c.DefaultScope:
		return e.scope == ""
	case c.Scope != "":
		return c.Scope == e.scope
	}
	return true
}

// matched is one endpoint's matcher result for a source.
type matched struct {
	candidate
	result clients.CallResult
}

// match runs e's matcher for s: the client rules for a call, the exact
// tier for an operation path (0.9, or 0.7 with server origins). A source
// of another member is matched against e's Next.js routes too (cross);
// one of e's own member never is (the same-origin rule, NX-L1, stays the
// repository's own).
func match(c candidate, s source) clients.CallResult {
	e := c.provider
	matcher := e.matcher
	if e.member != s.member {
		matcher = e.cross
	}
	if s.kind == KindCallsRoute {
		// A consumes entry that covers the source names e, an endpoint of
		// another member: the declaration rows UP-21 and UP-26 (PL-L7).
		declared := c.entry != nil && e.member != s.member
		request := s.call
		// covers admits a literal origin only through an entry whose
		// origins name it.
		request.DeclaredOrigin = declared && s.literalOrigin != ""
		r := matcher.Call(request)
		if r.UnknownPrefix {
			if !declared {
				// Row UP-25 (PL-R13): without a declaration naming the
				// provider, an outcome resting on unknown-prefix entries is
				// never a link, a tie or a possible caller here.
				return clients.CallResult{Status: statusUnknownPrefix}
			}
			// Row UP-26: the declaration stands in for the proof of the
			// server; the prefix stays unknown (UP-V1, UP-V2 as the
			// matcher records them) and the declaration is assumed
			// (UP-V3). UP-16 and UP-17 hold inside the provider's
			// matcher; a second possible provider makes it ambiguous
			// (PL-R11).
			r.Assumes = append(append([]string(nil), r.Assumes...), clients.AssumeDeclaredProvider)
		}
		return r
	}
	// Row LF-1 for operations: a spec's servers are the provider's only
	// when a consumes entry names one of them; otherwise a literal-free
	// route is no candidate, in the spec's own member too.
	unproven := len(s.origins) > 0 && (c.entry == nil || !namesOrigin(*c.entry, s.origins))
	exact := matcher.Exact(clients.ExactRequest{Method: s.method, Template: s.template, Scope: s.scope, TrailingSlash: s.trailingSlash, UnprovenOrigin: unproven})
	r := clients.CallResult{Status: exact.Status, Match: exact.Match, Candidates: exact.Candidates, CandidateCount: exact.CandidateCount, Assumes: exact.Assumes}
	if exact.Status == "linked" {
		r.Confidence = 0.9
		if len(s.origins) > 0 {
			r.Confidence = 0.7
		}
	}
	return r
}

// statusUnknownPrefix is match's status for a source whose matcher result
// rests on unknown-prefix entries (CallResult.UnknownPrefix): no link, no
// tie, no possible caller (row UP-25, PL-R13).
const statusUnknownPrefix = "unknown_prefix"

// unindexedMembers names, in order and once each, the members of
// candidates without a stored index.
func unindexedMembers(candidates []candidate) []string {
	var out []string
	seen := map[string]bool{}
	for _, c := range candidates {
		if c.provider.member.Run == "" && !seen[c.provider.member.Repository] {
			seen[c.provider.member.Repository] = true
			out = append(out, c.provider.member.Repository)
		}
	}
	return out
}

// listCandidates fills o's candidates (at most ten, ordered) and full count
// from the endpoints' results.
func listCandidates(o *Outcome, matches []matched) {
	count := 0
	for _, mt := range matches {
		for _, id := range mt.result.Candidates {
			o.Candidates = append(o.Candidates, Endpoint{Repository: mt.provider.member.Repository, Identity: id})
		}
		if mt.result.CandidateCount > 0 {
			count += mt.result.CandidateCount
		} else {
			count += len(mt.result.Candidates)
		}
	}
	sort.Slice(o.Candidates, func(i, j int) bool {
		a, b := o.Candidates[i], o.Candidates[j]
		return a.Repository < b.Repository || a.Repository == b.Repository && a.Identity < b.Identity
	})
	if len(o.Candidates) > 10 {
		o.Candidates = o.Candidates[:10]
	}
	if count > 1 {
		o.CandidateCount = count
	}
}

// resolve applies rows PL-R5 to PL-L6 to a source the member's own index
// left unmatched.
func (p *pass) resolve(s source) {
	m := s.member
	name, list := consumer(s)
	var candidates []candidate
	if len(list) == 0 {
		if s.kind == KindCallsRoute && s.literalOrigin != "" {
			p.record(s.kind, m, s.node, s.template, RowLiteralOrigin)
			return
		}
		if s.kind == KindDocumentsRoute && len(p.whole[m.Repository].routes) > 0 {
			// PL-R12: a server's own spec documents its own server; only a
			// consumes entry naming the provider links it across.
			o := p.record(s.kind, m, s.node, s.template, RowServerSpec)
			o.Reason = ServerSpecReason
			return
		}
		for _, other := range p.members {
			if other.Repository == m.Repository {
				continue
			}
			for _, e := range p.endpoints[other.Repository] {
				candidates = append(candidates, candidate{provider: e})
			}
		}
	} else {
		candidates = p.declared(list, s)
		if len(candidates) == 0 {
			id := RowNotDeclared
			if s.kind == KindCallsRoute && s.literalOrigin != "" {
				id = RowLiteralOrigin
			}
			p.record(s.kind, m, s.node, s.template, id)
			return
		}
	}
	if unindexed := unindexedMembers(candidates); len(unindexed) > 0 {
		o := p.record(s.kind, m, s.node, s.template, RowProviderUnindexed)
		o.Reason = "no stored index: " + strings.Join(unindexed, ", ")
		// The indexed providers' matching routes are listed (T022 review):
		// views show the source as a possible caller of exactly
		// those, never a link.
		var indexed []matched
		for _, c := range candidates {
			if c.provider.member.Run == "" {
				continue
			}
			if r := match(c, s); r.Status == "linked" || r.Status == "ambiguous" {
				indexed = append(indexed, matched{c, r})
			}
		}
		listCandidates(o, indexed)
		if len(indexed) == 0 {
			o.Reason += "; " + NoIndexedCandidate
		}
		return
	}

	var possible, testOnly []matched
	prefixOnly := false
	for _, c := range candidates {
		r := match(c, s)
		switch r.Status {
		case "linked", "ambiguous":
			possible = append(possible, matched{c, r})
		case "test_only":
			testOnly = append(testOnly, matched{c, r})
		case statusUnknownPrefix:
			prefixOnly = true
		}
	}
	switch {
	case len(possible) == 0 && len(testOnly) == 0 && prefixOnly:
		o := p.record(s.kind, m, s.node, s.template, RowUnknownPrefix)
		o.Reason = UnknownPrefixReason
		return
	case len(possible) == 0 && len(testOnly) == 0:
		p.record(s.kind, m, s.node, s.template, RowNoCandidate)
		return
	case len(possible) == 0:
		listCandidates(p.record(s.kind, m, s.node, s.template, RowTestOnly), testOnly)
		return
	case len(possible) > 1:
		listCandidates(p.record(s.kind, m, s.node, s.template, RowSeveralProviders), possible)
		return
	case possible[0].result.Status == "ambiguous":
		listCandidates(p.record(s.kind, m, s.node, s.template, RowProviderAmbiguous), possible)
		return
	}

	winner := possible[0]
	var id string
	switch {
	case winner.result.UnknownPrefix:
		// match admits an unknown-prefix result only through a covering
		// consumes entry naming another member (row UP-26).
		id = RowCallUnknownPrefix
	case s.kind == KindDocumentsRoute && winner.entry == nil:
		id = RowDocumentsDefault
	case s.kind == KindDocumentsRoute:
		id = RowDocumentsDeclared
	case winner.entry == nil:
		id = RowCallDefault
	case s.literalOrigin != "":
		id = RowCallOrigin
	case winner.entry.Select.Empty():
		id = RowCallDeclared
	default:
		id = RowCallSelector
	}
	p.link(s, name, winner, id)
}

// overlay applies rows PL-S1 to PL-S3 to a source its member's own index
// left ambiguous, and reports whether one applied. They apply only when the
// member declares link scopes and a consumes entry of the source's
// consumer (its declared scope, or the member for its default scope) that
// covers the source names the member's own repository. Otherwise the
// repository's own result stands (PL-R1).
//
// The declarations may only narrow the in-repository candidates: the
// overlay links only when exactly one covered endpoint has a candidate,
// that endpoint is a scope of the source's own repository, its matcher
// links one route, and that route is one of the candidates the member's
// own matcher (over all its routes, replaying its own result) left tied.
// Anything else stays ambiguous (PL-S3), never a link chosen by order.
func (p *pass) overlay(s source) bool {
	m := s.member
	if len(m.Scopes) == 0 {
		return false
	}
	name, list := consumer(s)
	candidates := p.declared(list, s)
	own := false
	for _, c := range candidates {
		own = own || c.provider.member == m
	}
	if !own {
		return false
	}
	full := match(candidate{provider: p.whole[m.Repository]}, s)
	tied := map[string]bool{}
	for _, id := range full.Candidates {
		tied[id] = true
	}
	// A truncated candidate list (more than ten ties) cannot prove the
	// narrowed route was among them.
	complete := full.Status == "ambiguous" && full.CandidateCount <= len(full.Candidates)
	unindexed := unindexedMembers(candidates)
	var possible []matched
	for _, c := range candidates {
		if c.provider.member.Run == "" {
			continue
		}
		r := match(c, s)
		if r.Status == "linked" || r.Status == "ambiguous" {
			possible = append(possible, matched{c, r})
		}
	}
	if complete && len(unindexed) == 0 && len(possible) == 1 {
		winner := possible[0]
		if winner.provider.member == m && winner.result.Status == "linked" && tied[winner.result.Candidates[0]] {
			id := RowScopeCall
			if s.kind == KindDocumentsRoute {
				id = RowScopeDocuments
			}
			p.link(s, name, winner, id)
			return true
		}
	}
	// The outcome lists what the declarations leave: other repositories'
	// candidates and the in-repository ties inside the declared scopes; when
	// that is nothing, the in-repository ties.
	o := p.record(s.kind, m, s.node, s.template, RowScopeUnresolved)
	var left []matched
	for _, mt := range possible {
		if mt.provider.member != m {
			left = append(left, mt)
			continue
		}
		var ids []string
		for _, id := range mt.result.Candidates {
			if tied[id] {
				ids = append(ids, id)
			}
		}
		if len(ids) > 0 {
			left = append(left, matched{mt.candidate, clients.CallResult{Status: mt.result.Status, Candidates: ids}})
		}
	}
	if len(left) == 0 {
		left = []matched{{candidate: candidate{provider: p.whole[m.Repository]}, result: full}}
	}
	listCandidates(o, left)
	if len(unindexed) > 0 {
		o.Reason = "no stored index: " + strings.Join(unindexed, ", ")
	}
	return true
}

// link records the outcome of a linking row and adds its link from the
// source to winner's single route.
func (p *pass) link(s source, consumerName string, winner matched, id string) {
	m := s.member
	assumption, declaration := DefaultAssumption, ""
	if winner.entry != nil {
		entry := *winner.entry
		if s.kind == KindDocumentsRoute && !namesOrigin(entry, s.origins) {
			// The selector did not decide this operation (covers).
			entry.Select = config.Selector{}
		}
		declaration = declarationText(consumerName, entry)
		assumption = "declared in " + config.ProjectFile + ": " + declaration
	}
	if id == RowScopeCall || id == RowScopeDocuments {
		assumption += "; " + ScopeOverlayNote
	}
	assumes := winner.result.Assumes
	// The matched route path entry's assumptions (GR-V5, RO-V1,
	// RO-V2) get their own note; the rest are the tier's.
	var rowAssumes, pathAssumes []string
	for _, id := range assumes {
		if clients.PathAssumption(id) {
			pathAssumes = append(pathAssumes, id)
		} else {
			rowAssumes = append(rowAssumes, id)
		}
	}
	switch {
	case winner.result.UnknownPrefix:
		assumption += "; " + UnknownPrefixAssumptionNote(rowAssumes)
	case len(rowAssumes) > 0:
		assumption += "; " + NextAssumptionNote(rowAssumes)
	}
	if len(pathAssumes) > 0 {
		assumption += "; " + PathAssumptionNote(pathAssumes)
	}
	// An origin nothing proves to be the provider's (row LF-1): a call's
	// unresolved origin, or an operation's servers no consumes entry names.
	origin := winner.result.AssumedOrigin
	if s.kind == KindDocumentsRoute && len(s.origins) > 0 && (winner.entry == nil || !namesOrigin(*winner.entry, s.origins)) {
		origin = s.origins[0]
	}
	if origin != "" {
		assumption += "; " + clients.OriginAssumption(origin, winner.provider.member.Repository)
	}
	target := winner.provider.routes[winner.result.Candidates[0]]
	o := p.record(s.kind, m, s.node, s.template, id)
	o.Target = &Endpoint{Repository: winner.provider.member.Repository, Identity: target.Identity}
	o.Candidates = []Endpoint{*o.Target}

	evidence := []contract.Evidence{}
	if s.node.Span != nil {
		evidence = append(evidence, contract.Evidence{File: s.node.File, Span: *s.node.Span})
	}
	attributes := map[string]any{"row": id, "match": winner.result.Match, "template": s.template}
	if declaration != "" {
		attributes["declaration"] = declaration
	}
	if sc := scopeOf(m, s.node.File); sc != nil {
		attributes["source_scope"] = sc.Name
	}
	if winner.provider.scope != "" {
		attributes["target_scope"] = winner.provider.scope
	}
	if winner.result.UnknownPrefix {
		attributes["route_prefix"] = "unresolved"
	}
	link := store.ProjectLink{
		Kind:       s.kind,
		Source:     store.ProjectEndpoint{Repository: m.Repository, Identity: s.node.Identity, Run: m.Run, ContentHash: s.node.ContentHash},
		Target:     store.ProjectEndpoint{Repository: winner.provider.member.Repository, Identity: target.Identity, Run: winner.provider.member.Run, ContentHash: target.ContentHash},
		Provenance: "inferred",
		Confidence: winner.result.Confidence,
		Evidence:   evidence,
		Assumption: assumption,
		Assumes:    assumes,
		Attributes: attributes,
	}
	// Several path entries of one operation reaching one target keep the
	// strongest confidence, as inside one repository.
	if old, ok := p.links[link.Key()]; !ok || link.Confidence > old.Confidence {
		p.links[link.Key()] = link
	}
}

// declarationText names a consumes entry and its selector lists, for the
// assumption a declared link shows.
func declarationText(consumer string, c config.Consume) string {
	provider := c.Repository
	if c.Scope != "" {
		provider += "/" + c.Scope
	}
	if c.DefaultScope {
		provider += " (default scope)"
	}
	text := fmt.Sprintf("`%s` consumes `%s`", consumer, provider)
	var parts []string
	for _, l := range []struct {
		name   string
		values []string
	}{{"bases", c.Select.Bases}, {"origins", c.Select.Origins}, {"path_prefixes", c.Select.PathPrefixes}} {
		if len(l.values) > 0 {
			parts = append(parts, l.name+": "+strings.Join(l.values, ", "))
		}
	}
	if len(parts) > 0 {
		text += " (" + strings.Join(parts, "; ") + ")"
	}
	return text
}

// CoveredCalls counts the call alternatives of m's endpoint scope (a
// declared scope's name, or empty for the member's default scope, which is
// the whole member when it declares none) that entry c covers, by the same
// rules the pass uses (covers). Only alternatives the pass could link to
// another endpoint count: `unmatched` or `ambiguous`, with a template and a
// method, and with a stored base for an alternative of a call with several.
// `graphvein project suggest` uses it to propose only selectors that name
// what the consumer's stored calls record (T022 B6b).
func CoveredCalls(m *Member, scope string, c config.Consume) int {
	n := 0
	for _, node := range m.Nodes {
		if node.Kind != "client_call" {
			continue
		}
		in := ""
		if sc := scopeOf(m, node.File); sc != nil {
			in = sc.Name
		}
		if in != scope {
			continue
		}
		var a callAttributes
		if !decode(node.Attributes, &a) || a.Paths == nil || a.Method == nil {
			continue
		}
		paths := *a.Paths
		several := len(paths) > 1 || len(paths) == 1 && paths[0].Conditional
		for _, path := range paths {
			status, base, expression := a.Status, a.Base, a.BaseExpression
			if several {
				status, base = path.Status, path.Base
				expression = ""
				if path.BaseExpression != nil {
					expression = *path.BaseExpression
				}
			}
			if status != "unmatched" && status != "ambiguous" || path.Template == nil || several && base == "" {
				continue
			}
			if covers(c, callSource(m, node, a, path, base, expression), nil) {
				n++
			}
		}
	}
	return n
}
