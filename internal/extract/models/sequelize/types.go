package sequelize

import (
	"fmt"
	"strings"

	"github.com/graphvein/graphvein/internal/lang/typescript"
)

// This file reads attribute data types (row SQ-A1) and renders them per
// dialect: the text Sequelize 6.37.8 writes into CREATE TABLE
// (lib/data-types.js and lib/dialects/<dialect>/data-types.js, through
// normalizeDataType, lib/sequelize.js:558-573) and the type the claim
// states: for postgres the catalog's format_type text, for sqlite the
// declared type as the catalog keeps it (the oracle's forms, the investigation), for
// mysql, mariadb and mssql the rendered text (mapped, SQ-V6).

// scope is where an attribute or option object is written: a module, or
// the body of a factory `module.exports = (sequelize, DataTypes) => {...}`
// run by a call that passes the instance (row SQ-K1). Names resolve
// through the factory's parameters first, then the module.
type scope struct {
	d    *discovery
	g    *Instance
	file string
	fac  *Factory
	// locals are the names the factory body declares; they shadow module
	// names and are not resolved.
	locals map[string]bool
}

// instanceValue marks the instance itself (a factory's first parameter).
func (s scope) instanceValue() value { return value{File: s.g.File, Decl: s.g.Name} }

// resolve resolves an identifier or member expression of the scope.
func (s scope) resolve(e *typescript.ModelExpr) value {
	if e == nil {
		return value{}
	}
	switch e.Kind {
	case typescript.ModelIdentifier:
		if s.fac != nil {
			if len(s.fac.Params) > 0 && e.Name == s.fac.Params[0] {
				return s.instanceValue()
			}
			for i, p := range s.fac.Params {
				if i == 0 || p != e.Name {
					continue
				}
				if i < len(s.fac.Call.Args) {
					return s.d.expr(s.fac.CallFile, &s.fac.Call.Args[i], 0)
				}
				return value{Why: "the factory call passes no argument for " + p}
			}
			if s.locals[e.Name] {
				return value{Why: e.Name + " is declared in the factory body"}
			}
		}
		return s.d.local(s.file, e.Name, 0)
	case typescript.ModelMember:
		o := s.resolve(e.Object)
		if o.External != "" && (o.Name == "" || o.Name == "*" || o.Name == "default" || o.Name == "Sequelize") {
			return value{External: o.External, Name: e.Name}
		}
		return value{Why: "member " + e.Name + " of a value that is not a package object"}
	}
	return s.d.expr(s.file, e, 0)
}

// isTypes reports an expression that holds Sequelize's data types: the
// `DataTypes` export of `sequelize`, the Sequelize class or the package
// object (both carry every type as a static member), a factory parameter
// bound to one of them.
func (s scope) isTypes(e *typescript.ModelExpr) bool {
	v := s.resolve(e)
	if v.External != pkgSequelize {
		return false
	}
	switch v.Name {
	case "", "*", "default", "Sequelize", "DataTypes":
		return true
	}
	return false
}

// isSequelize reports the instance or the Sequelize class or package
// object: the receivers of `literal` and `fn`.
func (s scope) isSequelize(e *typescript.ModelExpr) bool {
	v := s.resolve(e)
	if v.External == pkgSequelize {
		return v.Name == "" || v.Name == "*" || v.Name == "default" || v.Name == "Sequelize"
	}
	return v.Decl == s.g.Name && v.File == s.g.File
}

// typeSpec is a data type of SQ-A1's closed list (the forms the investigation
// oracle ran).
type typeSpec struct {
	// key is the DataTypes member: STRING, CHAR, TEXT, INTEGER, BIGINT,
	// SMALLINT, FLOAT, DOUBLE, DECIMAL, BOOLEAN, DATE, DATEONLY, TIME, UUID,
	// JSON, JSONB, BLOB, ENUM, ARRAY, RANGE or VIRTUAL.
	key string
	// form is the oracle form ("STRING(n)", "DECIMAL(p,s)", "ARRAY(ENUM)").
	form string
	// length is STRING(n) and CHAR(n)'s n (255 when omitted).
	length int
	// precision and scale are DECIMAL(p, s)'s; hasPS says they are given.
	precision, scale int
	hasPS            bool
	// values are an ENUM's labels; valuesKnown says every label is a
	// string literal; valuesExpr is set when the labels are written in the
	// type's arguments (the attribute's `values` key overrides them).
	values      []string
	valuesKnown bool
	valuesGiven bool
	// elem is ARRAY's element type and RANGE's subtype.
	elem *typeSpec
}

