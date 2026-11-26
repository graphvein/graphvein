package clients

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/lang/typescript"
	"github.com/graphvein/graphvein/internal/testscope"
)

type orvalOperation struct {
	node                    contract.Node
	name, file, method, url string
	optionsIndex            int
	query, hook             bool
}

type orvalProject struct {
	config, name, directory, mutator string
	operations                       []orvalOperation
}

var orvalMajor = regexp.MustCompile(`^=?\s*[~^]?v?8(?:\.(?:[0-9]+|[xX*])){0,2}(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)
var orvalOperationID = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9 _-]*$`)
var orvalWords = regexp.MustCompile(`[A-Za-z0-9]+`)
var orvalParameter = regexp.MustCompile(`\{[^}]+\}`)

func orvalPascal(s string) string {
	if !strings.ContainsAny(s, "abcdefghijklmnopqrstuvwxyz") {
		s = strings.ToLower(s)
	}
	words := orvalWords.FindAllString(s, -1)
	for i, word := range words {
		words[i] = strings.ToUpper(word[:1]) + word[1:]
	}
	return strings.Join(words, "")
}
func orvalCamel(s string) string {
	underscores := len(s) - len(strings.TrimLeft(s, "_"))
	prefix := s[:underscores]
	s = s[underscores:]
	s = orvalPascal(s)
	if s == "" {
		return prefix
	}
	return prefix + strings.ToLower(s[:1]) + s[1:]
}
func orvalKebab(s string) string {
	words := orvalWords.FindAllString(s, -1)
	if len(words) == 1 && words[0] == s {
		var b strings.Builder
		for i := 0; i < len(s); i++ {
			if i > 0 && s[i-1] >= 'a' && s[i-1] <= 'z' && s[i] >= 'A' && s[i] <= 'Z' {
				b.WriteByte('-')
			}
			b.WriteByte(s[i])
		}
		return strings.ToLower(b.String())
	}
	return strings.ToLower(strings.Join(words, "-"))
}
func orvalReserved(s string) bool {
	return strings.Contains(" await break case catch class const continue debugger default delete do else enum export extends false finally for function if implements import in instanceof interface let new null package private protected public return static super switch this throw true try typeof var void while with yield ", " "+s+" ")
}
func ascii(s string) bool {
	for _, c := range s {
		if c > 127 {
			return false
		}
	}
	return true
}
func orvalKeys(e expr, allowed, prefix string) string {
	if e.Kind != "object" || e.Invalid {
		return prefix + " must be an object with own literal keys"
	}
	for _, entry := range e.Keywords {
		if entry.Name == "" {
			return prefix + " has a spread or opaque key"
		}
	}
	for _, key := range e.Keys {
		if !strings.Contains(" "+allowed+" ", " "+key+" ") {
			return prefix + "." + key
		}
	}
	return ""
}
func containedPath(dir, target string) (string, bool) {
	if path.IsAbs(target) || strings.ContainsAny(target, "\\:") {
		return "", false
	}
	p := path.Clean(path.Join(dir, target))
	return p, p != ".." && !strings.HasPrefix(p, "../")
}
func orvalDiagnostic(file, project, code, reason string) contract.Diagnostic {
	return contract.Diagnostic{Type: "diagnostic", File: file, Severity: "info", Code: code, Message: fmt.Sprintf("%s: Orval project %s: %s", file, project, reason)}
}

func (r *resolver) orvalFlag(file string, e expr, key string, fallback bool) (bool, bool) {
	v, exists := property(e, key)
	if !exists {
		return fallback, true
	}
	ev := newEvaluation(r)
	values := ev.expression(evaluationContext{file: file}, v)
	if len(values) != 1 || values[0].kind != "boolean" {
		return false, false
	}
	return values[0].text == "true", true
}

