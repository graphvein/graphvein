package server

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/openapi"
	"github.com/graphvein/graphvein/internal/query"
	"github.com/graphvein/graphvein/internal/store"
)

// This file serves GET /api/v0/search, the jump-to palette's search (T040,
// ADR 0089) under the claim contract docs/contract/claims/jump-search.md
// (rows JS-1..JS-21 and D). It matches stored names and paths only: it
// never reads a file's text and never states that something does not
// exist.

// searchPath is the search endpoint.
const searchPath = "/api/v0/search"

// Bounds of a search request (rows JS-1, JS-3).
const (
	searchMaxQuery     = 200
	searchDefaultLimit = 10
	searchMaxLimit     = 50
)

// Result kinds in their fixed order (row JS-11).
const (
	searchRoute     = "route"
	searchOperation = "operation"
	searchTable     = "table"
	searchSymbol    = "symbol"
	searchFile      = "file"
)

var searchKinds = []string{searchRoute, searchOperation, searchTable, searchSymbol, searchFile}

// Match tiers (row JS-9), best first.
const (
	tierPrefix = iota
	tierWord
	tierSubstring
)

var tierNames = []string{"prefix", "word", "substring"}

// absenceNotProven is the only absence a search answer states (row JS-14).
const absenceNotProven = "not_proven"

// symbolKinds are the node kinds searched as symbols (row JS-7): the
// declaration kinds the language readers emit at level 4.
var symbolKinds = map[string]bool{
	"annotation": true, "class": true, "constant": true, "constructor": true, "enum": true,
	"extension": true, "extension_type": true, "function": true, "interface": true, "macro": true,
	"method": true, "mixin": true, "record": true, "struct": true, "trait": true, "type": true,
	"type_alias": true, "union": true, "variable": true,
}

// searchRow is one row of the claim contract; searchRows and the document
// are kept 1:1 by TestSearchContractTable, and each row names the test
// that checks it.
type searchRow struct {
	ID    string
	Topic string
	Test  string
}

// searchRows is the claim contract in table order.
var searchRows = []searchRow{
	{"JS-1", "query", "TestSearchParameters"},
	{"JS-2", "kinds", "TestSearchParameters"},
	{"JS-3", "limit", "TestSearchParameters"},
	{"JS-4", "routes", "TestSearchFixtureGolden"},
	{"JS-5", "operations", "TestSearchOperations"},
	{"JS-6", "tables", "TestSearchSynthetic"},
	{"JS-7", "symbols", "TestSearchSynthetic"},
	{"JS-8", "files", "TestSearchFixtureGolden"},
	{"JS-9", "tiers", "TestSearchTiers"},
	{"JS-10", "ranking", "TestSearchSynthetic"},
	{"JS-11", "group order", "TestSearchSynthetic"},
	{"JS-12", "totals", "TestSearchSynthetic"},
	{"JS-13", "searched", "TestSearchSynthetic"},
	{"JS-14", "no result", "TestSearchNoResult"},
	{"JS-15", "project scope", "TestSearchProject"},
	{"JS-16", "project order", "TestSearchProject"},
	{"JS-17", "no checkout", "TestSearchProject"},
	{"JS-18", "not indexed", "TestSearchNoResult"},
	{"JS-19", "editor", "TestSearchParameters"},
	{"JS-20", "everything else", "TestSearchSynthetic"},
	{"JS-21", "in-process reader", "TestSearchReader"},
	{"D", "default", "TestSearchSynthetic"},
}

// searchRequest is a parsed search request.
type searchRequest struct {
	query  string
	kinds  []string
	limit  int
	editor *editorChoice
}

func (r searchRequest) wants(kind string) bool {
	for _, k := range r.kinds {
		if k == kind {
			return true
		}
	}
	return false
}

