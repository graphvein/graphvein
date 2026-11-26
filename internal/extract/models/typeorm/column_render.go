package typeorm

import (
	"fmt"
	"strings"

	"github.com/graphvein/graphvein/internal/extract/models"
)

// render computes the column's facts on every group dialect and records
// the TO-P1 failures they prove.
func (col *column) render(t *table) {
	c := t.c
	col.r = map[string]rendered{}
	if col.unknownWhy != "" {
		return
	}
	source, why := col.typeSource()
	if source == "design" {
		design, object, dwhy := col.designType()
		switch {
		case object != "":
			// The failure is decided after the relation pass
			// (objectFailures): a column that merges with a join column has
			// its type replaced before validation.
			col.object = fmt.Sprintf("%s.%s (%s:%d): %s, which no driver supports (metadata-builder/EntityMetadataValidator.js:89)", t.e.Name, col.prop, col.file, col.line, object)
			why = object
			source = ""
		case design == "":
			why, source = dwhy, ""
		default:
			source = "design:" + design
		}
	}
	for _, d := range c.g.Dialects {
		r := rendered{}
		if source == "" {
			r.typWhy = why
		} else {
			col.typeOn(d, source, &r)
		}
		col.optionFailures(d, source)
		col.defaultOn(d, &r)
		col.r[d] = r
	}
}

// optionFailures records the TO-P1 failures of a column's length and
// precision options on dialect d (Revision 4 A5): (13) a length on a type
// outside the driver's withLengthColumnTypes fails the
// EntityMetadataValidator (:89-92) on every dialect whose list is read;
// (14) on postgres a precision (without a length) on a type that takes no
// type modifier fails at CREATE TABLE. source is typeSource's (with the
// design type), "" when the type is not read; generated, date and version
// columns are not checked (their type is the driver's).
func (col *column) optionFailures(d, source string) {
	c := col.t.c
	norm := ""
	switch {
	case col.explicit != "" && col.explicit != "enum" && col.explicit != "simple-enum" && col.deco != "PrimaryGeneratedColumn":
		if sup, ok := supportedType(d, col.explicit); !ok || !sup {
			return
		}
		norm = col.explicit
	case strings.HasPrefix(source, "design:"):
		norm = map[string]string{"string": "varchar", "number": "int", "boolean": "boolean", "Date": "timestamp"}[strings.TrimPrefix(source, "design:")]
	}
	if norm == "" {
		return
	}
	list := d
	if mysqlFamily(d) {
		list = "mysql"
	}
	where := fmt.Sprintf("%s.%s (%s:%d)", col.t.e.Name, col.prop, col.file, col.line)
	if col.length != nil && *col.length > 0 && lengthExplicit[list] != nil && !lengthExplicit[list][norm] {
		c.fail("(13)", fmt.Sprintf("%s: a length on the type %s, which %s's driver does not give a length (metadata-builder/EntityMetadataValidator.js:89-92)", where, norm, d), d)
	}
	if d == "postgres" && col.precision != nil && col.length == nil && pgNoModifier[pgTypes[norm]] {
		c.fail("(14)", fmt.Sprintf("%s: a precision on the type %s, which postgres gives no type modifier (PostgresDriver.js:850-864 writes %s(%d))", where, pgTypes[norm], pgTypes[norm], *col.precision), d)
	}
}

// typeSource says where the column's type comes from: "explicit", "enum",
// "date", "version", "generated" or "design", or "" with the reason the
// type is unknown.
func (col *column) typeSource() (string, string) {
	switch {
	case col.wrapType:
		return "", "the wrapper " + col.wrapper + " chooses the type by database (TO-C6)"
	case col.typeWhy != "":
		return "", col.typeWhy
	case col.generated:
		return "", "the generated option is not read"
	case col.array && col.isEnum:
		return "", "an enum array is not read"
	case col.deco == "PrimaryGeneratedColumn":
		return "generated", ""
	case col.explicit == "enum" || col.explicit == "simple-enum":
		return "enum", ""
	case col.isEnum:
		return "", "an enum option without type enum or simple-enum is not read"
	case col.explicit != "":
		return "explicit", ""
	case col.deco == "CreateDateColumn" || col.deco == "UpdateDateColumn" || col.deco == "DeleteDateColumn":
		return "date", ""
	case col.deco == "VersionColumn":
		return "version", ""
	}
	return "design", ""
}

