package prisma

import (
	"fmt"
	"sort"
	"unicode/utf8"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/models"
)

// owningRel is an owning relation field (row PR-R1): `@relation(fields:
// [..], references: [..], ...)` on a field whose type is a model.
type owningRel struct {
	field            *Field
	attr             *Attribute
	fields, refs     []string
	onDelete         *string
	onUpdate         *string
	fkName           *string
	fkNameUnreadable bool
}

// relField is one relation field of a model with its relation name (the
// explicit name, else the generated one), for pairing the two sides of a
// relation (row PR-R3).
type relField struct {
	t      *table
	f      *Field
	name   string
	owning bool
}

// referentialActions are Prisma's referential actions (psl
// parser-database ReferentialAction).
var referentialActions = []string{"Cascade", "Restrict", "NoAction", "SetNull", "SetDefault"}

// relationName is Prisma's generated relation name: the two model names
// in byte order joined by "To" (parser-database walkers/relation_field.rs
// RelationName::generated).
func relationName(a, b string) string {
	if a < b {
		return a + "To" + b
	}
	return b + "To" + a
}

// relationUnread records a relation field the rows do not read (D): no
// column's foreign keys are known, since the field may own any of them.
func (t *table) relationUnread(f *Field, why string) {
	tag("D")
	t.relUnread = true
	t.rec.ConstraintsComplete = false
	t.tableReason("D", "constraint_set", f.Line, "relation field %s: %s", f.Name, why)
}

// relationField reads a field whose type is a model of the schema: an
// owning relation (PR-R1) is resolved once every table is built; a
// back-relation has no column (PR-R2); a list field without fields: may be
// a side of an implicit many-to-many relation (PR-R3).
func (t *table) relationField(f *Field) {
	var rel *Attribute
	for _, a := range f.Attributes {
		if a.Name != "relation" || rel != nil {
			t.relationUnread(f, "attribute "+a.Raw+" is not read")
			return
		}
		rel = a
	}
	name := relationName(t.b.Name, f.Type.Name)
	if rel == nil {
		t.relFields = append(t.relFields, relField{t: t, f: f, name: name})
		tag("PR-R2")
		return
	}
	x := splitArgs(rel.Args)
	if !x.ok || len(x.pos) > 1 {
		t.relationUnread(f, "the @relation arguments are not read")
		return
	}
	explicit := false
	if len(x.pos) == 1 {
		v, ok := strLit(x.pos[0])
		if !ok {
			t.relationUnread(f, "the relation name is not a string literal")
			return
		}
		name, explicit = v, true
	}
	r := &owningRel{field: f, attr: rel}
	for _, k := range x.order {
		v := x.named[k]
		switch k {
		case "name":
			s, ok := strLit(v)
			if !ok || explicit {
				t.relationUnread(f, "the relation name is not one string literal")
				return
			}
			name = s
		case "fields", "references":
			list, ok := constantList(v)
			if !ok {
				t.relationUnread(f, k+": is not a list of field names")
				return
			}
			if k == "fields" {
				r.fields = list
			} else {
				r.refs = list
			}
		case "onDelete", "onUpdate":
			if v.Kind != ExprConstant || !contains(referentialActions, v.Value) {
				t.relationUnread(f, k+": is not a referential action")
				return
			}
			action := v.Value
			if k == "onDelete" {
				r.onDelete = &action
			} else {
				r.onUpdate = &action
			}
		case "map":
			if s, ok := strLit(v); ok {
				r.fkName = &s
			} else {
				r.fkNameUnreadable = true
			}
		default:
			t.relationUnread(f, "argument "+k+": is not read")
			return
		}
	}
	switch {
	case r.fields == nil && r.refs == nil && r.onDelete == nil && r.onUpdate == nil && r.fkName == nil && !r.fkNameUnreadable:
		t.relFields = append(t.relFields, relField{t: t, f: f, name: name})
		tag("PR-R2")
	case r.fields == nil || r.refs == nil || len(r.fields) != len(r.refs) || len(r.fields) == 0 || f.Type.List:
		t.relationUnread(f, "an owning relation needs fields: and references: lists of the same length on a non-list field")
	default:
		t.relFields = append(t.relFields, relField{t: t, f: f, name: name, owning: true})
		t.owning = append(t.owning, r)
	}
}

