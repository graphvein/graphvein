package tauri

import "strings"

// A wrapper must pass its own parameter unchanged (I2). The client extractor
// already drops the parameter binding for a plain or compound assignment to
// it, but not for update expressions, destructuring assignments or for-in/of
// heads, and none of these are in the cache (no TypeScript cache field
// records them: RouteScope bindings are reset only for identifier
// assignment targets, URLBinding.Mutated only for member mutations at module
// scope). writesParameter decides them at link time from the wrapper's
// source text (Revision 2, m1), so a wrapper never forwards a value Tauri
// does not receive.
//
// The lexer below does not parse JavaScript; it fails closed (Revision 3,
// MA1). Whenever its token stream may differ from the real one, the
// parameter counts as written and the callable is not a wrapper: a string
// that reaches a line break or the end, a template, regular expression or
// block comment that reaches the end, a `/` whose regex-or-division reading
// depends on context, a backslash or non-ASCII byte outside literals and
// comments, unbalanced or mismatched brackets, JSX-like `<` in files
// that may hold JSX, and a TypeScript non-null assertion (a postfix `!`).

// jsToken is one JavaScript or TypeScript token of a wrapper's source.
type jsToken struct {
	kind string // ident, punct, literal
	text string
}

// assignOperators are the operators that write their left operand.
var assignOperators = names("= += -= *= /= %= **= <<= >>= >>>= &= |= ^= &&= ||= ??=")

// punctuators, longest first, so `>>>=` is never read as `>>` and `>=`.
var punctuators = []string{">>>=", "...", "===", "!==", "**=", "<<=", ">>=", ">>>", "&&=", "||=", "??=", "=>", "==", "!=", "<=", ">=", "&&", "||", "??", "?.", "++", "--", "+=", "-=", "*=", "/=", "%=", "&=", "|=", "^=", "**", "<<", ">>"}

// regexAfter are the keywords after which `/` starts a regular expression.
var regexAfter = names("return typeof instanceof in of new delete void throw case do else yield await")

// keywords are reserved and contextual words. After one of them the reading
// of `/` depends on the grammar (`of`, `yield`, `await` and `let` can be
// identifiers; `this / 2` divides), so the lexer does not guess.
var keywords = union(regexAfter, names("break catch class const continue debugger default export extends finally for function if import let static super switch this try var while with async get set null true false undefined as satisfies implements interface package private protected public enum"))

// writesParameter reports whether source (a wrapper's text, parameters
// included) may write the binding name: an assignment, compound assignment
// or update of the name, the name inside a destructuring assignment target
// or a for-in/of head, a function or class declaration of the name, or any
// use of eval, arguments or with. The test is deliberately wide: a nested
// function's own binding of the same name, a default value or a computed key
// also count. jsx is set for files that may hold JSX (anything but .ts,
// .mts and .cts). When the lexer is unsure of its tokens (see above) the
// result is true. So a writing callable is never accepted as long as the
// lexer's token stream is the real one, and every input where it might not
// be is rejected; a real wrapper may be rejected too (under-claim).
func writesParameter(source []byte, name string, jsx bool) bool {
	tokens, unsure := lexJS(source, jsx)
	if unsure {
		return true
	}
	// Matching open and close brackets, so a name inside a group followed by
	// an assignment operator is seen as a destructuring target.
	closeOf := map[int]int{}
	var stack []int
	for i, t := range tokens {
		if t.kind != "punct" {
			continue
		}
		switch t.text {
		case "(", "[", "{":
			stack = append(stack, i)
		case ")", "]", "}":
			if len(stack) == 0 || tokens[stack[len(stack)-1]].text != opening[t.text] {
				return true
			}
			closeOf[stack[len(stack)-1]] = i
			stack = stack[:len(stack)-1]
		}
	}
	if len(stack) > 0 {
		return true
	}
	at := func(i int) jsToken {
		if i >= 0 && i < len(tokens) {
			return tokens[i]
		}
		return jsToken{}
	}
	var open []int
	for i, t := range tokens {
		if t.kind == "punct" {
			switch t.text {
			case "(", "[", "{":
				open = append(open, i)
			case ")", "]", "}":
				if len(open) > 0 {
					open = open[:len(open)-1]
				}
			}
			continue
		}
		if t.kind != "ident" {
			continue
		}
		prev, next := at(i-1), at(i+1)
		if prev.text == "." || prev.text == "?." {
			continue
		}
		switch t.text {
		case "eval", "arguments", "with":
			return true
		case "for":
			if writesInForHead(tokens, i, closeOf, name) {
				return true
			}
		}
		if t.text != name {
			continue
		}
		if next.kind == "punct" && assignOperators[next.text] || prev.text == "++" || prev.text == "--" || next.text == "++" || next.text == "--" {
			return true
		}
		if prev.kind == "ident" && (prev.text == "function" || prev.text == "class") {
			return true
		}
		for _, o := range open {
			c, ok := closeOf[o]
			if !ok {
				return true
			}
			after := at(c + 1)
			if after.kind != "punct" || !assignOperators[after.text] {
				continue
			}
			before := at(o - 1)
			subscript := tokens[o].text == "[" && (before.kind == "ident" && !regexAfter[before.text] || before.kind == "literal" || before.text == ")" || before.text == "]")
			if !subscript {
				return true
			}
		}
	}
	return false
}

