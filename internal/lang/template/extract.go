package template

import (
	"bytes"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/parse"
)

// Extract records syntax on normalized source, never emitting graph records.
func Extract(source []byte, root *parse.Node) Facts {
	f := Facts{ErrorSpans: append([]contract.Span{}, root.ErrorSpans...), Elements: []Element{}, Attributes: []Attribute{}, Blocks: []Block{}, Pipes: []Pipe{}}
	location := func(a, b int) Location {
		s := sourceSpan(source, a, b)
		v := Location{Span: s}
		for _, e := range f.ErrorSpans {
			if intersects(s, e) {
				v.Flagged = true
			}
		}
		return v
	}
	// Scan only expression-bearing ranges. In particular static values, dropped
	// elements and non-bindable descendants cannot invent uses of pipes.
	pipes := func(a, b int, track bool) {
		for _, p := range pipeNames(source, a, b) {
			f.Pipes = append(f.Pipes, Pipe{Location: location(p[0], p[1]), Name: string(source[p[0]:p[1]]), Track: track})
		}
	}
	var walk func(*parse.Node, int, int, bool)
	walk = func(n *parse.Node, parentElement, parentBlock int, nonBindable bool) {
		if n == nil {
			return
		}
		switch n.Kind {
		case "element", "script_element", "style_element":
			var tag *parse.Node
			for _, c := range n.Children {
				if c.Kind == "start_tag" || c.Kind == "self_closing_tag" {
					tag = c
					break
				}
			}
			if tag == nil {
				return
			}
			e := Element{Location: location(int(n.Start), int(n.End)), Parent: parentElement, NonBindable: nonBindable}
			for _, c := range tag.Children {
				if c.Kind == "tag_name" {
					e.Name = c.Text
				}
			}
			i := len(f.Elements)
			f.Elements = append(f.Elements, e)
			structural := false
			ownNonBindable := false
			for _, c := range tag.Children {
				if c.Kind != "attribute" {
					continue
				}
				a := captureAttribute(source, c)
				a.Location = location(int(c.Start), int(c.End))
				a.Element = i
				if nonBindable {
					a.Kind = "static"
					a.Name = a.NormalizedName
				}
				if a.NormalizedName == "ngNonBindable" {
					ownNonBindable = true
				}
				if a.Kind == "structural" {
					a.ExtraStructural = structural
					structural = true
				}
				a.InvalidVariable = a.Kind == "variable" && e.Name != "ng-template"
				if e.Name == "ng-content" && a.NormalizedName == "select" {
					f.Elements[i].Select = a.Value
				}
				if e.Name == "link" && a.NormalizedName == "rel" && strings.EqualFold(a.Value, "stylesheet") {
					f.Elements[i].Dropped = true
				}
				f.Attributes = append(f.Attributes, a)
			}
			f.Elements[i].Dropped = f.Elements[i].Dropped || e.Name == "script" || e.Name == "style"
			f.Elements[i].NonBindable = nonBindable || ownNonBindable
			if !f.Elements[i].Dropped && !nonBindable {
				for _, a := range f.Attributes {
					if a.Element != i || a.ValueSpan == nil {
						continue
					}
					start, end := offsets(source, *a.ValueSpan)
					switch a.Kind {
					case "property", "two_way", "event", "animation", "structural":
						pipes(start, end, false)
					case "interpolated":
						for _, r := range interpolations(source, start, end) {
							pipes(r[0], r[1], false)
						}
					}
				}
			}
			if f.Elements[i].Dropped {
				return
			}
			for _, c := range n.Children {
				if c != tag && c.Kind != "end_tag" {
					walk(c, i, parentBlock, nonBindable || ownNonBindable)
				}
			}
			return
		case "interpolation":
			if !nonBindable {
				for _, r := range interpolations(source, int(n.Start), int(n.End)) {
					pipes(r[0], r[1], false)
				}
			}
			return
		}
		if strings.HasSuffix(n.Kind, "_statement") && !nonBindable {
			var keywords []string
			headerStart := int(n.Start) + 1
			for _, c := range n.Children {
				if c.Kind == "control_keyword" {
					keywords = append(keywords, c.Text)
					headerStart = int(c.End)
				}
			}
			if len(keywords) > 0 {
				name := strings.Join(keywords, " ")
				b := Block{Location: location(int(n.Start), int(n.End)), Name: name, RawName: string(source[int(n.Start)+1 : headerStart]), Parameters: []Parameter{}, Parent: parentBlock, Element: parentElement}
				// The header ends at its first body. Raw slices retain syntax such as
				// @default never even where the pinned grammar recovers with ERROR.
				start := headerStart
				end := int(n.End)
				for _, c := range n.Children {
					if c.Kind == "statement_block" || c.Kind == "switch_body" {
						end = int(c.Start)
						break
					}
				}
				if name == "let" && end > start && source[end-1] == ';' {
					end--
				}
				start, end = trim(source, start, end)
				if start < end && source[start] == '(' && source[end-1] == ')' {
					start++
					end--
				}
				for _, r := range splitParameters(source, start, end) {
					a, z := trim(source, r[0], r[1])
					if a == z {
						continue
					}
					p := location(a, z)
					b.Parameters = append(b.Parameters, Parameter{Text: string(source[a:z]), Span: p.Span, Flagged: p.Flagged})
					pipes(a, z, name == "for" && strings.HasPrefix(string(source[a:z]), "track "))
				}
				parentBlock = len(f.Blocks)
				f.Blocks = append(f.Blocks, b)
				for _, c := range n.Children {
					if c.Kind == "statement_block" || c.Kind == "switch_body" {
						walk(c, parentElement, parentBlock, nonBindable)
					}
				}
				return
			}
		}
		for _, c := range n.Children {
			walk(c, parentElement, parentBlock, nonBindable)
		}
	}
	walk(root, -1, -1, false)
	return f
}

