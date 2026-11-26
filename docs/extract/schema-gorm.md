# Schema from GORM models

ADR 0063 (T018g5, `docs/contract/claims/gorm-models.md`; inventory and oracle
baseline in the T018g5 task record (internal)). The GORM front end
`internal/extract/models/gorm` reads each Go module that requires
`gorm.io/gorm` (GORM v2), its `gorm.Open` sites and the migrate calls that
create its tables, from the cached Go model payload version 1
(`docs/lang/go-models.md`) and the retained `go.mod` sources, and hands
its tables to the tool-neutral model kit `internal/extract/models` (ADR
0058, `docs/extract/schema-models.md`), which emits the records every ORM
front end shares (`source: "model"`, `tool: "gorm"`, group
`models/gorm/<module dir>`).

**Status (2026-11-15):** discovery (step GB2: `discover.go`,
`handle.go`, `sites.go`, `order.go`; pins from the shared reader
`internal/extract/models/lockfile` (`gomod.go`); module-own import
resolution shared with the Go linker, `internal/lang/golang/modules.go`),
the contract table (`contract.go`), the schema port (step GB3:
`tables.go`, `schema.go`, `render.go`, `naming.go`, `inflection.go`,
`tags.go`; "Claim pass" below), relations, the migrate simulation and the
call failures (step GB4: `relations.go`, `migrate.go`, `foreignkeys.go`;
"Relations and migrate order" below), the catalogue (`catalogue.go`) and
the indexer call (`internal/index/index.go`) and the oracle (step GB5:
`scripts/oracle/gorm_catalog.sh`, `TestGormOracle`; "Oracle" below).
Step GF (Revision 2, independent review) adds the forms of "Revision 2"
below: raw type texts, `SetupJoinTable`, `Migrator()` DDL and DDL text,
handle flows in the receiver proof, the added GO-P1 failures, local types,
driver pins, the dialector local and the discovery guard.

## Discovery

`Discover(Input)` returns the modules, sorted by directory, and its
diagnostics. Each module is read in its own guarded step (Revision 2,
C1): a failure becomes `schema_model_internal_error` on its `go.mod` and
the other modules are read; the indexer's call of `Link` recovers too
(`safeGorm`). `Input` holds the facts of every file (Go files carry their
imports, the call cache and the Go model payload) and the retained
sources.

### Modules and pins (GO-K1)

- A module is a directory with a `go.mod` (the Go linker's
  `golang.ModulePaths`) whose `require` lines list `gorm.io/gorm`, direct
  or `// indirect`. Its group key is `models/gorm/<module dir>` (`.` for
  the repository root).
- `lockfile.ParseGoMod` reads `module`, `require` and `replace` in their
  single-line and block forms; lines it cannot read are skipped (fewer
  pins, never wrong ones).
- `Requires` keeps the require lines of `gorm.io/gorm`, every
  `gorm.io/driver/*`, `github.com/glebarez/sqlite` and
  `github.com/jinzhu/inflection`, in file order. `Pins` maps each of them
  to its version; `gorm.io/gorm` and inflection are always present (`nil`
  when not proven). A `replace` to the same module at another version
  pins that version; a `replace` to another path (a fork, a directory)
  makes the pin `nil`, and for `gorm.io/gorm` refuses the module (the
  code that migrated the database is not the pinned one).
- Version (Revision 1, O5): `VersionVerified` for an exact release from
  v1.25.12 to v1.31.2 (both ends oracle-verified), `VersionAssumed` for
  any later v1 version (a pseudo-version included), read under GO-V1 with
  the note `VersionAssumption` ("rules verified at gorm 1.25.12-1.31.2"),
  `VersionRefused` for an older v1, a v1.25.12 pre-release, another major
  or a missing pin.
- Files read (`Files`): the module's non-test `.go` files (the nearest
  `go.mod` governs a file, so nested modules are their own modules),
  outside `testdata`, `vendor` and directories starting with `.` or `_`
  (the go command does not build them; GO-V7). `_test.go` files are test
  scope (O10).
- Package selection (ADR 0068 known gap, closed in extractors-44): within
  a directory GORM reads exactly the non-test files the Go structure
  contract selects (`golang.Package`, `docs/lang/go-structure.md`), the
  same selection as import edges and call bindings: a file no build
  configuration builds (`//go:build ignore`, a file name for no listed
  port, a file importing `"C"` that excludes `cgo`, `package
  documentation`, a name starting with `_` or `.`) contributes no model,
  `TableName`, constant, function, `*gorm.DB` variable or migrate site. A
  directory `golang.Package` refuses (an unreadable constraint, a file
  go/parser rejects or without a payload, a lone CR, several packages) is
  not read: each of its files gets a row D marker
  (`schema_model_unread`), the group's table list may be incomplete, and
  an import of it binds nothing. Selection is the union over
  configurations, so files for different ports are all read; GO-V7 still
  covers what a single build would leave out.
- A refused module (version, an unsupported dialector, a configuration
  that may set `DryRun`, no `gorm.Open` site) gets one
  `schema_model_not_proven` diagnostic on its `go.mod` naming the reason,
  and its row D markers are dropped (its group has no table list).

### Dialect and configuration (GO-K2)

