package gorm

import (
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/extract/models"
)

// fkInfo is a table's foreign keys (row GO-R3): the keys of each column,
// the columns whose keys are not known and why, or allWhy when no column's
// keys are known.
type fkInfo struct {
	byCol          map[*gfield][]*models.ForeignKey
	colWhy         map[*gfield]*fkWhy
	unknown        []fkWhy
	allWhy, allRow string
}

// fkWhy is why a foreign key is not known.
type fkWhy struct {
	row, file string
	line      int
	why       string
}

// validActions are the referential actions a constraint tag may name.
var validActions = map[string]bool{"CASCADE": true, "SET NULL": true, "SET DEFAULT": true, "RESTRICT": true, "NO ACTION": true}

// action is a constraint tag's action as the catalog shows it: the
// database default NO ACTION when the tag names none (GORM writes no
// clause), nil for a text the port does not read.
func action(v string) any {
	v = strings.ToUpper(strings.Join(strings.Fields(v), " "))
	switch {
	case v == "":
		return "NO ACTION"
	case validActions[v]:
		return v
	}
	return nil
}

// foreignKeys builds a table's foreign keys: every constraint of its own
// relations and of the has-relations registered on it whose Schema is the
// table (CreateTable, migrator.go:277-290; an AutoMigrate of an existing
// table adds the missing ones, migrator.go:168-180). A registered relation
// is on the table when its owner is parsed before a call that creates or
// migrates the table; where the migrate order does not
// decide that, the key's columns are unknown.
func (b *builder) foreignKeys(mt *mtable, sc *gschema) fkInfo {
	cfg := b.m.Config
	out := fkInfo{byCol: map[*gfield][]*models.ForeignKey{}, colWhy: map[*gfield]*fkWhy{}}
	switch {
	case cfg.IgnoreRelationships.Known && cfg.IgnoreRelationships.Value || cfg.DisableForeignKeys.Known && cfg.DisableForeignKeys.Value:
		// No constraint is created (G2): no foreign keys, proven.
		b.tag("GO-R3")
		return out
	case !cfg.DisableForeignKeys.Known:
		out.allWhy, out.allRow = "whether the configuration creates foreign keys is not known: "+cfg.DisableForeignKeys.Reason, "GO-K2"
	case !cfg.IgnoreRelationships.Known:
		out.allWhy, out.allRow = "whether the configuration reads relations is not known: "+cfg.IgnoreRelationships.Reason, "GO-K2"
	case sc.FKWhy != "":
		out.allWhy, out.allRow = sc.FKWhy, "D"
	case len(b.m.Incomplete) > len(b.m.RawDDL):
		out.allWhy, out.allRow = "a migrate site that is not read may parse a model whose has-relations add foreign keys to this table", "D"
	case len(b.m.RawDDL) > 0:
		out.allWhy, out.allRow = "DDL text the application runs ("+strings.Join(b.m.RawDDL, ", ")+") may add foreign keys to this table", "GO-K3"
	}
	if out.allWhy != "" {
		b.tag(out.allRow)
		return out
	}
	b.tag("GO-R3")
	type item struct {
		rel        *grel
		registered bool
	}
	var items []item
	for _, r := range sc.Rels {
		items = append(items, item{r, false})
	}
	for _, r := range sc.Registered {
		items = append(items, item{r, true})
	}
	for _, it := range items {
		r := it.rel
		if r.Field.IgnoreMig {
			continue
		}
		con := b.w.constraint(r)
		if con == nil || con.Schema != sc {
			continue
		}
		b.tag("GO-R1")
		vis := triYes
		if it.registered {
			vis = b.w.registered(mt, r)
		}
		var why string
		switch {
		case vis == triNo:
			continue
		case vis == triUnknown:
			why = "the " + strings.ReplaceAll(r.Kind, "_", "-") + " relation " + r.Schema.Name + "." + r.Name + " adds this foreign key only if " + r.Schema.Name + " is parsed before the table is created, and the migrate order is not proven (GO-V6)"
		case con.Ref.Table == "":
			why = "the referenced table's name is not known: " + con.Ref.TableWhy
		}
		local := make([]string, len(con.FKs))
		refs := make([]string, len(con.Refs))
		for i, f := range con.FKs {
			local[i], refs[i] = f.DBName, con.Refs[i].DBName
			if f.IgnoreMig || f.DBName == "" || con.Refs[i].DBName == "" {
				why = "the foreign key pairs a field that has no column"
			}
		}
		if why != "" {
			w := fkWhy{row: "GO-R1", file: r.Field.File, line: r.Field.Line, why: why}
			if !strings.Contains(why, "not proven") {
				w.row = "GO-R3"
			}
			out.unknown = append(out.unknown, w)
			for _, f := range con.FKs {
				w := w
				out.colWhy[f] = &w
			}
			continue
		}
		name := b.fkName(con)
		del, upd := action(con.OnDelete), action(con.OnUpdate)
		for i, f := range con.FKs {
			out.byCol[f] = append(out.byCol[f], &models.ForeignKey{
				Table: con.Ref.Table, Column: refs[i], OnDelete: del, Columns: local, RefColumns: refs, OnUpdate: upd, Name: name,
				File: r.Field.File, Line: r.Field.Line,
			})
		}
	}
	return out
}

