package typeorm

import (
	"regexp"
	"strconv"
	"strings"
)

// This file holds the per-dialect rendering of column types and defaults
// (rows TO-C1 to TO-C5): the explicit type lists the oracle ran (postgres,
// sqlite; the investigation), the mapped MySQL and MariaDB list (TA2), the
// drivers' supported type lists that prove a failure (TO-P1 (2)), and the
// default texts of each driver's normalizeDefault. Citations are to
// typeorm 0.3.31 (the 1.1.3 lines are in docs/extract/schema-typeorm.md,
// "TypeORM 1.x comparison"; every list below is the same at 1.1.3 except
// SQLite's supported list, which adds jsonb).

// pgTypes maps the postgres explicit types of TO-C2 to the catalog type
// (driver/postgres/PostgresDriver.js normalizeType :732-789; the catalog
// spells int as integer and time as time without time zone).
var pgTypes = map[string]string{
	"int": "integer", "int4": "integer", "integer": "integer",
	"varchar": "character varying", "character varying": "character varying",
	"text": "text", "bigint": "bigint", "int8": "bigint", "smallint": "smallint", "int2": "smallint",
	"boolean": "boolean", "bool": "boolean", "decimal": "numeric", "numeric": "numeric",
	"float": "double precision", "double precision": "double precision", "float8": "double precision",
	"real": "real", "float4": "real", "date": "date", "time": "time without time zone",
	"timestamp": "timestamp without time zone", "timestamptz": "timestamp with time zone",
	"uuid": "uuid", "json": "json", "jsonb": "jsonb", "character": "character", "char": "character",
	"simple-array": "text", "simple-json": "text",
}

// sqliteTypes maps the sqlite explicit types of TO-C2 to the declared
// type TypeORM writes (driver/sqlite-abstract/AbstractSqliteDriver.js
// normalizeType :433-461: int is integer, the simple types text).
var sqliteTypes = map[string]string{
	"int": "integer", "integer": "integer", "varchar": "varchar", "text": "text", "bigint": "bigint",
	"smallint": "smallint", "boolean": "boolean", "decimal": "decimal", "numeric": "numeric",
	"float": "float", "double precision": "double precision", "real": "real", "date": "date",
	"time": "time", "datetime": "datetime", "character": "character",
	"simple-array": "text", "simple-json": "text",
}

// mysqlTypes maps the mapped MySQL and MariaDB types of TO-C2
// (driver/mysql/MysqlDriver.js normalizeType :578-640).
var mysqlTypes = map[string]string{
	"int": "int", "integer": "int", "tinyint": "tinyint", "smallint": "smallint", "bigint": "bigint",
	"varchar": "varchar", "text": "text", "datetime": "datetime", "date": "date",
	"boolean": "tinyint", "bool": "tinyint", "decimal": "decimal", "float": "float", "double": "double",
	"json": "json",
}