// parseSearch reads q, kinds, limit and editor (rows JS-1..JS-3, JS-19).
func parseSearch(q url.Values) (searchRequest, error) {
	var out searchRequest
	text, present, err := param(q, "q")
	if err != nil {
		return out, err
	}
	text = strings.TrimSpace(text)
	if !present || text == "" {
		return out, badRequest("parameter q is required")
	}
	if len(text) > searchMaxQuery || !utf8.ValidString(text) {
		return out, badRequest("q must be valid UTF-8 of at most %d bytes", searchMaxQuery)
	}
	out.query = text
	kinds, present, err := param(q, "kinds")
	if err != nil {
		return out, err
	}
	if !present {
		out.kinds = append([]string(nil), searchKinds...)
	} else {
		asked := map[string]bool{}
		for _, k := range strings.Split(kinds, ",") {
			known := false
			for _, s := range searchKinds {
				known = known || s == k
			}
			if !known {
				return out, badRequest("kinds must list route, operation, table, symbol or file, not %q", k)
			}
			if asked[k] {
				return out, badRequest("kinds names %s more than once", k)
			}
			asked[k] = true
		}
		for _, s := range searchKinds {
			if asked[s] {
				out.kinds = append(out.kinds, s)
			}
		}
	}
	out.limit = searchDefaultLimit
	limit, present, err := param(q, "limit")
	if err != nil {
		return out, err
	}
	if present {
		n, err := strconv.Atoi(limit)
		if err != nil || n < 1 || n > searchMaxLimit {
			return out, badRequest("limit must be an integer from 1 to %d, not %q", searchMaxLimit, limit)
		}
		out.limit = n
	}
	out.editor, err = editorParam(q)
	return out, err
}

// Response bodies.

// searchResponse is the body of a one-repository search (and of one
// project member's, with repository=).
type searchResponse struct {
	APIVersion int            `json:"api_version"`
	Repository string         `json:"repository"`
	Query      string         `json:"query"`
	Kinds      []string       `json:"kinds"`
	Limit      int            `json:"limit"`
	IDE        ideStatus      `json:"ide"`
	Index      *lastRun       `json:"index"`
	Searched   searchedDoc    `json:"searched"`
	Absence    string         `json:"absence"`
	Totals     map[string]int `json:"totals"`
	Groups     []searchGroup  `json:"groups"`
}

// searchProjectResponse is the body of a project-scope search (rows
// JS-15, JS-16).
type searchProjectResponse struct {
	APIVersion int               `json:"api_version"`
	Project    string            `json:"project"`
	Query      string            `json:"query"`
	Kinds      []string          `json:"kinds"`
	Limit      int               `json:"limit"`
	Absence    string            `json:"absence"`
	Members    []searchMemberDoc `json:"members"`
	Totals     map[string]int    `json:"totals"`
	Groups     []searchGroup     `json:"groups"`
}

// searchMemberDoc is one member's part of a project-scope search.
type searchMemberDoc struct {
	Repository string         `json:"repository"`
	Checkout   bool           `json:"checkout"`
	IDE        ideStatus      `json:"ide"`
	Index      *lastRun       `json:"index"`
	Searched   searchedDoc    `json:"searched"`
	Totals     map[string]int `json:"totals"`
}

// searchedDoc names what one repository's search read (row JS-13).
type searchedDoc struct {
	Indexed bool `json:"indexed"`
	// Files counts the stored file records.
	Files int `json:"files"`
	// FilesNotRead counts the files the couldn't-read page lists as not
	// read (row CR-19); ScannerGaps the scanner's paths not indexed
	// (row CR-21). Neither is searched.
	FilesNotRead int `json:"files_not_read"`
	ScannerGaps  int `json:"scanner_gaps"`
}

// searchGroup is one kind's results.
type searchGroup struct {
	Kind string `json:"kind"`
	// Available is false when the kind could not be searched in a
	// repository; Unavailable names each with the reason (row JS-5).
	Available   bool                `json:"available"`
	Unavailable []searchUnavailable `json:"unavailable"`
	Total       int                 `json:"total"`
	// BestTier is the best tier of the kind's matches, null without one.
	BestTier  *string      `json:"best_tier"`
	Truncated bool         `json:"truncated"`
	Items     []searchItem `json:"items"`
}

