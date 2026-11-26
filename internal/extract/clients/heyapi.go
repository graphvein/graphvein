package clients

import (
	"fmt"
	"path"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/testscope"
)

type heyClient struct {
	base baseAssignment
}

func under(file, dir string) bool { return dir == "." || strings.HasPrefix(file, dir+"/") }

func (r *resolver) evaluatedString(file string, scope int, e expr) (string, bool) {
	ev := newEvaluation(r)
	v := ev.expression(evaluationContext{file: file, scope: scope}, e)
	if ev.exhausted || len(v) != 1 || v[0].kind != "string" || !v[0].known() {
		return "", false
	}
	return v[0].text, true
}

func (r *resolver) heyBase(file string, scope int, e expr) baseAssignment {
	text, ok := r.evaluatedString(file, scope, e)
	kind := "unresolved"
	if ok {
		kind = "literal"
		if e.Kind != "literal" {
			kind = "constant"
		}
	}
	return baseAssignment{text, kind, compact(e.Text)}
}

// setupHeyAPI proves the configured transport before resolving any request.
// Header comments supply diagnostics only; they never confer client identity.
func (r *resolver) setupHeyAPI(sources map[string][]byte, rules testscope.Rules, root string) []contract.Diagnostic {
	r.heyClients = map[string]*heyClient{}
	var diagnostics []contract.Diagnostic
	outputs := []string{}
	for _, file := range facts.Paths(r.files) {
		base := path.Base(file)
		ext := path.Ext(base)
		if strings.TrimSuffix(base, ext) != "openapi-ts.config" || !strings.Contains(" .ts .mts .cts .js .mjs .cjs ", " "+ext+" ") {
			continue
		}
		d := r.files[file].Routes
		if d == nil {
			continue
		}
		name := d.Exports["default"]
		b, _, ok := r.binding(file, 0, name)
		e := b.Value
		if !ok || e.Kind != "call_expression" || e.Object == nil || !r.external(file, 0, *e.Object, "@hey-api/openapi-ts", "defineConfig") || len(e.Args) != 1 {
			continue
		}
		config := e.Args[0]
		if config.Kind != "object" || config.Invalid {
			continue
		}
		out, ok := property(config, "output")
		if !ok {
			continue
		}
		if out.Kind == "object" {
			out, _ = property(out, "path")
		}
		output, ok := r.evaluatedString(file, 0, out)
		if !ok || path.IsAbs(output) {
			continue
		}
		plugins, ok := property(config, "plugins")
		if !ok || plugins.Kind != "array" {
			continue
		}
		axios := false
		for _, plugin := range plugins.Parts {
			if plugin.Kind == "object" && !plugin.Invalid {
				plugin, _ = property(plugin, "name")
			}
			if plugin.Kind == "literal" && plugin.Name == "@hey-api/client-axios" {
				axios = true
			}
		}
		if !axios {
			continue
		}
		dir := path.Clean(path.Join(path.Dir(file), output))
		if dir == ".." || strings.HasPrefix(dir, "../") || path.IsAbs(dir) || strings.Contains(dir, ":") {
			continue
		}
		anyFiles := false
		for f := range sources {
			anyFiles = anyFiles || under(f, dir)
		}
		if !anyFiles {
			diagnostics = append(diagnostics, contract.Diagnostic{Type: "diagnostic", File: file, Severity: "info", Code: "generated_client_absent", Message: fmt.Sprintf("%s: %s", file, generatedAbsent(root, dir, sources))})
		}
		clientFile := path.Join(dir, "client.gen.ts")
		cd := r.files[clientFile].Routes
		if cd == nil || cd.Exports["client"] != "client" {
			continue
		}
		cb, _, ok := r.binding(clientFile, 0, "client")
		if !ok || !cb.ModuleConst {
			continue
		}
		ce := cb.Value
		if ce.Kind != "call_expression" || ce.Object == nil {
			continue
		}
		importRoot := *ce.Object
		for importRoot.Kind == "member_expression" && importRoot.Object != nil {
			importRoot = *importRoot.Object
		}
		imported, _, ok := r.binding(clientFile, 0, importRoot.Name)
		if importRoot.Kind != "identifier" || !ok || imported.Module == "" {
			continue
		}
		factory := r.token(clientFile, 0, *ce.Object)
		if factory.External || factory.Ambiguous || factory.Name != "createClient" || !under(factory.File, dir) || r.files[factory.File].Routes == nil {
			continue
		}
		client := &heyClient{base: baseAssignment{kind: "none"}}
		// The richer initializer cache retains spreads and expression values.
		if b := cd.URLScopes[0].Bindings["client"]; b.Evaluation != nil {
			ce = b.Evaluation.Value
		}
		initial := arg(ce.Args, 0)
		if initial.Kind == "call_expression" && initial.Object != nil {
			factory := r.token(clientFile, 0, *initial.Object)
			if !factory.External && !factory.Ambiguous && factory.Name == "createConfig" && under(factory.File, dir) {
				initial = arg(initial.Args, 0)
			} else {
				initial = expr{Kind: "unknown"}
			}
		}
		if initial.Kind != "" {
			if initial.Kind != "object" || initial.Invalid {
				client.base = baseAssignment{kind: "unresolved", expression: "initializer config"}
			} else if v, ok := property(initial, "baseURL"); ok {
				client.base = r.heyBase(clientFile, 0, v)
			}
		}
		r.heyClients[clientFile+"#client"] = client
		outputs = append(outputs, dir)
	}
	for key, client := range r.heyClients {
		count, unresolved := 0, false
		for _, file := range facts.Paths(r.files) {
			d := r.files[file].Routes
			if d == nil {
				continue
			}
			for _, call := range d.Calls {
				if call.Method != "setConfig" {
					continue
				}
				receiver := r.resolve(file, call.Scope, call.Receiver, 0, nil)
				if receiver.kind != "hey-api" || receiver.key != key {
					continue
				}
				config := arg(call.Args, 0)
				if call.Scope != 0 || call.Conditional || testscope.Classify(file, rules) == testscope.Test || len(call.Args) != 1 || config.Kind != "object" || config.Invalid {
					unresolved = true
					continue
				}
				if base, ok := property(config, "baseURL"); ok {
					ownBases := 0
					for _, entry := range config.Keywords {
						if entry.Name == "baseURL" {
							ownBases++
						}
					}
					if ownBases != 1 {
						unresolved = true
					}
					count++
					client.base = r.heyBase(file, 0, base)
				}
			}
			for _, call := range d.Clients {
				e := call.Expression
				var receiver expr
				if e.Kind == "config_mutation" && len(e.Parts) > 0 {
					receiver = e.Parts[0]
					for receiver.Object != nil {
						receiver = *receiver.Object
					}
				} else if e.Object != nil && e.Object.Kind == "member_expression" && e.Object.Object != nil && e.Object.Object.Name == "Object" && (e.Object.Name == "assign" || e.Object.Name == "defineProperty" || e.Object.Name == "defineProperties" || e.Object.Name == "setPrototypeOf") {
					receiver = arg(e.Args, 0)
					for receiver.Object != nil {
						receiver = *receiver.Object
					}
				}
				v := r.resolve(file, call.Scope, receiver, 0, nil)
				if v.kind == "hey-api" && v.key == key {
					unresolved = true
				}
			}
		}
		if unresolved || count > 1 {
			client.base = baseAssignment{kind: "unresolved", expression: "hey-api config mutations"}
		}
	}
	for _, file := range facts.Paths(r.files) {
		s := strings.TrimSpace(string(sources[file]))
		comment := ""
		if strings.HasPrefix(s, "//") {
			comment = strings.SplitN(s, "\n", 2)[0]
		} else if strings.HasPrefix(s, "/*") {
			comment = strings.SplitN(s, "*/", 2)[0]
		}
		if !strings.Contains(comment, "generated") || !strings.Contains(comment, "@hey-api/openapi-ts") {
			continue
		}
		proven := false
		for _, dir := range outputs {
			proven = proven || under(file, dir)
		}
		if !proven {
			diagnostics = append(diagnostics, contract.Diagnostic{Type: "diagnostic", File: file, Severity: "info", Code: "generated_client_unproven", Message: "generated @hey-api/openapi-ts file is outside a proven client-axios output directory: " + file})
		}
	}
	return diagnostics
}

