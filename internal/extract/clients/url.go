package clients

import (
	"regexp"
	"strings"
)

type piece struct {
	literal    bool
	text       string
	expression expr
}

func (r *resolver) pieces(file string, scope int, e expr) []piece {
	if text, ok := r.literal(file, scope, e, nil); ok {
		return []piece{{literal: true, text: text, expression: e}}
	}
	if e.Kind == "template_string" || e.Kind == "binary_expression" && e.Operator == "+" {
		var out []piece
		for _, p := range e.Parts {
			out = append(out, r.pieces(file, scope, p)...)
		}
		return out
	}
	return []piece{{expression: e}}
}

var absolute = regexp.MustCompile(`(?i)^([a-z][a-z\d+\-.]*:)?//`)

func normalize(s string) string {
	return "/" + strings.Join(strings.FieldsFunc(s, func(c rune) bool { return c == '/' }), "/")
}
func dynamicName(e expr) string {
	if e.Kind == "identifier" || e.Kind == "member_expression" || e.Kind == "orval_parameter" {
		return e.Name
	}
	return ""
}

type urlResult struct {
	path, origin, base, expression string
	valid                          bool
	slash                          bool
}

func (r *resolver) template(file string, scope int, pieces []piece) urlResult {
	u := urlResult{base: "none", valid: true}
	// Coalesce adjacent literals before origin/query processing.
	var parts []piece
	for _, p := range pieces {
		if p.literal && len(parts) > 0 && parts[len(parts)-1].literal {
			parts[len(parts)-1].text += p.text
		} else {
			parts = append(parts, p)
		}
	}
	if len(parts) == 0 {
		u.valid = false
		return u
	}
	if !parts[0].literal {
		// A runtime base comes from a proven helper (row UP-39,
		// runtime_base.go); its expression is the base's spelling.
		if len(parts) > 1 && parts[1].literal && strings.HasPrefix(parts[1].text, "/") && (parts[0].expression.Kind == runtimeBaseKind || r.baseShaped(file, scope, parts[0].expression)) {
			u.base = "unresolved"
			u.expression = compact(parts[0].expression.Text)
			parts = parts[1:]
		} else {
			u.valid = false
			return u
		}
	}
	if len(parts) > 0 && parts[0].literal && absolute.MatchString(parts[0].text) {
		text := parts[0].text
		start := len(absolute.FindString(text))
		end := strings.IndexAny(text[start:], "/?#")
		if end < 0 {
			end = len(text)
		} else {
			end += start
		}
		// No path delimiter before the next dynamic piece means that piece
		// belongs to the authority, including a host following the scheme's //.
		if end == len(text) && len(parts) > 1 {
			u.valid = false
			u.base = "unresolved"
			return u
		}
		u.origin = text[:end]
		parts[0].text = text[end:]
		// A dynamic authority cannot be separated into a proven origin.
		if end == len(text) && len(parts) > 1 && !strings.HasSuffix(text, "/") {
			u.valid = false
			return u
		}
	}
	if u.origin == "" && u.base == "none" && !strings.HasPrefix(parts[0].text, "/") {
		u.base = "unresolved"
		u.expression = "page-relative"
	}
	var segments []string
	segment := ""
	dynamic := false
	name := ""
	flush := func() {
		if segment != "" || dynamic {
			if dynamic {
				segments = append(segments, "{"+name+"}")
			} else {
				segments = append(segments, segment)
			}
		}
		segment = ""
		dynamic = false
		name = ""
	}
	stop := false
	for _, p := range parts {
		if p.literal {
			for _, c := range p.text {
				if c == '?' || c == '#' {
					stop = true
					break
				}
				if c == '/' {
					u.slash = true
					flush()
				} else {
					u.slash = false
					segment += string(c)
				}
			}
		} else {
			u.slash = false
			dynamic = true
			if name == "" {
				name = dynamicName(p.expression)
			}
		}
		if stop {
			break
		}
	}
	flush()
	u.path = "/" + strings.Join(segments, "/")
	u.slash = u.slash && u.path != "/"
	return u
}

func (r *resolver) defaultsBase(key string) (string, string, string, bool) {
	values, present := r.defaults[key]
	if !present {
		return "", "", "", false
	}
	result := ""
	kind := "literal"
	for i, e := range values {
		if e.kind == "unresolved" || i > 0 && e.value != result {
			return "", "unresolved", "defaults.baseURL", true
		}
		result = e.value
		if e.kind == "constant" {
			kind = "constant"
		}
	}
	return result, kind, "", true
}

