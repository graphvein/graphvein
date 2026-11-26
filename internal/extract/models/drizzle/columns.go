package drizzle

import (
	"fmt"
	"strings"

	"github.com/graphvein/graphvein/internal/extract/models"
	"github.com/graphvein/graphvein/internal/lang/typescript"
)

// column is one column being read (rows DR-C1, DR-C2, DR-C3, DR-E1,
// DR-K2, DR-K3).
type column struct {
	t    *table
	key  string
	file string
	line int
	rec  *models.Column
	// builderName is the name drizzle-orm's builder holds: the literal
	// name, else the property key (before casing); unique constraint
	// names are made from it.
	builderName string
	shared      bool
	// sqlType is drizzle's getSQLType() text (lowercased), "" unknown;
	// kind is "builtin", "enum" or "custom".
	sqlType string
	kind    string
	serial  bool
	// Modifier state, as the builder's config holds it.
	notNull, primaryKey, unique bool
	uniqueName                  *string
	uniqueNameKnown             bool
	nullsNotDistinct            any
	identity                    string
	generated                   bool
	sqliteIntegerPK             bool
	// MySQL: .autoincrement() (AUTO_INCREMENT) and .onUpdateNow() (ON
	// UPDATE CURRENT_TIMESTAMP) on a builder that has them.
	autoIncrement, onUpdateNow bool
	// def is the .default() argument (nil: none); defFile its file;
	// defUnknown when a default modifier is not read.
	def        *typescript.ModelExpr
	defFile    string
	defSQL     string
	defIsSQL   bool
	defUnknown string
	client     bool
	// unread names the first modifier no row reads: the absences it could
	// change are unknown.
	unread string
	// failure is a DR-P1 failure of the column.
	failure string
	ms      modState
	// refs are the column's .references() keys; refUnknown says why one
	// is not read (the column's foreign keys are then unknown).
	refs       []*fkSpec
	refUnknown string
	// Facts the extra config (row DR-X1) sets: a one-column index, a
	// one-column unique constraint or unique index (partial or not), a
	// foreignKey() element using the column, the row that made it a
	// primary key column.
	indexed, uniqueExtra, uniquePartial, extraFK bool
	pkRow                                        string
}

func (c *column) reason(row string, fact *string, format string, a ...any) {
	c.rec.Reasons = append(c.rec.Reasons, models.Reason{Row: row, Fact: fact, File: c.file, Line: c.line, Reason: fmt.Sprintf(format, a...)})
}

func (c *column) claim(fact string, value any, row string) {
	c.rec.Attributes[fact] = value
	c.rec.Claims[fact] = models.ClaimSource{Row: row}
}

func (c *column) tag(row string) { c.t.g.tag(row) }

// unknownColumn makes every fact of the column unknown (row D or the row
// named): the value is not a column builder the contract reads.
func (c *column) unknownColumn(row, format string, a ...any) {
	c.tag(row)
	c.rec.Unknown = true
	c.t.rec.ColumnsComplete = false
	c.t.rec.ConstraintsComplete = false
	c.reason(row, nil, format, a...)
}

// builtin column builders the contract reads per dialect (row DR-C1, the
// The investigation oracle run list; MySQL from the mapped source, DR-V6).
var builtins = map[string]map[string]bool{
	"postgresql": set("text", "varchar", "char", "integer", "smallint", "bigint", "serial", "smallserial", "bigserial", "boolean", "real", "doublePrecision", "numeric", "timestamp", "date", "time", "interval", "uuid", "inet", "json", "jsonb"),
	"sqlite":     set("text", "integer", "real", "blob", "numeric"),
	"mysql":      set("int", "smallint", "tinyint", "mediumint", "bigint", "boolean", "text", "tinytext", "mediumtext", "longtext", "varchar", "char", "json", "date", "datetime", "timestamp", "time", "year", "serial"),
}

func set(names ...string) map[string]bool {
	m := map[string]bool{}
	for _, n := range names {
		m[n] = true
	}
	return m
}