- An Open site is a call `<g>.Open(...)` where `<g>` is the file's
  binding of `gorm.io/gorm` (and not a local): a captured site, a call
  inside another captured site's tree (the payload does not repeat it,
  `w.New(gorm.Open(...))`) or a package-level `var` initialiser. A migrate
  call found inside such a tree is read the same way; one in a
  package-level initialiser has nesting `package` and is never ordered.
- Dialector: `<pkg>.Open(e)` or `<pkg>.New(e)` with `<pkg>` bound to
  `gorm.io/driver/postgres` (postgres), `gorm.io/driver/sqlite` or
  `github.com/glebarez/sqlite` (sqlite) or `gorm.io/driver/mysql` (mysql).
  Any other dialector (another driver, a variable, a call of another
  function) leaves the site without a dialect and refuses the module
  (marker). A dialector local (Revision 2): `gorm.Open(d, cfg)` with `d` a
  local of the function (`var d gorm.Dialector`, or a define) whose every
  value, wherever the function assigns it (a `switch`, an `if`), is a
  dialector call above: one site per assigned dialector, so the group's
  dialects are their union (a runtime group when they differ); any other
  value refuses the module.
- `Dialects` are the distinct dialects of the module's sites, sorted;
  `Dialect` is set only when there is one. Several make a runtime group
  (GO-V3): its dialect-dependent facts are per dialect (Revision 1, O1)
  through the T018g0b kit, which the claim pass uses.
- mysql: `mysql.Open(dsn)` keeps the driver defaults; `mysql.New(
  mysql.Config{...})` reads `DefaultStringSize`, `DisableDatetimePrecision`
  and `DefaultDatetimePrecision` when they are number or boolean literals
  (`MySQLConfig`); a non-literal value, positional elements or a
  non-literal argument make that setting unknown. The module's `MySQL`
  holds the settings every mysql site agrees on.
- Configuration (`Config`): exactly two arguments, the second a
  `&gorm.Config{...}` literal ("config literal") or an identifier that is
  a local of the same function declared once from such a literal and
  never assigned ("once-defined local"). No configuration argument
  (`gorm.Open(d)`, "no configuration argument") is GORM's zero `Config`
  (`gorm.go:138-179`: the default naming strategy, foreign keys on,
  relations read; Revision 1 addendum 2, P1). More options, `&cfg` of a
  value literal or any other form: every setting unknown (names and
  foreign keys). Read keys: `NamingStrategy` (a
  `schema.NamingStrategy{...}` literal or `&` of one whose fields are
  literal `TablePrefix`, `SingularTable`, `NoLowerCase`,
  `IdentifierMaxLength`; `NameReplacer`, any other field or a non-literal
  value: naming unknown), `DisableForeignKeyConstraintWhenMigrating` and
  `IgnoreRelationshipsWhenMigrating` (literal booleans, else unknown). A
  `DryRun` key that is not literal `false` refuses the module (the
  migration may not run; row D). Other keys have no table fact.
- The module's `Config` holds a key only when every site states it and
  they agree; an assignment in a read file to a selector named `Config`
  (every key), `NamingStrategy`, `TablePrefix`, `SingularTable`,
  `NoLowerCase`, `IdentifierMaxLength`, `NameReplacer` (naming),
  `DisableForeignKeyConstraintWhenMigrating` or
  `IgnoreRelationshipsWhenMigrating` makes that key unknown (GO-V4).
- An `inflection.Add*` call in a read file is listed in
  `InflectionChanged` (plural-derived names unknown, GO-K4, claim pass).

### Migrate sites and receivers (GO-K3)