func captureAttribute(source []byte, n *parse.Node) Attribute {
	a, z := int(n.Start), int(n.End)
	b := a
	for b < z && source[b] != '=' && !unicode.IsSpace(rune(source[b])) {
		b++
	}
	raw := string(source[a:b])
	normalized := raw
	if len(normalized) >= 5 && strings.EqualFold(normalized[:5], "data-") {
		normalized = normalized[5:]
	}
	out := Attribute{RawName: raw, NormalizedName: normalized, Name: normalized, Kind: "static", NameSpan: sourceSpan(source, a, b)}
	v := b
	for v < z && source[v] != '=' {
		v++
	}
	if v < z {
		v++
		v, z = trim(source, v, z)
		if v < z && (source[v] == '\'' || source[v] == '"') && source[z-1] == source[v] {
			v++
			z--
		}
		out.Value = string(source[v:z])
		s := sourceSpan(source, v, z)
		out.ValueSpan = &s
	}
	out.I18n = normalized == "i18n" || strings.HasPrefix(normalized, "i18n-")
	if strings.HasPrefix(normalized, "*") {
		out.Kind = "structural"
		out.Name = normalized[1:]
		return out
	}
	for _, p := range []struct{ prefix, kind string }{{"bind-", "property"}, {"let-", "variable"}, {"ref-", "reference"}, {"#", "reference"}, {"on-", "event"}, {"bindon-", "two_way"}, {"@", "animation"}} {
		if strings.HasPrefix(normalized, p.prefix) {
			out.Kind = p.kind
			out.Name = strings.TrimPrefix(normalized, p.prefix)
			return out
		}
	}
	for _, p := range []struct{ open, close, kind string }{{"[(", ")]", "two_way"}, {"[", "]", "property"}, {"(", ")", "event"}} {
		if strings.HasPrefix(normalized, p.open) && strings.HasSuffix(normalized, p.close) && len(normalized) > len(p.open)+len(p.close) {
			out.Kind = p.kind
			out.Name = normalized[len(p.open) : len(normalized)-len(p.close)]
			return out
		}
	}
	if len(interpolations([]byte(out.Value), 0, len(out.Value))) > 0 {
		out.Kind = "interpolated"
	}
	return out
}