// typeOn renders the type on dialect d (TO-C1, TO-C2, TO-C3, TO-C5).
func (col *column) typeOn(d, source string, r *rendered) {
	c := col.t.c
	if d == "mssql" {
		r.typWhy = "mssql types are not in the contract's mapped type list (TO-C2)"
		return
	}
	switch {
	case source == "generated":
		col.generatedType(d, r)
	case source == "enum":
		col.enumType(d, r)
	case source == "explicit":
		if sup, ok := supportedType(d, col.explicit); ok && !sup {
			c.fail("(2)", fmt.Sprintf("%s.%s (%s:%d): the type %q is not supported by %s (metadata-builder/EntityMetadataValidator.js:89)", col.t.e.Name, col.prop, col.file, col.line, col.explicit, d), d)
			r.typWhy = "the type " + col.explicit + " is not supported on " + d
			return
		}
		col.listType(d, col.explicit, r)
	case source == "date":
		if col.precision != nil || col.scale != nil || col.length != nil {
			r.typWhy = "a precision, scale or length on a date column is not read (TO-C3)"
			return
		}
		switch d {
		case "postgres":
			r.typ = "timestamp without time zone"
		case "sqlite":
			r.typ = "datetime"
		default:
			// MysqlDriver.js mappedDataTypes :176-185: datetime with
			// precision 6.
			r.typ, r.precision = "datetime(6)", intPtr(6)
		}
	case source == "version":
		if col.precision != nil || col.scale != nil || col.length != nil {
			r.typWhy = "a precision, scale or length on a version column is not read"
			return
		}
		r.typ = map[string]string{"postgres": "integer", "sqlite": "integer", "mysql": "int", "mariadb": "int"}[d]
	case strings.HasPrefix(source, "design:"):
		col.designOn(d, strings.TrimPrefix(source, "design:"), r)
	}
}

// listType renders an explicit type from the dialect's list (TO-C2).
func (col *column) listType(d, explicit string, r *rendered) {
	var base string
	switch d {
	case "postgres":
		base = pgTypes[explicit]
	case "sqlite":
		base = sqliteTypes[explicit]
	case "mysql", "mariadb":
		base = mysqlTypes[explicit]
		if d == "mariadb" && explicit == "json" {
			r.typWhy = "mariadb's json depends on the server version (MysqlDriver.js:597-606)"
			return
		}
	}
	if base == "" {
		r.typWhy = "the type " + explicit + " is not in the contract's " + d + " list (TO-C2)"
		return
	}
	if col.array {
		if d != "postgres" {
			r.typWhy = "array columns are read on postgres only"
			return
		}
	}
	col.modifiers(d, base, explicit == "varchar" || base == "varchar", r)
	if r.typ != "" && col.array {
		r.typ += "[]"
	}
}

// modifiers appends length or precision and scale to base (TO-C2:
// `(n)`, `(p,s)`), with the MySQL varchar default length 255
// (MysqlDriver.getColumnLength :684-700).
func (col *column) modifiers(d, base string, varchar bool, r *rendered) {
	typ := base
	switch {
	case col.length != nil:
		if !lengthTypes[d][base] || col.precision != nil || col.scale != nil {
			r.typWhy = "a length on " + base + " is not read"
			return
		}
		typ += "(" + intText(*col.length) + ")"
		r.length = col.length
	case col.precision != nil:
		if !precisionTypes[d][base] {
			r.typWhy = "a precision on " + base + " is not read"
			return
		}
		if col.scale != nil {
			typ += "(" + intText(*col.precision) + "," + intText(*col.scale) + ")"
			r.scale = col.scale
		} else if d == "sqlite" {
			typ += "(" + intText(*col.precision) + ")"
		} else {
			// NUMERIC(p) and DECIMAL(p) have scale 0, and the catalogs
			// say so (format_type: numeric(5,0); MySQL decimal(5,0)): the
			// TOB5 oracle.
			typ += "(" + intText(*col.precision) + ",0)"
			r.scale = intPtr(0)
		}
		r.precision = col.precision
	case col.scale != nil:
		r.typWhy = "a scale without a precision is not read"
		return
	case (d == "mysql" || d == "mariadb") && varchar:
		typ += "(255)"
		r.length = intPtr(255)
	case (d == "mysql" || d == "mariadb") && base == "decimal":
		r.typWhy = "a decimal without precision takes MySQL's own default (not mapped)"
		return
	case d == "postgres" && base == "character":
		r.typWhy = "a character type without a length was not compared by the oracle"
		return
	}
	r.typ = typ
}