// constantList reads `[a, b]`: a list of plain names.
func constantList(e *Expr) ([]string, bool) {
	if e == nil || e.Kind != ExprArray {
		return nil, false
	}
	out := []string{}
	for _, item := range e.Items {
		if item.Kind != ExprConstant {
			return nil, false
		}
		out = append(out, item.Value)
	}
	return out, true
}

// foreignKeys claims the table's foreign keys (row PR-R1) once every
// model's table is built: under relationMode "foreignKeys" one constraint
// per owning relation, under "prisma" none (the relations are listed in
// emulated_relations). A relation whose fields, referenced model or
// columns are not known leaves its columns' foreign keys unknown (D).
func (t *table) foreignKeys() {
	s := t.s
	if s.relationMode == "" || s.provider == "" {
		t.rec.ConstraintsComplete = false
		why := s.providerReason
		if s.relationMode == "" {
			why = "the datasource's relationMode is not a string literal"
		}
		t.tableReason("PR-B1", "constraint_set", t.b.StartLine, "foreign keys are not known: %s", why)
	}
	for _, r := range t.owning {
		t.owningRelation(r)
	}
	if s.relationMode == "prisma" {
		emulated := t.emulated
		if emulated == nil {
			emulated = []map[string]any{}
		}
		t.rec.Attributes["emulated_relations"] = emulated
	}
	for _, c := range t.cols {
		if c.rec.Unknown {
			continue
		}
		absence := c.absenceOK && t.blockOK
		switch {
		case s.relationMode == "":
			c.rec.ForeignKeysUnknown = true
			c.reason("PR-B1", models.Fact("foreign_keys"), "the datasource's relationMode is not a string literal")
		case s.provider == "":
			c.rec.ForeignKeysUnknown = true
			c.reason("PR-B1", models.Fact("foreign_keys"), "%s", s.providerReason)
		case t.relUnread || t.fkUnknown[c] || !absence && len(c.rec.ForeignKeys) == 0:
			c.rec.ForeignKeysUnknown = true
		default:
			c.rec.Claims["foreign_keys"] = models.ClaimSource{Row: "PR-R1", Verification: t.fkVerification(len(c.rec.ForeignKeys) > 0)}
		}
	}
}

// fkVerification is a foreign-key list's verification: the structure is
// provider-independent (oracle); a list with constraints on a mapped
// provider carries its connector's default actions (mapped, PR-V6).
func (t *table) fkVerification(constraints bool) string {
	if known, _ := t.s.providerDependentKnown(); constraints && known && mappedProvider(t.s.provider) {
		return models.VerificationMapped
	}
	return models.VerificationOracle
}

// owningRelation resolves one owning relation (row PR-R1).
func (t *table) owningRelation(r *owningRel) {
	s := t.s
	var local []*column
	for _, name := range r.fields {
		c := t.byField[name]
		if c == nil || c.rec.Unknown || c.field.Type.List {
			t.relationD(r, nil, "fields: names %s, which is not a known scalar column of model %s", name, t.b.Name)
			return
		}
		local = append(local, c)
	}
	target := s.byModel[r.field.Type.Name]
	if target == nil {
		t.relationD(r, local, "the referenced model %s has no claimed table", r.field.Type.Name)
		return
	}
	var refs []*column
	for _, name := range r.refs {
		c := target.byField[name]
		if c == nil || c.rec.Unknown {
			t.relationD(r, local, "references: names %s, which is not a known column of model %s", name, target.b.Name)
			return
		}
		refs = append(refs, c)
	}
	if !target.rec.SchemaKnown {
		t.relationD(r, local, "the schema of the referenced table %s is not known", target.rec.Name)
		return
	}
	localNames, refNames := columnNames(local), columnNames(refs)
	if s.relationMode == "prisma" {
		tag("PR-R1")
		t.emulated = append(t.emulated, map[string]any{"field": r.field.Name, "model": target.b.Name, "table": target.rec.Name, "columns": localNames, "references": refNames})
		return
	}
	if s.relationMode != "foreignKeys" || s.provider == "" {
		return
	}
	if r.onDelete != nil && *r.onDelete == "Restrict" && s.provider == providerSQLServer {
		t.relationD(r, local, "onDelete: Restrict is not a SQL Server referential action")
		return
	}
	tag("PR-R1")
	var onDelete, onUpdate any
	if r.onDelete != nil {
		onDelete = *r.onDelete
	} else if known, _ := s.providerDependentKnown(); known {
		onDelete = t.defaultOnDelete(r, local)
	}
	if r.onUpdate != nil {
		onUpdate = *r.onUpdate
	} else {
		// sql_schema_calculator.rs push_inline_relations: Cascade.
		onUpdate = "Cascade"
	}
	if r.fkNameUnreadable {
		t.tableReason("PR-R1", "constraint_set", r.attr.Line, "%s: map: is not a string literal, so the constraint name is not known", r.attr.Raw)
	}
	for i, c := range local {
		c.rec.ForeignKeys = append(c.rec.ForeignKeys, &models.ForeignKey{
			Schema: copyString(target.rec.Schema), Table: target.rec.Name, Column: refNames[i], OnDelete: onDelete,
			Columns: localNames, RefColumns: refNames, OnUpdate: onUpdate, Name: r.fkName, File: t.file, Line: r.field.Line,
		})
	}
	if s.provider == providerMySQL || s.provider == providerCockroach {
		// MySQL (InnoDB) creates an index for a foreign key's columns when
		// none exists, and CockroachDB may; Prisma's DDL does not state
		// it, so whether the columns are indexed is not claimed.
		for _, c := range local {
			delete(c.rec.Claims, "index")
			c.rec.Attributes["index"] = nil
			c.reason("PR-R1", models.Fact("index"), "%s may create an index for the foreign key of relation field %s", s.provider, r.field.Name)
		}
		t.rec.ConstraintsComplete = false
		t.tableReason("PR-R1", "constraint_set", r.field.Line, "%s may create an index for the foreign key of relation field %s, which the indexes list does not hold", s.provider, r.field.Name)
	}
}

