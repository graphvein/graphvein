package clients

import (
	"net/url"
	"strconv"
	"strings"
)

func (ev *evaluation) urlObject(c evaluationContext, e expr) valueSet {
	dynamic := func() valueSet { return valueSet{unknownValue(e)} }
	if e.Invalid || ev.urlAliases > 1 || ev.depth > 0 || len(e.Args) < 1 || len(e.Args) > 2 {
		return dynamic()
	}
	at, err := strconv.Atoi(e.Operator)
	if err != nil {
		return dynamic()
	}
	c.scope = at
	input := ev.expression(c, e.Args[0])
	if len(input) != 1 || input[0].kind != "string" || len(input[0].pieces) == 0 {
		return dynamic()
	}
	v := input[0]
	v.pieces = nil
	for _, p := range input[0].pieces {
		if p.literal && len(v.pieces) > 0 && v.pieces[len(v.pieces)-1].literal {
			v.pieces[len(v.pieces)-1].text += p.text
		} else {
			v.pieces = append(v.pieces, p)
		}
	}
	if !v.pieces[0].literal {
		return dynamic()
	}
	u := ev.r.template(c.file, c.scope, v.pieces)
	if !u.valid {
		return dynamic()
	}
	leading := strings.ToLower(v.pieces[0].text)
	abs := strings.HasPrefix(leading, "http://") || strings.HasPrefix(leading, "https://")
	base := ""
	if abs {
		origin, err := url.Parse(u.origin)
		if err != nil || origin.Hostname() == "" {
			return dynamic()
		}
	} else {
		if !strings.HasPrefix(v.pieces[0].text, "/") || strings.HasPrefix(v.pieces[0].text, "//") {
			return dynamic()
		}
		origin := ""
		if len(e.Args) == 2 {
			base := ev.expression(c, e.Args[1])
			if len(base) == 1 && base[0].kind == "string" && base[0].known() {
				b, err := url.Parse(base[0].text)
				if err == nil && (strings.EqualFold(b.Scheme, "http") || strings.EqualFold(b.Scheme, "https")) && b.Hostname() != "" {
					origin = b.Scheme + "://" + b.Host
				}
			}
		}
		base = ev.urlObjectBase(c, e, origin != "")
		if origin == "" && !strings.Contains(e.Name, "host ") && base == "" {
			return dynamic()
		}
		if origin != "" {
			v.pieces = append([]piece{{literal: true, text: origin}}, v.pieces...)
		}
	}
	if !urlObjectPath(v.pieces) {
		return dynamic()
	}
	if allLiteral(v.pieces) {
		v.text = ""
		for _, p := range v.pieces {
			v.text += p.text
		}
	}
	if abs {
		base = ev.urlObjectBase(c, e, true)
	}
	if e.Name != "" || base != "" {
		v.originResolution = "unresolved"
		v.originBase = base
	}
	ev.evaluated = true
	return valueSet{v}
}

// urlObjectBase returns the base expression of a URL object's unknown
// origin: the one base-shaped expression (an environment member, a module
// const bound to one, or a build-time constant) that supplies every
// authority part the object does not take from a literal. Its sources are
// each authority assignment (the E of `const A = new URL(E)` read as
// A.<same property>, proven by the syntax cache) and, unless protocol and
// host are both assigned, the origin the object was built with: a literal
// origin (literal true) is not E, and for an absolute-path input it is
// the second argument. "" means no such single expression.
func (ev *evaluation) urlObjectBase(c evaluationContext, e expr, literal bool) string {
	assigned := map[string]bool{}
	var sources []string
	for _, k := range e.Keywords {
		assigned[k.Name] = true
		sources = append(sources, ev.baseSpelling(c, k.Value))
	}
	if !assigned["protocol"] || !assigned["host"] {
		switch {
		case literal:
			if len(sources) > 0 {
				sources = append(sources, "")
			}
		case len(e.Args) == 2:
			sources = append(sources, ev.baseSpelling(c, e.Args[1]))
		default:
			sources = append(sources, "")
		}
	}
	for _, s := range sources {
		if s == "" || s != sources[0] {
			return ""
		}
	}
	if len(sources) == 0 {
		return ""
	}
	return sources[0]
}

// baseSpelling returns the spelling of e when it is base-shaped in c: an
// environment member, a module const bound to an environment base, or an
// unbound build-time constant; otherwise "".
func (ev *evaluation) baseSpelling(c evaluationContext, e expr) string {
	if envMember(e) {
		return spelling(e)
	}
	if e.Kind != "identifier" {
		return ""
	}
	if _, _, ok := c.env.get(e.Name); ok {
		return ""
	}
	if _, at, ok, _ := ev.binding(c, e.Name); ok {
		if at == 0 && ev.r.baseShaped(c.file, 0, e) {
			return e.Name
		}
		return ""
	}
	if ev.r.ambientConstant(e.Name) {
		return e.Name
	}
	return ""
}

// spelling rebuilds an identifier or member chain from cached syntax,
// whose source text may be compacted away.
func spelling(e expr) string {
	switch e.Kind {
	case "identifier":
		return e.Name
	case "meta_property":
		return compact(e.Text)
	case "member_expression":
		if e.Object == nil || e.Name == "" {
			return ""
		}
		if object := spelling(*e.Object); object != "" {
			return object + "." + e.Name
		}
	}
	return ""
}

func urlObjectPath(pieces []piece) bool {
	var path strings.Builder
	for _, p := range pieces {
		if p.literal {
			path.WriteString(p.text)
		} else {
			path.WriteString("{}")
		}
	}
	s := path.String()
	if start := strings.Index(s, "://"); start >= 0 {
		at := strings.IndexAny(s[start+3:], "/?#")
		if at < 0 {
			s = "/"
		} else {
			s = s[start+3+at:]
		}
	}
	if stop := strings.IndexAny(s, "?#"); stop >= 0 {
		s = s[:stop]
	}
	for i := 0; i < len(s); i++ {
		if s[i] <= 32 || s[i] >= 127 || s[i] == '\\' {
			return false
		}
		if s[i] == '%' {
			if i+2 >= len(s) || !hexByte(s[i+1]) || !hexByte(s[i+2]) {
				return false
			}
			i += 2
		}
	}
	for _, segment := range strings.Split(s, "/") {
		// WHATWG also removes percent-encoded dot segments.
		dot := strings.ReplaceAll(strings.ReplaceAll(segment, "%2e", "."), "%2E", ".")
		if dot == "." || dot == ".." {
			return false
		}
	}
	return true
}
func hexByte(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}
