package gorm

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// This file renders a parsed field per dialect (rows GO-T2, GO-C1-C3):
// the column type as the claim states it (postgres: the catalog's
// format_type text; sqlite: the declared type GORM writes; mysql: the type
// text GORM writes, mapped from gorm.io/driver/mysql v1.6.0, GO-V5), its
// modifiers, whether the database generates the value (autoincrement),
// the not-null flag the catalog shows and the default.

// rendered is one field on one dialect. A nil pointer or an empty string
// with its Why set is unknown.
type rendered struct {
	Type, TypeWhy            string
	Length, Precision, Scale *int
	// AI says the database generates the value (serial, AUTOINCREMENT,
	// AUTO_INCREMENT); InlinePK marks sqlite's inline "integer PRIMARY KEY
	// AUTOINCREMENT", which replaces the table's primary key.
	AI, InlinePK bool
	// NotNull is the catalog's not-null flag; NotNullWhy set when unknown.
	NotNull    bool
	NotNullWhy string
	Default    defaultFact
}

// defaultFact is a column default on one dialect: Why set when even its
// presence is unknown; Text the default text the catalog shows, or nil
// with TextWhy.
type defaultFact struct {
	Present      bool
	Why          string
	Text         *string
	TextWhy      string
	presentKnown bool
}

func intp(n int) *int { return &n }

// pgRaw are the raw type: texts the postgres claim reads (GO-T2's closed
// list), with the catalog text each gives.
var pgRaw = []struct {
	re  *regexp.Regexp
	out func([]string) string
}{
	{regexp.MustCompile(`(?i)^varchar\((\d+)\)$`), func(m []string) string { return "character varying(" + m[1] + ")" }},
	{regexp.MustCompile(`(?i)^text$`), func([]string) string { return "text" }},
	{regexp.MustCompile(`(?i)^uuid$`), func([]string) string { return "uuid" }},
	{regexp.MustCompile(`(?i)^timestamp$`), func([]string) string { return "timestamp without time zone" }},
	{regexp.MustCompile(`(?i)^timestamptz$`), func([]string) string { return "timestamp with time zone" }},
	{regexp.MustCompile(`(?i)^text\[\]$`), func([]string) string { return "text[]" }},
	{regexp.MustCompile(`(?i)^(jsonb|json|date|boolean|bigint|integer|smallint|bytea)$`), func(m []string) string { return strings.ToLower(m[1]) }},
}

var (
	// reRawType is one type name with optional (n[,m]) or [] (Revision 2,
	// A1); reSerialType the serial family.
	reRawType    = regexp.MustCompile(`^\s*[A-Za-z_][A-Za-z0-9_]*\s*(\(\s*\d+\s*(,\s*\d+\s*)?\))?\s*(\[\s*\])?\s*$`)
	reSerialType = regexp.MustCompile(`(?i)^\s*(small|big)?serial[248]?\s*$`)
	reLength     = regexp.MustCompile(`^(?:character varying|varchar|varbinary)\((\d+)\)$`)
	reNumeric    = regexp.MustCompile(`^(?:numeric|decimal)\((\d+), ?(\d+)\)$`)
	reTimePrec   = regexp.MustCompile(`^(?:timestamp|datetime)\((\d+)\)`)
	reIntText    = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)
	reFloatText  = regexp.MustCompile(`^(0|[1-9][0-9]*)(\.[0-9]*[1-9])?$`)
)

// modifiers reads a rendered type's length, precision and scale.
func (r *rendered) modifiers() {
	if m := reLength.FindStringSubmatch(r.Type); m != nil {
		n, _ := strconv.Atoi(m[1])
		r.Length = intp(n)
	}
	if m := reNumeric.FindStringSubmatch(r.Type); m != nil {
		p, _ := strconv.Atoi(m[1])
		s, _ := strconv.Atoi(m[2])
		r.Precision, r.Scale = intp(p), intp(s)
	}
	if m := reTimePrec.FindStringSubmatch(r.Type); m != nil {
		n, _ := strconv.Atoi(m[1])
		r.Precision = intp(n)
	}
}