// designOn renders a design-type column (TO-C2).
func (col *column) designOn(d, design string, r *rendered) {
	if col.precision != nil || col.scale != nil || (col.length != nil && design != "string") {
		r.typWhy = "a modifier on a " + design + " design type is not read"
		return
	}
	base := map[string]map[string]string{
		"string":  {"postgres": "character varying", "sqlite": "varchar", "mysql": "varchar", "mariadb": "varchar"},
		"number":  {"postgres": "integer", "sqlite": "integer", "mysql": "int", "mariadb": "int"},
		"boolean": {"postgres": "boolean", "sqlite": "boolean", "mysql": "tinyint", "mariadb": "tinyint"},
		"Date":    {"postgres": "timestamp without time zone", "sqlite": "datetime", "mysql": "datetime", "mariadb": "datetime"},
	}[design][d]
	col.modifiers(d, base, design == "string", r)
}

// generatedType renders a PrimaryGeneratedColumn (TO-C1): increment, uuid
// and (postgres) identity.
func (col *column) generatedType(d string, r *rendered) {
	t := col.t
	switch col.strategy {
	case "increment", "identity":
		if col.strategy == "identity" && d != "postgres" {
			r.typWhy, r.aiWhy = "the identity strategy is read on postgres only", "the identity strategy is read on postgres only"
			return
		}
		if col.explicit != "" {
			col.listType(d, col.explicit, r)
			if r.typ != "" && !integerTypes[strings.SplitN(r.typ, "(", 2)[0]] {
				r.typ, r.typWhy = "", "a generated key of type "+col.explicit+" is not read"
			}
		} else {
			r.typ = map[string]string{"postgres": "integer", "sqlite": "integer", "mysql": "int", "mariadb": "int"}[d]
		}
		if r.typ == "" {
			r.aiWhy = r.typWhy
			return
		}
		if d == "sqlite" && (t.primaries != 1 || r.typ != "integer") {
			r.aiWhy = "sqlite's AUTOINCREMENT is read only on the single integer primary key"
			return
		}
		r.ai = boolPtr(true)
	case "uuid":
		if col.explicit != "" || col.length != nil {
			r.typWhy = "a uuid key with an explicit type or length is not read"
			return
		}
		switch d {
		case "postgres":
			r.typ = "uuid"
		case "sqlite":
			r.typ = "varchar"
		case "mysql":
			// MysqlDriver normalizeType and getColumnLength (:594, :690).
			r.typ, r.length = "varchar(36)", intPtr(36)
		default:
			r.typWhy = "mariadb's uuid type depends on the server version (MysqlDriver.js:594)"
		}
	default:
		r.typWhy, r.aiWhy = "the generation strategy "+col.strategy+" is not read", "the generation strategy is not read"
	}
}

// enumType renders an enum column (TO-C5).
func (col *column) enumType(d string, r *rendered) {
	t := col.t
	c := t.c
	if col.enumWhy != "" || col.enum == nil {
		r.typWhy = col.enumWhy
		if r.typWhy == "" {
			r.typWhy = "the enum values are not given"
		}
		return
	}
	switch d {
	case "postgres":
		if col.enumName != "" {
			r.typ = col.enumName
			break
		}
		if c.g.EntityPrefix != "" || t.rec.Schema != nil {
			r.typWhy = "the enum type name with an entity prefix or a schema is not read"
			return
		}
		// PostgresQueryRunner.js:2597-2612 (TO-K2).
		r.typ = t.rec.Name + "_" + strings.ToLower(col.name) + "_enum"
	case "sqlite":
		if col.explicit == "enum" {
			c.fail("(6)", fmt.Sprintf("%s.%s (%s:%d): the enum type is not supported by sqlite (use simple-enum)", t.e.Name, col.prop, col.file, col.line), d)
			r.typWhy = "enum is not supported on sqlite"
			return
		}
		r.typ = "varchar"
	default:
		r.typ = "enum"
	}
}