func (r *resolver) attributes(file string, scope int, q request, method string, static bool, basePaths map[string]string) map[string]any {
	attrs := map[string]any{"library": q.library, "method": nil, "origin": nil, "path_template": nil, "base": "none", "status": "dynamic", "match": nil, "candidates": []string{}, "candidate_count": 0}
	if static {
		attrs["method"] = method
	} else {
		attrs["method_expression"] = compact(q.method.Text)
	}
	pieces := r.pieces(file, scope, q.url)
	if q.library == "hey-api" && len(pieces) == 1 && pieces[0].literal && !strings.HasPrefix(pieces[0].text, "/") {
		// getUrl treats even an absolute URL spelling as a request path.
		pieces[0].text = "/" + pieces[0].text
	}
	u := r.template(file, scope, pieces)
	if q.library == "angular" && q.angular.kind != "" {
		if q.angular.opaque {
			// An opaque rewrite can replace resource segments as well as the base.
			u = urlResult{base: "unresolved"}
		} else if q.angular.kind == "unresolved" {
			u.base = "unresolved"
			u.expression = "angular interceptor"
		} else if u.origin != "" {
			u = urlResult{base: "unresolved"}
		} else if u.valid {
			// Angular's clone prefix uses concatenation, not axios slash trimming.
			u = r.template(file, scope, append([]piece{{literal: true, text: q.angular.text}}, r.pieces(file, scope, q.url)...))
			u.base = q.angular.kind
		}
	}
	slash := u.slash
	baseText, baseKind, baseExpression := "", "none", ""
	if q.library == "hey-api" {
		base := r.heyClients[q.object.key].base
		if b, ok := property(q.config, "baseURL"); ok {
			base = r.heyBase(file, scope, b)
		}
		if u.valid {
			if base.kind == "unresolved" {
				if u.origin != "" {
					u = urlResult{base: "unresolved"}
				} else {
					u.base, u.expression = "unresolved", base.expression
				}
			} else if base.kind != "none" {
				// The bundled client prepends '/' to relative URL text and
				// concatenates the base; it does not delegate joining to axios.
				if base.value != "" {
					u = r.template(file, scope, []piece{{literal: true, text: base.value + pieces[0].text}})
					u.base = base.kind
					slash = u.slash
				} else {
					u.base = base.kind
				}
			}
		}
	}
	if q.library == "axios" {
		if b, ok := property(q.config, "baseURL"); ok {
			baseText, ok = r.literal(file, scope, b, nil)
			baseExpression = compact(b.Text)
			baseKind = "unresolved"
			if ok {
				baseKind = "literal"
				if b.Kind != "literal" {
					baseKind = "constant"
				}
			}
		} else if q.object.base != nil {
			b := *q.object.base
			var ok bool
			baseText, ok = r.literal(q.object.file, q.object.scope, b, nil)
			baseExpression = compact(b.Text)
			baseKind = "unresolved"
			if ok {
				baseKind = "literal"
				if b.Kind != "literal" {
					baseKind = "constant"
				}
			}
		} else {
			key := objectKey(q.object)
			text, kind, expression, present := r.defaultsBase(key)
			if !present && q.object.kind == "axios" {
				text, kind, expression, present = r.defaultsBase("axios:default")
			}
			if present {
				baseText = text
				baseKind = kind
				baseExpression = expression
			}
		}
	}
	// Absolute URLs ignore axios baseURL, including unresolved values.
	if u.origin != "" {
		baseKind = "none"
	}
	if u.valid && baseKind != "none" && baseText != "" {
		joined := strings.TrimRight(baseText, "/") + "/" + strings.TrimLeft(u.path, "/")
		u = r.template(file, scope, []piece{{literal: true, text: joined}})
		u.base = baseKind
	} else if u.valid && baseKind == "unresolved" {
		u.base = "unresolved"
		u.expression = baseExpression
	}
	if !u.valid && (baseText != "" || baseKind == "unresolved") {
		u.base = baseKind
		u.expression = baseExpression
	}
	if u.base == "unresolved" && u.valid {
		best := ""
		bestLength := -1
		found := false
		for dir := range basePaths {
			length := len(dir)
			if dir == "." {
				length = 0 // The repository root has no directory components.
			}
			if (dir == "." || strings.HasPrefix(file, dir+"/")) && length > bestLength {
				best = dir
				bestLength = length
				found = true
			}
		}
		if found {
			if u.valid {
				u.path = normalize(basePaths[best] + "/" + u.path)
			}
			u.base = "configured"
			u.expression = ""
		}
	}
	attrs["base"] = u.base
	if u.base == "unresolved" {
		attrs["base_expression"] = u.expression
	}
	if u.origin != "" {
		attrs["origin"] = u.origin
	}
	if u.valid {
		attrs["path_template"] = u.path
		if slash && u.path != "/" {
			attrs["trailing_slash"] = true
		}
	} else {
		attrs["base"] = "unresolved"
	}
	return attrs
}
