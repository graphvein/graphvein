package prisma

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/models"
)

// column is one scalar, enum or Unsupported field of a model while its
// table is built.
type column struct {
	field *Field
	rec   *models.Column
	// absenceOK is false when a field attribute is unread (row D): then no
	// absence (not unique, no default, no key) is claimed for it.
	absenceOK bool
	idAttr    *Attribute
	unique    *Attribute
	def       *Attribute
	updatedAt bool
	native    *Attribute
	nameKnown bool
}

// table builds one model's table record (row PR-B3 and the field and block
// attribute rows).
type table struct {
	s    *schema
	file string
	b    *Block
	rec  *models.Table
	cols []*column
	// byField finds a column by field name for block attributes.
	byField map[string]*column
	// blockOK is false when a block attribute is unread (row D); uniqueOK
	// and indexOK are false when an @@unique or @@index is not read, so no
	// column is claimed not unique or not indexed.
	blockOK, uniqueOK, indexOK bool
	// pkCols are the primary key's columns by @id or @@id; pkOpen means the
	// key is not known.
	pkOpen     bool
	pkRow      string
	pkCols     []*column
	pkName     any
	uniqueCols map[*column]string
	indexCols  map[*column]bool
	// owning are the model's owning relation fields (row PR-R1) and lists
	// its list relation fields without fields: (PR-R2, or a side of an
	// implicit many-to-many, PR-R3); relUnread is set when a relation field
	// is not read (D), so no column's foreign keys are known.
	owning    []*owningRel
	relFields []relField
	relUnread bool
	// fkUnknown marks columns whose foreign keys a relation the rows do
	// not read may set (D).
	fkUnknown map[*column]bool
	// failure is the PR-P1 rule the model breaks ("" when none): Prisma
	// rejects the schema, so the model's table is not claimed.
	failure      string
	failureLine  int
	assumptionV5 bool
	// emulated lists the relations of relationMode "prisma" (PR-R1).
	emulated []map[string]any
}

func reason(row string, fact *string, file string, line int, format string, a ...any) models.Reason {
	return models.Reason{Row: row, Fact: fact, File: file, Line: line, Reason: fmt.Sprintf(format, a...)}
}

func (t *table) tableReason(row, fact string, line int, format string, a ...any) {
	t.rec.Reasons = append(t.rec.Reasons, reason(row, models.Fact(fact), t.file, line, format, a...))
}

func (c *column) reason(row string, fact *string, format string, a ...any) {
	c.rec.Reasons = append(c.rec.Reasons, reason(row, fact, c.rec.File, c.field.Line, format, a...))
}

// claim sets a column fact with the row (and verification) that claims it.
func (c *column) claim(fact string, value any, row, verification string) {
	c.rec.Attributes[fact] = value
	c.rec.Claims[fact] = models.ClaimSource{Row: row, Verification: verification}
}

// buildTable builds a model's table; nil when the table claim is refused
// (a non-literal @@map, row PR-AA4; a validation failure, row PR-P1).
// Foreign keys (PR-R1) are added once every model's table is built.
func (s *schema) buildTable(file string, b *Block) *table {
	t := &table{s: s, file: file, b: b, byField: map[string]*column{}, blockOK: true, uniqueOK: true, indexOK: true, uniqueCols: map[*column]string{}, indexCols: map[*column]bool{}, fkUnknown: map[*column]bool{}}
	t.rec = &models.Table{
		Identity: "table:" + file + "#" + b.Name, File: file, Name: b.Name, Model: b.Name, ModelLine: b.StartLine,
		Span: contract.Span{StartLine: b.StartLine, EndLine: b.EndLine}, ColumnsComplete: true, ConstraintsComplete: true,
		Attributes: map[string]any{"model": b.Name, "relation_mode": nilIfEmpty(s.relationMode)},
	}
	t.rec.Verification = s.providerVerification()
	tag("PR-B3")
	// Block attributes that name the table come first: @@map, @@schema,
	// @@ignore; an unread one makes every absence of the model unknown.
	var ids, uniques, indexes []*Attribute
	var schemaAttr *Attribute
	seen := map[string]bool{}
	for _, a := range b.Attributes {
		if seen[a.Name] && (a.Name == "map" || a.Name == "schema" || a.Name == "id" || a.Name == "ignore") {
			t.unreadBlock(a, "@@"+a.Name+" is written twice")
			continue
		}
		seen[a.Name] = true
		switch a.Name {
		case "map":
			v, ok := singleString(a)
			if !ok {
				tag("PR-AA4")
				diagf(s.steps, file, "info", codeNotClaimed, "%s:%d: model %s: @@map is not one string literal, so the table name is not known and no table is claimed (PR-AA4)", file, a.Line, b.Name)
				return nil
			}
			tag("PR-AA4")
			t.rec.Name = v
		case "schema":
			schemaAttr = a
		case "ignore":
			if !noArgs(a) {
				t.unreadBlock(a, "@@ignore with arguments is not read")
				continue
			}
			t.rec.Attributes["ignored"] = true
		case "id":
			ids = append(ids, a)
		case "unique":
			uniques = append(uniques, a)
		case "index":
			indexes = append(indexes, a)
		default:
			t.unreadBlock(a, "block attribute @@"+a.Name+" is not read")
		}
	}
	t.readSchemaAttr(schemaAttr)
	for _, f := range b.Fields {
		t.field(f)
	}
	for _, a := range ids {
		t.blockID(a)
	}
	for _, a := range uniques {
		t.blockIndex(a, true)
	}
	for _, a := range indexes {
		t.blockIndex(a, false)
	}
	if t.failure != "" {
		tag("PR-P1")
		diagf(s.steps, file, "warning", codeImportFailure, "%s:%d: PR-P1: model %s: %s; Prisma rejects the schema, so the model's table is not claimed", file, t.failureLine, b.Name, t.failure)
		return nil
	}
	t.finish()
	return t
}

