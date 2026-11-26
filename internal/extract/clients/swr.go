package clients

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/routes"
	"github.com/graphvein/graphvein/internal/lang/facts"
)

// swrHooks are the SWR hooks row NX-L2 reads, by resolved module and export.
var swrHooks = map[string]map[string]bool{
	"swr":           {"default": true, "useSWR": true},
	"swr/immutable": {"default": true},
}

// swrRequest reads `useSWR(k, f)` (row NX-L2). k is a literal or bounded
// const string, or a key the bounded URL evaluator (ADR 0020) reads as
// strings, where a `null` alternative is SWR's "do not fetch" key and
// drops out. f is either a resolved fetch wrapper that forwards its first
// parameter unchanged as the URL of a fetch without options, or an inline
// arrow function whose expression body starts with a fetch or axios GET
// of its only parameter, without options (proven on the arrow's text and
// the recorded call at the body's start). The request is then a GET of k
// through that fetcher; inline is the site key of the inline fetcher's
// request, so its own node can be marked as the transport. swr reports
// whether the callee is an SWR hook at all, so unread forms can be
// counted (row D).
func (r *resolver) swrRequest(file string, c facts.ClientCall, wrappers map[string]wrapper) (q request, inline string, swr bool, ok bool) {
	e := c.Expression
	if e.Object == nil {
		return request{}, "", false, false
	}
	callee := r.resolve(file, c.Scope, *e.Object, 0, nil)
	if callee.kind != "external" || !swrHooks[callee.file][callee.name] {
		return request{}, "", false, false
	}
	if len(e.Args) < 2 || !r.swrKey(file, c, e.Args[0]) {
		return request{}, "", true, false
	}
	key := c
	key.Expression.Args = e.Args[:1]
	fetcher := e.Args[1]
	switch fetcher.Kind {
	case "identifier":
		target := r.functionIdentity(file, c.Scope, fetcher.Name)
		w, found := wrappers[target]
		if !found || w.request.library != "fetch" || len(w.params) == 0 || w.request.config.Kind != "" || w.request.url.Kind != "identifier" || w.request.url.Name != w.params[0].Name {
			return request{}, "", true, false
		}
		q = r.applyWrapper(file, key, w, target)
	case "arrow_function":
		transport, site, found := r.inlineFetcher(file, fetcher)
		if !found {
			return request{}, "", true, false
		}
		q, inline = transport, site
		q.url = e.Args[0]
	default:
		return request{}, "", true, false
	}
	q.swr = true
	q.method = defaultMethod()
	return q, inline, true, true
}

// swrKey reports whether an SWR key is read: a literal or bounded const
// string, or an expression whose every evaluated value is a string or
// `null` (at least one string).
func (r *resolver) swrKey(file string, c facts.ClientCall, k expr) bool {
	if _, literal := r.literal(file, c.Scope, k, nil); literal {
		return true
	}
	ev := newEvaluation(r)
	values := ev.expression(evaluationContext{file: file, scope: c.URLScope}, k)
	if ev.exhausted {
		return false
	}
	read := 0
	for _, v := range values {
		switch v.kind {
		case "string":
			read++
		case "null":
		default:
			return false
		}
	}
	return read > 0
}

// inlineArrow matches the head of an arrow function with one plain
// parameter (an optional type annotation, an optional return type) up to
// its expression body.
var inlineArrow = regexp.MustCompile(`^(?:async\s*)?(?:([A-Za-z_$][A-Za-z0-9_$]*)|\(\s*([A-Za-z_$][A-Za-z0-9_$]*)\s*(?::\s*[^,()=]+?)?\s*\))\s*(?::\s*[^=(){}]+?)?\s*=>\s*`)

// inlineFetcher proves an inline SWR fetcher: the arrow's expression body
// starts with a recorded fetch or axios call whose URL is the parameter
// and which has no options and method GET, and that call is the only
// request inside the arrow. It returns that request and its site key.
func (r *resolver) inlineFetcher(file string, arrow expr) (request, string, bool) {
	m := inlineArrow.FindStringSubmatch(arrow.Text)
	if m == nil || strings.HasPrefix(arrow.Text[len(m[0]):], "{") {
		return request{}, "", false
	}
	name := m[1] + m[2]
	start, end := arrow.Offset+len(m[0]), arrow.Offset+len(arrow.Text)
	d := r.files[file].Routes
	if d == nil {
		return request{}, "", false
	}
	var found *facts.ClientCall
	for i := range d.Clients {
		inner := d.Clients[i]
		if inner.Expression.Offset < arrow.Offset || inner.Expression.Offset >= end {
			continue
		}
		if _, recognized := r.recognize(file, inner); !recognized {
			continue
		}
		if found != nil || inner.Expression.Offset != start {
			return request{}, "", false
		}
		found = &d.Clients[i]
	}
	if found == nil {
		return request{}, "", false
	}
	q, _ := r.recognize(file, *found)
	if q.library != "fetch" && q.library != "axios" || q.config.Kind != "" || q.url.Kind != "identifier" || q.url.Name != name {
		return request{}, "", false
	}
	if method, static := r.literal(file, found.Scope, q.method, nil); !static || !strings.EqualFold(method, "GET") {
		return request{}, "", false
	}
	// The parameter must be the arrow's own: bound as a parameter in the
	// function scope that encloses the call.
	nearest := found.Scope
	for nearest > 0 && nearest < len(d.Scopes) && !d.Scopes[nearest].Function {
		nearest = d.Scopes[nearest].Parent
	}
	b, at, bound := r.binding(file, found.Scope, name)
	if !bound || b.Value.Kind != "parameter" || at != nearest || at == 0 {
		return request{}, "", false
	}
	return q, siteKey(file, found.Span), true
}

// functionIdentity resolves a name in scope to a function declaration:
// an import through the shared resolver, else a module-level function of
// the file. A name bound in an inner scope is not resolved.
func (r *resolver) functionIdentity(file string, scope int, name string) string {
	b, at, bound := r.binding(file, scope, name)
	if !bound {
		return ""
	}
	if b.Module != "" {
		identity := r.modules.ImportBinding(file, b.Module, b.Export, "").Identity
		if kind, _, _, err := contract.ParseIdentity(identity); err == nil && kind == "function" {
			return identity
		}
		return ""
	}
	if at != 0 {
		return ""
	}
	for _, n := range r.files[file].Nodes {
		if n.Kind == "function" && n.QualifiedName == name {
			return n.Identity
		}
	}
	return ""
}

// swrUnread is the diagnostic counting an SWR call row NX-L2 does not read.
func swrUnread(file string, s contract.Span) contract.Diagnostic {
	routes.NextTag("D")
	return contract.Diagnostic{Type: "diagnostic", File: file, Severity: "info", Code: "swr_call_unread", Message: fmt.Sprintf("useSWR at %d:%d: key or fetcher not read (row NX-L2)", s.StartLine, s.StartColumn)}
}

// nextTag reports a Next.js contract row (routes.NextContract); a local
// name, since LinkWithRoot's parameter shadows the routes package.
func nextTag(row string) { routes.NextTag(row) }