// The drivers' supportedDataTypes (PostgresDriver.js:50-126,
// AbstractSqliteDriver.js:37-70, MysqlDriver.js:44-104), checked by
// EntityMetadataValidator.js:89 after normalizeType: a type outside them
// makes initialize() fail for the whole data source (TO-P1 (2)).
var (
	pgSupported = set(
		"int", "int2", "int4", "int8", "smallint", "integer", "bigint", "decimal", "numeric", "real",
		"float", "float4", "float8", "double precision", "money", "character varying", "varchar",
		"character", "char", "text", "citext", "hstore", "bytea", "bit", "varbit", "bit varying",
		"timetz", "timestamptz", "timestamp", "timestamp without time zone", "timestamp with time zone",
		"date", "time", "time without time zone", "time with time zone", "interval", "bool", "boolean",
		"enum", "point", "line", "lseg", "box", "path", "polygon", "circle", "cidr", "inet", "macaddr",
		"macaddr8", "tsvector", "tsquery", "uuid", "xml", "json", "jsonb", "jsonpath", "int4range",
		"int8range", "numrange", "tsrange", "tstzrange", "daterange", "int4multirange", "int8multirange",
		"nummultirange", "tsmultirange", "tstzmultirange", "datemultirange", "geometry", "geography",
		"cube", "ltree", "vector", "halfvec",
	)
	sqliteSupported = set(
		"int", "integer", "tinyint", "smallint", "mediumint", "bigint", "unsigned big int", "int2",
		"int8", "character", "varchar", "varying character", "nchar", "native character", "nvarchar",
		"text", "clob", "blob", "real", "double", "double precision", "float", "numeric", "decimal",
		"boolean", "date", "time", "datetime", "json",
	)
	mysqlSupported = set(
		"bit", "int", "integer", "tinyint", "smallint", "mediumint", "bigint", "float", "double",
		"double precision", "real", "decimal", "dec", "numeric", "fixed", "bool", "boolean", "date",
		"datetime", "timestamp", "time", "year", "char", "nchar", "national char", "varchar", "nvarchar",
		"national varchar", "blob", "text", "tinyblob", "tinytext", "mediumblob", "mediumtext",
		"longblob", "longtext", "enum", "set", "binary", "varbinary", "json", "geometry", "point",
		"linestring", "polygon", "multipoint", "multilinestring", "multipolygon", "geometrycollection",
		"vector", "uuid", "inet4", "inet6",
	)
)

func set(list ...string) map[string]bool {
	out := map[string]bool{}
	for _, w := range list {
		out[w] = true
	}
	return out
}

// supportedType reports whether the driver of dialect accepts the explicit
// type t after its normalizeType; ok is false for a dialect whose list is
// not read (mssql).
func supportedType(dialect, t string) (supported, ok bool) {
	switch dialect {
	case "postgres":
		n := t
		switch t {
		case "simple-array", "simple-json":
			n = "text"
		case "simple-enum":
			n = "enum"
		}
		return pgSupported[n], true
	case "sqlite":
		n := t
		switch t {
		case "uuid", "simple-enum":
			n = "varchar"
		case "simple-array", "simple-json":
			n = "text"
		}
		return sqliteSupported[n], true
	case "mysql", "mariadb":
		n := t
		switch t {
		case "simple-array", "simple-json":
			n = "text"
		case "simple-enum":
			n = "enum"
		}
		return mysqlSupported[n], true
	}
	return false, false
}

// lengthTypes are the rendered types a literal length is written for
// (PostgresDriver.js withLengthColumnTypes :138-148,
// AbstractSqliteDriver.js :78-88, MysqlDriver getColumnLength :684-700).
var lengthTypes = map[string]map[string]bool{
	"postgres": {"character varying": true, "character": true},
	"sqlite":   {"varchar": true, "character": true, "text": true},
	"mysql":    {"varchar": true},
	"mariadb":  {"varchar": true},
}

// precisionTypes are the rendered types a literal precision (and scale)
// is written for; elsewhere the fact is not claimed.
var precisionTypes = map[string]map[string]bool{
	"postgres": {"numeric": true},
	"sqlite":   {"decimal": true, "numeric": true},
	"mysql":    {"decimal": true},
	"mariadb":  {"decimal": true},
}

// integerTypes are the rendered integer types (a fractional default on
// one fails on postgres, TO-P1 (5)).
var integerTypes = map[string]bool{"integer": true, "smallint": true, "bigint": true, "int": true, "tinyint": true}

