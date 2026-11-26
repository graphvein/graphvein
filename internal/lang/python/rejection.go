package python

import (
	"bytes"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode"

	"github.com/graphvein/graphvein/internal/parse"
	"golang.org/x/text/unicode/runenames"
)

// Rejection forms of row PS-F2 (docs/lang/python-structure.md, "Rejection
// forms"): a tree with an error, or a form the grammar reads without an
// error node but the reference Python's tokenizer or parser rejects. Each
// is one name in the document's table; rejection returns at most one.
const (
	rejectTreeSitterError   = "tree_sitter_error"
	rejectPrintStatement    = "print_statement"
	rejectExecStatement     = "exec_statement"
	rejectRaiseComma        = "raise_comma"
	rejectDiamond           = "diamond_operator"
	rejectBacktick          = "backtick_repr"
	rejectStringPrefix      = "string_prefix"
	rejectStringLiteral     = "string_literal"
	rejectLegacyInteger     = "legacy_integer"
	rejectNumberLiteral     = "number_literal"
	rejectExceptComma       = "except_comma"
	rejectExceptStar        = "except_star"
	rejectParameterOrder    = "parameter_order"
	rejectArgumentOrder     = "argument_order"
	rejectComprehensionIn   = "comprehension_tuple"
	rejectBareWalrus        = "unparenthesized_walrus"
	rejectInvalidTarget     = "invalid_target"
	rejectConstantName      = "constant_name"
	rejectAugmentedUnpacked = "augmented_unpacking"
	rejectImportFromList    = "import_from_list"
	rejectIndentation       = "indentation"
	rejectNestingLimit      = "nesting_limit"
	rejectEncodingCookie    = "encoding_cookie"
	rejectUnicodeVersion    = "unicode_version"
)

// rejectForms lists every rejection form in document order (the contract
// test checks that the document names each, with no other, and that
// TestPythonRejectionForms has a case for each).
var rejectForms = []string{rejectTreeSitterError, rejectPrintStatement, rejectExecStatement, rejectRaiseComma, rejectDiamond, rejectBacktick, rejectStringPrefix, rejectStringLiteral, rejectLegacyInteger, rejectNumberLiteral, rejectExceptComma, rejectExceptStar, rejectParameterOrder, rejectArgumentOrder, rejectComprehensionIn, rejectBareWalrus, rejectInvalidTarget, rejectConstantName, rejectAugmentedUnpacked, rejectImportFromList, rejectIndentation, rejectNestingLimit, rejectEncodingCookie, rejectUnicodeVersion}

// stringPrefixAllowed reports a string prefix the reference Python accepts
// (t-strings, PEP 750, from 3.14).
func stringPrefixAllowed(prefix string) bool {
	switch strings.ToLower(prefix) {
	case "", "r", "u", "b", "br", "rb", "f", "fr", "rf":
		return true
	case "t", "tr", "rt":
		return referenceAtLeast(14)
	}
	return false
}

// legacyInteger reports a Python 2 integer literal: a long suffix, or a
// decimal with a leading zero that is not all zeros.
func legacyInteger(text string) bool {
	if strings.HasSuffix(text, "l") || strings.HasSuffix(text, "L") {
		return true
	}
	if len(text) < 2 || text[0] != '0' {
		return false
	}
	switch text[1] {
	case 'x', 'X', 'o', 'O', 'b', 'B', '.', 'e', 'E', 'j', 'J':
		return false
	}
	digits := strings.ReplaceAll(text, "_", "")
	for _, c := range digits {
		if c == '.' || c == 'e' || c == 'E' || c == 'j' || c == 'J' {
			return false
		}
	}
	return strings.Trim(digits, "0") != ""
}