func (r *resolver) orvalModel(file, project string, config expr, sources map[string][]byte, specs []contract.Node) (orvalProject, string, []contract.Diagnostic) {
	p := orvalProject{config: file, name: project}
	var diagnostics []contract.Diagnostic
	// Source authority is independent of whether the remaining configuration
	// belongs to the modeled subset.
	if output, exists := property(config, "output"); exists {
		workspace := ""
		workspaceKnown := true
		if v, exists := property(output, "workspace"); exists {
			workspace, workspaceKnown = r.evaluatedString(file, 0, v)
		}
		target, _ := property(output, "target")
		out, targetKnown := r.evaluatedString(file, 0, target)
		work, workOK := containedPath(path.Dir(file), workspace)
		dir, dirOK := containedPath(work, out)
		if workspaceKnown && targetKnown && workOK && dirOK && out != "" && path.Ext(out) == "" {
			for f := range sources {
				if under(f, dir) {
					return p, "", nil
				}
			}
		}
	}
	if reason := orvalKeys(config, "input output", "project"); reason != "" {
		return p, reason, nil
	}
	var pkg struct{ Dependencies, DevDependencies map[string]string }
	if json.Unmarshal(sources[path.Join(path.Dir(file), "package.json")], &pkg) != nil {
		return p, "package.json unavailable or invalid", nil
	}
	version := pkg.Dependencies["orval"]
	if version == "" {
		version = pkg.DevDependencies["orval"]
	}
	if !orvalMajor.MatchString(strings.TrimSpace(version)) {
		return p, "package.json orval range is not proven major 8", nil
	}
	input, _ := property(config, "input")
	if input.Kind == "object" {
		if reason := orvalKeys(input, "target", "input"); reason != "" {
			return p, reason, nil
		}
		input, _ = property(input, "target")
	}
	in, ok := r.evaluatedString(file, 0, input)
	if !ok {
		return p, "input.target is not a string", nil
	}
	specFile, ok := containedPath(path.Dir(file), in)
	if !ok {
		return p, "input.target is outside the repository", nil
	}
	supported := false
	for _, n := range specs {
		if n.Kind == "api_spec" && n.File == specFile && n.Attributes["version_supported"] == true {
			supported = true
		}
	}
	if !supported {
		return p, "input.target has no scanned version_supported api_spec", nil
	}
	output, _ := property(config, "output")
	if reason := orvalKeys(output, "target mode client httpClient workspace baseUrl clean prettier biome indexFiles docs schemas mock override", "output"); reason != "" {
		return p, reason, nil
	}
	for _, field := range [][2]string{{"client", "react-query"}, {"mode", "tags-split"}, {"httpClient", "fetch"}} {
		v, exists := property(output, field[0])
		if !exists && field[0] == "httpClient" {
			continue
		}
		s, ok := r.evaluatedString(file, 0, v)
		if !ok || s != field[1] {
			return p, "output." + field[0], nil
		}
	}
	workspace := ""
	if v, exists := property(output, "workspace"); exists {
		var ok bool
		workspace, ok = r.evaluatedString(file, 0, v)
		if !ok {
			return p, "output.workspace", nil
		}
	}
	work, ok := containedPath(path.Dir(file), workspace)
	if !ok {
		return p, "output.workspace is outside the repository", nil
	}
	target, _ := property(output, "target")
	out, ok := r.evaluatedString(file, 0, target)
	if !ok || out == "" || path.Ext(out) != "" {
		return p, "output.target must be a relative extensionless directory", nil
	}
	p.directory, ok = containedPath(work, out)
	if !ok {
		return p, "output.target is outside the repository", nil
	}
	// Scanned source takes precedence over predictions, even if other settings
	// are outside the model. An empty reason with no operations is silent.
	for f := range sources {
		if under(f, p.directory) {
			p.directory = ""
			return p, "", nil
		}
	}
	base := ""
	if v, exists := property(output, "baseUrl"); exists {
		var ok bool
		base, ok = r.evaluatedString(file, 0, v)
		if !ok {
			return p, "output.baseUrl", nil
		}
	}
	override, exists := property(output, "override")
	if exists {
		if reason := orvalKeys(override, "mutator mock fetch query zod header components jsDoc", "output.override"); reason != "" {
			return p, reason, nil
		}
	}
	query, hasQuery := property(override, "query")
	if hasQuery {
		if reason := orvalKeys(query, "useQuery useMutation useInfinite useSuspenseQuery useSuspenseInfiniteQuery signal shouldExportQueryKey shouldExportHttpClient shouldExportMutatorHooks usePrefetch useInvalidate", "output.override.query"); reason != "" {
			return p, reason, nil
		}
	}
	if fetch, exists := property(override, "fetch"); exists {
		if reason := orvalKeys(fetch, "includeHttpResponseReturnType forceSuccessResponse runtimeValidation", "output.override.fetch"); reason != "" {
			return p, reason, nil
		}
	}
	useQuery, ok := r.orvalFlag(file, query, "useQuery", true)
	if !ok {
		return p, "output.override.query.useQuery", nil
	}
	useMutation, ok := r.orvalFlag(file, query, "useMutation", true)
	if !ok {
		return p, "output.override.query.useMutation", nil
	}
	regularQuery := useQuery
	for _, flag := range []string{"useInfinite", "useSuspenseQuery", "useSuspenseInfiniteQuery"} {
		v, ok := r.orvalFlag(file, query, flag, false)
		if !ok {
			return p, "output.override.query." + flag, nil
		}
		if flag == "useSuspenseQuery" {
			regularQuery = regularQuery || v
		}
	}
	if mutator, exists := property(override, "mutator"); exists {
		if reason := orvalKeys(mutator, "path name default", "output.override.mutator"); reason != "" {
			return p, reason, nil
		}
		if v, exists := property(mutator, "default"); exists && v.Kind != "false" {
			return p, "output.override.mutator.default", nil
		}
		mp, _ := property(mutator, "path")
		mn, _ := property(mutator, "name")
		if mp.Kind != "literal" || mn.Kind != "literal" || mn.Name == "" {
			return p, "mutator.path and name must be literal strings", nil
		}
		mf, ok := containedPath(work, mp.Name)
		if !ok {
			return p, "mutator.path is outside the repository", nil
		}
		// Resolve relative to the workspace, retaining TypeScript extension probing.
		resolved := r.modules.Resolve(path.Join(work, "__orval__.ts"), "./"+mp.Name)
		if resolved.File == "" {
			return p, "mutator.path has no scanned file", nil
		}
		mf = resolved.File
		b := r.modules.Export(mf, mn.Name)
		if b.Identity == "" || b.External || b.Ambiguous || b.Namespace {
			return p, "mutator export is not uniquely emitted", nil
		}
		kind, _, _, _ := contract.ParseIdentity(b.Identity)
		if kind != "function" {
			return p, "mutator export is not a function", nil
		}
		binding, _, found, _ := newEvaluation(r).binding(evaluationContext{file: b.File}, b.Name)
		if !found || binding.Evaluation == nil || binding.Evaluation.Helper == nil || binding.Evaluation.Mutated || binding.Evaluation.Helper.BoundConst && !binding.Evaluation.Constant {
			return p, "mutator is not a function declaration or const function", nil
		}
		if strings.HasPrefix(mn.Name, "use") && len(r.files[b.File].Routes.Parameters[b.Identity]) == 0 {
			return p, "hook mutator", nil
		}
		p.mutator = b.Identity
	}
	names := map[string]int{}
	for _, n := range specs {
		if n.Kind == "api_operation" && n.File == specFile {
			id, _ := n.Attributes["operation_id"].(string)
			names[orvalCamel(id)]++
		}
	}
	for _, n := range specs {
		if n.Kind != "api_operation" || n.File != specFile {
			continue
		}
		id, _ := n.Attributes["operation_id"].(string)
		name := orvalCamel(id)
		tags := orvalStrings(n.Attributes["tags"])
		tag := "default"
		if len(tags) > 0 {
			tag = tags[0]
		}
		reason := ""
		switch {
		case !orvalOperationID.MatchString(id):
			reason = "unsupported or absent operationId"
		case orvalReserved(name):
			reason = "reserved operation name"
		case names[name] != 1:
			reason = "nonunique operation name"
		case len(orvalStrings(n.Attributes["request_content_types"])) > 1:
			reason = "multiple request content types"
		case !ascii(tag):
			reason = "non-ASCII first tag"
		}
		if reason != "" {
			diagnostics = append(diagnostics, orvalDiagnostic(file, project, "generated_client_operation_unmodeled", n.Identity+": "+reason))
			continue
		}
		verb, _ := n.Attributes["method"].(string)
		raw, _ := n.Attributes["path"].(string)
		url := orvalParameter.ReplaceAllStringFunc(raw, func(s string) string { return "${" + orvalCamel(s[1:len(s)-1]) + "}" })
		if strings.HasSuffix(base, "/") && strings.HasPrefix(url, "/") {
			url = base + url[1:]
		} else {
			url = base + url
		}
		index := 0
		hasQuery, hasHeader := false, false
		hasBody := len(orvalStrings(n.Attributes["request_content_types"])) > 0
		data, _ := json.Marshal(n.Attributes["parameters"])
		var params []map[string]any
		_ = json.Unmarshal(data, &params)
		for _, param := range params {
			switch param["in"] {
			case "path":
				index++
			case "query":
				hasQuery = true
			case "header":
				hasHeader = true
			case "body", "formData":
				hasBody = true
			}
		}
		if hasBody {
			index++
		}
		if hasQuery {
			index++
		}
		if hasHeader {
			index++
		}
		tag = orvalKebab(tag)
		p.operations = append(p.operations, orvalOperation{node: n, name: name, file: path.Join(p.directory, tag, tag+".ts"), method: verb, url: url, optionsIndex: index, query: verb == "GET" && regularQuery, hook: verb == "GET" && useQuery || verb != "GET" && useMutation})
	}
	return p, "", diagnostics
}
func orvalStrings(value any) []string {
	data, _ := json.Marshal(value)
	var out []string
	_ = json.Unmarshal(data, &out)
	return out
}