func (r *resolver) heyRequest(file string, c facts.ClientCall) (request, bool) {
	if len(r.heyClients) == 0 {
		return request{}, false
	}
	e := c.Expression
	if e.Object == nil || e.Object.Kind != "member_expression" || e.Object.Object == nil {
		return request{}, false
	}
	f := *e.Object
	receiver := *f.Object
	if receiver.Kind == "binary_expression" && receiver.Operator == "??" && len(receiver.Parts) == 2 {
		p := receiver.Parts[0]
		if p.Kind != "member_expression" || p.Name != "client" || p.Object == nil || p.Object.Kind != "identifier" {
			return request{}, false
		}
		b, at, ok := r.binding(file, c.Scope, p.Object.Name)
		if !ok || b.Value.Kind != "parameter" || at != r.heyFunctionScope(file, c.Scope) {
			return request{}, false
		}
		receiver = receiver.Parts[1]
	}
	v := r.resolve(file, c.Scope, receiver, 0, nil)
	if v.kind != "hey-api" {
		return request{}, false
	}
	if f.Name == "setConfig" || f.Name == "getConfig" || f.Name == "buildUrl" {
		return request{}, false
	}
	q := request{library: "hey-api", object: v, config: arg(e.Args, 0), url: expr{Kind: "unknown", Text: arg(e.Args, 0).Text}, method: expr{Kind: "unknown"}}
	knownVerb := strings.Contains(" connect delete get head options patch post put trace ", " "+f.Name+" ")
	if knownVerb {
		q.method = expr{Kind: "literal", Name: strings.ToUpper(f.Name)}
	}
	if !knownVerb || r.heyFunctionScope(file, c.Scope) < 0 || len(e.Args) != 1 || q.config.Kind != "object" {
		return q, true
	}
	url, hasURL := property(q.config, "url")
	if hasURL {
		q.url.Text = url.Text
	}
	count, invalid := 0, false
	for _, entry := range q.config.Keywords {
		if entry.Name == "url" {
			count++
		}
		if entry.Name == "" {
			spread := entry.Value
			// A1 proves the generated Options type excludes URL. Only that
			// immediate parameter spread is admitted; arbitrary spreads are gaps.
			if spread.Kind != "spread_element" || len(spread.Parts) != 1 {
				invalid = true
				continue
			}
			// Earlier spreads cannot replace the following own URL. The
			// corpus's form-body serializer uses precisely this shape.
			if count == 0 {
				continue
			}
			p := spread.Parts[0]
			b, at, ok := r.binding(file, c.Scope, p.Name)
			if p.Kind != "identifier" || !ok || b.Value.Kind != "parameter" || at != r.heyFunctionScope(file, c.Scope) {
				invalid = true
			}
		}
	}
	if count != 1 || invalid {
		return q, true
	}
	if _, override := property(q.config, "client"); override {
		return q, true
	}
	if value, ok := r.evaluatedString(file, c.URLScope, url); ok && simpleHeyURL(value) {
		// Keep source syntax so pathAttributes retains constant/helper proof,
		// resolution labels and return/property evidence on the resulting edge.
		q.url = url
	}
	return q, true
}