// readColumn reads one property of the columns object.
func (t *table) readColumn(e entry) {
	c := &column{t: t, key: e.key, file: e.file, line: e.line, shared: e.shared, rec: &models.Column{
		Name: e.key, IdentityName: e.key, Attribute: e.key, File: e.file, Line: e.line,
		Attributes: map[string]any{}, Claims: map[string]models.ClaimSource{},
	}}
	for _, k := range []string{"type", "length", "precision", "scale", "timezone", "nullable", "primary_key", "unique", "index", "default", "server_default", "server_default_text"} {
		c.rec.Attributes[k] = nil
	}
	t.columns = append(t.columns, c)
	t.rec.Columns = append(t.rec.Columns, c.rec)
	g := t.g
	base, links, ok := chain(e.value)
	if !ok {
		c.unknownColumn("D", "the value of %s is not a column builder call", e.key)
		return
	}
	// The builder: a dialect module export, an enum, a customType.
	r := g.in.calleeRef(e.file, base.Callee)
	var nameArg *typescript.ModelExpr
	var config *typescript.ModelExpr
	configOK := true
	nameAndConfig := func() {
		// getColumnNameAndConfig (drizzle-orm/utils.js:117-122): a
		// non-empty string is the name; an object first argument is the
		// config.
		switch {
		case len(base.Args) == 0:
		case base.Args[0].Kind == typescript.ModelObject:
			config = &base.Args[0]
			if len(base.Args) > 1 {
				configOK = false
			}
		default:
			nameArg = &base.Args[0]
			if len(base.Args) > 1 {
				config = &base.Args[1]
			}
			if len(base.Args) > 2 {
				configOK = false
			}
		}
	}
	var enumInfo map[string]any
	switch d, core := coreDialect[r.Module]; {
	case core && d != g.dialect:
		c.unknownColumn("D", "%s is a %s builder in a %s table (DR-V4)", r.Export, d, g.dialect)
		return
	case core && r.Export == "mysqlEnum" && g.dialect == "mysql":
		c.kind = "enum"
		if len(base.Args) >= 1 && base.Args[0].Kind != typescript.ModelArray {
			nameArg = &base.Args[0]
		}
		values := base.Args[len(base.Args)-1:]
		c.sqlType = mysqlEnumType(values)
	case core && builtins[g.dialect][r.Export]:
		c.kind = "builtin"
		nameAndConfig()
	case core:
		// Another builder of the dialect module: the name convention is
		// drizzle-orm's, the type is not read.
		c.kind = "other"
		nameAndConfig()
	case r.Decl != nil:
		kind, info, why := g.columnFunction(r)
		switch kind {
		case "enum":
			c.kind = "enum"
			enumInfo = info
			if len(base.Args) > 0 {
				nameArg = &base.Args[0]
			}
			if len(base.Args) > 1 {
				configOK = false
			}
			if why != "" {
				c.failure = why
			}
		case "custom":
			c.kind = "custom"
			nameAndConfig()
			if why == "" {
				c.sqlType = info["type"].(string)
			} else {
				c.reason("DR-C3", models.Fact("type"), "%s", why)
			}
		default:
			c.unknownColumn("D", "%s does not call a column builder the contract reads", e.key)
			return
		}
	default:
		c.unknownColumn("D", "%s does not call a column builder the contract reads", e.key)
		return
	}
	// The name (DR-C1, DR-K2, DR-K3).
	cfg := g.c
	switch {
	case nameArg != nil && nameArg.Kind == typescript.ModelString && nameArg.Value == "":
		nameArg = nil
		fallthrough
	case nameArg == nil:
		if !cfg.CasingKnown {
			c.unknownColumn("DR-K2", "the column name is omitted and the config's casing is not literal, so the name is not known")
			return
		}
		c.builderName = e.key
		c.rec.Name = e.key
		if cfg.Casing != "" {
			c.rec.Name = convertCasing(cfg.Casing, e.key)
			t.casingDerived = true
		}
		c.claim("name", c.rec.Name, "DR-K2")
		c.tag("DR-K2")
	default:
		name, ok := literalString(*nameArg)
		if !ok {
			c.unknownColumn("DR-C1", "the column name is not a string literal")
			return
		}
		c.builderName = name
		c.rec.Name = name
		row := "DR-C1"
		if cfg.Casing != "" {
			row = "DR-K3"
		}
		c.claim("name", name, row)
		c.tag(row)
	}
	// The type (DR-C1, DR-C3, DR-E1).
	switch c.kind {
	case "builtin":
		if !configOK {
			c.reason("DR-C1", models.Fact("type"), "the builder has more arguments than a name and a config")
		} else {
			c.builtinType(r.Export, config)
		}
	case "other":
		c.tag("DR-C1")
		c.reason("DR-C1", models.Fact("type"), "%s is not in the contract's column builder list, so the type is not known", r.Export)
	case "enum":
		if g.dialect == "postgresql" && enumInfo != nil && configOK {
			c.tag("DR-E1")
			c.rec.Attributes["enum"] = enumInfo
			c.sqlType = enumInfo["sql_type"].(string)
			delete(enumInfo, "sql_type")
		} else if g.dialect == "mysql" && c.sqlType != "" {
			c.tag("DR-E1")
		} else if c.failure == "" {
			c.reason("DR-E1", models.Fact("type"), "the enum's values are not literal")
		}
	case "custom":
		if c.sqlType != "" {
			c.tag("DR-C3")
		}
	}
	c.modifiers(links)
	if c.sqlType != "" {
		row := map[string]string{"builtin": "DR-C1", "enum": "DR-E1", "custom": "DR-C3"}[c.kind]
		c.claim("type", catalogType(g.dialect, c.sqlType), row)
	}
}