// defaultOnDelete is Prisma's default ON DELETE action
// (psl-core walker_ext_traits.rs default_on_delete_action and
// parser-database relation_field.rs referential_arity): Restrict (NoAction
// where the connector has no Restrict: sqlserver) when any field of
// fields: is required, else the relation field's own arity decides
// (required: as before, optional: SetNull).
func (t *table) defaultOnDelete(r *owningRel, local []*column) string {
	required := !r.field.Type.Optional
	for _, c := range local {
		if !c.field.Type.Optional {
			required = true
		}
	}
	switch {
	case !required:
		return "SetNull"
	case t.s.provider == providerSQLServer:
		// mssql_datamodel_connector.rs:99-103 lists no Restrict.
		return "NoAction"
	}
	return "Restrict"
}

// relationD records an owning relation the rows cannot resolve (D): its
// columns' foreign keys are unknown and the constraint set partial.
func (t *table) relationD(r *owningRel, local []*column, format string, a ...any) {
	tag("D")
	t.rec.ConstraintsComplete = false
	if local == nil {
		t.relUnread = true
	}
	for _, c := range local {
		t.fkUnknown[c] = true
	}
	t.tableReason("D", "constraint_set", r.field.Line, "relation field %s: %s", r.field.Name, fmt.Sprintf(format, a...))
}

func columnNames(cols []*column) []string {
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = c.rec.Name
	}
	return out
}

