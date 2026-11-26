package openapi

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/routeshape"
)

// Description is the document's info.description (row OG-4).
const Description = "Routes registered in this repository's source, as Graphvein extracted them " +
	"from its index. Graphvein ran no code and read no running server. A parameter, request body, " +
	"response or security requirement that is absent was not read, which is not the same as none: " +
	"each operation lists what was not read in x-graphvein.not_read. Routes Graphvein could not place " +
	"on a path, forms OpenAPI cannot express and conflicting registrations are listed in the root " +
	"x-graphvein, never guessed."

// SetByYou is the assumption id of a path part the user supplied (OG-92).
const SetByYou = "set_by_you"

// methods are OpenAPI 3.1's eight operation methods.
var methods = map[string]bool{"GET": true, "PUT": true, "POST": true, "DELETE": true, "OPTIONS": true, "HEAD": true, "PATCH": true, "TRACE": true}

// methodOrder is OpenAPI's method order within a path item.
var methodOrder = []string{"GET", "PUT", "POST", "DELETE", "OPTIONS", "HEAD", "PATCH", "TRACE"}

// routeInfo is one route node as the generator reads it.
type routeInfo struct {
	id, method, framework, pattern, file string
	line                                 int
	// hash is the route's stored content hash (its registration).
	hash string
	// spanText is the registration span as diagnostics write it.
	spanText                   string
	handler, handlerExpression *string
	assumptions                []string
	equivalent                 []string
	host                       string
	project                    *string
	attrs                      map[string]any
	shape                      *routeshape.Shape
	shapeError                 *string
	// mounts are the route's unresolved mount hops (OG-94).
	mounts []mountInfo
}

// pathEntry is one stored path entry.
type pathEntry struct {
	template      *string
	resolution    string
	conditional   bool
	trailingSlash bool
	optional      bool
	test          bool
	assumptions   []string
	// above is the known prefix above an unresolved mount hop (OG-95).
	above string
}

// candidate is one (route, path key) pair before the conflict checks.
type candidate struct {
	route       *routeInfo
	key         string
	template    string
	resolution  string
	conditional bool
	assumptions []Assumption
	userKey     string
	optional    bool
	// mount marks a candidate placed by a mount value (OG-95);
	// shadowing is its hop's "not_checked" (OG-96).
	mount     bool
	shadowing string
}

// Generate builds the document of in. It never fails: a route whose facts
// do not fit a row is listed in the root x-graphvein, and a stored shape
// that does not decode is reported on its operation and read as absent.
func Generate(in Input) *Document {
	g := &generator{in: in, values: map[string]*UserValue{}, schemes: map[string]map[string]schemeEntry{}, models: newModelRegistry(),
		unrepresentable: []Unrepresentable{}, conflicts: []Conflict{}, unresolved: []Unresolved{}}
	return g.run()
}

type generator struct {
	in              Input
	doc             *Document
	values          map[string]*UserValue
	documents       map[string][]DocumentedBy
	unknown         map[string]diagnosticPart
	schemes         map[string]map[string]schemeEntry
	models          *modelRegistry
	unrepresentable []Unrepresentable
	conflicts       []Conflict
	unresolved      []Unresolved
	// placed are the candidates place considered, for OG-19.
	placed []candidate
	// twins counts routes per file and content hash.
	twins map[string]int
	// hopKeys counts the mount keys per hop hash (OG-94).
	hopKeys map[string]int
	// refusedMounts are the mount candidates refused against a code-read
	// operation (OG-92), listed by OG-19 on that operation (OG-95).
	refusedMounts []candidate
}

