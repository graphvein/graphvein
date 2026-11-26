package routes

import (
	"fmt"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
)

// This file composes Axum `Router::layer` / `Router::route_layer` onto the
// routes they wrap (T026, ADR 0079 option 3A,
// docs/extract/auth.md "Axum"). Read in axum 0.7.9 and 0.8.8
// (`routing/mod.rs`, `routing/path_router.rs`): both methods map over the
// routes the router holds when they are called (`self.routes.into_iter()
// .map(|(id, endpoint)| endpoint.layer(layer.clone()))`), so a layer wraps
// the routes added before it on that router, never later ones;
// `route_layer` differs only in leaving the fallback alone. `nest` and
// `merge` move the other router's already layered endpoints into this
// one, so a nested or merged route keeps its own router's layers and gains
// every layer the parent applies after the nest or merge. The facts say
// which layers the code applies, as written; a layer from a closed list
// is named ("declares tower-http ValidateRequestHeaderLayer (bearer)",
// "middleware function f"), never "protected".

// AxumAuthContract lists the Axum rows in task-file order. Every form the
// code reads maps to exactly one row; everything else is DAD (an entry in
// layers_unknown, or nothing).
var AxumAuthContract = []AuthRow{
	{"DA-X1", "Router::layer / Router::route_layer on a proven axum Router after the route's registration or its nest/merge"},
	{"DA-X2", "a layer argument from a closed list (tower-http ValidateRequestHeaderLayer::bearer/basic, axum::middleware::from_fn/from_fn_with_state, axum-login login_required!)"},
	{"DA-X3", "MethodRouter::layer / ::route_layer on the method router of a route, wrapping the methods chained before it"},
	{"DA-X4", "the inner layers of a tower ServiceBuilder::new() stack passed as a layer, in written order (outermost first)"},
	{"DAD", "every other form"},
}

// axumLayer is one layer call on a proven router.
type axumLayer struct {
	file, object, method string
	expr                 facts.RustExpr
	span                 contract.Span
	conditional          bool
	seq                  int
}

// axumEdge is one nest, merge or factory return: to's routes move into
// from at seq.
type axumEdge struct {
	from, to, via string
	seq           int
}

// maxAxumChains bounds the mount chains read per route.
const maxAxumChains = 64

func (r *axumLinker) edge(from, to, via string) {
	r.seq++
	r.edges = append(r.edges, axumEdge{from: from, to: to, seq: r.seq, via: via})
}

func (r *axumLinker) layer(file, object string, e facts.RustExpr) {
	if len(e.Args) != 1 {
		return
	}
	r.seq++
	r.layers = append(r.layers, axumLayer{file: file, object: object, method: e.Name, expr: e.Args[0], span: e.Span, conditional: e.Conditional, seq: r.seq})
}

// layerText is the layer argument as written, whitespace collapsed.
func layerText(e facts.RustExpr) string {
	return strings.Join(strings.Fields(stripRustComments(e.Text)), " ")
}

