package diesel

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/models"
)

// This file is the claim pass: rows DI-T1, DI-C1, DI-C2, DI-S1,
// DI-J1, DI-J2, DI-U2 and the default branch D, over one schema file whose
// discovery (DI-K1), items (DI-P1) and provenance (DI-G1) are read. It
// builds the model kit's records; the kit resolves the foreign keys and
// writes the nodes.

// group is the claim pass over one schema file: one model group.
type group struct {
	in    Input
	f     *File
	sf    *SchemaFile
	prov  *Provenance
	steps *models.Steps
	// verification is the group's: oracle on postgres (the oracle compared
	// these fact kinds on Postgres 17), mapped on mysql, sqlite and an
	// unknown dialect.
	verification string
	tables       []*models.Table
	// byRust finds a table by its module path and Rust name, for the
	// joinable! lines of the same module.
	byRust map[string]*tableRec
	// sqlTypes are the structs of each sql_types module, by the path of the
	// module that holds it and the struct name.
	sqlTypes map[string]map[string]*SQLType
	// typeClaimed is set once a Postgres type is claimed: the group then
	// states DI-V3.
	typeClaimed bool
	// newerPin is set for a generated file whose diesel pin is newer than
	// the verified diesel_cli versions: the group states DI-V8.
	newerPin bool
	// olderPin is set for a generated file whose diesel pin is older than
	// the verified diesel_cli versions, or not proven: the group states
	// DI-V9 (Revision 2, A1).
	olderPin bool
	// before21 is set unless the file proves it was written by
	// diesel_cli 2.1 or later (proves21), whatever the diesel pin:
	// diesel_cli 2.0 writes no #[max_length] and keeps joinable! lines of
	// composite foreign keys, so a length-taking type without
	// #[max_length] is its family and no joinable! is a foreign key
	// (Revision 2, A1 and addendum; docs "Older diesel_cli"), except on
	// SQLite, whose joinable! set 2.0 writes as 2.1 does (addendum 2).
	before21 bool
}

// tableRec is one table! as the claim pass read it.
type tableRec struct {
	item *Item
	t    *Table
	rec  *models.Table
	// known is true when the table's facts other than names are claimed:
	// a generated name line and no patch removal inside the block (O-a).
	known bool
	// pkKnown is true when the key list is claimed as the primary key.
	pkKnown bool
	cols    map[string]*colRec
}

// colRec is one column definition as the claim pass read it.
type colRec struct {
	c   *Column
	rec *models.Column
	// known is true when the column's line is generated and its facts were
	// read (nullability at least).
	known bool
}

// Row ids the claim pass tags and writes.
const (
	rowT1 = "DI-T1"
	rowC1 = "DI-C1"
	rowC2 = "DI-C2"
	rowS1 = "DI-S1"
	rowJ1 = "DI-J1"
	rowJ2 = "DI-J2"
	rowU2 = "DI-U2"
	rowG1 = "DI-G1"
	rowD  = "D"
)

// readGroup reads one discovered schema file into its model group.
func readGroup(in Input, f *File, steps *models.Steps) *group {
	src := in.Sources[f.Path]
	sf, diags := ReadFile(in, f.Path, src)
	steps.Diagnostics = append(steps.Diagnostics, diags...)
	if sf.LexError != nil {
		in.tag(rowD)
		return nil
	}
	prov, diags := ReadProvenance(in, f, src)
	steps.Diagnostics = append(steps.Diagnostics, diags...)
	g := &group{in: in, f: f, sf: sf, prov: prov, steps: steps, byRust: map[string]*tableRec{}, sqlTypes: map[string]map[string]*SQLType{}}
	// DI-V8 for a pin newer than the verified versions (Revision 1 O7,
	// addendum 4): the claims stand, and the group lists the assumption
	// that names the versions the rules were verified at.
	g.newerPin = prov.Generated && f.Version != "" && newerThan(f.Version, CLIVerified[len(CLIVerified)-1])
	// The diesel pin does not say which diesel_cli wrote the file (DI-V6):
	// the diesel_cli 2.0 rules apply unless the file itself shows 2.1 or
	// later output, and DI-V9 is listed then too (Revision 2 addendum).
	g.before21 = !(f.Configured && prov.Generated && provesCLI21(sf.Items, prov))
	g.olderPin = prov.Generated && (olderThan(f.Version, CLIVerified[0]) || g.before21)
	g.verification = models.VerificationMapped
	if f.Dialect == "postgres" && f.Configured && prov.Generated {
		// Names on hand-written lines (a hand-written or unconfigured
		// file) are mapped, not oracle (Revision 2, A6): the oracle
		// compared generated lines only.
		g.verification = models.VerificationOracle
	}
	g.collectTypes(sf.Items)
	g.readItems(sf.Items)
	g.readJoinables(sf.Items)
	g.tableList()
	return g
}

