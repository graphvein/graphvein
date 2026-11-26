# Claim contract: GORM models

The environment assumptions and claim contract of the GORM model extractor (ADR 0063)
as recorded when it was designed (task T018g5, internal record).
`docs/extract/schema-gorm.md` and the code table in `internal/extract/models/gorm` must
equal them (`TestGormContract`).

## Environment (assumptions, part of the contract)

- **GO-V1 Pins.** The `gorm.io/gorm`, dialector driver and
  `github.com/jinzhu/inflection` versions in the module's `go.mod` are the
  ones that built the binary that migrated the database
  (`pins: {"gorm.io/gorm": "<v>", "gorm.io/driver/<d>": "<v>", ...}`), and
  gorm's DDL code is the same as at the verified pins for every version in
  the accepted range (Revision 1, O5: verified at 1.25.12 and 1.31.2; a
  newer v1 version is read under this assumption, shown as "rules verified
  at gorm 1.25.12-1.31.2"); a dialector driver pin outside the verified
  driver versions (postgres v1.5.11 and v1.6.3, sqlite v1.5.7 and v1.6.0,
  glebarez sqlite v1.11.0, mysql v1.6.0) is read under it too and shown
  with the verified versions.
- **GO-V2 Schema deployed.** The database was created by the migrate calls
  read, on an empty database, and every call succeeded (the application
  started), and no other code changed the schema: a read `Migrator()` DDL
  call or `Exec`/`Raw` with DDL text is marked (the tables it may change
  keep partial sets, the table list may be incomplete),
  while SQL the application builds at run time or a separate migration
  tool runs is outside the records. `AutoMigrate` on existing tables only
  adds columns, indexes and constraints; a database created by older
  versions of the application (gotify has kept one since 2018) can hold
  more.
- **GO-V3 Dialect.** The database is of the group's dialect: the single
  dialector every read `gorm.Open` site uses, or one of several chosen at
  runtime (then the dialect-dependent facts are claimed per dialect)
  (Revision 1 O1).
- **GO-V4 No runtime naming changes.** No code changes the naming
  strategy, `inflection`'s rules or the configuration after `gorm.Open`
  beyond what is read (a read mutation makes the dependent facts unknown).
- **GO-V5 Mapped dialect.** MySQL facts follow gorm.io/driver/mysql as its
  source states at the pinned version, for a MySQL server 5.6 or later
  (`SELECT VERSION()` switches datetime precision off for 5.0-5.5 and
  changes options for MariaDB, `mysql.go:128-161`); never `oracle`.
- **GO-V6 Schema parse order.** No code parses a model's schema (a query,
  `db.Model(...)`, another goroutine) before the migrate calls that the
  order rule reads, so GORM's schema cache holds what the migrate calls
  put there.
- **GO-V7 Build.** The packages compile as read: no build constraint,
  generated file or `vendor` copy changes the struct set or the migrate
  calls.

## Claim contract

Finalised from the inventory and the oracle (the investigation). Row kinds: **K**
scope and naming; **M** models; **F** fields; **T** types; **C** column
facts; **I** indexes; **R** relations; **P** provable failures; **U**
unread tool; **V** assumptions; **D** the single default branch. Every
form the corpora use has exactly one row (counts per row:
the T018g5 inventory (internal record)); forms only the fixture uses come from
the same oracle runs (scratch probes). The contract test keeps
`docs/extract/schema-gorm.md`, the Go table and the code 1:1 (plan, shared
piece 4). **22 rows: 21 rows plus D** (GO-V counted, as SQ-V is in
T018g3; the investigation wrote "21 rows: 20 rows plus D" corrected the
count); GO-M2 and GO-U1 only mark, GO-P1 only refuses, GO-V has no
branch.

Dialect rule (developer decision 2026-11-13):
- **postgres:** `verification: "oracle"`: `AutoMigrate` on a scratch
  Postgres 17, catalog read; rendering from gorm.io/driver/postgres
  v1.6.3 `postgres.go:210-306`, `migrator.go:95-171`, `216-241`.
