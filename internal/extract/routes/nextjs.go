package routes

import (
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/lang/typescript"
	"github.com/graphvein/graphvein/internal/testscope"
)

// Next.js route handlers (App Router `route.<ext>`) and Pages Router API
// files become HTTP route nodes (T024a, ADR 0077). The rows named in the
// comments are those of docs/extract/routes.md; nothing here guesses a
// fact the table does not list.

// nextVerbs are the HTTP methods a route handler file may export (NX-R2).
var nextVerbs = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"}

// nextConfigNames is the order in which Next.js looks for its config file.
var nextConfigNames = []string{"next.config.js", "next.config.mjs", "next.config.cjs", "next.config.ts", "next.config.mts"}

var defaultPageExtensions = []string{"tsx", "ts", "jsx", "js"}

var (
	nextOptionalCatchAll = regexp.MustCompile(`^\[\[\.\.\.([A-Za-z_$][A-Za-z0-9_$]*)\]\]$`)
	nextCatchAll         = regexp.MustCompile(`^\[\.\.\.([A-Za-z_$][A-Za-z0-9_$]*)\]$`)
	nextDynamic          = regexp.MustCompile(`^\[([A-Za-z_$][A-Za-z0-9_$-]*)\]$`)
	nextIntercepting     = regexp.MustCompile(`^\((\.{1,3}|\.\.\)\(\.\.)\)`)
)

// nextProject is one Next.js application (NX-P1, NX-P2, NX-P3).
type nextProject struct {
	dir, appDir, pagesDir string
	config                facts.NextConfig
	configFile            string
	extensions            []string
}