// CLIVerified are the diesel_cli versions whose print-schema rules the
// contract cites and was checked against (DI-V6, DI-V8; Revision 1 O7): 2.3.14
// by the investigation, 2.4.0 by step DB4 (docs/extract/schema-diesel.md
// "diesel_cli 2.4.0").
var CLIVerified = []string{"2.3.14", "2.4.0"}

// OlderCLIChecked are the older diesel_cli releases whose print-schema
// output was checked against the rules the contract cites (DI-V9;
// Revision 2, A1; docs/extract/schema-diesel.md "Older diesel_cli").
var OlderCLIChecked = []string{"2.0.1", "2.1.1", "2.2.0"}

// provesCLI21 reports whether a generated file shows output diesel_cli
// 2.0 never writes, so it was written by diesel_cli 2.1 or later (row
// DI-G1, Revision 2 addendum): a #[max_length = n] on a generated column
// (no max_length anywhere in the 2.0.1 sources), or a `///` doc comment on
// a generated table or column that is not 2.0's generated text (2.0.1
// writes only that text, inference.rs:198-224, and reads no database
// comment; 2.1.1 writes database comments, pg.rs:74-149).
func provesCLI21(items []Item, prov *Provenance) bool {
	for i := range items {
		it := &items[i]
		if it.Kind == ItemModule && provesCLI21(it.Items, prov) {
			return true
		}
		t := it.Table
		if it.Kind != ItemTable || t == nil {
			continue
		}
		if !prov.HandWrittenSpan(t.FirstLine, t.Line) && laterDocs(docAttrs(t.Attrs), cli20TableDoc) {
			return true
		}
		for j := range t.Columns {
			c := &t.Columns[j]
			if prov.HandWrittenSpan(c.FirstLine, c.Line) {
				continue
			}
			if c.MaxLength != nil || laterDocs(c.Docs, cli20ColumnDoc) {
				return true
			}
		}
	}
	return false
}

// diesel_cli 2.0.1's generated doc text for a table and a column
// (inference.rs:198-224), one pattern per `///` line.
var (
	cli20TableDoc  = docShape("Representation of the `.*` table\\.", "", "\\(Automatically generated by Diesel\\.\\)")
	cli20ColumnDoc = docShape("The `.*` column of the `.*` table\\.", "", "Its SQL type is `.*`\\.", "", "\\(Automatically generated by Diesel\\.\\)")
)

func docShape(lines ...string) []*regexp.Regexp {
	out := make([]*regexp.Regexp, len(lines))
	for i, l := range lines {
		out[i] = regexp.MustCompile("^" + l + "$")
	}
	return out
}

func docAttrs(attrs []Attr) []Attr {
	var out []Attr
	for _, a := range attrs {
		if a.Doc {
			out = append(out, a)
		}
	}
	return out
}

// laterDocs reports doc comments that diesel_cli 2.0 cannot have written:
// `///` lines (diesel_cli writes no #[doc] attribute) that are not 2.0's
// generated text.
func laterDocs(docs []Attr, cli20 []*regexp.Regexp) bool {
	if len(docs) == 0 {
		return false
	}
	for _, d := range docs {
		if d.Value != nil {
			// #[doc = "..."]: no diesel_cli writes it, so it proves nothing.
			return false
		}
	}
	if len(docs) != len(cli20) {
		return true
	}
	for i, d := range docs {
		// print-schema writes "///" and a space before a non-empty line.
		if !cli20[i].MatchString(strings.TrimPrefix(d.Text, " ")) {
			return true
		}
	}
	return false
}

// olderThan reports a version older than base by its numeric core; a
// version that is empty or not three numbers counts as older (the pin is
// not proven, so the older rules apply).
func olderThan(v, base string) bool {
	if _, ok := versionCore(v); !ok {
		return true
	}
	return newerThan(base, v)
}