func (r *resolver) urlExternal(c evaluationContext, e expr, pkg, name string) bool {
	member := ""
	if e.Kind == "member_expression" && e.Object != nil {
		member = e.Name
		e = *e.Object
	}
	if e.Kind != "identifier" {
		return false
	}
	b, _, ok, _ := newEvaluation(r).binding(c, e.Name)
	if !ok || b.Module == "" || b.Value.Invalid {
		return false
	}
	t := r.modules.ImportBinding(c.file, b.Module, b.Export, member)
	return t.External && !t.Ambiguous && t.File == pkg && t.Name == name
}
func (r *resolver) queryClient(c evaluationContext, e expr) bool {
	if e.Kind != "identifier" {
		return false
	}
	ev := newEvaluation(r)
	b, at, ok, _ := ev.binding(c, e.Name)
	if !ok {
		return false
	}
	if b.Module != "" {
		t := r.modules.ImportBinding(c.file, b.Module, b.Export, "")
		var valid bool
		c, b, valid = ev.imported(t)
		if !valid {
			return false
		}
		at = 0
	}
	v := b.Evaluation
	if v == nil || !v.Constant || at != 0 && !v.Single || v.Mutated || v.Offset >= e.Offset && at != 0 {
		return false
	}
	c.scope = at
	init := v.Value
	if init.Object == nil {
		return false
	}
	if init.Kind == "new_expression" {
		return r.urlExternal(c, *init.Object, "@tanstack/react-query", "QueryClient") || r.urlExternal(c, *init.Object, "@tanstack/query-core", "QueryClient")
	}
	return at != 0 && init.Kind == "call_expression" && r.urlExternal(c, *init.Object, "@tanstack/react-query", "useQueryClient")
}
func (r *resolver) scheduled(file string, call facts.ClientCall) bool {
	for _, parent := range r.files[file].Routes.Clients {
		e := parent.Expression
		if len(e.Args) == 0 || e.Args[0].Offset != call.Expression.Offset || e.Object == nil {
			continue
		}
		f := *e.Object
		c := evaluationContext{file: file, scope: parent.URLScope}
		if r.urlExternal(c, f, "@tanstack/react-query", "useQuery") || r.urlExternal(c, f, "@tanstack/react-query", "useSuspenseQuery") {
			return true
		}
		if f.Kind == "member_expression" && f.Object != nil && (f.Name == "fetchQuery" || f.Name == "ensureQueryData" || f.Name == "prefetchQuery") && r.queryClient(c, *f.Object) {
			return true
		}
	}
	return false
}
func (r *resolver) orvalImport(file string, call facts.ClientCall) (string, string, bool) {
	if call.Expression.Object == nil {
		return "", "", false
	}
	e := *call.Expression.Object
	member := ""
	if e.Kind == "member_expression" && e.Object != nil {
		member = e.Name
		e = *e.Object
	}
	if e.Kind != "identifier" {
		return "", "", false
	}
	b, _, ok := r.binding(file, call.Scope, e.Name)
	if !ok || b.Module == "" || b.Value.Invalid {
		return "", "", false
	}
	export := b.Export
	if member != "" {
		if export != "*" {
			return "", "", false
		}
		export = member
	} else if export == "*" {
		return "", "", false
	}
	resolved := r.modules.Resolve(file, b.Module)
	if resolved.File != "" || len(resolved.Candidates) == 0 {
		return "", "", false
	}
	return resolved.Candidates[0], export, true
}
func predictedFile(candidate, file string) bool {
	return candidate == file || candidate == strings.TrimSuffix(file, ".ts") || candidate == strings.TrimSuffix(file, ".ts")+".js"
}