// defaultOn renders the default on dialect d (TO-C1, TO-C3, TO-C4).
func (col *column) defaultOn(d string, r *rendered) {
	c := col.t.c
	known := func(present bool, text string, row string) {
		r.defKnown, r.present, r.defaultRow = true, present, row
		if present && text != "" {
			r.text = &text
		}
	}
	switch {
	case d == "mssql":
		r.defWhy = "mssql defaults are not mapped (TO-C4)"
		return
	case col.wrapDefault:
		r.defWhy = "the wrapper " + col.wrapper + " chooses the default (TO-C6)"
		return
	case col.generated:
		r.defWhy = "the generated option is not read"
		return
	case col.defWhy != "":
		r.defWhy = col.defWhy
		return
	case col.deco == "PrimaryGeneratedColumn":
		if col.hasDefault {
			r.defWhy = "a default on a generated key is not read"
			return
		}
		switch {
		case col.strategy == "increment" && r.ai != nil:
			if d == "postgres" {
				// SERIAL: a nextval default whose sequence postgres names.
				known(true, "", "TO-C1")
				r.defForm = "generated:increment"
				r.textWhy = "SERIAL: the nextval default names a sequence postgres chooses"
				return
			}
			known(false, "", "TO-C1")
			r.defForm = "generated:none"
		case col.strategy == "uuid" && r.typ != "":
			if d == "postgres" {
				known(true, "", "TO-C1")
				r.defForm = "generated:uuid"
				if len(c.g.Options) > 0 && c.g.Options[0].Value("uuidExtension") == nil {
					s := "uuid_generate_v4()"
					r.text = &s
				} else {
					r.textWhy = "the uuidExtension option chooses the generator (PostgresDriver.js:1139-1143)"
				}
				return
			}
			known(false, "", "TO-C1")
			r.defForm = "generated:none"
		case col.strategy == "identity" && r.ai != nil:
			r.defWhy = "GENERATED BY DEFAULT AS IDENTITY: the database generates the value; it is not a default"
		default:
			r.defWhy = "the generation is not read"
		}
		return
	}
	date := col.deco == "CreateDateColumn" || col.deco == "UpdateDateColumn"
	if date {
		falsy := col.hasDefault && ((col.def.kind == "number" && strings.Trim(col.def.text, "-0.") == "") || (col.def.kind == "string" && col.def.text == "") || (col.def.kind == "boolean" && col.def.text == "false"))
		switch {
		case falsy:
			r.defWhy = "a falsy default on a create or update date column: 0.3 replaces it with the driver's, 1.x keeps it (TO-C3, open point O19)"
			return
		case !col.hasDefault || col.def.kind == "null":
			// The driver's default (mappedDataTypes).
			text := map[string]string{"postgres": "now()", "sqlite": "datetime('now')", "mysql": "CURRENT_TIMESTAMP(6)", "mariadb": "CURRENT_TIMESTAMP(6)"}[d]
			known(true, text, "TO-C3")
			r.defForm = "date:driver"
			return
		}
	}
	if !col.hasDefault || col.def.kind == "null" {
		known(false, "", "TO-C4")
		r.defForm = "absent"
		return
	}
	if col.array {
		r.defWhy = "a default on an array column is not read"
		return
	}
	v := col.def
	fam := col.family(d, r)
	where := fmt.Sprintf("%s.%s (%s:%d)", col.t.e.Name, col.prop, col.file, col.line)
	switch v.kind {
	case "string":
		if strings.Contains(v.text, "'") {
			c.fail("(4)", fmt.Sprintf("%s: the string default %q contains a quote, which the driver does not escape", where, v.text), d)
			r.defWhy = "the string default contains a quote"
			return
		}
		col.stringDefault(d, fam, where, r)
	case "number":
		base := strings.SplitN(r.typ, "(", 2)[0]
		if d == "postgres" && fractional(v.text) && integerTypes[base] {
			c.fail("(5)", fmt.Sprintf("%s: the fractional default %s on an integer column", where, v.text), d)
			r.defWhy = "a fractional default on an integer column"
			return
		}
		if !plainNumber.MatchString(v.text) {
			r.defWhy = "the number literal " + v.text + " is not read as default text"
			return
		}
		col.numberDefault(d, fam, r)
	case "boolean":
		switch {
		case d == "postgres" && fam != famBool:
			// PostgresDriver.js:808-809 writes true/false, which a
			// non-boolean column does not take (Revision 4 A2).
			r.defWhy = "a boolean default on a column that is not boolean is not read on postgres"
		case d == "postgres":
			known(true, v.text, "TO-C4")
			r.defForm = "boolean:" + fam
		case mysqlFamily(d) && fam != famBool && fam != famInteger && fam != famNumeric:
			r.defWhy = "a boolean default on a column that is not boolean or a number is not read on " + d
		default:
			// AbstractSqliteDriver.js:470-471, MysqlDriver.js:657-658: 1/0.
			text := "0"
			if v.text == "true" {
				text = "1"
			}
			known(true, text, "TO-C4")
			r.defForm = "boolean:" + fam
		}
	case "fn":
		switch d {
		case "postgres":
			text, ok := pgFunctionDefault(v.text)
			if !ok {
				r.defWhy = "postgres rewrites a datetime function default other than CURRENT_TIMESTAMP (PostgresDriver.js:1270-1304)"
				return
			}
			known(true, text, "TO-C4")
		case "sqlite":
			known(true, v.text, "TO-C4")
		default:
			text, ok := mysqlFunctionDefault(d, v.text)
			if !ok {
				r.defWhy = "the driver rewrites this datetime function default (MysqlDriver.js:1115-1135)"
				return
			}
			known(true, text, "TO-C4")
		}
		r.defForm = "fn:" + v.text
	}
}

