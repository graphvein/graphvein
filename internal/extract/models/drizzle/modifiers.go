package drizzle

import (
	"fmt"
	"strings"

	"github.com/graphvein/graphvein/internal/extract/models"
	"github.com/graphvein/graphvein/internal/lang/typescript"
)

// clientModifiers fill values in the Drizzle client only (A3): no
// database default, no constraint.
var clientModifiers = map[string]bool{"$defaultFn": true, "$default": true, "$onUpdate": true, "$onUpdateFn": true}

// unreadModifier records a modifier no row reads (row D): the facts it
// could change are unknown.
func (c *column) unreadModifier(l link, format string, a ...any) {
	c.tag("D")
	if c.unread == "" {
		c.unread = l.Name
	}
	c.t.rec.ConstraintsComplete = false
	c.rec.Reasons = append(c.rec.Reasons, models.Reason{Row: "D", Fact: nil, File: c.file, Line: l.Line, Reason: fmt.Sprintf(".%s(): ", l.Name) + fmt.Sprintf(format, a...)})
}

// modState is modifier state kept for the facts set at the end: the row
// that made the column not null, the generated expression, the line of
// the default modifier.
type modState struct {
	notNullRow string
	genExpr    *typescript.ModelExpr
	defLine    int
}

// modifiers applies the builder's method calls in order, as each sets the
// builder's config (drizzle-orm column-builder.js, <dialect>-core
// columns/common.js), rows DR-C1 (.array()) and DR-C2.
func (c *column) modifiers(links []link) {
	g := c.t.g
	d := g.dialect
	if c.serial && d == "postgresql" {
		// serial, smallserial, bigserial set notNull and hasDefault in
		// their constructors (pg-core/columns/serial.js:7-8).
		c.notNull = true
		c.ms.notNullRow = "DR-C1"
	}
	for i, l := range links {
		n := len(l.Args)
		switch {
		case l.Name == "array" && d == "postgresql":
			if i > 0 {
				// PgArrayBuilder starts from a fresh config
				// (pg-core/columns/common.js:147-153): what the earlier
				// modifiers set is not on the array column.
				c.resetModifiers()
				c.unreadModifier(l, "modifiers written before .array() are not on the array column (a new builder); the column's other facts are not read")
			}
			c.tag("DR-C1")
			switch {
			case n > 0:
				c.reason("DR-C1", models.Fact("type"), ".array() with a size argument is not read")
				c.sqlType = ""
			case c.serial || c.sqlType == "":
				c.sqlType = ""
			default:
				c.sqlType += "[]"
			}
			c.serial = false
		case l.Name == "notNull" && n == 0:
			c.tag("DR-C2")
			c.notNull, c.ms.notNullRow = true, "DR-C2"
		case l.Name == "primaryKey" && (n == 0 || d == "sqlite" && n == 1 && autoIncrementArg(l.Args[0])):
			c.tag("DR-C2")
			c.primaryKey, c.notNull, c.ms.notNullRow = true, true, "DR-C2"
			if d == "sqlite" && c.sqlType == "integer" && c.kind == "builtin" {
				c.sqliteIntegerPK = true
			}
		case l.Name == "unique" && (n <= 1 || d == "postgresql" && n == 2):
			c.tag("DR-C2")
			c.unique, c.uniqueName, c.uniqueNameKnown, c.nullsNotDistinct = true, nil, true, false
			if n >= 1 && !(l.Args[0].Kind == typescript.ModelIdentifier && l.Args[0].Name == "undefined") {
				if s, ok := literalString(l.Args[0]); ok {
					c.uniqueName = &s
				} else {
					c.uniqueNameKnown = false
				}
			}
			if n == 2 {
				c.nullsNotDistinct = nil
				if cfg, ok := configValues(&l.Args[1]); ok {
					switch v, _ := literalString(cfg["nulls"]); {
					case len(cfg) == 1 && v == "not distinct":
						c.nullsNotDistinct = true
					case len(cfg) == 1 && v == "distinct", len(cfg) == 0:
						c.nullsNotDistinct = false
					}
				}
			}
		case l.Name == "default" && n == 1:
			c.tag("DR-C2")
			arg := l.Args[0]
			c.def, c.defFile, c.defIsSQL, c.defSQL, c.defUnknown, c.ms.defLine = &arg, c.file, false, "", "", l.Line
			c.readDefaultArg()
		case l.Name == "defaultNow" && n == 0 && d == "postgresql" && c.kind == "builtin" && (strings.HasPrefix(c.sqlType, "timestamp") || strings.HasPrefix(c.sqlType, "time") || c.sqlType == "date"):
			// date.common.js:6-8: default(sql`now()`).
			c.tag("DR-C2")
			c.def, c.defIsSQL, c.defSQL, c.defUnknown, c.ms.defLine = nil, true, "now()", "", l.Line
		case l.Name == "defaultRandom" && n == 0 && d == "postgresql" && c.sqlType == "uuid":
			// uuid.js:12-14: default(sql`gen_random_uuid()`).
			c.tag("DR-C2")
			c.def, c.defIsSQL, c.defSQL, c.defUnknown, c.ms.defLine = nil, true, "gen_random_uuid()", "", l.Line
		case l.Name == "defaultNow" && n == 0 && d == "mysql" && c.kind == "builtin" && strings.HasPrefix(c.sqlType, "timestamp"):
			// mysql-core date.common.js:6-8 (timestamp only):
			// default(sql`(now())`); drizzle-kit writes DEFAULT (now())
			// (bin.cjs:16064-16066). MySQL default texts are presence only.
			c.tag("DR-C2")
			c.def, c.defIsSQL, c.defSQL, c.defUnknown, c.ms.defLine = nil, true, "(now())", "", l.Line
		case l.Name == "onUpdateNow" && n == 0 && d == "mysql" && c.kind == "builtin" && strings.HasPrefix(c.sqlType, "timestamp"):
			// date.common.js:10-14: hasOnUpdateNow, and drizzle-orm marks
			// the column as having a default; drizzle-kit writes ON
			// UPDATE CURRENT_TIMESTAMP (bin.cjs:16028, :23158). Whether
			// MySQL then gives the column an implicit default depends on
			// the server (explicit_defaults_for_timestamp), so without an
			// explicit default the default is unknown; nothing else changes.
			c.tag("DR-C2")
			c.onUpdateNow = true
		case l.Name == "autoincrement" && n == 0 && d == "mysql" && c.kind == "builtin" && mysqlAutoIncrement[strings.TrimSuffix(c.sqlType, " unsigned")]:
			// mysql-core common.js:61-65; drizzle-kit writes AUTO_INCREMENT
			// (bin.cjs:16027, :23159): MySQL generates the value. It is not
			// a default; the other facts are read as usual.
			c.tag("DR-C2")
			c.autoIncrement = true
		case clientModifiers[l.Name] && n == 1:
			c.tag("DR-C2")
			c.client = true
		case l.Name == "$type" && n == 0:
			c.tag("DR-C2")
		case l.Name == "references":
			c.readReference(l)
		case l.Name == "generatedAlwaysAs" && n == 1 && d == "postgresql":
			c.tag("DR-C2")
			c.generated = true
			arg := l.Args[0]
			c.ms.genExpr = &arg
		case (l.Name == "generatedAlwaysAsIdentity" || l.Name == "generatedByDefaultAsIdentity") && n <= 1 && d == "postgresql" && c.kind == "builtin" && (c.sqlType == "integer" || c.sqlType == "smallint" || c.sqlType == "bigint"):
			// int.common.js:5-38: identity, notNull and hasDefault.
			c.tag("DR-C2")
			c.identity = "always"
			if l.Name == "generatedByDefaultAsIdentity" {
				c.identity = "by default"
			}
			c.notNull, c.ms.notNullRow = true, "DR-C2"
		default:
			c.unreadModifier(l, "this modifier (or these arguments) is not read")
		}
	}
}