func (g *generator) run() *Document {
	in := g.in
	doc := &Document{
		OpenAPI: Version,
		Info:    Info{Title: in.Repository + " routes extracted by Graphvein", Version: "unversioned", Description: Description},
		Servers: in.Servers,
		Paths:   map[string]*PathItem{},
		X: RootExt{Generator: "graphvein", ContractVersion: contract.Version, Repository: in.Repository, Group: "repository", Scope: "production",
			Unresolved: []Unresolved{}, Unrepresentable: []Unrepresentable{}, Conflicts: []Conflict{}, ImportedSpecs: []ImportedSpec{}, UserValues: []UserValue{}},
	}
	g.doc = doc
	if in.Run != nil {
		doc.X.Index = &IndexInfo{Run: in.Run.ID, FinishedAt: in.Run.FinishedAt, Extractor: in.Run.Header.Indexer.Name + " " + in.Run.Header.Indexer.Version, Commit: in.Run.Header.Commit}
	}
	for _, pv := range in.PathValues {
		v := &UserValue{Key: pv.Key, Value: pv.Value, Registration: pv.Registration, Status: UserValueUnused, Why: WhyNoRoute}
		if strings.HasPrefix(pv.Key, config.PathValueRoutePrefix) && pv.Registration == "" {
			v.Warning = WarnNoRegistration
		}
		if strings.HasPrefix(pv.Key, config.PathValueMountPrefix) && pv.Registration == "" {
			v.Warning = WarnNoMountHash
		}
		g.values[pv.Key] = v
	}
	g.documents = documentedBy(in.Documents, in.SpecOperations)
	g.unknown = unknownParts(in.Diagnostics)

	routes := make([]contract.Node, len(in.Routes))
	copy(routes, in.Routes)
	// Registrations with the same text in one file have one content
	// hash: a value cannot be tied to either (OG-90).
	g.twins = map[string]int{}
	for _, n := range routes {
		g.twins[n.File+"\x00"+n.ContentHash]++
	}
	sort.Slice(routes, func(i, j int) bool { return routes[i].Identity < routes[j].Identity })
	var production []*routeInfo
	for _, n := range routes {
		doc.X.Counts.Routes++
		if s, _ := n.Attributes["scope"].(string); s == "test" {
			doc.X.ExcludedTest.Routes++
			continue
		}
		doc.X.Counts.Production++
		r := readRoute(n)
		if r.shapeError != nil {
			doc.X.ShapeErrors++
		}
		production = append(production, r)
	}
	g.collectMounts(production)
	var cands []candidate
	for _, r := range production {
		cands = append(cands, g.candidates(r)...)
	}
	g.place(cands)
	for _, v := range g.values {
		// A value whose operations were all refused (OG-92) is unused.
		if v.Status == UserValueUsed && v.Operations == 0 {
			v.Status = UserValueUnused
			if v.Why == "" {
				v.Why = WhyNoOperation
			}
		}
	}
	g.shadowing()
	g.finishSecurity()
	if !g.models.empty() || len(g.registeredSchemes()) > 0 {
		doc.Components = &Components{Schemas: g.models.components(), SecuritySchemes: g.registeredSchemes()}
	}
	doc.X.ImportedSpecs = importedSpecs(in.Specs, in.SpecOperations)
	for _, pv := range in.PathValues {
		doc.X.UserValues = append(doc.X.UserValues, *g.values[pv.Key])
	}
	sort.Slice(g.unresolved, func(i, j int) bool { return g.unresolved[i].Route < g.unresolved[j].Route })
	sort.Slice(g.unrepresentable, func(i, j int) bool {
		a, b := g.unrepresentable[i], g.unrepresentable[j]
		if a.Route != b.Route {
			return a.Route < b.Route
		}
		return a.Key < b.Key
	})
	sort.SliceStable(g.conflicts, func(i, j int) bool {
		a, b := g.conflicts[i], g.conflicts[j]
		if a.Reason != b.Reason {
			return a.Reason < b.Reason
		}
		if a.Keys[0] != b.Keys[0] {
			return a.Keys[0] < b.Keys[0]
		}
		return a.Method < b.Method
	})
	doc.X.Unresolved, doc.X.Unrepresentable, doc.X.Conflicts = g.unresolved, g.unrepresentable, g.conflicts
	c := &doc.X.Counts
	c.Unresolved, c.Unrepresentable, c.Conflicts, c.PathKeys = len(g.unresolved), len(g.unrepresentable), len(g.conflicts), len(doc.Paths)
	for _, item := range doc.Paths {
		c.AnyMethod += len(item.X.AnyMethod)
		for _, m := range methodOrder {
			op := *item.slot(m)
			if op == nil {
				continue
			}
			c.Operations++
			if op.X.Conditional {
				c.Conditional++
			}
			if op.X.Resolution == "settings_default" {
				c.SettingsDefault++
			}
			if op.X.Shape != nil {
				c.WithShape++
			}
			if len(op.X.MayBeShadowed) > 0 {
				c.MayBeShadowed++
			}
			for _, a := range op.X.Assumptions {
				if a.ID == SetByYou {
					c.SetByYou++
					break
				}
			}
		}
	}
	return doc
}

// readRoute reads the attributes the generator uses.
func readRoute(n contract.Node) *routeInfo {
	r := &routeInfo{id: n.Identity, file: n.File, attrs: n.Attributes, hash: n.ContentHash}
	if n.Span != nil {
		r.line = n.Span.StartLine
		r.spanText = fmt.Sprintf("%d:%d-%d:%d", n.Span.StartLine, n.Span.StartColumn, n.Span.EndLine, n.Span.EndColumn)
	}
	r.method, _ = n.Attributes["method"].(string)
	r.framework, _ = n.Attributes["framework"].(string)
	r.pattern, _ = n.Attributes["pattern"].(string)
	r.host, _ = n.Attributes["host"].(string)
	if s, ok := n.Attributes["handler"].(string); ok {
		r.handler = &s
	}
	if s, ok := n.Attributes["handler_expression"].(string); ok {
		r.handlerExpression = &s
	}
	if s, ok := n.Attributes["project"].(string); ok {
		r.project = &s
	}
	r.assumptions = stringList(n.Attributes["assumptions"])
	r.equivalent = stringList(n.Attributes["equivalent_registrations"])
	r.mounts = readMounts(n.Attributes[mountAttribute])
	if value, ok := n.Attributes[routeshape.AttributeKey]; ok {
		s, err := routeshape.FromAttribute(value)
		if err != nil {
			msg := err.Error()
			r.shapeError = &msg
		} else {
			r.shape = s
		}
	}
	return r
}