// pgIntType is getSchemaIntType (postgres.go:271-285): the size grows by
// one for an unsigned type.
func pgIntType(size int, unsigned bool) string {
	if unsigned {
		size++
	}
	switch {
	case size <= 16:
		return "smallint"
	case size <= 32:
		return "integer"
	}
	return "bigint"
}

// render renders f on dialect d; my holds the mysql dialector settings.
func render(f *gfield, d string, my MySQLConfig) rendered {
	var r rendered
	switch d {
	case DialectPostgres:
		r = renderPostgres(f)
	case DialectSQLite:
		r = renderSQLite(f)
	case DialectMySQL:
		r = renderMySQL(f, my)
	}
	if r.Type != "" {
		r.modifiers()
	}
	r.Default = defaultOn(f, d, r)
	switch {
	case f.RawWhy != "":
		r.Type, r.TypeWhy = "", f.RawWhy
		r.Length, r.Precision, r.Scale = nil, nil, nil
		r.NotNullWhy = f.RawWhy
		r.Default = defaultFact{Why: f.RawWhy}
	case f.SerialRaw != "":
		r.NotNullWhy = f.SerialRaw
		r.Default = defaultFact{Why: f.SerialRaw}
	}
	return r
}

func renderPostgres(f *gfield) rendered {
	var r rendered
	serial := false
	switch f.DataType {
	case "bool":
		r.Type = "boolean"
	case "int", "uint":
		r.Type = pgIntType(f.Size, f.DataType == "uint")
		serial = f.AI
	case "float":
		if f.Precision > 0 {
			r.Type = fmt.Sprintf("numeric(%d,%d)", f.Precision, f.Scale)
		} else {
			r.Type = "numeric"
		}
	case "string":
		if f.Size > 0 && f.Size <= 10485760 {
			r.Type = fmt.Sprintf("character varying(%d)", f.Size)
		} else {
			r.Type = "text"
		}
	case "time":
		if f.Precision > 0 {
			r.Type = fmt.Sprintf("timestamp(%d) with time zone", f.Precision)
		} else {
			r.Type = "timestamp with time zone"
		}
	case "bytes":
		r.Type = "bytea"
	default:
		if f.AI && !strings.Contains(strings.ToLower(f.DataType), "serial") {
			// getSchemaCustomType (postgres.go:287-306): a serial by the
			// size, unsigned by the GORM data type.
			serial = true
			r.Type = pgIntType(f.Size, f.GORMDataType == "uint")
			break
		}
		for _, x := range pgRaw {
			if m := x.re.FindStringSubmatch(f.DataType); m != nil {
				r.Type = x.out(m)
				break
			}
		}
		if r.Type == "" {
			r.TypeWhy = "the raw type " + strconv.Quote(f.DataType) + " is not in GO-T2's closed postgres list"
		}
	}
	r.AI = serial
	r.NotNull = f.NotNull || f.PK || serial
	return r
}

func renderSQLite(f *gfield) rendered {
	var r rendered
	switch f.DataType {
	case "bool":
		r.Type = "numeric"
	case "int", "uint":
		r.Type = "integer"
		if f.AI {
			r.AI, r.InlinePK = true, true
		}
	case "float":
		r.Type = "real"
	case "string":
		r.Type = "text"
	case "time":
		if v, ok := f.Tags["TYPE"]; ok {
			r.Type = v
		} else {
			r.Type = "datetime"
		}
	case "bytes":
		r.Type = "blob"
	default:
		r.Type = f.DataType
		if strings.Contains(strings.ToUpper(f.DataType), "PRIMARY KEY") {
			r.Type, r.TypeWhy = "", "the raw type "+strconv.Quote(f.DataType)+" carries a PRIMARY KEY"
		}
	}
	// sqlite keeps only the tag's NOT NULL: an INTEGER PRIMARY KEY or a
	// composite key column is nullable in the catalog (GO-C2).
	r.NotNull = f.NotNull
	return r
}