func (r *resolver) heyFunctionScope(file string, scope int) int {
	d := r.files[file].Routes
	for scope >= 0 && scope < len(d.Scopes) {
		if d.Scopes[scope].Function {
			return scope
		}
		scope = d.Scopes[scope].Parent
	}
	return -1
}

func simpleHeyURL(url string) bool {
	url = strings.SplitN(strings.SplitN(url, "?", 2)[0], "#", 2)[0]
	for _, segment := range strings.Split(url, "/") {
		if !strings.ContainsAny(segment, "{}") {
			continue
		}
		if !strings.HasPrefix(segment, "{") || !strings.HasSuffix(segment, "}") {
			return false
		}
		name := segment[1 : len(segment)-1]
		if name == "" || strings.ContainsAny(name, "{}*") || strings.HasPrefix(name, ".") || strings.HasPrefix(name, ";") {
			return false
		}
	}
	return true
}

func (r *resolver) heyOverrides(requests map[string]request) []contract.Diagnostic {
	if len(r.heyClients) == 0 {
		return nil
	}
	methods := map[string]bool{}
	for _, file := range facts.Paths(r.files) {
		d := r.files[file].Routes
		if d == nil {
			continue
		}
		for _, c := range d.Clients {
			if requests[siteKey(file, c.Span)].library == "hey-api" {
				methods[c.Owner] = true
			}
		}
	}
	var out []contract.Diagnostic
	for _, file := range facts.Paths(r.files) {
		d := r.files[file].Routes
		if d == nil {
			continue
		}
		for _, c := range d.Clients {
			f := c.Expression.Object
			if f == nil {
				continue
			}
			target := r.token(file, c.Scope, *f)
			if f.Kind == "member_expression" && f.Object != nil {
				class := r.token(file, c.Scope, *f.Object)
				if !class.External && !class.Ambiguous {
					for _, n := range r.files[class.File].Nodes {
						if n.QualifiedName == class.Name+"."+f.Name {
							target.Identity = n.Identity
						}
					}
				}
			}
			if !methods[target.Identity] {
				continue
			}
			options := arg(c.Expression.Args, 0)
			if options.Kind != "object" {
				continue
			}
			for _, key := range []string{"client", "baseURL", "url"} {
				if _, ok := property(options, key); ok {
					out = append(out, contract.Diagnostic{Type: "diagnostic", File: file, Severity: "info", Code: "generated_client_override", Message: fmt.Sprintf("%s:%d invokes %s with a client/baseURL/url override; generated request links describe the SDK default", file, c.Span.StartLine, target.Identity)})
					break
				}
			}
		}
	}
	return out
}