// mysqlAutoIncrement are the builders of the mysql run list with
// .autoincrement() (MySqlColumnBuilderWithAutoIncrement: the integer
// types and serial).
var mysqlAutoIncrement = map[string]bool{"int": true, "smallint": true, "tinyint": true, "mediumint": true, "bigint": true, "serial": true}

// resetModifiers forgets the modifier state (a fresh builder).
func (c *column) resetModifiers() {
	c.notNull, c.primaryKey, c.unique, c.generated, c.client = false, false, false, false, false
	c.refs, c.refUnknown = nil, ""
	c.uniqueName, c.uniqueNameKnown, c.nullsNotDistinct, c.identity, c.sqliteIntegerPK = nil, false, nil, "", false
	c.autoIncrement, c.onUpdateNow = false, false
	c.def, c.defIsSQL, c.defSQL, c.defUnknown = nil, false, "", ""
	c.ms = modState{}
}

// autoIncrementArg reads sqlite's primaryKey({ autoIncrement }) config.
func autoIncrementArg(e typescript.ModelExpr) bool {
	cfg, ok := configValues(&e)
	if !ok {
		return false
	}
	for k, v := range cfg {
		if k != "autoIncrement" || v.Kind != typescript.ModelBoolean {
			return false
		}
	}
	return true
}

