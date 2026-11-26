package angular

import (
	"bytes"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/template"
)

func startBefore(a, b contract.Span) bool {
	return a.StartLine < b.StartLine || a.StartLine == b.StartLine && a.StartColumn < b.StartColumn
}

func spanEnd(s contract.Span) contract.Span {
	return contract.Span{StartLine: s.EndLine, StartColumn: s.EndColumn}
}

func containsSpan(a, b contract.Span) bool {
	return !startBefore(b, a) && !startBefore(spanEnd(a), spanEnd(b))
}

func intersectsSpan(a, b contract.Span) bool {
	if b.StartLine == b.EndLine && b.StartColumn == b.EndColumn {
		return !startBefore(b, a) && startBefore(b, spanEnd(a))
	}
	return startBefore(a, spanEnd(b)) && startBefore(b, spanEnd(a))
}

func sourceOffset(src []byte, line, col int) int {
	at := 0
	for n := 1; n < line; n++ {
		next := bytes.IndexByte(src[at:], '\n')
		if next < 0 {
			return len(src)
		}
		at += next + 1
	}
	return min(at+col, len(src))
}

// A structural recovery suppresses the remaining template. Orphan boundaries
// are already cached, so only comment/whitespace adjacency needs source bytes.
type templateRecovery struct {
	first      *contract.Span
	structural *contract.Span
	cause      string
	orphans    []contract.Span
}

func recoveryFor(f template.Facts, source []byte) templateRecovery {
	r := templateRecovery{}
	report := func(s contract.Span, cause string) {
		if r.first == nil || startBefore(s, *r.first) {
			copy := s
			r.first, r.cause = &copy, cause
		}
	}
	for _, err := range f.ErrorSpans {
		expression := false
		for _, a := range f.Attributes {
			if !f.Elements[a.Element].Dropped && a.ValueSpan != nil && containsSpan(*a.ValueSpan, err) {
				expression = true
			}
		}
		for _, b := range f.Blocks {
			for _, p := range b.Parameters {
				if containsSpan(p.Span, err) {
					expression = true
				}
			}
		}
		if !expression {
			if r.structural == nil || startBefore(err, *r.structural) {
				copy := err
				r.structural = &copy
			}
			report(err, "recovery")
		}
	}
	for i, b := range f.Blocks {
		orphan := false
		switch b.Name {
		case "case", "default":
			orphan = b.Parent < 0 || f.Blocks[b.Parent].Name != "switch"
		case "else", "else if", "empty", "placeholder", "loading", "error":
			previous := -1
			for j := i - 1; j >= 0; j-- {
				if f.Blocks[j].Parent == b.Parent && f.Blocks[j].Element == b.Element {
					previous = j
					break
				}
			}
			orphan = true
			if previous >= 0 {
				p := f.Blocks[previous]
				allowed := false
				switch b.Name {
				case "else", "else if":
					allowed = p.Name == "if" || p.Name == "else if" || p.Name == "else"
				case "empty":
					allowed = p.Name == "for"
				default:
					allowed = p.Name == "defer" || p.Name == "placeholder" || p.Name == "loading" || p.Name == "error"
				}
				if allowed && !startBefore(b.Span, spanEnd(p.Span)) {
					a := sourceOffset(source, p.Span.EndLine, p.Span.EndColumn)
					z := sourceOffset(source, b.Span.StartLine, b.Span.StartColumn)
					orphan = !onlyCommentsAndWhitespace(string(source[a:z]))
				}
			}
		}
		if orphan {
			r.orphans = append(r.orphans, b.Span)
			report(b.Span, "orphan block "+b.Name)
		}
	}
	return r
}

func onlyCommentsAndWhitespace(s string) bool {
	for {
		s = strings.TrimFunc(s, angularWhitespace)
		if s == "" {
			return true
		}
		if !strings.HasPrefix(s, "<!--") {
			return false
		}
		i := strings.Index(s[4:], "-->")
		if i < 0 {
			return false
		}
		s = s[4+i+3:]
	}
}

func (r templateRecovery) unknown(s contract.Span) bool {
	if r.structural != nil && !startBefore(s, *r.structural) {
		return true
	}
	for _, orphan := range r.orphans {
		if containsSpan(orphan, s) {
			return true
		}
	}
	return false
}
