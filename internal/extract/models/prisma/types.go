package prisma

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Providers of the datasource block (row PR-B1). postgresql facts are
// checked by the oracle; mysql, sqlserver and cockroachdb facts that depend
// on the provider are mapped from Prisma's connector source at the locked
// version (developer decision 2026-11-09, assumption PR-V6); sqlite's
// mapping is checked by the SQLite oracle (the SQL of Prisma
// 5.22.0, 6.19.3 and 7.10.0 applied to a SQLite file); mongodb has no
// tables.
const (
	providerPostgres  = "postgresql"
	providerMySQL     = "mysql"
	providerSQLite    = "sqlite"
	providerSQLServer = "sqlserver"
	providerCockroach = "cockroachdb"
	providerMongo     = "mongodb"
)

// sqlProviders are the SQL providers the contract lists (PR-B1 forms).
var sqlProviders = []string{providerPostgres, providerMySQL, providerSQLite, providerSQLServer, providerCockroach}

// mappedProvider says whether a provider's provider-dependent facts are
// mapped (verification "mapped", PR-V6).
func mappedProvider(p string) bool {
	return p == providerMySQL || p == providerSQLServer || p == providerCockroach
}

// oracleProvider says whether a provider's provider-dependent facts are
// checked by the oracle (verification "oracle").
func oracleProvider(p string) bool {
	return p == providerPostgres || p == providerSQLite
}

// scalarTypes are the scalar types of row PR-F1.
var scalarTypes = []string{"String", "Int", "BigInt", "Float", "Decimal", "DateTime", "Boolean", "Json", "Bytes"}

// postgresScalar is the Postgres type of each scalar type without a native
// type attribute (row PR-F1), in the catalog's format_type spelling: the
// connector's defaults (psl-core builtin_connectors/
// postgres_datamodel_connector.rs:85-95), checked by the oracle (the investigation).
var postgresScalar = map[string]string{
	"String": "text", "Int": "integer", "BigInt": "bigint", "Float": "double precision",
	"Decimal": "numeric(65,30)", "DateTime": "timestamp(3) without time zone", "Boolean": "boolean",
	"Json": "jsonb", "Bytes": "bytea",
}

// sqliteScalar is the declared column type Prisma renders on sqlite for
// each scalar type (row PR-F1), as the SQLite oracle reads it (pragma
// table_info): the same at 5.22.0, 6.19.3 and 7.10.0; Json only from
// 6.2.0 (sqliteJSONEnums).
var sqliteScalar = map[string]string{
	"String": "TEXT", "Int": "INTEGER", "BigInt": "BIGINT", "Float": "REAL",
	"Decimal": "DECIMAL", "DateTime": "DATETIME", "Boolean": "BOOLEAN",
	"Json": "JSONB", "Bytes": "BLOB",
}

// sqliteJSONEnums is why Json fields and enums are not read on sqlite
// before Prisma 6.2.0, which rejects them there (the oracle: 6.1.0 and
// 5.22.0 refuse both, 6.2.0 renders them as 7.10.0 does).
const sqliteJSONEnums = "Json fields and enums are supported on sqlite from Prisma 6.2.0; the locked version is not proven to be one"

// mappedScalar is the column type Prisma renders for each scalar type
// without a native type attribute on the mapped providers: the
// connector's SCALAR_TYPE_DEFAULTS rendered by the schema engine's
// renderer, as DDL text (prisma-engines 0edf323efd1d; cited per provider
// in docs/extract/schema-prisma.md "Provider mapping").
var mappedScalar = map[string]map[string]string{
	// mysql_datamodel_connector.rs:75-86 (DATE_TIME_DEFAULT DateTime(3));
	// flavour/mysql/renderer.rs:426-459 (a Boolean column's TinyInt
	// renders BOOLEAN, :429).
	providerMySQL: {
		"String": "VARCHAR(191)", "Int": "INTEGER", "BigInt": "BIGINT", "Float": "DOUBLE",
		"Decimal": "DECIMAL(65, 30)", "DateTime": "DATETIME(3)", "Boolean": "BOOLEAN",
		"Json": "JSON", "Bytes": "LONGBLOB",
	},
	// mssql_datamodel_connector.rs:64-80; flavour/mssql/renderer.rs:525-555.
	providerSQLServer: {
		"String": "NVARCHAR(1000)", "Int": "INT", "BigInt": "BIGINT", "Float": "FLOAT(53)",
		"Decimal": "DECIMAL(32,16)", "DateTime": "DATETIME2", "Boolean": "BIT",
		"Json": "NVARCHAR(1000)", "Bytes": "VARBINARY(max)",
	},
	// cockroach_datamodel_connector.rs:71-83; flavour/postgres/renderer.rs:620-659.
	providerCockroach: {
		"String": "STRING", "Int": "INT4", "BigInt": "INT8", "Float": "FLOAT8",
		"Decimal": "DECIMAL(65,30)", "DateTime": "TIMESTAMP(3)", "Boolean": "BOOL",
		"Json": "JSONB", "Bytes": "BYTES",
	},
}