// mysqlSetting reads one dialector setting.
func mysqlSetting(my MySQLConfig, key string) (Setting, string) {
	s, ok := my[key]
	if !ok || !s.Known {
		why := s.Reason
		if why == "" {
			why = "the mysql dialector's " + key + " is not read"
		}
		return s, why
	}
	return s, ""
}

func renderMySQL(f *gfield, my MySQLConfig) rendered {
	var r rendered
	switch f.DataType {
	case "bool":
		r.Type = "boolean"
	case "int", "uint":
		if f.PK && f.HasDefault && strings.ToLower(strings.TrimSpace(f.Default)) == "auto_random()" {
			r.TypeWhy = "auto_random() is a TiDB form, not mapped"
			break
		}
		size := f.Size
		var t string
		switch {
		case size <= 8:
			t = "tinyint"
		case size <= 16:
			t = "smallint"
		case size <= 24:
			t = "mediumint"
		case size <= 32:
			t = "int"
		default:
			t = "bigint"
		}
		if f.DataType == "uint" {
			t += " unsigned"
		}
		r.Type = t
		r.AI = f.AI
	case "float":
		switch {
		case f.Precision > 0:
			r.Type = fmt.Sprintf("decimal(%d, %d)", f.Precision, f.Scale)
		case f.Size <= 32:
			r.Type = "float"
		default:
			r.Type = "double"
		}
	case "string":
		size := f.Size
		if size == 0 {
			s, why := mysqlSetting(my, "DefaultStringSize")
			n, _ := strconv.Atoi(s.Value)
			switch {
			case why != "":
				r.TypeWhy = why
				return r
			case s.Set && n > 0:
				size = n
			case f.PK || f.HasDefault || f.Tags["INDEX"] != "" || f.Tags["UNIQUE"] != "":
				size = 191
			}
		}
		switch {
		case size >= 65536 && size <= 1<<24:
			r.Type = "mediumtext"
		case size > 1<<24 || size <= 0:
			r.Type = "longtext"
		default:
			r.Type = fmt.Sprintf("varchar(%d)", size)
		}
	case "time":
		prec := f.Precision
		disable, why := mysqlSetting(my, "DisableDatetimePrecision")
		if why != "" {
			r.TypeWhy = why
			return r
		}
		if !(disable.Set && disable.Value == "true") && prec == 0 && f.Tags["PRECISION"] == "" {
			def, why := mysqlSetting(my, "DefaultDatetimePrecision")
			switch {
			case why != "":
				r.TypeWhy = why
				return r
			case def.Set:
				n, err := strconv.Atoi(def.Value)
				if err != nil {
					r.TypeWhy = "the mysql dialector's DefaultDatetimePrecision is not a number"
					return r
				}
				prec = n
			default:
				prec = 3 // mysql.go:66
			}
		}
		r.Type = "datetime"
		if prec > 0 {
			r.Type += fmt.Sprintf("(%d)", prec)
		}
	case "bytes":
		switch {
		case f.Size > 0 && f.Size < 65536:
			r.Type = fmt.Sprintf("varbinary(%d)", f.Size)
		case f.Size >= 65536 && f.Size <= 1<<24:
			r.Type = "mediumblob"
		default:
			r.Type = "longblob"
		}
	default:
		r.Type = f.DataType
		r.AI = f.AI
	}
	r.NotNull = f.NotNull || f.PK
	if f.AI && !f.PK {
		r.NotNullWhy = "mysql: an AUTO_INCREMENT column that is not the primary key is not mapped"
		if r.Type != "" {
			r.Type, r.TypeWhy = "", r.NotNullWhy
		}
	}
	return r
}