type searchUnavailable struct {
	Repository string `json:"repository"`
	Reason     string `json:"reason"`
}

// searchItem is one result. Label is the text to show; the kind-specific
// fields are present only for their kind.
type searchItem struct {
	Kind       string `json:"kind"`
	Repository string `json:"repository"`
	// ID is the node identity: the route for routes and operations, the
	// table, the symbol, the file node for files (null without one).
	ID    *string `json:"id"`
	Label string  `json:"label"`
	// Tier and MatchKey say which key matched and how (rows JS-9, JS-10).
	Tier     string         `json:"tier"`
	MatchKey string         `json:"match_key"`
	File     string         `json:"file"`
	Line     *int           `json:"line"`
	IDE      *query.IDELink `json:"ide"`

	Method        *string `json:"method,omitempty"`
	Path          *string `json:"path,omitempty"`
	PrefixUnknown *string `json:"prefix_unknown,omitempty"`
	Test          *bool   `json:"test,omitempty"`
	NodeKind      *string `json:"node_kind,omitempty"`
	QualifiedName *string `json:"qualified_name,omitempty"`
	Source        *string `json:"source,omitempty"`
	Language      *string `json:"language,omitempty"`
}

// Matching.

// searchKey is one key of a candidate with the name the answer gives it.
type searchKey struct {
	name, text string
}

// searchMatch is a candidate's best match (rows JS-9, JS-10).
type searchMatch struct {
	tier int
	key  searchKey
}

// searchTierOf is the tier of query (already folded) in key, or -1 when it does
// not occur (row JS-9).
func searchTierOf(key, folded string) int {
	k := store.FoldASCII(key)
	if strings.HasPrefix(k, folded) {
		return tierPrefix
	}
	best := -1
	for from := 0; from <= len(k)-len(folded); {
		i := strings.Index(k[from:], folded)
		if i < 0 {
			break
		}
		at := from + i
		if wordStart(key, at) {
			return tierWord
		}
		best = tierSubstring
		from = at + 1
	}
	return best
}

// wordStart reports whether byte i of key begins a word: the start, after
// a byte that is no ASCII letter or digit, at a lower-to-upper case change
// or at a change between letters and digits.
func wordStart(key string, i int) bool {
	if i == 0 {
		return true
	}
	prev, cur := key[i-1], key[i]
	switch {
	case !isASCIIAlnum(prev):
		return true
	case 'a' <= prev && prev <= 'z' && 'A' <= cur && cur <= 'Z':
		return true
	case isASCIIDigit(prev) != isASCIIDigit(cur) && isASCIIAlnum(cur):
		return true
	}
	return false
}

func isASCIIDigit(c byte) bool { return '0' <= c && c <= '9' }

func isASCIIAlnum(c byte) bool {
	return isASCIIDigit(c) || 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z'
}

// bestMatch is the best tier over keys, then the shortest key, then the
// key's bytes (row JS-10).
func bestMatch(keys []searchKey, folded string) (searchMatch, bool) {
	var best searchMatch
	found := false
	for _, k := range keys {
		t := searchTierOf(k.text, folded)
		if t < 0 {
			continue
		}
		m := searchMatch{tier: t, key: k}
		if !found || lessMatch(m, best) {
			best, found = m, true
		}
	}
	return best, found
}

func lessMatch(a, b searchMatch) bool {
	if a.tier != b.tier {
		return a.tier < b.tier
	}
	if len(a.key.text) != len(b.key.text) {
		return len(a.key.text) < len(b.key.text)
	}
	return a.key.text < b.key.text
}

// searchCandidate is one match before its item is built.
type searchCandidate struct {
	match searchMatch
	id    string
	build func(searchMatch) searchItem
}

func sortCandidates(c []searchCandidate) {
	sort.SliceStable(c, func(i, j int) bool {
		if lessMatch(c[i].match, c[j].match) {
			return true
		}
		if lessMatch(c[j].match, c[i].match) {
			return false
		}
		return c[i].id < c[j].id
	})
}

// One repository's search.