// writesInForHead reports whether name appears before the top-level `of` or
// `in` of the for statement at tokens[i] (`for (name of ..)`,
// `for ([name] of ..)`, `for (const name in ..)`).
func writesInForHead(tokens []jsToken, i int, closeOf map[int]int, name string) bool {
	j := i + 1
	if j < len(tokens) && tokens[j].text == "await" {
		j++
	}
	if j >= len(tokens) || tokens[j].text != "(" {
		return false
	}
	end, ok := closeOf[j]
	if !ok {
		return true
	}
	depth, seen := 0, false
	for k := j + 1; k < end; k++ {
		t := tokens[k]
		switch {
		case t.kind == "punct" && (t.text == "(" || t.text == "[" || t.text == "{"):
			depth++
		case t.kind == "punct" && (t.text == ")" || t.text == "]" || t.text == "}"):
			depth--
		case t.kind == "punct" && t.text == ";" && depth == 0:
			return false
		case t.kind == "ident" && depth == 0 && (t.text == "of" || t.text == "in"):
			return seen
		case t.kind == "ident" && t.text == name:
			seen = true
		}
	}
	return false
}

// opening maps each closing bracket to its opening one.
var opening = map[string]string{")": "(", "]": "[", "}": "{"}

// lexJS splits JavaScript or TypeScript source into identifiers,
// punctuators and opaque literals (strings, templates, numbers, regular
// expressions). Comments and whitespace are dropped. Template substitutions
// are lexed as code. unsure reports that the tokens may not be the real
// ones (see the file comment); callers must then not rely on them.
func lexJS(src []byte, jsx bool) (out []jsToken, unsure bool) {
	var templates []int // brace depth at each open template substitution
	depth := 0
	// slashUnsure: after `)`, `]`, `}`, `++`, `--` or a keyword, `/` may
	// start a regular expression or divide, depending on the grammar.
	slashUnsure := func() bool {
		if len(out) == 0 {
			return false
		}
		last := out[len(out)-1]
		switch last.kind {
		case "ident":
			return keywords[last.text]
		case "punct":
			switch last.text {
			case ")", "]", "}", "++", "--":
				return true
			}
		}
		return false
	}
	// nonNullAssertion: a lone `!` (not `!=` or `!==`) after an identifier,
	// `)` or `]` is TypeScript's postfix non-null assertion (`cmd! = 'o'`,
	// `(cmd)!++`, `n! / 2`). The write test and the regex-or-division
	// reading do not model it, so the lexer is unsure (review 3, MA3).
	// A `!` after a keyword that starts an expression (`return !x`) is the
	// prefix operator; `of`, `yield` and `await` can be identifiers, so a
	// `!` after them is unsure as well.
	nonNullAssertion := func() bool {
		if len(out) == 0 {
			return false
		}
		last := out[len(out)-1]
		switch last.kind {
		case "ident":
			return !regexAfter[last.text] || last.text == "of" || last.text == "yield" || last.text == "await"
		case "punct":
			return last.text == ")" || last.text == "]"
		}
		return false
	}
	regexAllowed := func() bool {
		if len(out) == 0 {
			return true
		}
		return out[len(out)-1].kind == "punct"
	}
	template := func(i int) (int, bool) {
		// i is just after a backtick or a closing substitution brace.
		for i < len(src) {
			switch src[i] {
			case '\\':
				i += 2
				continue
			case '`':
				return i + 1, true
			case '$':
				if i+1 < len(src) && src[i+1] == '{' {
					templates = append(templates, depth)
					depth++
					return i + 2, true
				}
			}
			i++
		}
		return i, false
	}
	for i := 0; i < len(src); {
		b := src[i]
		switch {
		case b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\f' || b == '\v':
			i++
		case b == '/' && i+1 < len(src) && src[i+1] == '/':
			for i < len(src) && src[i] != '\n' && src[i] != '\r' {
				i++
			}
		case b == '/' && i+1 < len(src) && src[i+1] == '*':
			end := strings.Index(string(src[i+2:]), "*/")
			if end < 0 {
				return out, true
			}
			i += end + 4
		case b == '\'' || b == '"':
			j := i + 1
			for j < len(src) && src[j] != b {
				if src[j] == '\n' || src[j] == '\r' {
					return out, true
				}
				if src[j] == '\\' {
					j++
				}
				j++
			}
			if j >= len(src) {
				return out, true
			}
			out = append(out, jsToken{kind: "literal", text: "string"})
			i = j + 1
		case b == '`':
			out = append(out, jsToken{kind: "literal", text: "template"})
			next, ok := template(i + 1)
			if !ok {
				return out, true
			}
			i = next
		case b == '}' && len(templates) > 0 && templates[len(templates)-1] == depth-1:
			templates = templates[:len(templates)-1]
			depth--
			next, ok := template(i + 1)
			if !ok {
				return out, true
			}
			i = next
		case b == '/' && slashUnsure():
			return out, true
		case b == '/' && regexAllowed():
			j, class, closed := i+1, false, false
			for j < len(src) && src[j] != '\n' && src[j] != '\r' {
				c := src[j]
				if c == '\\' {
					j += 2
					continue
				}
				if c == '[' {
					class = true
				} else if c == ']' {
					class = false
				} else if c == '/' && !class {
					closed = true
					break
				}
				j++
			}
			if !closed {
				return out, true
			}
			j++
			for j < len(src) && identByte(src[j]) {
				j++
			}
			out = append(out, jsToken{kind: "literal", text: "regex"})
			i = j
		case b == '\\' || b >= 0x80:
			// An escaped identifier (`c\u{6D}d` is `cmd`) or a non-ASCII
			// identifier or space the lexer does not decode.
			return out, true
		case jsx && b == '<' && jsxStart(src[i+1:]):
			return out, true
		case identByte(b) && !(b >= '0' && b <= '9'):
			j := i
			for j < len(src) && identByte(src[j]) {
				j++
			}
			out = append(out, jsToken{kind: "ident", text: string(src[i:j])})
			i = j
		case b >= '0' && b <= '9' || b == '.' && i+1 < len(src) && src[i+1] >= '0' && src[i+1] <= '9':
			j := i + 1
			for j < len(src) && (identByte(src[j]) || src[j] == '.') {
				j++
			}
			out = append(out, jsToken{kind: "literal", text: "number"})
			i = j
		default:
			p := string(src[i : i+1])
			for _, candidate := range punctuators {
				if strings.HasPrefix(string(src[i:min(len(src), i+len(candidate))]), candidate) {
					p = candidate
					break
				}
			}
			if p == "!" && nonNullAssertion() {
				return out, true
			}
			switch p {
			case "{", "(", "[":
				depth++
			case "}", ")", "]":
				depth--
			}
			out = append(out, jsToken{kind: "punct", text: p})
			i += len(p)
		}
	}
	if len(templates) > 0 {
		return out, true
	}
	return out, false
}

// jsxStart reports whether the text after a `<` may open or close a JSX
// element or fragment: an identifier start, `>` or `/`, after optional
// whitespace. It is wide on purpose (`a < b` and type arguments match too).
func jsxStart(rest []byte) bool {
	for _, b := range rest {
		switch {
		case b == ' ' || b == '\t' || b == '\n' || b == '\r':
			continue
		case b == '>' || b == '/' || b == '\\' || b >= 0x80:
			return true
		default:
			return identByte(b) && !(b >= '0' && b <= '9')
		}
	}
	return false
}

// identByte accepts ASCII identifier bytes only; a non-ASCII byte ends the
// identifier and makes the lexer unsure.
func identByte(b byte) bool {
	return b == '_' || b == '$' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
}
