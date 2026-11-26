package diesel

import (
	"fmt"

	"github.com/graphvein/graphvein/internal/contract"
)

// SchemaFile is one schema file as the lexer and item reader see it.
type SchemaFile struct {
	Path string
	// Items are the file's items; nil when LexError is set.
	Items []Item
	// LexError is set when the lexer refuses the file: every definition
	// in it is unread (row D, one marker).
	LexError error
}

// ReadFile lexes and reads a schema file and checks the failures the
// Diesel macros prove (row DI-P1): each failing table gets a
// schema_model_import_failure diagnostic, and two table! with one Rust
// name in one module fail both. A file the lexer refuses gets one
// schema_model_unread marker.
func ReadFile(in Input, file string, source []byte) (*SchemaFile, []contract.Diagnostic) {
	var diags []contract.Diagnostic
	sf := &SchemaFile{Path: file}
	trees, err := LexTrees(string(source))
	if err != nil {
		sf.LexError = err
		diags = append(diags, contract.Diagnostic{Type: "diagnostic", File: file, Severity: "info", Code: codeUnread,
			Message: fmt.Sprintf("%s: the schema file could not be read (%v); no definition in it is read", file, err)})
		return sf, diags
	}
	sf.Items = ReadItems(trees)
	markDuplicateTables(sf.Items)
	walkTables(sf.Items, func(it *Item) {
		f := it.Table.Failure
		if f == nil {
			return
		}
		in.tag("DI-P1/" + f.Form)
		diags = append(diags, contract.Diagnostic{Type: "diagnostic", File: file, Severity: "warning", Code: codeImportFailure,
			Message: fmt.Sprintf("%s:%d: %s! %s does not compile (%s); no table claimed (DI-P1)", file, f.Line, it.Macro, tableLabel(it.Table), f.Message)})
	})
	return sf, diags
}

func tableLabel(t *Table) string {
	if t.RustName == "" {
		return "<unnamed>"
	}
	if t.Schema != "" {
		return t.Schema + "." + t.RustName
	}
	return t.RustName
}

// walkTables calls fn for every table item at any module depth, in file
// order.
func walkTables(items []Item, fn func(*Item)) {
	for i := range items {
		switch items[i].Kind {
		case ItemTable:
			fn(&items[i])
		case ItemModule:
			walkTables(items[i].Items, fn)
		}
	}
}

// markDuplicateTables fails every table! whose Rust name another table!
// of the same module declares (two modules of one name do not compile).
func markDuplicateTables(items []Item) {
	byName := map[string][]*Table{}
	var order []string
	for i := range items {
		switch items[i].Kind {
		case ItemTable:
			t := items[i].Table
			if t.Failure != nil && t.Failure.Form == FailSyntax {
				continue
			}
			key := identKey(t.RustName, t.RustRaw)
			if byName[key] == nil {
				order = append(order, key)
			}
			byName[key] = append(byName[key], t)
		case ItemModule:
			markDuplicateTables(items[i].Items)
		}
	}
	for _, key := range order {
		tables := byName[key]
		if len(tables) < 2 {
			continue
		}
		for _, t := range tables {
			t.Failure = &Failure{Form: FailDuplicateTable, Line: t.Line,
				Message: fmt.Sprintf("table %s is declared %d times in one module", t.RustName, len(tables))}
		}
	}
}