func readEntries(v any) []pathEntry {
	var out []pathEntry
	for _, m := range objects(v) {
		e := pathEntry{}
		if s, ok := m["template"].(string); ok {
			e.template = &s
		}
		e.resolution, _ = m["resolution"].(string)
		e.conditional, _ = m["conditional"].(bool)
		e.trailingSlash, _ = m["trailing_slash"].(bool)
		e.optional, _ = m["optional"].(bool)
		scope, _ := m["scope"].(string)
		e.test = scope == "test"
		e.assumptions = stringList(m["assumptions"])
		e.above, _ = m["above"].(string)
		out = append(out, e)
	}
	return out
}

// routeAssumptions are the route-level assumption ids.
func (r *routeInfo) routeAssumptions() []Assumption {
	out := []Assumption{}
	for _, id := range r.assumptions {
		out = append(out, Assumption{ID: id, From: "route"})
	}
	return out
}

// baseKey is the explorer.path_values key of a Next.js route's basePath.
func (r *routeInfo) baseKey() string {
	project := "."
	if r.project != nil && *r.project != "" {
		project = *r.project
	}
	return config.PathValueNextBasePrefix + project
}

// candidates turns a production route into its path key candidates, or
// lists it as unresolved (rows OG-10, OG-45, OG-90..OG-92).
func (g *generator) candidates(r *routeInfo) []candidate {
	var out []candidate
	var afterBase []string
	usable, test := 0, 0
	for _, e := range readEntries(r.attrs["paths"]) {
		if e.test {
			g.doc.X.ExcludedTest.PathEntries++
			test++
			continue
		}
		if e.template == nil {
			continue
		}
		if e.resolution == "unresolved" {
			afterBase = append(afterBase, withSlash(*e.template, e.trailingSlash))
			continue
		}
		usable++
		as := r.routeAssumptions()
		for _, id := range e.assumptions {
			as = append(as, Assumption{ID: id, From: "path_entry"})
		}
		template := *e.template
		if r.framework == "fastapi" {
			template = starlettePathForm(template)
		}
		out = append(out, candidate{route: r, key: withSlash(template, e.trailingSlash), template: template, resolution: e.resolution,
			conditional: e.conditional, assumptions: as, optional: e.optional})
	}
	if usable == 0 && len(afterBase) == 0 && test > 0 {
		// Every path of the route is reached only from tests (OG-48).
		return nil
	}
	if usable > 0 {
		if v := g.values[r.id]; v != nil {
			v.Why = "the route has a path read from code; a value set by you never replaces it"
		}
		return out
	}
	// No usable path: a value set by the user for the route, then for the
	// Next.js basePath, else the route is unresolved.
	if v := g.values[r.id]; v != nil && r.hash != "" && g.twins[r.file+"\x00"+r.hash] > 1 {
		v.Why = WhyRegistrationTwin
	} else if v != nil && v.Registration != "" && v.Registration != r.hash {
		v.Why = WhyRegistrationChanged
	} else if v != nil {
		v.Status, v.Why = UserValueUsed, ""
		for _, m := range r.mounts {
			// A route key comes before its hops' keys (OG-95).
			if mv := g.values[m.key]; mv != nil && mv.Status == UserValueUnused && mv.Why == WhyNoRoute {
				mv.Why = WhyMountRouteValue
			}
		}
		as := append(r.routeAssumptions(), Assumption{ID: SetByYou, From: "graphvein.yaml explorer.path_values", Key: r.id, Value: v.Value,
			Text: "the whole path of this route is set by you; Graphvein did not read it"})
		return []candidate{{route: r, key: v.Value, template: v.Value, resolution: SetByYou, assumptions: as, userKey: r.id}}
	}
	if mc := g.mountCandidates(r); len(mc) > 0 {
		return mc
	}
	keys := []string{r.id}
	for _, m := range r.mounts {
		keys = append(keys, m.key)
	}
	if len(afterBase) > 0 {
		key := r.baseKey()
		keys = append(keys, key)
		if v := g.values[key]; v != nil {
			v.Status, v.Why = UserValueUsed, ""
			for _, e := range readEntries(r.attrs["paths"]) {
				if e.test || e.template == nil || e.resolution != "unresolved" {
					continue
				}
				template := joinBase(v.Value, *e.template)
				as := append(r.routeAssumptions(), Assumption{ID: SetByYou, From: "graphvein.yaml explorer.path_values", Key: key, Value: v.Value,
					Text: "the Next.js basePath is set by you; Graphvein read it as not literal"})
				for _, id := range e.assumptions {
					as = append(as, Assumption{ID: id, From: "path_entry"})
				}
				out = append(out, candidate{route: r, key: withSlash(template, e.trailingSlash), template: template, resolution: SetByYou,
					conditional: e.conditional, assumptions: as, userKey: key, optional: e.optional})
			}
			return out
		}
	}
	u := Unresolved{Route: r.id, Method: r.method, Pattern: r.pattern, File: r.file, Line: r.line, Keys: keys}
	if len(afterBase) > 0 {
		project := strings.TrimPrefix(r.baseKey(), config.PathValueNextBasePrefix)
		u.Reason, u.TemplatesAfterBase = UnresolvedBase, afterBase
		u.Unknown = Unknown{Part: PartBasePath, Project: project,
			Text: "the basePath of the Next.js project in " + project + " is not a literal (read from the environment or a condition)"}
	} else {
		u.Reason = UnresolvedNoPath
		u.Unknown = Unknown{Part: PartMount, Text: "no chain from an app to this registration is proven: a mount, prefix or router argument on the way is not read as a constant"}
		if d, ok := g.unknown[r.file+"\x00"+r.spanText]; ok {
			u.Unknown.Diagnostic, u.Unknown.Expression, u.Unknown.Text = d.Diagnostic, d.Expression, d.Text
		}
	}
	g.unresolved = append(g.unresolved, u)
	return nil
}

