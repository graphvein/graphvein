package diesel

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/graphvein/graphvein/internal/extract/models"
)

// The DI-C2 type rules: the database type a generated column's Diesel type
// path names, per dialect (docs/extract/schema-diesel.md "Claim contract"
// and "Mapped dialects").

// lengthRule says how #[max_length] applies to a builtin type.
type lengthRule int

const (
	// noLength: the type has no length; #[max_length] on it is not
	// print-schema output (type unknown).
	noLength lengthRule = iota
	// optionalLength: `(n)` with #[max_length = n], else the type without
	// a length (varchar and varbit without one are unbounded).
	optionalLength
	// requiredLength: `(n)` with #[max_length = n]; without one the
	// length is not stated (bpchar, bit: print-schema writes it whenever
	// information_schema has one).
	requiredLength
)

// pgBuiltin is one entry of DI-C2's closed Postgres builtin list.
type pgBuiltin struct {
	// udt is the udt_name print-schema writes as UpperCamelCase(udt)
	// (diesel_cli 2.3.14 infer_schema_internals/pg.rs:66-67).
	udt string
	// spelling is the type in format_type's spelling.
	spelling string
	length   lengthRule
	// dropped lists the kit modifiers (ADR 0065) print-schema drops for
	// the type (Revision 1 O4): the type is claimed as its family with
	// these modifiers unknown. An interval's fields have no kit modifier:
	// its family lists precision, and the fields are not stated (the
	// family claims the base type only).
	dropped []string
	// structOnly marks a pg_catalog type diesel provides no SQL type for:
	// print-schema writes it as a sql_types struct with schema =
	// "pg_catalog" (timetz), never as a bare ident, so it is in the
	// spelling map for structs only (Revision 2, A2a).
	structOnly bool
}

// pgBuiltins is the builtin list of row DI-C2 (and numeric, whose
// modifiers print-schema drops).
var pgBuiltins = []pgBuiltin{
	{udt: "int2", spelling: "smallint"},
	{udt: "int4", spelling: "integer"},
	{udt: "int8", spelling: "bigint"},
	{udt: "float4", spelling: "real"},
	{udt: "float8", spelling: "double precision"},
	{udt: "bool", spelling: "boolean"},
	{udt: "text", spelling: "text"},
	{udt: "varchar", spelling: "character varying", length: optionalLength},
	{udt: "bpchar", spelling: "character", length: requiredLength},
	{udt: "char", spelling: `"char"`},
	{udt: "bytea", spelling: "bytea"},
	{udt: "date", spelling: "date"},
	{udt: "time", spelling: "time without time zone", dropped: []string{"precision"}},
	{udt: "timestamp", spelling: "timestamp without time zone", dropped: []string{"precision"}},
	{udt: "timestamptz", spelling: "timestamp with time zone", dropped: []string{"precision"}},
	{udt: "timetz", spelling: "time with time zone", dropped: []string{"precision"}, structOnly: true},
	{udt: "interval", spelling: "interval", dropped: []string{"precision"}},
	{udt: "numeric", spelling: "numeric", dropped: []string{"precision", "scale"}},
	{udt: "json", spelling: "json"},
	{udt: "jsonb", spelling: "jsonb"},
	{udt: "uuid", spelling: "uuid"},
	{udt: "oid", spelling: "oid"},
	{udt: "money", spelling: "money"},
	{udt: "inet", spelling: "inet"},
	{udt: "cidr", spelling: "cidr"},
	{udt: "macaddr", spelling: "macaddr"},
	{udt: "macaddr8", spelling: "macaddr8"},
	{udt: "citext", spelling: "citext"},
	{udt: "tsvector", spelling: "tsvector"},
	{udt: "tsquery", spelling: "tsquery"},
	{udt: "bit", spelling: "bit", length: requiredLength},
	{udt: "varbit", spelling: "bit varying", length: optionalLength},
	{udt: "xml", spelling: "xml"},
	{udt: "int4range", spelling: "int4range"},
	{udt: "int8range", spelling: "int8range"},
	{udt: "daterange", spelling: "daterange"},
	{udt: "numrange", spelling: "numrange"},
	{udt: "tsrange", spelling: "tsrange"},
	{udt: "tstzrange", spelling: "tstzrange"},
	{udt: "pg_lsn", spelling: "pg_lsn"},
}