// columnFunction classifies a module-level declaration used as a column
// builder: an enum (pgEnum or <s>.enum, row DR-E1) or a customType (row
// DR-C3). why is a DR-P1 failure for an enum, the unknown-type reason for
// a custom type.
func (g *group) columnFunction(r ref) (kind string, info map[string]any, why string) {
	base, links, ok := chain(r.Decl.Value)
	if !ok || len(links) > 0 {
		return "", nil, ""
	}
	callee := base.Callee
	var schema *visibleDecl
	cr := g.in.calleeRef(r.File, callee)
	switch {
	case g.dialect != "postgresql" && cr.Export != "customType":
		return "", nil, ""
	case cr.Module == "drizzle-orm/pg-core" && cr.Export == "pgEnum":
	case callee.Kind == typescript.ModelMember && callee.Name == "enum" && callee.Object.Kind == typescript.ModelIdentifier:
		sr := g.in.lookup(r.File, callee.Object.Name)
		if sr.Decl == nil {
			return "", nil, ""
		}
		sb, _, ok := chain(sr.Decl.Value)
		if sbr := g.in.calleeRef(sr.File, sb.Callee); !ok || sbr.Module != "drizzle-orm/pg-core" || sbr.Export != "pgSchema" {
			return "", nil, ""
		}
		schema = &visibleDecl{sr.File, sr.Decl}
	case coreDialect[cr.Module] == g.dialect && cr.Export == "customType":
		sql, why := g.customTypeSQL(r)
		return "custom", map[string]any{"type": sql}, why
	default:
		return "", nil, ""
	}
	if len(base.Args) != 2 {
		return "", nil, ""
	}
	name, ok := literalString(base.Args[0])
	if !ok {
		return "", nil, ""
	}
	schemaName := "public"
	if schema != nil {
		s, why := g.pgSchemaName(*schema)
		if why != "" {
			if strings.HasPrefix(why, "DR-P1 ") {
				return "enum", nil, strings.TrimPrefix(why, "DR-P1 ")
			}
			return "", nil, ""
		}
		schemaName = s
	}
	if !g.visible[r.Decl] {
		return "enum", nil, fmt.Sprintf("enum %s (%s) is not exported from the schema modules, so drizzle-kit creates no type %q and the table fails", r.Decl.Name, r.File, name)
	}
	if ok, why := g.schemaCertain(schemaName); !ok {
		return "enum", nil, "the enum's " + why
	}
	var values any
	if base.Args[1].Kind == typescript.ModelArray {
		list := []string{}
		for _, el := range base.Args[1].Elements {
			s, ok := literalString(el)
			if !ok {
				list = nil
				break
			}
			list = append(list, s)
		}
		if list != nil {
			values = list
		}
	}
	// format_type quotes a name that needs it (oracle, G2B5: dokploy's
	// "applicationStatus").
	sqlType := pgQuoteIdent(name)
	if schema != nil {
		sqlType = pgQuoteIdent(schemaName) + "." + sqlType
	}
	return "enum", map[string]any{"name": name, "schema": schemaName, "values": values, "sql_type": sqlType}, ""
}

