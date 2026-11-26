package prisma

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Parse reads one .prisma file. It never fails: what Prisma's parser would
// reject is a ParseError of the file, and the parser goes on at the next
// line so the rest of the file is still read (for diagnostics; a schema
// with a parse error yields no claim).
func Parse(path string, src []byte) *File {
	f := &File{Path: path}
	if !utf8.Valid(src) {
		f.Errors = append(f.Errors, ParseError{Line: 1, Col: 1, Message: "the file is not valid UTF-8"})
		return f
	}
	p := &parser{lx: newLexer(string(src)), file: f}
	p.parseFile()
	return f
}

type parser struct {
	lx   *lexer
	buf  []token
	file *File
	// last is the end of the last consumed token, for adjacency checks.
	last token
}

func (p *parser) peekN(i int) token {
	for len(p.buf) <= i {
		p.buf = append(p.buf, p.lx.next())
	}
	return p.buf[i]
}

func (p *parser) peek() token { return p.peekN(0) }

func (p *parser) take() token {
	t := p.peek()
	p.buf = p.buf[1:]
	p.last = t
	return t
}

func (p *parser) isPunct(t token, s string) bool { return t.kind == tokPunct && t.text == s }

func (p *parser) errorf(t token, format string, args ...any) {
	p.file.Errors = append(p.file.Errors, ParseError{Line: t.line, Col: t.col, Message: fmt.Sprintf(format, args...)})
}

// skipRestOfLine drops the rest of the current line, newline included:
// the tokens already looked ahead up to a newline, else the source up to
// the next newline.
func (p *parser) skipRestOfLine() {
	for len(p.buf) > 0 {
		t := p.buf[0]
		p.buf = p.buf[1:]
		if t.kind == tokNewline || t.kind == tokEOF {
			if t.kind == tokEOF {
				p.buf = append(p.buf[:0], t)
			}
			return
		}
	}
	p.lx.skipLine()
}

// takeBlockOpen consumes `{` and, like Prisma's BLOCK_OPEN rule, the rest
// of its line. The caller has checked that `{` is the next token and that
// nothing after it is buffered.
func (p *parser) takeBlockOpen() {
	open := p.take()
	if len(p.buf) != 0 {
		// Never happens: the block rules look ahead at most to the brace.
		p.buf = p.buf[:0]
	}
	if rest := strings.TrimSpace(p.lx.skipLine()); rest != "" && !strings.HasPrefix(rest, "//") {
		// Prisma ignores this text silently, so the line's fields are lost
		// to it; reported so the block is never read as complete.
		p.errorf(open, "text after `{` on the block's first line is ignored by Prisma: %q", rest)
	}
}

var (
	identRe  = regexp.MustCompile(`^[\p{L}0-9][\p{L}0-9_-]*$`)
	numberRe = regexp.MustCompile(`^-?[0-9]+(\.[0-9]+)?$`)
)

func isIdent(s string) bool { return identRe.MatchString(s) }

// isPath reports whether s is a Prisma path: an identifier followed by
// dot-separated parts, each an identifier or empty (`path = identifier ~
// ("." ~ path?)*`).
func isPath(s string) bool {
	parts := strings.Split(s, ".")
	if !isIdent(parts[0]) {
		return false
	}
	for _, part := range parts[1:] {
		if part != "" && !isIdent(part) {
			return false
		}
	}
	return true
}

func (p *parser) parseFile() {
	for {
		t := p.peek()
		switch {
		case t.kind == tokEOF:
			return
		case t.kind == tokNewline || t.kind == tokComment:
			p.take()
		case t.kind == tokWord && isIdent(t.text):
			p.parseTopLevel(t)
		default:
			p.errorf(t, "this line is invalid: it does not start with a Prisma schema keyword")
			p.skipRestOfLine()
		}
	}
}