// stringDefault renders a string default (TO-C4, Revision 4 A1, A2): every
// driver writes '<value>' (quoteDefault), which a character, text or enum
// column stores as written; on a boolean column the value Postgres stores
// (pgBooleanText) or, on sqlite, the text as written; on any other family
// the stored value is the database's own rendering of the text (a date,
// a uuid, a number) and is not claimed, except the texts the database
// rejects, which are TO-P1 (10) to (12).
func (col *column) stringDefault(d, fam, where string, r *rendered) {
	c := col.t.c
	v := col.def
	known := func(text string, form string) {
		r.defKnown, r.present, r.defaultRow, r.defForm = true, true, "TO-C4", form
		r.text = &text
	}
	switch {
	case fam == famChar || fam == famEnum:
		known(quoteDefault(v.text), "string:"+fam)
	case fam == famBool && d == "postgres":
		if b, ok := pgBooleanText(v.text); ok {
			known(b, "string:bool")
			return
		}
		r.defWhy = "a string default on a boolean column that postgres's boolean input does not name"
	case fam == famBool && d == "sqlite":
		if _, ok := pgBooleanText(v.text); ok {
			known(quoteDefault(v.text), "string:bool")
			return
		}
		r.defWhy = "a string default on a boolean column is read only for true/false, yes/no, on/off, 1/0, t/f and y/n"
	case fam == famDatetime && d == "postgres" && pgDatetimeKeyword.MatchString(v.text):
		c.fail("(10)", fmt.Sprintf("%s: the string default %q on a date or time column, which postgres's date and time input rejects (write () => %q)", where, v.text, v.text), d)
		r.defWhy = "the string default names a datetime keyword"
	case fam == famUUID && d == "postgres" && strings.Contains(v.text, "("):
		c.fail("(11)", fmt.Sprintf("%s: the string default %q on a uuid column, which postgres's uuid input rejects (write () => %q)", where, v.text, v.text), d)
		r.defWhy = "the string default is not a uuid"
	case fam == famDatetime && d == "mysql" && mysqlDatetimeString.MatchString(v.text):
		c.fail("(12)", fmt.Sprintf("%s: the string default %q on a date or time column, which MySQL rejects (ER_INVALID_DEFAULT; write () => %q)", where, v.text, v.text), d)
		r.defWhy = "the string default names a datetime function"
	case fam == "":
		r.defWhy = "a string default is read only on a known character, text or enum type"
	default:
		r.defWhy = "a string default on a " + fam + " column is stored in the database's own rendering (not read)"
	}
}