// customTypeSQL reads a customType's literal dataType() (row DR-C3).
func (g *group) customTypeSQL(r ref) (string, string) {
	base, _, _ := chain(r.Decl.Value)
	if len(base.Args) != 1 || base.Args[0].Kind != typescript.ModelObject {
		return "", "customType without one object literal argument: type unknown"
	}
	sql := ""
	for _, en := range base.Args[0].Entries {
		switch {
		case en.Key == "dataType" && en.Kind == typescript.ModelMethod && en.Value.Kind == typescript.ModelString:
			sql = en.Value.Value
		case en.Key == "dataType":
			return "", "customType dataType() does not return one string literal: type unknown"
		case (en.Key == "toDriver" || en.Key == "fromDriver") && (en.Kind == typescript.ModelMethod || en.Kind == typescript.ModelProperty):
			// Client-side conversions: no SQL effect.
		default:
			return "", fmt.Sprintf("customType entry %q is not read: type unknown", en.Key)
		}
	}
	if sql == "" {
		return "", "customType has no literal dataType(): type unknown"
	}
	switch {
	case g.dialect == "postgresql" && !customTypesCompared[sql]:
		return "", fmt.Sprintf("customType dataType() %q is not a type the oracle compared (%s): type unknown", sql, strings.Join(sortedKeys(customTypesCompared), ", "))
	case g.dialect == "mysql":
		// MySQL types are claimed in the catalog form it reports;
		// a dataType() literal is free text MySQL may spell its own
		// way (integer reads back as int), and none is mapped.
		return "", fmt.Sprintf("customType dataType() %q on mysql: MySQL may report the type in another form, and no customType literal is mapped: type unknown", sql)
	}
	return sql, ""
}

// customTypesCompared are the postgresql dataType() literals the oracle
// compared (G2B5: kaneo bytea, dokploy and the fixture text): Postgres
// spells another literal its own way (varchar(255) is character
// varying(255), int4 is integer), so only these are claimed as written
// (row DR-C3). SQLite keeps a declared type as written.
var customTypesCompared = map[string]bool{"bytea": true, "text": true}