// memberSearch is one repository's ranked results per kind: at most
// limit built items each, plus the totals.
type memberSearch struct {
	repository string
	ide        ideStatus
	index      *lastRun
	searched   searchedDoc
	groups     map[string]*memberGroup
}

type memberGroup struct {
	unavailable *string
	total       int
	best        int
	items       []rankedItem
}

type rankedItem struct {
	match searchMatch
	item  searchItem
}

// searchRepository runs one repository's search inside the API's
// consistency check (rows JS-4..JS-13, JS-17, JS-18).
func searchRepository(ctx context.Context, opt Options, req searchRequest) (memberSearch, error) {
	h := &apiHandler{opt: opt}
	body, err := h.consistent(ctx, func(ctx context.Context, rd *reader, runs []store.Run) (any, error) {
		return searchRead(ctx, h, rd, runs, req)
	})
	if err != nil {
		return memberSearch{}, err
	}
	return body.(memberSearch), nil
}

func searchRead(ctx context.Context, h *apiHandler, rd *reader, runs []store.Run, req searchRequest) (memberSearch, error) {
	out := memberSearch{repository: h.opt.Repository, index: indexOf(runs), groups: map[string]*memberGroup{}}
	for _, k := range req.kinds {
		out.groups[k] = &memberGroup{best: -1, items: []rankedItem{}}
	}
	hashes, err := rd.fileHashes()
	if err != nil {
		return out, err
	}
	p := h.newPresenter(hashes, req.editor)
	out.ide = p.ide
	if out.index == nil {
		// Row JS-18: nothing stored, nothing searched.
		return out, nil
	}
	out.searched.Indexed = true
	out.searched.Files = len(hashes)
	gaps, err := readFileGaps(rd, hashes)
	if err != nil {
		return out, err
	}
	out.searched.FilesNotRead, out.searched.ScannerGaps = len(gaps.notReadFiles()), len(gaps.scanner)
	folded := store.FoldASCII(req.query)
	add := func(kind string, candidates []searchCandidate) {
		g := out.groups[kind]
		sortCandidates(candidates)
		g.total = len(candidates)
		if len(candidates) > 0 {
			g.best = candidates[0].match.tier
		}
		for i, c := range candidates {
			if i == req.limit {
				break
			}
			item := c.build(c.match)
			item.Kind, item.Repository = kind, h.opt.Repository
			item.Tier, item.MatchKey = tierNames[c.match.tier], c.match.key.name
			g.items = append(g.items, rankedItem{match: c.match, item: item})
		}
	}
	ideOf := func(n contract.Node) *query.IDELink { return p.node(n).IDE }
	if req.wants(searchRoute) {
		routes, err := rd.nodes(httpTransport.EndpointKind)
		if err != nil {
			return out, err
		}
		withheld, err := rd.withheldSites()
		if err != nil {
			return out, err
		}
		var candidates []searchCandidate
		for _, n := range routes {
			paths := fullPaths(objects(n.Attributes["paths"]))
			var keys []searchKey
			for _, entry := range paths {
				if s, ok := servedPath(entry); ok {
					keys = append(keys, searchKey{"path", s})
				}
			}
			if pattern := stringAttr(n, "pattern"); pattern != nil {
				keys = append(keys, searchKey{"pattern", *pattern})
			}
			// The unknown-prefix path the item is labelled with, without
			// its leading "…", so any part of the label shown finds the
			// route (JS-4, ADR 0089 Revision 2).
			if routePath(n, scopeProduction) == nil {
				if prefix := routePrefix(n, scopeAll); prefix != nil {
					keys = append(keys, searchKey{"prefix_unknown", strings.TrimPrefix(prefix.Path, "…")})
				}
			}
			keys = append(keys, searchKey{"name", n.Name})
			m, ok := bestMatch(keys, folded)
			if !ok {
				continue
			}
			n := n
			candidates = append(candidates, searchCandidate{match: m, id: n.Identity, build: func(searchMatch) searchItem {
				id := n.Identity
				item := searchItem{ID: &id, Label: n.Name, File: n.File, Line: lineOf(n), IDE: ideOf(n), Method: stringAttr(n, "method")}
				test := n.Attributes["scope"] == "test"
				item.Test = &test
				if shown := routePath(n, scopeProduction); shown != nil {
					item.Path, item.Label = shown, *shown
				} else if prefix := routePrefix(n, scopeAll); prefix != nil {
					item.PrefixUnknown, item.Label = &prefix.Path, prefix.Path
				} else {
					// No path is known: the pattern is marked so it is
					// never read as one (JS-4, ADR 0089 Revisions 4, 5).
					item.Label = routeLabel(n, withheld)
				}
				return item
			}})
		}
		add(searchRoute, candidates)
	}
	if req.wants(searchOperation) {
		doc, err := generateDocument(ctx, h.opt)
		var ae *apiError
		switch {
		case errors.As(err, &ae):
			reason := ae.message
			out.groups[searchOperation].unavailable = &reason
		case err != nil:
			return out, err
		default:
			var candidates []searchCandidate
			keys := make([]string, 0, len(doc.Paths))
			for k := range doc.Paths {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, key := range keys {
				item := doc.Paths[key]
				for _, op := range pathOperations(item) {
					m, ok := bestMatch([]searchKey{{"path", key}}, folded)
					if !ok {
						continue
					}
					key, op := key, op
					candidates = append(candidates, searchCandidate{match: m, id: key + " " + op.method, build: func(searchMatch) searchItem {
						route, method := op.op.X.Route, op.method
						line := op.op.X.Line
						out := searchItem{ID: &route, Label: key, File: op.op.X.File, Method: &method, Path: &key}
						if line > 0 {
							out.Line = &line
						}
						return out
					}})
				}
			}
			add(searchOperation, candidates)
		}
	}
	if req.wants(searchTable) {
		tables, err := rd.s.Nodes(rd.ctx, rd.repo, store.Query{Kind: "table", Contains: req.query})
		if err != nil {
			return out, err
		}
		add(searchTable, nodeCandidates(tables, folded, func(n contract.Node) searchItem {
			id, qn := n.Identity, n.QualifiedName
			return searchItem{ID: &id, Label: n.Name, File: n.File, Line: lineOf(n), IDE: ideOf(n), QualifiedName: &qn, Source: stringAttr(n, "source")}
		}))
	}
	if req.wants(searchSymbol) {
		level := 4
		nodes, err := rd.s.Nodes(rd.ctx, rd.repo, store.Query{Level: &level, Contains: req.query})
		if err != nil {
			return out, err
		}
		var symbols []contract.Node
		for _, n := range nodes {
			if symbolKinds[n.Kind] {
				symbols = append(symbols, n)
			}
		}
		add(searchSymbol, nodeCandidates(symbols, folded, func(n contract.Node) searchItem {
			id, kind, qn := n.Identity, n.Kind, n.QualifiedName
			return searchItem{ID: &id, Label: n.Name, File: n.File, Line: lineOf(n), IDE: ideOf(n), NodeKind: &kind, QualifiedName: &qn}
		}))
	}
	if req.wants(searchFile) {
		var candidates []searchCandidate
		for p := range hashes {
			m, ok := bestMatch([]searchKey{{"base_name", path.Base(p)}, {"path", p}}, folded)
			if !ok {
				continue
			}
			candidates = append(candidates, searchCandidate{match: m, id: p})
		}
		sortCandidates(candidates)
		// The shown files' records and file nodes are read for their
		// language and editor link.
		var shown, ids []string
		for i, c := range candidates {
			if i == req.limit {
				break
			}
			shown = append(shown, c.id)
			if id, err := contract.Identity("file", c.id, c.id); err == nil {
				ids = append(ids, id)
			}
		}
		records, err := rd.s.FilesByPath(rd.ctx, rd.repo, shown)
		if err != nil {
			return out, err
		}
		language := map[string]string{}
		for _, f := range records {
			language[f.Path] = f.Language
		}
		fileNodes, err := rd.byIdentity(ids)
		if err != nil {
			return out, err
		}
		for i := range candidates {
			p := candidates[i].id
			candidates[i].build = func(searchMatch) searchItem {
				item := searchItem{Label: p, File: p}
				if l, ok := language[p]; ok {
					item.Language = &l
				}
				if id, err := contract.Identity("file", p, p); err == nil {
					if n, ok := fileNodes[id]; ok {
						item.ID, item.IDE = &n.Identity, ideOf(n)
					}
				}
				return item
			}
		}
		add(searchFile, candidates)
	}
	return out, nil
}

// pathOperation is one operation of a path item with its method.
type pathOperation struct {
	method string
	op     *openapi.Operation
}

// pathOperations lists a path item's operations in OpenAPI's method order.
func pathOperations(item *openapi.PathItem) []pathOperation {
	var out []pathOperation
	for _, o := range []pathOperation{{"GET", item.Get}, {"PUT", item.Put}, {"POST", item.Post}, {"DELETE", item.Delete}, {"OPTIONS", item.Options}, {"HEAD", item.Head}, {"PATCH", item.Patch}, {"TRACE", item.Trace}} {
		if o.op != nil {
			out = append(out, o)
		}
	}
	return out
}

// nodeCandidates matches nodes by name and qualified name (rows JS-6,
// JS-7).
func nodeCandidates(nodes []contract.Node, folded string, build func(contract.Node) searchItem) []searchCandidate {
	var out []searchCandidate
	for _, n := range nodes {
		m, ok := bestMatch([]searchKey{{"name", n.Name}, {"qualified_name", n.QualifiedName}}, folded)
		if !ok {
			continue
		}
		n := n
		out = append(out, searchCandidate{match: m, id: n.Identity, build: func(searchMatch) searchItem { return build(n) }})
	}
	return out
}

// Merging (rows JS-11, JS-12, JS-15, JS-16).

// mergeGroups builds the answer's groups from the repositories' results,
// in the given repository order.
func mergeGroups(req searchRequest, members []memberSearch) ([]searchGroup, map[string]int) {
	totals := map[string]int{}
	groups := []searchGroup{}
	for _, kind := range req.kinds {
		g := searchGroup{Kind: kind, Available: true, Unavailable: []searchUnavailable{}, Items: []searchItem{}}
		best := -1
		// Row JS-16: tier first, then member order, then the rest of
		// JS-10. Each member's items are already in JS-10 order and the
		// merged order agrees with it within a member, so the first limit
		// items of the merge are among the members' first limit items.
		type memberItem struct {
			member int
			ranked rankedItem
		}
		var merged []memberItem
		for i, m := range members {
			mg := m.groups[kind]
			if mg.unavailable != nil {
				g.Available = false
				g.Unavailable = append(g.Unavailable, searchUnavailable{Repository: m.repository, Reason: *mg.unavailable})
			}
			g.Total += mg.total
			if mg.best >= 0 && (best < 0 || mg.best < best) {
				best = mg.best
			}
			for _, it := range mg.items {
				merged = append(merged, memberItem{member: i, ranked: it})
			}
		}
		sort.SliceStable(merged, func(i, j int) bool {
			a, b := merged[i], merged[j]
			if a.ranked.match.tier != b.ranked.match.tier {
				return a.ranked.match.tier < b.ranked.match.tier
			}
			return a.member < b.member
		})
		for _, it := range merged {
			if len(g.Items) < req.limit {
				g.Items = append(g.Items, it.ranked.item)
			}
		}
		if best >= 0 {
			name := tierNames[best]
			g.BestTier = &name
		}
		g.Truncated = g.Total > len(g.Items)
		totals[kind] = g.Total
		groups = append(groups, g)
	}
	order := map[string]int{}
	for i, k := range searchKinds {
		order[k] = i
	}
	tier := func(g searchGroup) int {
		if g.BestTier == nil {
			return len(tierNames)
		}
		for i, n := range tierNames {
			if n == *g.BestTier {
				return i
			}
		}
		return len(tierNames)
	}
	sort.SliceStable(groups, func(i, j int) bool {
		if ti, tj := tier(groups[i]), tier(groups[j]); ti != tj {
			return ti < tj
		}
		return order[groups[i].Kind] < order[groups[j].Kind]
	})
	return groups, totals
}

// Handlers.

// searchHandler answers GET /api/v0/search for one repository (or one
// project member, dispatched by its repository parameter).
type searchHandler struct{ opt Options }

func (h searchHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	req, err := parseSearch(r.URL.Query())
	if err == nil && h.opt.project == nil {
		if _, present := r.URL.Query()["first"]; present {
			err = badRequest("first is for graphvein serve --project without repository")
		}
	}
	var body any
	if err == nil {
		var m memberSearch
		if m, err = searchRepository(ctx, h.opt, req); err == nil {
			groups, totals := mergeGroups(req, []memberSearch{m})
			body = searchResponse{Repository: h.opt.Repository, Query: req.query, Kinds: req.kinds, Limit: req.limit, IDE: m.ide, Index: m.index, Searched: m.searched, Absence: absenceNotProven, Totals: totals, Groups: groups}
		}
	}
	answer(w, r, h.opt, body, err)
}

// searchProjectHandler answers GET /api/v0/search on a project server:
// with repository= the member's search, else every member's (row JS-15).
type searchProjectHandler struct {
	ps       *projectState
	dispatch http.Handler
}

func (h searchProjectHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if _, present := q["repository"]; present {
		if _, first := q["first"]; first {
			writeError(w, http.StatusBadRequest, "bad_request", "first orders the members of a project-scope search; it cannot be combined with repository")
			return
		}
		h.dispatch.ServeHTTP(w, r)
		return
	}
	ctx := r.Context()
	req, err := parseSearch(q)
	var order []string
	if err == nil {
		order, err = h.ps.memberOrder(q)
	}
	var body any
	if err == nil {
		out := searchProjectResponse{Project: h.ps.project.Name, Query: req.query, Kinds: req.kinds, Limit: req.limit, Absence: absenceNotProven, Members: []searchMemberDoc{}}
		var results []memberSearch
		for _, name := range order {
			mo := h.ps.members[name]
			var m memberSearch
			if m, err = searchRepository(ctx, mo, req); err != nil {
				break
			}
			results = append(results, m)
			totals := map[string]int{}
			for _, k := range req.kinds {
				totals[k] = m.groups[k].total
			}
			out.Members = append(out.Members, searchMemberDoc{Repository: name, Checkout: !mo.noCheckout, IDE: m.ide, Index: m.index, Searched: m.searched, Totals: totals})
		}
		if err == nil {
			out.Groups, out.Totals = mergeGroups(req, results)
			body = out
		}
	}
	answer(w, r, h.ps.opt, body, err)
}

// memberOrder is the project's members with first= first, then manifest
// order (row JS-15).
func (ps *projectState) memberOrder(q url.Values) ([]string, error) {
	first, present, err := param(q, "first")
	if err != nil {
		return nil, err
	}
	if !present {
		return append([]string(nil), ps.names...), nil
	}
	found := false
	order := []string{first}
	for _, n := range ps.names {
		if n == first {
			found = true
			continue
		}
		order = append(order, n)
	}
	if !found {
		return nil, badRequest("first %q is not a member of project %s (members: %s)", first, ps.project.Name, strings.Join(ps.names, ", "))
	}
	return order, nil
}

// answer writes a computed body or the error the API maps it to, the way
// apiHandler does.
func answer(w http.ResponseWriter, r *http.Request, opt Options, body any, err error) {
	ctx := r.Context()
	if ctx.Err() != nil {
		cancelled(w, ctx)
		return
	}
	var ae *apiError
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, body)
	case errors.As(err, &ae):
		writeError(w, ae.status, ae.code, ae.message)
	case errors.Is(err, errIndexChanged):
		writeError(w, http.StatusServiceUnavailable, "index_changed", err.Error())
	default:
		logFailure(opt.logger(), r, err)
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
	}
}
