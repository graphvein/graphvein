package sequelize

import (
	"fmt"
	"strings"

	"github.com/graphvein/graphvein/internal/extract/models"
	"github.com/graphvein/graphvein/internal/lang/typescript"
)

// This file turns a model's attributes into column facts per dialect, as
// Sequelize 6.37.8's attributeToSQL and createTableQuery write them:
// postgres (dialects/postgres/query-generator.js:62-101, :333-421,
// :622-642), sqlite (dialects/sqlite/query-generator.js:36-80, :206-250),
// mysql and mariadb (dialects/mysql/query-generator.js:77-130, :313-365),
// mssql (dialects/mssql/query-generator.js:126-175, :482-540). Facts are
// derived from the same decisions the generators make; renderColumn
// writes the column definition text itself, which TestDDLRendering
// compares with the generators' output for every form the pass reads.

// ddlMarkers are the texts Sequelize's createTableQuery and its regular
// expressions look for inside a column definition (includes("PRIMARY
// KEY"), includes("INT"), "SERIAL", "NOT NULL", the COMMENT split, the
// REFERENCES split, /^ENUM\(.+\)/, replaceBooleanDefaults). A default,
// label, comment or referenced table written by the user that contains
// one would change how the definition is rewritten, so the column is not
// read.
var ddlMarkers = []string{"PRIMARY KEY", "NOT NULL", "SERIAL", "INT", "COMMENT", "REFERENCES", "AUTOINCREMENT", "auto_increment", "IDENTITY", "ENUM(", "UNIQUE", "DEFAULT"}

func hasMarker(s string) string {
	for _, m := range ddlMarkers {
		if strings.Contains(s, m) {
			return m
		}
	}
	return ""
}

// colFacts is a column's facts as the pass derives them, before they are
// written into the record.
type colFacts struct {
	a   *attr
	rec *models.Column
	tt  typeText
	pk  bool
	// serial is a postgres autoIncrement column (SERIAL family).
	serial bool
	// intPK is sqlite's single primary key whose definition contains INT
	// (rewritten to INTEGER PRIMARY KEY).
	intPK bool
	// defText is the default's SQL text (pg, sqlite); defKind its kind.
	defText string
	// inlineUnique is set when the definition keeps its UNIQUE.
	inlineUnique bool
	// schemable is set when the definition writes a DEFAULT clause, and
	// defUnknown when whether it does is not read (the cyclic
	// rule).
	schemable, defUnknown bool
	// fkWhy says why the foreign-key list is unknown (fkUnknown).
	fkWhy string
}

// columns builds the column records (rows SQ-A1, SQ-A2, SQ-O1, SQ-K2).
func (t *table) columns() bool {
	d := t.gr.dialect
	// Provable failures of the attribute set (row SQ-P1).
	seen := map[string]*attr{}
	for _, a := range t.attrs {
		if a.unknown != "" {
			continue
		}
		if a.virtual {
			if set, _ := readUniqueSet(a); set {
				t.gr.failure(failVirtualUnique, a.file, a.line, "%s.%s: a VIRTUAL attribute with unique: Sequelize writes UNIQUE (%s) for a column the table does not have", t.modelName, a.key, a.field)
				return false
			}
			continue
		}
		if a.nameWhy != "" {
			continue
		}
		if prev := seen[a.field]; prev != nil {
			t.gr.failure(failColumnTwice, a.file, a.line, "%s: the attributes %s and %s both have the column name %s: the CREATE TABLE names it twice", t.modelName, prev.key, a.key, a.field)
			return false
		}
		seen[a.field] = a
		if d == "postgres" && !pgIdentifierOK(a.field) {
			a.unknown, a.unknownRow = "the column name is longer than 63 bytes (postgres truncates it)", "SQ-K2"
		}
	}
	// pkSure: every attribute's primaryKey is known (sqlite's rewrite
	// depends on the number of primary keys).
	pkSure, pkCount := !t.unknownEntries, 0
	for _, a := range t.attrs {
		if a.virtual && a.unknown == "" {
			continue
		}
		if a.unknown != "" || a.primaryKey == triUnknown {
			pkSure = false
			continue
		}
		if set, _ := a.primaryKey.truthy(); set {
			pkCount++
		}
	}
	var cols []*colFacts
	for _, a := range t.attrs {
		if a.virtual && a.unknown == "" {
			t.tag("SQ-A1")
			continue
		}
		rec := &models.Column{Name: a.field, IdentityName: a.key, Attribute: a.key, File: a.file, Line: a.line, Attributes: map[string]any{}, Claims: map[string]models.ClaimSource{}}
		for _, k := range []string{"type", "length", "precision", "scale", "timezone", "nullable", "primary_key", "unique", "index", "default", "server_default", "server_default_text"} {
			rec.Attributes[k] = nil
		}
		// The foreign-key list is decided once every table is built
		// (foreignKeys, row SQ-A3).
		rec.ForeignKeysUnknown = true
		t.rec.Columns = append(t.rec.Columns, rec)
		c := &colFacts{a: a, rec: rec}
		if t.mutated != "" || t.removed[a.key] || a.unknown != "" || a.nameWhy != "" {
			t.unknownCols = append(t.unknownCols, c)
		}
		switch {
		case t.mutated != "":
			c.unknownColumn(t, t.formRow(), "the attributes change after init (%s)", t.mutated)
			continue
		case t.removed[a.key]:
			c.unknownColumn(t, t.formRow(), "%s.removeAttribute(%q) after init removes this attribute", t.modelName, a.key)
			continue
		case a.unknown != "":
			c.unknownColumn(t, a.unknownRow, "%s", a.unknown)
			continue
		case a.nameWhy != "":
			c.unknownColumn(t, "SQ-K2", "the column name is not known: %s", a.nameWhy)
			continue
		}
		cols = append(cols, c)
		if !t.columnFacts(c, pkSure, pkCount) {
			return false
		}
	}
	t.cols = cols
	if ddlHook != nil {
		for _, c := range cols {
			if !c.rec.Unknown {
				ddlHook(t.rec.Name, c)
			}
		}
	}
	return true
}