// arrayElements are the element types ARRAY(T) is read for (the investigation ran
// ARRAY(STRING), ARRAY(INTEGER) and ARRAY(ENUM)).
var arrayElements = map[string]bool{"STRING": true, "INTEGER": true, "ENUM": true}

// noArgTypes take no arguments (a call with none is the same type).
var noArgTypes = map[string]bool{
	"TEXT": true, "INTEGER": true, "BIGINT": true, "SMALLINT": true, "FLOAT": true, "DOUBLE": true,
	"BOOLEAN": true, "DATE": true, "DATEONLY": true, "TIME": true, "UUID": true, "JSON": true,
	"JSONB": true, "BLOB": true,
}

// readType reads a type expression: `DataTypes.X` or `DataTypes.X(args)`.
// why says why it is not read: the column itself is then unknown (an
// unread type could be VIRTUAL).
func (s scope) readType(e *typescript.ModelExpr) (typeSpec, string) {
	if e == nil {
		return typeSpec{}, "the attribute has no type"
	}
	if e.Kind == typescript.ModelNew {
		return typeSpec{}, "a `new` data type expression is not read (row SQ-A1)"
	}
	callee, args, call := e, []typescript.ModelExpr(nil), false
	if e.Kind == typescript.ModelCall {
		callee, args, call = e.Callee, e.Args, true
	}
	if callee.Kind != typescript.ModelMember || !s.isTypes(callee.Object) {
		if e.Kind == typescript.ModelString {
			return typeSpec{}, "a string type (raw SQL) is not read (row SQ-A1)"
		}
		return typeSpec{}, "the type is not a member of Sequelize's DataTypes (a project type or helper is not read, row SQ-A1)"
	}
	key := callee.Name
	t := typeSpec{key: key, form: key}
	for i := range args {
		if args[i].Kind == typescript.ModelSpread && key != "ENUM" {
			return typeSpec{}, "DataTypes." + key + " with a spread argument is not read"
		}
	}
	switch {
	case key == "VIRTUAL":
		return t, ""
	case noArgTypes[key]:
		if len(args) > 0 {
			return typeSpec{}, fmt.Sprintf("DataTypes.%s with arguments is not read (row SQ-A1)", key)
		}
		return t, ""
	case key == "STRING" || key == "CHAR":
		t.length = 255
		switch {
		case len(args) == 0 && key == "STRING":
			_ = call
			return t, ""
		case len(args) == 1:
			n, ok := intArg(&args[0])
			if !ok || n < 1 {
				return typeSpec{}, fmt.Sprintf("DataTypes.%s with an argument that is not a positive integer literal is not read", key)
			}
			t.length, t.form = n, key+"(n)"
			return t, ""
		}
		return typeSpec{}, fmt.Sprintf("DataTypes.%s with these arguments is not read (row SQ-A1)", key)
	case key == "DECIMAL":
		if len(args) == 0 {
			return t, ""
		}
		if len(args) == 2 {
			p, ok1 := intArg(&args[0])
			sc, ok2 := intArg(&args[1])
			if ok1 && ok2 && p >= 1 && p <= 1000 && sc <= p {
				t.precision, t.scale, t.hasPS, t.form = p, sc, true, "DECIMAL(p,s)"
				return t, ""
			}
		}
		return typeSpec{}, "DataTypes.DECIMAL is read without arguments or with two integer literals (precision, scale) only"
	case key == "ENUM":
		t.values, t.valuesKnown = enumValues(args)
		t.valuesGiven = len(args) > 0
		return t, ""
	case key == "ARRAY":
		if len(args) != 1 {
			return typeSpec{}, "DataTypes.ARRAY is read with one element type only"
		}
		el, why := s.readType(&args[0])
		if why != "" {
			return typeSpec{}, "ARRAY element: " + why
		}
		if !arrayElements[el.key] {
			return typeSpec{}, "ARRAY(" + el.form + ") is not read (row SQ-A1 reads ARRAY of STRING, INTEGER and ENUM)"
		}
		t.elem, t.form = &el, "ARRAY("+el.key+")"
		return t, ""
	case key == "RANGE":
		if len(args) == 1 {
			el, why := s.readType(&args[0])
			if why == "" && el.key == "DATE" {
				t.elem, t.form = &el, "RANGE(DATE)"
				return t, ""
			}
		}
		return typeSpec{}, "only RANGE(DATE) is read (row SQ-A1)"
	}
	return typeSpec{}, "DataTypes." + key + " is not in the read list (row SQ-A1)"
}