// plainNumber matches a number literal whose JavaScript string form is its
// own text, so the default text TypeORM writes is the source text.
var plainNumber = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]*[1-9])?$`)

// fractional reports a number literal with a fractional part.
func fractional(n string) bool { return strings.Contains(n, ".") }

// pgDatetimeKeywords are the texts PostgresDriver.normalizeDatetimeFunction
// rewrites (PostgresDriver.js:1270-1304).
var pgDatetimeKeywords = []string{"CURRENT_TIMESTAMP", "CURRENT_DATE", "CURRENT_TIME", "LOCALTIMESTAMP", "LOCALTIME"}

// pgFunctionDefault renders a function default on postgres: exactly
// CURRENT_TIMESTAMP is now(); any other text naming a datetime keyword is
// rewritten in ways the claim does not follow (unknown); other texts are
// kept as written.
func pgFunctionDefault(text string) (string, bool) {
	upper := strings.ToUpper(text)
	for _, k := range pgDatetimeKeywords {
		if strings.Contains(upper, k) {
			return "now()", text == "CURRENT_TIMESTAMP"
		}
	}
	return text, true
}

// mysqlFunctionDefault renders a function default on mysql and mariadb
// (MysqlDriver.normalizeDatetimeFunction :1115-1135): a text naming
// CURRENT_TIMESTAMP or NOW is rewritten to CURRENT_TIMESTAMP with the
// text's precision (mariadb: CURRENT_TIMESTAMP() without one), so only
// the texts that stay as written are claimed (TO-C4: a function default
// as its text): on mysql exactly CURRENT_TIMESTAMP and
// CURRENT_TIMESTAMP(<n>); any other datetime text, and every one on
// mariadb, is unknown.
func mysqlFunctionDefault(dialect, text string) (string, bool) {
	upper := strings.ToUpper(text)
	if !strings.Contains(upper, "CURRENT_TIMESTAMP") && !strings.Contains(upper, "NOW") {
		return text, true
	}
	if dialect == "mysql" && mysqlTimestamp.MatchString(text) {
		return text, true
	}
	return "", false
}

var mysqlTimestamp = regexp.MustCompile(`^CURRENT_TIMESTAMP(\([0-9]+\))?$`)

// quoteDefault is the default text of a string value: every driver writes
// '<value>' without escaping (PostgresDriver.js:806, AbstractSqliteDriver.js:477,
// MysqlDriver.js:650).
func quoteDefault(v string) string { return "'" + v + "'" }

func intText(n int) string { return strconv.Itoa(n) }

// Type families of a rendered column type (Revision 4 A1, A2): the
// default forms a family accepts and the oracle compared differ, so the
// default claims are made per family.
const (
	famChar     = "char"
	famEnum     = "enum"
	famBool     = "bool"
	famInteger  = "integer"
	famNumeric  = "numeric"
	famDatetime = "datetime"
	famUUID     = "uuid"
	famOther    = "other"
)

// family is the type family of the column's type on dialect d, "" when
// the type is not known there.
func (col *column) family(d string, r *rendered) string {
	if r.typ == "" {
		return ""
	}
	if col.explicit == "enum" || col.explicit == "simple-enum" {
		return famEnum
	}
	base := strings.TrimSuffix(strings.SplitN(r.typ, "(", 2)[0], "[]")
	switch {
	case base == "tinyint" && mysqlFamily(d) && col.explicit != "tinyint":
		// A boolean (design or explicit boolean/bool) is tinyint there.
		return famBool
	case integerTypes[base]:
		return famInteger
	}
	switch base {
	case "text":
		if mysqlFamily(d) {
			// MySQL takes no literal default on TEXT (reference manual,
			// "Data Type Default Values": BLOB, TEXT, GEOMETRY and JSON
			// only as an expression).
			return famOther
		}
		return famChar
	case "character varying", "character", "varchar":
		return famChar
	case "boolean":
		return famBool
	case "numeric", "decimal", "real", "double precision", "float", "double":
		return famNumeric
	case "date", "time", "time without time zone", "timestamp without time zone", "timestamp with time zone", "datetime":
		return famDatetime
	case "uuid":
		return famUUID
	}
	return famOther
}

// pgBooleanText is the value Postgres stores for a boolean input text
// TypeORM writes quoted (PG17 boolin: true/false, yes/no, on/off, 1/0
// and t/f/y/n, any case); a text outside this list is not claimed (Revision
// 4 A2, oracle-compared).
func pgBooleanText(s string) (string, bool) {
	switch strings.ToLower(s) {
	case "true", "t", "yes", "y", "on", "1":
		return "true", true
	case "false", "f", "no", "n", "off", "0":
		return "false", true
	}
	return "", false
}

// pgDatetimeKeyword matches a string default naming an SQL datetime
// keyword (any case, an optional precision): Postgres's date and time
// input rejects it, so TypeORM's quoted '<keyword>' default fails at
// CREATE TABLE (TO-P1 (10), PG17: invalid input syntax for type timestamp).
var pgDatetimeKeyword = regexp.MustCompile(`(?i)^(CURRENT_TIMESTAMP|CURRENT_DATE|CURRENT_TIME|LOCALTIMESTAMP|LOCALTIME)(\([0-9]+\))?$`)

// mysqlDatetimeString matches a string default MySQL rejects on a date or
// time column under its default sql_mode (TO-P1 (12)): TypeORM writes it
// quoted (MysqlDriver.normalizeDefault :646-649), a string literal that is
// not a date or time value, so CREATE TABLE fails with ER_INVALID_DEFAULT
// (MySQL reference manual, "Data Type Default Values": a literal default
// must be a constant of the column's type; CURRENT_TIMESTAMP is the
// exception only unquoted).
var mysqlDatetimeString = regexp.MustCompile(`(?i)^(CURRENT_TIMESTAMP(\([0-9]*\))?|NOW\([0-9]*\))$`)

// The explicit types whose driver accepts a length
// (withLengthColumnTypes after normalizeType: PostgresDriver.js:138-148,
// AbstractSqliteDriver.js:78-88, MysqlDriver.js:124-131); a length on a
// type the driver knows and these do not hold fails the
// EntityMetadataValidator (:89-92, TO-P1 (13)). The values are the
// explicit spellings (and the simple types normalizeType turns into
// one).
var lengthExplicit = map[string]map[string]bool{
	"postgres": set("character varying", "varchar", "character", "char", "bit", "varbit", "bit varying", "vector", "halfvec"),
	"sqlite":   set("character", "varchar", "varying character", "nchar", "native character", "nvarchar", "text", "blob", "clob", "uuid", "simple-enum", "simple-array", "simple-json"),
	"mysql":    set("char", "varchar", "nvarchar", "binary", "varbinary", "vector"),
}

// pgNoModifier are the postgres catalog types (pgTypes values) whose DDL
// takes no type modifier: TypeORM's createFullType appends (p) or (p,s)
// for a precision (PostgresDriver.js:850-864) and PG17 rejects
// `integer(5)` (syntax error) and `text(5)` (type modifier is not
// allowed) (TO-P1 (14)).
var pgNoModifier = set("integer", "smallint", "bigint", "text", "boolean", "uuid", "date", "double precision", "real", "json", "jsonb")

// oracleDefaultForms are the default forms (rendered.defForm) the oracle
// fixture compares on each dialect (scripts/oracle/typeorm_catalog.sh
// --fixture, Revision 4 B1): a default of another form is labelled
// `mapped` even on an oracle dialect, and TestTypeORMOracle fails when a
// form listed here is not compared by a fixture run. A function default's
// form is its exact text.
var oracleDefaultForms = map[string]map[string]bool{
	"postgres": set("absent", "generated:increment", "generated:uuid", "date:driver",
		"string:char", "string:enum", "string:bool", "number:integer", "number:numeric", "number:enum", "number:bool",
		"boolean:bool", "fn:CURRENT_TIMESTAMP", "fn:now()", "fn:gen_random_uuid()", "fn:'none'"),
	"sqlite": set("absent", "generated:none", "date:driver",
		"string:char", "string:enum", "string:bool", "number:integer", "number:numeric", "number:bool",
		"boolean:bool", "fn:CURRENT_TIMESTAMP", "fn:datetime('now')", "fn:'none'"),
}

// defaultFormHook, when set (TestTypeORMOracle only), receives the form
// of each known default on a dialect whose verification is oracle, with
// its column node's identity.
var defaultFormHook func(column, dialect, form string)