// ddlHook is a test-only hook: every read column with the definition
// text the pass derived its facts from (TestDDLRendering).
var ddlHook func(table string, c *colFacts)

func readUniqueSet(a *attr) (bool, bool) {
	if a.unique.unknown != "" {
		return false, false
	}
	return a.unique.kind != uniqueNone, true
}

func (c *colFacts) reason(row, fact, format string, a ...any) {
	c.rec.Reasons = append(c.rec.Reasons, models.Reason{Row: row, Fact: models.Fact(fact), File: c.a.file, Line: c.a.line, Reason: fmt.Sprintf(format, a...)})
}

func (c *colFacts) claim(fact string, v any, row string) {
	c.rec.Attributes[fact] = v
	c.rec.Claims[fact] = models.ClaimSource{Row: row}
	if claimHook != nil {
		claimHook(c.rec, fact, row, c.form(fact, v))
	}
}

// unknownColumn makes the whole column unknown: it may not exist (an
// unread type could be VIRTUAL), so the table's column set
// and constraint set are partial.
func (c *colFacts) unknownColumn(t *table, row, format string, a ...any) {
	t.tag(row)
	c.rec.Unknown = true
	for k := range c.rec.Attributes {
		c.rec.Attributes[k] = nil
	}
	// The kit writes the name from the record; enum describes a read type.
	delete(c.rec.Attributes, "name")
	delete(c.rec.Attributes, "enum")
	c.rec.Claims = map[string]models.ClaimSource{}
	c.reason(row, "", format, a...)
	c.rec.Reasons[len(c.rec.Reasons)-1].Fact = nil
	t.rec.ColumnsComplete = false
	t.rec.ConstraintsComplete = false
}

// claimHook is a test-only hook: every claim with the form the oracle
// compares it under (TestSequelizeClaims).
var claimHook func(col *models.Column, fact, row, form string)

// form names the oracle form of a claim (TestSequelizeClaims checks that
// every oracle-labelled claim's form is one the investigation compared).
func (c *colFacts) form(fact string, v any) string {
	a := c.a
	switch fact {
	case "type":
		switch {
		case c.serial:
			return "autoIncrement " + a.typ.form
		case c.intPK:
			return "sqlite integer primary key " + a.typ.form
		}
		return a.typ.form
	case "nullable":
		switch {
		case c.serial:
			return "autoIncrement"
		case c.intPK:
			return "sqlite integer primary key"
		case c.pk:
			return "primary key"
		}
		return "allowNull " + triName(a.allowNull)
	case "default", "server_default":
		if c.serial {
			return "autoIncrement"
		}
		if a.def.kind == "" {
			return "absent"
		}
		return a.def.kind + " on " + a.typ.key
	case "unique":
		return fmt.Sprintf("unique %v", v)
	}
	return fmt.Sprintf("%v", v)
}

func triName(t tri) string {
	switch t {
	case triTrue:
		return "true"
	case triFalse:
		return "false"
	}
	return "absent"
}