// NextRewriteRule is one literal rewrite rule as route nodes carry it.
type NextRewriteRule struct {
	Phase       string `json:"phase"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
}

// NextRewrites is the `rewrites` route attribute: the project's literal
// rules (status "literal") or "unread". The client matcher reads it (row
// NX-L3).
type NextRewrites struct {
	Status string            `json:"status"`
	Rules  []NextRewriteRule `json:"rules,omitempty"`
}

// LinkNext builds the Next.js route nodes, their containment and
// exposes_route edges, and the diagnostics that count refused forms.
func LinkNext(files map[string]facts.Facts, modules *typescript.ModuleResolver, sources map[string][]byte, scopeRules ...testscope.Rules) ([]contract.Node, []contract.Edge, []contract.Diagnostic) {
	rules := selectedRules(scopeRules)
	var nodes []contract.Node
	var edges []contract.Edge
	var diagnostics []contract.Diagnostic
	diagnose := func(file, code, message string) {
		diagnostics = append(diagnostics, contract.Diagnostic{Type: "diagnostic", File: file, Severity: "info", Code: code, Message: message})
	}
	projects := nextProjects(files, sources, diagnose)
	if len(projects) == 0 {
		return nil, nil, nil
	}
	repeated := map[string]bool{}
	for _, f := range files {
		for _, group := range f.Repeated {
			for _, identity := range group {
				repeated[identity] = true
			}
		}
	}
	l := nextLinker{files: files, modules: modules, sources: sources, repeated: repeated}
	for _, file := range facts.Paths(files) {
		p := owningProject(projects, file)
		if p == nil || !scriptFile(file) {
			continue
		}
		switch {
		case p.appDir != "" && strings.HasPrefix(file, p.appDir+"/"):
			n, e := l.appRoute(p, file, rules, diagnose)
			nodes, edges = append(nodes, n...), append(edges, e...)
		case p.pagesDir != "" && strings.HasPrefix(file, p.pagesDir+"/api/"):
			n, e := l.pagesRoute(p, file, rules, diagnose)
			nodes, edges = append(nodes, n...), append(edges, e...)
		}
	}
	return nodes, edges, diagnostics
}

// scriptFile reports files the TypeScript extractor reads; route files
// are always among them.
func scriptFile(file string) bool {
	switch path.Ext(file) {
	case ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs", ".mts", ".cts":
		return true
	}
	return false
}

// nextLayout is a package.json declaring `next` whose directory holds an
// app or pages directory (row NX-P1, before the config is read).
type nextLayout struct {
	manifest, dir, appDir, pagesDir string
}

// nextLayouts finds every package.json declaring `next` with an app or
// pages directory (NX-P1, Next.js findPagesDir), in path order.
func nextLayouts(sources map[string][]byte) []nextLayout {
	dirs := map[string]bool{}
	for file := range sources {
		for d := path.Dir(file); ; d = path.Dir(d) {
			dirs[d] = true
			if d == "." || d == "/" {
				break
			}
		}
	}
	join := func(dir, name string) string {
		if dir == "." {
			return name
		}
		return dir + "/" + name
	}
	var paths []string
	for file := range sources {
		if path.Base(file) == "package.json" {
			paths = append(paths, file)
		}
	}
	sort.Strings(paths)
	var out []nextLayout
	for _, file := range paths {
		var manifest struct {
			Dependencies    map[string]any `json:"dependencies"`
			DevDependencies map[string]any `json:"devDependencies"`
		}
		if json.Unmarshal(sources[file], &manifest) != nil {
			continue
		}
		_, dep := manifest.Dependencies["next"]
		_, dev := manifest.DevDependencies["next"]
		if !dep && !dev {
			continue
		}
		dir := path.Dir(file)
		l := nextLayout{manifest: file, dir: dir}
		for _, candidate := range []string{"app", "src/app"} {
			if dirs[join(dir, candidate)] {
				l.appDir = join(dir, candidate)
				break
			}
		}
		for _, candidate := range []string{"pages", "src/pages"} {
			if dirs[join(dir, candidate)] {
				l.pagesDir = join(dir, candidate)
				break
			}
		}
		if l.appDir == "" && l.pagesDir == "" {
			continue
		}
		out = append(out, l)
	}
	return out
}

// NextProjectDirs lists the directories of the repository's Next.js
// applications (row NX-P1's package.json and app or pages directory), the
// origins a same-origin client call can be served from (row NX-L1, T024
// review). A project Next.js refuses (row NX-P1's refusal) is listed
// too: its calls still run against its own origin.
func NextProjectDirs(sources map[string][]byte) []string {
	var out []string
	for _, l := range nextLayouts(sources) {
		out = append(out, l.dir)
	}
	return out
}

// nextProjects finds every package.json declaring `next` (NX-P1) and reads
// its config (NX-P2, NX-P3).
func nextProjects(files map[string]facts.Facts, sources map[string][]byte, diagnose func(file, code, message string)) []*nextProject {
	join := func(dir, name string) string {
		if dir == "." {
			return name
		}
		return dir + "/" + name
	}
	var out []*nextProject
	for _, l := range nextLayouts(sources) {
		file, dir := l.manifest, l.dir
		p := &nextProject{dir: dir, appDir: l.appDir, pagesDir: l.pagesDir}
		if p.appDir != "" && p.pagesDir != "" && path.Dir(p.appDir) != path.Dir(p.pagesDir) {
			NextTag("NX-P1")
			diagnose(file, "nextjs_project_unread", fmt.Sprintf("app directory %s and pages directory %s are under different folders, which Next.js refuses", p.appDir, p.pagesDir))
			continue
		}
		NextTag("NX-P1")
		p.config = facts.NextConfig{Shape: "object", BasePathState: "absent", TrailingSlashState: "absent", PageExtensionsState: "absent", RewritesState: "absent"}
		for _, name := range nextConfigNames {
			config := join(dir, name)
			if _, ok := sources[config]; !ok {
				continue
			}
			p.configFile = config
			f := files[config]
			if f.Next != nil && f.Next.Config != nil {
				p.config = *f.Next.Config
			} else {
				p.config = facts.NextConfig{Shape: "unread", BasePathState: "unread", TrailingSlashState: "unread", PageExtensionsState: "unread", RewritesState: "unread"}
			}
			break
		}
		if p.configFile != "" {
			NextTag("NX-P2")
			var unread []string
			for _, key := range []struct{ name, state string }{{"basePath", p.config.BasePathState}, {"trailingSlash", p.config.TrailingSlashState}, {"pageExtensions", p.config.PageExtensionsState}} {
				if key.state == "unread" {
					unread = append(unread, key.name)
				}
			}
			if len(unread) > 0 {
				NextTag("D")
				diagnose(p.configFile, "nextjs_config_unread", "not a literal value: "+strings.Join(unread, ", "))
			}
			switch p.config.RewritesState {
			case "literal":
				NextTag("NX-P3")
			case "unread":
				NextTag("D")
				diagnose(p.configFile, "nextjs_rewrites_unread", "rewrites are not a literal list of source and destination strings; they are not applied (assumption NX-V1)")
			}
		}
		p.extensions = defaultPageExtensions
		if p.config.PageExtensionsState == "literal" {
			p.extensions = p.config.PageExtensions
		}
		out = append(out, p)
	}
	return out
}

// owningProject returns the deepest project whose directory holds file.
func owningProject(projects []*nextProject, file string) *nextProject {
	var best *nextProject
	for _, p := range projects {
		if p.dir != "." && !strings.HasPrefix(file, p.dir+"/") {
			continue
		}
		if best == nil || len(p.dir) > len(best.dir) || best.dir == "." {
			best = p
		}
	}
	return best
}

// pageName splits a file base name into its name and page extension;
// ok is false when no extension of the project matches.
func (p *nextProject) pageName(base string) (string, bool) {
	extensions := append([]string(nil), p.extensions...)
	sort.SliceStable(extensions, func(i, j int) bool { return len(extensions[i]) > len(extensions[j]) })
	for _, ext := range extensions {
		if strings.HasSuffix(base, "."+ext) && len(base) > len(ext)+1 {
			return strings.TrimSuffix(base, "."+ext), true
		}
	}
	return "", false
}

// nextTemplate turns route segments into a template (NX-R1): app groups
// and slots drop out, brackets become parameters. refusal names the form
// Graphvein does not read; private is true for an app private folder.
func nextTemplate(parts []string, app bool) (template string, optional bool, refusal string, private bool) {
	var out []string
	for i, part := range parts {
		last := i == len(parts)-1
		if app && strings.HasPrefix(part, "_") {
			return "", false, "", true
		}
		if strings.Contains(part, "%") {
			return "", false, "percent_segment", false
		}
		if app && nextIntercepting.MatchString(part) {
			return "", false, "intercepting_segment", false
		}
		if app && strings.HasPrefix(part, "(") && strings.HasSuffix(part, ")") {
			continue
		}
		if app && strings.HasPrefix(part, "@") {
			continue
		}
		if m := nextOptionalCatchAll.FindStringSubmatch(part); m != nil {
			if !last {
				return "", false, "catch_all_not_last", false
			}
			out = append(out, "{*"+m[1]+"}")
			optional = true
			continue
		}
		if m := nextCatchAll.FindStringSubmatch(part); m != nil {
			if !last {
				return "", false, "catch_all_not_last", false
			}
			out = append(out, "{*"+m[1]+"}")
			continue
		}
		if m := nextDynamic.FindStringSubmatch(part); m != nil {
			out = append(out, "{"+m[1]+"}")
			continue
		}
		if strings.ContainsAny(part, "[]") {
			return "", false, "bracket_unrecognised", false
		}
		out = append(out, part)
	}
	return "/" + strings.Join(out, "/"), optional, "", false
}

// NextAppTemplate exposes the App Router template rule to the oracle test.
func NextAppTemplate(dirs []string) (template string, optional bool, refusal string, private bool) {
	return nextTemplate(dirs, true)
}

type nextLinker struct {
	files    map[string]facts.Facts
	modules  *typescript.ModuleResolver
	sources  map[string][]byte
	repeated map[string]bool
}

// appRoute reads one file under the app directory (NX-R1, NX-R2).
func (l *nextLinker) appRoute(p *nextProject, file string, rules testscope.Rules, diagnose func(file, code, message string)) ([]contract.Node, []contract.Edge) {
	name, ok := p.pageName(path.Base(file))
	if !ok || name != "route" {
		return nil, nil
	}
	if p.config.PageExtensionsState == "unread" {
		NextTag("D")
		return nil, nil
	}
	rel := strings.TrimPrefix(file, p.appDir+"/")
	dirs := strings.Split(rel, "/")
	dirs = dirs[:len(dirs)-1]
	template, optional, refusal, private := nextTemplate(dirs, true)
	if private {
		NextTag("NX-R1")
		return nil, nil
	}
	dir := path.Dir(file)
	if refusal == "" {
		for _, ext := range p.extensions {
			if _, page := l.sources[dir+"/page."+ext]; page {
				refusal = "page_and_route"
				break
			}
		}
	}
	if refusal != "" {
		NextTag("NX-R1")
		NextTag("D")
		diagnose(file, "nextjs_route_unread", refusal)
		return nil, nil
	}
	NextTag("NX-R1")
	f := l.files[file]
	type export struct {
		method, handler, expression string
		span                        contract.Span
	}
	var exports []export
	starUnknown := false
	var starSpan contract.Span
	for _, c := range f.ExportClauses {
		if c.Star && !c.TypeOnly {
			if r := l.modules.Resolve(file, c.Module); r.File == "" {
				starUnknown = true
				starSpan = c.Span
			}
		}
	}
	for _, verb := range nextVerbs {
		if e, ok := l.verbExport(file, verb); ok {
			exports = append(exports, export{verb, e.handler, e.expression, e.span})
			continue
		}
		// An export * of a scanned module: the shared resolver expands it.
		if b := l.modules.Export(file, verb); b.File != "" && !b.External && !b.Ambiguous {
			span := contract.Span{}
			for _, c := range f.ExportClauses {
				if c.Star && !c.TypeOnly {
					span = c.Span
					break
				}
			}
			handler := l.function(b.Identity)
			expression := ""
			if handler == "" {
				expression = verb + " (export *)"
			}
			exports = append(exports, export{verb, handler, expression, span})
		}
	}
	if starUnknown {
		NextTag("D")
		diagnose(file, "nextjs_route_exports_unknown", fmt.Sprintf("export * at %d:%d names exports Graphvein cannot list; other verbs may be routes", starSpan.StartLine, starSpan.StartColumn))
	}
	var nodes []contract.Node
	var edges []contract.Edge
	for _, e := range exports {
		NextTag("NX-R2")
		n, es := l.routeNode(p, file, "nextjs", e.method, template, optional, e.handler, e.expression, e.span, rules)
		nodes = append(nodes, n)
		edges = append(edges, es...)
	}
	return nodes, edges
}

type verbBinding struct {
	handler, expression string
	span                contract.Span
}

// verbExport finds the export named verb in the forms of row NX-R2.
func (l *nextLinker) verbExport(file, verb string) (verbBinding, bool) {
	f := l.files[file]
	for _, c := range f.ExportClauses {
		if c.Star || c.TypeOnly || c.Alias != verb {
			continue
		}
		if c.Namespace {
			return verbBinding{expression: "* as " + verb + " from " + c.Module, span: c.Span}, true
		}
		if c.Module != "" {
			handler := l.function(l.modules.ImportBinding(file, c.Module, c.Name, "").Identity)
			if handler == "" {
				return verbBinding{expression: c.Name + " from " + c.Module, span: c.Span}, true
			}
			return verbBinding{handler: handler, span: c.Span}, true
		}
		if handler := l.localFunction(file, c.Name); handler != "" {
			return verbBinding{handler: handler, span: c.Span}, true
		}
		return verbBinding{expression: c.Name, span: c.Span}, true
	}
	if identity := f.Exports[verb]; identity != "" {
		span := contract.Span{}
		for _, n := range f.Nodes {
			if n.Identity == identity && n.Span != nil {
				span = *n.Span
			}
		}
		if handler := l.function(identity); handler != "" {
			return verbBinding{handler: handler, span: span}, true
		}
		// export const GET = h: one level through the bound identifier.
		if f.Routes != nil && len(f.Routes.Scopes) > 0 {
			if b, ok := f.Routes.Scopes[0].Bindings[verb]; ok {
				if b.Value.Kind == "identifier" {
					if handler := l.localFunction(file, b.Value.Name); handler != "" {
						return verbBinding{handler: handler, span: span}, true
					}
				}
				return verbBinding{expression: shortExpression(b.Value.Text, verb), span: span}, true
			}
		}
		return verbBinding{expression: verb, span: span}, true
	}
	if f.Next != nil {
		for _, d := range f.Next.DestructuredExports {
			if d.Name == verb {
				return verbBinding{expression: verb + " (destructured)", span: d.Span}, true
			}
		}
	}
	return verbBinding{}, false
}

// shortExpression keeps a handler expression readable in the API view.
func shortExpression(text, fallback string) string {
	text = whitespace(text)
	if text == "" {
		return fallback
	}
	if len(text) > 120 {
		return text[:117] + "..."
	}
	return text
}

// function returns identity when it names a function or method that is
// not one of several same-named declarations.
func (l *nextLinker) function(identity string) string {
	if identity == "" || l.repeated[identity] {
		return ""
	}
	kind, _, _, err := contract.ParseIdentity(identity)
	if err != nil || kind != "function" && kind != "method" {
		return ""
	}
	return identity
}

// localFunction resolves a module-scope name of file to a function: an
// imported binding through the shared resolver, else a declaration.
func (l *nextLinker) localFunction(file, name string) string {
	f := l.files[file]
	for _, imp := range f.Imports {
		if exported, ok := imp.Bindings[name]; ok {
			return l.function(l.modules.ImportBinding(file, imp.Module, exported, "").Identity)
		}
	}
	for _, n := range f.Nodes {
		if n.QualifiedName == name {
			return l.function(n.Identity)
		}
	}
	return ""
}

// pagesRoute reads one file under <pagesDir>/api (NX-R3).
func (l *nextLinker) pagesRoute(p *nextProject, file string, rules testscope.Rules, diagnose func(file, code, message string)) ([]contract.Node, []contract.Edge) {
	base := path.Base(file)
	name, ok := p.pageName(base)
	if !ok {
		return nil, nil
	}
	if p.config.PageExtensionsState == "unread" || strings.HasSuffix(name, ".d") {
		NextTag("D")
		return nil, nil
	}
	f := l.files[file]
	var span *contract.Span
	handler, expression := "", ""
	for _, c := range f.ExportClauses {
		if !c.Star && !c.TypeOnly && c.Alias == "default" {
			s := c.Span
			span = &s
			if c.Module != "" {
				handler = l.function(l.modules.ImportBinding(file, c.Module, c.Name, "").Identity)
			} else {
				handler = l.localFunction(file, c.Name)
			}
			expression = c.Name
		}
	}
	if span == nil && f.Next != nil && f.Next.DefaultExport != nil {
		span = f.Next.DefaultExport
		handler = l.function(f.Exports["default"])
		if handler == "" && f.Routes != nil && f.Routes.Exports["default"] != "" && !strings.HasPrefix(f.Routes.Exports["default"], "@") {
			handler = l.localFunction(file, f.Routes.Exports["default"])
		}
		expression = "default export"
	}
	if span == nil {
		NextTag("D")
		return nil, nil
	}
	if handler != "" {
		expression = ""
	}
	rel := strings.TrimPrefix(path.Dir(file)+"/"+name, p.pagesDir+"/")
	parts := strings.Split(rel, "/")
	if parts[len(parts)-1] == "index" {
		parts = parts[:len(parts)-1]
	}
	template, optional, refusal, _ := nextTemplate(parts, false)
	if refusal != "" {
		NextTag("D")
		diagnose(file, "nextjs_route_unread", refusal)
		return nil, nil
	}
	NextTag("NX-R3")
	n, edges := l.routeNode(p, file, "nextjs_pages", "ANY", template, optional, handler, expression, *span, rules)
	return []contract.Node{n}, edges
}

// routeNode composes the path (NX-R4) and builds one route node.
func (l *nextLinker) routeNode(p *nextProject, file, framework, method, template string, optional bool, handler, expression string, span contract.Span, rules testscope.Rules) (contract.Node, []contract.Edge) {
	NextTag("NX-R4")
	q := method + " " + template
	identity := facts.ID("route", file, q)
	entry := pathEntry{Template: template, Resolution: "literal", Optional: optional}
	attrs := map[string]any{"framework": framework, "method": method, "pattern": template, "handler": nil, "handler_inline": false, "middleware": []string{}, "project": p.dir}
	switch p.config.BasePathState {
	case "literal":
		entry.Template = joinPath(p.config.BasePath, template)
		attrs["base_path"] = p.config.BasePath
	case "unread":
		entry.Resolution = "unresolved"
		attrs["base_path"] = nil
	default:
		attrs["base_path"] = ""
	}
	if testscope.Classify(file, rules) == testscope.Test {
		attrs["scope"] = "test"
		entry.Scope = testscope.Test
	}
	type nextEntry struct {
		pathEntry
		TrailingSlash bool `json:"trailing_slash,omitempty"`
	}
	attrs["paths"] = []nextEntry{{entry, p.config.TrailingSlashState == "literal" && p.config.TrailingSlash}}
	switch p.config.RewritesState {
	case "literal":
		rewrites := NextRewrites{Status: "literal"}
		for _, r := range p.config.Rewrites {
			rewrites.Rules = append(rewrites.Rules, NextRewriteRule{r.Phase, r.Source, r.Destination})
		}
		attrs["rewrites"] = rewrites
	case "unread":
		attrs["rewrites"] = NextRewrites{Status: "unread"}
	}
	var edges []contract.Edge
	if handler != "" {
		attrs["handler"] = handler
		edges = append(edges, facts.Edge("exposes_route", handler, identity, file, span))
	} else {
		attrs["handler_expression"] = expression
	}
	edges = append(edges, facts.Edge("contains", facts.ID("file", file, file), identity, file, span))
	text := spanText(l.sources[file], span)
	node := contract.Node{Type: "node", Identity: identity, Kind: "route", Level: 4, Name: q, QualifiedName: q, File: file, Span: &span, ContentHash: contract.ContentHash([]byte(q + "\x00" + text)), Attributes: attrs}
	return node, edges
}

// joinPath prefixes basePath with ADR 0013's slash normalization.
func joinPath(base, template string) string {
	parts := strings.FieldsFunc(base+"/"+template, func(r rune) bool { return r == '/' })
	return "/" + strings.Join(parts, "/")
}

// spanText returns the source text a span covers (lines 1-based, columns
// in bytes), or "" when the span is outside the source.
func spanText(source []byte, s contract.Span) string {
	lines := strings.SplitAfter(string(source), "\n")
	if s.StartLine < 1 || s.EndLine < s.StartLine || s.EndLine > len(lines) {
		return ""
	}
	var b strings.Builder
	for i := s.StartLine; i <= s.EndLine; i++ {
		line := lines[i-1]
		from, to := 0, len(line)
		if i == s.StartLine {
			from = min(s.StartColumn, len(line))
		}
		if i == s.EndLine {
			to = min(s.EndColumn, len(line))
		}
		if from < to {
			b.WriteString(line[from:to])
		}
	}
	return b.String()
}
