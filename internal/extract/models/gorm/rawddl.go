package gorm

import (
	"strings"
	"unicode/utf8"
)

// RawChange is one statement of an Exec or Raw literal DDL text that may
// change a claimed table (Revision 2 addendum): an ALTER,
// DROP or RENAME can drop, rename or alter a table or column, and a
// CREATE TABLE decides a table's columns, types, constraints and indexes
// (AutoMigrate then only adds missing columns), so the tables it names
// keep only their names; a CREATE INDEX adds an index, so the index lists
// of the table it names are unknown.
type RawChange struct {
	File, Method string
	Line         int
	// Statement is the statement text, shortened for reasons.
	Statement string
	// Tables are the table names the statement names (the last part of a
	// qualified name, quotes removed); empty when the target is not read
	// from the text: the change may touch every table of the group.
	Tables []string
	// IndexOnly marks a CREATE INDEX: only the index and constraint lists
	// of the table change.
	IndexOnly bool
}

// ddlWords are the first words that make an Exec or Raw text DDL.
var ddlWords = map[string]bool{"CREATE": true, "ALTER": true, "DROP": true, "RENAME": true}

// matches reports whether the statement may change the table named name:
// names compare without case (mysql can fold them) and without a schema
// qualifier, so a statement is never missed for its spelling.
func (c RawChange) matches(name string) bool {
	if len(c.Tables) == 0 {
		return true
	}
	name = lastPart(name)
	for _, t := range c.Tables {
		if strings.EqualFold(t, name) {
			return true
		}
	}
	return false
}

func lastPart(name string) string {
	if i := strings.LastIndex(name, "."); i >= 0 {
		return name[i+1:]
	}
	return name
}

// readRawDDL reads a literal SQL text: the first word of its first DDL
// statement ("" when no statement starts with CREATE, ALTER, DROP or
// RENAME) and the statements that may change a claimed table. A CREATE
// TABLE counts as an ALTER of the table it names whatever its place
// beside the migrate calls, because the order the code runs them in is
// not proven (Revision 2 addendum, part 2); a CREATE that is neither a
// table nor an index (a view, a trigger, a function, ...) changes no
// claimed table's facts; it only marks the group (GO-V2).
func readRawDDL(text string) (string, []RawChange) {
	first := ""
	var out []RawChange
	for _, stmt := range splitStatements(text) {
		toks := sqlTokens(stmt)
		if len(toks) == 0 || toks[0].quoted || !ddlWords[upper(toks[0])] {
			continue
		}
		word := upper(toks[0])
		if first == "" {
			first = word
		}
		ch := RawChange{Statement: shorten(stmt)}
		switch word {
		case "CREATE":
			switch {
			case createsTable(toks):
				ch.Tables = createTables(toks)
			case createsIndex(toks):
				ch.IndexOnly = true
				if t := indexTable(toks); t != "" {
					ch.Tables = []string{t}
				}
			default:
				continue
			}
		case "ALTER":
			ch.Tables = alterTables(toks)
		case "DROP":
			ch.Tables = dropTables(toks)
		case "RENAME":
			ch.Tables = renameTables(toks)
		}
		out = append(out, ch)
	}
	return first, out
}

// sqlToken is a word, a quoted identifier, a string literal or one
// punctuation character of a statement.
type sqlToken struct {
	text           string
	quoted, string bool
}

func upper(t sqlToken) string {
	if t.quoted || t.string {
		return ""
	}
	return strings.ToUpper(t.text)
}

// splitStatements splits a text at the semicolons outside quotes and
// comments.
func splitStatements(text string) []string {
	var out []string
	start := 0
	for i := 0; i < len(text); i++ {
		switch c := text[i]; c {
		case '\'', '"', '`':
			i = closing(text, i, c)
		case '-':
			if i+1 < len(text) && text[i+1] == '-' {
				i = lineEnd(text, i)
			}
		case '/':
			if i+1 < len(text) && text[i+1] == '*' {
				i = commentEnd(text, i)
			}
		case ';':
			out = append(out, text[start:i])
			start = i + 1
		}
	}
	return append(out, text[start:])
}

// closing is the index of the quote closing the one at i (a doubled quote
// is part of the text), or the text's last index.
func closing(text string, i int, q byte) int {
	for j := i + 1; j < len(text); j++ {
		if text[j] == q {
			if j+1 < len(text) && text[j+1] == q {
				j++
				continue
			}
			return j
		}
	}
	return len(text) - 1
}

func lineEnd(text string, i int) int {
	if j := strings.IndexByte(text[i:], '\n'); j >= 0 {
		return i + j
	}
	return len(text) - 1
}

func commentEnd(text string, i int) int {
	if j := strings.Index(text[i+2:], "*/"); j >= 0 {
		return i + 2 + j + 1
	}
	return len(text) - 1
}

// sqlTokens splits a statement into tokens, dropping comments.
func sqlTokens(stmt string) []sqlToken {
	var out []sqlToken
	for i := 0; i < len(stmt); i++ {
		c := stmt[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
		case c == '-' && i+1 < len(stmt) && stmt[i+1] == '-':
			i = lineEnd(stmt, i)
		case c == '/' && i+1 < len(stmt) && stmt[i+1] == '*':
			i = commentEnd(stmt, i)
		case c == '"' || c == '`' || c == '\'':
			j := closing(stmt, i, c)
			inner := ""
			if j > i {
				inner = stmt[i+1 : j]
			}
			q := string(c)
			inner = strings.ReplaceAll(inner, q+q, q)
			out = append(out, sqlToken{text: inner, quoted: c != '\'', string: c == '\''})
			i = j
		case isWordByte(c):
			j := i
			for j < len(stmt) && isWordByte(stmt[j]) {
				j++
			}
			out = append(out, sqlToken{text: stmt[i:j]})
			i = j - 1
		default:
			out = append(out, sqlToken{text: string(c)})
		}
	}
	return out
}