// fail records a PR-P1 validation failure of the model (the first wins).
func (t *table) fail(line int, why string) {
	if t.failure == "" {
		t.failure, t.failureLine = why, line
	}
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (t *table) unreadBlock(a *Attribute, why string) {
	tag("D")
	t.blockOK = false
	t.rec.ConstraintsComplete = false
	t.tableReason("D", "constraint_set", a.Line, "%s: %s", a.Raw, why)
}

// readSchemaAttr places the table (row PR-AA5, PR-V5).
func (t *table) readSchemaAttr(a *Attribute) {
	s := t.s
	defaultSchema := s.provider == providerPostgres || s.provider == providerCockroach
	switch {
	case a == nil && !s.schemasListed:
		t.rec.SchemaKnown = true
		if defaultSchema {
			public := "public"
			t.rec.Schema = &public
			t.assumptionV5 = true
		}
	case a == nil && !s.schemasKnown:
		t.rec.SchemaKnown = false
		t.tableReason("PR-AA5", "schema", t.b.StartLine, "the datasource's schemas list is not literal")
	case a == nil:
		// The datasource lists schemas: PR-P1 where the rules are in
		// force (schemas without the preview feature before 6.13.0 already
		// refused the schema); unknown where the version is not proven.
		if inForce, _ := s.multiSchema(); inForce {
			t.fail(t.b.StartLine, "the datasource lists schemas but the model has no @@schema")
			return
		}
		t.rec.SchemaKnown = false
		t.tableReason("PR-AA5", "schema", t.b.StartLine, "whether the datasource's schemas list applies is not known for this Prisma 6 version without the multiSchema preview feature")
	default:
		v, ok := singleString(a)
		inForce, known := s.multiSchema()
		switch {
		case !ok:
			t.unreadBlock(a, "@@schema is not one string literal")
		case !known || !inForce:
			t.tableReason("PR-AA5", "schema", a.Line, "@@schema is not read without the multiSchema preview feature at this Prisma version (GA from 6.13.0)")
		case s.schemasListed && !s.schemasKnown:
			t.tableReason("PR-AA5", "schema", a.Line, "the datasource's schemas list is not literal")
		case !s.schemasListed || !contains(s.schemas, v):
			t.fail(a.Line, fmt.Sprintf("@@schema(%q) names a schema the datasource's schemas list does not hold", v))
		case !defaultSchema && s.provider != providerSQLServer:
			t.tableReason("PR-AA5", "schema", a.Line, "@@schema is not read for provider %q", s.provider)
		default:
			tag("PR-AA5")
			t.rec.Schema, t.rec.SchemaKnown = &v, true
		}
	}
}

// field reads one field: a column for a scalar, enum or Unsupported type,
// none for a relation field (PR-R2; an owning relation's foreign key is
// row PR-R1), the default branch for anything else.
func (t *table) field(f *Field) {
	s := t.s
	if f.Type == nil || f.Type.Legacy != "" || f.LegacyColon {
		tag("D")
		t.rec.ColumnsComplete, t.rec.ConstraintsComplete = false, false
		t.tableReason("D", "column_set", f.Line, "field %s: obsolete or missing type form", f.Name)
		return
	}
	name := f.Type.Name
	switch {
	case name == "Unsupported" || contains(scalarTypes, name):
	case s.enums[name] != nil:
	case s.models[name] != nil:
		t.relationField(f)
		return
	default:
		tag("D")
		t.rec.ColumnsComplete, t.rec.ConstraintsComplete = false, false
		why := "a type that is not a scalar, enum or model of this schema"
		if k := s.others[name]; k != "" {
			why = "a " + k + " type, which is not read"
		}
		t.tableReason("D", "column_set", f.Line, "field %s has %s (%s)", f.Name, why, name)
		return
	}
	if f.Type.List && (s.provider == providerMySQL || s.provider == providerSQLite || s.provider == providerSQLServer) {
		// Scalar lists need the ScalarLists capability, which only the
		// postgresql and cockroachdb connectors have.
		t.fail(f.Line, fmt.Sprintf("field %s is a scalar list, which provider %s does not support", f.Name, s.provider))
	}
	c := &column{field: f, absenceOK: true, nameKnown: true, rec: &models.Column{
		Name: f.Name, IdentityName: f.Name, Attribute: f.Name, File: t.file, Line: f.Line,
		Attributes: map[string]any{}, Claims: map[string]models.ClaimSource{},
	}}
	for _, k := range []string{"type", "sql_type", "native_type", "length", "precision", "scale", "timezone", "nullable", "primary_key", "unique", "index", "default", "default_value", "server_default", "server_default_text"} {
		c.rec.Attributes[k] = nil
	}
	t.cols = append(t.cols, c)
	t.byField[f.Name] = c
	t.rec.Columns = append(t.rec.Columns, c.rec)
	seen := map[string]bool{}
	for _, a := range f.Attributes {
		if seen[a.Name] {
			t.unreadField(c, a, "written twice")
			continue
		}
		seen[a.Name] = true
		switch a.Name {
		case "id":
			c.idAttr = a
		case "unique":
			c.unique = a
		case "default":
			c.def = a
		case "updatedAt":
			if !noArgs(a) {
				t.unreadField(c, a, "@updatedAt with arguments is not read")
				continue
			}
			c.updatedAt = true
		case "map":
			if v, ok := singleString(a); ok {
				tag("PR-A5")
				c.rec.Name = v
			} else {
				c.nameKnown = false
			}
		case "ignore":
			if !noArgs(a) {
				t.unreadField(c, a, "@ignore with arguments is not read")
				continue
			}
			tag("PR-A7")
			c.rec.Attributes["ignored"] = true
		default:
			if _, ok := nativeName(a); ok {
				c.native = a
				continue
			}
			t.unreadField(c, a, "field attribute @"+a.Name+" is not read")
		}
	}
	if !c.nameKnown {
		c.rec.Unknown = true
		t.rec.ColumnsComplete = false
		c.reason("PR-A5", nil, "@map is not one string literal, so the column name is not known")
		return
	}
	c.claim("name", c.rec.Name, nameRow(c), models.VerificationOracle)
	t.nullable(c)
	t.columnType(c)
	t.columnDefault(c)
	t.columnUnique(c)
	if c.idAttr != nil {
		t.fieldID(c)
	}
}

func nameRow(c *column) string {
	for _, a := range c.field.Attributes {
		if a.Name == "map" {
			return "PR-A5"
		}
	}
	return "PR-B3"
}

func (t *table) unreadField(c *column, a *Attribute, why string) {
	tag("D")
	c.absenceOK = false
	t.rec.ConstraintsComplete = false
	c.reason("D", nil, "%s: %s; absences (no default, not unique, no key, no foreign key) are not claimed", a.Raw, why)
}

// nullable claims row PR-F2.
func (t *table) nullable(c *column) {
	ft := c.field.Type
	switch {
	case ft.List && ft.Optional:
		tag("D")
		c.reason("D", models.Fact("nullable"), "`[]?` is not a valid modifier")
	case ft.List:
		tag("PR-F2/[]")
		c.claim("nullable", true, "PR-F2", models.VerificationOracle)
	case ft.Optional:
		tag("PR-F2/?")
		c.claim("nullable", true, "PR-F2", models.VerificationOracle)
	default:
		tag("PR-F2/none")
		c.claim("nullable", false, "PR-F2", models.VerificationOracle)
	}
}

var integerLiteral = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)