// newerThan reports a version "M.m.p[-pre]" newer than base by its numeric
// core; a pre-release of a newer core counts as newer, a version that is
// not three numbers never does.
func newerThan(v, base string) bool {
	a, okA := versionCore(v)
	b, okB := versionCore(base)
	if !okA || !okB {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

func versionCore(v string) ([3]int, bool) {
	var out [3]int
	core, _, _ := strings.Cut(v, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// group returns the kit group: the discovery assumptions plus DI-V2 and
// DI-V6 for a generated file (and DI-V8 when its pin is newer than the
// verified diesel_cli versions) and DI-V3 once a Postgres type is claimed.
func (g *group) group() models.Group {
	have := map[string]bool{}
	for _, a := range g.f.Assumptions {
		have[a] = true
	}
	if g.prov.Generated {
		have[AssumeGenerated], have[AssumeCLI] = true, true
	}
	if g.typeClaimed {
		have[AssumeModifiers] = true
	}
	if g.newerPin {
		have[AssumeNewerPin] = true
	}
	if g.olderPin {
		have[AssumeOlderPin] = true
	}
	var assumptions []string
	for _, a := range Assumptions {
		if have[a] {
			assumptions = append(assumptions, a)
		}
	}
	dialect := g.f.Dialect
	if dialect == "unknown" {
		dialect = ""
	}
	return models.Group{Tool: Tool, Root: g.f.Path, Pins: g.f.Pins, Assumptions: assumptions, Dialect: dialect, Verification: g.verification}
}

func modulePath(module []string) string { return strings.Join(module, "::") }

func rustKey(module []string, name string) string { return modulePath(module) + "\x00" + name }

// collectTypes records the structs of every sql_types module (row DI-S1).
func (g *group) collectTypes(items []Item) {
	for i := range items {
		it := &items[i]
		if it.Kind != ItemModule {
			continue
		}
		if it.Name == "sql_types" {
			key := modulePath(it.Module)
			for j := range it.Items {
				if s := it.Items[j]; s.Kind == ItemStruct && s.Struct != nil {
					if g.sqlTypes[key] == nil {
						g.sqlTypes[key] = map[string]*SQLType{}
					}
					g.sqlTypes[key][s.Name] = s.Struct
				}
			}
			continue
		}
		g.collectTypes(it.Items)
	}
}

// unread reports a whole definition no row reads (row D): a marker that
// also makes the group's table list not proven complete.
func (g *group) unread(line int, format string, args ...any) {
	g.in.tag(rowD)
	g.steps.Diag(g.f.Path, "info", codeUnread, fmt.Sprintf("%s:%d: ", g.f.Path, line)+fmt.Sprintf(format, args...)+" (row D); it is not read")
}

// readItems walks the file's items: tables (DI-T1), the sql_types module
// (DI-S1), allow_tables_to_appear_in_same_query! (DI-J2) and everything
// else (D). joinable! lines are read once every table is (readJoinables).
func (g *group) readItems(items []Item) {
	for i := range items {
		it := &items[i]
		switch it.Kind {
		case ItemTable:
			g.steps.Guard(g.f.Path, "table", fmt.Sprintf("Diesel table at %s:%d", g.f.Path, it.Line), func() { g.readTable(it) })
		case ItemModule:
			if it.Name == "sql_types" {
				for j := range it.Items {
					switch s := &it.Items[j]; s.Kind {
					case ItemStruct, ItemUse, ItemInner:
					default:
						g.unread(s.Line, "%s in the sql_types module", itemLabel(s))
					}
				}
				continue
			}
			if it.Name == "rust_types" {
				g.readRustTypes(it)
				continue
			}
			g.readItems(it.Items)
		case ItemAllowTables:
			if it.Unread != "" {
				g.unread(it.Line, "%s! (%s)", it.Macro, it.Unread)
				continue
			}
			// DI-J2: a Rust-level permission, never a fact.
			g.in.tag(rowJ2)
		case ItemJoinable:
			if it.Unread != "" {
				g.unread(it.Line, "%s! (%s)", it.Macro, it.Unread)
			}
		case ItemStruct:
			g.unread(it.Line, "struct %s outside a sql_types module", it.Name)
		case ItemEnum:
			g.unread(it.Line, "%s outside a rust_types module", it.Reason)
		case ItemOther:
			g.unread(it.Line, "%s", itemLabel(it))
		case ItemUse, ItemInner:
			// No definition.
		}
	}
}

// readRustTypes reads diesel_cli 2.4's `pub mod rust_types { ... }` (row
// DI-S1; print_schema.rs:1333-1349): the Rust enums it generates for the
// enum types of the sql_types module are no definition (no marker); any
// other item in it is read as anywhere else.
func (g *group) readRustTypes(it *Item) {
	g.in.tag("DI-S1/pub mod rust_types { ... }")
	for j := range it.Items {
		switch s := &it.Items[j]; s.Kind {
		case ItemEnum, ItemUse, ItemInner:
		default:
			g.readItems(it.Items[j : j+1])
		}
	}
}

func itemLabel(it *Item) string {
	switch it.Kind {
	case ItemOther, ItemEnum:
		return it.Reason
	case ItemModule:
		return "module " + it.Name
	case ItemStruct:
		return "struct " + it.Name
	case ItemTable, ItemJoinable, ItemAllowTables:
		return it.Macro + "!"
	}
	return "an item"
}

// tagTableForms reports the DI-T1 forms a table uses.
func (g *group) tagTableForms(it *Item) {
	t := it.Table
	switch it.Macro {
	case "table":
		g.in.tag("DI-T1/table!")
	case "diesel::table":
		g.in.tag("DI-T1/diesel::table!")
	case "::diesel::table":
		g.in.tag("DI-T1/::diesel::table!")
	}
	if len(it.Module) > 0 {
		g.in.tag("DI-T1/pub mod <ident> { ... }")
	}
	if len(t.Uses) > 0 {
		g.in.tag("DI-T1/use")
	}
	for _, a := range t.Attrs {
		if a.Name == "sql_name" && !a.Doc {
			g.in.tag(`DI-T1/#[sql_name = "<s>"]`)
		}
	}
	if t.Schema != "" {
		g.in.tag("DI-T1/schema.name")
	}
	if t.HasKeys && len(t.Keys) == 0 {
		g.in.tag("DI-T1/()")
	}
	raw := t.RustRaw
	for _, c := range t.Columns {
		raw = raw || c.RustRaw
	}
	if raw {
		g.in.tag("DI-T1/r#")
	}
}

// rowidAliases are SQLite's names of the implicit rowid key that
// print-schema prepends as a column when a table declares no primary key
// (diesel_cli 2.3.14 infer_schema_internals/sqlite.rs:171-190, :301,
// :330-343).
var rowidAliases = map[string]bool{"rowid": true, "oid": true, "_rowid_": true}

// implicitRowid reports, on sqlite, the key column print-schema may have
// prepended for the implicit rowid (O-b): a single key named like a rowid
// alias, written `Integer` or `BigInt` (sqlite_integer_primary_key_is_bigint)
// and not nullable.
func implicitRowid(dialect string, t *Table) *Column {
	if dialect != "sqlite" || len(t.Keys) != 1 || !rowidAliases[t.Keys[0]] {
		return nil
	}
	for i := range t.Columns {
		c := &t.Columns[i]
		if c.RustName == t.Keys[0] && c.SQLName == c.RustName && c.Type != nil && len(c.OtherAttrs) == 0 {
			if s := c.Type.String(); s == "Integer" || s == "BigInt" {
				return c
			}
		}
	}
	return nil
}

// readTable builds one table's records (rows DI-T1, DI-C1, DI-C2; DI-U2 for
// an unconfigured file; DI-G1 selects).
func (g *group) readTable(it *Item) {
	t := it.Table
	switch {
	case t.Failure != nil:
		// DI-P1: ReadFile reported it; nothing is claimed.
		return
	case t.Unread != "":
		g.unread(t.Line, "%s! %s (%s)", it.Macro, tableLabel(t), t.Unread)
		return
	case len(t.OtherAttrs) > 0:
		g.unread(t.Line, "%s! %s with the attribute %s", it.Macro, tableLabel(t), strings.Join(t.OtherAttrs, ", "))
		return
	case t.Schema != "" && strings.HasPrefix(t.SQLName, t.Schema+"."):
		// diesel_cli 2.3 writes a renamed table of a schema section as
		// `#[sql_name = "<schema>.<name>"] <schema>.<ident>`
		// (print_schema.rs:805-811 uses full_sql_name; 2.4.0 writes the
		// name alone), so the line names neither the table Diesel queries
		// ("<schema>"."<schema>.<name>") nor, for certain, the database's
		// (the oracle's probe run).
		g.unread(t.Line, "%s! %s with #[sql_name = %q], a schema-qualified name (diesel_cli 2.3 writes one for a renamed table of a schema section)", it.Macro, tableLabel(t), t.SQLName)
		return
	}
	g.tagTableForms(it)
	f := g.f
	// The model name is the table's Rust path in the file, modules joined
	// with "." (a node identity allows no "::" or "#"); a raw identifier
	// is written without r# (r#name and name are one Rust identifier).
	model := strings.Join(append(append([]string(nil), it.Module...), t.RustName), ".")
	rec := &models.Table{
		Identity: "table:" + f.Path + "#" + model, File: f.Path, Name: t.SQLName, Model: model, ModelLine: t.Line,
		Span: spanOf(it.Line, it.EndLine), ElementsUnknown: true, Verification: g.verification,
	}
	// Schema (DI-T1, DI-K1): the written `schema.` prefix, else the
	// section's literal schema, else none (the dialect's default schema).
	switch {
	case t.Schema != "":
		s := t.Schema
		rec.Schema, rec.SchemaKnown = &s, true
	case f.Configured:
		rec.SchemaKnown = true
		if f.Section != nil && f.Section.Schema != nil {
			s := *f.Section.Schema
			rec.Schema = &s
		}
	}
	tr := &tableRec{item: it, t: t, rec: rec, cols: map[string]*colRec{}}
	nameRow := rowC1
	generated := !g.prov.HandWrittenSpan(t.FirstLine, t.Line)
	if !generated || !f.Configured {
		// Names on hand-written lines are mapped (Revision 2, A6).
		rec.Verification = models.VerificationMapped
	}
	switch {
	case !f.Configured:
		nameRow = rowU2
		g.in.tag(rowU2)
		g.tableReason(rec, rowU2, "column_set", t.Line, "the file is not configured by diesel.toml and may be stale (DI-V7): only table and column names are claimed")
	case !generated:
		g.tableReason(rec, rowT1, "column_set", t.Line, "the table is written by hand (%s): its kind (a view, a temporary or an imaginary table is valid Diesel), primary key and column set are unknown; only names are claimed", g.handWhySpan(t.FirstLine, t.Line))
	case g.prov.RemovedWithin(t.BodyLine, t.EndLine):
		g.tableReason(rec, rowG1, "column_set", t.Line, "the patch_file %s removes lines inside this table! (%s): every fact of the table other than its table and column names is unknown", f.Section.PatchPath, g.removedHunks(t))
	default:
		tr.known = true
	}
	var rowid *Column
	if tr.known {
		g.tableReason(rec, rowT1, "constraint_set", t.Line, "schema.rs states no defaults, unique constraints, indexes or check constraints, and foreign keys only as the joinable! lines print-schema writes (one per single-column key to a single-column primary key; F5): the constraint set and every other foreign key are unknown")
		rowid = implicitRowid(f.Dialect, t)
		switch {
		case !t.HasKeys:
			g.tableReason(rec, rowT1, "constraint_set", t.Line, "no key list: print-schema always writes one, so the primary key is unknown")
		case len(t.Keys) == 0:
			g.tableReason(rec, rowT1, "constraint_set", t.Line, "an empty key list (): the primary key is unknown")
		case rowid != nil:
			g.tableReason(rec, rowT1, "constraint_set", t.Line, "the key column %s may be SQLite's implicit rowid that print-schema prepends for a table without a declared primary key: it is not claimed as a column, and the primary key is unknown", rowid.RustName)
		default:
			tr.pkKnown = true
			if c := g.keyWithAddedAttrs(t); c != nil {
				// A patch-added attribute above a key column (a #[sql_name]
				// changes the name the key list stands for): the primary
				// key is not the generated one (Revision 2, A3).
				tr.pkKnown = false
				g.tableReason(rec, rowG1, "constraint_set", c.FirstLine, "the patch_file %s adds an attribute line to the key column %s: the primary key is unknown", f.Section.PatchPath, c.RustName)
			}
		}
	}
	rec.PrimaryKeyOpen = !tr.pkKnown
	if tr.pkKnown {
		for _, k := range t.Keys {
			for _, c := range t.Columns {
				if c.RustName == k {
					rec.PrimaryKey = append(rec.PrimaryKey, c.SQLName)
					break
				}
			}
		}
	}
	replaced := g.replacedColumns(t)
	complete := tr.known
	if rowid != nil {
		// O-b as Revision 1 addendum 3 changed it: the key column may be
		// the implicit rowid or a declared column of that name, so it is
		// not claimed, and a declared one is never hidden behind a
		// complete column set.
		complete = false
		g.tableReason(rec, rowT1, "column_set", rowid.Line, "the key column %s may be SQLite's implicit rowid that print-schema prepends, or a declared column of that name: it is not claimed, so the column set is not proven", rowid.RustName)
	}
	for i := range t.Columns {
		c := &t.Columns[i]
		if c == rowid {
			continue
		}
		cr := g.readColumn(tr, c, nameRow)
		if tr.known && tr.pkKnown && !cr.known && (!g.prov.Added[c.Line] || replaced[c.RustName]) && c.Unread == "" && len(c.OtherAttrs) == 0 {
			// A column line the patch rewrites (a changed type), or a
			// generated column line under a patch-added attribute, names a
			// column print-schema wrote: its key membership comes from the
			// generated key list (DI-T1), its type and nullability stay
			// unknown (hand-written, DI-C1).
			cr.rec.Attributes["primary_key"] = slices.Contains(t.Keys, c.RustName)
			cr.rec.Claims["primary_key"] = models.ClaimSource{Row: rowT1, Line: t.Line}
		}
		if tr.known && !cr.known && g.prov.Added[c.Line] && !replaced[c.RustName] {
			complete = false
			g.tableReason(rec, rowT1, "column_set", c.Line, "the patch_file %s adds the column line %s, which replaces no column print-schema wrote: the column set is not proven", f.Section.PatchPath, c.RustName)
		}
		tr.cols[c.RustName] = cr
		rec.Columns = append(rec.Columns, cr.rec)
	}
	rec.ColumnsComplete = complete
	g.byRust[rustKey(it.Module, t.RustName)] = tr
	g.tables = append(g.tables, rec)
}

func spanOf(first, last int) contract.Span { return contract.Span{StartLine: first, EndLine: last} }

func (g *group) tableReason(rec *models.Table, row, fact string, line int, format string, args ...any) {
	rec.Reasons = append(rec.Reasons, models.Reason{Row: row, Fact: models.Fact(fact), File: g.f.Path, Line: line, Reason: fmt.Sprintf(format, args...)})
}

func (g *group) columnReason(rec *models.Column, row string, fact *string, format string, args ...any) {
	rec.Reasons = append(rec.Reasons, models.Reason{Row: row, Fact: fact, File: g.f.Path, Line: rec.Line, Reason: fmt.Sprintf(format, args...)})
}

// keyWithAddedAttrs returns a key column whose attribute lines (above its
// name line) the patch adds, nil when there is none.
func (g *group) keyWithAddedAttrs(t *Table) *Column {
	for i := range t.Columns {
		c := &t.Columns[i]
		if slices.Contains(t.Keys, c.RustName) && c.FirstLine < c.Line && g.prov.HandWrittenSpan(c.FirstLine, c.Line-1) {
			return c
		}
	}
	return nil
}

// handWhy says why a line is hand-written (row DI-G1).
func (g *group) handWhy(line int) string {
	if !g.prov.Generated {
		return g.prov.Reason
	}
	return "the patch_file " + g.f.Section.PatchPath + " adds this line"
}

// handWhySpan says why an item spanning the lines first..last is
// hand-written (row DI-G1, Revision 2 A3).
func (g *group) handWhySpan(first, last int) string {
	if !g.prov.Generated || g.prov.Added[last] {
		return g.handWhy(last)
	}
	return "the patch_file " + g.f.Section.PatchPath + " adds a line between its first attribute and its name"
}

// removedHunks names the patch hunks whose non-benign removals lie inside
// a table's block.
func (g *group) removedHunks(t *Table) string {
	var hunks []string
	for _, r := range g.prov.Removals {
		if !r.Benign && r.After >= t.BodyLine && r.After < t.EndLine {
			h := fmt.Sprintf("hunk %d", r.Hunk+1)
			if !slices.Contains(hunks, h) {
				hunks = append(hunks, h)
			}
		}
	}
	return strings.Join(hunks, ", ")
}

// replacedColumns are the Rust names of the column lines that patch hunks
// remove inside a table's block: a column line the patch adds under one of
// these names replaces a generated column (a changed type) and adds none.
func (g *group) replacedColumns(t *Table) map[string]bool {
	out := map[string]bool{}
	for _, r := range g.prov.Removals {
		if r.After < t.BodyLine || r.After >= t.EndLine {
			continue
		}
		for _, l := range r.Lines {
			if m := columnLine.FindStringSubmatch(l); m != nil {
				out[m[1]] = true
			}
		}
	}
	return out
}

// readColumn builds one column's record (DI-C1, DI-C2).
func (g *group) readColumn(tr *tableRec, c *Column, nameRow string) *colRec {
	rec := &models.Column{Name: c.SQLName, Attribute: identKey(c.RustName, c.RustRaw), File: g.f.Path, Line: c.Line, Attributes: map[string]any{}, Claims: map[string]models.ClaimSource{}, ForeignKeysUnknown: true}
	// The identity keeps the Rust name (without r#) when #[sql_name]
	// renames the column, as Prisma's keeps its field name (ADR 0057).
	if c.RustName != rec.Name {
		rec.IdentityName = c.RustName
	}
	cr := &colRec{c: c, rec: rec}
	if c.SQLName != c.RustName {
		g.in.tag(`DI-C1/#[sql_name = "<s>"]`)
	}
	// The declared Diesel type, a Rust-side annotation (Revision 1 O5): the
	// kit's tool_type (ADR 0065), never claimed or verified.
	if c.Type != nil {
		rec.SetToolType(c.Type.String())
	}
	handWritten := !g.f.Configured || g.prov.HandWrittenSpan(c.FirstLine, c.Line)
	rec.Claims["name"] = models.ClaimSource{Row: nameRow}
	if handWritten {
		// Names on hand-written lines are mapped (Revision 2, A6).
		rec.Claims["name"] = models.ClaimSource{Row: nameRow, Verification: models.VerificationMapped}
	}
	g.in.tag(nameRow)
	switch {
	case len(c.OtherAttrs) > 0:
		g.columnReason(rec, rowC1, nil, "the attribute %s: only the column name is claimed", strings.Join(c.OtherAttrs, ", "))
		return cr
	case !g.f.Configured:
		return cr
	case !tr.known:
		// The table's reason says why; the column repeats it for its cells.
		g.columnReason(rec, rowC1, nil, "only the column name is claimed: %s", tableWhy(tr))
		return cr
	case handWritten:
		g.columnReason(rec, rowC1, nil, "the column is written by hand (%s): only the name is claimed", g.handWhySpan(c.FirstLine, c.Line))
		return cr
	}
	// A generated line of a table whose facts are known: its key
	// membership comes from the key list (DI-T1), nullability from DI-C1
	// and the type from DI-C2.
	if tr.pkKnown {
		rec.Attributes["primary_key"] = slices.Contains(tr.t.Keys, c.RustName)
		rec.Claims["primary_key"] = models.ClaimSource{Row: rowT1, Line: tr.t.Line}
	}
	if c.Unread != "" {
		g.in.tag(rowD)
		g.columnReason(rec, rowD, models.Fact("nullable"), "%s (row D): nullability and type unknown", c.Unread)
		g.columnReason(rec, rowD, models.Fact("type"), "%s (row D): nullability and type unknown", c.Unread)
		return cr
	}
	if c.MaxLength != nil {
		g.in.tag("DI-C1/#[max_length = <int>]")
	}
	inner, nullable, ok := splitNullable(c.Type)
	if !ok {
		why := fmt.Sprintf("the type %s is not a plain type path with an optional outermost Nullable<...>: nullability and type unknown", c.Type)
		g.columnReason(rec, rowC1, models.Fact("nullable"), "%s", why)
		g.columnReason(rec, rowC1, models.Fact("type"), "%s", why)
		return cr
	}
	cr.known = true
	if nullable {
		g.in.tag("DI-C1/Nullable<...>")
	}
	rec.Attributes["nullable"] = nullable
	rec.Claims["nullable"] = models.ClaimSource{Row: rowC1}
	switch ans := g.columnType(tr, inner, c.MaxLength); {
	case ans.typ != "":
		rec.Attributes["type"] = ans.typ
		rec.Claims["type"] = models.ClaimSource{Row: rowC2}
	case ans.family != "":
		// The type family with the modifiers print-schema drops unknown
		// (Revision 1 O4): type and those modifiers are written unknown.
		rec.SetTypeFamily(ans.family, ans.unknown, models.ClaimSource{Row: rowC2})
	default:
		rec.Attributes["type"] = nil
		g.columnReason(rec, rowC2, models.Fact("type"), "%s", ans.why)
	}
	return cr
}

func tableWhy(tr *tableRec) string {
	for _, r := range tr.rec.Reasons {
		if r.Fact != nil && *r.Fact == "column_set" {
			return r.Reason
		}
	}
	return "the table's facts are unknown"
}

// splitNullable removes an outermost `Nullable<T>` (DI-C1). ok is false
// for a type whose outermost form is not a bare path segment (a qualified
// path such as diesel::sql_types::Nullable<...>, which print-schema never
// writes) or a Nullable without exactly one argument.
func splitNullable(t *Type) (*Type, bool, bool) {
	if !bare(t) {
		return t, false, false
	}
	s := t.Segments[0]
	if s.Name != "Nullable" {
		return t, false, true
	}
	if len(s.Args) != 1 {
		return t, false, false
	}
	return s.Args[0], true, true
}

// bare reports a one-segment path without a leading `::`.
func bare(t *Type) bool { return t != nil && !t.Leading && len(t.Segments) == 1 }

// columnType is the DI-C2 branch: the database type of a generated
// column's type path (Nullable removed) on the group's dialect, or why it
// is unknown.
func (g *group) columnType(tr *tableRec, t *Type, maxLength *int) typeAnswer {
	switch g.f.Dialect {
	case "postgres":
		ans := g.pgType(tr, t, maxLength)
		if ans.why == "" {
			g.typeClaimed = true
		}
		return ans
	case "mysql":
		return g.mysqlType(tr, t, maxLength)
	case "sqlite":
		return typeUnknown(fmt.Sprintf("on sqlite print-schema writes the affinity class %s, from which the declared type cannot be recovered (dialect rule): the type is unknown", t))
	}
	return typeUnknown("the dialect is unknown (" + g.f.DialectReason + "): the type is not claimed")
}

// structFor returns the sql_types struct a table's column type ident
// names (row DI-S1): a struct of the sql_types module beside the table,
// imported by the table's `use super::sql_types::X` or
// `use super::sql_types::*`.
func (g *group) structFor(tr *tableRec, ident string) *SQLType {
	s := g.sqlTypes[modulePath(tr.item.Module)][ident]
	if s == nil {
		return nil
	}
	for _, u := range tr.t.Uses {
		switch {
		case u == "super::sql_types::*":
			g.in.tag("DI-S1/use super::sql_types::*")
			return s
		case u == "super::sql_types::"+ident:
			g.in.tag("DI-S1/use super::sql_types::X")
			return s
		case strings.HasPrefix(u, "super::sql_types::{") && strings.HasSuffix(u, "}"):
			for _, name := range strings.Split(strings.TrimSuffix(strings.TrimPrefix(u, "super::sql_types::{"), "}"), ",") {
				if strings.TrimSpace(name) == ident {
					g.in.tag("DI-S1/use super::sql_types::X")
					return s
				}
			}
		}
	}
	return nil
}

// structType is the DI-S1 branch: the type a sql_types struct names on
// the group's dialect (backend), or why it is unknown.
func (g *group) structType(ident string, s *SQLType, backend string) (string, string) {
	g.in.tag(rowS1)
	switch {
	case g.prov.HandWrittenSpan(s.Line, s.NameLine):
		// A struct the patch adds or rewrites (its postgres_type name, for
		// example) states a type print-schema did not write (Revision 2,
		// A3).
		return "", fmt.Sprintf("the sql_types struct %s is written by hand (%s): the type is unknown", ident, g.handWhySpan(s.Line, s.NameLine))
	case s.Unread != "":
		return "", fmt.Sprintf("the sql_types struct %s is not read (%s): the type is unknown", ident, s.Unread)
	case s.OID:
		g.in.tag("DI-S1/postgres_type(oid = ..., array_oid = ...)")
		return "", fmt.Sprintf("the sql_types struct %s names its type by oid only: the type is unknown", ident)
	case s.Backend != backend:
		if s.Backend != "postgres" {
			g.in.tag("DI-S1/" + s.Backend + "_type")
		}
		return "", fmt.Sprintf("the sql_types struct %s is a %s_type, which names a type on %s only: the type is unknown on %s", ident, s.Backend, s.Backend, backend)
	}
	if backend == "mysql" {
		g.in.tag("DI-S1/mysql_type")
	}
	return s.Name, ""
}