// HeyAPIAuthRow is one row of the hey-api auth claim contract (rows DA-H
// of docs/contract/claims/architecture-graph.md).
type HeyAPIAuthRow struct {
	ID   string
	Form string
}

// HeyAPIAuthContract lists the rows in task-file order; DAD is the single
// unknown branch (no attribute).
var HeyAPIAuthContract = []HeyAPIAuthRow{
	{"DA-H1", "security: [...] in a hey-api call's options object literal"},
	{"DAD", "every other form"},
}

// authTag is a test-only hook: every branch reports its row ("<row>" or
// "<row>/<form>").
var authTag func(row string)

func tagAuth(row string) {
	if authTag != nil {
		authTag(row)
	}
}

// heySecurityKeys are the keys of a generated security entry (hey-api's
// Auth type: in, key, name, scheme, type).
var heySecurityKeys = []string{"in", "key", "name", "scheme", "type"}

// heySecurity is row DA-H1: the security array of a hey-api call's options
// object literal, as written (the generated client documents the
// operation's scheme; the transport adds credentials only when the
// application configures auth, which is not read). A present entry that
// is not a literal array of literal objects is "unknown"; no security
// property is DAD (no attribute).
func heySecurity(q request) (any, bool) {
	if q.library != "hey-api" || q.config.Kind != "object" {
		tagAuth("DAD/config")
		return nil, false
	}
	v, ok := property(q.config, "security")
	if !ok {
		tagAuth("DAD/absent")
		return nil, false
	}
	if q.config.Invalid && !onlySpreads(q.config) || v.Kind != "array" {
		tagAuth("DA-H1/unknown")
		return "unknown", true
	}
	out := []map[string]string{}
	for _, entry := range v.Parts {
		if entry.Kind != "object" || entry.Invalid || entry.Spread {
			tagAuth("DA-H1/unknown")
			return "unknown", true
		}
		m := map[string]string{}
		for _, key := range entry.Keys {
			value := entry.Properties[key]
			known := false
			for _, k := range heySecurityKeys {
				known = known || k == key
			}
			if !known || value.Kind != "literal" {
				tagAuth("DA-H1/unknown")
				return "unknown", true
			}
			m[key] = value.Name
		}
		out = append(out, m)
	}
	tagAuth("DA-H1")
	return out, true
}

// onlySpreads reports whether an object literal's own entries are plain
// keyed properties and spreads only (the generated options object spreads
// the caller's options): the property map is then exact for its keys.
func onlySpreads(e expr) bool {
	for _, entry := range e.Keywords {
		if entry.Name == "__proto__" || entry.Name == "" && entry.Value.Kind != "spread_element" {
			return false
		}
	}
	return true
}