// nativeType is one postgresql native type of row PR-A6: the scalar type
// it applies to (psl-core postgres_datamodel_connector/native_types.rs),
// how many integer arguments it takes (min, max) and how its catalog type
// is spelled.
type nativeType struct {
	scalar   string
	min, max int
	render   func(args []int) string
}

func fixed(name string) func([]int) string { return func([]int) string { return name } }

// withLength renders name(n), or bare when there is no argument; suffix is
// appended after the argument (" without time zone").
func withArg(name, suffix string) func([]int) string {
	return func(args []int) string {
		if len(args) == 0 {
			return name + suffix
		}
		return fmt.Sprintf("%s(%d)%s", name, args[0], suffix)
	}
}

// postgresNative are the native types of row PR-A6 with the Postgres
// catalog type each renders (flavour/postgres/renderer.rs:580-610 renders
// the DDL; the catalog spells it as format_type does). A bare
// `@db.Bit` is Postgres's bit(1).
var postgresNative = map[string]nativeType{
	"VarChar":         {"String", 0, 1, withArg("character varying", "")},
	"Char":            {"String", 0, 1, withArg("character", "")},
	"Text":            {"String", 0, 0, fixed("text")},
	"Uuid":            {"String", 0, 0, fixed("uuid")},
	"SmallInt":        {"Int", 0, 0, fixed("smallint")},
	"Integer":         {"Int", 0, 0, fixed("integer")},
	"BigInt":          {"BigInt", 0, 0, fixed("bigint")},
	"Real":            {"Float", 0, 0, fixed("real")},
	"DoublePrecision": {"Float", 0, 0, fixed("double precision")},
	"Decimal": {"Decimal", 0, 2, func(args []int) string {
		if len(args) == 0 {
			return "numeric"
		}
		return fmt.Sprintf("numeric(%d,%d)", args[0], args[1])
	}},
	"Timestamp":   {"DateTime", 0, 1, withArg("timestamp", " without time zone")},
	"Timestamptz": {"DateTime", 0, 1, withArg("timestamp", " with time zone")},
	"Date":        {"DateTime", 0, 0, fixed("date")},
	"Time":        {"DateTime", 0, 1, withArg("time", " without time zone")},
	"Timetz":      {"DateTime", 0, 1, withArg("time", " with time zone")},
	"Boolean":     {"Boolean", 0, 0, fixed("boolean")},
	"Json":        {"Json", 0, 0, fixed("json")},
	"JsonB":       {"Json", 0, 0, fixed("jsonb")},
	"ByteA":       {"Bytes", 0, 0, fixed("bytea")},
	"Inet":        {"String", 0, 0, fixed("inet")},
	"Xml":         {"String", 0, 0, fixed("xml")},
	"Bit": {"String", 0, 1, func(args []int) string {
		if len(args) == 0 {
			return "bit(1)"
		}
		return fmt.Sprintf("bit(%d)", args[0])
	}},
	"VarBit": {"String", 0, 1, withArg("bit varying", "")},
	"Oid":    {"Int", 0, 0, fixed("oid")},
	"Citext": {"String", 0, 0, fixed("citext")},
	"Money":  {"Decimal", 0, 0, fixed("money")},
}

// Decimal takes no argument or exactly two.
func nativeArgCountOK(name string, n int) bool {
	nt := postgresNative[name]
	if name == "Decimal" {
		return n == 0 || n == 2
	}
	return n >= nt.min && n <= nt.max
}