// numberUnderscoreInvalid reports an underscore the tokenizer rejects in
// a number literal: each `_` must stand between two digits, or follow the
// radix prefix of `0x`, `0o`, `0b` before a digit.
func numberUnderscoreInvalid(text string) bool {
	hex := len(text) > 1 && text[0] == '0' && (text[1] == 'x' || text[1] == 'X')
	digit := func(c byte) bool {
		return c >= '0' && c <= '9' || hex && (c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F')
	}
	for i := 0; i < len(text); i++ {
		if text[i] != '_' {
			continue
		}
		if i+1 >= len(text) || !digit(text[i+1]) {
			return true
		}
		prefix := i == 2 && text[0] == '0' && strings.ContainsRune("xXoObB", rune(text[1]))
		if i == 0 || !digit(text[i-1]) && !prefix {
			return true
		}
	}
	return false
}

func hasToken(n *parse.Node, token string) bool {
	for _, t := range n.Tokens {
		if t == token {
			return true
		}
	}
	return false
}

// rejection returns the first PS-F2 rejection form of a file: its
// encoding cookie, its tree, then its indentation and nesting as the
// reference tokenizer reads them; "" when none applies.
func rejection(root *parse.Node, source []byte) string {
	if encodingCookieInvalid(source) {
		return rejectEncodingCookie
	}
	found := ""
	var visit func(*parse.Node)
	visit = func(n *parse.Node) {
		if n == nil || found != "" {
			return
		}
		found = nodeRejection(n)
		for _, c := range n.Children {
			visit(c)
		}
	}
	visit(root)
	if found != "" {
		return found
	}
	return tokenizerRejection(root, source)
}

// nodeRejection returns the rejection form one node holds, or "".
func nodeRejection(n *parse.Node) string {
	switch n.Kind {
	case "ERROR":
		return rejectTreeSitterError
	case "print_statement":
		return rejectPrintStatement
	case "exec_statement":
		return rejectExecStatement
	case "raise_statement":
		// Python 2 `raise E, V[, T]`: the grammar reads an expression list.
		for _, c := range n.Children {
			if c.Kind == "expression_list" {
				return rejectRaiseComma
			}
		}
	case "comparison_operator":
		if hasToken(n, "<>") {
			return rejectDiamond
		}
	case "string_start":
		prefix := strings.TrimRight(n.Text, `"'`)
		if strings.HasPrefix(n.Text, "`") {
			return rejectBacktick
		}
		if !stringPrefixAllowed(prefix) {
			return rejectStringPrefix
		}
	case "string":
		if form := stringInvalid(n); form != "" {
			return form
		}
	case "concatenated_string":
		// Bytes do not concatenate with str, nor templates with anything
		// else (PEP 750).
		kinds := map[string]bool{}
		for _, c := range n.Children {
			if c.Kind == "string" {
				kinds[stringKind(c)] = true
			}
		}
		if len(kinds) > 1 && (kinds["bytes"] || kinds["template"]) {
			return rejectStringLiteral
		}
	case "integer", "float":
		if n.Kind == "integer" && legacyInteger(n.Text) {
			return rejectLegacyInteger
		}
		if numberUnderscoreInvalid(n.Text) {
			return rejectNumberLiteral
		}
	case "except_clause", "except_group_clause":
		// `except A, B:` is PEP 758 (3.14); with `as` it stays an error.
		if hasToken(n, ",") {
			alias := false
			for _, c := range n.Children {
				if c.Kind == "as_pattern" {
					alias = true
				}
			}
			if alias || !referenceAtLeast(14) {
				return rejectExceptComma
			}
		}
	case "try_statement":
		if exceptStarInvalid(n) {
			return rejectExceptStar
		}
	case "parameters", "lambda_parameters":
		if parameterOrderInvalid(n) {
			return rejectParameterOrder
		}
	case "argument_list":
		if argumentOrderInvalid(n) {
			return rejectArgumentOrder
		}
	case "for_in_clause":
		// `[x for x in a, b]` (Python 2); also the misread `f(x for x in
		// y, 1)`: an unparenthesised tuple after `in`.
		if hasToken(n, ",") {
			return rejectComprehensionIn
		}
	case "expression_statement":
		if len(n.Children) == 1 && n.Children[0].Kind == "named_expression" {
			return rejectBareWalrus
		}
	case "delete_statement":
		for _, c := range n.Children {
			if c.Kind != "comment" && !validTarget(c, targetDelete) {
				return rejectInvalidTarget
			}
		}
	case "with_item":
		for _, c := range n.Children {
			if c.Kind == "as_pattern" {
				if alias := c.Fields["alias"]; alias != nil && !validTarget(alias, targetAssign) {
					return rejectInvalidTarget
				}
			}
		}
	case "assignment":
		if n.Fields["type"] != nil && !validTarget(n.Fields["left"], targetAnnotated) {
			return rejectInvalidTarget
		}
	case "augmented_assignment":
		if left := n.Fields["left"]; left != nil && (left.Kind == "pattern_list" || left.Kind == "tuple_pattern" || left.Kind == "list_pattern" || left.Kind == "tuple" || left.Kind == "list" || left.Kind == "expression_list") {
			return rejectAugmentedUnpacked
		}
	case "import_from_statement":
		if importFromListInvalid(n) {
			return rejectImportFromList
		}
	case "identifier":
		if unicodeChanged(n.Text, identifierChanged) {
			return rejectUnicodeVersion
		}
		if constantName(n.Text) {
			return rejectConstantName
		}
	}
	return ""
}

// constantName reports an identifier the grammar reads as a name but whose
// NFKC form is `True`, `False` or `None` (`\U0001d413rue`, mathematical bold
// T): CPython's parser normalises it to the keyword's spelling and rejects
// the file in every identifier position (ValueError "identifier field
// can't represent 'True' constant", ast.parse). Only a non-ASCII spelling
// gets here: the ASCII keywords are not identifiers in the tree.
func constantName(text string) bool {
	switch identifier(text) {
	case "True", "False", "None":
		return text != "True" && text != "False" && text != "None"
	}
	return false
}

// unicodeChanged reports whether text holds a code point of ranges, one of
// the generated lists of unicodeversion.go: characters whose properties
// differ between the reference Python's Unicode version and the tables
// Graphvein normalises and names with. An identifier holding one may be
// normalised, sorted or accepted differently from the reference Python,
// so the file is refused (row PS-F2, unicode_version) instead.
func unicodeChanged(text string, ranges [][2]rune) bool {
	for _, r := range text {
		if r < 0x80 {
			continue
		}
		if runeInRanges(r, ranges) {
			return true
		}
	}
	return false
}

// runeInRanges reports whether r lies in one of the sorted inclusive ranges.
func runeInRanges(r rune, ranges [][2]rune) bool {
	i := sort.Search(len(ranges), func(i int) bool { return ranges[i][1] >= r })
	return i < len(ranges) && ranges[i][0] <= r
}

// stringKind sorts a string literal: bytes, template, or str (plain and
// f-strings).
func stringKind(n *parse.Node) string {
	for _, c := range n.Children {
		if c.Kind == "string_start" {
			prefix := strings.ToLower(strings.TrimRight(c.Text, `"'`))
			switch {
			case strings.Contains(prefix, "b"):
				return "bytes"
			case strings.Contains(prefix, "t"):
				return "template"
			}
		}
	}
	return "str"
}

// stringInvalid returns the rejection form of a literal the tokenizer or
// parser rejects, or "": string_literal for a bytes literal holding a
// non-ASCII character, a `\N{...}` escape naming no character Graphvein
// knows (Unicode names, ASCII letters in any case: unicodeNameKey), or an
// f-string conversion other than `!s`, `!r`, `!a`; unicode_version for a `\N{...}` name only the tables'
// newer Unicode version holds (the reference Python does not know it).
func stringInvalid(n *parse.Node) string {
	prefix := ""
	for _, c := range n.Children {
		if c.Kind == "string_start" {
			prefix = strings.ToLower(strings.TrimRight(c.Text, `"'`))
		}
	}
	bytesLiteral := strings.Contains(prefix, "b")
	raw := strings.Contains(prefix, "r")
	invalid := ""
	var visit func(*parse.Node)
	visit = func(c *parse.Node) {
		if invalid != "" {
			return
		}
		switch c.Kind {
		case "string_content":
			if bytesLiteral {
				for i := 0; i < len(c.Text); i++ {
					if c.Text[i] >= 0x80 {
						invalid = rejectStringLiteral
					}
				}
			}
		case "escape_sequence":
			if !bytesLiteral && !raw && strings.HasPrefix(c.Text, `\N{`) && strings.HasSuffix(c.Text, "}") {
				key, ascii := unicodeNameKey(c.Text[3 : len(c.Text)-1])
				switch {
				case !ascii:
					invalid = rejectStringLiteral
				case newerUnicodeName(key):
					invalid = rejectUnicodeVersion
				case !unicodeName(key):
					invalid = rejectStringLiteral
				}
			}
		case "type_conversion":
			switch c.Text {
			case "!s", "!r", "!a":
			default:
				invalid = rejectStringLiteral
			}
		case "string":
			if c != n {
				// A nested literal inside an interpolation is checked on
				// its own visit.
				return
			}
		}
		for _, d := range c.Children {
			visit(d)
		}
	}
	visit(n)
	return invalid
}

var (
	runeNamesOnce sync.Once
	runeNames     map[string]bool
	newerNames    map[string]bool
)

// loadRuneNames fills runeNames with the names unicodeName confirms and
// newerNames with the names of golang.org/x/text's tables whose code point
// is in nameChanged (characters the reference Python's Unicode version does
// not name).
func loadRuneNames() {
	runeNames = map[string]bool{}
	newerNames = map[string]bool{}
	for r := rune(0); r <= unicode.MaxRune; r++ {
		n := runenames.Name(r)
		if n == "" || strings.HasPrefix(n, "<") {
			continue
		}
		if runeInRanges(r, nameChanged) {
			newerNames[n] = true
		} else {
			runeNames[n] = true
		}
	}
}

// unicodeNameKey returns a `\N{...}` name as the reference Python looks
// it up: CPython's unicodedata `_getcode` upper-cases the name byte by
// byte with Py_TOUPPER, which maps ASCII letters only. A name holding a
// non-ASCII byte matches no character name there (every name is ASCII),
// even one whose Unicode upper case would be ASCII (U+017F `ſ` to `S`,
// U+0131 `ı` to `I`, U+212A KELVIN SIGN to `K`): ascii is false and the
// literal is string_literal. Unicode case mapping (strings.ToUpper) would
// read the tables' Unicode version and accept such names.
func unicodeNameKey(name string) (key string, ascii bool) {
	b := []byte(name)
	for i, c := range b {
		switch {
		case c >= 0x80:
			return "", false
		case c >= 'a' && c <= 'z':
			b[i] = c - 'a' + 'A'
		}
	}
	return string(b), true
}

// newerUnicodeName reports a `\N{...}` name key (unicodeNameKey) that only
// the tables' newer Unicode version holds (row PS-F2, unicode_version).
func newerUnicodeName(key string) bool {
	runeNamesOnce.Do(loadRuneNames)
	return newerNames[key]
}

// unicodeName reports a name key (unicodeNameKey: ASCII, upper case) that
// `\N{...}` accepts and Graphvein can confirm: a character name of
// golang.org/x/text's Unicode tables whose name the reference Python's
// Unicode version also holds (not in nameChanged: characters newer than
// the reference are left out). Aliases, algorithmic names (CJK, Hangul,
// Tangut) and names the tables do not hold are not confirmed: the file is
// refused (over-refusal, never a wrong claim).
func unicodeName(key string) bool {
	runeNamesOnce.Do(loadRuneNames)
	return runeNames[key]
}

// exceptStarInvalid reports a try statement mixing `except` and `except*`,
// or an `except*` without an exception type.
func exceptStarInvalid(n *parse.Node) bool {
	plain, star := false, false
	for _, c := range n.Children {
		if c.Kind != "except_clause" && c.Kind != "except_group_clause" {
			continue
		}
		if c.Kind == "except_group_clause" || hasToken(c, "*") {
			star = true
			typed := false
			for _, d := range c.Children {
				if d.Kind != "block" && d.Kind != "comment" {
					typed = true
				}
			}
			if !typed {
				return true
			}
		} else {
			plain = true
		}
	}
	return plain && star
}

// importFromListInvalid reports a from-list with a dotted name
// (`from a import b.c`) or an unparenthesised trailing comma.
func importFromListInvalid(n *parse.Node) bool {
	module := n.Fields["module_name"]
	for _, c := range n.Children {
		if c == module {
			continue
		}
		name := c
		if c.Kind == "aliased_import" {
			name = c.Fields["name"]
		}
		if name != nil && name.Kind == "dotted_name" {
			identifiers := 0
			for _, d := range name.Children {
				if d.Kind == "identifier" {
					identifiers++
				}
			}
			if identifiers > 1 {
				return true
			}
		}
	}
	return !hasToken(n, "(") && strings.HasSuffix(strings.TrimRight(n.Text, " \t\f\\\n"), ",")
}

// Target contexts of validTarget.
const (
	targetAssign = iota
	targetDelete
	targetAnnotated
)

// validTarget reports a target Python's grammar accepts: a name, an
// attribute or a subscript, in parentheses or not; for an assignment or
// deletion also a tuple or list of targets (an assignment's may hold one
// starred target); an annotated assignment takes a single target only.
func validTarget(n *parse.Node, context int) bool {
	if n == nil {
		return false
	}
	switch n.Kind {
	case "identifier", "attribute", "subscript":
		return true
	case "as_pattern_target", "parenthesized_expression":
		inner := firstNamed(n)
		if inner == nil {
			return false
		}
		if context == targetAnnotated && n.Kind == "parenthesized_expression" {
			return inner.Kind == "identifier" || inner.Kind == "attribute" || inner.Kind == "subscript" || inner.Kind == "parenthesized_expression" && validTarget(inner, context)
		}
		return validTarget(inner, context)
	case "tuple", "list", "tuple_pattern", "list_pattern", "pattern_list", "expression_list":
		if (n.Kind == "tuple" || n.Kind == "tuple_pattern") && !hasToken(n, ",") {
			// `(x)` read as a one-element pattern: a parenthesised target.
			inner := firstNamed(n)
			return inner != nil && inner.Kind != "list_splat" && inner.Kind != "list_splat_pattern" && validTarget(inner, context)
		}
		if context == targetAnnotated {
			return false
		}
		for _, c := range n.Children {
			if c.Kind != "comment" && !validTarget(c, context) {
				return false
			}
		}
		return true
	case "list_splat", "list_splat_pattern":
		return context == targetAssign && validTarget(firstNamed(n), context)
	}
	return false
}

// parameterOrderInvalid reports a parameter list the reference parser
// rejects: a parameter without a default after one with a default (before
// `*`), a bare `*` with no named parameter after it, anything after
// `**kwargs`, a second `*` or `/`, a `/` first or after `*`.
func parameterOrderInvalid(n *parse.Node) bool {
	defaulted, star, named, doubleStar, slash, any := false, false, false, false, false, false
	for _, c := range n.Children {
		kind := c.Kind
		if kind == "comment" {
			continue
		}
		if doubleStar {
			return true
		}
		if kind == "typed_parameter" && len(c.Children) > 0 {
			// `*args: T` and `**kwargs: T` are typed splats.
			if first := c.Children[0].Kind; first == "list_splat_pattern" || first == "dictionary_splat_pattern" {
				kind = first
			}
		}
		switch kind {
		case "default_parameter", "typed_default_parameter":
			defaulted = true
			if star {
				named = true
			}
		case "identifier", "typed_parameter":
			if star {
				named = true
			} else if defaulted {
				return true
			}
		case "keyword_separator", "list_splat_pattern":
			if star {
				return true
			}
			if kind == "list_splat_pattern" {
				named = true
			}
			star = true
		case "dictionary_splat_pattern":
			if star && !named {
				return true
			}
			doubleStar = true
		case "positional_separator":
			if !any || slash || star {
				return true
			}
			slash = true
		}
		any = true
	}
	return star && !named
}

// argumentOrderInvalid reports a call whose positional argument follows a
// keyword argument or `**`, or whose `*` follows `**`.
func argumentOrderInvalid(n *parse.Node) bool {
	keyword, doubleStar := false, false
	for _, c := range n.Children {
		switch c.Kind {
		case "comment":
		case "keyword_argument":
			keyword = true
		case "dictionary_splat":
			doubleStar = true
		case "list_splat":
			if doubleStar {
				return true
			}
		default:
			if keyword || doubleStar {
				return true
			}
		}
	}
	return false
}

// Limits of CPython's tokenizer (Parser/tokenizer.h, Parser/lexer).
const (
	maxBracketDepth = 200 // MAXLEVEL: "too many nested parentheses"
	maxIndentLevels = 100 // MAXINDENT: "too many levels of indentation"
	tabSize         = 8
)

// tokenizerRejection reads the indentation and bracket nesting as CPython's
// tokenizer does, outside the string and comment tokens of the tree: at
// the start of each logical line (bracket depth 0, no backslash
// continuation, not blank or comment-only), the column with tabs to the
// next multiple of 8 and the column with tabs as one column must order the
// same against the indentation stack, or the tokenizer raises TabError
// (Python 2 files mixing tabs and spaces) or IndentationError
// (indentation); over 200 nested brackets or 100 indentation levels raise
// a limit error (nesting_limit).
func tokenizerRejection(root *parse.Node, source []byte) string {
	var opaque [][2]uint32
	var collect func(*parse.Node)
	collect = func(n *parse.Node) {
		if n.Kind == "string" || n.Kind == "comment" {
			opaque = append(opaque, [2]uint32{n.Start, n.End})
			return
		}
		for _, c := range n.Children {
			collect(c)
		}
	}
	collect(root)
	k := 0
	depth := 0
	continuation, lineStart := false, true
	indents, alts := []int{0}, []int{0}
	i := 0
	if bytes.HasPrefix(source, []byte("\xef\xbb\xbf")) {
		i = 3
	}
	for i < len(source) {
		for k < len(opaque) && int(opaque[k][1]) <= i {
			k++
		}
		if k < len(opaque) && int(opaque[k][0]) <= i {
			i = int(opaque[k][1])
			lineStart = false
			continue
		}
		if lineStart {
			lineStart = false
			if depth == 0 && !continuation {
				col, alt, j := 0, 0, i
				for ; j < len(source); j++ {
					switch source[j] {
					case ' ':
						col++
						alt++
						continue
					case '\t':
						col = (col/tabSize + 1) * tabSize
						alt++
						continue
					case '\f':
						col, alt = 0, 0
						continue
					}
					break
				}
				blank := j >= len(source) || source[j] == '#' || source[j] == '\n' || source[j] == '\\'
				if !blank {
					top := len(indents) - 1
					switch {
					case col == indents[top]:
						if alt != alts[top] {
							return rejectIndentation
						}
					case col > indents[top]:
						if top+1 >= maxIndentLevels {
							return rejectNestingLimit
						}
						if alt <= alts[top] {
							return rejectIndentation
						}
						indents, alts = append(indents, col), append(alts, alt)
					default:
						for top > 0 && col < indents[top] {
							top--
						}
						indents, alts = indents[:top+1], alts[:top+1]
						if col != indents[top] || alt != alts[top] {
							return rejectIndentation
						}
					}
				}
				i = j
				continuation = false
				continue
			}
			continuation = false
		}
		switch source[i] {
		case '(', '[', '{':
			depth++
			if depth > maxBracketDepth {
				return rejectNestingLimit
			}
		case ')', ']', '}':
			if depth > 0 {
				depth--
			}
		case '\\':
			if i+1 < len(source) && source[i+1] == '\n' {
				continuation, lineStart = true, true
				i += 2
				continue
			}
		case '\n':
			lineStart = true
		}
		i++
	}
	return ""
}

// The coding cookie of PEP 263, on line 1, or on line 2 when line 1 is
// blank or a comment (CPython's tokenizer).
var (
	cookieLine  = regexp.MustCompile(`^[ \t\f]*#.*?coding[:=][ \t]*([-\w.]+)`)
	blankOrNote = regexp.MustCompile(`^[ \t\f]*(?:#|$)`)
)

// encodingCookieInvalid reports a coding cookie Graphvein cannot follow:
// a byte order mark with a cookie that is not UTF-8 (CPython: "encoding
// problem"), a cookie naming no codec of the reference Python whose ASCII
// bytes read as ASCII (unknown or not ASCII-compatible), or a non-UTF-8
// codec over a file with non-ASCII bytes (Python reads other characters
// than Graphvein's UTF-8 text).
func encodingCookieInvalid(source []byte) bool {
	bom := bytes.HasPrefix(source, []byte("\xef\xbb\xbf"))
	text := source
	if bom {
		text = text[3:]
	}
	lines := bytes.SplitN(text, []byte("\n"), 3)
	cookie := ""
	if m := cookieLine.FindSubmatch(lines[0]); m != nil {
		cookie = string(m[1])
	} else if len(lines) > 1 && blankOrNote.Match(lines[0]) {
		if m := cookieLine.FindSubmatch(lines[1]); m != nil {
			cookie = string(m[1])
		}
	}
	if cookie == "" {
		return false
	}
	if normalCookie(cookie) == "utf-8" {
		return false
	}
	if bom {
		return true
	}
	key := codecKey(cookie)
	if !asciiCodecs[key] && !asciiCodecs[strings.ReplaceAll(key, ".", "_")] {
		return true
	}
	for _, c := range source {
		if c >= 0x80 {
			return !utf8Codecs[key] && !utf8Codecs[strings.ReplaceAll(key, ".", "_")]
		}
	}
	return false
}

// normalCookie is the tokenizer's get_normal_name: "utf-8" for any
// "utf-8" or "utf-8-*" spelling (case and `_` ignored in the first 12
// characters), else the cookie.
func normalCookie(cookie string) string {
	head := cookie
	if len(head) > 12 {
		head = head[:12]
	}
	head = strings.ReplaceAll(strings.ToLower(head), "_", "-")
	if head == "utf-8" || strings.HasPrefix(head, "utf-8-") {
		return "utf-8"
	}
	return cookie
}

// codecKey is the codec registry's normalisation of an encoding name:
// lower case, each run of characters other than letters, digits and `.`
// one `_`, none at either end (encodings.normalize_encoding).
func codecKey(name string) string {
	var b strings.Builder
	punct := false
	for _, c := range strings.ToLower(name) {
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '.' {
			if punct && b.Len() > 0 {
				b.WriteByte('_')
			}
			b.WriteRune(c)
			punct = false
		} else {
			punct = true
		}
	}
	return b.String()
}

var (
	asciiCodecs = nameSet(asciiCodecNames)
	utf8Codecs  = nameSet(utf8CodecNames)
)