// numberDefault renders a number default (TO-C4, Revision 4 A2):
// postgres writes '<n>' (PostgresDriver.js:802-806), stored as written on
// integer, numeric, character and enum columns and as true/false for 1/0
// on a boolean column; sqlite writes the number as written
// (AbstractSqliteDriver.js:467-468); mysql and mariadb write
// '<n.toFixed(scale)>' (MysqlDriver.js:654-655).
func (col *column) numberDefault(d, fam string, r *rendered) {
	v := col.def
	known := func(text string) {
		r.defKnown, r.present, r.defaultRow, r.defForm = true, true, "TO-C4", "number:"+fam
		r.text = &text
	}
	switch {
	case fam == "":
		r.defWhy = "a number default is read only on a known type"
	case d == "postgres" && fam == famBool:
		switch v.text {
		case "1":
			known("true")
		case "0":
			known("false")
		default:
			r.defWhy = "a number default other than 1 or 0 on a boolean column"
		}
	case d == "postgres" && fam == famEnum:
		known(quoteDefault(v.text))
	case d == "postgres" && (fam == famInteger || fam == famNumeric || fam == famChar):
		known(v.text)
	case d == "postgres":
		r.defWhy = "a number default on a " + fam + " column is not read on postgres"
	case d == "sqlite":
		known(v.text)
	case fam == famDatetime || fam == famUUID || fam == famOther:
		r.defWhy = "a number default on a " + fam + " column is not read on " + d
	case !fractional(v.text) && col.scale == nil:
		// MysqlDriver.normalizeDefault :654-655 (toFixed of no scale).
		known(quoteDefault(v.text))
	default:
		r.defWhy = "a fractional or scaled number default is not mapped on " + d
	}
}