// columnType claims the column's type: rows PR-F1 (scalar), PR-A6 (a
// postgresql native type), PR-F3 (Unsupported), PR-F4 (enum), with the
// list suffix of PR-F2.
func (t *table) columnType(c *column) {
	s := t.s
	ft := c.field.Type
	ok, why := s.providerDependentKnown()
	if !ok {
		c.reason("PR-F1", models.Fact("type"), "%s", why)
		return
	}
	if ft.List && ft.Optional {
		return
	}
	suffix := ""
	if ft.List {
		suffix = "[]"
	}
	v := s.providerVerification()
	switch {
	case ft.Name == "Unsupported":
		lit, isLit := strLit(ft.Unsupported)
		if !isLit || c.native != nil {
			c.reason("PR-F3", models.Fact("type"), "Unsupported(...) is not one string literal")
			return
		}
		tag("PR-F3")
		c.claim("type", lit, "PR-F3", v)
		c.rec.Attributes["sql_type"] = lit + suffix
	case s.enums[ft.Name] != nil:
		t.enumType(c, s.enums[ft.Name], suffix, v)
	case c.native != nil:
		t.nativeType(c, suffix)
	default:
		var sqlType string
		switch s.provider {
		case providerPostgres:
			sqlType = postgresScalar[ft.Name]
		case providerSQLite:
			if ft.Name == "Json" && !s.since(6, 2) {
				c.reason("PR-F1", models.Fact("type"), "%s", sqliteJSONEnums)
				return
			}
			sqlType = sqliteScalar[ft.Name]
		default:
			sqlType = mappedScalar[s.provider][ft.Name]
		}
		tag("PR-F1/" + ft.Name)
		c.claim("type", ft.Name, "PR-F1", v)
		c.rec.Attributes["sql_type"] = sqlType + suffix
		t.sizes(c, sqlType, "PR-F1")
	}
}

// sizes claims length, precision, scale and timezone from a postgresql
// catalog type (oracle-checked); mapped providers state only the type
// text.
func (t *table) sizes(c *column, sqlType, row string) {
	if t.s.provider != providerPostgres {
		return
	}
	for k, val := range postgresSizes(sqlType) {
		c.claim(k, val, row, models.VerificationOracle)
	}
}

