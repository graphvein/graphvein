package schema

import "fmt"

// Keyword categories in element and expression positions (T018a2 Revision
// 1.6 V3, class P7; extends U8). gram.y reads an index element and a
// partition key element (index_elem 8206, part_elem 4513) as a bare ColId,
// a func_expr_windowless or a parenthesised a_expr; an operator class after
// an element (opt_qualified_name 1137) as any_name, first part a ColId; an
// EXCLUDE element (ExclusionConstraintElem 4360) as an index_elem; and a
// column reference inside an expression (columnref 16773) as a ColId
// followed by ColLabels. A reserved or type_func_name keyword in one of
// these ColId positions is a syntax error (42601): row D. A bare keyword of
// the SQL value function list (func_expr_common_subexpr 15646) is that
// function, which is STABLE: an index element, index expression or
// predicate, partition key or generation expression holding one is refused
// (42P17); a DEFAULT may hold one.

// sqlValueKeywords are the SQL value functions gram.y spells as keywords.
var sqlValueKeywords = sqlValueFunctions

// operandStarters are the reserved and type_func_name keywords that may
// start an operand of a_expr (constants, special forms, prefix NOT, the
// SQL value functions and the arguments of trim).
var operandStarters = map[string]bool{
	"not": true, "case": true, "cast": true, "array": true, "exists": true, "null": true, "true": true,
	"false": true, "any": true, "all": true, "some": true, "distinct": true, "select": true,
	"leading": true, "trailing": true, "both": true, "collation": true, "default": true,
}

// operandAfter are the keywords after which an operand follows.
var operandAfter = map[string]bool{
	"and": true, "or": true, "not": true, "when": true, "then": true, "else": true, "case": true,
	"in": true, "like": true, "ilike": true, "between": true, "escape": true, "similar": true,
	"to": true, "is": true, "from": true, "where": true, "select": true, "distinct": true,
	"symmetric": true, "asymmetric": true, "any": true, "all": true, "some": true, "by": true,
	"on": true, "having": true, "limit": true, "offset": true, "for": true, "placing": true,
	"overlaps": true, "with": true,
}

// typeWord are the words of gram.y's multi-word SQL types after the first.
var typeWord = map[string]bool{
	"precision": true, "varying": true, "with": true, "without": true, "time": true, "zone": true,
	"year": true, "month": true, "day": true, "hour": true, "minute": true, "second": true, "to": true,
	"character": true, "char": true,
}

// keywordUse is what keywordInExpr found: why a keyword is a syntax error
// there, or the first SQL value function written as a bare keyword.
type keywordUse struct {
	syntax, valueFunction string
}

// keywordInExpr scans toks[from:to] (an a_expr) for a reserved or
// type_func_name keyword in an operand (columnref) position.
func keywordInExpr(toks []token, from, to int) keywordUse {
	var u keywordUse
	operand := true
	skipType := false
	label := false
	for j := from; j < to; j++ {
		t := toks[j]
		switch t.kind {
		case tokPunct:
			skipType = false
			switch t.value {
			case "(", ",", "[":
				operand = true
			case ".":
				label = true
				continue
			default:
				operand = false
			}
			label = false
			continue
		case tokOp:
			skipType = t.value == "::"
			operand = !skipType
			label = false
			continue
		case tokIdent:
		default:
			operand, label = false, false
			continue
		}
		if label {
			// After a dot or AS every keyword is a ColLabel.
			label = false
			continue
		}
		if skipType {
			// After :: a type name (columnType's own rules): its first word
			// and the words of the SQL-standard multi-word types.
			if !typeWord[t.value] {
				skipType = false
			}
			if skipType || toks[j-1].kind == tokOp {
				skipType = true
				continue
			}
		}
		cat := pgKeywordCategories[t.value]
		next := token{}
		if j+1 < to {
			next = toks[j+1]
		}
		call := next.kind == tokPunct && next.value == "("
		if t.value == "as" {
			// AS ColLabel (an alias or a cast's type).
			label = true
			operand = false
			continue
		}
		if !operand {
			// An infix NOT (NOT IN, NOT LIKE, NOT BETWEEN, ...) keeps the
			// position; the keyword after it decides.
			operand = operandAfter[t.value] && t.value != "not"
			continue
		}
		if t.value == "distinct" && next.kind == tokIdent && next.value == "from" {
			// IS [NOT] DISTINCT FROM.
			operand = false
			continue
		}
		if cat == 'R' || cat == 'T' {
			switch {
			case sqlValueKeywords[t.value] != "":
				if u.valueFunction == "" {
					u.valueFunction = t.value
				}
				operand = false
				continue
			case operandStarters[t.value]:
				operand = operandAfter[t.value] || t.value == "not" || t.value == "case" || t.value == "cast" || t.value == "array" || t.value == "exists" || t.value == "collation"
				if t.value == "null" || t.value == "true" || t.value == "false" {
					operand = false
				}
				continue
			case cat == 'T' && call:
				// A type_func_name keyword is a function name.
				operand = false
				continue
			}
			if u.syntax == "" {
				u.syntax = fmt.Sprintf("keyword %s %s (gram.y columnref, ColId)", t.value, keywordNameError)
			}
			operand = false
			continue
		}
		operand = false
	}
	return u
}

