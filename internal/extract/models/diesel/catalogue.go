package diesel

import (
	"slices"

	"github.com/graphvein/graphvein/internal/extract/models"
)

// The catalogue of the Diesel claim contract for the schema API's
// /api/v0/schema/assumptions (T018f, ADR 0055 option 6A, registered as the
// other model front ends do; Revision 1 addendum 3): the environment
// assumptions DI-V1 to DI-V9 and the rows of docs/extract/schema-diesel.md
// "Claim contract" with their claim branch and claim cell as written there
// (TestDieselCatalogue checks both against the document).

var environmentAssumptions = []models.Assumption{
	{ID: "DI-V1", Title: "Pins", Text: "The `diesel` version in the workspace's `Cargo.lock` is the one the code is built with (`pins: {\"diesel\": \"<v>\"}`); a missing or ambiguous pin gives `pins: null` with DI-V1 still listed."},
	{ID: "DI-V2", Title: "Generated file is current", Text: "A generated schema file is print-schema output from the deployed database, with only the configured `patch_file` applied afterwards, regenerated after the latest migration and not edited by hand outside the patch."},
	{ID: "DI-V3", Title: "No domains", Text: "No column's type is a domain (print-schema writes a domain's base type unless `pg_domains_as_custom_types` matches it; `pg.rs:136-147`)."},
	{ID: "DI-V4", Title: "Dialect", Text: "The database is of the dialect the workspace's Diesel backend features name, the backends the known companion crates enable included."},
	{ID: "DI-V5", Title: "Generation settings", Text: "The schema file was generated with the `[print_schema]` section that names it as it stands (its `schema`, `filter`, `import_types`, `pg_domains_as_custom_types`, `sqlite_integer_primary_key_is_bigint`)."},
	{ID: "DI-V6", Title: "diesel_cli rules", Text: "The diesel_cli that generated the file follows the print-schema rules the claim contract cites, verified at diesel_cli 2.3.14 and 2.4.0. diesel_cli is a separate binary, not in `Cargo.lock`; the `diesel` library pin does not pin it."},
	{ID: "DI-V7", Title: "Unconfigured schema file", Text: "A `schema.rs` that no `diesel.toml` names is not configured by diesel.toml and may be stale."},
	{ID: "DI-V8", Title: "Newer diesel pin", Text: "Rules verified at diesel_cli 2.3.14 and 2.4.0: the `diesel` pin is newer than these versions, and the diesel_cli that generated the file is assumed to write the forms the claim contract reads as they do."},
	{ID: "DI-V9", Title: "Older diesel_cli output", Text: "Rules verified at diesel_cli 2.3.14 and 2.4.0; older diesel_cli output checked for the header, the `pub mod` wrap, `Nullable`, the type idents, `#[sql_name]`, the key list, base tables only, `sql_types` structs, SQLite nullability, the MySQL type rule and the patch (diffy 0.3, whose parser and hunk search are 0.4's) at 2.0.1, 2.1.1 and 2.2.0, and for `#[max_length]` and the `joinable!` filters at 2.1.1 and 2.2.0 (SQLite's `joinable!` set also at 2.0.1): the `diesel` pin is older than 2.3.14 (or not proven), or the file shows no diesel_cli 2.1 or later output (no `#[max_length]` and no database comment on a generated line), and the diesel_cli that generated the file is assumed to write those forms as the checked releases do."},
}

// catalogueRow is a row's claim branch (the function that holds it,
// "none" for a row without a branch) and its claim cell.
type catalogueRow struct {
	Branch, Summary string
}