// nativeType claims row PR-A6 on postgresql.
func (t *table) nativeType(c *column, suffix string) {
	a := c.native
	name, _ := nativeName(a)
	if t.s.provider != providerPostgres {
		c.reason("PR-A6", models.Fact("type"), "native type %s is read for postgresql only", a.Raw)
		return
	}
	nt, known := postgresNative[name]
	if !known {
		c.reason("PR-A6", models.Fact("type"), "native type %s is not one the contract lists for postgresql (Prisma rejects an undefined one, PR-P1)", a.Raw)
		return
	}
	if nt.scalar != c.field.Type.Name {
		c.reason("PR-A6", models.Fact("type"), "native type %s does not apply to %s", a.Raw, c.field.Type.Name)
		return
	}
	x := splitArgs(a.Args)
	var nums []int
	for _, e := range x.pos {
		if e.Kind != ExprNumber || !integerLiteral.MatchString(e.Value) {
			x.ok = false
			break
		}
		n, err := strconv.Atoi(e.Value)
		if err != nil {
			x.ok = false
			break
		}
		nums = append(nums, n)
	}
	if !x.ok || len(x.named) > 0 || !nativeArgCountOK(name, len(nums)) {
		c.reason("PR-A6", models.Fact("type"), "the arguments of %s are not integer literals of the native type's form", a.Raw)
		return
	}
	tag("PR-A6/" + name)
	sqlType := nt.render(nums)
	c.claim("type", c.field.Type.Name, "PR-A6", models.VerificationOracle)
	c.rec.Attributes["sql_type"] = sqlType + suffix
	c.rec.Attributes["native_type"] = strings.TrimPrefix(a.Raw, "@db.")
	t.sizes(c, sqlType, "PR-A6")
}