// record writes the kit column (finish).
func (col *column) record() *models.Column {
	t := col.t
	c := t.c
	rec := &models.Column{Name: col.name, Attribute: col.prop, File: col.file, Line: col.line, Attributes: map[string]any{}, Claims: map[string]models.ClaimSource{}, ForeignKeysUnknown: true}
	if seg := identitySegment(col.name); seg != col.name {
		rec.IdentityName = seg
	}
	for _, k := range []string{"type", "length", "precision", "scale", "nullable", "primary_key", "autoincrement", "unique", "default", "server_default", "server_default_text"} {
		rec.Attributes[k] = nil
	}
	reason := func(row, fact, format string, args ...any) {
		var f *string
		if fact != "" {
			f = models.Fact(fact)
		}
		rec.Reasons = append(rec.Reasons, models.Reason{Row: row, Fact: f, File: col.file, Line: col.line, Reason: fmt.Sprintf(format, args...)})
	}
	if col.unknownWhy != "" {
		rec.Unknown = true
		reason(col.unknownRow, "", "%s", col.unknownWhy)
		t.partial(col.unknownRow, col.file, col.line, "the column %s is not read", col.name)
		return rec
	}
	flat := c.weakest()
	if t.syncOff {
		flat = models.VerificationMapped
	}
	claim := func(fact string, v any, row string) {
		c.tag(row)
		rec.Attributes[fact] = v
		rec.Claims[fact] = models.ClaimSource{Row: row, Verification: flat}
	}
	// defaultVer lowers a default's verification to mapped when the
	// oracle fixture does not compare its form on the dialect (Revision 4
	// B1).
	defaultVer := map[string]bool{}
	perDialect := func(d, fact string, v any, row string) {
		c.tag(row)
		ver := c.g.Verification[d]
		if t.syncOff || defaultVer[d] && (fact == "server_default" || fact == "server_default_text") {
			ver = models.VerificationMapped
		}
		src := models.ClaimSource{Row: row, Verification: ver}
		if c.runtime() {
			rec.SetDialectFact(d, fact, v, src)
			return
		}
		rec.Attributes[fact] = v
		if fact != "server_default_text" {
			rec.Claims[fact] = src
		}
	}
	wrapRow := func(row string) string {
		if col.wrapper != "" {
			return "TO-C6"
		}
		return row
	}
	claim("name", col.name, wrapRow(col.nameRow))
	claim("primary_key", col.primary, wrapRow("TO-C1"))
	switch {
	case col.primary && col.nullable != nil && *col.nullable:
		reason("TO-C1", "nullable", "a primary column with nullable: true is not read")
	case col.primary:
		claim("nullable", false, wrapRow("TO-C1"))
	case col.deco == "DeleteDateColumn":
		claim("nullable", true, wrapRow("TO-C3"))
	case col.nullWhy != "":
		reason("TO-C4", "nullable", "%s", col.nullWhy)
	case col.nullable != nil:
		claim("nullable", *col.nullable, wrapRow("TO-C4"))
	default:
		claim("nullable", false, wrapRow("TO-C4"))
	}
	switch {
	case col.uniqueWhy != "":
		reason("TO-C4", "unique", "%s", col.uniqueWhy)
	case col.unique != nil && *col.unique:
		claim("unique", true, wrapRow("TO-C4"))
	}
	typeRow := map[string]string{"explicit": "TO-C2", "enum": "TO-C5", "date": "TO-C3", "version": "TO-C3", "generated": "TO-C1"}
	source, _ := col.typeSource()
	row := typeRow[source]
	switch {
	case col.mergedRef != nil:
		row = col.mergedRow
	case col.wrapType:
		row = "TO-C6"
	case col.typeWhy != "" && col.typeRow != "":
		row = col.typeRow
	case row == "":
		row = "TO-C2"
	}
	anyType := false
	for _, d := range c.dialects {
		r := col.r[d]
		if r.typ == "" {
			reason(row, "type", "%s: %s", d, r.typWhy)
		} else {
			anyType = true
			perDialect(d, "type", r.typ, row)
			if r.length != nil {
				perDialect(d, "length", *r.length, row)
			}
			if r.precision != nil {
				perDialect(d, "precision", *r.precision, row)
			}
			if r.scale != nil {
				perDialect(d, "scale", *r.scale, row)
			}
		}
		if r.ai != nil {
			perDialect(d, "autoincrement", *r.ai, "TO-C1")
		}
	}
	if col.isEnum && col.enum != nil && anyType {
		var labels []string
		for _, v := range col.enum {
			labels = append(labels, v.text)
		}
		rec.Attributes["enum"] = map[string]any{"values": labels}
	}
	// Defaults: server_default (and its text) per dialect; default when
	// every dialect agrees.
	agree, present, first := true, false, true
	defRow := "TO-C4"
	formMapped := false
	for _, d := range c.dialects {
		r := col.r[d]
		if !r.defKnown {
			agree = false
			reason("TO-C4", "server_default", "%s: %s", d, r.defWhy)
			continue
		}
		if !oracleDefaultForms[d][r.defForm] {
			defaultVer[d], formMapped = true, true
		}
		if defaultFormHook != nil && c.g.Verification[d] == models.VerificationOracle && !t.syncOff {
			defaultFormHook(t.columnIdentity(col.name), d, r.defForm)
		}
		defRow = r.defaultRow
		if first {
			present, first = r.present, false
		} else if r.present != present {
			agree = false
		}
		perDialect(d, "server_default", r.present, r.defaultRow)
		switch {
		case r.text != nil:
			perDialect(d, "server_default_text", *r.text, r.defaultRow)
		case r.present:
			reason(r.defaultRow, "server_default_text", "%s: %s", d, r.textWhy)
		}
	}
	if formMapped {
		flat = models.VerificationMapped
	}
	switch {
	case !agree:
		reason(defRow, "default", "the default is not known on every dialect, or not the same")
	case present:
		claim("default", "server", defRow)
	default:
		claim("default", "absent", defRow)
	}
	return rec
}

// columnIdentity is the identity the kit gives the column node named
// name (TestTypeORMOracle maps default forms through it).
func (t *table) columnIdentity(name string) string {
	return "column:" + strings.TrimPrefix(t.rec.Identity, "table:") + "." + name
}

// finish writes the table's columns, then its constraints
// (constraints.go) and the reasons of its partial sets.
func (t *table) finish() {
	recs := map[*column]*models.Column{}
	for _, col := range t.cols {
		var rec *models.Column
		switch {
		case col.join != nil:
			rec = col.joinRecord()
		case col.mergedRef != nil:
			col.mergeTypes()
			rec = col.record()
		default:
			rec = col.record()
		}
		t.rec.Columns = append(t.rec.Columns, rec)
		recs[col] = rec
	}
	t.constraints(recs)
}