// parseTopLevel reads a block that starts with the word t, in the order
// of Prisma's schema rule: model/type/view, enum, datasource/generator,
// type alias, arbitrary block, else an invalid line.
func (p *parser) parseTopLevel(t token) {
	name := p.peekN(1)
	if p.isPunct(name, "{") {
		// arbitrary_block: refused by Prisma; it runs to the next `}`.
		// Nothing after the brace has been looked ahead.
		p.take()
		p.take()
		b := &Block{Name: t.text, StartLine: t.line}
		p.lx.skipUntil('}')
		b.EndLine = p.lx.line
		p.file.Blocks = append(p.file.Blocks, b)
		p.errorf(t, "block %s does not start with a Prisma schema keyword", t.text)
		return
	}
	named := name.kind == tokWord && isIdent(name.text)
	var brace token
	if named {
		brace = p.peekN(2)
	}
	switch {
	case named && p.isPunct(brace, "{") && (t.text == KeywordModel || t.text == KeywordView || t.text == KeywordType):
		p.take()
		p.take()
		p.takeBlockOpen()
		p.parseModelBody(&Block{Keyword: t.text, Name: name.text, StartLine: t.line})
	case named && p.isPunct(brace, "{") && t.text == KeywordEnum:
		p.take()
		p.take()
		p.takeBlockOpen()
		p.parseEnumBody(&Block{Keyword: t.text, Name: name.text, StartLine: t.line})
	case named && p.isPunct(brace, "{") && (t.text == KeywordDatasource || t.text == KeywordGenerator):
		p.take()
		p.take()
		p.takeBlockOpen()
		p.parseConfigBody(&Block{Keyword: t.text, Name: name.text, StartLine: t.line})
	case named && p.isPunct(brace, "=") && t.text == KeywordType:
		// type_alias: refused by Prisma ("Invalid type definition").
		p.file.Blocks = append(p.file.Blocks, &Block{Keyword: KeywordTypeAlias, Name: name.text, StartLine: t.line, EndLine: t.line})
		p.errorf(t, "type alias %s is not a valid Prisma type definition", name.text)
		p.skipRestOfLine()
	default:
		p.errorf(t, "this line is invalid: it does not start with a Prisma schema keyword")
		p.skipRestOfLine()
	}
}

// endBlock consumes the closing brace and records the block.
func (p *parser) endBlock(b *Block) {
	t := p.take()
	b.EndLine = t.line
	p.file.Blocks = append(p.file.Blocks, b)
}

// unclosed records a block that reached the end of the file.
func (p *parser) unclosed(b *Block) {
	b.EndLine = p.lx.line
	p.file.Blocks = append(p.file.Blocks, b)
	p.errorf(p.peek(), "block %s %s is not closed", b.Keyword, b.Name)
}

// endOfLine accepts an optional trailing comment and the newline (or the
// end of the file, where the block's own error is reported); anything
// else is an invalid line.
func (p *parser) endOfLine(what string) bool {
	t := p.peek()
	if t.kind == tokComment {
		p.take()
		t = p.peek()
	}
	switch t.kind {
	case tokNewline:
		p.take()
		return true
	case tokEOF:
		return true
	}
	p.errorf(t, "%s: unexpected %q", what, t.text)
	p.skipRestOfLine()
	return false
}

func (p *parser) parseModelBody(b *Block) {
	for {
		t := p.peek()
		switch {
		case t.kind == tokEOF:
			p.unclosed(b)
			return
		case t.kind == tokNewline || t.kind == tokComment:
			p.take()
		case p.isPunct(t, "}"):
			p.endBlock(b)
			return
		case p.isPunct(t, "@@"):
			if a := p.parseAttribute(); a != nil && p.endOfLine("block attribute") {
				b.Attributes = append(b.Attributes, a)
			}
		case t.kind == tokWord && isIdent(t.text):
			if f := p.parseField(); f != nil && p.endOfLine("field "+f.Name) {
				b.Fields = append(b.Fields, f)
			}
		default:
			p.errorf(t, "this line is not a valid field or attribute definition")
			p.skipRestOfLine()
		}
	}
}

func (p *parser) parseEnumBody(b *Block) {
	for {
		t := p.peek()
		switch {
		case t.kind == tokEOF:
			p.unclosed(b)
			return
		case t.kind == tokNewline || t.kind == tokComment:
			p.take()
		case p.isPunct(t, "}"):
			p.endBlock(b)
			return
		case p.isPunct(t, "@@"):
			if a := p.parseAttribute(); a != nil && p.endOfLine("block attribute") {
				b.Attributes = append(b.Attributes, a)
			}
		case t.kind == tokWord && isIdent(t.text):
			p.take()
			v := &EnumValue{Name: t.text, Line: t.line, Col: t.col}
			ok := true
			for ok && p.isPunct(p.peek(), "@") {
				a := p.parseAttribute()
				if a == nil {
					ok = false
					break
				}
				v.Attributes = append(v.Attributes, a)
			}
			if ok && p.endOfLine("enum value "+v.Name) {
				b.Values = append(b.Values, v)
			}
		default:
			p.errorf(t, "this line is not a valid enum value or attribute definition")
			p.skipRestOfLine()
		}
	}
}