// enumType claims row PR-F4 with the enum's facts (PR-E1).
func (t *table) enumType(c *column, e *enumInfo, suffix, v string) {
	s := t.s
	if c.native != nil {
		c.reason("PR-F4", models.Fact("type"), "a native type on an enum field is not read")
		return
	}
	var values any
	if e.valuesOK {
		values = append([]string{}, e.values...)
	}
	var dbName any
	if e.dbName != nil {
		dbName = *e.dbName
	}
	var schema any
	if e.schema != nil {
		schema = *e.schema
	}
	c.rec.Attributes["enum"] = map[string]any{"name": e.name, "database_name": dbName, "values": values, "schema": schema}
	tag("PR-E1")
	switch s.provider {
	case providerPostgres, providerCockroach:
		if e.dbName == nil {
			c.reason("PR-E1", models.Fact("type"), "the enum's database name is not known: %s", e.reason)
			return
		}
		tag("PR-F4")
		c.claim("type", *e.dbName, "PR-F4", v)
		c.rec.Attributes["sql_type"] = *e.dbName + suffix
	case providerMySQL:
		// flavour/mysql/renderer.rs:398-401: an inline ENUM of the values.
		if !e.valuesOK {
			c.reason("PR-E1", models.Fact("type"), "the enum's values are not known: %s", e.reason)
			return
		}
		quoted := make([]string, len(e.values))
		for i, val := range e.values {
			if strings.ContainsAny(val, `'\`) {
				c.reason("PR-E1", models.Fact("type"), "an enum value holds a quote or backslash, whose MySQL rendering is not read")
				return
			}
			quoted[i] = "'" + val + "'"
		}
		tag("PR-F4")
		text := "ENUM(" + strings.Join(quoted, ", ") + ")"
		c.claim("type", text, "PR-F4", v)
		c.rec.Attributes["sql_type"] = text
	case providerSQLite:
		// The oracle: an enum column is TEXT (no CHECK constraint) from
		// Prisma 6.2.0, which first accepts enums on sqlite.
		if !s.since(6, 2) {
			c.reason("PR-F4", models.Fact("type"), "%s", sqliteJSONEnums)
			return
		}
		tag("PR-F4")
		c.claim("type", "TEXT", "PR-F4", v)
		c.rec.Attributes["sql_type"] = "TEXT" + suffix
	default:
		c.reason("PR-F4", models.Fact("type"), "provider %s has no enums (Prisma rejects the enum)", s.provider)
	}
}

// columnDefault claims row PR-A3 (and PR-A4's absence of a server
// default).
func (t *table) columnDefault(c *column) {
	s := t.s
	if c.updatedAt {
		tag("PR-A4")
	}
	ok, why := s.providerDependentKnown()
	if !ok {
		c.reason("PR-A3", models.Fact("default"), "%s", why)
		return
	}
	v := s.providerVerification()
	scalar := c.field.Type.Name
	if s.provider == providerSQLite && c.idAttr != nil && scalar == "Int" && !c.field.Type.List {
		c.reason("PR-A3", models.Fact("default"), "%s", sqliteRowid)
		c.reason("PR-A3", models.Fact("server_default"), "%s", sqliteRowid)
		return
	}
	if c.def == nil {
		if !c.absenceOK || !t.blockOK {
			return
		}
		c.claim("server_default", false, "PR-A3", v)
		if c.updatedAt {
			c.reason("PR-A4", models.Fact("default"), "@updatedAt: the client sets the value; there is no database default")
			return
		}
		c.claim("default", "absent", "PR-A3", v)
		return
	}
	x := splitArgs(c.def.Args)
	if name, hasMap := x.named["map"]; hasMap {
		// A default constraint name is SQL Server's only
		// (flavour/mssql/renderer.rs:56-60).
		if _, lit := strLit(name); !lit || s.provider != providerSQLServer {
			x.ok = false
		}
	}
	if !x.ok || len(x.pos) != 1 || len(x.named) > len(mapOnly(x.named)) {
		c.reason("PR-A3", models.Fact("default"), "%s: arguments other than one value and a literal map: are not read", c.def.Raw)
		return
	}
	e := x.pos[0]
	// server claims a server default; text is the database's text (the
	// DEFAULT expression Prisma renders), "" when the mapping does not
	// prove it: then only the presence is claimed (developer decision
	// 2026-11-09: never the Prisma literal).
	server := func(form, text, why string) {
		tag("PR-A3/" + form)
		c.claim("default", "server", "PR-A3", v)
		c.claim("server_default", true, "PR-A3", v)
		c.rec.Attributes["default_value"] = e.Raw
		if text != "" {
			c.rec.Attributes["server_default_text"] = text
			return
		}
		c.reason("PR-A3", models.Fact("server_default_text"), "%s", why)
	}
	switch {
	case literalDefault(e):
		if s.provider == providerMySQL && (scalar == "Json" || scalar == "Bytes") {
			// flavour/mysql/renderer.rs:22-34: no default is rendered for
			// Json or Bytes columns; what the client fills in is not read.
			c.reason("PR-A3", models.Fact("default"), "MySQL renders no default for a %s column", scalar)
			return
		}
		if s.provider == providerSQLite && scalar == "Json" {
			// The oracle: Prisma writes the JSON text unquoted (DEFAULT
			// {}), which SQLite rejects, so no such database exists.
			c.reason("PR-A3", models.Fact("default"), "on sqlite Prisma renders a Json literal default as SQL that SQLite rejects; the default is not claimed")
			return
		}
		text, why := t.literalText(c, e)
		server("(a)", text, why)
	case e.Kind == ExprCall && e.Value == "now" && emptyCall(e):
		if scalar != "DateTime" || c.rec.Attributes["type"] == nil {
			c.reason("PR-A3", models.Fact("default"), "now() on a column whose type is not a known DateTime type")
			return
		}
		server("(b)", nowText[s.provider], "")
	case e.Kind == ExprCall && e.Value == "autoincrement" && emptyCall(e):
		if !autoincrementType(c, s.provider) {
			c.reason("PR-A3", models.Fact("default"), "autoincrement() on a type the contract does not list for it")
			return
		}
		if why, property := autoincrementProperty[s.provider]; property {
			tag("PR-A3/(c)")
			c.reason("PR-A3", models.Fact("default"), "%s", why)
			c.reason("PR-A3", models.Fact("server_default"), "%s", why)
			return
		}
		server("(c)", autoincrementText[s.provider], autoincrementWhy[s.provider])
	case e.Kind == ExprCall && e.Value == "dbgenerated":
		y := splitArgs(e.Args)
		lit, isLit := "", false
		if y.ok && len(y.pos) == 1 && len(y.named) == 0 {
			lit, isLit = strLit(y.pos[0])
		}
		if !isLit {
			c.reason("PR-A3", models.Fact("default"), "dbgenerated without one string literal is not read")
			return
		}
		if s.provider == providerSQLite {
			// The oracle: SQLite needs the expression parenthesised and
			// keeps it without the outer parentheses, so the text as
			// written is not the database's.
			server("(d)", "", "SQLite keeps a DEFAULT expression without its outer parentheses, so the text as written is not the database's; only the default's presence is claimed")
			break
		}
		server("(d)", lit, "")
	case clientDefault(e):
		if why := t.clientDefaultVersion(e); why != "" {
			c.reason("PR-A3", models.Fact("default"), "%s", why)
			return
		}
		tag("PR-A3/(e)")
		c.claim("default", "client", "PR-A3", v)
		c.claim("server_default", false, "PR-A3", v)
		c.rec.Attributes["default_value"] = e.Raw
	default:
		c.reason("PR-A3", models.Fact("default"), "%s is not a default form the contract lists", c.def.Raw)
	}
}

func mapOnly(named map[string]*Expr) map[string]*Expr {
	if v, ok := named["map"]; ok {
		return map[string]*Expr{"map": v}
	}
	return nil
}

func emptyCall(e *Expr) bool { return e.Args == nil || len(e.Args.Args) == 0 }

// literalDefault is form (a): a boolean, number, string, enum value or a
// list of those.
func literalDefault(e *Expr) bool {
	switch e.Kind {
	case ExprString, ExprNumber:
		return true
	case ExprConstant:
		return !strings.Contains(e.Value, ".")
	case ExprArray:
		for _, item := range e.Items {
			if item.Kind == ExprArray || !literalDefault(item) {
				return false
			}
		}
		return true
	}
	return false
}

// clientDefault is form (e): a function the Prisma client evaluates.
func clientDefault(e *Expr) bool {
	if e.Kind != ExprCall {
		return false
	}
	x := splitArgs(e.Args)
	if !x.ok || len(x.named) > 0 {
		return false
	}
	switch len(x.pos) {
	case 0:
		return e.Value == "uuid" || e.Value == "cuid" || e.Value == "nanoid" || e.Value == "ulid"
	case 1:
		a := x.pos[0]
		if a.Kind != ExprNumber || !integerLiteral.MatchString(a.Value) {
			return false
		}
		switch e.Value {
		case "uuid":
			return a.Value == "4" || a.Value == "7"
		case "cuid":
			return a.Value == "2"
		case "nanoid":
			// psl attributes/default.rs validate_nanoid_args: n >= 2.
			return a.Value != "0" && a.Value != "1"
		}
	}
	return false
}

// autoincrementType says whether autoincrement() is on a type row PR-A3
// (c) lists: Int or BigInt without a native type, or (postgresql)
// @db.SmallInt, @db.Integer, @db.BigInt.
func autoincrementType(c *column, provider string) bool {
	st := c.field.Type.Name
	if c.field.Type.List || (st != "Int" && st != "BigInt") || c.rec.Attributes["type"] == nil {
		return false
	}
	if c.native == nil {
		return true
	}
	name, _ := nativeName(c.native)
	return provider == providerPostgres && (name == "SmallInt" || name == "Integer" || name == "BigInt")
}

// columnUnique reads @unique (row PR-A2): a single-column unique index.
func (t *table) columnUnique(c *column) {
	a := c.unique
	if a == nil {
		return
	}
	x := splitArgs(a.Args)
	if !x.ok || len(x.pos) > 0 {
		c.reason("PR-A2", models.Fact("unique"), "%s: arguments are not read, so uniqueness is not known", a.Raw)
		t.rec.ConstraintsComplete = false
		c.unique = nil
		c.absenceOK = false
		return
	}
	if _, partial := x.named["where"]; partial {
		// A partial unique index (where:, the partialIndexes preview
		// feature) is unique only where its predicate holds: D, as for
		// @@index(where:).
		t.unreadIndex(a, true, "a partial unique index (where:) is not read")
		c.unique = nil
		return
	}
	name, ok := t.mapName(x, a, "PR-A2")
	if !ok {
		c.reason("PR-A2", models.Fact("unique"), "%s: map: is not a string literal, so uniqueness is not known", a.Raw)
		t.rec.ConstraintsComplete = false
		c.unique = nil
		c.absenceOK = false
		return
	}
	tag("PR-A2")
	t.uniqueCols[c] = "PR-A2"
	t.addUnique(name, []string{c.rec.Name}, a, "PR-A2")
}

// addUnique records a unique index Prisma derives from @unique or
// @@unique without a predicate in the list the provider creates it in:
// indexes (unique: true) or unique_constraints (SQL Server); when the
// kind is not known the constraint set is partial.
// The column's unique fact does not depend on it.
func (t *table) addUnique(name any, cols []string, a *Attribute, row string) {
	kind, verification, why := t.s.uniqueKind()
	switch kind {
	case uniqueAsConstraint:
		tag(row + "/constraint")
		t.rec.Uniques = append(t.rec.Uniques, map[string]any{"name": name, "columns": cols})
	case uniqueAsIndex:
		if verification == models.VerificationOracle {
			tag(row + "/index")
		} else {
			tag(row + "/mapped index")
		}
		t.rec.Indexes = append(t.rec.Indexes, map[string]any{"name": name, "columns": cols, "unique": true, "method": nil})
	default:
		t.rec.ConstraintsComplete = false
		t.tableReason(row, "constraint_set", a.Line, "%s: whether the database holds a unique index or a unique constraint is provider-dependent and not known: %s", a.Raw, why)
	}
}

// mapName reads a literal map: argument (the only name a row claims) and
// notes every other named argument as unread; ok is false when map: is
// not a string literal.
func (t *table) mapName(x args, a *Attribute, row string) (any, bool) {
	var name any
	for _, k := range x.order {
		switch k {
		case "map":
			v, lit := strLit(x.named[k])
			if !lit {
				return nil, false
			}
			name = v
		case "name":
			// @@id(name:) and @@unique(name:) are client-only names.
		default:
			t.tableReason(row, "options", a.Line, "%s: argument %s: is not read", a.Raw, k)
		}
	}
	return name, true
}

// fieldID reads @id (row PR-A1).
func (t *table) fieldID(c *column) {
	a := c.idAttr
	x := splitArgs(a.Args)
	if !x.ok || len(x.pos) > 0 {
		t.openPK(a, "@id arguments are not read")
		return
	}
	name, ok := t.mapName(x, a, "PR-A1")
	if !ok {
		name = nil
		t.tableReason("PR-A1", "primary_key_name", a.Line, "%s: map: is not a string literal", a.Raw)
	}
	if t.pkCols != nil || t.pkRow != "" {
		t.fail(a.Line, "two @id attributes")
		return
	}
	tag("PR-A1")
	t.pkRow, t.pkCols, t.pkName = "PR-A1", []*column{c}, name
}

func (t *table) openPK(a *Attribute, why string) {
	t.pkOpen = true
	t.rec.ConstraintsComplete = false
	t.tableReason("PR-A1", "primary_key", a.Line, "%s: %s", a.Raw, why)
}

// fieldRefs reads a block attribute's field list: literal names of this
// model's columns, each optionally with field arguments (sort:, ops:,
// length:), which are noted as not read.
func (t *table) fieldRefs(e *Expr, a *Attribute, row string) ([]*column, bool) {
	if e == nil || e.Kind != ExprArray || len(e.Items) == 0 {
		return nil, false
	}
	var out []*column
	for _, item := range e.Items {
		name := item.Value
		if item.Kind != ExprConstant && item.Kind != ExprCall || strings.Contains(name, ".") {
			return nil, false
		}
		c := t.byField[name]
		if c == nil || c.rec.Unknown {
			return nil, false
		}
		if item.Kind == ExprCall && item.Args != nil && len(item.Args.Args) > 0 {
			t.tableReason(row, "options", a.Line, "%s: field arguments of %s are not read", a.Raw, name)
		}
		out = append(out, c)
	}
	return out, true
}

// blockID reads @@id (row PR-AA1).
func (t *table) blockID(a *Attribute) {
	x := splitArgs(a.Args)
	var cols []*column
	ok := x.ok && len(x.pos) == 1
	if ok {
		cols, ok = t.fieldRefs(x.pos[0], a, "PR-AA1")
	}
	if !ok {
		tag("D")
		t.openPK(a, "the field list is not literal names of this model's fields")
		return
	}
	name, lit := t.mapName(x, a, "PR-AA1")
	if !lit {
		name = nil
		t.tableReason("PR-AA1", "primary_key_name", a.Line, "%s: map: is not a string literal", a.Raw)
	}
	if t.pkRow != "" {
		t.fail(a.Line, "@id together with @@id, or two @@id")
		return
	}
	tag("PR-AA1")
	t.pkRow, t.pkCols, t.pkName = "PR-AA1", cols, name
}

// unreadIndex records an @@unique or @@index the rows do not read (D):
// the index list is partial and the absence it could contradict (not
// unique, not indexed) is not claimed; other facts are unaffected.
func (t *table) unreadIndex(a *Attribute, unique bool, why string) {
	tag("D")
	t.rec.ConstraintsComplete = false
	if unique {
		t.uniqueOK = false
	} else {
		t.indexOK = false
	}
	t.tableReason("D", "constraint_set", a.Line, "%s: %s", a.Raw, why)
}

// blockIndex reads @@unique (row PR-AA2) and @@index (row PR-AA3).
func (t *table) blockIndex(a *Attribute, unique bool) {
	row := "PR-AA3"
	if unique {
		row = "PR-AA2"
	}
	x := splitArgs(a.Args)
	var cols []*column
	ok := x.ok && len(x.pos) == 1
	if ok {
		cols, ok = t.fieldRefs(x.pos[0], a, row)
	}
	if !ok {
		t.unreadIndex(a, unique, "the field list is not literal names of this model's fields")
		return
	}
	var method any
	if _, partial := x.named["where"]; unique && partial {
		t.unreadIndex(a, unique, "a partial unique index (where:) is not read")
		return
	}
	if !unique {
		for _, k := range x.order {
			switch k {
			case "map", "name":
			case "type":
				v := x.named[k]
				if v.Kind != ExprConstant || !contains(IndexTypes, v.Value) {
					t.unreadIndex(a, unique, "type: is not an index type the contract lists")
					return
				}
				method = v.Value
			case "where":
				t.unreadIndex(a, unique, "a partial index (where:) is not read")
				return
			default:
				t.unreadIndex(a, unique, "argument "+k+": is not read")
				return
			}
		}
		if method == nil {
			method = "BTree"
		}
		tag("PR-AA3/" + method.(string))
		if t.s.provider != providerPostgres {
			method = nil
		}
		delete(x.named, "type")
		x.order = without(x.order, "type")
	}
	name, lit := t.mapName(x, a, row)
	if !lit {
		t.unreadIndex(a, unique, "map: is not a string literal")
		return
	}
	tag(row)
	names := make([]string, len(cols))
	for i, c := range cols {
		names[i] = c.rec.Name
	}
	if unique {
		t.addUnique(name, names, a, row)
	} else {
		t.rec.Indexes = append(t.rec.Indexes, map[string]any{"name": name, "columns": names, "unique": false, "method": method})
	}
	if len(cols) == 1 {
		if unique {
			t.uniqueCols[cols[0]] = "PR-AA2"
		} else {
			t.indexCols[cols[0]] = true
		}
	}
}

func without(list []string, s string) []string {
	var out []string
	for _, v := range list {
		if v != s {
			out = append(out, v)
		}
	}
	return out
}

// finish claims the key facts that need the whole model: primary key,
// unique and index, and the table's assumptions. Foreign keys need every
// model's table (foreignKeys, row PR-R1).
func (t *table) finish() {
	s := t.s
	if s.relationMode == "" || s.provider == "" {
		t.rec.ConstraintsComplete = false
		why := s.providerReason
		if s.relationMode == "" {
			why = "the datasource's relationMode is not a string literal"
		}
		t.tableReason("PR-B1", "constraint_set", t.b.StartLine, "foreign keys are not known: %s", why)
	}
	inPK := map[*column]bool{}
	for _, c := range t.pkCols {
		inPK[c] = true
	}
	if t.pkOpen {
		t.rec.PrimaryKeyOpen = true
	} else if t.pkRow == "PR-AA1" {
		order := make([]string, len(t.pkCols))
		for i, c := range t.pkCols {
			order[i] = c.rec.Name
		}
		t.rec.PrimaryKey = order
	}
	if !t.pkOpen && t.pkRow != "" {
		t.rec.Attributes["primary_key_name"] = t.pkName
	}
	for _, c := range t.cols {
		if c.rec.Unknown {
			continue
		}
		absence := c.absenceOK && t.blockOK
		switch {
		case t.pkOpen:
		case inPK[c]:
			c.claim("primary_key", true, t.pkRow, models.VerificationOracle)
		case absence:
			row := "PR-B3"
			if t.pkRow != "" {
				row = t.pkRow
			}
			c.claim("primary_key", false, row, models.VerificationOracle)
		}
		if row, ok := t.uniqueCols[c]; ok {
			c.claim("unique", true, row, models.VerificationOracle)
		} else if absence && t.uniqueOK {
			c.claim("unique", false, "PR-A2", models.VerificationOracle)
		}
		if t.indexCols[c] {
			c.claim("index", true, "PR-AA3", models.VerificationOracle)
		} else if absence && t.indexOK {
			c.claim("index", false, "PR-AA3", models.VerificationOracle)
		}
	}
	assumptions := []string{"PR-V1", "PR-V2", "PR-V3", "PR-V4"}
	if t.assumptionV5 {
		assumptions = append(assumptions, "PR-V5")
	}
	if s.mappedFacts() {
		assumptions = append(assumptions, "PR-V6")
	}
	t.rec.Attributes["assumptions"] = assumptions
}

// literalText is the database's text of a literal default (row PR-A3 (a))
// on postgresql and sqlite, as Prisma's renderer writes it
// (flavour/postgres/renderer.rs render_default, the same at 5.22.0,
// 6.19.3 and 7.10.0; sqlite as the SQLite oracle reads it at those
// versions): a string or enum value quoted with ' doubled (an enum value
// as its @map'd database name), an integer (on postgresql only from 0 to
// 2147483647, which Postgres keeps uncast) and a boolean as they print.
// Every other literal (Float, Decimal, Json, DateTime, Bytes, lists) and
// every other provider: "" with the reason, so only the default's
// presence is claimed.
func (t *table) literalText(c *column, e *Expr) (string, string) {
	if !oracleProvider(t.s.provider) {
		return "", fmt.Sprintf("the %s text of a literal default is not checked; only its presence is claimed", t.s.provider)
	}
	sqlite := t.s.provider == providerSQLite
	scalar := c.field.Type.Name
	notProven := "the database's text of this literal is not proven by the mapping; only the default's presence is claimed"
	if c.field.Type.List {
		return "", notProven
	}
	quote := func(v string) string { return "'" + strings.ReplaceAll(v, "'", "''") + "'" }
	switch {
	case scalar == "String" && e.Kind == ExprString:
		return quote(e.Value), ""
	case (scalar == "Int" || scalar == "BigInt") && e.Kind == ExprNumber:
		n, err := strconv.ParseInt(e.Value, 10, 64)
		if err != nil || strconv.FormatInt(n, 10) != e.Value {
			return "", notProven
		}
		// Postgres keeps a negative or non-int4 constant as a cast
		// string ('-1'::integer, '3000000000'::bigint); SQLite keeps
		// the text as written.
		if !sqlite && (n < 0 || n > 2147483647) {
			return "", "Postgres stores this integer as a cast string whose type Prisma's DDL does not state; only the default's presence is claimed"
		}
		return e.Value, ""
	case scalar == "Boolean" && e.Kind == ExprConstant && (e.Value == "true" || e.Value == "false"):
		return e.Value, ""
	case t.s.enums[scalar] != nil && e.Kind == ExprConstant:
		en := t.s.enums[scalar]
		if db, ok := en.byName[e.Value]; ok && en.valuesOK {
			return quote(db), ""
		}
		return "", "the enum value's database name is not known"
	}
	return "", notProven
}

// clientDefaultVersion returns why a client-side default function is not
// read at the schema's Prisma version ("" when it is): cuid(2) from 6.0.0
// (prisma-engines 5e70d191c9), ulid() from 6.2.0 (3aa926082b), uuid(n)
// is read from 6.0.0 on (its 5.x history is not checked).
func (t *table) clientDefaultVersion(e *Expr) string {
	hasArg := e.Args != nil && len(e.Args.Args) > 0
	switch {
	case e.Value == "ulid" && !t.s.since(6, 2):
		return "ulid() is a client default from Prisma 6.2.0; the locked version is not proven to be one"
	case (e.Value == "cuid" || e.Value == "uuid") && hasArg && !t.s.since(6, 0):
		return e.Raw + " is a client default from Prisma 6.0.0; the locked version is not proven to be one"
	}
	return ""
}