var catalogueRows = map[string]catalogueRow{
	"DI-K1": {"Discover", "one group per configured file (`models/diesel/<file>`), `dialect`, `pins`, the section's literal `schema`"},
	"DI-G1": {"ReadProvenance", "no fact of its own; selects which rows' generated claims apply; a table, a column or a `sql_types` struct is hand-written when any line from its first attribute (or doc comment) line to its name line is added by a located hunk (Revision 2 A3: a patch that adds `#[max_length]` above a column or rewrites a struct's `postgres_type(name = ...)` makes it hand-written); a generated file is diesel_cli 2.1 or later output only when it shows it: a `#[max_length = <int>]` on a generated column, or a `///` doc comment on a generated table or column that is not diesel_cli 2.0's generated text (2.0.1 writes no `max_length` and reads no database comment, `inference.rs:198-224`; 2.1.1 writes database comments, `pg.rs:74-149`); otherwise DI-C2 and DI-J1 (except on sqlite: Revision 2 addendum 2) take the diesel_cli 2.0 rules and the group lists DI-V9, whatever the pin (Revision 2 addendum)"},
	"DI-T1": {"readTable", "every line: table `name` (`#[sql_name]` else the ident, `r#` stripped), `schema` (written `schema.` prefix, else the section's `schema`, else none: the dialect default); generated: base table (print-schema writes only `BASE TABLE`s), `primary_key` = the key list in order (DB names through each column's `#[sql_name]`), `column_set: complete` unless a patch hunk adds a column line that replaces no removed column of the same name (a patch removal in the block: DI-G1); group `table_list` complete when every table is generated, no `filter` is set, and no patch hunk removes a `table!` block"},
	"DI-C1": {"readColumn", "every line: column `name` (`#[sql_name]` else the ident, `r#` stripped); generated: nullability (outermost `Nullable<...>`: nullable; otherwise not null; `pg.rs:69`, `infer_schema_internals/data_structures.rs:137-150`), and the type per DI-C2; every line also carries the declared Diesel type (`Nullable<Int4>`) as a Rust-side annotation, not a database fact (no `claims` entry, Revision 1 O5; attribute `tool_type`, ADR 0065)"},
	"DI-C2": {"columnType", "Postgres `type` in `format_type` spelling: `integer`, `bigint`, `smallint`, `real`, `double precision`, `boolean`, `text`, `character varying`, `character`, `\"char\"`, `bytea`, `date`, `time without time zone`, `timestamp without time zone`, `timestamp with time zone`, `interval`, `bit`, `bit varying`, ... ; `#[max_length = n]` on `varchar`, `bpchar`, `bit`, `varbit` adds `(n)`; `T[]` for arrays; a `sql_types` struct: its `postgres_type` name (a builtin name through the same spelling map, else as written, `schema.name` with its `schema` unless that is `pg_catalog`, which print-schema writes and `format_type` never does: Revision 1 addendum 3), as the plain type when the struct is `#[diesel(enum_type)]` (an enum takes no type modifiers) or its schema is `pg_catalog` (every `pg_catalog` type that takes a modifier is in the spelling map), else as its family with the generic `modifiers` unknown (an extension type such as PostGIS `geometry` or pgvector `vector` loses its modifiers in schema.rs: Revision 2 A2b); the spelling map also holds `timetz` (`time with time zone`, a family with `precision` unknown), which print-schema writes only as a `pg_catalog` struct (Revision 2 A2a); `Numeric`, `Time`, `Timestamp`, `Timestamptz`, `Interval` and any other type whose modifiers print-schema drops (also `Bpchar` and `Bit` without `#[max_length]`, and `Array<T>` of `Varchar`, `Bpchar`, `Bit`, `Varbit`: information_schema has no length for an array column; in a file that shows no diesel_cli 2.1 or later output (DI-G1), whatever the pin, `Varchar` and `Varbit` without `#[max_length]` too, since diesel_cli 2.0 never writes it: Revision 2 A1 and addendum): the type family with the dropped modifiers unknown (Revision 1 O4, the kit's `type_family` of ADR 0065; `interval`'s fields have no kit modifier and are not stated); mysql and sqlite per the dialect rule (`mapped`)"},
	"DI-S1": {"structType", "the type name used by DI-C2 (an `enum_type` struct: an enum, the plain type); a `rust_types` module's `enum` items: no definition and no marker (Revision 2 A4)"},
	"DI-J1": {"readJoinable", "generated: a foreign key from the child's column to the parent's single primary-key column (print-schema emits one only for such a constraint, `foreign_keys.rs:8-71`); `on_delete`/`on_update` unknown; constraint name unknown"},
	"DI-J2": {"readItems", "nothing (Rust-level: which tables may share a query)"},
	"DI-P1": {"ReadFile", "diagnostic `schema_model_import_failure` (row named); no claim for the table involved; on duplicate table names, for both"},
	"DI-U1": {"Discover", "`unread_tools` entry (found, not read yet), count of migration directories"},
	"DI-U2": {"readGroup", "its own group (`models/diesel/<file>`), names only: table and column names as DI-T1 and DI-C1 read them on hand-written lines, `pins`, `dialect`"},
	"DI-V":  {"none", "stated, never claimed as facts"},
	"D":     {"unread", "nothing; the table or column is partial with `D` as reason, or a marker diagnostic `schema_model_unread` (tool `diesel`) for a whole unread definition in a configured file"},
}

// Catalogue returns the Diesel contract's catalogue: rows in the docs
// table's order, each with the assumptions of its row (Contract).
func Catalogue() models.Catalogue {
	c := models.Catalogue{Tool: Tool, Assumptions: slices.Clone(environmentAssumptions)}
	for _, r := range Contract {
		e := catalogueRows[r.ID]
		// A row without assumptions lists none ([]), never null.
		c.Rows = append(c.Rows, models.ContractEntry{ID: r.ID, Branch: e.Branch, Summary: e.Summary, Assumptions: append([]string{}, r.Assumptions...)})
	}
	return c
}

func init() {
	models.Register(Catalogue())
}