func (p *parser) parseConfigBody(b *Block) {
	for {
		t := p.peek()
		switch {
		case t.kind == tokEOF:
			p.unclosed(b)
			return
		case t.kind == tokNewline || t.kind == tokComment:
			p.take()
		case p.isPunct(t, "}"):
			p.endBlock(b)
			return
		case t.kind == tokWord && isIdent(t.text) && p.isPunct(p.peekN(1), "="):
			p.take()
			p.take()
			prop := &Property{Key: t.text, Line: t.line, Col: t.col}
			if n := p.peek(); n.kind != tokNewline && n.kind != tokComment && n.kind != tokEOF {
				prop.Value = p.parseExpr()
				if prop.Value == nil {
					continue
				}
			}
			if p.endOfLine("property " + prop.Key) {
				b.Properties = append(b.Properties, prop)
			}
		default:
			p.errorf(t, "this line is not a valid key = value definition")
			p.skipRestOfLine()
		}
	}
}

// parseField reads `name [:] [type] @attr...` up to the trailing comment.
// It returns nil after reporting an error and skipping the line.
func (p *parser) parseField() *Field {
	t := p.take()
	f := &Field{Name: t.text, Line: t.line, Col: t.col}
	if p.isPunct(p.peek(), ":") {
		p.take()
		f.LegacyColon = true
	}
	if n := p.peek(); n.kind == tokWord || p.isPunct(n, "[") {
		ft := p.parseFieldType()
		if ft == nil {
			return nil
		}
		f.Type = ft
	}
	for p.isPunct(p.peek(), "@") {
		a := p.parseAttribute()
		if a == nil {
			return nil
		}
		f.Attributes = append(f.Attributes, a)
	}
	return f
}

func (p *parser) parseFieldType() *FieldType {
	start := p.peek()
	ft := &FieldType{Line: start.line, Col: start.col}
	legacyList := false
	if p.isPunct(start, "[") {
		p.take()
		legacyList = true
	}
	base := p.peek()
	switch {
	case base.kind == tokWord && base.text == "Unsupported" && p.isPunct(p.peekN(1), "(") && p.peekN(1).off == base.end:
		p.take()
		p.take()
		s := p.peek()
		if s.kind != tokString {
			p.errorf(s, "Unsupported takes one string literal")
			p.skipRestOfLine()
			return nil
		}
		e := p.parseExpr()
		if e == nil {
			return nil
		}
		if !p.isPunct(p.peek(), ")") {
			p.errorf(p.peek(), "Unsupported takes one string literal")
			p.skipRestOfLine()
			return nil
		}
		p.take()
		ft.Name, ft.Unsupported = "Unsupported", e
	case base.kind == tokWord && isIdent(base.text):
		p.take()
		ft.Name = base.text
	default:
		p.errorf(base, "invalid field type %q", base.text)
		p.skipRestOfLine()
		return nil
	}
	if legacyList {
		if !p.isPunct(p.peek(), "]") {
			p.errorf(p.peek(), "invalid field type: missing `]`")
			p.skipRestOfLine()
			return nil
		}
		p.take()
		ft.Legacy, ft.List = "[]", true
		return ft
	}
	n := p.peek()
	switch {
	case p.isPunct(n, "[") && p.isPunct(p.peekN(1), "]") && p.peekN(1).off == n.end:
		p.take()
		p.take()
		ft.List = true
		if p.isPunct(p.peek(), "?") {
			p.take()
			ft.Optional = true
		}
	case p.isPunct(n, "?"):
		p.take()
		ft.Optional = true
	case p.isPunct(n, "!"):
		p.take()
		ft.Legacy = "!"
	}
	return ft
}

// parseAttribute reads `@path(args)` or `@@path(args)`; the next token is
// the @ sign. It returns nil after reporting an error and skipping the
// line.
func (p *parser) parseAttribute() *Attribute {
	at := p.take()
	a := &Attribute{Block: at.text == "@@", Line: at.line, Col: at.col}
	name := p.peek()
	if name.kind != tokWord || !isPath(name.text) {
		p.errorf(name, "attribute without a name")
		p.skipRestOfLine()
		return nil
	}
	p.take()
	a.Name = name.text
	if p.isPunct(p.peek(), "(") {
		args := p.parseArgs()
		if args == nil {
			return nil
		}
		a.Args = args
	}
	a.Raw = p.lx.src[at.off:p.last.end]
	return a
}