// enumValues reads ENUM's arguments: string literals, or one array literal
// of them. Anything else (an identifier, a spread, a number) leaves the
// labels unknown.
func enumValues(args []typescript.ModelExpr) ([]string, bool) {
	list := args
	if len(args) == 1 && args[0].Kind == typescript.ModelArray {
		list = args[0].Elements
	}
	if len(list) == 0 {
		return nil, false
	}
	var out []string
	for i := range list {
		v, ok := literalString(&list[i])
		if !ok {
			return nil, false
		}
		out = append(out, v)
	}
	return out, true
}

// enumNonString reports an ENUM label list that holds a literal that is
// not a string (postgres writes it unquoted into CREATE TYPE, row SQ-P1).
func enumNonString(args []typescript.ModelExpr) bool {
	list := args
	if len(args) == 1 && args[0].Kind == typescript.ModelArray {
		list = args[0].Elements
	}
	for i := range list {
		switch list[i].Kind {
		case typescript.ModelNumber, typescript.ModelBoolean, typescript.ModelNull:
			return true
		}
	}
	return false
}

// typeText is a rendered type: sql as Sequelize writes it into CREATE
// TABLE, claim as the claim states it.
type typeText struct {
	sql, claim string
}

// render renders t on dialect for column field of table (postgres enum
// names, lib/dialects/postgres/query-generator.js:564-572 and
// lib/utils.js generateEnumName). why says why it is not rendered.
func (t typeSpec) render(dialect, table, field string, schema bool) (typeText, string) {
	switch dialect {
	case "postgres":
		return t.renderPostgres(table, field, schema)
	case "sqlite":
		return t.renderSQLite()
	case "mysql", "mariadb":
		return t.renderMySQL()
	case "mssql":
		return t.renderMSSQL()
	}
	return typeText{}, "the dialect is not known"
}

func (t typeSpec) renderPostgres(table, field string, schema bool) (typeText, string) {
	switch t.key {
	case "STRING":
		return typeText{fmt.Sprintf("VARCHAR(%d)", t.length), fmt.Sprintf("character varying(%d)", t.length)}, ""
	case "CHAR":
		return typeText{fmt.Sprintf("CHAR(%d)", t.length), fmt.Sprintf("character(%d)", t.length)}, ""
	case "DECIMAL":
		if t.hasPS {
			return typeText{fmt.Sprintf("DECIMAL(%d,%d)", t.precision, t.scale), fmt.Sprintf("numeric(%d,%d)", t.precision, t.scale)}, ""
		}
		return typeText{"DECIMAL", "numeric"}, ""
	case "ENUM":
		if schema {
			return typeText{}, "an ENUM of a table with a schema option: the type's name as the catalog prints it is not compared"
		}
		name := "enum_" + table + "_" + field
		if !pgIdentifierOK(name) {
			return typeText{}, "the enum type name " + name + " is longer than 63 bytes (postgres truncates it)"
		}
		return typeText{"ENUM", pgTypeName(name)}, ""
	case "ARRAY":
		el, why := t.elem.renderPostgres(table, field, schema)
		if why != "" {
			return typeText{}, why
		}
		return typeText{el.sql + "[]", el.claim + "[]"}, ""
	case "RANGE":
		return typeText{"tstzrange", "tstzrange"}, ""
	}
	if p, ok := pgTypes[t.key]; ok {
		return p, ""
	}
	return typeText{}, t.key + " is not rendered on postgres"
}

// pgTypes are the postgres renderings of the types without arguments:
// the generator's text and format_type's.
var pgTypes = map[string]typeText{
	"TEXT": {"TEXT", "text"}, "INTEGER": {"INTEGER", "integer"}, "BIGINT": {"BIGINT", "bigint"},
	"SMALLINT": {"SMALLINT", "smallint"}, "FLOAT": {"FLOAT", "double precision"},
	"DOUBLE": {"DOUBLE PRECISION", "double precision"}, "BOOLEAN": {"BOOLEAN", "boolean"},
	"DATE": {"TIMESTAMP WITH TIME ZONE", "timestamp with time zone"}, "DATEONLY": {"DATE", "date"},
	"TIME": {"TIME", "time without time zone"}, "UUID": {"UUID", "uuid"}, "JSON": {"JSON", "json"},
	"JSONB": {"JSONB", "jsonb"}, "BLOB": {"BYTEA", "bytea"},
}

