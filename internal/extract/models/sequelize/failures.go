package sequelize

import (
	"fmt"

	"github.com/graphvein/graphvein/internal/contract"
)

// This file lists the provable failures of row SQ-P1 (the
// row's refusal column names every failure the code refuses, and
// TestSequelizeFailureKinds keeps the two 1:1) and the load-order rule for
// failures that throw while the models load.

// failureKind names one provable failure of row SQ-P1.
type failureKind string

// The failure kinds; failureKinds holds the refusal-cell item of each.
const (
	failSameTable           failureKind = "same table name"
	failIndexName           failureKind = "index name"
	failIDNoPK              failureKind = "id without primaryKey"
	failNoType              failureKind = "no type"
	failEnumNoValues        failureKind = "ENUM without values"
	failTimestampName       failureKind = "timestamp name"
	failAutoIncrementTwice  failureKind = "two autoIncrement"
	failIndexNoFields       failureKind = "index without fields"
	failMissingKey          failureKind = "missing target key"
	failEnumLabel           failureKind = "ENUM label"
	failEnumDefault         failureKind = "ENUM default"
	failFnDefault           failureKind = "fn default"
	failVirtualUnique       failureKind = "VIRTUAL unique"
	failColumnTwice         failureKind = "column twice"
	failSerialType          failureKind = "SERIAL type"
	failSerialDefault       failureKind = "SERIAL default"
	failSqliteAutoIncrement failureKind = "sqlite AUTOINCREMENT"
	failCrossSchema         failureKind = "cross schema"
	failMissingColumn       failureKind = "missing column"
	failIndexColumn         failureKind = "index column"
	failKeyNotUnique        failureKind = "key not unique"
	failMissingTable        failureKind = "missing table"
)

// failureInfo is a failure kind's item in SQ-P1's refusal column,
// whether Sequelize throws while the models load (load) rather than the
// database rejecting a statement of sync, and whether the table is still
// created (kept: a CREATE INDEX fails after it, or another model of that
// name created it).
type failureInfo struct {
	text string
	load bool
	kept bool
}

// failureKinds are the items of SQ-P1's refusal column, in its order.
var failureKinds = []struct {
	kind failureKind
	info failureInfo
}{
	{failSameTable, failureInfo{text: "two models of the group with one table name", kept: true}},
	{failIndexName, failureInfo{text: "two indexes of a table, or of two tables in one schema, with one explicit name", kept: true}},
	{failIDNoPK, failureInfo{text: "an `id` attribute without `primaryKey` when no attribute is a primary key (load)", load: true}},
	{failNoType, failureInfo{text: "an attribute without a type (load)", load: true}},
	{failEnumNoValues, failureInfo{text: "an ENUM without values (load)", load: true}},
	{failTimestampName, failureInfo{text: "a `createdAt`, `updatedAt` or `deletedAt` option that is an empty string, `null` or a number (load)", load: true}},
	{failAutoIncrementTwice, failureInfo{text: "more than one `autoIncrement` attribute (load)", load: true}},
	{failIndexNoFields, failureInfo{text: "an index without `fields` (load)", load: true}},
	{failMissingKey, failureInfo{text: "a belongsTo `targetKey` or hasOne `sourceKey` that is not an attribute of the other model (load)", load: true}},
	{failEnumLabel, failureInfo{text: "an ENUM label that is not a string literal on postgres"}},
	{failEnumDefault, failureInfo{text: "a default outside the ENUM's labels on postgres, mysql and mariadb"}},
	{failFnDefault, failureInfo{text: "a `fn(...)` default on sqlite"}},
	{failVirtualUnique, failureInfo{text: "a VIRTUAL attribute with `unique`"}},
	{failColumnTwice, failureInfo{text: "two attributes with one column name"}},
	{failSerialType, failureInfo{text: "`autoIncrement` on a type other than INTEGER, BIGINT or SMALLINT on postgres"}},
	{failSerialDefault, failureInfo{text: "`autoIncrement` with a default on postgres"}},
	{failSqliteAutoIncrement, failureInfo{text: "`autoIncrement` on a sqlite primary key that is not the single INTEGER PRIMARY KEY"}},
	{failCrossSchema, failureInfo{text: "an association or join table from a model with a `schema` to a target without one on postgres"}},
	{failMissingColumn, failureInfo{text: "`references` naming a column the referenced table does not have"}},
	{failIndexColumn, failureInfo{text: "an index naming a column the table does not have", kept: true}},
	{failKeyNotUnique, failureInfo{text: "a `references` key, belongsTo `targetKey` or hasOne/hasMany `sourceKey` that is provably neither the primary key nor a unique column of the other table (not sqlite)"}},
	{failMissingTable, failureInfo{text: "`references` to a table of the group that sync provably does not create (not sqlite)"}},
}