- **sqlite:** the same oracle on a scratch SQLite file through
  `github.com/glebarez/sqlite` (pure Go, modernc SQLite 3.41; its
  `migrator.go` and `DataTypeOf` are byte-identical to gorm.io/driver/sqlite
  v1.6.0) and, as a cross-check, gorm.io/driver/sqlite v1.6.0
  (cgo, mattn 1.14.32); rendering `sqlite.go:208-236`,
  `migrator.go:233-279`. SQLite's `pragma table_xinfo` reports the
  declared type (upper-cased for `INTEGER`, `TEXT`, `BLOB`, `REAL` by
  SQLite 3.37+): compared case-insensitively.
- **mysql:** `verification: "mapped"` from gorm.io/driver/mysql v1.6.0
  `mysql.go:360-506` (`DataTypeOf`, float `379-389`, string `391-414` with
  `DefaultStringSize` and the 191 rule for keyed or defaulted strings,
  time `416-430` with default precision 3 (`mysql.go:66`), bytes
  `432-442`, int `469-496`, custom `498-506`), `migrator.go:40-48`
  (comments), indexes inline in `CREATE TABLE` with prefix `length`
  (`migrator/migrator.go:251-275`, `808-827`); GO-V5. A `mysql.New`
  dialector with non-literal `DefaultStringSize`,
  `DisableDatetimePrecision` or `DefaultDatetimePrecision`: string and
  time types unknown.
- **runtime (several dialectors):** dialect-independent facts (below),
  and the dialect-dependent facts per dialect, each with its own
  `verification` (`oracle` for postgres and sqlite, `mapped` for mysql;
  Revision 1, O1 and O2) through the T018g0b kit (ADR 0065); a unit
  without that kit leaves them unknown with a reason.
- **Unknown in every dialect:** names the database chooses (Postgres
  `<table>_pkey`, sequences; SQLite `sqlite_autoindex_*`), collations,
  sequence options, index predicate text, column comments.

Dialect-independent facts (claimed for every supported dialect and in a
runtime group): table and column names and the column set, GORM's data
type with size, precision and scale (the unclaimed `tool_type`), primary-key columns
(except the SQLite autoincrement collapse, GO-C1), nullability of columns
that are neither primary key nor autoincrement, default presence and the
text of numeric literal defaults, index names, columns, uniqueness and
partiality, unique-constraint names and columns, foreign-key columns,
target, referenced columns and actions, and the names GORM writes
(`idx_`, `uni_`, `fk_`, explicit names) up to 63 bytes. Dialect-dependent
facts (the SQL `type` with its `length`, `precision` and `scale`,
nullability of primary-key and autoincrement columns,
autoincrement defaults, string and boolean default text, identifiers over
63 bytes): claimed in a single-dialect group, and per dialect in a
runtime group (Revision 1, O1).