// sqliteTypes are the sqlite renderings (the declared type the catalog
// keeps); ENUM is TEXT.
var sqliteTypes = map[string]string{
	"TEXT": "TEXT", "INTEGER": "INTEGER", "BIGINT": "BIGINT", "SMALLINT": "SMALLINT", "FLOAT": "FLOAT",
	"DOUBLE": "DOUBLE PRECISION", "BOOLEAN": "TINYINT(1)", "DATE": "DATETIME", "DATEONLY": "DATE",
	"TIME": "TIME", "UUID": "UUID", "JSON": "JSON", "BLOB": "BLOB", "ENUM": "TEXT",
}

func (t typeSpec) renderSQLite() (typeText, string) {
	switch t.key {
	case "STRING":
		s := fmt.Sprintf("VARCHAR(%d)", t.length)
		return typeText{s, s}, ""
	case "CHAR":
		s := fmt.Sprintf("CHAR(%d)", t.length)
		return typeText{s, s}, ""
	case "DECIMAL":
		s := "DECIMAL"
		if t.hasPS {
			s = fmt.Sprintf("DECIMAL(%d,%d)", t.precision, t.scale)
		}
		return typeText{s, s}, ""
	}
	if s, ok := sqliteTypes[t.key]; ok {
		return typeText{s, s}, ""
	}
	return typeText{}, t.key + " is a postgres type (not on sqlite)"
}

// mysqlTypes are the mysql and mariadb renderings (mapped, SQ-V6).
var mysqlTypes = map[string]string{
	"TEXT": "TEXT", "INTEGER": "INTEGER", "BIGINT": "BIGINT", "SMALLINT": "SMALLINT", "FLOAT": "FLOAT",
	"DOUBLE": "DOUBLE PRECISION", "BOOLEAN": "TINYINT(1)", "DATE": "DATETIME", "DATEONLY": "DATE",
	"TIME": "TIME", "UUID": "CHAR(36) BINARY", "JSON": "JSON", "BLOB": "BLOB",
}

func (t typeSpec) renderMySQL() (typeText, string) {
	switch t.key {
	case "STRING":
		s := fmt.Sprintf("VARCHAR(%d)", t.length)
		return typeText{s, s}, ""
	case "CHAR":
		s := fmt.Sprintf("CHAR(%d)", t.length)
		return typeText{s, s}, ""
	case "DECIMAL":
		s := "DECIMAL"
		if t.hasPS {
			s = fmt.Sprintf("DECIMAL(%d,%d)", t.precision, t.scale)
		}
		return typeText{s, s}, ""
	case "ENUM":
		if !t.valuesKnown {
			return typeText{}, "the ENUM labels are not literal, so the mysql column type is not known"
		}
		var parts []string
		for _, v := range t.values {
			if strings.ContainsAny(v, "'\\\x00\n\r\x1a\"") {
				return typeText{}, "an ENUM label holds a character mysql's escape rewrites; not mapped"
			}
			parts = append(parts, "'"+v+"'")
		}
		s := "ENUM(" + strings.Join(parts, ", ") + ")"
		return typeText{s, s}, ""
	}
	if s, ok := mysqlTypes[t.key]; ok {
		return typeText{s, s}, ""
	}
	return typeText{}, t.key + " is not mapped on mysql or mariadb"
}

// mssqlTypes are the mssql renderings (mapped, SQ-V6); JSON is not
// supported there (dialects/abstract/index.js:61) and ENUM writes a CHECK
// instead of the column's other clauses, so neither is mapped.
var mssqlTypes = map[string]string{
	"TEXT": "NVARCHAR(MAX)", "INTEGER": "INTEGER", "BIGINT": "BIGINT", "SMALLINT": "SMALLINT", "FLOAT": "FLOAT",
	"DOUBLE": "DOUBLE PRECISION", "BOOLEAN": "BIT", "DATE": "DATETIMEOFFSET", "DATEONLY": "DATE",
	"TIME": "TIME", "UUID": "CHAR(36)", "BLOB": "VARBINARY(MAX)",
}

func (t typeSpec) renderMSSQL() (typeText, string) {
	switch t.key {
	case "STRING":
		s := fmt.Sprintf("NVARCHAR(%d)", t.length)
		return typeText{s, s}, ""
	case "CHAR":
		s := fmt.Sprintf("CHAR(%d)", t.length)
		return typeText{s, s}, ""
	case "DECIMAL":
		s := "DECIMAL"
		if t.hasPS {
			s = fmt.Sprintf("DECIMAL(%d,%d)", t.precision, t.scale)
		}
		return typeText{s, s}, ""
	}
	if s, ok := mssqlTypes[t.key]; ok {
		return typeText{s, s}, ""
	}
	return typeText{}, t.key + " is not mapped on mssql"
}