// pgQuoteIdent quotes a Postgres identifier as quote_ident and format_type
// do: unquoted only when it is lower-case letters, digits and underscores,
// starts with a letter or underscore, and is not a keyword of a category
// other than unreserved.
func pgQuoteIdent(name string) string {
	safe := name != "" && !pgQuotedKeywords[name] && (name[0] == '_' || name[0] >= 'a' && name[0] <= 'z')
	for i := 0; safe && i < len(name); i++ {
		c := name[i]
		safe = c == '_' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9'
	}
	if safe {
		return name
	}
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// pgQuotedKeywords are Postgres 17's keywords that quote_ident quotes:
// every category but unreserved (SELECT word FROM pg_get_keywords() WHERE
// catcode <> 'U' on the oracle's Postgres 17.11).
var pgQuotedKeywords = map[string]bool{}

func init() {
	for _, w := range strings.Fields(pgQuotedKeywordList) {
		pgQuotedKeywords[w] = true
	}
}

const pgQuotedKeywordList = `
all analyse analyze and any array as asc asymmetric authorization between bigint binary bit boolean
both case cast char character check coalesce collate collation column concurrently constraint create
cross current_catalog current_date current_role current_schema current_time current_timestamp
current_user dec decimal default deferrable desc distinct do else end except exists extract false
fetch float for foreign freeze from full grant greatest group grouping having ilike in initially
inner inout int integer intersect interval into is isnull join json json_array json_arrayagg
json_exists json_object json_objectagg json_query json_scalar json_serialize json_table json_value
lateral leading least left like limit localtime localtimestamp merge_action national natural nchar
none normalize not notnull null nullif numeric offset on only or order out outer overlaps overlay
placing position precision primary real references returning right row select session_user setof
similar smallint some substring symmetric system_user table tablesample then time timestamp to
trailing treat trim true union unique user using values varchar variadic verbose when where window
with xmlattributes xmlconcat xmlelement xmlexists xmlforest xmlnamespaces xmlparse xmlpi xmlroot
xmlserialize xmltable`

// mysqlEnumType is mysqlEnum's getSQLType (mysql-core/columns/enum.js:20)
// for literal values without a quote; "" otherwise.
func mysqlEnumType(args []typescript.ModelExpr) string {
	if len(args) != 1 || args[0].Kind != typescript.ModelArray || len(args[0].Elements) == 0 {
		return ""
	}
	var parts []string
	for _, el := range args[0].Elements {
		s, ok := literalString(el)
		if !ok || strings.ContainsAny(s, `'\`) {
			return ""
		}
		parts = append(parts, "'"+s+"'")
	}
	return "enum(" + strings.Join(parts, ",") + ")"
}

// configValues reads a builder's config object: literal values by key;
// ok false when an entry is not a plain property (a spread, a method).
func configValues(config *typescript.ModelExpr) (map[string]typescript.ModelExpr, bool) {
	out := map[string]typescript.ModelExpr{}
	if config == nil {
		return out, true
	}
	if config.Kind != typescript.ModelObject {
		return nil, false
	}
	for _, en := range config.Entries {
		if en.Kind != typescript.ModelProperty {
			return nil, false
		}
		out[en.Key] = en.Value
	}
	return out, true
}

// builtinType sets the drizzle SQL type of a builtin builder from its
// config (getSQLType of drizzle-orm/<dialect>-core/columns/*.js); a config
// value the type depends on that is not literal leaves it unknown.
func (c *column) builtinType(builder string, config *typescript.ModelExpr) {
	cfg, ok := configValues(config)
	unknown := func(why string) { c.reason("DR-C1", models.Fact("type"), "%s: %s", builder, why) }
	if !ok {
		unknown("the config argument is not an object literal of properties")
		return
	}
	c.tag("DR-C1")
	num := func(key string, min, max int) (int, bool, bool) {
		v, present := cfg[key]
		if !present || v.Kind == typescript.ModelIdentifier && v.Name == "undefined" {
			return 0, false, true
		}
		n, ok := intLiteral(v)
		return n, true, ok && n >= min && n <= max
	}
	boolean := func(key string) (bool, bool) {
		v, present := cfg[key]
		if !present {
			return false, true
		}
		return v.Value == "true", v.Kind == typescript.ModelBoolean
	}
	str := func(key string) (string, bool, bool) {
		v, present := cfg[key]
		if !present {
			return "", false, true
		}
		s, ok := literalString(v)
		return s, true, ok
	}
	switch c.t.g.dialect {
	case "postgresql":
		switch builder {
		case "text", "integer", "smallint", "boolean", "real", "doublePrecision", "date", "uuid", "inet", "json", "jsonb":
			c.sqlType = map[string]string{"doublePrecision": "double precision"}[builder]
			if c.sqlType == "" {
				c.sqlType = strings.ToLower(builder)
			}
		case "serial", "smallserial":
			c.sqlType, c.serial = builder, true
		case "bigint", "bigserial":
			// bigint(a, b) reads config.mode, so it needs a config object
			// (drizzle-orm pg-core/columns/bigint.js:52).
			if config == nil {
				unknown("without a config object drizzle-orm throws while loading the schema")
				return
			}
			c.sqlType, c.serial = builder, builder == "bigserial"
		case "varchar", "char":
			n, present, ok := num("length", 1, 10485760)
			if !ok {
				unknown("length is not a literal integer from 1 to 10485760")
				return
			}
			c.sqlType = builder
			if present {
				c.sqlType = fmt.Sprintf("%s(%d)", builder, n)
			}
		case "numeric":
			p, hasP, okP := num("precision", 1, 1000)
			s, hasS, okS := num("scale", 0, 1000)
			switch {
			case !okP || !okS || hasP && hasS && s > p:
				unknown("precision or scale is not a literal the type allows")
				return
			case hasP && hasS:
				c.sqlType = fmt.Sprintf("numeric(%d, %d)", p, s)
			case hasP:
				c.sqlType = fmt.Sprintf("numeric(%d)", p)
			default:
				c.sqlType = "numeric"
			}
		case "timestamp", "time":
			p, hasP, okP := num("precision", 0, 6)
			tz, okTZ := boolean("withTimezone")
			if !okP || !okTZ {
				unknown("precision or withTimezone is not literal")
				return
			}
			prec := ""
			if hasP {
				prec = fmt.Sprintf("(%d)", p)
				if builder == "timestamp" {
					prec = " " + prec
				}
			}
			c.sqlType = builder + prec
			if tz {
				c.sqlType += " with time zone"
			}
		case "interval":
			if len(cfg) > 0 {
				unknown("fields and precision are not read")
				return
			}
			c.sqlType = "interval"
		}
	case "sqlite":
		switch builder {
		case "integer", "real", "blob", "numeric":
			c.sqlType = builder
		case "text":
			mode, _, okMode := str("mode")
			n, hasLen, okLen := num("length", 1, 1000000000)
			if !okMode || !okLen {
				unknown("mode or length is not literal")
				return
			}
			c.sqlType = "text"
			if hasLen && mode != "json" {
				c.sqlType = fmt.Sprintf("text(%d)", n)
			}
		}
	case "mysql":
		switch builder {
		case "int", "smallint", "tinyint", "mediumint", "bigint":
			if builder == "bigint" && config == nil {
				unknown("without a config object drizzle-orm throws while loading the schema")
				return
			}
			u, okU := boolean("unsigned")
			if !okU {
				unknown("unsigned is not literal")
				return
			}
			c.sqlType = builder
			if u {
				c.sqlType += " unsigned"
			}
		case "boolean", "text", "tinytext", "mediumtext", "longtext", "json", "date", "year", "serial":
			c.sqlType = builder
			c.serial = builder == "serial"
		case "varchar", "char":
			n, present, ok := num("length", 1, 65535)
			switch {
			case !ok:
				unknown("length is not a literal integer")
				return
			case !present && builder == "varchar":
				// varchar.js:24 renders `varchar`, which MySQL rejects
				// without a length.
				unknown("varchar without a length: drizzle-kit writes `varchar`, which MySQL rejects")
				return
			}
			c.sqlType = builder
			if present {
				c.sqlType = fmt.Sprintf("%s(%d)", builder, n)
			}
		case "datetime", "timestamp", "time":
			p, hasP, ok := num("fsp", 0, 6)
			if !ok {
				unknown("fsp is not a literal from 0 to 6")
				return
			}
			c.sqlType = builder
			if hasP {
				c.sqlType = fmt.Sprintf("%s(%d)", builder, p)
			}
		}
	}
}

// catalogType is the type as the database catalog shows it (row DR-C1):
// format_type on postgresql (varchar(20) -> character varying(20),
// timestamp -> timestamp without time zone, serial -> integer, arrays
// <type>[]); the declared type on sqlite; on mysql (mapped) the
// information_schema COLUMN_TYPE MySQL 8.0.19 and later reports for
// drizzle-kit's type (mysqlCatalogType).
func catalogType(dialect, sqlType string) string {
	switch dialect {
	case "mysql":
		return mysqlCatalogType(sqlType)
	case "postgresql":
	default:
		return sqlType
	}
	suffix := ""
	for strings.HasSuffix(sqlType, "[]") {
		sqlType = strings.TrimSuffix(sqlType, "[]")
		suffix = "[]"
	}
	t := sqlType
	switch {
	case t == "serial":
		t = "integer"
	case t == "smallserial":
		t = "smallint"
	case t == "bigserial":
		t = "bigint"
	case t == "varchar":
		t = "character varying"
	case strings.HasPrefix(t, "varchar("):
		t = "character varying" + strings.TrimPrefix(t, "varchar")
	case t == "char":
		t = "character(1)"
	case strings.HasPrefix(t, "char("):
		t = "character" + strings.TrimPrefix(t, "char")
	case strings.HasPrefix(t, "numeric("):
		inner := strings.TrimSuffix(strings.TrimPrefix(t, "numeric("), ")")
		p, s, two := strings.Cut(inner, ", ")
		if !two {
			s = "0"
		}
		t = "numeric(" + p + "," + s + ")"
	case strings.HasPrefix(t, "timestamp") || strings.HasPrefix(t, "time"):
		word := "time"
		if strings.HasPrefix(t, "timestamp") {
			word = "timestamp"
		}
		rest := strings.TrimSpace(strings.TrimPrefix(t, word))
		prec := ""
		if strings.HasPrefix(rest, "(") {
			end := strings.Index(rest, ")")
			prec = rest[:end+1]
			rest = strings.TrimSpace(rest[end+1:])
		}
		zone := " without time zone"
		if rest == "with time zone" {
			zone = " with time zone"
		}
		t = word + prec + zone
	}
	return t + suffix
}

// mysqlCatalogType is the COLUMN_TYPE MySQL reports for a type drizzle-kit
// writes (T018g2 review, DR-V6): SERIAL is an alias for BIGINT UNSIGNED
// NOT NULL AUTO_INCREMENT UNIQUE, so the column reads back as `bigint
// unsigned` (drizzle-kit's own MySQL introspection reads `bigint unsigned`
// with auto_increment and a unique index back as serial, bin.cjs:16431-16437);
// BOOLEAN is a synonym for TINYINT(1), the one integer type MySQL 8.0.19+
// still shows with a display width; CHAR without a length is CHAR(1).
// Every other type of the mysql run list (integers with `unsigned`, text
// types, varchar(n), char(n), date and time types with fsp, json, year,
// enum('a','b')) reads back as written.
func mysqlCatalogType(t string) string {
	switch t {
	case "serial":
		return "bigint unsigned"
	case "boolean":
		return "tinyint(1)"
	case "char":
		return "char(1)"
	}
	return t
}