// LinkOrval derives invocation facts only after OpenAPI declarations exist.
// Generated source, when scanned, remains authoritative over this bounded model.
// Existing client attribute maps in the context are updated when a proven
// mutator send must become a wrapper.
func LinkOrval(files map[string]facts.Facts, modules *typescript.ModuleResolver, sources map[string][]byte, specs, routes []contract.Node, root string, rules testscope.Rules) ([]contract.Node, []contract.Edge, []contract.Diagnostic) {
	r := &resolver{files: files, modules: modules, defaults: map[string][]baseAssignment{}, memo: map[string]memoValue{}}
	var projects []orvalProject
	var diagnostics []contract.Diagnostic
	for _, file := range facts.Paths(files) {
		base := path.Base(file)
		ext := path.Ext(base)
		if strings.TrimSuffix(base, ext) != "orval.config" || !strings.Contains(" .ts .js .mjs .mts ", " "+ext+" ") {
			continue
		}
		d := files[file].Routes
		if d == nil {
			continue
		}
		b, _, ok := r.binding(file, 0, d.Exports["default"])
		e := b.Value
		if !ok || e.Kind != "call_expression" || e.Object == nil || !r.urlExternal(evaluationContext{file: file}, *e.Object, "orval", "defineConfig") || len(e.Args) != 1 || e.Args[0].Kind != "object" || e.Args[0].Invalid {
			diagnostics = append(diagnostics, orvalDiagnostic(file, "<default>", "generated_client_model_unproven", "default export must call external orval defineConfig with one object"))
			continue
		}
		for _, name := range e.Args[0].Keys {
			p, reason, ds := r.orvalModel(file, name, e.Args[0].Properties[name], sources, specs)
			diagnostics = append(diagnostics, ds...)
			if reason != "" {
				diagnostics = append(diagnostics, orvalDiagnostic(file, name, "generated_client_model_unproven", reason))
				continue
			}
			if p.directory == "" {
				continue
			}
			projects = append(projects, p)
			diagnostics = append(diagnostics, orvalDiagnostic(file, name, "generated_client_absent", generatedAbsent(root, p.directory, sources)))
		}
	}
	if len(projects) == 0 {
		return nil, nil, diagnostics
	}
	var nodes []contract.Node
	var edges []contract.Edge
	duplicates := map[string]int{}
	transportDiagnostics := map[string]bool{}
	matcher := newMatcher(routes, routesProof(files, modules, routes)).withNextOrigins(nextOrigins(sources))
	for _, file := range facts.Paths(files) {
		d := files[file].Routes
		if d == nil {
			continue
		}
		calls := append([]facts.ClientCall(nil), d.Clients...)
		sort.SliceStable(calls, func(i, j int) bool { return sourceBefore(calls[i].Span, calls[j].Span) })
		for _, call := range calls {
			candidate, export, ok := r.orvalImport(file, call)
			if !ok {
				continue
			}
			for _, p := range projects {
				inProject := false
				for _, op := range p.operations {
					inProject = inProject || predictedFile(candidate, op.file)
				}
				if !inProject {
					continue
				}
				for _, op := range p.operations {
					kind := ""
					if export == op.name {
						kind = "direct"
					}
					if op.query && export == orvalCamel("get-"+op.name+"-queryOptions") && r.scheduled(file, call) {
						kind = "query_options"
					}
					if op.hook && export == orvalCamel("use-"+op.name) {
						kind = "hook"
					}
					if kind == "" {
						continue
					}
					if kind != "direct" {
						optionsIndex := op.optionsIndex
						if kind == "hook" && op.method != "GET" {
							optionsIndex = 0
						}
						options := arg(call.Expression.Args, optionsIndex)
						query, ok := property(options, "query")
						_, override := property(query, "queryFn")
						if options.Kind == "object" && query.Kind == "object" && ok && override {
							diagnostics = append(diagnostics, orvalDiagnostic(file, p.name, "generated_client_override", fmt.Sprintf("%s:%d: %s has query.queryFn", file, call.Span.StartLine, export)))
							continue
						}
					}
					callee := compact(call.Expression.Object.Text)
					name := op.method + " " + callee
					qualified := facts.Join(call.OwnerName, name)
					key := file + "#" + qualified
					duplicates[key]++
					if duplicates[key] > 1 {
						qualified += fmt.Sprintf("@%d", duplicates[key])
					}
					identity := facts.ID("client_call", file, qualified)
					attrs := map[string]any{"library": "orval", "method": op.method, "invocation": kind, "operation": op.node.Identity, "generated_file": op.file, "generated_export": export, "url_expression": callee, "wrapper": nil, "base": "unresolved", "status": "dynamic", "paths": []map[string]any{{"template": nil, "origin": nil, "conditional": false, "resolution": "generated", "status": "dynamic", "match": nil, "candidates": []string{}, "candidate_count": 0}}}
					if p.mutator != "" {
						attrs["wrapper"] = p.mutator
					}
					if testscope.Classify(file, rules) == testscope.Test {
						attrs["scope"] = "test"
					}
					if !predictedFile(candidate, op.file) {
						attrs["wrapper"] = nil
						attrs["operation"] = nil
						attrs["paths"] = []map[string]any{}
						attrs["status"] = "unmatched"
						diagnostics = append(diagnostics, orvalDiagnostic(file, p.name, "generated_client_mismatch", fmt.Sprintf("%s:%d: %s expected %s", file, call.Span.StartLine, export, op.file)))
					} else {
						edges = append(edges, contract.Edge{Type: "edge", Kind: "calls_operation", Source: identity, Target: op.node.Identity, Provenance: "inferred", Confidence: .9, Evidence: []contract.Evidence{{File: file, Span: call.Span}}})
						transport, evaluation, evidence, reason := r.orvalTransport(p, op)
						if reason != "" {
							key := p.config + "#" + p.name + "#" + reason
							if !transportDiagnostics[key] {
								diagnostics = append(diagnostics, orvalDiagnostic(p.config, p.name, "generated_client_transport_unproven", p.mutator+": "+reason))
								transportDiagnostics[key] = true
							}
						} else {
							for _, key := range []string{"paths", "base", "base_expression", "helpers", "helper_count"} {
								if value, ok := transport[key]; ok {
									attrs[key] = value
								}
							}
							for _, path := range attrs["paths"].([]map[string]any) {
								path["resolution"] = "generated"
							}
							links := matcher.linkPaths(identity, file, call.Span, attrs, evaluation)
							for i := range links {
								if evidence != nil {
									links[i].Evidence = append(links[i].Evidence, *evidence)
								}
							}
							edges = append(edges, links...)
							if evidence != nil {
								for _, n := range routes {
									if n.Kind == "client_call" && n.File == evidence.File && n.Span != nil && *n.Span == evidence.Span {
										n.Attributes["status"] = "wrapper"
									}
								}
							}
						}
					}
					nodes = append(nodes, contract.Node{Type: "node", Kind: "client_call", Level: 4, Identity: identity, Name: name, QualifiedName: qualified, File: file, Span: &call.Span, ContentHash: call.Hash, Attributes: attrs})
					edges = append(edges, facts.Edge("contains", call.Owner, identity, file, call.Span))
				}
			}
		}
	}
	return nodes, edges, diagnostics
}