func (k failureKind) info() failureInfo {
	for _, f := range failureKinds {
		if f.kind == k {
			return f.info
		}
	}
	panic("unknown failure kind " + string(k))
}

// loadPos is where a model definition or an association call runs while
// the models load: a module-level statement of file (line, column), and
// within it an inner position (a define in the factory that statement
// calls; the order of an association call in an associate loop).
type loadPos struct {
	file      string
	line, col int
	inner     [2]int
	// what locates the definition or call for a reason.
	what string
}

func posAt(file string, s contract.Span) loadPos {
	return loadPos{file: file, line: s.StartLine, col: s.StartColumn}
}

// modelPos is where a model's definition runs: its module-level statement,
// or the factory call that runs its define.
func modelPos(m *Model) loadPos {
	if m.Factory != nil {
		p := posAt(m.Factory.CallFile, m.Factory.Call.Span)
		p.inner = [2]int{m.Span.StartLine, m.Span.StartColumn}
		p.what = spanLine(m.File, m.Span)
		return p
	}
	p := posAt(m.File, m.Span)
	p.what = spanLine(m.File, m.Span)
	return p
}

// assocPos is where an association call runs: its module-level statement,
// or the associate loop that runs it (order: its index among the group's
// association calls, which follow the loop's execution order).
func (gr *group) assocPos(a *Association) loadPos {
	if a.Context == "associate" && a.LoopFile != "" {
		p := posAt(a.LoopFile, a.LoopSpan)
		for i, x := range gr.g.Associations {
			if x == a {
				p.inner = [2]int{i, 0}
			}
		}
		p.what = spanLine(a.File, a.Span)
		return p
	}
	p := posAt(a.File, a.Span)
	p.what = spanLine(a.File, a.Span)
	return p
}

// order compares two load positions: -1 when a runs before b, 1 after,
// 0 when the order is not proven (different modules, whose load order
// depends on the entry point and on what each module requires).
func order(a, b loadPos) int {
	if a.file != b.file {
		return 0
	}
	x := [4]int{a.line, a.col, a.inner[0], a.inner[1]}
	y := [4]int{b.line, b.col, b.inner[0], b.inner[1]}
	for i := range x {
		switch {
		case x[i] < y[i]:
			return -1
		case x[i] > y[i]:
			return 1
		}
	}
	return 0
}

// afterThrow says whether code at p runs after a load-time throw of the
// group (after: proven after one; why the order to one is not proven
// otherwise; both empty: before every throw). Loading stops at the first
// throw, so what runs after it never runs, and what is not proven to run
// before it may not.
func (gr *group) afterThrow(p loadPos) (after, unproven string) {
	for _, t := range gr.throws {
		switch order(p, t) {
		case 1:
			return "it runs after " + t.what + ", which throws while the models load", ""
		case 0:
			if p.file == t.file && p.line == t.line && p.col == t.col && p.inner == t.inner {
				continue
			}
			if unproven == "" {
				unproven = fmt.Sprintf("%s throws while the models load, and this definition is not proven to run before it (another module, whose load order is not read)", t.what)
			}
		}
	}
	return "", unproven
}