// columnFacts derives one column's facts on the group's dialect.
func (t *table) columnFacts(c *colFacts, pkSure bool, pkCount int) bool {
	a := c.a
	d := t.gr.dialect
	c.claim("name", a.field, a.row("SQ-K2"))
	pk, _ := a.primaryKey.truthy()
	c.pk = pk
	c.claim("primary_key", pk, a.row("SQ-A2"))
	if d == "" {
		for _, f := range []string{"type", "nullable", "unique", "index", "default", "server_default"} {
			c.reason("SQ-K1", f, "the dialect is not known (open point O3), so this dialect-dependent fact is not claimed")
		}
		return true
	}
	t.tag(a.row("SQ-A1"))
	t.tag(a.row("SQ-A2"))
	tt, why := a.typ.render(d, t.rec.Name, a.field, t.hasSchema)
	if why != "" {
		c.unknownColumn(t, "SQ-A1", "%s", why)
		return true
	}
	c.tt = tt
	// User texts Sequelize's rewriting would match.
	texts := []string{}
	switch a.def.kind {
	case defString, defLiteral, defFn, defJSON:
		texts = append(texts, a.def.value)
	}
	if a.typ.key == "ENUM" || a.typ.elem != nil && a.typ.elem.key == "ENUM" {
		texts = append(texts, a.typ.values...)
		if a.typ.elem != nil {
			texts = append(texts, a.typ.elem.values...)
		}
	}
	if a.refKnown {
		texts = append(texts, a.refText)
	}
	if d != "postgres" && d != "sqlite" && a.commentKnown {
		texts = append(texts, a.commentText)
	}
	for _, s := range texts {
		if m := hasMarker(s); m != "" {
			c.unknownColumn(t, "SQ-A2", "the text %q contains %q, which Sequelize's CREATE TABLE rewriting matches", s, m)
			return true
		}
	}
	if d != "postgres" && d != "sqlite" && a.comment && !a.commentKnown {
		c.unknownColumn(t, "SQ-A2", "a comment that is not a string literal is written into the %s column definition, which Sequelize's CREATE TABLE rewriting reads", d)
		return true
	}
	if a.autoIncrement == triUnknown || a.allowNull == triUnknown && pk {
		c.unknownColumn(t, "SQ-A2", "autoIncrement or allowNull of a primary key is not a literal")
		return true
	}
	isEnum := a.typ.key == "ENUM" || a.typ.elem != nil && a.typ.elem.key == "ENUM"
	if d == "postgres" && isEnum && (a.references || strings.Contains(a.def.value, ")")) {
		c.unknownColumn(t, "SQ-A2", "postgres replaces the column definition up to its last \")\" with the enum type name (query-generator.js:638-640), which a default or REFERENCES clause after the type changes")
		return true
	}
	ai, _ := a.autoIncrement.truthy()
	// Default text and validity.
	defKind, defText, defWhy, fail := t.defaultFor(a, d)
	if fail != "" {
		kind := failFnDefault
		if a.typ.key == "ENUM" {
			kind = failEnumDefault
		}
		t.gr.failure(kind, a.file, a.line, "%s.%s: %s", t.modelName, a.key, fail)
		return false
	}
	if d == "sqlite" && (strings.HasPrefix(defText, "true") || strings.HasPrefix(defText, "false") || strings.HasPrefix(defText, "'true") || strings.HasPrefix(defText, "'false")) && defKind != defBoolean {
		c.unknownColumn(t, "SQ-A2", "sqlite: Sequelize's replaceBooleanDefaults rewrites DEFAULT %s (query-generator.js:348-350)", defText)
		return true
	}
	schemable := defKind == defNull || defKind == defString || defKind == defNumber || defKind == defBoolean || defKind == defJSON || defKind == defLiteral || defKind == defFn
	switch d {
	case "postgres":
		if ai {
			switch a.typ.key {
			case "INTEGER", "BIGINT":
			case "SMALLINT":
				c.unknownColumn(t, "SQ-A2", "autoIncrement on SMALLINT (SMALLSERIAL) is not a form the oracle compared")
				return true
			default:
				t.gr.failure(failSerialType, a.file, a.line, "%s.%s: autoIncrement on %s writes \"%s SERIAL\", which postgres rejects", t.modelName, a.key, a.typ.form, tt.sql)
				return false
			}
			if schemable {
				t.gr.failure(failSerialDefault, a.file, a.line, "%s.%s: autoIncrement (SERIAL) with a default: postgres rejects two defaults", t.modelName, a.key)
				return false
			}
			if defWhy != "" {
				c.unknownColumn(t, "SQ-A2", "autoIncrement with a default that is not read (%s)", defWhy)
				return true
			}
			c.serial = true
		}
	case "sqlite":
		if pk {
			if !pkSure {
				c.unknownColumn(t, "SQ-A2", "sqlite: whether this primary key is rewritten depends on the number of primary keys, and an attribute that is not read may be one")
				return true
			}
			if a.references && !a.refKnown {
				c.unknownColumn(t, "SQ-A2", "sqlite: the primary key's REFERENCES clause decides whether its definition contains INT")
				return true
			}
			full := renderSQLiteAttr(a, tt.sql, defText, defKind)
			switch {
			case pkCount > 1 && ai:
				t.gr.failure(failSqliteAutoIncrement, a.file, a.line, "%s.%s: an autoIncrement member of a composite primary key: sqlite rejects \"NOT NULL AUTOINCREMENT\"", t.modelName, a.key)
				return false
			case pkCount == 1 && strings.Contains(full, "INT"):
				if a.typ.key != "INTEGER" && a.typ.key != "BIGINT" {
					c.unknownColumn(t, "SQ-A2", "sqlite rewrites this %s primary key to INTEGER PRIMARY KEY; the oracle compared the rewrite for INTEGER and BIGINT only", a.typ.form)
					return true
				}
				c.intPK = true
			case pkCount == 1 && ai:
				t.gr.failure(failSqliteAutoIncrement, a.file, a.line, "%s.%s: AUTOINCREMENT on a primary key of type %s: sqlite allows it only on INTEGER PRIMARY KEY", t.modelName, a.key, tt.sql)
				return false
			}
		}
	case "mysql", "mariadb":
		if ai && !pk {
			c.unknownColumn(t, "SQ-A2", "auto_increment on a column that is not the primary key is not mapped")
			return true
		}
	case "mssql":
		if a.typ.key == "ENUM" {
			c.unknownColumn(t, "SQ-A1", "mssql writes an ENUM as VARCHAR(255) with a CHECK and drops the column's other clauses (query-generator.js:496-504); not mapped")
			return true
		}
		if ai {
			switch a.typ.key {
			case "INTEGER", "BIGINT", "SMALLINT":
			default:
				c.unknownColumn(t, "SQ-A2", "IDENTITY on a column that is not an integer is not mapped")
				return true
			}
		}
	}
	// Type.
	typeRow := a.row("SQ-A1")
	switch {
	case c.serial:
		c.claim("type", strings.ToLower(a.typ.key), typeRow)
	case c.intPK:
		c.claim("type", "INTEGER", typeRow)
	default:
		c.claim("type", tt.claim, typeRow)
	}
	if isEnum && d == "postgres" {
		el := a.typ
		if a.typ.elem != nil {
			el = *a.typ.elem
		}
		var values any
		if el.valuesKnown {
			values = el.values
		} else {
			c.reason("SQ-A1", "enum", "the ENUM labels are not literal (open point O6: the type is claimed under SQ-V3, the labels are unknown)")
		}
		c.rec.Attributes["enum"] = map[string]any{"name": "enum_" + t.rec.Name + "_" + a.field, "schema": "public", "values": values}
	}
	// Nullability.
	row := a.row("SQ-A2")
	switch d {
	case "postgres":
		switch {
		case pk || c.serial || a.allowNull == triFalse:
			c.claim("nullable", false, row)
		case a.allowNull == triUnknown:
			c.reason("SQ-A2", "nullable", "allowNull is not a literal")
		default:
			c.claim("nullable", true, row)
		}
	case "sqlite":
		switch {
		case c.intPK:
			c.claim("nullable", true, row)
		case pk && pkCount > 1:
			c.claim("nullable", false, row)
		case a.allowNull == triFalse:
			c.claim("nullable", false, row)
		case a.allowNull == triUnknown:
			c.reason("SQ-A2", "nullable", "allowNull is not a literal")
		default:
			c.claim("nullable", true, row)
		}
	case "mysql", "mariadb":
		switch {
		case a.allowNull == triFalse:
			c.claim("nullable", false, row)
		case a.allowNull == triUnknown:
			c.reason("SQ-A2", "nullable", "allowNull is not a literal")
		case pk:
			c.reason("SQ-A2", "nullable", "a primary key without allowNull: false: Sequelize writes no NOT NULL; the server's own rule is not mapped")
		default:
			c.claim("nullable", true, row)
		}
	case "mssql":
		switch {
		case a.allowNull == triFalse:
			c.claim("nullable", false, row)
		case pk:
			c.reason("SQ-A2", "nullable", "a primary key without allowNull: false: Sequelize writes neither NULL nor NOT NULL; the server's own rule is not mapped")
		case a.allowNull == triUnknown:
			c.reason("SQ-A2", "nullable", "allowNull is not a literal")
		case schemable:
			c.reason("SQ-A2", "nullable", "mssql: Sequelize writes neither NULL nor NOT NULL for a column with a default, so its nullability is the server's ANSI_NULL_DFLT setting")
		default:
			c.claim("nullable", true, row)
		}
	}
	if src, ok := c.rec.Claims["nullable"]; ok && a.nullRow != "" {
		src.Row, src.File, src.Line = a.nullRow, a.nullFile, a.nullLine
		c.rec.Claims["nullable"] = src
		t.tag(a.nullRow)
	}
	// Default.
	c.defText = defText
	c.schemable = schemable
	c.defUnknown = defWhy != ""
	switch {
	case c.serial:
		c.claim("default", "server", row)
		c.claim("server_default", true, row)
		c.reason("SQ-A2", "server_default_text", "SERIAL: the nextval default names a sequence postgres chooses")
	case c.intPK:
		c.reason("SQ-A2", "default", "an integer primary key on sqlite is the rowid alias (Sequelize drops the rest of its definition); no default is claimed")
		c.reason("SQ-A2", "server_default", "an integer primary key on sqlite is the rowid alias; no default is claimed")
	case ai:
		c.reason("SQ-A2", "default", "%s generates the value; it is not a default, and none is claimed", d)
		c.reason("SQ-A2", "server_default", "%s generates the value; none is claimed", d)
	case defWhy != "":
		c.reason("SQ-A2", "default", "%s", defWhy)
		c.reason("SQ-A2", "server_default", "%s", defWhy)
	case defKind == defAbsent || defKind == defNull || mappedWithoutDefault(d, tt.sql):
		c.claim("default", "absent", row)
		c.claim("server_default", false, row)
	case defKind == defClient:
		c.reason("SQ-A2", "default", "the default (%s) is set by Sequelize at runtime; the database has none", a.def.value)
		c.claim("server_default", false, row)
	default:
		c.claim("default", "server", row)
		c.claim("server_default", true, row)
		switch {
		case defKind == defLiteral || defKind == defFn:
			c.reason("SQ-A2", "server_default_text", "a literal or fn default: the database re-renders its text, so only its presence is claimed")
		case d == "postgres" || d == "sqlite":
			c.rec.Attributes["server_default_text"] = defText
		default:
			c.reason("SQ-A2", "server_default_text", "the default's text is not mapped on %s", d)
		}
	}
	// Inline uniqueness (the table pass adds groups and indexes).
	if a.unique.kind == uniqueTrue && !c.intPK {
		c.inlineUnique = true
	}
	return true
}