// knownResolutions are the path entry resolutions a key is written for
// (OG-10, OG-92); another value is not one this version reads.
var knownResolutions = map[string]bool{"literal": true, "constant": true, "settings_default": true, SetByYou: true}

// withSlash is the path key of a template: a trailing slash is kept when
// the route was registered with one (ADR 0019).
func withSlash(template string, slash bool) string {
	if slash && template != "/" && !strings.HasSuffix(template, "/") {
		return template + "/"
	}
	return template
}

// joinBase puts a Next.js basePath before a template: the root template
// is the base itself, as Next.js serves it.
func joinBase(base, template string) string {
	if base == "" {
		return template
	}
	if template == "/" {
		return base
	}
	return base + template
}

var paramPattern = regexp.MustCompile(`\{([^{}]*)\}`)

// templateProblem checks a path key (rows OG-11..OG-14): the refusal
// reason, or "" when OpenAPI can express it.
func templateProblem(key string) string {
	depth := 0
	for _, c := range key {
		switch c {
		case '{':
			depth++
		case '}':
			depth--
		}
		if depth < 0 || depth > 1 {
			return ReasonInvalidTemplate
		}
	}
	if depth != 0 {
		return ReasonInvalidTemplate
	}
	seen := map[string]bool{}
	for _, m := range paramPattern.FindAllStringSubmatch(key, -1) {
		name := m[1]
		switch {
		case strings.HasPrefix(name, "*") || strings.HasSuffix(name, "..."):
			return ReasonWildcard
		case name == "" || strings.ContainsAny(name, "/:"):
			return ReasonInvalidTemplate
		case seen[name]:
			return ReasonDuplicateParam
		}
		seen[name] = true
	}
	return ""
}

// starlettePathForm writes a FastAPI route's final catch-all {*name}, which
// the routes extractor stores for Starlette's {name:path} (the FastAPI
// section of docs/extract/routes.md), as the one {name} parameter OG-18
// documents with its "matches" note.
func starlettePathForm(template string) string {
	i := strings.LastIndex(template, "/{*")
	if i < 0 || !strings.HasSuffix(template, "}") || strings.Contains(template[i+1:], "/") {
		return template
	}
	return template[:i] + "/{" + template[i+3:]
}

// paramNames are the {name} parameters of a key in template order.
func paramNames(key string) []string {
	var out []string
	for _, m := range paramPattern.FindAllStringSubmatch(key, -1) {
		out = append(out, m[1])
	}
	return out
}

// equivalentForm replaces every parameter by one placeholder (OG-15).
func equivalentForm(key string) string {
	return paramPattern.ReplaceAllString(key, "{}")
}