// stripRustComments removes line and (nested) block comments from Rust
// source text, keeping string, raw string and character literals: a
// layer "as written" is its code, not its comments (T026 review
// fix). A comment becomes one space.
func stripRustComments(src string) string {
	var b strings.Builder
	n := len(src)
	for i := 0; i < n; {
		c := src[i]
		switch {
		case c == '/' && i+1 < n && src[i+1] == '/':
			for i < n && src[i] != '\n' {
				i++
			}
			b.WriteByte(' ')
		case c == '/' && i+1 < n && src[i+1] == '*':
			depth := 0
			for i < n {
				if i+1 < n && src[i] == '/' && src[i+1] == '*' {
					depth++
					i += 2
					continue
				}
				if i+1 < n && src[i] == '*' && src[i+1] == '/' {
					depth--
					i += 2
					if depth == 0 {
						break
					}
					continue
				}
				i++
			}
			b.WriteByte(' ')
		case c == 'r' && i+1 < n && (src[i+1] == '"' || src[i+1] == '#') && (i == 0 || !identByte(src[i-1])):
			j := i + 1
			hashes := 0
			for j < n && src[j] == '#' {
				hashes++
				j++
			}
			if j >= n || src[j] != '"' {
				b.WriteByte(c)
				i++
				continue
			}
			end := strings.Index(src[j+1:], "\""+strings.Repeat("#", hashes))
			if end < 0 {
				b.WriteString(src[i:])
				return b.String()
			}
			stop := j + 1 + end + 1 + hashes
			b.WriteString(src[i:stop])
			i = stop
		case c == '"':
			j := i + 1
			for j < n && src[j] != '"' {
				if src[j] == '\\' {
					j++
				}
				j++
			}
			if j >= n {
				b.WriteString(src[i:])
				return b.String()
			}
			b.WriteString(src[i : j+1])
			i = j + 1
		case c == '\'' && i+2 < n && (src[i+2] == '\'' || src[i+1] == '\\'):
			// A character literal ('/', '\'', '\n'); a lifetime has no
			// closing quote.
			j := i + 2
			if src[i+1] == '\\' {
				for j < n && src[j] != '\'' {
					j++
				}
			}
			if j >= n {
				b.WriteString(src[i:])
				return b.String()
			}
			b.WriteString(src[i : j+1])
			i = j + 1
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}

func identByte(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// classify is row DA-X2: a layer argument that is a call (or macro) from
// the closed list, resolved by the shared Rust resolver. It returns nil
// for every other argument (DAD: the layer is stated as written only).
func (r *axumLinker) classify(file string, e facts.RustExpr) map[string]any {
	switch e.Kind {
	case "call":
		b := r.modules.Resolve(file, e.Scope, e.Name)
		if b.Ambiguous {
			break
		}
		switch {
		case b.External == "tower_http" && (b.Name == "validate_request::ValidateRequestHeaderLayer::bearer" || b.Name == "validate_request::ValidateRequestHeaderLayer::basic"):
			form := b.Name[strings.LastIndex(b.Name, ":")+1:]
			tagAuth("DA-X2/tower-http")
			return map[string]any{"declares": map[string]any{"library": "tower-http", "detail": "ValidateRequestHeaderLayer::" + form}}
		case b.External == "axum" && (b.Name == "middleware::from_fn" || b.Name == "middleware::from_fn_with_state"):
			index := 0
			if b.Name == "middleware::from_fn_with_state" {
				index = 1
			}
			if len(e.Args) > index {
				tagAuth("DA-X2/from_fn")
				return map[string]any{"middleware_function": strings.Join(strings.Fields(e.Args[index].Text), "")}
			}
		}
	case "macro_invocation":
		name, _, _ := strings.Cut(e.Text, "!")
		name = strings.TrimSpace(name)
		b := r.modules.Resolve(file, e.Scope, name)
		if b.External == "axum_login" && b.Name == "login_required" && !b.Ambiguous {
			tagAuth("DA-X2/axum-login")
			detail := "backend ?"
			if open := strings.Index(e.Text, "("); open >= 0 {
				inner := strings.TrimSuffix(strings.TrimSpace(e.Text[open+1:]), ")")
				first, _, _ := strings.Cut(inner, ",")
				if first = strings.Join(strings.Fields(first), ""); first != "" {
					detail = first
				}
			}
			return map[string]any{"declares": map[string]any{"library": "axum-login", "detail": "login_required!(" + detail + ")"}}
		}
	}
	tagAuth("DAD/layer-as-written")
	return nil
}

// axumLevel is one router of a mount chain and the seq after which its
// layers wrap the route: the route's registration, or the nest or merge
// that moved the route's router in.
type axumLevel struct {
	object string
	seq    int
}

// axumChains returns the chains from the route's router up to every root.
func (r *axumLinker) axumChains(object string, seq int) ([][]axumLevel, bool) {
	var out [][]axumLevel
	ok := true
	var walk func([]axumLevel, map[string]bool)
	walk = func(chain []axumLevel, seen map[string]bool) {
		if len(out) >= maxAxumChains {
			ok = false
			return
		}
		top := chain[len(chain)-1]
		parents := 0
		for _, e := range r.edges {
			if e.to != top.object || seen[e.from] {
				continue
			}
			parents++
			seen[e.from] = true
			walk(append(chain, axumLevel{e.from, e.seq}), seen)
			delete(seen, e.from)
		}
		if parents == 0 {
			out = append(out, append([]axumLevel(nil), chain...))
		}
	}
	walk([]axumLevel{{object, seq}}, map[string]bool{object: true})
	return out, ok
}

// routeLayers composes the layers of one registration: layers common to
// every chain, innermost (applied first) first, and unknown entries.
func (r *axumLinker) routeLayers(reg axumRegistration) ([]map[string]any, []map[string]any) {
	chains, ok := r.axumChains(reg.object.key, reg.seq)
	var unknown []map[string]any
	if !ok {
		tagAuth("DA-X1/chain-cap")
		unknown = append(unknown, map[string]any{"reason": fmt.Sprintf("more than %d mount chains", maxAxumChains)})
	}
	type entry struct {
		key  string
		data map[string]any
	}
	count := map[string]int{}
	all := map[string]entry{}
	var first []entry
	seenUnknown := map[string]bool{}
	// DA-X3: the method router's own layers wrap the endpoint inside every
	// Router layer; they are on every chain.
	var out []map[string]any
	for _, m := range reg.methodLayers {
		tagAuth("DA-X3/" + m.Name)
		data := map[string]any{"expression": layerText(m.Args[0]), "method": m.Name, "on": "method_router", "file": reg.file, "span": m.Span}
		for k, v := range r.classify(reg.file, m.Args[0]) {
			data[k] = v
		}
		if stack := r.stack(reg.file, m.Args[0]); stack != nil {
			data["stack"] = stack
		}
		if m.Conditional {
			tagAuth("DA-X1/conditional")
			unknown = append(unknown, map[string]any{"reason": "conditional layer", "expression": data["expression"], "method": m.Name, "file": reg.file, "span": m.Span})
			continue
		}
		out = append(out, data)
	}
	for i, chain := range chains {
		local := map[string]bool{}
		for _, lv := range chain {
			for _, l := range r.layers {
				if l.object != lv.object || l.seq < lv.seq {
					continue
				}
				text := layerText(l.expr)
				key := fmt.Sprintf("%s%v", l.file, l.span)
				if l.conditional {
					tagAuth("DA-X1/conditional")
					if !seenUnknown[key] {
						seenUnknown[key] = true
						unknown = append(unknown, map[string]any{"reason": "conditional layer", "expression": text, "method": l.method, "file": l.file, "span": l.span})
					}
					continue
				}
				if local[key] {
					continue
				}
				local[key] = true
				count[key]++
				if _, found := all[key]; !found {
					tagAuth("DA-X1/" + l.method)
					if lv.object != reg.object.key {
						tagAuth("DA-X1/parent")
					}
					data := map[string]any{"expression": text, "method": l.method, "file": l.file, "span": l.span}
					for k, v := range r.classify(l.file, l.expr) {
						data[k] = v
					}
					if stack := r.stack(l.file, l.expr); stack != nil {
						data["stack"] = stack
					}
					all[key] = entry{key, data}
					if i == 0 {
						first = append(first, all[key])
					}
				}
			}
		}
	}
	for _, e := range first {
		if count[e.key] == len(chains) {
			out = append(out, e.data)
		}
	}
	keys := make([]string, 0, len(all))
	for k := range all {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if count[k] < len(chains) {
			tagAuth("DA-X1/chains")
			d := all[k].data
			unknown = append(unknown, map[string]any{"reason": "applied on some mount chains only", "expression": d["expression"], "method": d["method"], "file": d["file"], "span": d["span"]})
		}
	}
	return out, unknown
}

// serviceBuilderMethods are tower's ServiceBuilder methods that add one
// layer (tower 0.4.11, 0.5.2 and 0.5.3 `builder/mod.rs`) and tower-http's
// ServiceBuilderExt methods (0.6.8 `builder.rs`), each returning a
// ServiceBuilder with one more Stack entry; identityMethods return the
// builder or its layer unchanged. Any other method (service, service_fn,
// one not listed) leaves the stack unnamed (DAD).
var (
	serviceBuilderMethods = map[string]bool{
		"layer": true, "option_layer": true, "layer_fn": true, "buffer": true, "concurrency_limit": true,
		"load_shed": true, "rate_limit": true, "retry": true, "timeout": true, "filter": true,
		"filter_async": true, "map_request": true, "map_response": true, "map_err": true,
		"map_future": true, "then": true, "and_then": true, "map_result": true, "boxed": true,
		"boxed_clone": true,
		// tower-http ServiceBuilderExt.
		"add_extension": true, "append_request_header": true, "append_response_header": true,
		"append_trailing_slash": true, "catch_panic": true, "compression": true, "decompression": true,
		"follow_redirects": true, "insert_request_header_if_not_present": true,
		"insert_response_header_if_not_present": true, "map_request_body": true, "map_response_body": true,
		"override_request_header": true, "override_response_header": true, "propagate_header": true,
		"propagate_request_id": true, "propagate_x_request_id": true, "request_body_limit": true,
		"sensitive_headers": true, "sensitive_request_headers": true, "sensitive_response_headers": true,
		"set_request_id": true, "set_x_request_id": true, "trace_for_grpc": true, "trace_for_http": true,
		"trim_trailing_slash": true,
	}
	identityMethods = map[string]bool{"into_inner": true, "check_clone": true, "check_service_clone": true, "check_service": true}
)

// stack is row DA-X4: a layer argument that is a method chain on
// `ServiceBuilder::new()` (tower's, resolved by the shared Rust resolver)
// names each layer of the stack in written order, which tower applies
// outermost first (`Stack::new(layer, self.layer)`): a request passes the
// first written layer first. `.layer(e)` entries take DA-X2's names. nil
// for any other argument or chain.
func (r *axumLinker) stack(file string, e facts.RustExpr) []map[string]any {
	var chain []facts.RustExpr
	for e.Kind == "method" && e.Object != nil {
		chain = append(chain, e)
		e = *e.Object
	}
	if e.Kind != "call" || len(e.Args) != 0 || len(chain) == 0 {
		return nil
	}
	b := r.modules.Resolve(file, e.Scope, e.Name)
	if b.Ambiguous || b.External != "tower" || (b.Name != "ServiceBuilder::new" && b.Name != "builder::ServiceBuilder::new") {
		return nil
	}
	out := []map[string]any{}
	for i := len(chain) - 1; i >= 0; i-- {
		m := chain[i]
		switch {
		case identityMethods[m.Name]:
			continue
		case !serviceBuilderMethods[m.Name]:
			tagAuth("DAD/stack-method")
			return nil
		}
		var entry map[string]any
		if (m.Name == "layer" || m.Name == "option_layer") && len(m.Args) == 1 {
			entry = map[string]any{"expression": layerText(m.Args[0]), "method": m.Name}
			for k, v := range r.classify(file, m.Args[0]) {
				entry[k] = v
			}
		} else {
			args := make([]string, len(m.Args))
			for j, a := range m.Args {
				args[j] = layerText(a)
			}
			entry = map[string]any{"expression": m.Name + "(" + strings.Join(args, ", ") + ")", "method": m.Name}
		}
		entry["span"] = m.Span
		out = append(out, entry)
	}
	tagAuth("DA-X4/stack")
	return out
}