func sourceSpan(source []byte, a, b int) contract.Span {
	point := func(i int) (int, int) {
		return bytes.Count(source[:i], []byte{'\n'}) + 1, i - bytes.LastIndexByte(source[:i], '\n') - 1
	}
	l, c := point(a)
	el, ec := point(b)
	return contract.Span{StartLine: l, StartColumn: c, EndLine: el, EndColumn: ec}
}
func offsets(source []byte, s contract.Span) (int, int) {
	point := func(line, col int) int {
		i := 0
		for n := 1; n < line; n++ {
			j := bytes.IndexByte(source[i:], '\n')
			if j < 0 {
				return len(source)
			}
			i += j + 1
		}
		return i + col
	}
	return point(s.StartLine, s.StartColumn), point(s.EndLine, s.EndColumn)
}
func intersects(a, b contract.Span) bool {
	less := func(l, c, m, n int) bool { return l < m || l == m && c < n }
	if b.StartLine == b.EndLine && b.StartColumn == b.EndColumn {
		return !less(b.StartLine, b.StartColumn, a.StartLine, a.StartColumn) && less(b.StartLine, b.StartColumn, a.EndLine, a.EndColumn)
	}
	return less(a.StartLine, a.StartColumn, b.EndLine, b.EndColumn) && less(b.StartLine, b.StartColumn, a.EndLine, a.EndColumn)
}
func trim(src []byte, a, b int) (int, int) {
	for a < b && unicode.IsSpace(rune(src[a])) {
		a++
	}
	for b > a && unicode.IsSpace(rune(src[b-1])) {
		b--
	}
	return a, b
}

// expressionScan skips quoted literals and tracks nesting so strings and nested
// semicolons cannot become block parameters or pipe operators.
func expressionScan(src []byte, a, b int, visit func(int, int)) {
	depth := 0
	var quote byte
	for i := a; i < b; i++ {
		c := src[i]
		if quote != 0 {
			if c == '\\' {
				i++
			} else if c == quote {
				quote = 0
			}
			continue
		}
		if c == '\'' || c == '"' || c == '`' {
			quote = c
			continue
		}
		switch c {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		}
		visit(i, depth)
	}
}
func pipeNames(src []byte, a, b int) [][2]int {
	var out [][2]int
	expressionScan(src, a, b, func(i, depth int) {
		if src[i] != '|' || i+1 < b && src[i+1] == '|' || i > a && src[i-1] == '|' {
			return
		}
		j := i + 1
		for j < b && unicode.IsSpace(rune(src[j])) {
			j++
		}
		start := j
		for j < b {
			r, n := utf8.DecodeRune(src[j:b])
			if !(unicode.IsLetter(r) || r == '_' || r == '$' || j > start && unicode.IsDigit(r)) {
				break
			}
			j += n
		}
		if j > start {
			out = append(out, [2]int{start, j})
		}
	})
	return out
}
func splitParameters(src []byte, a, b int) [][2]int {
	out := [][2]int{}
	start := a
	expressionScan(src, a, b, func(i, d int) {
		if src[i] == ';' && d == 0 {
			out = append(out, [2]int{start, i})
			start = i + 1
		}
	})
	return append(out, [2]int{start, b})
}
func interpolations(src []byte, a, b int) [][2]int {
	var out [][2]int
	for a < b {
		start := bytes.Index(src[a:b], []byte("{{"))
		if start < 0 {
			break
		}
		start += a + 2
		end := -1
		depth := 0
		var quote byte
		for i := start; i+1 < b; i++ {
			c := src[i]
			if quote != 0 {
				if c == '\\' {
					i++
				} else if c == quote {
					quote = 0
				}
				continue
			}
			if c == '\'' || c == '"' || c == '`' {
				quote = c
				continue
			}
			if c == '}' && src[i+1] == '}' && depth == 0 {
				end = i
				break
			}
			switch c {
			case '(', '[', '{':
				depth++
			case ')', ']', '}':
				depth--
			}
		}
		if end < start {
			break
		}
		out = append(out, [2]int{start, end})
		a = end + 2
	}
	return out
}