// readDefaultArg resolves a .default() argument: an identifier names a
// module-level const holding the value (same file or imported); an sql
// template is rendered when it has no substitution (drizzle-kit throws on
// one, sqlToStr bin.cjs:15930-15942).
func (c *column) readDefaultArg() {
	g := c.t.g
	e := *c.def
	file := c.file
	if e.Kind == typescript.ModelIdentifier {
		r := g.in.lookup(file, e.Name)
		if r.Decl == nil || r.Decl.Keyword != "const" {
			c.defUnknown = fmt.Sprintf("the default %s is not a module-level const the payload holds", e.Name)
			return
		}
		e, file = r.Decl.Value, r.File
		c.def, c.defFile = &e, file
	}
	if e.Kind == typescript.ModelTagged {
		if !g.in.isSQLTag(file, e.Tag) {
			c.defUnknown = "a tagged template other than drizzle-orm's sql"
			return
		}
		if len(e.Substitutions) > 0 {
			c.defUnknown = "an sql default with a substitution (drizzle-kit refuses it, bin.cjs:15930-15942)"
			return
		}
		c.defIsSQL, c.defSQL, c.def = true, e.Strings[0], nil
	}
}

// constraintName is the name of the column's unique constraint (sqlite:
// unique index): the literal name, else `<table>_<builder name>_unique`
// (pg-core/columns/common.js:61-62, unique-constraint.js:6-8:
// the builder name is the pre-casing key), truncated to 63 bytes on
// postgresql. A builder in a spread object is shared by every table that
// spreads it and keeps the name the first table built gives it, so its
// generated name is not known.
func (c *column) constraintName() (string, string) {
	switch {
	case !c.uniqueNameKnown:
		return "", "the unique constraint name is not a string literal"
	case c.uniqueName != nil:
		return *c.uniqueName, ""
	case c.shared:
		return "", "the builder is in a spread object shared by several tables; drizzle-orm stores the first table's generated unique name on it"
	}
	if c.t.g.c.Casing != "" {
		// Under casing the generated name keeps the pre-casing builder
		// name.
		c.tag("DR-K3")
	}
	return c.t.rec.Name + "_" + c.builderName + "_unique", ""
}

