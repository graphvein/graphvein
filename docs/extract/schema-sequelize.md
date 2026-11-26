# Schema from Sequelize models

ADR 0061 (T018g3, `docs/contract/claims/sequelize-models.md`; inventory and
oracle baseline in the T018g3 task record (internal)). The Sequelize front
end `internal/extract/models/sequelize` reads each package's Sequelize v6
instance and the models and association calls that reach it, from the
cached TypeScript model payload version 4 (`docs/lang/typescript-models.md`)
and the retained `package.json`, lockfile and node-config sources, and
will hand its tables to the tool-neutral model kit
`internal/extract/models` (ADR 0058, `docs/extract/schema-models.md`),
which emits the records every ORM front end shares (`source: "model"`,
`tool: "sequelize"`, group `models/sequelize/<instance file>`).

**Status (2026-11-13):** discovery (`discover.go`,
`options.go`, `cjs.go`, `reach.go`, `associations.go`; pins from the
shared lockfile reader `internal/extract/models/lockfile`), the contract
table (`contract.go`), the claim pass for models, attributes and options
(`link.go`, `tables.go`, `options_model.go`, `attrs.go`, `types.go`,
`ddl.go`, `inflection.go`, `render.go`) and for references,
associations, their failures and the cyclic rule (`refs.go`, `assoc.go`;
rows SQ-A3, SQ-R1, SQ-R2, SQ-P1) are in place, and the indexer
calls `Link` (`internal/index/index.go`, after Drizzle). The catalogue
(`catalogue.go`) is registered for `/api/v0/schema/assumptions`. Unit
SQB4b (Revision 2) reads attribute helpers (`helpers.go`, row SQ-A1 form
"helper member"); step SQB5 adds the oracle
(`scripts/oracle/sequelize_catalog.sh`, `TestSequelizeOracle`, "Tests"
below). Step SF (task Revision 3)
adds mssql's foreign-key actions, instance hooks, the key
uniqueness and missing-table refusals, the failure list of SQ-P1
(`failures.go`), the load-order rule, the all-or-nothing foreign-key
step, table reasons for partial sets, models of other packages and the
sequelize-cli forms.

Line numbers below are from the published `sequelize@6.37.8` package
(`lib/`), the version both corpora lock.

## Discovery

`Discover` returns the packages, their pins, drivers and migrations
folders, and per package the module-level instances; each instance is one
model group with the models and association calls that reach it.
Diagnostics are `info`: `schema_model_not_proven` (a package or instance
without claims, options not read, dialect unknown), `schema_model_unread`
(row D markers) and `schema_model_migrations` (row SQ-U1).

- **Packages (SQ-K1).** A directory with a `package.json` (not under
  `node_modules`) whose `dependencies` or `devDependencies` name
  `sequelize`. A package owns the script files below it that no nearer
  `package.json` owns.
- **Pins (SQ-V1).** The locked `sequelize` version from the lockfile
  beside the package, else the nearest one above it (the shared reader:
  `pnpm-lock.yaml` importer, `package-lock.json` `packages`, `yarn.lock`).
  `inflection` is a dependency of sequelize, so its version is read only
  from a `package-lock.json` (lockfile 2 or 3) as Node resolves it from
  the installed sequelize directory: `<sequelize>/node_modules/inflection`
  first, then each `node_modules` above. Other lockfiles do not list
  transitive dependencies by install path. `pins` is
  `{"sequelize": v, "inflection": v}` when both are proven, else null
  (claims are kept, SQ-V1 still listed).
- **Major.** The locked version, else the declared range, must have major
  6 (developer decision 2026-11-13); another major, or a range whose major
  is not fixed, refuses the package (`schema_model_not_proven`, no claims).
- **Drivers.** The dependencies (either list) among the drivers Sequelize
  loads per dialect (`lib/dialects/<dialect>/connection-manager.js`,
  `_loadDialectModule`): `pg` postgres (`postgres/connection-manager.js:15`),
  `sqlite3` sqlite (`sqlite/connection-manager.js:18`), `mysql2` mysql
  (`mysql/connection-manager.js:30`), `mariadb` mariadb
  (`mariadb/connection-manager.js:30`), `tedious` mssql
  (`mssql/connection-manager.js:14`), and `ibm_db`, `oracledb`,
  `snowflake-sdk` for the dialects of row D.
- **sequelize-typescript.** A dependency on it is a row D marker
  (developer decision 2026-11-13: decorated models are unread).
- **Instance (SQ-K1).** A module-level `const x = new <C>(...)` in a
  gated file of the package, where `<C>` resolves to Sequelize's
  constructor: the default or `Sequelize` export of `sequelize` (an ESM
  import, `require("sequelize")`, `require("sequelize").Sequelize`, a
  destructured require) or `<ns>.Sequelize` of the package object. A
  `let` or `var` instance is a marker (it may be reassigned). The group
  root is the file, or `<file>#<name>` when one file declares several
  instances. A package with no instance gets `schema_model_not_proven`.

### Options and dialect

The constructor picks its options argument by count
(`lib/sequelize.js:49-98`): one object argument is the options; one string,
or a string and an object, is the URL form, whose protocol overwrites any
`dialect` option (`:54-57`); four arguments put the options last; three
have none (`:96-98`). A spread argument makes the options unknown.

The options argument is read when it is an object literal, a
module-level `const` bound to one (no module-level assignment to it), or
open point O2's environment selector (below). Keys are read in order; a
later entry overrides an earlier one:

- `define` must be an object literal; anything else makes the options
  unknown (every option-derived fact of the group's models: table and
  column names, timestamps, the implicit `id`). Its entries are read by
  SQB3.
- `schema` is claimed when it is a string literal; another value makes
  it unknown.
- `dialect` is used when it is a string literal (`postgresql` is
  `postgres`, `lib/sequelize.js:138-140`).
- An entry the payload does not read (computed key, accessor) makes the
  options unknown.
- A `hooks` key (Revision 3) makes the options unknown:
  instance hooks such as `beforeDefine` and `afterDefine` run inside every
  `define` and `init` and may change any attribute or option (row D). So
  does a call `<receiver>.addHook(...)` or `<receiver>.<hook type>(...)`
  (every hook type of `lib/hooks.js:5-51`) anywhere in the package whose
  receiver resolves to the instance, or to Sequelize's class (every
  instance of the package); the group is then not claimed.
- **Open point O1 (SQ-V5).** A spread of a member path of the node-config
  object (an import or require of the `config` package; a tsconfig
  `paths` entry that maps `config` to a `.d.ts` file is the package at run
  time) is read when every file of the package's `config/` directory is
  checked: JSON files are parsed after removing comments (as node-config
  reads them) and must not hold `define` or `hooks` under that path; any
  other file there must not name either as a word. A file that holds or names
  `dialect`, `schema`, `dialectModule` or `dialectModulePath` there makes
  that key unknown (the spread overrides earlier entries). Any other
  spread makes the options unknown. opencollective-api: `...config.database.options`,
  the nine JSON files and `sequelize-cli.js` of `config/` checked, no key.
- **Open point O2.** An argument-free call of a function that a relative
  require or import binds, whose body is only `const` declarations and
  `return <id>[<expr>]` (or an expression-bodied arrow), `<id>` an object
  literal that another module exports through a relative require or
  import, whose entries are all object literals that agree on `dialect`,
  `define`, `schema`, `dialectModule`, `dialectModulePath` and `hooks`
  (structurally, spans ignored): the first entry is read. Gladys:
  `getConfig()` (`server/utils/getConfig.js`) over the three entries of
  `server/config/config.js`. The sequelize-cli form (Revision
  3): a module-level `const` bound to `require("<relative>")[<expr>]`
  (the template's `require(__dirname + "/../config/config.json")[env]`;
  `__dirname + "/<path>"` is the relative specifier `./<path>`), directly
  or through a `const` bound to the require, where the required module is
  a JSON file (read in key order) or a module exporting an object
  literal, under the same agreement rule.

Dialect (SQ-V4, open point O3): the literal `dialect` (not overwritten
by the URL form); otherwise the package's single driver. A
`dialectModule` or `dialectModulePath` option, no driver or several make
the dialect unknown (dialect-dependent facts unknown). Options that are
not read leave the dialect to the driver rule. `db2`, `oracle`
and `snowflake`, literal or by driver, are row D. The dialects are those
of `lib/sequelize.js:166-191`.

### CommonJS reach

For gated files only (ADR 0061), names resolve across modules through:

- ESM import bindings, through the shared TypeScript resolver; a bare
  specifier is the external package.
