package server

import (
	"encoding/json"
	"path"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	routepaths "github.com/graphvein/graphvein/internal/extract/routes"
)

// This file holds the pure parts of the API section (ADR 0042): transports,
// scope, source roots and the coverage counts of docs/cli/serve.md. Nothing
// here reads the store, so every count can be tested on hand-built slices.

// transport describes one kind of API boundary. The node kind decides the
// transport; a node's own transport attribute is never consulted, so an
// external indexer cannot move a node between tables.
type transport struct {
	Name         string
	EndpointKind string
	CallKind     string
	CallEdge     string
	ExposeEdge   string
	// Known lists the call statuses this Graphvein version documents for
	// the transport; any other value gets the status_unknown reason.
	Known map[string]bool
}

var (
	httpTransport = transport{
		Name: "http", EndpointKind: "route", CallKind: "client_call", CallEdge: "calls_route", ExposeEdge: "exposes_route",
		Known: setOf("linked", "ambiguous", "unmatched", "dynamic", "test_only", "wrapper", "generated"),
	}
	tauriTransport = transport{
		Name: "tauri_ipc", EndpointKind: "ipc_command", CallKind: "ipc_call", CallEdge: "invokes_command", ExposeEdge: "exposes_command",
		Known: setOf("dynamic", "registry_unproven", "acl_possible", "ambiguous", "shadow_possible", "unknown_command",
			"test_only", "unmatched", "plugin", "global_unlinked", "linked", "wrapper"),
	}
	// actionTransport is the React server action transport of Next.js
	// (T024b, ADR 0077): only proven uses are stored, so every call is
	// linked.
	actionTransport = transport{
		Name: "server_action", EndpointKind: "server_action", CallKind: "action_call", CallEdge: "calls_action", ExposeEdge: "exposes_action",
		Known: setOf("linked"),
	}
	// transports is the fixed response order.
	transports = []transport{httpTransport, tauriTransport, actionTransport}
)

func setOf(values ...string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, v := range values {
		out[v] = true
	}
	return out
}

// transportFor returns the transport whose endpoint kind is kind.
func transportFor(kind string) (transport, bool) {
	for _, t := range transports {
		if t.EndpointKind == kind {
			return t, true
		}
	}
	return transport{}, false
}

// Scope parameter values.
const (
	scopeProduction = "production"
	scopeTest       = "test"
	scopeAll        = "all"
)

// inScope applies the scope parameter: production is a node without a
// scope attribute, test is scope "test", all is every node. Any other scope
// value is unknown, so it is only in all (unknown is not production).
func inScope(n contract.Node, scope string) bool {
	value, present := n.Attributes["scope"]
	switch scope {
	case scopeProduction:
		return !present
	case scopeTest:
		return value == "test"
	default:
		return true
	}
}

// unknownScope reports a scope attribute that is neither absent nor "test".
func unknownScope(n contract.Node) bool {
	value, present := n.Attributes["scope"]
	return present && value != "test"
}

// statusOf is the raw call status; a missing or non-string status is "".
func statusOf(n contract.Node) string {
	s, _ := n.Attributes["status"].(string)
	return s
}

// stringAttr returns a string attribute, or nil when it is absent, null or
// not a string.
func stringAttr(n contract.Node, key string) *string {
	if s, ok := n.Attributes[key].(string); ok {
		return &s
	}
	return nil
}

// boolAttr is true only for a stored true; absent means false.
func boolAttr(attrs map[string]any, key string) bool {
	v, _ := attrs[key].(bool)
	return v
}