var (
	pgLength    = regexp.MustCompile(`^(character varying|character|bit varying|bit)\((\d+)\)$`)
	pgNumeric   = regexp.MustCompile(`^numeric\((\d+),(\d+)\)$`)
	pgTimeTyped = regexp.MustCompile(`^(timestamp|time)(?:\((\d+)\))? (with|without) time zone$`)
)

// postgresSizes reads the length, precision, scale and timezone facts of a
// Postgres catalog type (element type of an array); a fact the type has
// not is absent from the map.
func postgresSizes(sqlType string) map[string]any {
	t := strings.TrimSuffix(sqlType, "[]")
	out := map[string]any{}
	if m := pgLength.FindStringSubmatch(t); m != nil {
		n, _ := strconv.Atoi(m[2])
		out["length"] = n
	}
	if m := pgNumeric.FindStringSubmatch(t); m != nil {
		p, _ := strconv.Atoi(m[1])
		s, _ := strconv.Atoi(m[2])
		out["precision"], out["scale"] = p, s
	}
	if m := pgTimeTyped.FindStringSubmatch(t); m != nil {
		if m[2] != "" {
			p, _ := strconv.Atoi(m[2])
			out["precision"] = p
		}
		out["timezone"] = m[3] == "with"
	}
	return out
}

// autoincrementText is the database's default text of an autoincrement()
// column per provider (row PR-A3 (c)), only where Prisma renders a DEFAULT
// expression: CockroachDB's unique_rowid()
// (flavour/postgres/schema_calculator.rs:82-84, renderer.rs:887). The
// others have a server-generated value without a DEFAULT text Prisma
// states (autoincrementWhy).
var autoincrementText = map[string]string{
	providerCockroach: "unique_rowid()",
}

// autoincrementWhy says why Postgres's autoincrement() text is not
// claimed: its serial types create a sequence default whose name Prisma
// does not state (flavour/postgres/renderer.rs:586-590).
var autoincrementWhy = map[string]string{
	providerPostgres: "a serial column's default is nextval() of a sequence whose name Prisma does not state; only its presence is claimed",
}

// autoincrementProperty says why autoincrement() has no server default
// claim on the providers that render it as a column property, not a
// DEFAULT (decision 2026-11-09: never server_default true,
// and the record has no autoincrement attribute): MySQL's AUTO_INCREMENT
// (flavour/mysql/renderer.rs:41, :389-390), SQL Server's IDENTITY(1,1)
// (flavour/mssql/renderer.rs:49-50) and SQLite's AUTOINCREMENT (the
// oracle: an Int id is INTEGER PRIMARY KEY AUTOINCREMENT, a BigInt id
// BIGINT PRIMARY KEY with no generated value at all).
var autoincrementProperty = map[string]string{
	providerMySQL:     "autoincrement() renders AUTO_INCREMENT, a column property and not a DEFAULT; the record has no attribute for it, so the default is not claimed",
	providerSQLServer: "autoincrement() renders IDENTITY(1,1), a column property and not a DEFAULT; the record has no attribute for it, so the default is not claimed",
	providerSQLite:    "on sqlite autoincrement() renders AUTOINCREMENT on an Int id (a column property, not a DEFAULT) and nothing on a BigInt id; the default is not claimed",
}

// sqliteRowid is why an Int @id has no default claim on sqlite: Prisma
// renders every single-field Int id as INTEGER PRIMARY KEY AUTOINCREMENT,
// with or without @default(autoincrement()) (the oracle), so the database
// fills it without a DEFAULT.
const sqliteRowid = "on sqlite an Int @id renders INTEGER PRIMARY KEY AUTOINCREMENT, which the database fills without a DEFAULT (a column property); the default is not claimed"

// nowText is the server default of now() per provider (row PR-A3 (b)):
// CURRENT_TIMESTAMP (flavour/postgres/renderer.rs:885,
// flavour/mssql/renderer.rs:574), on MySQL with the column's timestamp
// precision (flavour/mysql/renderer.rs:514-518; DATETIME(3) for a DateTime
// without a native type).
var nowText = map[string]string{
	providerPostgres:  "CURRENT_TIMESTAMP",
	providerSQLite:    "CURRENT_TIMESTAMP",
	providerMySQL:     "CURRENT_TIMESTAMP(3)",
	providerSQLServer: "CURRENT_TIMESTAMP",
	providerCockroach: "CURRENT_TIMESTAMP",
}