// place applies OG-12..OG-20 and OG-15 to the candidates and fills paths.
func (g *generator) place(cands []candidate) {
	cands = g.refuseUserConflicts(cands)
	g.placed = cands
	type opKey struct{ key, method string }
	byOp := map[opKey][]candidate{}
	anyByKey := map[string][]candidate{}
	keys := map[string]bool{}
	for _, c := range cands {
		r := c.route
		if !knownResolutions[c.resolution] {
			g.unrepresentable = append(g.unrepresentable, Unrepresentable{Route: r.id, Method: r.method, Key: c.key, Reason: ReasonResolution})
			continue
		}
		if c.optional {
			g.unrepresentable = append(g.unrepresentable, Unrepresentable{Route: r.id, Method: r.method, Key: c.key, Reason: ReasonOptional})
			continue
		}
		if r.host != "" {
			g.unrepresentable = append(g.unrepresentable, Unrepresentable{Route: r.id, Method: r.method, Key: c.key, Reason: ReasonHost})
			continue
		}
		if p := templateProblem(c.key); p != "" {
			g.unrepresentable = append(g.unrepresentable, Unrepresentable{Route: r.id, Method: r.method, Key: c.key, Reason: p})
			continue
		}
		switch {
		case r.method == "ANY":
			anyByKey[c.key] = append(anyByKey[c.key], c)
		case methods[r.method]:
			byOp[opKey{c.key, r.method}] = append(byOp[opKey{c.key, r.method}], c)
		default:
			g.unrepresentable = append(g.unrepresentable, Unrepresentable{Route: r.id, Method: r.method, Key: c.key, Reason: ReasonMethod})
			continue
		}
		keys[c.key] = true
	}
	// OG-15: keys equal up to parameter names are never both written.
	forms := map[string][]string{}
	for k := range keys {
		forms[equivalentForm(k)] = append(forms[equivalentForm(k)], k)
	}
	refusedKey := map[string]bool{}
	for _, list := range forms {
		if len(list) < 2 {
			continue
		}
		sort.Strings(list)
		var routes []string
		for _, k := range list {
			refusedKey[k] = true
			for ok, cs := range byOp {
				if ok.key == k {
					for _, c := range cs {
						routes = append(routes, c.route.id)
					}
				}
			}
			for _, c := range anyByKey[k] {
				routes = append(routes, c.route.id)
			}
		}
		g.conflicts = append(g.conflicts, Conflict{Reason: ConflictEquivalent, Keys: list, Routes: sortedDistinct(routes)})
	}
	item := func(key string) *PathItem {
		p := g.doc.Paths[key]
		if p == nil {
			p = &PathItem{X: PathItemExt{Entries: []EntryRef{}}}
			g.doc.Paths[key] = p
		}
		return p
	}
	for key, cs := range anyByKey {
		if refusedKey[key] {
			continue
		}
		p := item(key)
		for _, c := range cs {
			p.X.AnyMethod = append(p.X.AnyMethod, RouteRef{Route: c.route.id, File: c.route.file, Line: c.route.line, Framework: c.route.framework})
			p.X.Entries = append(p.X.Entries, EntryRef{Route: c.route.id, Method: "ANY", Resolution: c.resolution, Conditional: c.conditional, Assumptions: c.assumptions})
			g.useValue(c)
		}
	}
	order := make([]opKey, 0, len(byOp))
	for ok := range byOp {
		order = append(order, ok)
	}
	rank := map[string]int{}
	for i, m := range methodOrder {
		rank[m] = i
	}
	sort.Slice(order, func(i, j int) bool {
		if order[i].key != order[j].key {
			return order[i].key < order[j].key
		}
		return rank[order[i].method] < rank[order[j].method]
	})
	for _, ok := range order {
		cs := byOp[ok]
		if refusedKey[ok.key] {
			continue
		}
		chosen, collapsed, conflict := pick(cs)
		if conflict {
			var routes []string
			for _, c := range cs {
				routes = append(routes, c.route.id)
			}
			g.conflicts = append(g.conflicts, Conflict{Reason: ConflictDuplicate, Keys: []string{ok.key}, Method: ok.method, Routes: sortedDistinct(routes)})
			continue
		}
		p := item(ok.key)
		for _, c := range cs {
			p.X.Entries = append(p.X.Entries, EntryRef{Route: c.route.id, Method: ok.method, Resolution: c.resolution, Conditional: c.conditional, Assumptions: c.assumptions})
		}
		*p.slot(ok.method) = g.operation(chosen, ok.key, collapsed)
		g.useValue(chosen)
	}
	for _, p := range g.doc.Paths {
		sort.Slice(p.X.Entries, func(i, j int) bool {
			if p.X.Entries[i].Route != p.X.Entries[j].Route {
				return p.X.Entries[i].Route < p.X.Entries[j].Route
			}
			return p.X.Entries[i].Method < p.X.Entries[j].Method
		})
		sort.Slice(p.X.AnyMethod, func(i, j int) bool { return p.X.AnyMethod[i].Route < p.X.AnyMethod[j].Route })
	}
}