// objects returns the object elements of an array attribute, skipping
// anything else, so a malformed external attribute cannot fail a request.
func objects(v any) []map[string]any {
	items, _ := v.([]any)
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// strings of an array attribute; non-strings are skipped.
func stringList(v any) []string {
	items, _ := v.([]any)
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// manifestNames are the file names that make a directory a source root, in
// byte order so the reported manifest is the first one of a directory.
var manifestNames = []string{"Cargo.toml", "deno.json", "deno.jsonc", "go.mod", "package.json", "pyproject.toml", "setup.cfg", "setup.py"}

// sourceRoot finds the source root of a calling site's file: the deepest
// directory holding a stored manifest (a key of stored), else the file's
// first path segment, else "." for a file at the repository root or a
// synthetic node. manifest is the manifest's path, or nil for a fallback.
// Only stored files count, so an ignored manifest never makes a root and
// the result is reproducible from the index alone.
func sourceRoot(file string, stored map[string]bool) (root string, manifest *string) {
	if file == "." || file == "" {
		return ".", nil
	}
	dir := path.Dir(file)
	for {
		for _, name := range manifestNames {
			candidate := name
			if dir != "." {
				candidate = dir + "/" + name
			}
			if stored[candidate] {
				return dir, &candidate
			}
		}
		if dir == "." {
			break
		}
		dir = path.Dir(dir)
	}
	first, _, found := strings.Cut(file, "/")
	if !found {
		return ".", nil
	}
	return first, nil
}

// lessRoot orders source roots: "." first, then byte order.
func lessRoot(a, b string) bool {
	if a == "." || b == "." {
		return a == "." && b != "."
	}
	return a < b
}

// rootFinder caches source roots per file for one request.
type rootFinder struct {
	stored map[string]bool
	cache  map[string]rootResult
}

type rootResult struct {
	root     string
	manifest *string
}

func newRootFinder(hashes map[string]string) *rootFinder {
	stored := make(map[string]bool, len(hashes))
	for p := range hashes {
		stored[p] = true
	}
	return &rootFinder{stored: stored, cache: map[string]rootResult{}}
}

func (f *rootFinder) find(file string) rootResult {
	if r, ok := f.cache[file]; ok {
		return r
	}
	root, manifest := sourceRoot(file, f.stored)
	r := rootResult{root, manifest}
	f.cache[file] = r
	return r
}

// callCounts are the call figures of one transport (docs/cli/serve.md).
type callCounts struct {
	Total             int            `json:"total"`
	ByStatus          map[string]int `json:"by_status"`
	LinkedWithoutEdge int            `json:"linked_without_edge"`
	EdgesFromUnlinked int            `json:"edges_from_unlinked"`
	UnknownScope      int            `json:"unknown_scope"`
	// ProjectByStatus (serve --project, HTTP only; T022 MR-UI) counts the
	// same calls by their project-scope status (projectCallStatus):
	// linked_across and ambiguous_across replace the repository's status
	// of calls the link pass decided across repositories. ByStatus keeps
	// the repository's own statuses.
	ProjectByStatus map[string]int `json:"project_by_status,omitempty"`
}

// rootCounts are the call figures of one source root.
type rootCounts struct {
	Root     string         `json:"root"`
	Manifest *string        `json:"manifest"`
	Total    int            `json:"total"`
	ByStatus map[string]int `json:"by_status"`
	// ProjectByStatus is the root's calls by project-scope status (serve
	// --project, HTTP only).
	ProjectByStatus map[string]int `json:"project_by_status,omitempty"`
}

// addProjectCounts fills ProjectByStatus of the counts and of every root
// from status, with the same scope and root rules as countCoverage.
func addProjectCounts(cov *transportSummary, calls []contract.Node, scope string, root *string, roots *rootFinder, status func(contract.Node) string) {
	cov.Calls.ProjectByStatus = map[string]int{}
	byRoot := map[string]map[string]int{}
	for _, c := range calls {
		if !inScope(c, scope) {
			continue
		}
		r := roots.find(c.File).root
		s := status(c)
		if byRoot[r] == nil {
			byRoot[r] = map[string]int{}
		}
		byRoot[r][s]++
		if root == nil || r == *root {
			cov.Calls.ProjectByStatus[s]++
		}
	}
	for i := range cov.Roots {
		cov.Roots[i].ProjectByStatus = byRoot[cov.Roots[i].Root]
		if cov.Roots[i].ProjectByStatus == nil {
			cov.Roots[i].ProjectByStatus = map[string]int{}
		}
	}
}

// coverage is the result of counting one transport.
type coverage struct {
	Endpoints   []contract.Node
	Calls       callCounts
	Roots       []rootCounts
	NeverCalled int
	// Callers maps an endpoint identity to its number of distinct in-scope
	// (and in-root) callers with a stored call edge to it.
	Callers map[string]int
}

// countCoverage applies the count definitions to one transport. endpoints
// and calls are every stored node of the transport's kinds; edges are the
// stored call edges into the endpoints. root, when non-nil, restricts the
// calls (not the per-root breakdown) to one source root.
func countCoverage(endpoints, calls []contract.Node, edges []contract.Edge, scope string, root *string, roots *rootFinder) coverage {
	out := coverage{Calls: callCounts{ByStatus: map[string]int{}}, Roots: []rootCounts{}, Callers: map[string]int{}, Endpoints: []contract.Node{}}
	for _, e := range endpoints {
		if inScope(e, scope) {
			out.Endpoints = append(out.Endpoints, e)
		}
	}
	endpointIDs := make(map[string]bool, len(endpoints))
	for _, e := range endpoints {
		endpointIDs[e.Identity] = true
	}
	edgesFrom := map[string]int{}
	for _, x := range edges {
		if endpointIDs[x.Target] {
			edgesFrom[x.Source]++
		}
	}
	selected := map[string]bool{}
	byRoot := map[string]*rootCounts{}
	for _, c := range calls {
		if !inScope(c, scope) {
			continue
		}
		r := roots.find(c.File)
		rc := byRoot[r.root]
		if rc == nil {
			rc = &rootCounts{Root: r.root, Manifest: r.manifest, ByStatus: map[string]int{}}
			byRoot[r.root] = rc
		}
		status := statusOf(c)
		rc.Total++
		rc.ByStatus[status]++
		if root != nil && r.root != *root {
			continue
		}
		selected[c.Identity] = true
		out.Calls.Total++
		out.Calls.ByStatus[status]++
		if unknownScope(c) {
			out.Calls.UnknownScope++
		}
		switch {
		case status == "linked" && edgesFrom[c.Identity] == 0:
			out.Calls.LinkedWithoutEdge++
		case status != "linked" && edgesFrom[c.Identity] > 0:
			out.Calls.EdgesFromUnlinked++
		}
	}
	for _, rc := range byRoot {
		out.Roots = append(out.Roots, *rc)
	}
	sort.Slice(out.Roots, func(i, j int) bool { return lessRoot(out.Roots[i].Root, out.Roots[j].Root) })
	seen := map[[2]string]bool{}
	for _, x := range edges {
		key := [2]string{x.Source, x.Target}
		if !selected[x.Source] || !endpointIDs[x.Target] || seen[key] {
			continue
		}
		seen[key] = true
		out.Callers[x.Target]++
	}
	for _, e := range out.Endpoints {
		if out.Callers[e.Identity] == 0 {
			out.NeverCalled++
		}
	}
	return out
}

// displayPath picks the path shown for a route in a scope: the first
// composed path whose entry is in scope, else the first path; nil when the
// route has no composed path.
//
// Only full composed paths count: a Next.js entry whose basePath is not
// read (row NX-R4) holds the template without the base and is shown by
// prefixPath instead (decision U6 of T033), never as a full path.
func displayPath(paths []map[string]any, scope string) *string {
	paths = fullPaths(paths)
	if len(paths) == 0 {
		return nil
	}
	chosen := paths[0]
	for _, p := range paths {
		isTest := p["scope"] == "test"
		if scope == scopeTest && isTest || scope != scopeTest && !isTest {
			chosen = p
			break
		}
	}
	if s, ok := servedPath(chosen); ok {
		return &s
	}
	return nil
}

// routePath is the full path a view shows for route n in scope
// (displayPath), except that a production or all-scope view never shows a
// path composed only through test-scope mounts as the route's path while
// the route has a production path whose prefix the code does not fix
// (T057, row UP-29): the test harness's prefix is not the production
// prefix, so the view shows the unknown-prefix path instead (nil here).
func routePath(n contract.Node, scope string) *string {
	paths := objects(n.Attributes["paths"])
	if scope != scopeTest && !hasProductionFullPath(paths) {
		for _, p := range routepaths.PrefixUnknownPaths(n.Attributes) {
			if p.Scope != scopeTest {
				return nil
			}
		}
	}
	return displayPath(paths, scope)
}

// hasProductionFullPath reports whether some stored path entry is a full
// composed path that is not test scope.
func hasProductionFullPath(paths []map[string]any) bool {
	for _, p := range fullPaths(paths) {
		if p["scope"] != "test" {
			return true
		}
	}
	return false
}

// fullPaths keeps the stored path entries that are full composed paths
// (routepaths.FullPathEntry).
func fullPaths(paths []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(paths))
	for _, p := range paths {
		if routepaths.FullPathEntry(p) {
			out = append(out, p)
		}
	}
	return out
}

// storedPrefixEntries are a route's stored path entries whose prefix the
// code does not fix: its unresolved_prefix_paths entries and its "paths"
// entries that are not full paths (NX-R4).
func storedPrefixEntries(n contract.Node) []map[string]any {
	out := objects(n.Attributes[routepaths.UnknownPrefixAttribute])
	for _, p := range objects(n.Attributes["paths"]) {
		if !routepaths.FullPathEntry(p) {
			out = append(out, p)
		}
	}
	return out
}

// prefixPath is the unknown-prefix path a view shows for a route without a
// full path in scope (T033 rows UP-29, UP-33): its display form ("…" for
// the prefix the code does not fix); nil when the route has a full path or
// no unknown-prefix path.
func prefixPath(n contract.Node, scope string) *string {
	if routePath(n, scope) != nil {
		return nil
	}
	if p := routepaths.ShownPrefixPath(routepaths.PrefixUnknownPaths(n.Attributes), scope); p != nil {
		return &p.Path
	}
	return nil
}

// servedPath is one composed path as served. The stored template is
// normalized without a trailing slash; a FastAPI path records the served
// slash as trailing_slash (ADR 0019), so the path is the one served (T025).
func servedPath(p map[string]any) (string, bool) {
	s, ok := p["template"].(string)
	if !ok {
		return "", false
	}
	if p["trailing_slash"] == true && s != "/" {
		s += "/"
	}
	return s, true
}

// ServedPath is one path a route is served at, with the scope of its
// composition: "test" when the entry that mounts it is test code, else
// "production" (the scope displayPath chooses by).
type ServedPath struct {
	Path  string `json:"path"`
	Scope string `json:"scope"`
}

// ServedPaths is every path a route is served at, in stored order and
// without repeats: each composed path as displayPath writes it, with its
// scope. The endpoints view shows one of them (path) and counts them
// (path_count); the MCP server matches a path filter or a "METHOD /path"
// target against the ones in the request's scope (T030 review), so
// a router mounted twice is found under either mount and a test-only
// mount is never a production path.
func ServedPaths(n contract.Node) []ServedPath {
	out := []ServedPath{}
	seen := map[ServedPath]bool{}
	for _, p := range fullPaths(objects(n.Attributes["paths"])) {
		path, ok := servedPath(p)
		if !ok {
			continue
		}
		sp := ServedPath{Path: path, Scope: scopeProduction}
		if p["scope"] == "test" {
			sp.Scope = scopeTest
		}
		if !seen[sp] {
			seen[sp] = true
			out = append(out, sp)
		}
	}
	return out
}

// routeFlags lists the stored facts the endpoint list marks on a route.
//
// A route without a full path is "prefix_unresolved" when it has a path
// whose prefix the code does not fix (T033 row UP-29: shown with "…"),
// "shadowed" when an earlier mount takes every composed path
// (shadowed_paths), "path_withheld" when it is a registration whose path
// the Go linker withholds (withheld holds its site: couldn't-read row
// CR-44, ADR 0089 Revision 5), else "unmounted" (no composed path stored;
// the views say "path not known": no stored fact proves the route is
// unreachable).
// "path_assumed" marks a route with a full path that rests on an
// assumption (GR-V5, RO-V1, RO-V2).
func routeFlags(n contract.Node, withheld map[routepaths.RouteSite]bool) []string {
	paths := fullPaths(objects(n.Attributes["paths"]))
	prefixed := routepaths.PrefixUnknownPaths(n.Attributes)
	flags := []string{}
	if len(paths) == 0 {
		switch {
		case len(prefixed) > 0:
			flags = append(flags, "prefix_unresolved")
		case len(objects(n.Attributes[routepaths.ShadowedAttribute])) > 0:
			// Every path is withheld because an earlier mount takes it
			// (route_shadowed): the route is
			// mounted.
			flags = append(flags, "shadowed")
		case routepaths.PathWithheld(n, withheld):
			// The route is found, but another pattern or route may answer
			// its path (row CR-44): withheld, not unmounted.
			flags = append(flags, "path_withheld")
		default:
			flags = append(flags, "unmounted")
		}
	}
	for _, p := range paths {
		if len(stringList(p["assumptions"])) > 0 {
			// A path that rests on an assumption (GR-V5 wrappers, RO-V1
			// and RO-V2 route order): the view names it.
			flags = append(flags, "path_assumed")
			break
		}
	}
	conditional, testPaths := false, false
	for _, p := range paths {
		conditional = conditional || boolAttr(p, "conditional")
		testPaths = testPaths || p["scope"] == "test"
	}
	if len(paths) == 0 {
		for _, e := range storedPrefixEntries(n) {
			conditional = conditional || boolAttr(e, "conditional")
			testPaths = testPaths || e["scope"] == "test"
		}
	}
	if conditional {
		flags = append(flags, "conditional")
	}
	if testPaths {
		flags = append(flags, "test_paths")
	}
	inline := boolAttr(n.Attributes, "handler_inline")
	if inline {
		flags = append(flags, "handler_inline")
	}
	if stringAttr(n, "handler") == nil && !inline {
		flags = append(flags, "handler_unresolved")
	}
	sort.Strings(flags)
	return flags
}

// commandFlags lists the stored facts the endpoint list marks on a command.
func commandFlags(n contract.Node) []string {
	flags := []string{}
	for _, key := range []string{"cfg_gated", "test_only", "unreachable"} {
		if boolAttr(n.Attributes, key) {
			flags = append(flags, key)
		}
	}
	switch n.Attributes["registry"] {
	case "unproven":
		flags = append(flags, "unproven_registry")
	case "plugin":
		flags = append(flags, "plugin_registry")
	}
	if stringAttr(n, "command") == nil {
		flags = append(flags, "unresolved_entry")
	}
	sort.Strings(flags)
	return flags
}

// actionFlags lists the stored facts the endpoint list marks on a server
// action: an inline action without a name, an action whose implementing
// function has no node.
func actionFlags(n contract.Node) []string {
	flags := []string{}
	if stringAttr(n, "action") == nil {
		flags = append(flags, "anonymous")
	}
	if stringAttr(n, "handler") == nil {
		flags = append(flags, "handler_unresolved")
	}
	if n.Attributes["action_scope"] == "inline" {
		flags = append(flags, "inline")
	}
	if unreadUses(n) {
		flags = append(flags, "unread_use")
	}
	sort.Strings(flags)
	return flags
}

// unreadUses reports a server action that an unread use (an action
// passed as a prop or argument, a conditional or member read the linker
// does not link) may reach: such an action is never reported as never
// called (T024b R1).
func unreadUses(n contract.Node) bool {
	switch v := n.Attributes["unread_uses"].(type) {
	case float64:
		return v > 0
	case int:
		return v > 0
	case int64:
		return v > 0
	case json.Number:
		f, err := v.Float64()
		return err == nil && f > 0
	}
	return false
}

// methodRank orders HTTP methods as the endpoint list shows them; other
// methods follow in byte order.
func methodRank(method *string) int {
	if method == nil {
		return len(methodOrder) + 1
	}
	for i, m := range methodOrder {
		if *method == m {
			return i
		}
	}
	return len(methodOrder)
}

var methodOrder = []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "ANY"}