// defaultOn is the default GORM writes for f on dialect d
// (migrator.go:98-106, field.go:210-282, row GO-C3).
func defaultOn(f *gfield, d string, r rendered) defaultFact {
	known := func(present bool) defaultFact { return defaultFact{Present: present, presentKnown: true} }
	if f.AI && f.Default != "" {
		return defaultFact{Why: "an autoincrement column with a default tag is not read"}
	}
	if r.AI && d == DialectPostgres {
		df := known(true)
		df.TextWhy = "a serial's nextval default names a sequence postgres chooses"
		return df
	}
	if !f.HasDefault || f.AI && f.Default == "" {
		return known(false)
	}
	v := f.Default
	if f.Parsed {
		df := known(true)
		text, why := "", ""
		switch f.Type.Kind {
		case "int", "uint":
			if !reIntText.MatchString(v) {
				why = "the default " + strconv.Quote(v) + " is not a plain decimal; GORM binds the parsed value"
			}
			text = v
		case "float":
			if !reFloatText.MatchString(v) {
				why = "the default " + strconv.Quote(v) + " is not a plain decimal; GORM binds the parsed value"
			}
			text = v
		case "bool":
			b, _ := strconv.ParseBool(v)
			text = strconv.FormatBool(b)
			if d == DialectMySQL {
				why = "mysql's text of a boolean default is not mapped"
			}
		case "string":
			switch {
			case d == DialectPostgres && (r.Type == "text" || strings.HasPrefix(r.Type, "character varying")) && plainText(v, "'"):
				text = "'" + v + "'::text"
				if r.Type != "text" {
					text = "'" + v + "'::character varying"
				}
			case d == DialectSQLite && plainText(v, `"`):
				text = `"` + v + `"`
			default:
				why = "the text of this string default is not claimed on " + d
			}
		default:
			why = "the default's text is not claimed"
		}
		if why == "" && (f.Type.Kind == "int" || f.Type.Kind == "uint" || f.Type.Kind == "float") && !numericType(d, r.Type) {
			why = "the default's text on a " + r.Type + " column is not claimed"
		}
		if why == "" && f.Type.Kind == "bool" && d == DialectPostgres && r.Type != "boolean" {
			why = "the default's text on a " + r.Type + " column is not claimed"
		}
		if r.Type == "" && why == "" {
			why = "the column type is not known"
		}
		if why != "" {
			df.TextWhy = why
		} else {
			df.Text = &text
		}
		return df
	}
	switch {
	case v == "" || v == "(-)":
		return known(false)
	case strings.ToLower(v) == "null":
		switch d {
		case DialectPostgres:
			return known(false)
		case DialectSQLite:
			df := known(true)
			t := "null"
			df.Text = &t
			return df
		}
		return defaultFact{Why: "mysql's handling of DEFAULT null is not mapped"}
	}
	df := known(true)
	df.TextWhy = "a raw default: only its presence is claimed"
	return df
}

// plainText reports a default the dialect quotes verbatim: no quote
// character, backslash or control character.
func plainText(v, quote string) bool {
	if strings.Contains(v, quote) || strings.Contains(v, `\`) {
		return false
	}
	for i := 0; i < len(v); i++ {
		if v[i] < 0x20 || v[i] == 0x7f {
			return false
		}
	}
	return true
}

// numericType reports a column type whose numeric default text the
// catalog keeps as GORM writes it.
func numericType(d, t string) bool {
	switch d {
	case DialectPostgres:
		return t == "smallint" || t == "integer" || t == "bigint" || t == "numeric" || strings.HasPrefix(t, "numeric(")
	case DialectSQLite:
		return t == "integer" || t == "real" || t == "numeric"
	case DialectMySQL:
		base := strings.TrimSuffix(t, " unsigned")
		return base == "tinyint" || base == "smallint" || base == "mediumint" || base == "int" || base == "bigint" || base == "float" || base == "double" || strings.HasPrefix(t, "decimal(")
	}
	return false
}