// pgByIdent and pgByUDT index the builtin list by the ident print-schema
// writes and by udt name.
var pgByIdent, pgByUDT = func() (map[string]*pgBuiltin, map[string]*pgBuiltin) {
	byIdent, byUDT := map[string]*pgBuiltin{}, map[string]*pgBuiltin{}
	for i := range pgBuiltins {
		b := &pgBuiltins[i]
		if !b.structOnly {
			byIdent[upperCamel(b.udt)] = b
		}
		byUDT[b.udt] = b
	}
	return byIdent, byUDT
}()

// upperCamel is heck's to_upper_camel_case for the lower-case udt names of
// the builtin list: words split at `_`, each capitalised ("pg_lsn" ->
// "PgLsn", "int4" -> "Int4").
func upperCamel(s string) string {
	var b strings.Builder
	for _, w := range strings.Split(s, "_") {
		if w != "" {
			b.WriteString(strings.ToUpper(w[:1]) + w[1:])
		}
	}
	return b.String()
}

// typeAnswer is DI-C2's answer for one column: a full type, a type family
// whose listed modifiers print-schema drops (Revision 1 O4, claimed
// through the kit's SetTypeFamily, ADR 0065), or why the type is unknown.
// Exactly one of typ, family and why is set.
type typeAnswer struct {
	typ string
	// family is the type without the modifiers in unknown (format_type's
	// spelling of the type with no modifier, `[]` for an array).
	family  string
	unknown []string
	why     string
}

func typeKnown(typ string) typeAnswer { return typeAnswer{typ: typ} }

func typeUnknown(why string) typeAnswer { return typeAnswer{why: why} }

func typeFamily(name string, unknown ...string) typeAnswer {
	return typeAnswer{family: name, unknown: unknown}
}

// array is the answer for an array of the element's answer: `[]` added to
// the type or the family.
func (a typeAnswer) array() typeAnswer {
	switch {
	case a.typ != "":
		a.typ += "[]"
	case a.family != "":
		a.family += "[]"
	}
	return a
}

// pgType is DI-C2 on postgres for a type path without its outer Nullable.
func (g *group) pgType(tr *tableRec, t *Type, maxLength *int) typeAnswer {
	if !bare(t) {
		return typeUnknown(fmt.Sprintf("the type path %s is not a bare ident (print-schema writes none): the type is unknown", t))
	}
	s := t.Segments[0]
	switch {
	case s.Name == "Array" && len(s.Args) == 1:
		g.in.tag("DI-C2/Array<T>")
		if maxLength != nil {
			return typeUnknown(fmt.Sprintf("#[max_length] on the array %s is not print-schema output: the type is unknown", t))
		}
		el := s.Args[0]
		if bare(el) && el.Segments[0].Name == "Nullable" && len(el.Segments[0].Args) == 1 {
			// Postgres stores no element nullability.
			el = el.Segments[0].Args[0]
		}
		if !bare(el) || len(el.Segments[0].Args) > 0 {
			return typeUnknown(fmt.Sprintf("the array element of %s is not a bare ident: the type is unknown", t))
		}
		return g.pgBase(tr, el.Segments[0].Name, nil, true).array()
	case len(s.Args) > 0:
		if s.Name == "Range" || s.Name == "Multirange" {
			g.in.tag("DI-C2/Range<T>")
		}
		return typeUnknown(fmt.Sprintf("the generic type %s is not print-schema's spelling (only Nullable and Array take arguments): the type is unknown", t))
	}
	return g.pgBase(tr, s.Name, maxLength, false)
}