| id | form | claim | refusal / unknown | assumptions |
|---|---|---|---|---|
| GO-K1 | scope: module `go.mod` requiring `gorm.io/gorm` v1 in the accepted range (O5); pins from its `require` lines; non-test files of the module's own packages | the group (`models/gorm/<module dir>`), `pins` | a v1 version older than 1.25.12 or another major: marker, no claims; a v1 version newer than 1.31.2: read under GO-V1, shown as "rules verified at gorm 1.25.12-1.31.2" (Revision 1, O5); a model package outside the module (a dependency): not read (marker naming the import); missing pin: `pins` entry `null`; a dialector driver pin outside the verified driver versions (postgres v1.5.11, v1.6.3; sqlite v1.5.7, v1.6.0; glebarez sqlite v1.11.0; mysql v1.6.0) or a driver not pinned: read under GO-V1, shown with the verified versions; a postgres driver older than v1.5.6 (its dialector does not set the 63-byte limit): generated index, unique and foreign-key names over 63 bytes unknown (Revision 2, A7) | GO-V1, GO-V7 |
| GO-K2 | `gorm.Open(<postgres/sqlite/glebarez sqlite/mysql dialector>, <config literal or once-defined local>)` sites, also with a `gorm.Dialector` local assigned dialectors as the dialector (one site per assigned dialector, a runtime group when they differ; Revision 2), or `gorm.Open(<dialector>)` with no configuration argument (GORM's zero `Config`: default naming, foreign keys on, `gorm.go:138-179`; Revision 1 addendum 2, P1); read config keys `NamingStrategy` (literal), `DisableForeignKeyConstraintWhenMigrating`, `IgnoreRelationshipsWhenMigrating`; `mysql.New(mysql.Config{...})` literal DDL keys | the group's `dialect` (single dialector) or runtime; the naming strategy; foreign keys on/off; join tables and auto-added targets on/off | other dialectors, a non-call dialector or a dialector local with another value: group not claimed (marker); sites disagreeing on a read key, a non-literal config, `NameReplacer`, a mutation of the config or `db.NamingStrategy` after `Open`: the facts that key decides unknown | GO-V3, GO-V4 |
| GO-K3 | migrate sites (Discovery): `AutoMigrate`, `Migrator().AutoMigrate`, `Migrator().CreateTable` on a proven `*gorm.DB` through the allowed chain (Revision 2, A4: a module function counts only when every return proves a handle through the allowed chain, a `*gorm.DB` parameter of a package function only when every call of it in the module passes one, a field only when every literal and assignment of it in a gated file does, a local only when every assignment of it does); arguments `new(T)`, `&T{...}`, `T{...}`; order by the statement rule; `AutoMigrate`'s `ReorderModels` auto-add (belongs-to constraint targets, join tables and, through them, many2many targets; `migrator.go:919-1020`); `CreateTable` adds nothing (`ReorderModels(values, false)`) | the model set (tables to claim), the migrate order, `table_list` complete only when every read site is fully read | an unproven receiver, another chain method, another argument form: D (marker), `table_list` `may_be_incomplete`; a method's parameter, a call of the function from a file outside gate G1, a return, call, literal or assignment passing a handle that is not proven (`Table(...)`, `Scopes(...)`, ...): the receiver is not proven (D, Revision 2, A4); an argument naming a type declared inside the function: D (A6); a `Migrator()` DDL call (`DropColumn`, `RenameColumn`, `AlterColumn`, `DropIndex`, `RenameIndex`, `DropTable`, `RenameTable`, `CreateIndex`, `DropConstraint`, ...): the column and constraint sets of the table it names (of every table when its model is not proven) partial, the index lists unknown for an index, constraint or table method, the column it names (every column for a table method or an unnamed column) unknown, marker (A3); `Exec`/`Raw` with literal DDL text (a statement starting with CREATE, ALTER, DROP, RENAME): marker, `table_list` `may_be_incomplete`, foreign keys unknown (A3); a statement `ALTER TABLE`, `DROP TABLE`, `RENAME TABLE` or `CREATE TABLE` (with `OR REPLACE`, `GLOBAL`, `LOCAL`, `TEMP`, `TEMPORARY`, `UNLOGGED`, `FOREIGN`, `IF NOT EXISTS`) naming a table (plain, quoted or qualified identifier, compared without case; a CREATE TABLE whatever its order beside the migrate calls): that table keeps only its table and column names (column and constraint sets partial; schema, primary key, index and constraint lists and every column unknown, with a reason naming the statement); another `ALTER`, `DROP` or `RENAME`, or a `CREATE TABLE` whose target table is not read: every table of the group keeps only its names; `CREATE [UNIQUE] INDEX ... ON <table>`: that table's index and constraint lists unknown (every table's when the table is not read); another `CREATE` (a view, a trigger, a function, ...): only the marker and the foreign keys (Revision 2 addendum and its part 2); sites whose order is not proven: GO-R1 child foreign keys and GO-R3 facts unknown; `IgnoreRelationshipsWhenMigrating`: no auto-add, no join tables | GO-V2, GO-V6 |
| GO-K4 | naming: table name from a `TableName()` method on `T` or `*T` whose body is one `return e`, `e` a string literal, a package-level `const` string of the module's own packages or a `+` concatenation of those (Revision 1, O7; Go constant evaluation; used verbatim, no prefix: `schema.go:185-193`), else `TablePrefix + inflection.Plural(toDBName(name))` or `TablePrefix + toDBName(name)` when `SingularTable` (`naming.go:44-49`, `131-188`, initialisms `119`); column `toDBName(field)` (`naming.go:62-64`); join table `JoinTableName` (`naming.go:67-76`: kept when already lower case); index `idx_<table>_<toDBName(field or composite)>`, unique `uni_<table>_<column>`, foreign key `fk_<table>_<toDBName(relation)>` through `formatName` with the identifier limit (`naming.go:98-115`; 64 by default, `gorm.go:179`; 63 under the postgres dialector, `postgres.go:39`, `53-72`) and its sha1 suffix; `NoLowerCase` keeps names | table `name`, column names, join-table, index and constraint names (`Person` -> `people`, `Human` -> `humen`, `Leaf` -> `leafs`, `HTTPLog` -> `http_logs`, `OIDCID` -> `o_id_c_id`) | a `TableName` with another body or `TableName(schema.Namer)`: table name unknown (table not claimed); a non-ASCII identifier: the names derived from it unknown; any `inflection.Add*` call in the module: plural-derived names unknown; a table or column name over 63 bytes in a postgres or runtime group: unknown (Postgres truncates) | GO-V1, GO-V4 |
| GO-M1 | a named struct type of a module package that is a migrate-site argument or auto-added by GO-K3 | a table (identity `table:<file>#<Struct>`) with its column set | the struct's file outside the payload (O3 fallback only): not claimed (marker) | GO-V2 |
| GO-M2 | a struct with `gorm:` tags or a `gorm.Model` embedding that GO-M1 does not reach | none: listed as "model, not proven migrated" (marker diagnostic naming the struct) | - | - |
| GO-F1 | field selection (`schema.go:220-269`, `field.go:343-445`): exported fields in order; skipped: unexported, `-`, `-:all`, `-:migration`, relation fields; kept: `->`, `<-`, `-:` other values; embedding: an anonymous module struct or `gorm.Model` (ID primary key, `CreatedAt`, `UpdatedAt`, `DeletedAt` with `index`), or `embedded`; `embeddedPrefix`; an embedded primary key stays one only with its own `primaryKey` tag (`field.go:423-434`); keys with no table fact: `comment`, `autoCreateTime`, `autoUpdateTime`, `serializer` encoding, `->`, `<-` | the column set and order | duplicate column names (`schema.go:236-258`): those columns unknown; an embedding field with other tags (they are copied into every embedded field's settings, `field.go:437-439`): the embedded columns' facts unknown; an anonymous non-struct field: P1 | GO-V2 |
| GO-F2 | column name: `column:` tag value verbatim (case kept, not trimmed), else GO-K4; `embeddedPrefix` prepended (`field.go:419-421`) | column `name` | the GO-K4 limits (non-ASCII, over 63 bytes in a postgres or runtime group); a duplicate name: GO-F1 | GO-V1 |
| GO-T1 | GORM data type (`field.go:139-341`): `bool`; `int*`, `uint*` with bit size; `float32/64`; `string`; `time.Time` (and `sql.NullTime`, `gorm.DeletedAt`); `[]byte`; pointers of these; `sql.NullString`, `NullInt64`, `NullInt32`, `NullInt16`, `NullByte`, `NullFloat64`, `NullBool` (the first field's type, `field.go:139-185`); module named types whose underlying type is one of these and that have no `Value`, `Scan`, `GormDataType` or `GormDBDataType` method; `serializer:`/`json` tags -> `string`; `size`, `precision`, `scale` tags; a `type:` tag: `bool`, `int`, `uint`, `float`, `string`, `time`, `bytes` in any case -> that type (keeping the kind's size: `type:Int` on a string is a 0-size int), anything else -> the raw text (`field.go:320-328`); a foreign-key field takes the referenced field's data type and size when its own size is 0 (`relationship.go:592-604`) | column `length`, `precision`, `scale`: the modifiers of the SQL type GO-T2 claims (per dialect in a runtime group); GORM's data type with its size, precision and scale is the unclaimed `tool_type` | another type (an external package type such as `uuid.UUID`, `decimal.Decimal`, `datatypes.JSON`; a module type with those methods; interfaces; a map or non-byte slice without `serializer`/`type`: P1): the column unknown and the table's column set not complete (the oracle shows a custom type whose DDL fails) | GO-V1 |
| GO-T2 | SQL type per dialect (single-dialect groups; per dialect in a runtime group, Revision 1 O1): postgres `boolean`, `smallint`/`integer`/`bigint` by size (+1 for unsigned, `postgres.go:271-285`), `numeric` (floats without precision: `decimal`), `numeric(p,s)`, `character varying(n)` for 0 < size <= 10485760 else `text`, `timestamp(p) with time zone`, `bytea`, raw `type:` texts from a closed list (`varchar(n)`, `text`, `uuid`, `timestamp`, `timestamptz`, `text[]`, `json`, `jsonb`, `date`, `boolean`, `bigint`, `integer`, `smallint`, `bytea`); sqlite the declared text `numeric`, `integer`, `real`, `text` (size ignored), `datetime` (or the `type:` text for time), `blob`, raw texts verbatim; mysql mapped (dialect rule) | column `type`: the SQL type (per dialect in a runtime group) | runtime group: per dialect (O1), `null` with reason without the T018g0b kit; a raw postgres text outside the list: `null`; `generated:` tag (postgres driver 1.6+): the column unknown; a raw `type:` text that is not one type name with optional `(n[,m])` or `[]` (`varchar(100) NOT NULL`, `int(11) NOT NULL AUTO_INCREMENT`): unknown on every dialect (Revision 2, A1), the column's foreign keys unknown and the table's constraint set partial (a `PRIMARY KEY` in it: the key unknown) | GO-V1, GO-V3, GO-V5 |
| GO-C1 | primary key: `primaryKey`/`primary_key` truthy (`utils.CheckTruth`: not empty, not `false`), else the field whose column or name is `id`/`ID` when no field is one (`schema.go:271-302`); autoincrement: `autoIncrement` truthy, or the prioritized single primary key of int/uint type without an `autoIncrement` tag (`schema.go:337-349`); postgres `serial`/`bigserial` (`postgres.go:233-244`); sqlite `integer PRIMARY KEY AUTOINCREMENT` inline (`sqlite.go:212-219`) | `primary_key` columns (in field order), autoincrement | sqlite: an autoincrement field in a composite key makes that field the only primary key (claimed as the port gives it on a sqlite group, and for sqlite per dialect in a runtime group, O1); two autoincrement fields on sqlite: P1 | GO-V2 |
| GO-C2 | nullability: `not null`/`notnull` truthy -> `NOT NULL` (`migrator.go:94-96`); postgres primary-key and serial columns not null; sqlite only the tag (an `INTEGER PRIMARY KEY` is nullable in the catalog); mysql mapped (tag, primary key, `datetime ... NULL` for nullable times) | `nullable` | runtime group: primary-key and autoincrement columns per dialect (O1); a raw `type:` text that is not one type name with optional `(n[,m])` or `[]` (`varchar(100) NOT NULL`, `int(11) NOT NULL AUTO_INCREMENT`): unknown on every dialect (Revision 2, A1); a raw serial type (`serial`, `bigserial`, `smallserial`): nullability and autoincrement unknown | GO-V2 |
| GO-C3 | default (`field.go:210-282`, `migrator.go:98-106`): a literal the field's kind parses (int, uint, float, bool; strings with `'`/`"` stripped) -> `DEFAULT <bound literal>`; text with `(` and `)`, or a time kind -> raw text; `null`; `(-)` and empty -> none | default present or absent; literal text: postgres `7`, `1.5`, `true`, `'x'::text`; sqlite `7`, `"x"`, `true`, `null`; numeric literal text in every dialect; raw text: presence only | time-kind defaults (`now.Parse` not ported): unknown; a literal the kind cannot parse: P1; runtime group: string and boolean text per dialect (O1; presence claimed); postgres serial default: present (text not claimed); a raw `type:` text that is not one type name with optional `(n[,m])` or `[]` (`varchar(100) NOT NULL`, `int(11) NOT NULL AUTO_INCREMENT`): unknown on every dialect (Revision 2, A1); a raw serial type: unknown | GO-V2 |
| GO-C4 | `unique` truthy -> `CONSTRAINT uni_<table>_<column> UNIQUE` in `CREATE TABLE` (`constraint.go:57-66`, `migrator.go:292-295`) | a unique constraint (name on postgres and mysql; on sqlite the column set only) and `unique: true` | name over the identifier limit: GO-K4; a raw `type:` text that is not one type name with optional `(n[,m])` or `[]` (`varchar(100) NOT NULL`, `int(11) NOT NULL AUTO_INCREMENT`): unknown on every dialect (Revision 2, A1) | GO-V2 |
| GO-I1 | `index` / `uniqueIndex` tags (`index.go:30-167`): name (explicit or GO-K4), `unique` or `uniqueIndex` -> unique, fields of one name merged and ordered by `priority` (default 10; stable for up to 12 fields), `sort:desc`, `where` (partial), `composite`, `length` (mysql prefix only; postgres and sqlite ignore it, `postgres migrator.go:95-112`), `type` (postgres `USING`), `class`, `option`, `collate`, `comment` | index name, columns in order, unique, partial, descending flags; created after the table on postgres (`IF NOT EXISTS`) and sqlite, inline on mysql | `expression:`: index claimed, its columns unknown; a name used by another table of the group: postgres skips it silently (`CREATE INDEX IF NOT EXISTS`, `migrator.go:149`): unknown; sqlite: P1; `type` or `class` on sqlite: P1; mysql `FULLTEXT`/`SPATIAL`: mapped; predicate text: unknown | GO-V2 |
| GO-R1 | belongs-to, has-one, has-many through `guessRelation` (`relationship.go:456-622`): a struct field tries has-one before belongs-to (`guessGuess`), a slice field has; default foreign key `<Field><PK>` (belongs-to) or `<Schema><PK>` (has), also `...ID`, `...Id` and the column name; `foreignKey`, `references`, `belongsTo` tags | the foreign-key column pairing and its type copy (GO-T1); has-relations register on the child (`relationship.go:101-106`) only when the parent's schema is parsed before the child's table is created: same `AutoMigrate` call (all values are parsed before any table is created), an earlier ordered call, or the parse closure of either (every relation field's struct is parsed) | `polymorphic`: D (marker; the target's foreign keys unknown); a guess that fails: P1; order not proven: has-relation foreign keys of the child unknown | GO-V6 |
| GO-R2 | `many2many:<name>` join table (`relationship.go:271-444`): table `JoinTableName(name)`, columns `toDBName(<Schema><PK>)` and `toDBName(<Target><PK>)` (self join: `Singular(field)+PK`, or `...Reference`), `joinForeignKey`/`joinReferences`, types copied from the keys, every column a primary-key member, belongs-to constraints `fk_<join>_<schema name>` carrying the many2many field's `constraint` tag | the join table (identity `table:<file>#<Struct>.<Field>`), its columns, types, foreign keys | the same join table declared from both sides (realworld `article_tags`): primary-key order unknown (open point O8); `IgnoreRelationshipsWhenMigrating`: no join table; a `SetupJoinTable` call naming the relation: its join table not claimed (D, marker), the custom join model not GO-M2; a `SetupJoinTable` call that is not read: every join table not claimed (Revision 2, A2) | GO-V2, GO-V6 |
| GO-R3 | foreign-key constraints (`relationship.go:661-726`, `migrator.go:168-180`, `277-290`): for every relation of the table whose constraint lives on it; name `fk_<rel.Schema.Table>_<toDBName(relation)>` or the `constraint:<name>,...` prefix; `OnDelete`, `OnUpdate` from the relation field's `constraint` tag (a has-relation's tag on the parent sets the child's actions), else `NO ACTION`; a belongs-to whose target has the reverse has-relation over the same columns yields to it (`relationship.go:667-685`); an existing table gains missing constraints on a later `AutoMigrate` of it | `foreign_keys` with name, columns, target, referenced columns, actions; `foreign_keys: []` proven when the config disables them (`DisableForeignKeyConstraintWhenMigrating`, gotify) or every relation is read | `constraint:-`: none; config unknown: foreign keys unknown; postgres `CreateTable` (not `AutoMigrate`) referencing a table not yet created: P1 | GO-V2, GO-V6 |
| GO-P1 | forms that make a migrate call fail on the dialect: a relation field GORM cannot parse (non-struct slice or map without `type`/`serializer`), a default the kind cannot parse, sqlite: index `type`/`class`, a duplicate index name, two autoincrement fields, a raw function default not wrapped in parentheses (`default:datetime('now')`); postgres: `CreateTable` with a foreign key to a missing table, an index `class` other than `UNIQUE` (Revision 2, A5, oracle-confirmed) | none for the failing model and every later model of the same call on that dialect (the call returns at the first error, `migrator.go:122-206`) | - | GO-V2 |
| GO-U1 | `github.com/jinzhu/gorm` (v1) import in a read file | marker (`unread_tools`: "found, not read") | - | - |
| GO-V | assumptions GO-V1-GO-V7 (Environment above) | stated on the records | - | - |
| D | anything else | nothing; marker diagnostic `schema_model_unread` with `tool: "gorm"` | - | - |