// defaultFor validates and renders an attribute's default on dialect: the
// kind the claim uses (absent, null, client, or the literal kinds), its
// SQL text on postgres and sqlite, why it is not read, or a provable
// failure (Sequelize's validation of the default throws, or the database
// rejects it).
func (t *table) defaultFor(a *attr, d string) (kind, text, why, fail string) {
	def := a.def
	if def.why != "" {
		return "", "", def.why, ""
	}
	if def.kind == "" || def.kind == defAbsent {
		return defAbsent, "", "", ""
	}
	key := a.typ.key
	isJSON := key == "JSON" || key == "JSONB"
	pgOrLite := d == "postgres" || d == "sqlite"
	quote := func(s string) (string, string) {
		if strings.ContainsRune(s, 0) {
			return "", "a default with a NUL character is not read"
		}
		return sqlQuote(s), ""
	}
	switch def.kind {
	case defNull, defClient:
		return def.kind, "NULL", "", ""
	case defLiteral:
		return defLiteral, def.value, "", ""
	case defFn:
		if d == "sqlite" {
			return "", "", "", "a fn(...) default on sqlite writes DEFAULT " + def.value + ", which sqlite rejects (it needs parentheses)"
		}
		return defFn, def.value, "", ""
	case defString:
		switch {
		case key == "STRING" || key == "CHAR" || key == "TEXT":
			s, w := quote(def.value)
			return defString, s, w, ""
		case key == "ENUM":
			// Sequelize does not validate the default (typeValidation is
			// off by default, lib/sequelize.js:129): postgres rejects a
			// default outside the enum type's labels, and mysql and mariadb
			// one outside ENUM(...); sqlite writes TEXT and takes any
			// default.
			strict := d == "postgres" || d == "mysql" || d == "mariadb"
			if !a.typ.valuesKnown && strict {
				return "", "", "an ENUM default whose labels are not literal: " + d + " rejects a default outside them", ""
			}
			found := !strict
			for _, v := range a.typ.values {
				found = found || v == def.value
			}
			if !found {
				return "", "", "", fmt.Sprintf("the default %q is not one of the ENUM's labels, which %s rejects", def.value, d)
			}
			s, w := quote(def.value)
			return defString, s, w, ""
		case isJSON:
			var b strings.Builder
			writeJSONString(&b, def.value)
			s, w := quote(b.String())
			return defJSON, s, w, ""
		}
	case defNumber:
		integer := !strings.Contains(def.value, ".")
		switch {
		case (key == "INTEGER" || key == "BIGINT" || key == "SMALLINT") && integer:
			return defNumber, def.value, "", ""
		case key == "FLOAT" || key == "DOUBLE":
			if pgOrLite {
				return defNumber, "'" + def.value + "'", "", ""
			}
			return defNumber, def.value, "", ""
		case key == "DECIMAL":
			return defNumber, def.value, "", ""
		case isJSON:
			return defJSON, "'" + def.value + "'", "", ""
		}
	case defBoolean:
		switch {
		case key == "BOOLEAN":
			if d == "sqlite" {
				if def.value == "true" {
					return defBoolean, "1", "", ""
				}
				return defBoolean, "0", "", ""
			}
			return defBoolean, def.value, "", ""
		case isJSON:
			return defJSON, "'" + def.value + "'", "", ""
		}
	case defJSON:
		if isJSON {
			s, w := quote(def.value)
			return defJSON, s, w, ""
		}
	}
	return "", "", fmt.Sprintf("a %s default on %s is not a form the oracle compared", def.kind, a.typ.form), ""
}