// pgBase maps a bare type ident (an array's element when array is set;
// the caller adds the `[]`).
func (g *group) pgBase(tr *tableRec, ident string, maxLength *int, array bool) typeAnswer {
	if s := g.structFor(tr, ident); s != nil {
		name, why := g.structType(ident, s, "postgres")
		if why != "" {
			return typeUnknown(why)
		}
		if maxLength != nil {
			return typeUnknown(fmt.Sprintf("#[max_length] on the custom type %s is not print-schema output: the type is unknown", ident))
		}
		// print-schema writes the schema of every type outside the default
		// schema, pg_catalog's too (tsvector, tsquery, xml, varbit, point:
		// diesel_cli 2.3.14 infer_schema_internals/pg.rs:58-64), but
		// format_type never qualifies a pg_catalog type (pg_catalog is
		// always searched first): such a struct names the type alone (the
		// oracle's probe run).
		structSchema := s.Schema
		if structSchema == "pg_catalog" {
			structSchema = ""
		}
		// A builtin name goes through the same spelling map.
		if b := pgByUDT[name]; b != nil && structSchema == "" {
			return g.pgBuiltin(ident, b, nil, array)
		}
		if s.Enum {
			g.in.tag("DI-S1/#[diesel(enum_type)]")
		}
		quoted, ok := pgIdent(name)
		if !ok {
			return typeUnknown(fmt.Sprintf("the sql_types struct %s names the type %q, whose format_type spelling (quoting) is not modelled: the type is unknown", ident, name))
		}
		if structSchema != "" {
			schema, ok := pgIdent(structSchema)
			if !ok {
				return typeUnknown(fmt.Sprintf("the sql_types struct %s names the schema %q, whose format_type spelling (quoting) is not modelled: the type is unknown", ident, structSchema))
			}
			quoted = schema + "." + quoted
		}
		switch {
		case s.Enum, s.Schema == "pg_catalog":
			// An enum takes no type modifiers (diesel_cli 2.4 marks its
			// struct #[diesel(enum_type)]); neither does any pg_catalog
			// type outside the builtin list (the types that take one,
			// SELECT typname FROM pg_type WHERE typmodin <> 0 on Postgres
			// 17, are all in it): the type is claimed as written.
			return typeKnown(quoted)
		}
		// Any other custom type may take modifiers print-schema drops
		// (PostGIS geometry(Point, 4326), pgvector vector(1536)): its
		// family with the generic modifiers unknown (Revision 2, A2b).
		return typeFamily(quoted, models.ModifiersUnknown)
	}
	b := pgByIdent[ident]
	if b == nil {
		return typeUnknown(fmt.Sprintf("%s is neither a builtin type print-schema writes nor a struct of the file's sql_types module the table imports: the type is unknown", ident))
	}
	return g.pgBuiltin(ident, b, maxLength, array)
}

// pgBuiltin maps a builtin of the DI-C2 list; a type whose modifiers
// print-schema drops is its family (Revision 1 O4).
func (g *group) pgBuiltin(ident string, b *pgBuiltin, maxLength *int, array bool) typeAnswer {
	switch {
	case maxLength != nil && b.length == noLength:
		return typeUnknown(fmt.Sprintf("#[max_length] on %s, which has no length, is not print-schema output: the type is unknown", ident))
	case len(b.dropped) > 0:
		if b.udt == "numeric" {
			g.in.tag("DI-C2/Numeric")
		}
		return typeFamily(b.spelling, b.dropped...)
	case array && b.length != noLength:
		// information_schema reports no length for an array column, so
		// print-schema writes none: varchar(n)[] reads as Array<Varchar>.
		return typeFamily(b.spelling, "length")
	case maxLength != nil:
		g.in.tag("DI-C2/#[max_length = n]")
		return typeKnown(fmt.Sprintf("%s(%d)", b.spelling, *maxLength))
	case b.length == requiredLength, g.before21 && b.length == optionalLength:
		// diesel_cli 2.0 writes no #[max_length] at all (Revision 2, A1):
		// in a file that does not prove diesel_cli 2.1 or later output,
		// whatever the pin, varchar and varbit without one are their
		// family too (Revision 2 addendum).
		return typeFamily(b.spelling, "length")
	}
	return typeKnown(b.spelling)
}

var plainIdent = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

// pgIdent is a type or schema name as format_type writes it: plain when it
// is a lower-case identifier that is not a keyword Postgres quotes, which
// is the only form claimed (ok false otherwise: quote_identifier's rules
// for other names are not modelled).
func pgIdent(name string) (string, bool) {
	if !plainIdent.MatchString(name) || pgQuotedKeywords[name] {
		return "", false
	}
	return name, true
}

// pgQuotedKeywords are Postgres 17's keywords that quote_identifier quotes
// (every category but unreserved: SELECT word FROM pg_get_keywords() WHERE
// catcode <> 'U').
var pgQuotedKeywords = func() map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(`all analyse analyze and any array as asc asymmetric authorization between
bigint binary bit boolean both case cast char character check coalesce collate collation column concurrently
constraint create cross current_catalog current_date current_role current_schema current_time current_timestamp
current_user dec decimal default deferrable desc distinct do else end except exists extract false fetch float
for foreign freeze from full grant greatest group grouping having ilike in initially inner inout int integer
intersect interval into is isnull join json json_array json_arrayagg json_exists json_object json_objectagg
json_query json_scalar json_serialize json_table json_value lateral leading least left like limit localtime
localtimestamp merge_action national natural nchar none normalize not notnull null nullif numeric offset on
only or order out outer overlaps overlay placing position precision primary real references returning right
row select session_user setof similar smallint some substring symmetric system_user table tablesample then time
timestamp to trailing treat trim true union unique user using values varchar variadic verbose when where window
with xmlattributes xmlconcat xmlelement xmlexists xmlforest xmlnamespaces xmlparse xmlpi xmlroot xmlserialize
xmltable`) {
		m[w] = true
	}
	return m
}()