func isWordByte(c byte) bool {
	return c == '_' || c == '$' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= utf8.RuneSelf
}

// name reads a possibly qualified name at toks[i]: its last part and the
// index after it ("" when toks[i] is not a name).
func name(toks []sqlToken, i int) (string, int) {
	last := ""
	for i < len(toks) {
		t := toks[i]
		if t.string || !t.quoted && !isWordByte(t.text[0]) {
			break
		}
		last = t.text
		i++
		if i+1 < len(toks) && toks[i].text == "." && !toks[i].quoted {
			i++
			continue
		}
		break
	}
	return last, i
}

// skip skips the keywords at toks[i] that are in words.
func skip(toks []sqlToken, i int, words ...string) int {
	for i < len(toks) {
		w := upper(toks[i])
		found := false
		for _, x := range words {
			found = found || w == x
		}
		if !found {
			return i
		}
		i++
	}
	return i
}

// alterTables reads ALTER TABLE [IF EXISTS] [ONLY] name ...; a RENAME TO
// names the new table too. Another ALTER (of a type, an index, a
// sequence, ...) returns nil: its target table is not read.
func alterTables(toks []sqlToken) []string {
	if len(toks) < 3 || upper(toks[1]) != "TABLE" {
		return nil
	}
	n, i := name(toks, skip(toks, 2, "IF", "EXISTS", "ONLY"))
	if n == "" {
		return nil
	}
	out := []string{n}
	for ; i+2 < len(toks); i++ {
		if upper(toks[i]) == "RENAME" && upper(toks[i+1]) == "TO" {
			if m, _ := name(toks, i+2); m != "" {
				out = append(out, m)
			}
		}
	}
	return out
}

// dropTables reads DROP TABLE [IF EXISTS] name {, name}; another DROP
// returns nil (an index, a type or a sequence dropped with CASCADE can
// change any table).
func dropTables(toks []sqlToken) []string {
	if len(toks) < 3 || upper(toks[1]) != "TABLE" {
		return nil
	}
	var out []string
	i := skip(toks, 2, "IF", "EXISTS")
	for {
		n, j := name(toks, i)
		if n == "" {
			return nil
		}
		out = append(out, n)
		if j >= len(toks) || toks[j].text != "," || toks[j].quoted {
			return out
		}
		i = j + 1
	}
}

// renameTables reads mysql's RENAME TABLE a TO b {, c TO d}: both names
// of every pair.
func renameTables(toks []sqlToken) []string {
	if len(toks) < 3 || upper(toks[1]) != "TABLE" {
		return nil
	}
	var out []string
	i := 2
	for {
		a, j := name(toks, i)
		if a == "" || j >= len(toks) || upper(toks[j]) != "TO" {
			return nil
		}
		b, k := name(toks, j+1)
		if b == "" {
			return nil
		}
		out = append(out, a, b)
		if k >= len(toks) || toks[k].text != "," || toks[k].quoted {
			return out
		}
		i = k + 1
	}
}

// tableModifiers are the words a CREATE TABLE may carry before TABLE:
// postgres's GLOBAL, LOCAL, TEMP, TEMPORARY, UNLOGGED and FOREIGN and
// mariadb's OR REPLACE.
var tableModifiers = []string{"OR", "REPLACE", "GLOBAL", "LOCAL", "TEMP", "TEMPORARY", "UNLOGGED", "FOREIGN"}

// createsTable reports CREATE [modifiers] TABLE.
func createsTable(toks []sqlToken) bool {
	i := skip(toks, 1, tableModifiers...)
	return i < len(toks) && upper(toks[i]) == "TABLE"
}

// createTables reads CREATE [modifiers] TABLE [IF NOT EXISTS] name; nil
// when the name is not read (a placeholder, a missing name), so the
// statement may create any table of the group.
func createTables(toks []sqlToken) []string {
	i := skip(toks, 1, tableModifiers...)
	if n, _ := name(toks, skip(toks, i+1, "IF", "NOT", "EXISTS")); n != "" {
		return []string{n}
	}
	return nil
}

// createsIndex reports CREATE [UNIQUE|FULLTEXT|SPATIAL] INDEX.
func createsIndex(toks []sqlToken) bool {
	i := skip(toks, 1, "UNIQUE", "FULLTEXT", "SPATIAL")
	return i < len(toks) && upper(toks[i]) == "INDEX"
}

// indexTable is the table after a CREATE INDEX's ON [ONLY] ("" when there
// is none).
func indexTable(toks []sqlToken) string {
	for i := 2; i < len(toks); i++ {
		if upper(toks[i]) == "ON" {
			n, _ := name(toks, skip(toks, i+1, "ONLY"))
			return n
		}
	}
	return ""
}

// shorten is a statement on one line, at most 80 characters.
func shorten(stmt string) string {
	s := strings.Join(strings.Fields(stmt), " ")
	if utf8.RuneCountInString(s) > 80 {
		r := []rune(s)
		s = string(r[:77]) + "..."
	}
	return s
}