- A module-level declaration whose initializer is `require("<spec>")`,
  `require("<spec>").x`, or a destructuring declaration from
  `require("<spec>")`. A relative specifier is placed as Node's CommonJS
  loader does: the file as written, `.js`, `.json`, `.node`, then the
  directory's `index.js` and `index.json`. A directory with its own
  `package.json` and a match only through a TypeScript extension (a
  loader such as ts-node) are not followed.
- The required module's `module.exports = e` (exactly one), its object
  literal entries, and `module.exports.x = e` / `exports.x = e`; a module
  without one falls back to its ESM exports.
- ESM exports: `export default`, exported declarations and classes,
  local export clauses (an imported binding re-exported), and re-exports
  from another module.

### Model reach

A model belongs to the group when:

- **init (SQ-M2 site).** A module-level statement `X.init(<attrs>,
  <object>)` whose object's `sequelize` entry (property or shorthand)
  resolves to the instance (opencollective-api: 52 statements, the
  instance imported from `../lib/sequelize`). The class declaration
  named `X` in the same file is recorded for SQB3 (the `extends` chain).
- **define at module level (SQ-M1 site).** A module-level `<r>.define(...)`
  statement or declaration initializer whose receiver resolves to the
  instance.
- **define in a factory (SQ-M1 site).** A module whose `module.exports`
  is an arrow or function with a plain first parameter `p`, whose body
  declares `const v = p.define(...)`, calls it or returns it; and a
  module-level call (outside function bodies) of a module of the package
  whose callee resolves to that factory and whose first argument
  resolves to the instance. A call that is the value of a property of a
  module-level `const` object records the object and key (Gladys: 32
  calls `<Key>: XModel(sequelize, Sequelize)` in `server/models/index.js`).

A definition that resolves to two instances is not claimed (marker).
A module-level define or init of a file that another package owns (a
nearer `package.json`) whose receiver or `sequelize` option resolves to
an instance is a marker on that instance's group (Revision
3); when that package has association calls, every model's
attributes of the group may change too.
Markers, each making every instance of the package incomplete (its table
list may be incomplete; each marker is written once per instance of the
package and ends with that group's table-list suffix
`; the table list of model group <set> may be incomplete`, which the
server reads): an init without a readable
`sequelize` option or with one that does not resolve, a module-level
define whose receiver does not resolve, a factory define no call runs
with an instance, a `require` with a computed path (a directory loader),
a `define` call inside any other function (the ungated call-site cache
`Facts.Routes.Clients`, every call with its callee chain), and every call
of such a function (open point O8: opencollective-api's
`Temporal(X, sequelize)`, ten history tables), and an `init` with a
`sequelize` option inside any function (a class model built in a
CommonJS factory, the sequelize-cli model template). Sites in test files
(`testscope`) are counted in one marker per package and never make a
group incomplete.

### Association reach

- **Module level.** Association calls (`belongsTo`, `hasOne`,
  `hasMany`, `belongsToMany`) that are module-level statements of a
  module the group reaches: the instance's file, the files of its models
  and factory calls, and every file of the package that imports or
  requires one of them (opencollective-api: 197 calls in
  `server/models/index.ts`).
- **The recognised loop.** A module-level statement
  `Object.values(M).filter((m) => typeof m.associate === "function").forEach((m) => m.associate(M))`
  (either operand order, `===` or `==`, expression or single-statement
  bodies) over a module-level `const` object literal `M`: for each entry
  in key order, the model it holds and its associate function
  (`<binding>.associate = fn` in the factory body, or
  `<Class>.associate = fn` at module level of the model's file), whose
  association-call statements are reached in statement order at the
  loop's position (Gladys: 53 calls in 28 functions). `models.Key` in
  the function resolves through the object's keys.
- **The sequelize-cli loop** (Revision 3). A module-level
  `Object.keys(M).forEach((k) => ...)` or `Object.values(M).forEach((m)
  => ...)`, or `for (const k of Object.keys(M))` / `for (const m of
  Object.values(M))` (payload row S7), whose body is the one statement
  `R.associate && R.associate(M)` or `if (R.associate) R.associate(M)`
  (payload row B-if; the guard may be `typeof R.associate ===
  "function"`), `R` being `M[k]` or `m`, runs like the recognised loop.
  The models object is the `const` object literal's entries followed by
  the module-level assignments `M.X = <value>` before the loop (a key
  keeps its first position and takes its last value); any other use of
  `M` before the loop (a computed assignment, a directory loader's
  `db[model.name] = model`, a call that receives it) makes the group's
  association attributes incomplete. An entry may be a factory call
  whose callee is `require("<relative>")` (`db.User =
  require("./user")(sequelize, DataTypes)`); an entry that resolves to
  the instance or to the `sequelize` package has no associate method. An
  init model's associate may be a plain `static associate(...)` method
  of its class (payload version 4 static members); a static member named
  `associate` that is not a plain method, a static block or computed
  name, or a static `associate` of a class it extends (whose `this` is
  the subclass) is not read and makes the group incomplete.
- Source and target are the models the receiver and the first argument
  resolve to (nil when they do not; the claim pass leaves those facts
  unknown).
- Execution order is proven within one module. When
  reached module-level calls (or a loop) sit in more than one module the
  order between modules depends on the entry point, and the group says so
  (`OrderProven` false).
- Markers that make the group incomplete: an associate function no
  recognised loop runs, a statement in a run associate function that is
  not an association call (a condition or loop around one, a helper
  call), an init model in the loop whose static `associate` member is
  not read, a models-object entry that is not a plain property or does
  not resolve, and every association call of the package's files outside
  both rules (the call-site cache).

`<instance>.sync(...)` call sites are listed per group (SQ-V2; no claim
depends on them): opencollective-api `scripts/db_setup.ts:47`, Gladys
none.

### Migrations (SQ-U1)

The package's `migrations/` folder, the default of sequelize-cli and of
umzug 2 (both relative to the working directory, assumed to be the
package directory), when it holds script files: one
`schema_model_migrations` marker, message `<package.json>: sequelize-cli or umzug migrations folder (not read): <dir>`
(opencollective-api `migrations`, 715 files; Gladys `server/migrations`,
81). A `.sequelizerc` (which may set `migrations-path`) is a marker; it is
not read. The server's unread-tool rule reads this marker:
`unread_tools` lists the folder with tool `sequelize-migrations`, its
script files at any depth as `files` and no marker file.

## Claim pass: models, attributes and options

`Link` runs discovery, then reads each model an instance reaches into one
table record of the kit (one model group per instance,
`models/sequelize/<instance file>`). A failure in one model is a
`schema_model_internal_error` diagnostic and never stops the others.
Groups whose options are not read (discovery says so) or whose dialect is
db2, oracle or snowflake (row D) claim nothing.

### Records

- Table identity `table:<model file>#<class name>` for `init`,
  `table:<model file>#<define name>` for `define`; `model_name` (the
  model's name: `modelName`, the class name or define's first argument)
  and `model_form` (`init` or `define`) are tool attributes. Columns keep
  the attribute key as identity name and `attribute`; `name` is the column
  (field) name.
- `dialect` is the instance's; `verification` is `oracle` on postgres and
  sqlite (developer decision 2026-11-13; step SQB5's oracle compares
  them) and `mapped` on mysql, mariadb and mssql, whose records also list
  SQ-V6. A group whose dialect is unknown (open point O3) claims only the
  dialect-independent facts (column names, column set, primary-key
  membership), as `mapped`, and lists no constraints.
- `assumptions` are SQ-V1 to SQ-V5 (plus SQ-V6 when mapped); `pins` the
  package's.
- Column facts: `type` (the catalog form on postgres, the declared type on
  sqlite, the generator's text when mapped), `nullable`, `primary_key`,
  `unique` (a single-column unique constraint or a non-partial unique
  index of the column alone), `index` (a non-unique index of the column
  alone, partial or not), `default` (`server` or `absent`), `server_default`
  and `server_default_text` (the text Sequelize writes on postgres and
  sqlite; not claimed for `literal`/`fn`, SERIAL or mapped dialects), and
  `enum` on postgres (`{name, schema, values}`, `values` null when the
  labels are not literal). `length`, `precision`, `scale` and `timezone`
  stay null (the type text carries them). `foreign_keys` follows
  "References and associations" below.

### Model forms (SQ-M1, SQ-M2, D)

- `define`: the name must be a string literal, the attributes an object
  literal and the options (if any) an object literal. `init`: the receiver
  is a class of the file with no decorators whose `extends` chain ends in
  Sequelize's `Model` (classes of the repository are followed: a project
  base class such as `ModelWithPublicId`), with one module-level `init`
  statement and none inside a function (the call-site cache), and an
  object-literal attributes argument. Anything else is a
  `schema_model_not_claimed` diagnostic (row D, with the group's table-list
  marker).
- Options: the model's options merged over the instance's `define` as
  `Utils.merge` does (`lib/utils.js:65-83`: a model value that is not
  `undefined` wins, `indexes` arrays concatenate model first,
  `lib/model.js:665-700`). Every entry must be a plain property of a key a
  row reads (`tableName`, `modelName`, `freezeTableName`, `underscored`,
  `timestamps`, `createdAt`, `updatedAt`, `deletedAt`, `paranoid`,
  `version`, `schema`, `indexes`) or has no table fact (`hooks`, `scopes`,
  `defaultScope`, `validate`, `getterMethods`, `setterMethods`, `name`,
  `sequelize`); another key or a spread makes the model D.
- Mutations after `init`: `X.removeAttribute("<key>")` makes
  that column unknown; another argument, `X.refreshAttributes()` or an
  assignment to `X.rawAttributes` / `X.tableAttributes` (in the model's
  file or factory body) makes every column unknown; the column set and the
  primary key are then partial.

### Naming (SQ-K2)

- Table name: a non-empty `tableName` literal, else the model name when
  `freezeTableName`, else `pluralize(name)` passed through `underscore`
  when `underscored` (`lib/model.js:705-709`). Options are read as
  JavaScript tests them (literal truthiness); a non-literal value makes the
  model not claimed.
- `inflection.go` ports inflection 1.13.4's `pluralize`, `singularize` and
  `underscore` (its rule tables in its order, `lib/inflection.js`) and
  Sequelize's own `camelize` (`lib/utils.js:89-91`, which throws on a
  trailing separator). The port is exact for ASCII only (JavaScript and Go
  fold case differently outside ASCII); a non-ASCII name is unknown.
  `TestInflection` compares it with inflection's own outputs for every
  model, table, attribute and association name of both corpora, the probe
  names and the fixture, each also with an `_id` suffix (1,631 inputs).