- A migrate site is a captured call whose selector is `AutoMigrate` or
  `CreateTable`, unless the selector's object is an import binding: then
  it is a package function call (a module package's function is read
  through the order rule; another package's is a row D marker).
- Call forms: `<r>.AutoMigrate(args)`, `<r>.Migrator().AutoMigrate(args)`,
  `<r>.Migrator().CreateTable(args)`; a `CreateTable` not called on
  `Migrator()` is D.
- Chain: between the start and the call only `Debug()`, `WithContext(e)`,
  `Set("gorm:table_options", e)` and `Session(&gorm.Session{...})` without
  a `DryRun` that is not literal `false`; any other method (`Table`,
  `Scopes`, `Model`, another `Set` key) is D.
- Receiver proof (`Receiver`): the start is
  - an identifier declared in the function: every declaration must prove
    it (a parameter of type `*gorm.DB`: "param"; `var x *gorm.DB`:
    "var"; a local defined from `gorm.Open(...)`'s first result:
    "gorm.Open"; from a module function `F()` or `pkg.F()` whose first
    declared result is `*gorm.DB`: "module function", with
    `ReceiverCall` naming it), so a shadowing declaration of another type
    refuses it;
  - an identifier not declared in the function that is a package-level
    `var X *gorm.DB` of the file's package, or `pkg.X` of a module
    package ("package var");
  - `s.f` where `s` is the method's receiver or a parameter of a module
    struct type `T` or `*T` whose field `f` has type `*gorm.DB` ("field").
  A function literal's parameters are not declared in the payload, so a
  site in a closure on its parameter (`func(tx *gorm.DB) { tx.AutoMigrate
  (...) }`, gormigrate) is D. Every other start is D.
- Handle flows (Revision 2, A4; payload `Returns`, `Calls`, `Stores`,
  `Literals`): a proof holds only where the handle cannot have been
  scoped (`Table(...)`, `Scopes(...)`, `Model(...)`). A module function
  counts only when every return statement's first value proves a handle
  (`gorm.Open(...)`, an allowed chain on a proven name, a proving module
  function's call, or `nil`); a bare return does not. A `*gorm.DB`
  parameter of a package function counts only when every call of the
  function in the module's gated files passes a proving value at its
  position, and no read file outside gate G1 calls it (the call cache);
  a method's parameter is D (its calls are not resolved). A field counts
  only when every composite literal of its struct (keyed or positional)
  and every `x.f = e` assignment in the gated files sets a proving value.
  A local counts only when every assignment of it proves a handle, and a
  `var x *gorm.DB = e` only when `e` does. A proof that depends on itself
  (recursion) fails. Known gaps: a literal or assignment in a file
  outside gate G1, a function used as a value, an assignment to a
  package-level variable.
- Arguments (`Args`): `new(T)`, `&T{...}` and `T{...}` with `T` or `pkg.T`
  a non-generic struct of the module's read files (same package, or a
  module import). Any other argument (a variable, a slice spread, a
  pointer type, a named non-struct type) is D; a type of a package outside
  the module is GO-K1's refusal (marker naming the import); a type name
  the function declares itself (`type Box struct{...}` in the body,
  payload `LocalTypes`) is D (Revision 2, A6).
- Schema changes after the migrate calls (Revision 2, A2, A3), in any
  read file and on any receiver: `SetupJoinTable(model, "Field",
  joinModel)` makes the join table of that relation row D (marker, and
  `schema_model_not_claimed` for the join table); the join model is not
  GO-M2; a call whose model or literal field is not read makes every join
  table D. A `Migrator()` DDL call (`facts.GoMigratorDDL`: `DropTable`,
  `RenameTable`, `AddColumn`, `DropColumn`, `AlterColumn`,
  `MigrateColumn`, `MigrateColumnUnique`, `RenameColumn`, `CreateView`,
  `DropView`, `CreateConstraint`, `DropConstraint`, `CreateIndex`,
  `DropIndex`, `RenameIndex`) on a model (`&T{}`, `new(T)`, `T{}`) or a
  string table name changes that table, else every table: its column and
  constraint sets are partial, an index, constraint or table method also
  leaves the index and constraint lists (and every column's `index` and
  `unique`) unknown, and a column method's literal column (field or
  column name) is an unknown column (every column for a table method or
  a column that is not a literal); marker `schema_model_unread`, with
  the list marker for `RenameTable`. `Exec` or `Raw` whose first argument
  is a string literal (or `+` of literals) starting with `CREATE`,
  `ALTER`, `DROP` or `RENAME` (in any of its `;`-separated statements):
  marker with the list marker (the database may hold more tables, GO-V2),
  and every table's foreign keys unknown. A statement `ALTER TABLE`,
  `DROP TABLE` or `RENAME TABLE` that names a claimed table (plain,
  quoted or schema-qualified, compared without case) leaves that table
  only its table and column names: column and constraint sets partial,
  schema, primary key, index and constraint lists and every column
  unknown, with a reason naming the statement (Revision 2 addendum;
  as Diesel's non-benign patch removal, ADR 0062). Any
  other `ALTER`, `DROP` or `RENAME` (a type, an index, a sequence: its
  target table is not read) leaves every table of the group only its
  names. A `CREATE TABLE` (any of `OR REPLACE`, `GLOBAL`, `LOCAL`,
  `TEMP`, `TEMPORARY`, `UNLOGGED`, `FOREIGN`, `IF NOT EXISTS`) decides the
  columns, types, constraints and indexes of the table it names
  (AutoMigrate then only adds missing columns), so it counts as an ALTER
  of that table whatever its order beside the migrate calls, which is not
  proven; one whose target is not read leaves every table of the group
  only its names (part 2). `CREATE [UNIQUE] INDEX ... ON <table>` leaves
  that table's index and constraint lists unknown (every table's when no
  table is read); another `CREATE` (a view, a trigger, a function)
  changes no claimed fact.
  In a read file outside gate G1 a call named `SetupJoinTable` or a
  Migrator DDL method (call cache) counts as one that is not read.
- `Models` are the structs proven sites list, first listing first, with
  identity `table:<file>#<Struct>` (GO-M1). Auto-added tables
  (`ReorderModels`) are the claim pass's (GB4).