// keywordInElement checks one index_elem or part_elem toks[from:to] (an
// EXCLUDE element without its WITH part).
func keywordInElement(toks []token, from, to int) keywordUse {
	if from >= to {
		return keywordUse{}
	}
	first := toks[from]
	j := from + 1
	switch {
	case first.kind == tokPunct && first.value == "(":
		c := &cursor{toks: toks[:to], i: from}
		end, err := c.group()
		if err != nil {
			return keywordUse{}
		}
		u := keywordInExpr(toks, from+1, end-1)
		if u.syntax != "" {
			return u
		}
		j = end
		if t := elementTail(toks, j, to); t != "" {
			u.syntax = t
		}
		return u
	case first.kind == tokIdent && j < to && toks[j].kind == tokPunct && toks[j].value == "(":
		// func_expr_windowless: an unqualified name is a type_function_name
		// or a func_expr_common_subexpr keyword.
		if cat := pgKeywordCategories[first.value]; cat == 'R' && !specialCall[first.value] {
			return keywordUse{syntax: fmt.Sprintf("keyword %s %s (gram.y func_name)", first.value, keywordNameError)}
		}
		c := &cursor{toks: toks[:to], i: j}
		end, err := c.group()
		if err != nil {
			return keywordUse{}
		}
		u := keywordInExpr(toks, j+1, end-1)
		if u.syntax == "" {
			u.syntax = elementTail(toks, end, to)
		}
		return u
	case first.kind == tokIdent && (j >= to || toks[j].kind != tokPunct || toks[j].value != "."):
		if cat := pgKeywordCategories[first.value]; cat == 'R' || cat == 'T' {
			if sqlValueKeywords[first.value] != "" {
				return keywordUse{valueFunction: first.value, syntax: elementTail(toks, j, to)}
			}
			return keywordUse{syntax: fmt.Sprintf("keyword %s %s (gram.y index_elem, ColId)", first.value, keywordNameError)}
		}
	case first.kind != tokQuoted:
		return keywordUse{}
	}
	return keywordUse{syntax: elementTail(toks, j, to)}
}

// elementTail checks the words after an element's column or expression:
// [COLLATE any_name] [opclass [(params)]] [ASC | DESC] [NULLS FIRST |
// LAST]; the operator class's first part must be a ColId.
func elementTail(toks []token, from, to int) string {
	j := from
	if j < to && toks[j].kind == tokIdent && toks[j].value == "collate" {
		j++
		for j < to && (toks[j].kind == tokIdent || toks[j].kind == tokQuoted || toks[j].kind == tokPunct && toks[j].value == ".") {
			j++
		}
	}
	if j < to && toks[j].kind == tokIdent {
		switch toks[j].value {
		case "asc", "desc", "nulls":
		default:
			if cat := pgKeywordCategories[toks[j].value]; cat == 'R' || cat == 'T' {
				return fmt.Sprintf("keyword %s %s (gram.y opt_qualified_name, ColId)", toks[j].value, keywordNameError)
			}
		}
	}
	return ""
}

// elementList checks every element of the parenthesised list at toks[at]
// (index elements, PARTITION BY, EXCLUDE); exclude cuts each element at
// its depth-0 WITH.
func elementList(toks []token, at int, exclude bool) keywordUse {
	c := &cursor{toks: toks, i: at}
	end, err := c.group()
	if err != nil {
		return keywordUse{}
	}
	var u keywordUse
	for _, r := range c.splitList(at+1, end-1) {
		to := r[1]
		if exclude {
			if w := clauseEnd(toks[:r[1]], r[0], "with"); w < r[1] {
				to = w
			}
		}
		e := keywordInElement(toks, r[0], to)
		if u.syntax == "" {
			u.syntax = e.syntax
		}
		if u.valueFunction == "" {
			u.valueFunction = e.valueFunction
		}
	}
	return u
}