- Schema: the model's `schema`, else the instance's; claimed on postgres.
  On another dialect a schema changes the table's name (the delimiter
  form), so the model is not claimed. On postgres a name longer than 63
  bytes (truncated by postgres) is not claimed; a column name longer than
  that makes the column unknown.
- Column name: the attribute's literal `field`, else the key through
  `underscore` when `underscored` (`lib/model.js:838-840`).

### Attributes (SQ-A1, SQ-A2)

An attribute is an object literal or, as `normalizeAttribute` reads it, a
bare type. Keys read: `type`, `allowNull`, `primaryKey`, `autoIncrement`,
`unique`, `defaultValue`, `field`, `values`, `index`, `references`,
`onDelete`, `onUpdate` (SQB4) and `comment`; `validate`, `get` and `set`
have no table fact; any other key, a spread or a computed key makes the
column unknown (row D). A type outside the read list makes the column
unknown (it could be VIRTUAL); its other keys are still read,
so the model's primary key can be known.

Types (`DataTypes.X` or `X(...)` on the `DataTypes` export, the Sequelize
class or package object, or a factory parameter bound to one; generator
text per `lib/data-types.js` and the dialect's `data-types.js`):

| form | postgres (claimed: format_type) | sqlite | mysql, mariadb | mssql |
|---|---|---|---|---|
| `STRING`, `STRING(n)` | `character varying(255)`, `(n)` | `VARCHAR(255)`, `VARCHAR(n)` | `VARCHAR(255)`, `VARCHAR(n)` | `NVARCHAR(255)`, `NVARCHAR(n)` |
| `CHAR(n)` | `character(n)` | `CHAR(n)` | `CHAR(n)` | `CHAR(n)` |
| `TEXT` | `text` | `TEXT` | `TEXT` | `NVARCHAR(MAX)` |
| `INTEGER`, `BIGINT`, `SMALLINT` | `integer`, `bigint`, `smallint` | the key | the key | the key |
| `FLOAT`, `DOUBLE` | `double precision` (both) | `FLOAT`, `DOUBLE PRECISION` | `FLOAT`, `DOUBLE PRECISION` | `FLOAT`, `DOUBLE PRECISION` |
| `DECIMAL`, `DECIMAL(p, s)` | `numeric`, `numeric(p,s)` | `DECIMAL`, `DECIMAL(p,s)` | the same | the same |
| `BOOLEAN` | `boolean` | `TINYINT(1)` | `TINYINT(1)` | `BIT` |
| `DATE`, `DATEONLY`, `TIME` | `timestamp with time zone`, `date`, `time without time zone` | `DATETIME`, `DATE`, `TIME` | `DATETIME`, `DATE`, `TIME` | `DATETIMEOFFSET`, `DATE`, `TIME` |
| `UUID` | `uuid` | `UUID` | `CHAR(36) BINARY` | `CHAR(36)` |
| `JSON`, `JSONB` | `json`, `jsonb` | `JSON`; JSONB unknown | `JSON`; JSONB unknown | unknown |
| `BLOB` | `bytea` | `BLOB` | `BLOB` | `VARBINARY(MAX)` |
| `ENUM(...)`, `{type: ENUM, values}` | `enum_<table>_<column>` (quoted unless lower case; unknown in a schema or beyond 63 bytes) | `TEXT` | `ENUM('a', 'b')` (literal labels) | unknown (a CHECK replaces the column's other clauses) |
| `ARRAY(STRING / INTEGER / ENUM)` | the element's type with `[]` | unknown | unknown | unknown |
| `RANGE(DATE)` | `tstzrange` | unknown | unknown | unknown |
| `VIRTUAL`, `VIRTUAL(...)` | no column | no column | no column | no column |

Other arguments (`INTEGER(11)`, `STRING(n, true)`, `DECIMAL(p)`), other
members (`STRING.BINARY`), `new DataTypes.X(...)`, string types and
project types are not read. ENUM labels are claimed when every label is a
string literal (an attribute `values` key replaces the type's,
`lib/sequelize.js:586-591`); otherwise the type is claimed under SQ-V3 and
the labels are unknown (open point O6).

Column rules follow the generators (`attributeToSQL`,
`createTableQuery`):

- Nullability: postgres not null with `allowNull: false`, a primary key or
  autoIncrement (SERIAL); sqlite: a single primary key whose definition
  contains `INT` becomes `INTEGER PRIMARY KEY` (type `INTEGER`, nullable,
  default not claimed; INTEGER and BIGINT only), composite
  primary-key members are not null, other columns only with `allowNull:
  false`; mysql and mariadb: not null with `allowNull: false`, nullable
  otherwise, unknown for a primary key without `allowNull: false`
  (Sequelize writes no NOT NULL; the server's implicit rule is not taken
  from Sequelize's source); mssql: not null with `allowNull: false`,
  nullable when Sequelize writes `NULL` (no default, not a primary key),
  unknown otherwise (the server's ANSI_NULL_DFLT or primary-key rule
  decides).
- autoIncrement: postgres SERIAL/BIGSERIAL on INTEGER/BIGINT (type
  `integer`/`bigint`, not null, `default: server`, text not claimed;
  SMALLINT is not a compared form); sqlite `AUTOINCREMENT` on the integer
  primary key; mysql `auto_increment` on the primary key; mssql
  `IDENTITY(1,1)` on an integer column; no default is claimed for the
  generated values. More than one autoIncrement attribute, autoIncrement
  on a non-integer postgres column, with a default on postgres, on a
  sqlite primary key without INT or on a composite sqlite key: SQ-P1.
- Defaults: absent or `null` (`DEFAULT NULL`) claims `absent`; NOW, UUIDV4,
  UUIDV1 and functions are filled by the client (`server_default: false`,
  `default` not claimed); a string on STRING/CHAR/TEXT, on ENUM when it is
  one of the literal labels (else Sequelize's validation throws: SQ-P1),
  a number on the integer types (integers only), FLOAT/DOUBLE (quoted on
  postgres and sqlite) and DECIMAL, a boolean on BOOLEAN (`1`/`0` on
  sqlite), and any literal on JSON/JSONB (`JSON.stringify`)
  claim `server` with the text Sequelize writes; `literal("...")` and
  `fn(...)` claim presence only (`fn` on sqlite is SQ-P1). mysql and
  mariadb write no DEFAULT on TEXT, BLOB, JSON and GEOMETRY (`absent`).
  Any other value or combination leaves the default unknown (open point
  O5 for enum members).
- Rewriting guards: Sequelize's `createTableQuery` and its regular
  expressions test the column definition text (`includes("PRIMARY KEY")`,
  sqlite `includes("INT")`, `SERIAL`, the postgres `COMMENT ` split and
  `/^ENUM\(.+\)/`, sqlite's `replaceBooleanDefaults`). A default, label,
  comment (mapped dialects) or referenced table text containing one of
  those markers, a sqlite string default starting with `true`/`false`, and
  a postgres ENUM column with a `references` key or a `)` in its default
  make the column unknown.
- Uniqueness: `unique: true` is an inline UNIQUE (dropped by sqlite's
  integer primary-key rewrite: not claimed); attributes sharing a group
  name (a string or `{name}`, an optional `msg`) form one `UNIQUE
  (columns)` when the last member's `unique` is not `true`
  (`lib/model.js:854-869`); `{msg}` alone is unknown. Constraint names are
  never claimed. An attribute that is not read may join a
  group, so groups are then not listed.

### Attribute helpers (SQ-A1, form "helper member")

T018g3 Revision 2 (open point O4; `helpers.go`): an attribute value
`H(args).member` or `H(args)` whose callee `H` is an identifier that
resolves (imports through the module resolver, `require` through the
CommonJS reach) to a module-level function is read as the value the
helper returns, when:

- the function is a function declaration (plain, `export` or `export
  default`; payload row S6), the `export default` expression, a
  `module.exports = F` or an `exports.<name> = F`, F a non-async,
  non-generator function or arrow (a function bound by `const` is not in
  the payload and stays unknown);
- its body is a single `return` of an object literal (or an arrow's
  object literal body);
- `member` is a plain property of that object that no later spread or
  unread entry can override (for `H(args)`, the whole object);
- the call passes one argument per parameter, each Sequelize's DataTypes
  (`DataTypes`, the Sequelize class or package object, a factory
  parameter bound to one) or a string, number, boolean or `null` literal,
  and a DataTypes parameter is used only as `<p>.X` in the member.

The member is then read as if written at the call site with each
parameter replaced by the call's argument; every other name in it (a
constant of the helper's module) is unknown, so a fact that needs it is
unknown (`validate`, `get` and `set` have no table fact). The facts are the
substituted value's, with its verification. Any other helper leaves the
column unknown (row SQ-A1), with the reason naming the helper. The helper
module needs a payload: opencollective-api's `server/models/DataTypes.ts`
imports nothing from Sequelize, so payload gate row G9 (version 3) admits
attribute helper modules. On opencollective-api the 7
`CustomDataTypes(DataTypes).currency` columns are read (`STRING(3)`, not
null, default `'USD'`).

### Default attributes (SQ-O1)

The implicit `id` (INTEGER, primary key, autoIncrement, not null) is
prepended when every attribute's `primaryKey` is known and none is set
(an `id` attribute then makes Sequelize throw: SQ-P1); `createdAt` and
`updatedAt` (DATE, not null) and, with `paranoid`, `deletedAt` (DATE,
nullable) follow `timestamps` (default true) and the string/false
options; `version` adds an INTEGER not null default `0`. A user attribute
with the same key wins. With an unread attributes entry the
default attributes are unknown columns.

### Indexes (SQ-O2)

`indexes` elements are object literals with `fields` (string literals or
`{name, order}`), `unique`, `name`, `using` (postgres access methods,
lower-cased; `btree` when absent) and a non-empty `where` object (partial;
the predicate is not claimed; not mapped on mysql, mariadb and mssql).
Fields are column names (Sequelize does not map attribute keys); a field
that is no column is SQ-P1. The name is the literal or
`underscore("<table>_<fields>")` (`lib/utils.js:413-421`),
unknown beyond 63 bytes on postgres. A JSONB attribute with `index: true`
adds a `gin` index named `underscore("<table>_<column>")`;
`index` on another type is not compared (the column's `index` fact is
unknown). Other keys make that index unknown (constraint set partial).

### Provable failures read here (SQ-P1)

`schema_model_import_failure` (warning, row named, the table-list marker)
and no table for: an attribute without a type, an ENUM without values,
an ENUM label that is not a string literal on postgres, a default outside
the ENUM's labels on postgres, mysql and mariadb (Sequelize does not
validate it, `typeValidation` is off by default, `lib/sequelize.js:129`;
those databases reject it, and sqlite writes the ENUM as `TEXT` and takes
it: claimed there), an `id` attribute without `primaryKey` when no
attribute is a primary key, an empty or non-string/boolean timestamp
option, two attributes with one column name, a VIRTUAL attribute with
`unique`, the autoIncrement cases above, a `fn` default on sqlite, an
index without fields or on a missing column, two indexes of a table with
one name, and, across the group, two models with one table name or one
explicit index name in one schema (one database on sqlite). The
reference and association failures follow below.

## Claim pass: references and associations

`read` (`tables.go`) follows Sequelize's own order: every model as `init`
or `define` leaves it (attributes, default attributes, mutations), then
the reached association calls in execution order (`assoc.go`), then each
table's columns, indexes and uniques as `sync` writes them, the group's
failures, and the foreign-key lists with the cyclic rule (`refs.go`).

### References (SQ-A3)

An attribute's `references: {model, key}` (an object literal whose keys
are `model` and `key`; any other key, such as `deferrable`, leaves the
foreign key unknown) with its `onDelete` / `onUpdate` keys. `model`: a
string literal names a table, resolved in the referencing table's schema
on postgres (`dialects/postgres/query-generator.js:378-390`); a reached
model class or module-level define binding names that model's table
(init replaces it by `getTableName()`, `lib/model.js:731-733`, so a
target with a schema is schema-qualified). Anything else leaves the list
unknown. `key` absent: `id`. Each column's `foreign_keys` holds one
entry (`table`, `schema`, `column`, `local_columns`, `columns`,
`on_delete`, `on_update`, `name` null: the database names it) or `[]`
when the attribute has no `references` (the list is then proven empty);
actions are the written string upper-cased (one of CASCADE, SET NULL,
SET DEFAULT, RESTRICT, NO ACTION; anything else, or a non-literal, makes
the list unknown), else `NO ACTION` (Sequelize writes no clause).

On mssql (Revision 3) the generator changes the actions:
`attributesToSQL` (`dialects/mssql/query-generator.js:542-561`) walks the
table's attributes in order and, for each with references, clears
`onUpdate` on the first reference to a table (`references.model` as the
generator sees it: a table name, mssql tables having no schema here) and
both actions on every later reference to the same table;
`attributeToSQL` (`:488-494`) clears both on a reference to the model's
own table. A cleared action writes no clause, so `NO ACTION` applies
(`M.belongsTo(U, {as: "author", onDelete: "CASCADE", onUpdate:
"CASCADE"})`, then `{as: "editor", onDelete: "CASCADE"}`, then
`M.belongsTo(M, {as: "parent", onDelete: "CASCADE"})`: CASCADE/NO ACTION,
NO ACTION/NO ACTION, NO ACTION/NO ACTION; a plain `G.belongsTo(O)`: SET
NULL/NO ACTION). An attribute before it whose references target is not
read (a column that is not read, an unread entry of the attributes
object, a references model that does not resolve) may be an earlier
reference to the same table, so the later lists are unknown.
`TestDDLRendering` compares these with the generator's own text.

The referenced column must be the target table's only primary-key
column or a unique column (Revision 3): postgres, mysql,
mariadb and mssql reject a foreign key to any other column. A key the
target's facts prove to be neither (the column is read, the primary key
is known and the unique fact is claimed false) is a provable failure
(below); a key not proven either way leaves the list unknown; sqlite
accepts the declaration, so its list is unknown there. This covers an
attribute's `references.key`, a belongsTo `targetKey` and a hasOne or
hasMany `sourceKey` alike.

### Associations (SQ-R1)

`belongsTo` (holder: the source), `hasOne` and `hasMany` (holder: the
target), each with no options or an object literal of `foreignKey` (a
string or `{name, allowNull, field}`), `as` (a string), `targetKey`
(read by belongsTo), `sourceKey` (read by hasOne and hasMany; the method
that does not read one ignores it, as Sequelize does), `onDelete`,
`onUpdate`, `constraints` and `scope` (no table fact). The call builds
one attribute (`associations/belongs-to.js:70-85`,
`has-one.js:73-88`, `has-many.js:96-112`):

- name: `foreignKey`, else `camelize(<as or the target's singular>_<target primaryKeyAttribute>)`
  (belongsTo: the primary key even with a `targetKey`, probe: `makerId`),
  `camelize(singularize(<as or the source's model name>)_<source primaryKeyAttribute>)`
  (hasOne), `camelize(<source's singular>_<source primaryKeyAttribute>)`
  (hasMany); the singular is `singularize(model name)` unless the model
  has a `name` option (then unknown);
- type: the key attribute's (`targetKey` / `sourceKey`, else the primary
  key) type; `allowNull` the foreignKey object's, else `true`; column
  name: the object's `field`, else the name through `underscored`;
- references to the other model's table (`getTableName()`) and key
  column unless `constraints: false`, or the key is a member of a
  composite primary key (`associations/helpers.js:9-21`; the attribute is
  still added); `onDelete` as written, else `SET NULL` when the holder's
  attribute (existing, else new) has a truthy `allowNull`, else
  `NO ACTION` (belongsTo) or `CASCADE` (hasOne, hasMany);
  `onUpdate` as written, else `CASCADE`.

The attribute is merged into the holder with `Utils.mergeDefaults`
(`lib/utils.js:54-63`): a new key is appended; an existing attribute
keeps every value it has and gains the keys it lacks (`allowNull`,
`references` or a missing `references.key`, `onDelete`, `onUpdate`),
first association first. Facts an association sets are
claimed by SQ-R1 at the call's site (a merged `allowNull` claims the
column's nullability). When the order between modules is not proven
(`OrderProven` false), an attribute associations of two modules touch is
unknown.

Not read, with what they make unknown: a call whose holder does not
resolve, discovery's association markers, or options that are not an
object literal (or hold a key the row does not list, or a foreignKey
object key other than name/allowNull/field): every column of every table
(or of the holder) is unknown, as any attribute key may be merged in; a
call whose other model is not claimed, a key name, key attribute or
primary key that is not known, `constraints` that is not a literal, or
`constraints: false` together with an action (Sequelize still writes
references; not compared): the holder's column set, the nullability of
its columns without `allowNull` and every foreign-key list are unknown.
When an unread attribute of the other model could make its primary key
composite (an unread entry before a known primary key makes the key
itself unknown), only the new or merged attribute's foreign-key list is
unknown. An attribute whose value is a `new` expression is never a
primary key (`normalizeAttribute`, `lib/sequelize.js:574-577`).

### Join tables (SQ-R2)

`A.belongsToMany(B, {through: "<table>", foreignKey?, otherKey?})` with
A not B, the only belongsToMany over that string in the group (no
non-literal through either), no model of the group named like it, and no
other option: a join table `<through>` (identity
`table:<call file>#<through>`, `model_form: "belongsToMany"`) defined
from A's options (`belongs-to-many.js:95-105`): `underscored`, the
`createdAt` / `updatedAt` names and A's schema; timestamps from the
instance's global `define` (`timestamps`, default true; a source with a
`version` option is not read). Columns: `createdAt`, `updatedAt` (not
null), then the two keys (`foreignKey`, else
`camelize(<A's singular>_<A's primaryKeyAttribute>)`; `otherKey`, else the
same for B), typed as the primary keys, together the primary key, each a
foreign key `CASCADE` / `CASCADE` to its model's key column
(`belongs-to-many.js:186-247`). Every fact is claimed by SQ-R2. A
refused string through (self-association, a second call, another key, a
model named like it) is a `schema_model_not_claimed` marker naming the
join table with the table-list suffix. `through: <model>` (or
`{model}`): the model's attributes named by the keys the call assigns
(the keys above; with A = B, `singularize(as)` for the other key) are
unknown, its implicit `id` (which the call deletes) is unknown, its
primary key is open, its column set partial when a key is new, and its
constraint set partial (a unique group over the keys may be added); when
the keys are not known, every attribute is unknown.