// parseArgs reads `( arg, ... [,] )`; arguments never span lines.
func (p *parser) parseArgs() *ArgList {
	p.take()
	list := &ArgList{Args: []*Arg{}}
	if p.isPunct(p.peek(), ")") {
		p.take()
		return list
	}
	for {
		arg := &Arg{}
		t := p.peek()
		if t.kind == tokWord && isIdent(t.text) && p.isPunct(p.peekN(1), ":") {
			p.take()
			p.take()
			arg.Name = t.text
			if n := p.peek(); !p.isPunct(n, ",") && !p.isPunct(n, ")") {
				if arg.Value = p.parseExpr(); arg.Value == nil {
					return nil
				}
			}
		} else if arg.Value = p.parseExpr(); arg.Value == nil {
			return nil
		}
		list.Args = append(list.Args, arg)
		t = p.peek()
		switch {
		case p.isPunct(t, ")"):
			p.take()
			return list
		case p.isPunct(t, ",") && p.isPunct(p.peekN(1), ")"):
			p.take()
			p.take()
			list.TrailingComma = true
			return list
		case p.isPunct(t, ","):
			p.take()
		default:
			p.errorf(t, "expected `,` or `)` in an argument list, found %q", t.text)
			p.skipRestOfLine()
			return nil
		}
	}
}

// parseExpr reads one expression: a call, an array, an object, a number,
// a string or a constant path. It returns nil after reporting an error and
// skipping the line.
func (p *parser) parseExpr() *Expr {
	t := p.peek()
	e := &Expr{Line: t.line, Col: t.col}
	switch {
	case t.kind == tokString:
		p.take()
		if t.bad != "" {
			p.errorf(t, "%s", t.bad)
			p.skipRestOfLine()
			return nil
		}
		e.Kind, e.Value = ExprString, t.value
	case t.kind == tokWord && (t.text[0] == '-' || t.text[0] >= '0' && t.text[0] <= '9'):
		if !numberRe.MatchString(t.text) {
			p.errorf(t, "invalid number %q", t.text)
			p.skipRestOfLine()
			return nil
		}
		p.take()
		e.Kind, e.Value = ExprNumber, t.text
	case t.kind == tokWord && isPath(t.text):
		p.take()
		e.Kind, e.Value = ExprConstant, t.text
		if p.isPunct(p.peek(), "(") {
			args := p.parseArgs()
			if args == nil {
				return nil
			}
			e.Kind, e.Args = ExprCall, args
		}
	case p.isPunct(t, "["):
		p.take()
		e.Kind, e.Items = ExprArray, []*Expr{}
		if !p.isPunct(p.peek(), "]") {
			for {
				item := p.parseExpr()
				if item == nil {
					return nil
				}
				e.Items = append(e.Items, item)
				if !p.isPunct(p.peek(), ",") {
					break
				}
				p.take()
			}
		}
		if !p.isPunct(p.peek(), "]") {
			p.errorf(p.peek(), "expected `]`, found %q", p.peek().text)
			p.skipRestOfLine()
			return nil
		}
		p.take()
	case p.isPunct(t, "{"):
		p.take()
		e.Kind, e.Members = ExprObject, []*Arg{}
		if !p.isPunct(p.peek(), "}") {
			for {
				k := p.peek()
				if k.kind != tokWord || !isIdent(k.text) || !p.isPunct(p.peekN(1), ":") {
					p.errorf(k, "expected `name: value` in an object, found %q", k.text)
					p.skipRestOfLine()
					return nil
				}
				p.take()
				p.take()
				v := p.parseExpr()
				if v == nil {
					return nil
				}
				e.Members = append(e.Members, &Arg{Name: k.text, Value: v})
				if !p.isPunct(p.peek(), ",") {
					break
				}
				p.take()
			}
		}
		if !p.isPunct(p.peek(), "}") {
			p.errorf(p.peek(), "expected `}`, found %q", p.peek().text)
			p.skipRestOfLine()
			return nil
		}
		p.take()
	default:
		p.errorf(t, "expected an expression, found %q", t.text)
		p.skipRestOfLine()
		return nil
	}
	e.Raw = p.lx.src[t.off:p.last.end]
	return e
}