// keywordPositions applies V3 to CREATE TABLE, CREATE INDEX, ALTER TABLE
// and CREATE / ALTER DOMAIN: why the statement is a syntax error (row D),
// or why Postgres refuses it (a STABLE SQL value function where an
// IMMUTABLE expression is needed), with the precondition id.
func keywordPositions(toks []token) (why, id string) {
	c := &cursor{toks: toks}
	var syntaxID, refuseID string
	switch {
	case c.peekKw("create", "index"), c.peekKw("create", "unique", "index"):
		syntaxID, refuseID = "M4.P7", "M4.P5"
	case c.peekKw("create", "domain"), c.peekKw("alter", "domain"):
		syntaxID, refuseID = "R1.P7", "R1.P7"
	case c.peekKw("alter", "table"):
		syntaxID, refuseID = "M5a.P7", "M5a.P5"
	case c.peekKw("create"):
		syntaxID, refuseID = "M1.P7", "M1.P5"
		k := 1
		for k < len(toks) && toks[k].kind == tokIdent && (toks[k].value == "global" || toks[k].value == "local" || toks[k].value == "temp" || toks[k].value == "temporary" || toks[k].value == "unlogged") {
			k++
		}
		if !c.isKw(k, "table") {
			return "", ""
		}
	default:
		return "", ""
	}
	isPunct := func(j int, v string) bool { return j < len(toks) && toks[j].kind == tokPunct && toks[j].value == v }
	isKw := func(j int, v string) bool { return j < len(toks) && toks[j].kind == tokIdent && toks[j].value == v }
	groupEnd := func(j int) int {
		g := &cursor{toks: toks, i: j}
		end, err := g.group()
		if err != nil {
			return j
		}
		return end
	}
	syntax, refusal := "", ""
	note := func(u keywordUse, refuses bool, what string) {
		if syntax == "" && u.syntax != "" {
			syntax = u.syntax
		}
		if refuses && refusal == "" && u.valueFunction != "" {
			refusal = fmt.Sprintf("%s holds %s, a STABLE SQL value function where Postgres needs an IMMUTABLE expression (42P17)", what, u.valueFunction)
		}
	}
	index := syntaxID == "M4.P7"
	elementsDone := false
	depth := 0
	for j := 0; j < len(toks); j++ {
		t := toks[j]
		if t.kind == tokPunct && (t.value == "(" || t.value == "[") {
			if index && !elementsDone && depth == 0 {
				// The index's element list: the first top-level group.
				note(elementList(toks, j, false), true, "an index element")
				elementsDone = true
			}
			depth++
			continue
		}
		if t.kind == tokPunct && (t.value == ")" || t.value == "]") {
			depth--
			continue
		}
		if t.kind != tokIdent {
			continue
		}
		switch {
		case index && depth == 0 && t.value == "where" && elementsDone:
			note(keywordInExpr(toks, j+1, len(toks)), true, "the index predicate")
			j = len(toks)
		case t.value == "check" && isPunct(j+1, "("):
			end := groupEnd(j + 1)
			note(keywordInExpr(toks, j+2, end-1), false, "a CHECK expression")
		case t.value == "generated" && isKw(j+1, "always") && isKw(j+2, "as") && isPunct(j+3, "("):
			end := groupEnd(j + 3)
			note(keywordInExpr(toks, j+4, end-1), true, "a generation expression")
		case t.value == "exclude":
			k := j + 1
			if isKw(k, "using") {
				k += 2
			}
			if isPunct(k, "(") {
				note(elementList(toks, k, true), true, "an EXCLUDE element")
				end := groupEnd(k)
				for end < len(toks) && !(isKw(end, "where") && isPunct(end+1, "(")) && !isPunct(end, ",") && !isPunct(end, ")") {
					end++
				}
				if isKw(end, "where") {
					pe := groupEnd(end + 1)
					note(keywordInExpr(toks, end+2, pe-1), true, "an EXCLUDE predicate")
				}
			}
		case t.value == "partition" && isKw(j+1, "by") && (isKw(j+2, "range") || isKw(j+2, "list") || isKw(j+2, "hash")) && isPunct(j+3, "("):
			note(elementList(toks, j+3, false), true, "a partition key")
		}
	}
	switch {
	case syntax != "":
		return syntax, syntaxID
	case refusal != "":
		return "Postgres refuses: " + refusal, refuseID
	}
	return "", ""
}