### Cyclic groups

The group's graph is Sequelize's `getModelsTopoSortedByForeignKey`
(`lib/model-manager.js:30-67`): nodes are the models' tables
(`<schema>.<table>` with a schema), edges the attributes' `references`
models (a string as written), self-edges ignored. With a cycle among the
edges read the group is cyclic; without one, a model that is not
claimed, an attribute or references object that is not read, an
association that is not read, a join table that is not claimed or a
definition marker leave it possibly cyclic, treated alike. `sync` then
creates every table without foreign keys and adds them in a per-model
`alter` pass (`lib/sequelize.js:385-400`, `lib/model.js:946-985`), which
skips primary-key columns and stops at the first column whose
changeColumn SQL postgres rejects (`dialects/postgres/query-generator.js:209-245`).
On postgres: a foreign key on a primary-key column is unknown, and every
foreign key of a model is unknown when one of its columns may fail: an
ARRAY of ENUM, a DEFAULT followed by UNIQUE or REFERENCES (`SET DEFAULT`
takes the rest of the definition), a SERIAL column that is not the
primary key, or a column that is not read (except one removed by
`removeAttribute` and a `new DataTypes.VIRTUAL(...)` value, which have
no column). On mysql, mariadb and mssql the alter pass is not mapped:
every foreign key is unknown. sqlite syncs every model in order with
foreign keys off, as an acyclic group (no change).