// mysqlExact are the MySQL idents print-schema writes whose lower-cased
// name is the type (docs "Mapped dialects", MySQL).
var mysqlExact = map[string]string{
	"Bool": "bool", "Integer": "integer", "Tinyint": "tinyint", "Smallint": "smallint", "Mediumint": "mediumint", "Bigint": "bigint",
	"Tinytext": "tinytext", "Text": "text", "Mediumtext": "mediumtext", "Longtext": "longtext",
	"Tinyblob": "tinyblob", "Blob": "blob", "Mediumblob": "mediumblob", "Longblob": "longblob",
	"Date": "date", "Year": "year", "Json": "json",
}

// mysqlLength are the idents whose length is #[max_length].
var mysqlLength = map[string]string{"Char": "char", "Varchar": "varchar", "Binary": "binary", "Varbinary": "varbinary"}

// mysqlFamily are the idents whose parameters print-schema drops, with the
// kit modifiers (ADR 0065) they are: a fractional seconds precision is
// precision.
var mysqlFamily = map[string][]string{
	"Decimal": {"precision", "scale"}, "Float": {"precision", "scale"}, "Double": {"precision", "scale"},
	"Bit": {"length"}, "Datetime": {"precision"}, "Timestamp": {"precision"}, "Time": {"precision"},
}

// mysqlInteger are the idents Unsigned<...> wraps.
var mysqlInteger = map[string]bool{"Integer": true, "Tinyint": true, "Smallint": true, "Mediumint": true, "Bigint": true}

// mysqlType is DI-C2 on mysql (mapped from diesel_cli 2.3.14
// infer_schema_internals/mysql.rs:219-280).
func (g *group) mysqlType(tr *tableRec, t *Type, maxLength *int) typeAnswer {
	if !bare(t) {
		return typeUnknown(fmt.Sprintf("the type path %s is not a bare ident (print-schema writes none): the type is unknown", t))
	}
	s := t.Segments[0]
	if s.Name == "Unsigned" && len(s.Args) == 1 {
		el := s.Args[0]
		if !bare(el) || len(el.Segments[0].Args) > 0 || !mysqlInteger[el.Segments[0].Name] || maxLength != nil {
			return typeUnknown(fmt.Sprintf("%s is not an unsigned integer type print-schema writes: the type is unknown", t))
		}
		return typeKnown(mysqlExact[el.Segments[0].Name] + " unsigned")
	}
	if len(s.Args) > 0 {
		return typeUnknown(fmt.Sprintf("the generic type %s is not print-schema's spelling on mysql: the type is unknown", t))
	}
	ident := s.Name
	if st := g.structFor(tr, ident); st != nil {
		name, why := g.structType(ident, st, "mysql")
		if why != "" {
			return typeUnknown(why)
		}
		switch name {
		case "Enum", "Set":
			// The labels are not in schema.rs and are no kit modifier, so
			// no family is claimed either.
			return typeUnknown(fmt.Sprintf("%s: the %s's labels are not in schema.rs and a type family has no labels modifier (ADR 0065): the type is unknown", ident, strings.ToLower(name)))
		}
		return typeUnknown(fmt.Sprintf("the mysql_type struct %s names %q, which print-schema does not write: the type is unknown", ident, name))
	}
	switch {
	case mysqlLength[ident] != "":
		if maxLength == nil {
			return typeFamily(mysqlLength[ident], "length")
		}
		return typeKnown(fmt.Sprintf("%s(%d)", mysqlLength[ident], *maxLength))
	case maxLength != nil:
		return typeUnknown(fmt.Sprintf("#[max_length] on %s is not print-schema output: the type is unknown", ident))
	case mysqlFamily[ident] != nil:
		return typeFamily(strings.ToLower(ident), mysqlFamily[ident]...)
	case mysqlExact[ident] != "":
		return typeKnown(mysqlExact[ident])
	}
	return typeUnknown(fmt.Sprintf("%s is not a MySQL type print-schema writes: the type is unknown", ident))
}