func copyString(p *string) *string {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

// manyToMany finds the implicit many-to-many relations (row PR-R3): two
// list relation fields without fields: that pair up (same model pair and
// relation name). A side whose model has no single-field primary key is a
// validation failure (PR-P1): that model's table is withdrawn. It returns
// the join tables.
func (s *schema) manyToMany() []*models.Table {
	groups := map[string][]relField{}
	var keys []string
	var names []string
	for name := range s.byModel {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		for _, rf := range s.byModel[name].relFields {
			a, b := name, rf.f.Type.Name
			if b < a {
				a, b = b, a
			}
			key := a + "\x00" + b + "\x00" + rf.name
			if groups[key] == nil {
				keys = append(keys, key)
			}
			groups[key] = append(groups[key], rf)
		}
	}
	sort.Strings(keys)
	type pair struct{ a, b relField }
	var pairs []pair
	for _, key := range keys {
		g := groups[key]
		if len(g) != 2 || !g[0].f.Type.List || !g[1].f.Type.List || g[0].owning || g[1].owning {
			continue
		}
		if s.byModel[g[0].f.Type.Name] == nil || s.byModel[g[1].f.Type.Name] == nil {
			continue
		}
		a, b := g[0], g[1]
		// Model A comes first by name; for a self-relation the field
		// names decide (parser-database relations.rs ingest_relation).
		if a.t.b.Name > b.t.b.Name || a.t.b.Name == b.t.b.Name && a.f.Name > b.f.Name {
			a, b = b, a
		}
		pairs = append(pairs, pair{a, b})
	}
	// PR-P1: an implicit many-to-many side needs a single-field id.
	for _, p := range pairs {
		for _, side := range []relField{p.a, p.b} {
			t := side.t
			if t.pkOpen || s.byModel[t.b.Name] != t || t.pkRow != "" && len(t.pkCols) == 1 {
				continue
			}
			tag("PR-P1")
			diagf(s.steps, t.file, "warning", codeImportFailure, "%s:%d: PR-P1: model %s is a side of the implicit many-to-many relation %s but has no single-field id; Prisma rejects the schema, so the model's table is not claimed", t.file, side.f.Line, t.b.Name, side.name)
			delete(s.byModel, t.b.Name)
		}
	}
	var out []*models.Table
	for _, p := range pairs {
		ta, tb := s.byModel[p.a.t.b.Name], s.byModel[p.b.t.b.Name]
		if ta != p.a.t || tb != p.b.t {
			continue
		}
		if j := s.joinTable(p.a, p.b); j != nil {
			out = append(out, j)
		}
	}
	return out
}

// joinTable builds the join table of an implicit many-to-many relation
// (row PR-R3; sql_schema_calculator.rs push_relation_tables):
// `_<relation name>` in model A's schema, columns A and B typed as the
// two models' ids, an index on B, the key (A, B) as a primary key on
// postgresql from Prisma 6.0 and as a unique index on postgresql with
// Prisma 5 and on sqlite (the oracle; on the mapped providers
// a unique index or constraint by uniqueKind, mapped), and under relationMode "foreignKeys" two foreign
// keys ON DELETE CASCADE ON UPDATE CASCADE (claimed where the key is).
func (s *schema) joinTable(a, b relField) *models.Table {
	ta, tb := a.t, b.t
	if ta.pkOpen || tb.pkOpen || ta.pkCols[0].rec.Unknown || tb.pkCols[0].rec.Unknown {
		diagf(s.steps, ta.file, "info", codeNotClaimed, "%s:%d: the join table of relation %s is not claimed: the id of model %s or %s is not known", ta.file, a.f.Line, a.name, ta.b.Name, tb.b.Name)
		return nil
	}
	name := "_" + a.name
	if utf8.RuneCountInString(name) > 63 {
		diagf(s.steps, ta.file, "info", codeNotClaimed, "%s:%d: the join table name %s is longer than 63 characters, which Prisma truncates per provider; not claimed", ta.file, a.f.Line, name)
		return nil
	}
	tag("PR-R3")
	span := contract.Span{StartLine: a.f.Line, EndLine: a.f.Line}
	j := &models.Table{
		Identity: "table:" + ta.file + "#" + name, File: ta.file, Name: name, Model: name, ModelLine: a.f.Line, Span: span,
		Schema: copyString(ta.rec.Schema), SchemaKnown: ta.rec.SchemaKnown, ColumnsComplete: true, ConstraintsComplete: true,
		Attributes: map[string]any{"model": nil, "relation_mode": nilIfEmpty(s.relationMode), "join_table": map[string]any{"relation": a.name, "models": []string{ta.b.Name, tb.b.Name}, "fields": []string{a.f.Name, b.f.Name}}},
	}
	j.Verification = s.providerVerification()
	assumptions := []string{"PR-V1", "PR-V2", "PR-V3", "PR-V4"}
	if ta.assumptionV5 {
		assumptions = append(assumptions, "PR-V5")
	}
	if s.mappedFacts() {
		assumptions = append(assumptions, "PR-V6")
	}
	j.Attributes["assumptions"] = assumptions
	// The key's form, oracle-checked: a primary key on postgresql from
	// Prisma 6.0 (JoinTableUniquenessConstraint; 6.19.3 and 7.10.0), a
	// unique index on (A, B) on postgresql with Prisma 5 (5.22.0) and on
	// sqlite (5.22.0, 6.19.3, 7.10.0). The mapped providers follow below.
	pkForm := s.provider == providerPostgres && s.since(6, 0)
	uniqueForm := s.provider == providerPostgres && s.major == 5 || s.provider == providerSQLite
	var method any
	if s.provider == providerPostgres {
		method = "BTree"
	}
	// The mapped providers' key:
	// not a primary key (JoinTableUniquenessConstraint::UniqueIndex, the
	// default outside postgresql: sql_schema_calculator.rs:349-354,
	// flavour/postgres/schema_calculator.rs:216-222 for cockroachdb), in
	// the list the provider creates a unique index in (uniqueKind: SQL
	// Server's UNIQUE constraint); the index on B for every provider
	// (sql_schema_calculator.rs:380-392). The foreign keys stay unknown.
	kind, _, _ := s.uniqueKind()
	mappedForm := mappedProvider(s.provider) && kind != ""
	switch {
	case pkForm:
		j.Indexes = []map[string]any{{"name": nil, "columns": []string{"B"}, "unique": false, "method": method}}
	case uniqueForm:
		j.Indexes = []map[string]any{
			{"name": nil, "columns": []string{"A", "B"}, "unique": true, "method": nil},
			{"name": nil, "columns": []string{"B"}, "unique": false, "method": method},
		}
	case mappedForm && kind == uniqueAsConstraint:
		tag("PR-R3/constraint")
		j.Uniques = []map[string]any{{"name": nil, "columns": []string{"A", "B"}}}
		j.Indexes = []map[string]any{{"name": nil, "columns": []string{"B"}, "unique": false, "method": nil}}
	case mappedForm:
		tag("PR-R3/mapped index")
		j.Indexes = []map[string]any{
			{"name": nil, "columns": []string{"A", "B"}, "unique": true, "method": nil},
			{"name": nil, "columns": []string{"B"}, "unique": false, "method": nil},
		}
	default:
		j.PrimaryKeyOpen, j.ConstraintsComplete = true, false
		j.Reasons = append(j.Reasons, reason("PR-R3", models.Fact("constraint_set"), ta.file, a.f.Line, "the join table's key form is provider-dependent and not known here (%s, Prisma %d)", nonEmpty(s.provider), s.major))
	}
	for i, side := range []relField{a, b} {
		col := "A"
		if i == 1 {
			col = "B"
		}
		id := side.t.pkCols[0]
		c := &models.Column{Name: col, Attribute: col, File: side.t.file, Line: side.f.Line, Attributes: map[string]any{}, Claims: map[string]models.ClaimSource{}}
		for _, k := range []string{"type", "sql_type", "native_type", "length", "precision", "scale", "timezone", "nullable", "primary_key", "unique", "index", "default", "default_value", "server_default", "server_default_text"} {
			c.Attributes[k] = nil
		}
		set := func(fact string, v any, verification string) {
			c.Attributes[fact] = v
			c.Claims[fact] = models.ClaimSource{Row: "PR-R3", Verification: verification}
		}
		set("name", col, models.VerificationOracle)
		set("nullable", false, models.VerificationOracle)
		if src, ok := id.rec.Claims["type"]; ok {
			for _, k := range []string{"type", "sql_type", "native_type", "length", "precision", "scale", "timezone", "enum"} {
				if v, has := id.rec.Attributes[k]; has {
					c.Attributes[k] = v
				}
				if typeSrc, claimed := id.rec.Claims[k]; claimed && k != "type" {
					c.Claims[k] = models.ClaimSource{Row: "PR-R3", Verification: typeSrc.Verification}
				}
			}
			c.Claims["type"] = models.ClaimSource{Row: "PR-R3", Verification: src.Verification}
		} else {
			c.Reasons = append(c.Reasons, reason("PR-R3", models.Fact("type"), side.t.file, side.f.Line, "the type of %s's id is not known", side.t.b.Name))
		}
		if known, _ := s.providerDependentKnown(); known {
			v := s.providerVerification()
			set("default", "absent", v)
			set("server_default", false, v)
		}
		if pkForm || uniqueForm {
			set("primary_key", pkForm, models.VerificationOracle)
			set("unique", false, models.VerificationOracle)
			set("index", col == "B", models.VerificationOracle)
		} else if mappedForm {
			set("primary_key", false, models.VerificationMapped)
			set("unique", false, models.VerificationMapped)
		}
		switch {
		case s.relationMode == "prisma":
			c.Claims["foreign_keys"] = models.ClaimSource{Row: "PR-R3", Verification: models.VerificationOracle}
		case s.relationMode == "foreignKeys" && (pkForm || uniqueForm):
			c.ForeignKeys = []*models.ForeignKey{{
				Schema: copyString(side.t.rec.Schema), Table: side.t.rec.Name, Column: id.rec.Name, OnDelete: "Cascade",
				Columns: []string{col}, RefColumns: []string{id.rec.Name}, OnUpdate: "Cascade", File: side.t.file, Line: side.f.Line,
			}}
			c.Claims["foreign_keys"] = models.ClaimSource{Row: "PR-R3", Verification: models.VerificationOracle}
		default:
			c.ForeignKeysUnknown = true
			c.Reasons = append(c.Reasons, reason("PR-R3", models.Fact("foreign_keys"), side.t.file, side.f.Line, "the join table's foreign keys are claimed on postgresql and sqlite only (their actions are flavour-specific: m2m_foreign_key_action)"))
			j.ConstraintsComplete = false
		}
		j.Columns = append(j.Columns, c)
	}
	return j
}

func nonEmpty(s string) string {
	if s == "" {
		return "unknown provider"
	}
	return s
}

// RelationGroup is one claimed Prisma schema as code-to-table linking
// (T018h rows CT-R1 and CT-R2) reads it: the packages that load it and the
// relation fields of its models. The table records do not carry relation
// fields, and adding them would change the records (ADR 0057), so this
// accessor exports what the claim pass already parses.
type RelationGroup struct {
	// Set is the group's tables' set attribute ("models/prisma/<schema
	// path>").
	Set string
	// Packages are the repository-relative directories of the packages
	// whose discovery names the schema ("." for the root), sorted.
	Packages []string
	// Models maps a model name to its relation fields in declaration
	// order; a model with no relation field has an empty list.
	Models map[string][]RelationField
}

// RelationField is one relation field of a model: a field whose type is a
// model of the schema.
type RelationField struct {
	// Name is the field name (the key in include, select, where and data).
	Name string
	// Model is the related model.
	Model string
	// List is set for a list field (T[]).
	List bool
	// ManyToMany is set for a side of an implicit many-to-many relation (two
	// list fields without fields:, row PR-R3): reading or writing it also
	// touches the join table.
	ManyToMany bool
	// Unread is set when the claim pass does not read the field's relation
	// attributes (row D): its side, and so whether a join table is
	// involved, is not known.
	Unread bool
}

// Relations reads every Prisma schema Link would claim and returns its
// relation fields, one group per claimed schema in Link's order. It parses
// the schemas again (they are small) and keeps no diagnostics: Link
// reports them.
func Relations(sources map[string][]byte) []RelationGroup {
	pkgs, _ := Discover(sources)
	steps := &models.Steps{}
	dirs := map[string][]string{}
	for _, p := range pkgs {
		if p.Files != nil {
			dirs[p.SchemaPath] = append(dirs[p.SchemaPath], p.Dir)
		}
	}
	var out []RelationGroup
	for _, p := range sharedSchemas(pkgs, steps) {
		if p.Files == nil {
			continue
		}
		var s *schema
		steps.Guard(p.Manifest, "schema", "Prisma schema "+p.SchemaPath, func() { s = readSchema(p, sources, steps) })
		if s == nil || len(s.tables) == 0 {
			continue
		}
		g := RelationGroup{Set: "models/prisma/" + p.SchemaPath, Models: map[string][]RelationField{}}
		g.Packages = append(g.Packages, dirs[p.SchemaPath]...)
		sort.Strings(g.Packages)
		read := map[*Field]relField{}
		pairs := map[string][]relField{}
		for _, t := range s.byModel {
			for _, rf := range t.relFields {
				read[rf.f] = rf
				a, b := t.b.Name, rf.f.Type.Name
				if b < a {
					a, b = b, a
				}
				key := a + "\x00" + b + "\x00" + rf.name
				pairs[key] = append(pairs[key], rf)
			}
		}
		m2m := map[*Field]bool{}
		for _, list := range pairs {
			if len(list) == 2 && list[0].f.Type.List && list[1].f.Type.List && !list[0].owning && !list[1].owning {
				m2m[list[0].f], m2m[list[1].f] = true, true
			}
		}
		for name, t := range s.byModel {
			fields := []RelationField{}
			for _, f := range t.b.Fields {
				if f.Type == nil || f.Type.Legacy != "" || f.LegacyColon || s.models[f.Type.Name] == nil || ignoredField(f) {
					continue
				}
				_, ok := read[f]
				fields = append(fields, RelationField{Name: f.Name, Model: f.Type.Name, List: f.Type.List, ManyToMany: m2m[f], Unread: !ok})
			}
			g.Models[name] = fields
		}
		out = append(out, g)
	}
	return out
}

// ignoredField reports a field with @ignore, which the generated client
// leaves out.
func ignoredField(f *Field) bool {
	for _, a := range f.Attributes {
		if a.Name == "ignore" {
			return true
		}
	}
	return false
}