### Provable failures (SQ-P1)

Also refused (`schema_model_import_failure`, no table): a references key
naming a column the referenced table of the group (complete, every
column read) does not have (`references ... (nope)`); an association
from a model in a schema to a target without one on postgres (Sequelize
writes `REFERENCES "<schema>"."<target>"`; also a join
table whose source has a schema and target has none); a `targetKey`
(belongsTo) or `sourceKey` (hasOne) that is not an attribute of a model
whose column set is complete (Sequelize throws); a referenced column that
is provably neither the target's primary key nor unique (not sqlite;
for example `R.belongsTo(T, {as: "byCode", targetKey: "code"})` with
`code` not unique, postgres rejects the foreign key and `Rs` is not
created); and, repeated until nothing changes, a table whose references
name a table of the group that sync does not create (a refused table, or
a model a load-time throw stops; not sqlite, where the list is unknown).
A table whose own CREATE TABLE succeeds (a later CREATE INDEX fails, or
another model of that name created it) is refused without counting as
missing.

SQ-P1's refusal column lists every failure the code refuses
(`failures.go`; `TestSequelizeFailureKinds` keeps the column and the
code's failure kinds 1:1).

**Load order.** A failure marked (load) throws while the
models load: `init` or `define` throws, or an association call does.
Loading stops there, so a definition or association call after it in the
same module never runs, and one in another module, or in a module whose
load order to it is not proven (the order between modules depends on the
entry point and on what each module requires), may not. Positions: a
module-level statement of its file; a factory's define runs at the
factory call's position; an associate function's calls run at the loop's
position in the loop's order. A definition after the throw, or not
proven before it, is not claimed (`schema_model_not_claimed`, row SQ-P1,
with the reason; the table counts as missing for the rule above); an
association call proven after it is skipped (it never runs), and one not
proven either way makes what it may add to its holder unknown (as an
association that is not read; a join table it may define is not
claimed). The throws found while reading the models (including more than
one `autoIncrement` attribute and an index without `fields`) order the
association calls; an association call that throws orders the calls and
the models after it.

**All-or-nothing foreign keys.** The foreign-key step
(each column's list, mssql's actions and the cyclic rule) runs as one
guarded step per group: when it fails part way, every list it wrote is
made unknown with the step's reason, so no list survives without the
rules that follow it (`TestForeignKeyStepAllOrNothing` injects a panic).

**Partial tables.** A table whose column or constraint
set is partial always carries a table-level reason for it: when only a
column that is not read made it partial, the reason names the column.

## Assumptions

Copied from the task file's "Environment" (ids and text identical);
each group's records list them (`assumptions`) with its `pins`:

- **SQ-V1 Pins.** The `sequelize` and `inflection` versions in the
  package's lockfile are the ones that created the database
  (`pins: {"sequelize": "<v>", "inflection": "<v>"}`).
- **SQ-V2 Schema deployed.** The database was created by the models'
  `sync` or by migrations that match the models (sequelize-cli or umzug
  migrations are not read). Gladys never calls `sync` (umzug migrations
  only); opencollective-api calls it in a dev setup script only.
- **SQ-V3 Models load.** Module side effects do not change the models
  beyond what is read, and values the rules do not read (ENUM label lists
  from imported constants) are valid on the dialect.
- **SQ-V4 Dialect.** The database is of the instance's literal `dialect`,
  or of the dialect the package's single installed Sequelize driver
  implies (open point O3).
- **SQ-V5 Global define.** The instance has no `define` options beyond the
  ones read (open point O1).
- **SQ-V6 Mapped dialects.** MySQL, MariaDB and MSSQL facts follow
  Sequelize's query generator as its source states at the pinned version
  (never `oracle`).

## Claim contract

Copied from `docs/contract/claims/sequelize-models.md` (ids and text
identical); `contract.go` holds the same rows, and `TestSequelizeContract`
checks the three against each other. The dialect rule (oracle for
postgres and sqlite, mapped for mysql, mariadb and mssql) is the task
file's; the mapped sources are cited below.

| id | form | claim | refusal / unknown | assumptions |
|---|---|---|---|---|
| SQ-K1 | scope (Discovery above): package dependency on `sequelize` 6.x, lockfile pins, the module-level `new Sequelize(...)` instance, its literal `dialect` or the single-driver rule, literal `define`/`schema` options, model reach (an imported instance binding as `init`'s `sequelize` option or `define` receiver; a factory's first parameter called with the instance through a relative `require`), association reach (module level; the recognised `associate(models)` loop; Revision 3, the sequelize-cli loop: `Object.keys(M).forEach`, `Object.values(M).forEach` or `for...of` over `Object.keys(M)` / `Object.values(M)` whose body is `R.associate && R.associate(M)` or `if (R.associate) R.associate(M)`, an init class's `static associate(models)`, a models object entry that is a `require(...)(...)` factory call, and models object assignments `M.X = ...` before the loop), the options `require(config)[env]` (open point O2's rule: read when every entry agrees) | the group (`models/sequelize/<instance file>`), its model set, `dialect`, `pins`, the global `define` options | another major: marker, no claims; dialect unknown (no or several drivers, non-literal without a driver rule): dialect-dependent facts unknown; constructor options with a spread or a non-literal `define`: SQ-V5 per open point O1, else every option-derived fact unknown; instance hooks: D; a model whose receiver does not resolve, or that reaches two instances: not claimed (marker); directory loaders: D | SQ-V1, SQ-V4, SQ-V5 |
| SQ-K2 | naming: model name (`define`'s literal first argument; `init`'s `modelName` option, else the class name); table name: `tableName` literal, else the model name when `freezeTableName`, else `inflection.pluralize(name)` (1.13.4), passed through `inflection.underscore` when `underscored` (`lib/model.js:705-709`); `schema` option (postgres); column name: the attribute's literal `field`, else the attribute key, passed through `inflection.underscore` when `underscored` (`lib/model.js:838-840`) | table `name` and `schema`, column names (`Person` -> `People`, `Mouse` -> `Mice`, `Archived` -> `Archiveds`, `BlogPost` underscored -> `blog_posts`; freeze ignores `underscored`) | a non-literal model name, `tableName`, `field`, `freezeTableName` or `underscored`: the names derived from it unknown (table or column not claimed); `schema` on sqlite/mysql: unknown | SQ-V1 |
| SQ-M1 | `<instance>.define("Name", {attributes}, {options}?)` at module level of a reached module, or as a statement of a reached factory's body; option keys with no table fact: `hooks`, `scopes`, `defaultScope`, `validate`, `getterMethods`, `setterMethods` | a model (table) | non-literal name, attributes or options object: D; `define` in any other function (opencollective-api `Temporal`): D (marker naming it; the group's `table_list` is `may_be_incomplete`) | SQ-V2, SQ-V3 |
| SQ-M2 | `class X extends Model` (from `sequelize`, or a class of a reached module whose chain ends in `Model`, e.g. `ModelWithPublicId`) with exactly one unconditional module-level statement `X.init({attributes}, {sequelize, ...options})`; same no-fact option keys as SQ-M1 | a model named by `modelName` or the class name | more than one `init` site, `init` in a function or a condition, a class chain not ending in Sequelize's `Model`: D; a later `X.removeAttribute(...)` or `rawAttributes` mutation: the table is partial (column set and primary key unknown) | SQ-V2, SQ-V3 |
| SQ-A1 | attribute type: `{type: DataTypes.X(args)}` or the shorthand `key: DataTypes.X` (`DataTypes` or `Sequelize` from `sequelize`, the factory's second parameter, or a re-export of either), from a closed per-dialect list the oracle has run (the investigation: `STRING`, `STRING(n)`, `CHAR(n)`, `TEXT`, `INTEGER`, `BIGINT`, `SMALLINT`, `FLOAT`, `DOUBLE`, `DECIMAL`, `DECIMAL(p, s)`, `BOOLEAN`, `DATE`, `DATEONLY`, `TIME`, `UUID`, `JSON`, `JSONB` (postgres), `BLOB`, `ENUM(...)` / `{type: ENUM, values}`, `ARRAY(T)` and `RANGE(DATE)` (postgres)); `VIRTUAL`; the helper member (Revision 2, open point O4): an attribute value `H(args).member` or `H(args)`, `H` an identifier resolving (module resolver, CommonJS reach) to a module-level function (a function declaration, plain, `export` or `export default`; `export default F`; `module.exports = F` or `exports.<name> = F`; F a non-async, non-generator function or arrow) whose body is a single `return` of an object literal, `member` (or the whole object) a plain property that no later entry can override, read as the attribute value this row and SQ-A2 read once each parameter is replaced by the call's own argument (`DataTypes` or `Sequelize` as above, used only as `<p>.X`, or a string, number, boolean or `null` literal; one argument per parameter) | column type as the catalog shows it: postgres `character varying(255)`, `timestamp with time zone`, `double precision` (FLOAT and DOUBLE), `numeric(p,s)`, `bytea`, `enum_<table>_<column>` (quoted when not lower case) and `[]` for arrays, `tstzrange`; sqlite `VARCHAR(255)`, `DATETIME`, `TINYINT(1)`, `DOUBLE PRECISION`, `TEXT` for ENUM, `UUID`, `JSON`; enum labels in order when every value is a string literal; `VIRTUAL`: no column; a helper member (`CustomDataTypes(DataTypes).currency`): the facts of the substituted value, with its verification | a type outside the list, a project class (`new X()` of `DataTypes.ABSTRACT` subclasses), any other helper result (a body other than one `return` of an object literal, a member a spread or later entry can override, a computed member, a parameter used other than as the DataTypes namespace, an argument that is neither DataTypes nor a literal, a function bound by `const`, an async or generator function; a name the helper's module binds is not resolved, so a fact that needs it is unknown), a string type (raw SQL), a `new DataTypes.X(...)` form: the column itself unknown (an unread type could be `VIRTUAL`), table column set not complete; non-literal ENUM values: type claimed (the enum type name), labels unknown | SQ-V1, SQ-V3 |
| SQ-A2 | column facts of a literal attribute object: `allowNull`, `primaryKey`, `autoIncrement`, `unique` (`true`, a literal group name, `{name}`), `defaultValue`, `index: true` on JSONB; keys with no table fact: `validate`, `get`, `set`, `comment` | nullability: postgres not null when `allowNull: false` or a primary key; sqlite (`dialects/sqlite/query-generator.js:36-75`) a single primary key whose type contains `INT` becomes `INTEGER PRIMARY KEY` (nullable in the catalog, type `INTEGER`), composite primary-key members are not null, any other column only with `allowNull: false`; primary key columns; `autoIncrement` on postgres: `SERIAL`/`BIGSERIAL` (integer, `nextval` default); unique: single-column unique, or composite unique over the columns sharing a group name (the group name is not the constraint name); default: string, number, boolean literal (sqlite booleans `1`/`0`), `null` as no default, any literal on JSON/JSONB as `JSON.stringify` text, `sequelize.literal("...")` / `fn(...)`: a database default is present (its text is not claimed: Postgres re-renders it, and the oracle compared presence only); `NOW`, `UUIDV4`, `UUIDV1`: no database default; `index: true` on JSONB: a `gin` index named `underscore("<table>_<column>")` (`lib/model.js:883-889`) | a non-literal value: that fact unknown; an enum-member default (`X.PENDING`, open point O5): default unknown; `fn(...)` default on sqlite: SQ-P1; constraint names: never claimed | SQ-V2 |
| SQ-A3 | attribute `references: {model: "table" or a reached model class, key}` with `onDelete` / `onUpdate` | foreign key to that table and column (`key` absent: `id`), actions as written upper-cased, else `NO ACTION` (the database default; Sequelize writes no clause); on mssql (Revision 3) the actions its attributesToSQL keeps: the first references to a table loses `onUpdate`, a later one to the same table and a self-reference lose both (`NO ACTION`; `dialects/mssql/query-generator.js:488-494`, `:542-561`) | non-literal `model` or `key`: foreign key unknown (table partial); postgres in a cyclic group: a foreign key on a primary-key column unknown, every foreign key of a model with an alter-failing attribute (`ARRAY(ENUM)`; a `literal`/`fn` default together with `unique: true`) unknown; also unknown (accepted in Revision 2, no claim change): in that cyclic rule a plain default with `unique: true`, a default with `references`, a SERIAL column that is not the primary key and a column that is not read are alter-failing, and a group that may be cyclic (a model, attribute, association or join table not read, a definition marker) is treated as cyclic; on mysql, mariadb and mssql every foreign key of a cyclic or possibly cyclic group; a foreign key to a target whose primary key may be composite (an attribute not read); a composite-key target: no foreign key (the attribute only, `[]`); a referenced column not proven to be the target's only primary-key column or a unique column: foreign key unknown (provably neither: SQ-P1); on mssql a reference after an attribute whose references target is not read: foreign key unknown (R3-A1) | SQ-V2 |
| SQ-O1 | default attributes: implicit `id` (`INTEGER`, primary key, auto increment) when no attribute is a primary key, prepended; `timestamps` (default true) with `createdAt` / `updatedAt` / `deletedAt` (string or `false`), `paranoid`, `version` (`lib/model.js:188-239`) | the `id`, `createdAt`, `updatedAt` (`DATE`, not null), `deletedAt` (`DATE`, nullable, with `paranoid`), `version` (`INTEGER` not null default `0`) columns, names through SQ-K2; a user attribute of the same name wins entirely | non-literal `timestamps`/`paranoid`/timestamp names: those columns unknown; an incomplete attribute set (spread, unread entry): implicit `id` unknown | SQ-V5 |
| SQ-O2 | option `indexes: [{fields, unique, name, using, where}]` with literal string fields or `{name, order}` objects | index name (omitted: `inflection.underscore("<table>_<fields>")`, `lib/utils.js:413-421`), columns, unique, method (`using`), partial (`where` present) | a non-literal element or field: that index unknown (table partial); predicate text: unknown (not compared) | SQ-V2 |
| SQ-R1 | `A.belongsTo(B, opts)`, `A.hasOne(B, opts)`, `A.hasMany(B, opts)` with literal options (`foreignKey` string or `{name, allowNull, field}`, `as`, `targetKey`/`sourceKey`, `onDelete`, `onUpdate`, `constraints`; `scope` has no table fact), in reach order | the foreign-key attribute on the holder (belongsTo: source; hasOne/hasMany: target): name `foreignKey`, else `camelize(as or B's singular + "_" + B's key)` (belongsTo), `camelize(A's singular + "_" + A's key)` (hasMany), `camelize(singularize(as or A's name) + "_" + A's key)` (hasOne uses `as`); a new attribute has the key's type and is nullable; merged into an existing attribute without overriding any value it already has, first association first; unless `constraints: false`, a foreign key with `onDelete` as written, else belongsTo `SET NULL` only when the attribute's `allowNull` is truthy and `NO ACTION` otherwise (an omitted `allowNull` gives `NO ACTION` on a nullable column), hasOne/hasMany `SET NULL` or `CASCADE` likewise; `onUpdate` as written, else `CASCADE` (`associations/belongs-to.js:77-80`, `has-many.js:103-106`, `has-one.js:78-81`); on mssql the actions as SQ-A3 states | non-literal options or targets, associations outside the reach rule: the facts they may add unknown (holder's column set not complete); `constraints: false` keeps an attribute's own `references`; a cross-schema association on postgres: SQ-P1; postgres cyclic group: as SQ-A3; a `targetKey` / `sourceKey` not proven unique: as SQ-A3 | SQ-V2 |
| SQ-R2 | `A.belongsToMany(B, {through: "table", foreignKey?, otherKey?})` with a string `through`, `A` not `B`, and no other `belongsToMany` over the same `through` | a join table named `through`: `createdAt`/`updatedAt` first (from the source's timestamps), then the two keys (`camelize(singular + "_" + key)` unless given), both not null, together the primary key, each a foreign key `CASCADE`/`CASCADE` to its model (`associations/belongs-to-many.js:186-247`) | `through: Model` or `{model, ...}`, self-associations, paired associations over one `through` (Sequelize deletes and rewrites keys, `belongs-to-many.js:70-130`): the facts it may add to the through table unknown (its column set not complete) | SQ-V2 |
| SQ-P1 | failures loading the models or `sync` would hit, provable from the source (the refusal column lists each; `lib/model.js:191-193`, `:247-258`, `:602-604`, `:715-717`, `:741-761`, `lib/sequelize.js:592-594`, `associations/belongs-to.js:51-53`, `has-one.js:53-55`; the database's rejections by probe or oracle) | diagnostic `schema_model_import_failure` (row named); no claim for the models involved | refused (Revision 3: each failure the code refuses): (1) two models of the group with one table name; (2) two indexes of a table, or of two tables in one schema, with one explicit name; (3) an `id` attribute without `primaryKey` when no attribute is a primary key (load); (4) an attribute without a type (load); (5) an ENUM without values (load); (6) a `createdAt`, `updatedAt` or `deletedAt` option that is an empty string, `null` or a number (load); (7) more than one `autoIncrement` attribute (load); (8) an index without `fields` (load); (9) a belongsTo `targetKey` or hasOne `sourceKey` that is not an attribute of the other model (load); (10) an ENUM label that is not a string literal on postgres; (11) a default outside the ENUM's labels on postgres, mysql and mariadb; (12) a `fn(...)` default on sqlite; (13) a VIRTUAL attribute with `unique`; (14) two attributes with one column name; (15) `autoIncrement` on a type other than INTEGER, BIGINT or SMALLINT on postgres; (16) `autoIncrement` with a default on postgres; (17) `autoIncrement` on a sqlite primary key that is not the single INTEGER PRIMARY KEY; (18) an association or join table from a model with a `schema` to a target without one on postgres; (19) `references` naming a column the referenced table does not have; (20) an index naming a column the table does not have; (21) a `references` key, belongsTo `targetKey` or hasOne/hasMany `sourceKey` that is provably neither the primary key nor a unique column of the other table (not sqlite); (22) `references` to a table of the group that sync provably does not create (not sqlite); a failure marked (load) throws while the models load: the definitions and association calls after it in its module, and those of modules not proven to load before it, never run or may not (not claimed); other failures: assumed absent (SQ-V3) | SQ-V3 |
| SQ-U1 | sequelize-cli / umzug migrations folders of the package | `unread_tools` entry (found, not read yet) | always unread | - |
| SQ-V | assumptions SQ-V1..SQ-V6 attached to every record of the group | stated, never claimed as facts | - | - |
| D | anything else: sequelize-typescript (`@Table`, `@Column`, ... ; developer decision 2026-11-13, capture moves to T018g6), other Sequelize majors, project wrappers (`Temporal(X, sequelize)`), hooks that change attributes, instance hooks (Revision 3: `addHook` or a hook-type method such as `beforeDefine` on the instance or on Sequelize's class, or a `hooks` key in the constructor options: every option- and attribute-derived fact of the group unknown, the group not claimed), `addScope`, `queryInterface` calls, models built in loops or functions (a class + `init` inside a CommonJS factory, the sequelize-cli model template: a marker), a model of another package on the instance (a marker on the instance's group), `sequelize.import`, `init`/`define` with computed attributes, directory loaders | nothing; the table or column is partial with `D` as reason, or a marker diagnostic `schema_model_unread` (tool `sequelize`) for whole definitions | always | - |

## Mapped dialects (SQ-V6)

MySQL, MariaDB and MSSQL facts are `verification: "mapped"`: taken from
Sequelize's data types and query generators at the pin, never from a
database (no driver is installed, so no rendered reference exists).
Base types are `lib/data-types.js`; a dialect's class replaces the base
one when `normalizeDataType` finds it in the dialect's `DataTypes`
(`lib/sequelize.js:558-573`; `dialects/mysql/index.js:7`, `:55`;
`dialects/mariadb/index.js:8`, `:56`; `dialects/mssql/index.js:7`, `:59`).
Type keys come from the export names (`lib/data-types.js:599-645`;
`DOUBLE`'s key is `DOUBLE PRECISION`, the first name it is exported
under, `:630`, `:643`).

| type | mysql, mariadb | mssql | source |
|---|---|---|---|
| `STRING`, `STRING(n)` | `VARCHAR(255)`, `VARCHAR(n)` | `NVARCHAR(255)`, `NVARCHAR(n)` | `data-types.js:47-60`; `mssql/data-types.js:49-55` |
| `CHAR(n)` | `CHAR(n)` (`CHAR(255)` without n) | the same | `data-types.js:79-89` |
| `TEXT` | `TEXT` | `NVARCHAR(MAX)` | `data-types.js:90-106`; `mssql/data-types.js:67-77` |
| `INTEGER`, `BIGINT`, `SMALLINT` | the key, with `(n)`, `UNSIGNED`, `ZEROFILL` when given | the key; options dropped | `data-types.js:127-157`, `:192-207`; `mssql/data-types.js:5-13`, `:104-127` |
| `FLOAT`, `DOUBLE` | `FLOAT`, `DOUBLE PRECISION` (with `(n[,d])` when given) | `FLOAT`, `DOUBLE PRECISION`; decimals, unsigned and zerofill dropped on `FLOAT` | `data-types.js:143-157`, `:208-228`; `mssql/data-types.js:140-157` |
| `DECIMAL`, `DECIMAL(p, s)` | `DECIMAL`, `DECIMAL(p,s)`, then `UNSIGNED`, `ZEROFILL` | `DECIMAL`, `DECIMAL(p,s)` | `data-types.js:229-238`; `mysql/data-types.js:29-40`, `mariadb/data-types.js:29-40` |
| `BOOLEAN` | `TINYINT(1)` | `BIT` | `data-types.js:268-271`; `mssql/data-types.js:79-83` |
| `DATE` | `DATETIME`, `DATETIME(n)` | `DATETIMEOFFSET` | `data-types.js:300-309`; `mysql/data-types.js:41-44`, `mariadb/data-types.js:41-44`; `mssql/data-types.js:94-98` |
| `DATEONLY` | `DATE` | `DATE` | `data-types.js:347-350` |
| `TIME` | `TIME` | `TIME` | `data-types.js:295-298` |
| `UUID` | `CHAR(36) BINARY` | `CHAR(36)` | `mysql/data-types.js:72-76`, `mariadb/data-types.js:69-73`; `mssql/data-types.js:84-88` |
| `JSON` | `JSON` | unknown (`JSON` is not supported, `dialects/abstract/index.js:61`) | `data-types.js:378-385`, `mysql/index.js:49` |
| `BLOB` | `BLOB` (`TINYBLOB`, `MEDIUMBLOB`, `LONGBLOB` by length) | `VARBINARY(MAX)` (`VARBINARY(256)` for tiny) | `data-types.js:390-406`; `mssql/data-types.js:34-48` |
| `ENUM(...)` | `ENUM('a', 'b')` (values escaped, in order) | `VARCHAR(255)` with `CHECK ("col" IN('a', 'b'))` | `mysql/data-types.js:103-107`, `mariadb/data-types.js:95-99`; `mssql/data-types.js:158-162`, `mssql/query-generator.js:496-503` |
| `JSONB`, `ARRAY(T)`, `RANGE(DATE)` | not mapped (postgres only) | not mapped | - |

Column clauses, `attributeToSQL` (MariaDB's generator extends MySQL's,
`mariadb/query-generator.js:18-20`, `:68`):

| fact | mysql, mariadb (`mysql/query-generator.js`) | mssql (`mssql/query-generator.js`) |
|---|---|---|
| not null | `allowNull: false` adds `NOT NULL` (`:321-323`) | `NOT NULL`; otherwise `NULL` unless a primary key or a schemable default (`:506-510`) |
| auto increment | `auto_increment` (`:324-326`) | `IDENTITY(1,1)` (`:511-513`) |
| default | `DEFAULT <escaped>` when schemable, never on `BLOB`, `TEXT`, `GEOMETRY`, `JSON` or a binary type (`:43`, `:327-329`) | `DEFAULT <escaped>` when schemable, not on a binary type (`:514-516`) |
| unique | `unique: true` adds `UNIQUE` (`:330-332`); groups in `createTableQuery` (`:117`) | `UNIQUE` (`:517-519`); groups as `CONSTRAINT ... UNIQUE` (`:164`) |
| primary key | `PRIMARY KEY` (`:333-335`), moved into a table clause (`:91-99`, `:122`) | `PRIMARY KEY` (`:520-522`), table clause (`:139-147`, `:169`) |
| references | `REFERENCES <table> (<key or id>)`, `ON DELETE` / `ON UPDATE` upper-cased when given (`:345-363`) | the same (`:523-536`), after `attributesToSQL` (`:542-561`) clears `onUpdate` on the first reference to a table and both actions on a later reference to the same table, and `attributeToSQL` (`:488-494`) both on a self-reference |

`createTableQuery` is `mysql/query-generator.js:77` and
`mssql/query-generator.js:126`. Constraint names the database chooses
are never claimed in any dialect.

## Tests

- `TestSequelizeContract` (`contract_test.go`): this table equals the
  task file's row for row; `contract.go` lists the same rows in the same
  order with the same assumptions and sides (eleven claim rows, SQ-P1
  refusal only, SQ-U1 marker only, SQ-V without a branch, D last); every
  listed form is named in its row; the assumptions section equals the
  task file's. `TestSequelizeContractTags` is the tag half: over the
  fixture every row with a branch is reached (SQ-V never), every listed
  form is reached and every form tag listed, every tag is a row, and the
  rows the emitted claims name are exactly the claim-side rows tagged.
  `TestSequelizeCatalogue` checks the registered catalogue against this
  document. `TestSequelizeFailureKinds` keeps SQ-P1's refusal column and
  the code's failure kinds 1:1 (Revision 3).
- `discover_test.go` over inline sources: the ESM and CommonJS paths,
  the options rules (O1 spread, O2 selector, URL form, driver rule,
  dialectModule, unsupported dialects, unread entries), packages (major,
  pins, pnpm, sequelize-typescript, `.sequelizerc`, no instance), the
  unreached markers, two instances in one file, the associate rules, and
  Node's require order.
- `TestInflection` (`inflection_test.go`): the port against inflection
  1.13.4's and Sequelize 6.37.8's own outputs
  (`testdata/inflection-1.13.4.json`, produced in a scratch Node process).
- `TestDDLRendering` (`ddl_test.go`): `testdata/ddl-cases.js` rendered by
  Sequelize's own query generator on all five dialects (scratch, a stub
  driver module, nothing connects; `testdata/ddl-6.37.8.json`) against the
  claim pass: column set, type text, NOT NULL, DEFAULT text, inline
  UNIQUE, PRIMARY KEY, SERIAL, composite uniques, sqlite's integer
  primary-key rewrite, and every claimed foreign-key list against the
  REFERENCES clause with its actions (Revision 3: mssql's cleared
  actions; this is the mssql reference, no driver being
  installed).
- `TestSequelizeFixture` and `TestSequelizeClaims` (fixture part 1,
  `testdata/fixtures/sequelize-models`): every table and the facts of the
  columns per form, markers, rows reached; claim rows per fact, `oracle`
  only on postgres and sqlite and only for forms the investigation's oracle compared,
  `mapped` on the other dialects, each listed form reached.
- `TestModelFailures`, `TestModelNotClaimed`, `TestColumnRules`,
  `TestLinkStepIsolation`, `TestForeignKeyStepAllOrNothing`
  (`tables_test.go`): SQ-P1, SQ-K2 and D refusals, dialect and rewriting
  rules, step isolation, the all-or-nothing foreign-key step.
- `revision3_test.go`: the Revision 3 forms beyond the fixture's cases
  (each sequelize-cli loop variant, static associate own and inherited,
  `require(config)[env]` agreeing and disagreeing, hooks on Sequelize's
  class, keys not unique on sqlite and not proven on postgres, an ENUM
  default on sqlite, an association whose order to a load throw is not
  proven).
- `TestSequelizeCorpusLink` (`corpus_link_test.go`, set
  `GRAPHVEIN_SEQUELIZE_CORPUS`): the claim pass on both corpora against
  the investigation's oracle baseline per table.
- `TestSequelizeOracle` (`oracle_test.go`; set
  `GRAPHVEIN_SEQUELIZE_ORACLE` to the dump of
  `scripts/oracle/sequelize_catalog.sh` and
  `GRAPHVEIN_SEQUELIZE_ORACLE_ROOT` to the fixture or `.corpus`
  directory): every claimed fact against the catalog Sequelize's own
  `sync` left in a scratch Postgres 17 database or SQLite file (table,
  complete column set, primary key, unique constraints per column set and
  per column, so the cyclic path's `_key1` duplicates count once, indexes,
  check constraints, type, nullability, default presence and text after
  stripping casts and quotes, jsonb texts as JSON values, enum labels,
  foreign keys with actions). The harness loads each run's modules in a
  Node process with every module outside the run's allow list stubbed,
  syncs with `sync({force: true})` and replays Sequelize's own steps per
  model when it fails (recording every failure). Over the fixture every
  oracle form of `TestSequelizeClaims` and the helper columns must be
  compared.
- `TestSequelizeCorpusDiscover` (`corpus_test.go`, set
  `GRAPHVEIN_SEQUELIZE_CORPUS` to the `.corpus` directory):
  opencollective-api and Gladys against the investigation's discovery rows.