- A row D site or argument appends `"<file>:<line>: <why>"` to
  `Incomplete` and emits a `schema_model_unread` diagnostic ending with
  `ListMarker(<group>)`, so the group's `table_list` is
  `may_be_incomplete` (the server's model-list marker rule).
- Files outside gate G1 (GB1 open point 2, decision): the
  payload has no sites for them, so a call named `AutoMigrate` or
  `CreateTable` in their call cache (`Facts.Calls`, `StaticName`) that is
  not `pkg.AutoMigrate(...)` on an import binding is D ("its receiver is
  not proven to be a *gorm.DB"); the table list may be incomplete. A read
  file without a payload (capture failure) is D the same way.

### Order (GO-K3, O6)

- Within one function, sites with nesting `unconditional` or `if-header`
  are ordered by their top-level statement index (`Ordered`).
- A top-level call statement (`F(...)`, `pkg.F(...)`, also as the single
  value of `:=`, `=`, `return` or an `if` initializer) of a module
  function that has migrate sites expands in place (`Expansions`), its
  ordered sites at the statement's position, after the statement's own
  sites. Depth 2: the expanded function's own call statements are not
  expanded.
- A root is a function with migrate sites that no other function with
  migrate sites expands; each root gives one of `Sequences` (sorted by
  file and declaration order), and each site's `Sequence` and `Position`
  are its first appearance. `OrderProven` holds when there is one
  sequence and every site is in it. A site in a condition, loop, closure
  or another root is unordered: its tables are claimed, the
  order-dependent facts (GO-R1 child foreign keys, GO-R3) are unknown
  (claim pass).
- realworld: `Migrate` expands `users.AutoMigrate()` (`UserModel`,
  `FollowModel`) before its own 5 calls; gotify: one site.

### Markers (GO-M2, GO-U1)

- GO-M2: a struct of a read file with a `gorm:` tag or an embedded
  `gorm.Model` that no proven site lists, directly or as an embedded
  struct (transitively) of a listed model: `schema_model_not_proven`
  "struct X has gorm tags or embeds gorm.Model, but no read migrate call
  lists it (model, not proven migrated)". GB4 also counts auto-added
  tables as reached: the claim pass drops the marker of a struct a
  migrate call reaches (listed, auto-added or failing) and of the structs
  it embeds.
- GO-U1: a non-test Go file importing `github.com/jinzhu/gorm` or a
  package under it: `schema_model_unread` "<file>:<line>: <import> import
  (GORM v1): found, not read". The server reads these markers (Revision 1
  addendum 2, P2; `internal/server/schemamodels.go` `gormV1Files`):
  `unread_tools` lists one `gorm-v1` entry per Go package directory with
  marked files (`path` the directory, `files` their count, no marker
  file).
- A module whose directory lies under `testdata`, `vendor` or a
  directory starting with `.` or `_` (the go command does not build it,
  GO-V7) is refused with `schema_model_not_proven` on its `go.mod` (unit
  GB4: this repository's own fixture modules).

### Corpora

| corpus | module | dialect | config | sites (receiver) | models | order |
|---|---|---|---|---|---|---|
| gotify `5ce7f0ac` | `github.com/gotify/server/v3`, gorm v1.31.2 (verified) | runtime: mysql, postgres, sqlite (3 sites in one `switch`) | once-defined local, foreign keys disabled | 1 (`var db *gorm.DB`) | 5 | proven |
| realworld `a41289b2` | `github.com/gothinkster/golang-gin-realworld-example-app`, gorm v1.25.12 (verified) | sqlite (2 sites) | two literals, defaults | 7 (5 param, 2 module function `common.GetDB`) | 7 | proven (one root, `users.AutoMigrate()` expanded) |

No marker on either corpus. `TestGormDiscoveryCorpus` checks every
discovery and read row of `docs/contract/claims/data/gorm-models-inventory.jsonl` for GO-K1,
GO-K2, GO-K3 and GO-M1 (gotify 21, realworld 30) against Discover's
result, both ways.

## Claim pass (GB3)

`Link(Input)` runs discovery and, for every read module, one kit group
(`models/gorm/<module dir>`): `dialect` for one dialector, `dialects`
for a run-time choice (GO-V3). Verification: `oracle` for postgres and
sqlite facts (the investigation oracle compared these forms), `mapped` for mysql
facts (GO-V5, which joins the group's assumptions); a run-time group's
dialect-independent facts are `oracle` (Revision 1, O2) and its
dialect-dependent facts carry each dialect's own verification through the
ADR 0065 kit. A newer gorm than the verified range adds the table
attribute `assumption_notes: {"GO-V1": "rules verified at gorm
1.25.12-1.31.2"}`.

- **Models (GO-M1).** The structs the proven migrate sites list, in
  listing order. Not claimed (`schema_model_not_claimed` with the group's
  list marker): a naming strategy that is not read (every derived name
  unknown), a table name that is not known (GO-K4), longer than 63 bytes
  in a group with postgres, or shared by two models (row D), and every
  model of a migrate call one of whose models provably fails on a dialect
  of the group (GO-P1: a default the kind cannot parse, a map or a slice
  of a non-struct type without a type or serializer, an embedded field
  that is not a struct, sqlite: two autoincrement integers, an index type
  or class, an index name another table uses). Step GB4 narrows GO-P1 to
  the failing model and the models after it in the call's order, and adds
  the tables AutoMigrate adds and the join tables ("Relations and migrate
  order").
- **Table name (GO-K4).** A `TableName()` method on `T` or `*T`, or one
  promoted from an embedded module struct (two at the same depth, or an
  embedded type outside the module, leave the name unknown), whose body
  is one `return e` with `e` a string literal, a package-level `const`
  of the module's packages or a `+` of those (Revision 1, O7); a
  `TableName(schema.Namer)` or any other body: unknown. Else the naming
  strategy: `TablePrefix + inflection.Plural(toDBName(name))`, or without
  the plural when `SingularTable`; an `inflection.Add*` call in the module
  makes plural names unknown; a non-ASCII struct name is unknown.
- **Fields and columns (GO-F1, GO-F2).** `ParseField` as ported: exported
  fields in order, `-`, `-:all` and `-:migration` without a column,
  relation fields (a module struct, a pointer or slice of one) without a
  column, `gorm.Model` and anonymous or `embedded` module structs
  flattened with `embeddedPrefix`; a column name from the `column` tag
  verbatim (GO-F2), else `toDBName` (GO-K4); a non-ASCII derived name or a
  name over 63 bytes in a group with postgres: no column record, the
  column set partial. Unknown columns (`state: unknown`): a field type the
  contract does not read (the column set partial), a name two fields
  write, a field an embedding field's tags reach, a `generated` tag.
- **Raw type texts (Revision 2, A1).** A raw `type:` tag that is not one
  type name with optional `(n[,m])` or `[]` (`varchar(100) NOT NULL`,
  `text NOT NULL DEFAULT ''`, `varchar(20) UNIQUE`, `int(11) NOT NULL
  AUTO_INCREMENT`): GORM writes the text verbatim, so the column's type,
  nullability, autoincrement, default, unique and foreign keys are
  unknown on every dialect and the table's constraint set is partial (a
  `PRIMARY KEY` in the text: the key unknown). A raw serial type
  (`serial`, `bigserial`, `smallserial`, `serial2/4/8`): nullability,
  autoincrement and default unknown on every dialect.
- **Types (GO-T1, GO-T2).** The column `type` is the dialect's SQL type:
  postgres the catalog's `format_type` text (`bigint` for a `bigserial`,
  `character varying(n)`, `numeric(p,s)`, `timestamp(p) with time zone`,
  the closed raw list), sqlite the declared type GORM writes (`integer`,
  `numeric`, `real`, `text`, `datetime`, `blob`, raw texts verbatim),
  mysql the type text GORM writes without `AUTO_INCREMENT` or `NULL`
  (`bigint unsigned`, `varchar(191)` for keyed or defaulted strings,
  `longtext`, `datetime(3)`, `longblob`, `boolean`; `DefaultStringSize`,
  `DisableDatetimePrecision` and `DefaultDatetimePrecision` from a
  `mysql.New` literal, unknown when not literal). `length`, `precision`
  and `scale` are the rendered type's modifiers (GO-T1). GORM's own data
  type with its size, precision and scale is the `tool_type` annotation
  (no claim). A foreign key's type follows the relation's type copy
  (`relationship.go:592-604`, "Relations and migrate order").
- **Keys (GO-C1).** GORM's primary-key rules (`primaryKey` truthy, else
  `id`/`ID`; autoincrement for the prioritized integer key without an
  `autoIncrement` tag); sqlite's inline `integer PRIMARY KEY
  AUTOINCREMENT` replaces the table's key. A run-time group
  whose key differs per dialect leaves the key unknown. `autoincrement`
  per dialect: postgres serial, sqlite inline integer, mysql
  `AUTO_INCREMENT`.
- **Nullability (GO-C2).** postgres `NOT NULL` tag, primary key or serial;
  sqlite the tag only; mysql the tag or primary key. One value on every
  dialect of the group, else unknown (per-dialect nullability is not
  modelled).
- **Defaults (GO-C3).** `server_default` and `server_default_text` per
  dialect, `default` (`server` or `absent`) when every dialect agrees. A
  serial: present, text unknown; sqlite and mysql autoincrement: absent.
  Text claimed for plain decimal integers and floats on a numeric type,
  booleans on postgres and sqlite, strings without quote, backslash or
  control characters (`'x'::text`, `'x'::character varying`; sqlite
  `"x"`), sqlite `null`; raw texts (parentheses, time kinds, unparsed
  forms): presence only; postgres drops `default:null`; mysql string,
  boolean and `null` texts are not mapped.
- **Unique and indexes (GO-C4, GO-I1).** `unique` truthy: a unique
  constraint `uni_<table>_<column>` (name `null` on sqlite and where the
  dialects' names differ) and `unique: true`; `index`/`uniqueIndex` tags
  through the `ParseIndexes` port: entries `name` (explicit, or
  `formatName` at each dialect's limit; `null` when they differ or
  postgres would truncate), `columns` in priority order (`null` with an
  `expression`), `unique`, `partial` (`where` text not claimed) and
  `descending`; `unique: true` for the only column of a unique index,
  `index: true` for a column in any index. An index list is unknown when
  an index has a class other than `UNIQUE` on postgres or sqlite, or an
  embedding field has tags; an index name two tables use follows the
  migrate order (below). A `check` tag leaves the constraint set partial
  (row D).
- **Foreign keys (GO-R3).** `[]` when the configuration disables them or
  ignores relations; otherwise the relations' constraints (below).

## Relations and migrate order (GB4)

- **Relations (GO-R1).** Every relation field of every schema a migrate
  call reaches is parsed once, as GORM's schema cache does
  (`relationship.go:65-131`): `guessRelation` (`456-622`) with its guess
  levels (a struct field tries has-one before belongs-to, a slice field
  has; a self reference belongs-to first), the lookup names
  `<Field><PK>` (belongs-to) or `<Schema><PK>` (has) with `ID`, `Id` and
  the column name for a single key, `foreignKey`, `references` and
  `belongsTo` tags, and GORM's pairing of the found keys with the primary
  fields by position. The referenced field's data type (when copyable),
  GORM data type and size (when the key's own size is 0) are copied onto
  the foreign key. A has-one or has-many relation registers on the child.
  A guess that finds no key, a `many2many` key or reference the schema
  does not have, or a polymorphic relation whose type or id field is
  missing fails the schema (GO-P1). Not read (row D, `schema_model_unread`;
  the foreign keys of both sides unknown, the types of the fields it may
  pick unknown): a polymorphic relation (no list marker: it adds no table)
  and a relation field of an embedded struct (with the list marker: a
  belongs-to target may be added).
- **Join tables (GO-R2).** `buildMany2ManyRelation` (`271-444`): the join
  table `JoinTableName(name)` (kept when lower case), its fields
  `Title(<Schema>)+<key>` and `Title(<Target>)+<key>` (a self join
  `Singular(field)+<key>` or `...Reference`; `joinForeignKey` and
  `joinReferences`), each a copy of the key field with `primaryKey` added
  and `column`, `autoincrement`, `index`, `unique`, `uniqueindex`
  removed (`utils.go:54-67`), types copied from the keys, every field a
  primary-key member, and two belongs-to relations whose constraints
  `fk_<join>_<schema name>` carry the many2many field's `constraint` tag.
  Record identity `table:<file>#<Struct>.<Field>` (no containing node).
  A join table several many2many relations declare (realworld's
  `article_tags`) keeps no primary-key order (open point O8; the column
  keys are claimed); declarations with different columns or types: not
  claimed (row D).
- **Constraints (GO-R3).** `ParseConstraint` (`661-726`): none for
  `constraint:-` and for a belongs-to whose target has the reverse
  has-relation over the same references (that relation's constraint is
  created, named after its owner: `fk_holders_pets`); the name is the
  `constraint:<name>,...` prefix or `RelationshipFKName` at each
  dialect's limit (`null` on sqlite, which keeps no name, and where the
  dialects differ); actions upper-cased from the tag, else `NO ACTION`
  (the database default; GORM writes no clause), `null` for another text.
  A table's foreign keys are the constraints of its own relations and of
  the has-relations registered on it whose `Schema` is the table; each
  local column lists the whole constraint (`local_columns`, `columns`).
  Unknown for every column: a configuration key that is not read, a
  relation that is not read, or a migrate site that is not read (it may
  parse a model whose has-relation registers on the table).