// mappedWithoutDefault reports a mysql column whose rendered type gets no
// DEFAULT clause (dialects/mysql/query-generator.js:43, :327).
func mappedWithoutDefault(d, sqlType string) bool {
	if d != "mysql" && d != "mariadb" {
		return false
	}
	switch sqlType {
	case "BLOB", "TEXT", "GEOMETRY", "JSON":
		return true
	}
	return false
}

// renderSQLiteAttr writes sqlite's attributesToSQL text for one attribute
// (without the REFERENCES actions): the input of createTableQuery's
// includes("INT") test.
func renderSQLiteAttr(a *attr, sqlType, defText, defKind string) string {
	s := sqlType
	if a.allowNull == triFalse {
		s += " NOT NULL"
	}
	if defKind != defAbsent && defKind != defClient && defKind != "" {
		s += " DEFAULT " + defText
	}
	if a.unique.kind == uniqueTrue {
		s += " UNIQUE"
	}
	if set, _ := a.primaryKey.truthy(); set {
		s += " PRIMARY KEY"
		if ai, _ := a.autoIncrement.truthy(); ai {
			s += " AUTOINCREMENT"
		}
	}
	if a.references {
		s += " REFERENCES `" + a.refText + "`"
	}
	return s
}

// uniqueFacts lists the table's unique constraints and sets each read
// column's unique and index facts: an attribute's `unique: true` is an
// inline UNIQUE; attributes sharing a group name (a string or {name})
// form one UNIQUE (columns) when the last member's unique is not true
// (lib/model.js:854-869, customIndex); a single-column unique index (row
// SQ-O2) also makes its column unique. A group name is not the
// constraint's name.
func (t *table) uniqueFacts() {
	d := t.gr.dialect
	if d == "" {
		t.rec.ElementsUnknown = true
		t.constraintsPartial("SQ-K1", t.m.Span.StartLine, "the dialect is not known (open point O3), so the unique constraints and indexes are not claimed")
		return
	}
	groupsSure := !t.unknownEntries
	type grp struct {
		name   string
		fields []string
		custom bool
	}
	var groups []*grp
	byName := map[string]*grp{}
	for _, a := range t.attrs {
		if a.unknown != "" || a.nameWhy != "" {
			groupsSure = false
			continue
		}
		if a.unique.unknown != "" {
			groupsSure = false
			continue
		}
		if a.unique.kind == uniqueNone {
			continue
		}
		name := a.unique.name
		if a.unique.kind == uniqueTrue {
			name = t.rec.Name + "_" + a.key + "_unique"
		}
		g := byName[name]
		if g == nil {
			g = &grp{name: name}
			byName[name] = g
			groups = append(groups, g)
		}
		g.fields = append(g.fields, a.field)
		g.custom = a.unique.kind != uniqueTrue
	}
	single := map[string]bool{}
	for _, c := range t.cols {
		if c.inlineUnique && !c.rec.Unknown {
			t.rec.Uniques = append(t.rec.Uniques, map[string]any{"name": nil, "columns": []string{c.a.field}})
			single[c.a.field] = true
		}
	}
	if groupsSure {
		for _, g := range groups {
			if !g.custom {
				continue
			}
			t.rec.Uniques = append(t.rec.Uniques, map[string]any{"name": nil, "columns": append([]string{}, g.fields...)})
			if len(g.fields) == 1 {
				single[g.fields[0]] = true
			}
		}
	} else if len(groups) > 0 || t.unknownEntries {
		t.constraintsPartial("SQ-A2", t.m.Span.StartLine, "an attribute that is not read may join a unique group, so the composite unique constraints are not listed")
	}
	for _, c := range t.cols {
		if c.rec.Unknown {
			continue
		}
		a := c.a
		row := a.row("SQ-A2")
		inGroup := false
		for _, g := range groups {
			if g.custom && containsString(g.fields, a.field) {
				inGroup = true
			}
		}
		switch {
		case a.unique.unknown != "":
			c.reason("SQ-A2", "unique", "%s", a.unique.unknown)
			t.constraintsPartial("SQ-A2", a.line, "%s.%s: %s", t.modelName, a.key, a.unique.unknown)
		case c.intPK && a.unique.kind == uniqueTrue:
			c.reason("SQ-A2", "unique", "sqlite drops the UNIQUE of an integer primary key's definition; not claimed")
		case single[a.field]:
			c.claim("unique", true, row)
		case t.uniqueIdx[a.field]:
			c.claim("unique", true, "SQ-O2")
		case inGroup && !groupsSure:
			c.reason("SQ-A2", "unique", "an attribute that is not read may join this unique group")
		case !t.indexesKnown:
			c.reason("SQ-O2", "unique", "an index that is not read may make the column unique")
		default:
			c.claim("unique", false, row)
		}
		switch {
		case t.indexed[a.field]:
			c.claim("index", true, "SQ-O2")
		case !t.indexesKnown:
			c.reason("SQ-O2", "index", "an index that is not read may cover the column")
		case t.hasIndexes:
			c.claim("index", false, "SQ-O2")
		case a.index != triAbsent:
			c.reason("SQ-A2", "index", "the attribute's index key (outside a JSONB column) is not compared")
		default:
			c.claim("index", false, row)
		}
	}
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// indexes reads the merged indexes option and the JSONB `index: true`
// attributes (row SQ-O2): each index's name (omitted:
// underscore("<table>_<fields>"), lib/utils.js:413-421),
// columns, uniqueness, method (postgres; `btree` without `using`) and
// whether it is partial (a non-empty `where` object; its predicate is not
// claimed). An index naming no column of the table is a provable failure
// (row SQ-P1).
func (t *table) indexes() bool {
	t.indexed, t.uniqueIdx = map[string]bool{}, map[string]bool{}
	t.indexesKnown = true
	d := t.gr.dialect
	if d == "" {
		t.indexesKnown = false
		return true
	}
	if t.opts.indexesUnknown != "" {
		t.indexesKnown = false
		t.constraintsPartial("SQ-O2", t.m.Span.StartLine, "%s", t.opts.indexesUnknown)
	}
	columns := map[string]bool{}
	for _, c := range t.rec.Columns {
		columns[c.Name] = true
	}
	add := func(entry map[string]any, cols []string, unique, partial bool) {
		t.hasIndexes = true
		t.rec.Indexes = append(t.rec.Indexes, entry)
		if len(cols) == 1 {
			switch {
			case !unique:
				t.indexed[cols[0]] = true
			case !partial:
				t.uniqueIdx[cols[0]] = true
			}
		}
	}
	for _, iv := range t.opts.indexes {
		t.tag("SQ-O2")
		entry, cols, unique, partial, why, fail := t.readIndex(iv.e, columns)
		if fail != "" {
			kind := failIndexColumn
			if strings.Contains(fail, "without fields") {
				kind = failIndexNoFields
			}
			t.gr.failure(kind, iv.file, iv.e.Span.StartLine, "%s: %s", t.modelName, fail)
			return false
		}
		if why != "" {
			t.indexesKnown = false
			t.constraintsPartial("SQ-O2", iv.e.Span.StartLine, "an index is not read: %s", why)
			continue
		}
		add(entry, cols, unique, partial)
	}
	for _, c := range t.cols {
		a := c.a
		if a.index == triAbsent {
			continue
		}
		if a.typ.key != "JSONB" {
			continue
		}
		if a.index != triTrue {
			if a.index == triUnknown {
				t.indexesKnown = false
				t.constraintsPartial("SQ-O2", a.line, "%s.%s: the index key is not a literal", t.modelName, a.key)
			}
			continue
		}
		t.tag("SQ-O2")
		name, ok := underscore(t.rec.Name + "_" + a.field)
		if !ok || !pgIdentifierOK(name) {
			t.indexesKnown = false
			t.constraintsPartial("SQ-O2", a.line, "%s.%s: the JSONB index name is not computed", t.modelName, a.key)
			continue
		}
		add(map[string]any{"name": name, "columns": []string{a.field}, "unique": false, "method": "gin", "partial": false, "where": nil}, []string{a.field}, false, false)
	}
	return true
}

// pgIndexMethods are the access methods `using` is read for on postgres.
var pgIndexMethods = map[string]bool{"btree": true, "hash": true, "gist": true, "gin": true, "brin": true, "spgist": true}

// readIndex reads one element of the indexes option.
func (t *table) readIndex(e *typescript.ModelExpr, columns map[string]bool) (map[string]any, []string, bool, bool, string, string) {
	d := t.gr.dialect
	if e.Kind != typescript.ModelObject {
		return nil, nil, false, false, "the element is not an object literal", ""
	}
	var fields []string
	var name *string
	unique, partial := false, false
	method := ""
	for i := range e.Entries {
		en := &e.Entries[i]
		if en.Kind != typescript.ModelProperty {
			return nil, nil, false, false, "an entry that is not a plain property", ""
		}
		v := &en.Value
		switch en.Key {
		case "fields":
			if v.Kind != typescript.ModelArray || len(v.Elements) == 0 {
				return nil, nil, false, false, "fields is not a non-empty array literal", ""
			}
			fields = nil
			for j := range v.Elements {
				el := &v.Elements[j]
				if s, ok := literalString(el); ok {
					fields = append(fields, s)
					continue
				}
				if el.Kind != typescript.ModelObject {
					return nil, nil, false, false, "a field that is not a string literal or {name, order}", ""
				}
				fname := ""
				for k := range el.Entries {
					fe := &el.Entries[k]
					s, ok := literalString(&fe.Value)
					switch {
					case fe.Kind != typescript.ModelProperty || !ok:
						return nil, nil, false, false, "a field object entry that is not a string literal", ""
					case fe.Key == "name":
						fname = s
					case fe.Key == "order" && (s == "ASC" || s == "DESC"):
					default:
						return nil, nil, false, false, "the field object key " + fe.Key + " is not read", ""
					}
				}
				if fname == "" {
					return nil, nil, false, false, "a field object without a name", ""
				}
				fields = append(fields, fname)
			}
		case "unique":
			switch readFlag(v) {
			case triTrue:
				unique = true
			case triFalse, triAbsent:
				unique = false
			default:
				return nil, nil, false, false, "unique is not a boolean literal", ""
			}
		case "name":
			s, ok := literalString(v)
			if !ok || s == "" {
				return nil, nil, false, false, "the name is not a string literal", ""
			}
			name = &s
		case "using":
			s, ok := literalString(v)
			if !ok || d != "postgres" || !pgIndexMethods[strings.ToLower(s)] {
				return nil, nil, false, false, "using is read on postgres only, as a literal access method", ""
			}
			method = strings.ToLower(s)
		case "where":
			if d == "mysql" || d == "mariadb" || d == "mssql" {
				return nil, nil, false, false, "a partial index is not mapped on " + d, ""
			}
			if v.Kind != typescript.ModelObject || len(v.Entries) == 0 {
				return nil, nil, false, false, "where is not a non-empty object literal", ""
			}
			partial = true
		default:
			return nil, nil, false, false, "the index key " + en.Key + " is not read", ""
		}
	}
	if fields == nil {
		return nil, nil, false, false, "", "an index without fields: Sequelize throws (lib/model.js:602-604)"
	}
	for _, f := range fields {
		if !columns[f] {
			if !t.rec.ColumnsComplete {
				return nil, nil, false, false, "the field " + f + " is not a known column (a column that is not read may have that name)", ""
			}
			return nil, nil, false, false, "", "the index names " + f + ", which is not a column of the table"
		}
	}
	if name == nil {
		n, ok := underscore(t.rec.Name + "_" + strings.Join(fields, "_"))
		if !ok {
			return nil, nil, false, false, "the generated name is not computed (non-ASCII)", ""
		}
		name = &n
	}
	if d == "postgres" && !pgIdentifierOK(*name) {
		return nil, nil, false, false, "the index name is longer than 63 bytes (postgres truncates it)", ""
	}
	entry := map[string]any{"name": *name, "columns": fields, "unique": unique}
	if d == "postgres" {
		if method == "" {
			method = "btree"
		}
		entry["method"] = method
	}
	if d == "postgres" || d == "sqlite" {
		entry["partial"], entry["where"] = partial, nil
	}
	return entry, fields, unique, partial, "", ""
}