// refuseUserConflicts drops each candidate from a value set by the user
// whose key is the key of a code-read operation with the same method, or
// equal to a code-read key up to parameter names (OG-92): a value set by
// you never removes or displaces an operation read from code. The value
// is reported unused with the reason when none of its candidates remain.
func (g *generator) refuseUserConflicts(cands []candidate) []candidate {
	codeForms := map[string]string{}
	codeOps := map[string]bool{}
	for _, c := range cands {
		if c.userKey == "" {
			codeForms[equivalentForm(c.key)] = c.key
			codeOps[c.key+" "+c.route.method] = true
		}
	}
	out := cands[:0:0]
	for _, c := range cands {
		if c.userKey != "" {
			code, clash := codeForms[equivalentForm(c.key)]
			switch {
			case clash && code != c.key:
				g.values[c.userKey].Why = "its path " + c.key + " equals the path " + code + " read from code up to parameter names; the operation read from code is kept"
				g.keepRefused(c)
				continue
			case codeOps[c.key+" "+c.route.method] || clash && c.route.method == "ANY":
				g.values[c.userKey].Why = "its path " + c.key + " is already an operation read from code; the operation read from code is kept"
				g.keepRefused(c)
				continue
			}
		}
		out = append(out, c)
	}
	return out
}

// keepRefused keeps a mount candidate refused against a code-read
// operation for OG-19: the route it places is still registered, so with
// the value you stated it may answer the code-read operation's requests
// first (OG-95).
func (g *generator) keepRefused(c candidate) {
	if c.mount {
		g.refusedMounts = append(g.refusedMounts, c)
	}
}

// useValue counts an operation or ANY entry that uses a user value.
func (g *generator) useValue(c candidate) {
	if c.userKey != "" {
		g.values[c.userKey].Operations++
	}
}

// pick chooses the one route of an operation (OG-20): the only one, or the
// first registration of one Express equivalent_registrations group (as the
// matcher does). Several other routes are a conflict.
func pick(cs []candidate) (candidate, []string, bool) {
	ids := map[string]bool{}
	for _, c := range cs {
		ids[c.route.id] = true
	}
	if len(ids) == 1 {
		return cs[0], nil, false
	}
	group := cs[0].route.equivalent
	if len(group) == 0 {
		return candidate{}, nil, true
	}
	for _, c := range cs {
		if strings.Join(c.route.equivalent, "\x00") != strings.Join(group, "\x00") {
			return candidate{}, nil, true
		}
	}
	for _, id := range group {
		if !ids[id] {
			continue
		}
		for _, c := range cs {
			if c.route.id == id {
				return c, group, false
			}
		}
	}
	return candidate{}, nil, true
}

// ruleOf names the extraction rules a framework's routes come from (OG-40).
func ruleOf(framework string) string {
	switch framework {
	case "express":
		return "Express routes (ADR 0013, docs/extract/routes.md)"
	case "fastapi":
		return "FastAPI routes (ADR 0015, ADR 0019)"
	case "axum":
		return "Axum routes (ADR 0016)"
	case "net/http", "chi", "gin":
		return "Go routes, GR rows (ADR 0076)"
	case "nextjs", "nextjs_pages":
		return "Next.js routes, NX rows (ADR 0077)"
	}
	return framework + " routes (no Graphvein rule: external indexer)"
}

// authKeys are the stored auth attributes copied as written (OG-42).
var authKeys = []string{"security_dependencies", "security_dependencies_unknown", "middleware", "mounted_middleware", "mounted_middleware_unknown", "middleware_declares", "layers", "layers_unknown"}

// operation builds one operation (rows OG-20..OG-32, OG-40..OG-44,
// OG-70..OG-89).
func (g *generator) operation(c candidate, key string, collapsed []string) *Operation {
	r := c.route
	op := &Operation{route: r, shape: r.shape, X: OperationExt{
		Route: r.id, File: r.file, Line: r.line, Framework: r.framework, Handler: r.handler, HandlerExpression: r.handlerExpression,
		Rule: ruleOf(r.framework), Confidence: 1, Template: c.template, Resolution: c.resolution, Conditional: c.conditional,
		Assumptions: c.assumptions, EquivalentRegistrations: collapsed, DocumentedBy: g.documents[r.id], Auth: map[string]any{},
		ShapeError: r.shapeError, Shadowing: c.shadowing,
	}}
	if op.X.DocumentedBy == nil {
		op.X.DocumentedBy = []DocumentedBy{}
	}
	for _, k := range authKeys {
		if v, ok := r.attrs[k]; ok {
			op.X.Auth[k] = v
		}
	}
	// Path parameters (OG-26, OG-18, OG-72, OG-73).
	convertors := patternConvertors(r)
	for _, name := range paramNames(key) {
		required := true
		p := Parameter{Name: name, In: "path", Required: &required, Schema: map[string]any{}, X: map[string]any{"type": "not_read"}}
		if r.framework == "fastapi" && (c.resolution != SetByYou || c.mount) {
			conv, own := convertors[name]
			switch {
			case conv == "path":
				p.X["matches"] = MatchesPath
			case !own:
				p.X["matches"] = MatchesNotRead
			}
		}
		op.Parameters = append(op.Parameters, p)
	}
	g.applyShape(op)
	if op.X.Shape == nil || op.X.Shape.Sections[routeshape.SectionSecurity].State == string(routeshape.NotRead) {
		g.attributeSecurity(op)
	}
	return op
}

