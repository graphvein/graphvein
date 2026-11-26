package template

import "github.com/graphvein/graphvein/internal/contract"

// Map moves guest locations to an unescaped host literal without changing text.
func (f *Facts) Map(contentStart contract.Span) {
	move := func(s contract.Span) contract.Span {
		if s.StartLine == 1 {
			s.StartColumn += contentStart.StartColumn
		}
		if s.EndLine == 1 {
			s.EndColumn += contentStart.StartColumn
		}
		s.StartLine += contentStart.StartLine - 1
		s.EndLine += contentStart.StartLine - 1
		return s
	}
	for i := range f.ErrorSpans {
		f.ErrorSpans[i] = move(f.ErrorSpans[i])
	}
	for i := range f.Elements {
		f.Elements[i].Span = move(f.Elements[i].Span)
	}
	for i := range f.Attributes {
		a := &f.Attributes[i]
		a.Span = move(a.Span)
		a.NameSpan = move(a.NameSpan)
		if a.ValueSpan != nil {
			s := move(*a.ValueSpan)
			a.ValueSpan = &s
		}
	}
	for i := range f.Blocks {
		b := &f.Blocks[i]
		b.Span = move(b.Span)
		for j := range b.Parameters {
			b.Parameters[j].Span = move(b.Parameters[j].Span)
		}
	}
	for i := range f.Pipes {
		f.Pipes[i].Span = move(f.Pipes[i].Span)
	}
}