// finish sets the column facts once the table is read.
func (c *column) finish() {
	if c.rec.Unknown {
		return
	}
	t := c.t
	d := t.g.dialect
	open := t.open
	why := func() string {
		if c.unread != "" {
			return "the unread modifier ." + c.unread + "() may change it"
		}
		return "an element of the table's extra config is not read"
	}
	row := func() string {
		if c.unread != "" {
			return "D"
		}
		return "DR-X1"
	}
	mysqlSerial := d == "mysql" && c.serial
	// Nullability.
	switch {
	case c.notNull && c.unread == "":
		c.claim("nullable", false, c.ms.notNullRow)
	case c.unread != "" || open && d != "sqlite":
		c.reason(row(), models.Fact("nullable"), "nullability: %s (a composite primary key makes its columns not null)", why())
	case mysqlSerial:
		c.reason("DR-C1", models.Fact("nullable"), "MySQL's serial type is NOT NULL by itself; not read")
	default:
		c.claim("nullable", true, "DR-C2")
	}
	// Primary key.
	switch {
	case c.unread != "" || open:
		c.reason(row(), models.Fact("primary_key"), "primary key: %s", why())
	case t.multiPK:
		c.reason("D", models.Fact("primary_key"), "several columns call .primaryKey()")
	case c.primaryKey:
		c.claim("primary_key", true, c.pkRowOr("DR-C2"))
	default:
		c.claim("primary_key", false, "DR-C2")
	}
	// Uniqueness and the constraint (unique index on sqlite).
	if c.unique {
		name, nameWhy := c.constraintName()
		var nameValue any
		if nameWhy == "" {
			if d == "postgresql" {
				name = truncateIdentifier(name)
			}
			nameValue = name
		} else {
			t.rec.ConstraintsComplete = false
			t.rec.Reasons = append(t.rec.Reasons, models.Reason{Row: "DR-C2", Fact: models.Fact("constraint_set"), File: c.file, Line: c.line, Reason: fmt.Sprintf("%s.unique(): %s", c.key, nameWhy)})
		}
		switch d {
		case "sqlite":
			t.rec.Indexes = append(t.rec.Indexes, map[string]any{"name": nameValue, "columns": []string{c.rec.Name}, "unique": true})
		case "postgresql":
			t.rec.Uniques = append(t.rec.Uniques, map[string]any{"name": nameValue, "columns": []string{c.rec.Name}, "nulls_not_distinct": c.nullsNotDistinct})
		default:
			t.rec.Uniques = append(t.rec.Uniques, map[string]any{"name": nameValue, "columns": []string{c.rec.Name}})
		}
	}
	switch {
	case c.unique && c.unread == "":
		c.claim("unique", true, "DR-C2")
	case c.uniqueExtra && c.unread == "":
		c.claim("unique", true, "DR-X1")
	case c.unread != "" || open:
		c.reason(row(), models.Fact("unique"), "uniqueness: %s", why())
	case c.uniquePartial:
		c.reason("DR-X1", models.Fact("unique"), "uniqueness: only a partial unique index covers the column alone")
	case mysqlSerial:
		c.reason("DR-C1", models.Fact("unique"), "MySQL's serial type is UNIQUE by itself; not read")
	default:
		c.claim("unique", false, "DR-C2")
	}
	// Index: only the extra config declares indexes; index is true when a
	// non-unique index has exactly this column (partial or not).
	switch {
	case c.indexed && c.unread == "":
		c.claim("index", true, "DR-X1")
	case c.unread != "" || open:
		c.reason(row(), models.Fact("index"), "index: %s", why())
	case t.extra > 0:
		c.claim("index", false, "DR-X1")
	default:
		c.claim("index", false, "DR-T1")
	}
	// Foreign keys: the group adds them (foreignKeys) once every table is
	// read; one it cannot read makes the list unknown there.
	fkRow := "DR-C2"
	if c.extraFK {
		fkRow = "DR-X1"
	}
	switch {
	case c.refUnknown != "":
		c.rec.ForeignKeysUnknown = true
		t.rec.ConstraintsComplete = false
		c.reason("DR-C2", models.Fact("foreign_keys"), ".references(): %s", c.refUnknown)
	case c.unread != "" || open:
		c.rec.ForeignKeysUnknown = true
		c.reason(row(), models.Fact("foreign_keys"), "foreign keys: %s", why())
	default:
		c.rec.Claims["foreign_keys"] = models.ClaimSource{Row: fkRow}
	}
	if c.identity != "" {
		c.rec.Attributes["identity"] = c.identity
	}
	if c.generated {
		c.rec.Attributes["generated"] = "stored"
		c.rec.Attributes["generated_expression"] = nil
		if c.unread == "" {
			c.rec.Attributes["generated_expression"] = c.generatedText()
		}
	}
	c.defaultFacts()
}

// defaultFacts sets default, server_default and server_default_text (row
// DR-C2; DR-C1 for the serial sequence default).
func (c *column) defaultFacts() {
	d := c.t.g.dialect
	unknown := func(row, format string, a ...any) {
		c.reason(row, models.Fact("default"), format, a...)
		c.reason(row, models.Fact("server_default"), format, a...)
	}
	has := c.def != nil || c.defIsSQL
	switch {
	case c.unread != "":
		unknown("D", "the unread modifier .%s() may set a default", c.unread)
	case c.generated:
		unknown("DR-C2", "a generated column: the database computes its value; no default is claimed")
	case c.identity != "":
		unknown("DR-C2", "an identity column: the database generates its values; no default is claimed")
	case c.sqliteIntegerPK:
		unknown("DR-C2", "an integer primary key on sqlite is the rowid alias, which SQLite assigns; no default is claimed")
	case c.autoIncrement:
		unknown("DR-C2", "AUTO_INCREMENT: MySQL generates the value; it is not a default, and none is claimed")
	case c.onUpdateNow && !has:
		unknown("DR-C2", ".onUpdateNow() without a default: drizzle-orm treats the column as having a default (date.common.js:10-14), and whether MySQL gives it one depends on the server; default unknown")
	case c.serial && has, c.serial && d == "mysql":
		unknown("DR-C1", "a serial column's default is not read with this form")
	case c.serial:
		c.claim("default", "server", "DR-C1")
		c.claim("server_default", true, "DR-C1")
	case c.defUnknown != "":
		unknown("DR-C2", "%s", c.defUnknown)
	case !has && c.client:
		c.reason("DR-C2", models.Fact("default"), "$defaultFn/$onUpdate: value set by Drizzle at runtime; database default unknown")
		c.claim("server_default", false, "DR-C2")
	case !has, d == "postgresql" && c.def != nil && c.def.Kind == typescript.ModelNull && !c.client:
		// .default(null) renders DEFAULT null, which Postgres does not
		// store as a default (no pg_attrdef row; G2B3 oracle check).
		c.claim("default", "absent", "DR-C2")
		c.claim("server_default", false, "DR-C2")
	case d == "postgresql" && c.def != nil && c.def.Kind == typescript.ModelNull:
		c.reason("DR-C2", models.Fact("default"), "$defaultFn/$onUpdate with .default(null): value set by Drizzle at runtime; database default unknown")
		c.claim("server_default", false, "DR-C2")
	default:
		text, ok, why := c.defaultText()
		if why != "" {
			unknown("DR-C2", "%s", why)
			return
		}
		c.claim("default", "server", "DR-C2")
		c.claim("server_default", true, "DR-C2")
		if ok {
			c.rec.Attributes["server_default_text"] = text
		} else {
			c.reason("DR-C2", models.Fact("server_default_text"), "the default is present; its text as drizzle-kit renders it is not computed for this value")
		}
	}
}