func generatedAbsent(root, directory string, sources map[string][]byte) string {
	if root == "" {
		return "generated output " + directory + " has no scanned files"
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(directory))); os.IsNotExist(err) {
		return "generated output " + directory + " is absent (directory does not exist)"
	}
	if source := generatedIgnoreSource(directory, sources); source != "" {
		return "generated output " + directory + " is excluded by " + source + "; re-include ignored paths with .graphveinignore negation (parents must remain visible; built-in ignores cannot be negated)"
	}
	return "generated output " + directory + " exists but has no scanned files"
}

func generatedIgnoreSource(directory string, sources map[string][]byte) string {
	parts := strings.Split(directory, "/")
	var ignores []string
	for i, part := range parts {
		if strings.Contains(" .git .graphvein .kilo .opencode .claude .codex .agents .cursor .worktrees worktrees node_modules target dist build vendor vendored .venv venv __pycache__ ", " "+part+" ") {
			return "built-in ignore"
		}
		prefix := strings.Join(parts[:i+1], "/")
		ignores = append(ignores, path.Join(strings.Join(parts[:i], "/"), ".gitignore"))
		ignoredBy := ""
		for _, file := range append(append([]string{}, ignores...), ".graphveinignore") {
			base := path.Dir(file)
			relative := prefix
			if base != "." {
				relative = strings.TrimPrefix(prefix, base+"/")
			}
			for _, line := range strings.Split(string(sources[file]), "\n") {
				line = strings.TrimRight(line, " \r")
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				negate := strings.HasPrefix(line, "!")
				if negate {
					line = line[1:]
				}
				pattern, err := generatedIgnorePattern(line)
				if err != nil {
					break
				}
				if pattern.MatchString(relative) {
					if negate {
						ignoredBy = ""
					} else {
						ignoredBy = file
					}
				}
			}
		}
		if ignoredBy != "" {
			return ignoredBy
		}
	}
	return ""
}