// What OG-18 says about a FastAPI path parameter's match.
const (
	// MatchesPath marks a {name:path} parameter of the route's own pattern.
	MatchesPath = "any text, including an empty value and /: Starlette's path convertor ({name:path}); an OpenAPI path parameter matches one segment"
	// MatchesNotRead marks a parameter from a router prefix: prefixes are
	// stored composed, without their convertors.
	MatchesNotRead = "not read: the parameter comes from a router prefix, whose convertor Graphvein does not store (a path convertor there would match any text, / included)"
)

// patternConvertors maps each {name} or {name:convertor} parameter of the
// route's own pattern (without prefixes) to its convertor ("" for none).
func patternConvertors(r *routeInfo) map[string]string {
	out := map[string]string{}
	for _, m := range paramPattern.FindAllStringSubmatch(r.pattern, -1) {
		name, conv, _ := strings.Cut(m[1], ":")
		out[strings.TrimSpace(name)] = strings.TrimSpace(conv)
	}
	return out
}

// notRead is OG-41's list, narrowed by the shape's read sections and a
// written security requirement.
func notRead(op *Operation) []string {
	read := func(section string) bool {
		if op.X.Shape == nil {
			return false
		}
		return op.X.Shape.Sections[section].State != string(routeshape.NotRead)
	}
	sections := map[string]string{"query": routeshape.SectionQuery, "header": routeshape.SectionHeader, "cookie": routeshape.SectionCookie,
		"requestBody": routeshape.SectionBody, "responses": routeshape.SectionResponses, "path_parameter_types": routeshape.SectionPath}
	var out []string
	for _, field := range notReadFields {
		switch {
		case field == "security":
			if len(op.Security) > 0 || read(routeshape.SectionSecurity) {
				continue
			}
		case sections[field] != "" && read(sections[field]):
			continue
		}
		out = append(out, field)
	}
	return out
}

// documentedBy groups the documents_route edges by route (OG-44).
func documentedBy(edges []contract.Edge, ops []contract.Node) map[string][]DocumentedBy {
	files := map[string]string{}
	for _, n := range ops {
		files[n.Identity] = n.File
	}
	out := map[string][]DocumentedBy{}
	for _, e := range edges {
		if e.Kind != "documents_route" {
			continue
		}
		out[e.Target] = append(out[e.Target], DocumentedBy{Operation: e.Source, File: files[e.Source], Confidence: e.Confidence})
	}
	for _, list := range out {
		sort.Slice(list, func(i, j int) bool { return list[i].Operation < list[j].Operation })
	}
	return out
}

// diagnosticPart is a diagnostic that names a route's unknown part.
type diagnosticPart struct{ Diagnostic, Expression, Text string }

// unknownTexts say what each diagnostic means for the route it names.
var unknownTexts = map[string]string{
	"route_unmounted":               "the router or app this route is registered on is not mounted on any app Graphvein can prove",
	"route_mount_unresolved":        "the router argument of a mount on the way to this route is not read",
	"route_prefix_unresolved":       "a prefix on the way to this route is not read as a constant",
	"route_trailing_slash_unproven": "a trailing slash on the way to this route is answered differently by axum versions and the crate's axum version is not proven within the checked ones",
}

// unknownParts indexes the route diagnostics by the registration span they
// name: their messages end with " at L:C-L:C" (the registration call's
// span), which is how a diagnostic names a route.
func unknownParts(diags []contract.Diagnostic) map[string]diagnosticPart {
	out := map[string]diagnosticPart{}
	for _, d := range diags {
		i := strings.LastIndex(d.Message, " at ")
		if i < 0 || unknownTexts[d.Code] == "" {
			continue
		}
		key := d.File + "\x00" + d.Message[i+4:]
		if _, dup := out[key]; !dup {
			out[key] = diagnosticPart{Diagnostic: d.Code, Expression: d.Message[:i], Text: unknownTexts[d.Code]}
		}
	}
	return out
}