// defaultText renders a literal default as drizzle-kit writes it into the
// SQL (postgresql bin.cjs:17021-17045, sqlite :18611-18617; MySQL:
// presence only). why is set when even the default's presence is not
// known (null, a value the database rejects for the column's type).
func (c *column) defaultText() (string, bool, string) {
	d := c.t.g.dialect
	if c.defIsSQL {
		return c.defSQL, d != "mysql", ""
	}
	e := *c.def
	base := strings.TrimSuffix(c.sqlType, "[]")
	array := strings.HasSuffix(c.sqlType, "[]")
	switch e.Kind {
	case typescript.ModelNull:
		return "", false, "a null default: drizzle-kit writes DEFAULT null; what the database keeps is checked on postgresql only"
	case typescript.ModelString, typescript.ModelTemplate:
		s, ok := literalString(e)
		if !ok {
			return "", false, "a template default with a substitution"
		}
		return sqlQuote(s), d != "mysql", ""
	case typescript.ModelNumber, typescript.ModelBoolean:
		switch {
		case d == "postgresql" && (base == "json" || base == "jsonb") && !array:
			j, ok := jsonStringify(e)
			return "'" + j + "'::" + base, ok, ""
		case d == "postgresql" && array:
			return "", false, "a scalar default on an array column"
		case e.Kind == typescript.ModelBoolean:
			return e.Value, d != "mysql", ""
		}
		n, ok := jsNumber(e.Value)
		return n, ok && d != "mysql", ""
	case typescript.ModelObject, typescript.ModelArray:
		switch {
		case d == "postgresql" && (base == "json" || base == "jsonb") && !array:
			j, ok := jsonStringify(e)
			return "'" + j + "'::" + base, ok && !strings.Contains(j, "'"), ""
		case d == "postgresql" && array && e.Kind == typescript.ModelArray:
			s, ok := pgArrayString(e, 0)
			return "'" + s + "'", ok, ""
		case d == "sqlite":
			j, ok := jsonStringify(e)
			return "'" + j + "'", ok && !strings.Contains(j, "'"), ""
		case d == "mysql" && base == "json":
			return "", false, ""
		}
		return "", false, "an object or array default on a column that is neither json nor an array"
	}
	return "", false, "a default that is not a literal (" + e.Kind + "); not read"
}

// generatedText renders a generated column's expression: an
// sql template, directly or returned by an arrow; a substitution renders
// only as a column of the same table, `"<table>"."<column>"`, and only in
// the arrow form (the table const is not initialised while the builder
// runs). nil when not rendered.
func (c *column) generatedText() any {
	t := c.t
	e := *c.ms.genExpr
	arrow := false
	if e.Kind == typescript.ModelArrow && len(e.Params) == 0 && e.Body != nil {
		e, arrow = *e.Body, true
	}
	if e.Kind != typescript.ModelTagged || !t.g.in.isSQLTag(c.file, e.Tag) {
		return nil
	}
	var b strings.Builder
	for i, s := range e.Strings {
		b.WriteString(s)
		if i >= len(e.Substitutions) {
			continue
		}
		sub := e.Substitutions[i]
		if !arrow || sub.Kind != typescript.ModelMember || sub.Object.Kind != typescript.ModelIdentifier || sub.Object.Name != t.decl.Name || c.file != t.file {
			return nil
		}
		var target *column
		for _, other := range t.columns {
			if other.key == sub.Name && !other.rec.Unknown {
				target = other
			}
		}
		if target == nil || strings.Contains(t.rec.Name+target.rec.Name, `"`) || t.rec.Schema == nil || *t.rec.Schema != "public" {
			return nil
		}
		b.WriteString(`"` + t.rec.Name + `"."` + target.rec.Name + `"`)
	}
	return b.String()
}