// fkName is the constraint name the catalog keeps: one name on every
// dialect of the group, nil on sqlite (pragma foreign_key_list has none),
// for a name the dialects compute differently, and for one the database
// truncates (postgres over 63 bytes) or rejects (mysql over 64).
func (b *builder) fkName(con *gcon) *string {
	if b.has(DialectSQLite) {
		return nil
	}
	name := ""
	for i, d := range b.m.Dialects {
		n := con.Name[d]
		if n == "" || i > 0 && n != name || d == DialectPostgres && len(n) > 63 || d == DialectMySQL && len(n) > 64 {
			return nil
		}
		name = n
	}
	return &name
}

// fieldType is the field as its column is created, given the relations
// that copy a referenced type onto it (relationship.go:592-604): the
// copied state when every copy is made before the table is created and
// migrated, the field's own state when none is, else unknown (why).
func (b *builder) fieldType(mt *mtable, sc *gschema, f *gfield) (*gfield, string) {
	if why := sc.TypeWhy[f]; why != "" {
		return f, why
	}
	if src := f.joinSrc; src != nil {
		for _, c := range src.copies {
			if c.Changed {
				return f, "the join column copies the type of " + src.Name + ", whose own type a relation changes"
			}
		}
	}
	owners := map[*gschema]bool{}
	var owner *gschema
	for _, c := range f.copies {
		if c.Changed {
			owners[c.Owner] = true
			owner = c.Owner
		}
	}
	switch {
	case len(owners) == 0:
		return f, ""
	case len(owners) > 1:
		var names []string
		for o := range owners {
			names = append(names, o.Name)
		}
		sort.Strings(names)
		return f, "several relations (" + strings.Join(names, ", ") + ") copy a type onto it in an order the port does not decide"
	}
	switch b.w.copied(mt, owner) {
	case triYes:
		b.tag("GO-R1")
		return f, ""
	case triNo:
		g := *f
		before := f.copies[0].Before
		g.DataType, g.GORMDataType, g.Size = before.DataType, before.GORMDataType, before.Size
		return &g, ""
	}
	return f, "a relation of " + owner.Name + " copies a type onto it (relationship.go:592-604) only once " + owner.Name + " is parsed, and the migrate order does not decide whether that is before the table is created"
}

// joinAgree checks that the join schemas created under one name (a join
// table declared from both sides) have the same columns with
// the same types; the records use the first.
func (b *builder) joinAgree(mt *mtable) string {
	first := mt.schemas[0]
	for _, s := range mt.schemas[1:] {
		if len(s.DBNames) != len(first.DBNames) {
			return "the many2many relations that declare it give different columns"
		}
		for _, n := range first.DBNames {
			f, g := first.ByDB[n], s.ByDB[n]
			if g == nil {
				return "the many2many relations that declare it give different columns"
			}
			if f.DataType != g.DataType || f.GORMDataType != g.GORMDataType || f.Size != g.Size {
				return "the many2many relations that declare it give the column " + n + " different types"
			}
		}
	}
	return ""
}