// importedSpecs lists the repository's imported specs (OG-60).
func importedSpecs(specs, ops []contract.Node) []ImportedSpec {
	out := []ImportedSpec{}
	for _, n := range specs {
		s := ImportedSpec{Spec: n.Identity, File: n.File, Scope: "production", Statuses: map[string]int{}}
		if v, ok := n.Attributes["title"].(string); ok {
			s.Title = &v
		}
		if v, ok := n.Attributes["spec_version"].(string); ok {
			s.SpecVersion = &v
		}
		if v, _ := n.Attributes["scope"].(string); v == "test" {
			s.Scope = "test"
		}
		for _, o := range ops {
			if o.File != n.File {
				continue
			}
			s.Operations++
			status, _ := o.Attributes["status"].(string)
			if status == "" {
				status = "unknown"
			}
			s.Statuses[status]++
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Spec < out[j].Spec })
	return out
}

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

func stringList(v any) []string {
	items, _ := v.([]any)
	var out []string
	for _, item := range items {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func sortedDistinct(list []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range list {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// canonical is the JSON text of v, used to compare definitions.
func canonical(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(data)
}

// firstMatch are the frameworks that dispatch a request to the first
// registered route that matches it (OG-19): Express walks its router stack
// in registration order, Starlette its route list in inclusion order. The
// other frameworks (gin, chi, net/http, Axum, Next.js) route by
// specificity, so a literal segment wins over a parameter whatever the
// order; Graphvein lists no candidates for them.
var firstMatch = map[string]bool{"express": true, "fastapi": true}

// shadowing lists, on each operation of a first-match framework, the
// other routes of that framework whose path matches every request of the
// operation's path with the same method (or ANY): when one of them is
// registered earlier on the same router or app, it answers those requests
// instead (OG-19). Registration order across routers and files is not
// stored, so every such route is listed as "may be shadowed", order not
// checked.
func (g *generator) shadowing() {
	g.eachOperation(func(key, method string, op *Operation) {
		r := op.route
		if !firstMatch[r.framework] {
			return
		}
		for _, c := range g.placed {
			o := c.route
			if o.id == r.id || o.framework != r.framework || (o.method != method && o.method != "ANY") {
				continue
			}
			if equivalentForm(c.key) == equivalentForm(key) || !covers(c.key, patternConvertors(o), key, r.framework) {
				continue
			}
			op.X.MayBeShadowed = append(op.X.MayBeShadowed, Shadow{Route: o.id, Method: o.method, Key: c.key, Order: "not_checked"})
		}
		// A mount candidate refused against this operation (OG-92) is
		// equal to it up to parameter names: it matches every request.
		for _, c := range g.refusedMounts {
			o := c.route
			if o.id == r.id || o.framework != r.framework || (o.method != method && o.method != "ANY") {
				continue
			}
			if equivalentForm(c.key) != equivalentForm(key) && !covers(c.key, patternConvertors(o), key, r.framework) {
				continue
			}
			op.X.MayBeShadowed = append(op.X.MayBeShadowed, Shadow{Route: o.id, Method: o.method, Key: c.key, Order: "not_checked", SetByYou: c.userKey})
		}
		sort.Slice(op.X.MayBeShadowed, func(i, j int) bool {
			a, b := op.X.MayBeShadowed[i], op.X.MayBeShadowed[j]
			if a.Route != b.Route {
				return a.Route < b.Route
			}
			return a.Key < b.Key
		})
	})
}

// covers reports whether every request path of key also matches the path
// general (a "may" claim: a request the general route takes), segment by
// segment: a literal equals a literal (Express compares without case: its
// routing is case-insensitive unless the app sets caseSensitive, which is
// not read), a {name} matches any one non-empty segment, and a final
// multi-segment parameter matches the rest: Starlette's {name:path}
// (regular expression .*) and Express 4's unnamed * (.*) also an empty
// rest, Express 5's {*name} and {name...} one segment or more. Express
// ignores a trailing slash by default; FastAPI compares it.
func covers(general string, convertors map[string]string, key, framework string) bool {
	express := framework == "express"
	if express {
		general, key = strings.TrimSuffix(general, "/"), strings.TrimSuffix(key, "/")
	}
	gs, ks := strings.Split(general, "/"), strings.Split(key, "/")
	for i, seg := range gs {
		name, isParam := strings.CutPrefix(seg, "{")
		name = strings.TrimSuffix(name, "}")
		if isParam && i == len(gs)-1 {
			switch {
			case convertors[strings.TrimPrefix(name, "*")] == "path":
				return len(ks) > i
			case express && name == "*":
				return len(ks) >= i
			case strings.HasPrefix(name, "*") || strings.HasSuffix(name, "..."):
				return len(ks) > i && ks[i] != ""
			}
		}
		if i >= len(ks) {
			return false
		}
		switch {
		case isParam && strings.HasSuffix(seg, "}"):
			if ks[i] == "" {
				return false
			}
		case express && !strings.EqualFold(seg, ks[i]):
			return false
		case !express && seg != ks[i]:
			return false
		}
	}
	return len(gs) == len(ks)
}