// lessNullLast compares optional strings with nil after every value.
func lessNullLast(a, b *string) (less, equal bool) {
	switch {
	case a == nil && b == nil:
		return false, true
	case a == nil:
		return false, false
	case b == nil:
		return true, false
	case *a == *b:
		return false, true
	default:
		return *a < *b, false
	}
}

// frameworksOf lists the distinct framework attributes of nodes, sorted.
func frameworksOf(nodes []contract.Node) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, n := range nodes {
		if f := stringAttr(n, "framework"); f != nil && !seen[*f] {
			seen[*f] = true
			out = append(out, *f)
		}
	}
	sort.Strings(out)
	return out
}

// lessBySite orders call nodes by file, span start, then identity, so lists
// read in source order. Nodes without a span sort before spanned ones of
// the same file.
func lessBySite(a, b contract.Node) bool {
	if a.File != b.File {
		return a.File < b.File
	}
	al, ac, bl, bc := -1, -1, -1, -1
	if a.Span != nil {
		al, ac = a.Span.StartLine, a.Span.StartColumn
	}
	if b.Span != nil {
		bl, bc = b.Span.StartLine, b.Span.StartColumn
	}
	if al != bl {
		return al < bl
	}
	if ac != bc {
		return ac < bc
	}
	return a.Identity < b.Identity
}