- **Migrate calls (GO-K3).** With the order proven (one sequence holding
  every site, open point O6), the calls run in that order; else each call
  runs alone. A call's values and every schema their relations reach are
  parsed before its first table is created. `AutoMigrate` ports
  `ReorderModels(values, true)` (`migrator.go:919-1020`): each value
  after the targets of its constraints (belongs-to targets, and the
  parents of has-relations registered on it), join tables appended as
  values; the many2many target's own dependency has no
  effect. `Migrator().CreateTable` creates its values only. A table that
  exists already is migrated, which adds the constraints that are
  missing (`migrator.go:168-180`). The tables a call creates are claimed
  (GO-M1: listed models and auto-added belongs-to targets; GO-R2 join
  tables), and their GO-M2 markers dropped.
- **Parse order (GO-R1, GO-V6, open point O12).** A registered
  has-relation's constraint is on the child when its owner is parsed by a
  call that creates or migrates the child; a type copy shows in the
  column when its owner is parsed before every such call, the field's own
  type when after all of them, else the type is unknown. With the order
  not proven, "parsed by a call" is yes only for the call's own parse,
  unknown when another call parses the owner: the key's columns are then
  unknown. Where another call's parse could add a table to
  a call, a marker with the group's list marker.
- **Failures (GO-P1).** A call stops at its first failing table
  (`migrator.go:122-206`): a schema that fails to parse (GB3's forms, a
  relation GORM cannot build, a relation target that fails), sqlite's
  two autoincrement integers or index type, sqlite's raw function default
  not wrapped in parentheses (`default:datetime('now')`: `near "(": syntax
  error`; Revision 2, A5), postgres' index `class` other than `UNIQUE`
  (`CREATE FULLTEXT INDEX`: syntax error, the table exists without it and
  the call stops; Revision 2, A5; a lower-case `unique` class still
  leaves the index list unknown), on postgres and mysql with
  foreign keys on a constraint to a table that does not exist yet
  (`Migrator().CreateTable` of a model whose target is not created;
  a foreign-key cycle in one call), on sqlite an index name a
  table created earlier has (sqlite fails the second `CREATE INDEX`;
  postgres skips it, `migrator.go:149`, so the second table lacks that
  index; where which table is first is not known, both
  index lists are unknown on postgres and both tables are withheld on
  sqlite). The tables the call created before the failure are claimed:
  the segments before it (a value and the dependencies inserted with
  it), and in the failing segment the failing table's own dependencies;
  the failing table and the later ones are not claimed
  (`schema_model_not_claimed`, row GO-P1, with the group's list marker).
  A failure that depends on what is not read (an order that is not
  proven, a foreign-key setting that is not read): row D.

## Driver pins (Revision 2, A7)

The verified dialector driver versions are gorm.io/driver/postgres
v1.5.11 and v1.6.3, gorm.io/driver/sqlite v1.5.7 and v1.6.0,
github.com/glebarez/sqlite v1.11.0 (oracle) and gorm.io/driver/mysql
v1.6.0 (mapped). A module whose go.mod pins another version of a driver
it requires, or that opens a driver it does not pin, is read under GO-V1
with the note shown on every table (`assumption_notes`, `GO-V1`: "<module>
<version> is not a verified driver version (<verified versions>)"). A
postgres driver older than v1.5.6 has no `Apply`, so GORM keeps its
64-byte identifier limit: a generated index, unique or foreign-key name
is hashed at 64 bytes and postgres truncates it (oracle, `rev2old/`), so
such a name over 63 bytes is not claimed.

## Mapped dialect (GO-V5)

MySQL facts are never `oracle`: they are mapped from gorm.io/driver/mysql
v1.6.0 as the dialect rule below cites (`mysql.go:360-506` for
`DataTypeOf`: floats `379-389`, strings `391-414` with `DefaultStringSize`
and the 191 rule for keyed or defaulted strings, times `416-430` with the
default precision 3 of `mysql.go:66`, bytes `432-442`, ints `469-496`,
custom types `498-506`; comments `migrator.go:40-48`; indexes inline in
`CREATE TABLE` with the prefix `length` of
`migrator/migrator.go:251-275` and `808-827`), for a MySQL server 5.6 or
later (`SELECT VERSION()` switches datetime precision off for 5.0-5.5 and
changes options for MariaDB, `mysql.go:128-161`). A `mysql.New` dialector
with a non-literal `DefaultStringSize`, `DisableDatetimePrecision` or
`DefaultDatetimePrecision` leaves the string and time types unknown
(discovery's `MySQL` settings).

## Oracle (GB5)

`scripts/oracle/gorm_catalog.sh` (never run by CI) builds scratch Go
modules from `scripts/oracle/gorm_oracle/` (its files carry
`//go:build ignore`, so they stay out of the graphvein module and its
`go.mod`; the script removes the line in the scratch copy) at gorm
1.31.2 (postgres driver 1.6.3, pgx 5.10.0) and gorm 1.25.12 (postgres
driver 1.5.11), both with `github.com/glebarez/sqlite` 1.11.0 and
`CGO_ENABLED=0`. `copytypes.go` copies the model packages (or single
model files) into them with only type and constant declarations,
literal variables and the methods GORM's schema parse calls
(`TableName`, `Value`, `Scan`, `GormDataType`, `GormDBDataType`): no
corpus function runs. Each run (`runs_*.go`) is one module's
`gorm.Config` and migrate calls in source order; every call runs even
after an earlier one fails (the error is recorded), into a fresh Postgres
17 database (`scripts/cloud-postgres.sh`) or SQLite file, and the catalog
is dumped. The `pg/` fixture run creates the domain `blob` first, the
type `CustomProbe.Blob`'s `GormDataType` names (GO-V2 and open point O11:
the record assumes the application started; the column is not claimed).

Revision 2 adds the fixture runs `rev2` (the closed raw list, raw texts,
the postgres `FULLTEXT` failure, `SetupJoinTable`, a handle through a
function's returns and a parameter), `rev2ddl` (`DropColumn`, `DropIndex`,
`Exec` DDL, the scoped handles and the local type, run as the source
states them), `rev2lite` (sqlite raw texts and the bare default's
failure), `dialvar` (postgres and sqlite) and, as its own build at
gorm.io/driver/postgres v1.5.4 (`fixtureold`), `rev2old`; a run step may
be another call of the source (`Do`). The script names its binaries with
`$(go env GOEXE)` (Git Bash on Windows).

extractors-44 adds the run `buildsel` (its `model/` package only; `mixed/`
holds two packages the go command cannot build), and `copytypes` keeps
each file's build constraint lines and an import of `"C"`, so the go
command building the scratch module (`CGO_ENABLED=0`) selects the copies
as it selects the originals; the oracle never uses Graphvein's file
selection. Re-run (2026-11-17): 0 disagreements on every fixture run and
on gotify and realworld-gin (888 and 505 oracle-labelled facts, all
compared); main's code on the same dump disagrees on exactly the three
`buildsel` tables (`ghosts`, `ignored_accounts`, `cgo_ledgers`). The
runs `rev2alter` and `rev2create` (the migrate call, then each `Exec` as
written; `db.Raw` without a finisher runs nothing) close the preset's
earlier gap: every oracle-labelled fact of the fixture (3,827) is now
compared, 0 disagreements. `rev2create`'s `CREATE TABLE IF NOT EXISTS ?`
fails on Postgres (the placeholder is a bind parameter); the group claims
only table and column names there.

`TestGormOracle` (`GRAPHVEIN_GORM_ORACLE` the dump,
`GRAPHVEIN_GORM_ORACLE_ROOT` the fixture or corpus directory) links each
run's repository and compares every claimed fact with the catalog on the
run's dialect: table existence, column set, primary key in key order,
indexes (name, columns, unique, partial, descending; all of them when the
constraint set is complete), unique and check constraints; per column
existence, type (case-insensitive on sqlite) with length, precision and
scale, nullability, primary key, autoincrement, index, unique, default
presence and text, and foreign keys both ways (columns, target, actions,
name where claimed). A run-time group is compared per dialect
(`by_dialect`), its flat facts on postgres and sqlite. A cross-dialect run
(realworld on postgres) compares only dialect-independent facts and
covers nothing. Every fact labelled `oracle` must be compared on its
dialect, and a MySQL fact labelled `oracle` fails the test.

## Assumptions

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

## Tests

- `contract_test.go`: `TestGormContract` (the docs table equals the task
  file's, the code table the same rows in the same order with the same
  assumptions and sides, every listed form named in its row, the
  assumptions equal the task file's environment section) and
  `TestGormDiscoveryTags` (discovery's tags are rows and listed forms;
  every GO-K2 and GO-K3 form reached by the inline modules).
- `fixture_test.go`: `TestGormFixture` (the records of
  `testdata/fixtures/gorm-models/`, part 1, equal
  `testdata/link.golden.json`; `modeltest.CheckClaims` with
  `CheckDialectFacts`; the facts of the forms each row decides),
  `TestGormClaimTags` (the tag half of the contract test: every tag a
  row or listed form, every row with a branch and every listed form of
  GO-K2 and GO-K3 reached, every claim by a claim row that decides its
  fact) and `TestGormStepIsolation`. Step GB4 adds the relation modules
  `rel/`, `relsplit/`, `relorder/`, `relignore/` and `ctlite/` (the investigation relations, relations-split, autoadd, createtable and
  relations-ignore probes) with their foreign keys, join-table keys and
  narrowed GO-P1 failures checked.
- `indexed_test.go`: `TestGormIndexedFixture` (the fixture through the
  indexer equals `testdata/fixtures/gorm-models.golden.json`; a warm
  replay parses nothing and emits the same stream).
- `contract_test.go`: `TestGormCatalogue` (the `/api/v0/schema/assumptions`
  catalogue is registered; its rows are the contract's with the docs'
  claim cells, assumptions and a branch function of the package; its
  assumptions are the docs' entries).
- `tables_test.go`: mysql renderings and settings, column rules (raw
  autoincrement, non-canonical and quoted defaults, named types,
  duplicate columns, interfaces with and without a serializer), table
  names (ambiguous promotion, embedded external types, non-ASCII, shared
  names, `inflection.Add*`), shared index names (in one call, and in two
  calls whose order is not proven), a run-time group's per-dialect key and
  defaults, and `TestGormRelationOrder` (a has-one parent's type copy and
  constraint with the child migrated first, after, in one call and in an
  order that is not proven).
- `port_test.go`: `TestInflection` and `TestNaming` against the outputs
  of jinzhu/inflection v1.0.0 and gorm v1.31.2's `NamingStrategy`
  produced in scratch (`testdata/port-1.31.2.json`: every struct and
  field identifier of both corpora, the probes and the fixture, and
  inflection's own test list).
- `discover_test.go`: inline modules for gotify's and realworld's shapes,
  every receiver proof and chain form and their refusals, the argument
  forms, configuration (literal and pointer naming, `NameReplacer`,
  non-literal and disagreeing keys, mutations, assigned locals, no
  configuration, `DryRun`, other dialectors), dialectors (glebarez,
  `mysql.New` literals, sqlserver), versions and replaces, nested
  modules, `testdata`/`vendor`/test files, GO-M2 and GO-U1 markers, the
  ungated call marker, a dependency's `AutoMigrate`, conditional sites,
  several roots and `inflection.Add*`; `TestGormModuleUnderTestdata`
  (step GB4: a module under `testdata`, `vendor`, `.` or `_` refused).
- `corpus_test.go`: `TestGormCorpusLink` (the claim pass over both
  corpora against the investigation coverage targets: gotify 5 tables, 42
  columns, 10 indexes, foreign keys proven absent; realworld 8 tables
  with `article_tags`, 46 columns, 9 indexes and the 10 foreign keys of
  the investigation sqlite catalog; every column read and typed on every dialect
  of its group) and `TestGormDiscoveryCorpus` (set
  `GRAPHVEIN_GORM_CENSUS_CORPUS` to the directory holding the
  `scripts/cloud-corpus.sh gotify realworld-gin` checkouts).
- `revision2_test.go` (Revision 2): `TestGormHandleFlows` (returns,
  parameters, fields, assignments, a method parameter, an ungated
  caller), `TestGormLocalType`, `TestGormJoinSetup`, `TestGormSchemaChanges`
  (a column, an index, an unread model, a table name, DDL text, an
  ungated DDL call), `TestGormRawTypes` (every dialect),
  `TestGormRevision2Failures`, `TestGormDriverPins`,
  `TestGormDialectorLocal`, `TestGormDiscoverIsolation` (C1); the fixture
  modules `rev2/`, `rev2ddl/`, `rev2lite/`, `rev2old/`, `dialvar/`;
  `internal/index` `TestGormGuard` (the indexer call's recover).
- `oracle_test.go`: `TestGormOracle` ("Oracle" above; skipped unless
  `GRAPHVEIN_GORM_ORACLE` and `GRAPHVEIN_GORM_ORACLE_ROOT` are set).
- `internal/extract/models/lockfile`: `TestParseGoMod`.
- `internal/server`: `TestUnreadToolsGormV1` (the `gorm-v1` rule over
  marker diagnostics) and `TestSchemaGormV1` (a GORM v1 import reaches
  `unread_tools` through the indexer, on both backends). The rule reads
  only the GO-U1 markers (`store.DiagnosticsWithMessage`, code
  `schema_model_unread` and the marker text; `TestDiagnosticsWithMessage`
  on both backends; Revision 2, C3).