// objectFailures records TO-P1 (3) for the columns whose design type is
// proven Object and that no join column reuses.
func (t *table) objectFailures() {
	if t.relFailed || t.fkOpen {
		// An unread owning relation may reuse the column.
		return
	}
	for _, col := range t.cols {
		if col.object != "" && col.mergedRef == nil {
			t.c.fail("(3)", col.object)
		}
	}
}

// mergeTypes gives an explicit column a join column reuses the referenced
// column's type on every dialect (RelationJoinColumnBuilder.js:186-190 /
// :205-209). Only the type is replaced, so a type with a
// length, precision or scale on either side is unknown.
func (col *column) mergeTypes() {
	ref := col.mergedRef
	for d, r := range col.r {
		rr := ref.r[d]
		switch {
		case rr.typ == "":
			r.typ, r.typWhy = "", d+": the referenced column's type is unknown"
		case rr.length != nil || rr.precision != nil || rr.scale != nil || col.length != nil || col.precision != nil || col.scale != nil:
			r.typ, r.typWhy = "", "the reused column takes the referenced type without its length, precision or scale (not read)"
		default:
			r.typ, r.typWhy = rr.typ, ""
		}
		r.length, r.precision, r.scale = nil, nil, nil
		r.ai, r.aiWhy = nil, ""
		col.r[d] = r
	}
}

// joinRecord writes a join column the relation pass added (TO-R1, TO-R2)
// or a junction column (TO-R4): typed as the referenced column on every
// dialect (length, precision and scale kept), no default, no generation.
func (col *column) joinRecord() *models.Column {
	t := col.t
	c := t.c
	j := col.join
	rec := &models.Column{Name: col.name, Attribute: col.prop, File: col.file, Line: col.line, Attributes: map[string]any{}, Claims: map[string]models.ClaimSource{}, ForeignKeysUnknown: true}
	if seg := identitySegment(col.name); seg != col.name {
		rec.IdentityName = seg
	}
	for _, k := range []string{"type", "length", "precision", "scale", "nullable", "primary_key", "autoincrement", "unique", "default", "server_default", "server_default_text"} {
		rec.Attributes[k] = nil
	}
	flat := t.flatVerification()
	claim := func(fact string, v any, row string) {
		c.tag(row)
		rec.Attributes[fact] = v
		rec.Claims[fact] = models.ClaimSource{Row: row, Verification: flat}
	}
	perDialect := func(d, fact string, v any) {
		c.tag(j.row)
		ver := c.g.Verification[d]
		if t.syncOff {
			ver = models.VerificationMapped
		}
		src := models.ClaimSource{Row: j.row, Verification: ver}
		if c.runtime() {
			rec.SetDialectFact(d, fact, v, src)
			return
		}
		rec.Attributes[fact] = v
		rec.Claims[fact] = src
	}
	claim("name", col.name, j.row)
	claim("primary_key", col.primary, j.row)
	if j.nullable != nil {
		claim("nullable", *j.nullable, j.row)
	} else {
		rec.Reasons = append(rec.Reasons, models.Reason{Row: j.row, Fact: models.Fact("nullable"), File: col.file, Line: col.line, Reason: j.nullWhy})
	}
	for _, d := range c.dialects {
		rr := j.ref.r[d]
		if rr.typ == "" {
			rec.Reasons = append(rec.Reasons, models.Reason{Row: j.row, Fact: models.Fact("type"), File: col.file, Line: col.line, Reason: d + ": the referenced column " + j.ref.name + "'s type is unknown"})
		} else {
			perDialect(d, "type", rr.typ)
			if rr.length != nil {
				perDialect(d, "length", *rr.length)
			}
			if rr.precision != nil {
				perDialect(d, "precision", *rr.precision)
			}
			if rr.scale != nil {
				perDialect(d, "scale", *rr.scale)
			}
		}
		perDialect(d, "server_default", false)
		if defaultFormHook != nil && c.g.Verification[d] == models.VerificationOracle && !t.syncOff {
			// A join column has no default: the form "absent".
			defaultFormHook(t.columnIdentity(col.name), d, "absent")
		}
	}
	claim("default", "absent", j.row)
	return rec
}

func intPtr(n int) *int    { return &n }
func boolPtr(b bool) *bool { return &b }
