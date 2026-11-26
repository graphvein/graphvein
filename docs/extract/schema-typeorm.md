# Schema from TypeORM models

ADR 0064 (T018g6, `docs/contract/claims/typeorm-models.md`; inventory and
oracle baselines in the T018g6 task record (internal)). The TypeORM front
end `internal/extract/models/typeorm` reads each package's TypeORM 0.3.x
or 1.x data sources, ormconfig files and NestJS `TypeOrmModule` roots and
the entity classes they reach, from the cached TypeScript model payload
version 5 (`docs/lang/typescript-models.md`) and the retained
`package.json`, lockfile, tsconfig and `ormconfig.json` sources, and hands
its tables to the tool-neutral model kit `internal/extract/models`
(ADR 0058, `docs/extract/schema-models.md`), which emits the records every
ORM front end shares (`source: "model"`, `tool: "typeorm"`, group
`models/typeorm/<data source file>`).

**Status (2026-11-15):** discovery (`discover.go`,
`options.go`, `nest.go`, `entities.go`, `refs.go`, `scope.go`; pins from
the shared lockfile reader `internal/extract/models/lockfile`; compiler
flags from the shared TypeScript resolver's tsconfig chain,
`typescript.ModuleResolver.CompilerFlags`), the contract table
(`contract.go`), the claim pass (`claims.go`, `entity.go`, `column.go`,
`column_render.go`, `render.go`: tables, columns, types and defaults;
`relations.go`, `constraints.go`, `names.go`: relations, junction
tables, indexes, constraints and foreign keys; "Claim pass" below), the
catalogue for `/api/v0/schema/assumptions` (`catalogue.go`) and the call
in `internal/index` (after the Sequelize front end) are in place, and
the oracle ("Tests" below) compares every claimed fact with
the catalogs TypeORM's own synchronize builds.

Line numbers are from the npm package `typeorm@0.3.31` (files under its
root) and, written `0.3.31 line / 1.1.3 line` or marked **1.x**, from
`typeorm@1.1.3`; `@nestjs/typeorm` is cited at 11.0.0 and 12.0.3 under
`dist/`, `typeorm-naming-strategies` at 4.1.0.

## Discovery

`Discover` returns the packages, their pins and major line, their entity
classes and forFeature calls, and per package the model groups: data
sources, ormconfig groups and NestJS roots, each with its candidate
options, dialect set, naming strategy, reached entities, migrations
option and the reasons its table list may be incomplete. Diagnostics are
`info`: `schema_model_not_proven` (a package or group without claims,
options not read), `schema_model_unread` (row D markers, entity classes
no group reaches, incomplete table lists: the message ends with "the
table list of model group `<set>` may be incomplete"),
`schema_model_migrations` (row TO-U1: "`<file>`: typeorm migrations (not
read): `<dir>`") and `schema_model_import_failure` (TO-P1 failures
discovery proves: 1.x `type: "sqlite"`, failure (7)).

### Packages, pins and major lines (TO-K1, TO-V1, TO-V9)

- **Package.** A directory with a `package.json` (not under
  `node_modules`) whose `dependencies` or `devDependencies` name
  `typeorm`. A package owns the script files below it that no nearer
  `package.json` owns.
- **Pins.** The locked `typeorm` version from the lockfile beside the
  package, else the nearest one above it (`pnpm-lock.yaml` importer,
  `package-lock.json` `packages`, `yarn.lock` classic or Berry), and the
  same for `typescript` (declared by the package, else by the nearest
  manifest above it that declares it). `pins` is
  `{"typeorm": v, "typescript": v}` when both are proven, else null
  (claims kept, TO-V1 listed). `@nestjs/typeorm` (TO-K3, TO-K4: 11.x and
  12.x differ) and `typeorm-naming-strategies` (TO-V10) are read the same
  way. The Yarn Berry reader now accepts a key list quoted as one string
  (Maintainerr's `"typescript@npm:^6.0.3, typescript@npm:~6.0.2":`).
- **Major line.** The locked version, else the declared range when its
  major and minor are fixed (`^0.3.31`, `~1.1.2`): `0.3.x` reads the 0.3
  rules, `1.x` the cells marked **1.x** (Revision 2 O12); any other
  version, or one that is not proven, refuses the package (marker, row
  D). Every 0.3.x and 1.x pin is read with TO-V9 shown (Revision 3 O14).
- **Drivers** (`pg`, `sqlite3`, `better-sqlite3`, `mysql2`, ...) are
  recorded; the dialect comes from the literal `type`.

### Data sources and the candidate rule (TO-K1, TO-K7, TO-K8)

- **Data source.** A module-level `const x = new DataSource(<options>)`
  or `export default new DataSource(<options>)` (`DataSource` resolving to
  the `typeorm` export) in a package file; its group root is the file
  (`<file>#<binding>` when the file holds several groups). A `let` or
  `var` binding is a marker. A data source or ormconfig in a test file
  (the repository's test rules, or TO-K4's file patterns) is read like
  any other group and its tables carry `scope: test`, as other front
  ends' test models do (Revision 3 addendum 3); NestJS roots and
  `forFeature` calls in test files stay markers.
- **Candidates.** `<options>` is read when it is an object literal, an
  identifier bound to a module-level `const` whose initializer is an
  object literal (a type annotation allowed), a conditional `c ? a : b`
  of candidates, or a call (no arguments) to a module-level non-async
  function of the same file whose every `return` is a candidate, inside
  `if`/`else` chains (payload rows B-if with `else`, E-conditional);
  declarations and expression statements in the function are allowed, any
  other statement (a loop, a `try`, a `switch`) could return other
  options and leaves them unread. seerr: `server/datasource.ts:184`
  `new DataSource(getDataSource())`, five candidates.
- **Imported options (TO-K7, Revision 3 O15).** An identifier imported
  (default or named, through re-exports the resolver follows) from a
  module of the same package where it is a module-level `const` object
  literal is read as written in place (Maintainerr
  `src/app/config/typeOrmConfig.ts:4`, used at `src/app/app.module.ts:49`).
  A `let`, an object assigned into (`x.key = v`, `x[k] = v`) or passed to
  `Object.assign` as the target at module level, or a module of another
  package leaves the options unread.
- **Option entries.** Plain and shorthand entries are read by key (the
  last write wins); a method entry is ignored unless its key is a scope
  option; a computed key or an accessor leaves the options unread. A
  spread is read through a registerAs namespace (TO-K5, below) or when it
  is scope-neutral (**TO-K8**, Revision 3 O16): its argument is an
  identifier bound to a module-level `const` whose initializer is an
  object literal, a conditional of object literals or an object literal
  with `&&`-guarded spreads of object literals, recursively, and none of
  those literals has a scope key (`type`, `entities`, `entityPrefix`,
  `schema`, `namingStrategy`, `name`, `autoLoadEntities`) or a computed
  key (aqualink `ormconfig.ts:15-34`). Any other spread leaves the options
  unread.
- **Scope options.** `entities`, `entityPrefix`, `schema`,
  `namingStrategy`, `name` and `autoLoadEntities` must be equal across the
  candidates (identifiers compared by the declaration they resolve to),
  else the options are not read; `entityPrefix` and `schema` must be
  string literals; `synchronize` and `migrationsRun` are recorded per
  candidate as scope evidence only.
- Options that are not read: the group is kept (its key and the reason),
  claims nothing, and every entity it could reach stays a marker.

### ormconfig (TO-K1)

`ormconfig.json`, or `ormconfig.ts` / `ormconfig.js` with `export default
<object literal>` or `module.exports = <object literal>` (an array: one
group per element with a literal `name`, root `<file>#<name>`), at the
package root, read only when the package has no data source (TypeORM 0.3
reads it only for `createConnection()` / `getConnectionOptions()`,
`globals.js:46`, `:83`; `connection/ConnectionOptionsReader.js:193`).
`.yml`, `.yaml`, `.xml` and `.env` forms are D; under **1.x** every
ormconfig file is D (`globals.js` keeps only `getMetadataArgsStorage`).
Payload gate row G10 now also opens on the names `ormconfig.ts` and
`ormconfig.js`, which often import nothing.

### NestJS (TO-K3, TO-K4, TO-K5)

- **Roots (TO-K3).** `TypeOrmModule.forRoot(<options>)` and
  `TypeOrmModule.forRootAsync({...})` (`TypeOrmModule` resolving to the
  `@nestjs/typeorm` export) in module-level positions of a non-test file
  (class decorator arguments such as `@Module({imports: [...]})`,
  module-level declarations and statements): one group per call, root the
  file (`<file>#<data source name>` when the file holds several). A call
  inside a function body runs only when the function is called: marker.
  `forRoot` reads its options as a data source's; the data source name is
  the options' literal `name`, default `default`
  (`common/typeorm.utils.js:58-66`), and with `@nestjs/typeorm` 12.x a
  literal second argument (`dist/typeorm.module.js:25-26` at 12.0.3; 11.x
  ignores it). `forRoot()` without options is D.
- `forRootAsync` reads `useFactory` (an arrow or function expression,
  not async: the payload keeps no async function) whose every return is a
  candidate, `inject`, `imports`, `name` (literal) and `dataSourceFactory`
  when absent or `(o) => new DataSource(o)`, optionally `.initialize()`d
  (the default, `typeorm-core.module.js:160-165` at 11.0.0). `useClass`,
  `useExisting` (`createTypeOrmOptions()`, `:143-149`), any other key or
  factory: the options are not read (D, open point O9).
- **Function scope (TOB3 OP2).** The parameters of a
  `useFactory`, a `registerAs` factory or a selector function and every
  name its body declares (in nested blocks too) shadow the file's
  module-level names: an option value, returned options object, spread or
  `configService.get` token naming one is read from the function, never
  from the module. Such a name is unknown (the option not read; a scope
  option then leaves the group's options unread or its table list
  incomplete) unless it is a `const` local bound to a string, number,
  boolean or `null` literal, which is read as that literal (`scope.go`).
- **Entity reach (TO-K4).** `TypeOrmModule.forFeature([<identifiers>],
  <data source name literal>?)` calls in module-level positions of
  package files other than tests (`*.spec.*`, `*.test.*`, `*.e2e-spec.*`,
  files under `test/` or `__tests__/`, and the configured test rules)
  register their `@Entity` classes for that name
  (`typeorm.module.js:23-25`, `entities-metadata.storage.js:5-28`). With
  a literal `autoLoadEntities: true` the group reaches the `entities`
  option (an array, read as TO-K1 reads it) plus every `forFeature`
  entity of its name (`typeorm-core.module.js:166-183` at 11.0.0); a
  non-array `entities` is replaced by that list in 11.x (`:176-179`) and
  in 12.x an object map's values are kept and the list appended (`:182-186`
  at 12.0.3); a string `entities` is not read. Without
  `autoLoadEntities`, `forFeature` reaches nothing. A `forFeature`
  element that is not an identifier of an `@Entity` class, a non-literal
  name, a non-array argument, a call inside a function body, or a group
  whose name is not a literal leaves the table list incomplete. Calls in
  test files are counted in one marker. `@ViewEntity` and
  `@ChildEntity` classes add no table of their own.
- **Config namespaces (TO-K5, Revision 2 O9).** In a `useFactory` whose
  parameter receives `ConfigService` (the `inject` element at its
  position resolves to the `@nestjs/config` export),
  `<param>.get('<token>')` (type arguments dropped) spread into or
  returned as the options reads the package's single `registerAs(<token>,
  <factory>)` (`registerAs` from `@nestjs/config`; the token a string
  literal or a module-level const bound to one; the factory an arrow with
  an expression body or one `return` of a candidate) as if written in
  place (TO-V8). No match, several, a non-literal token or another factory
  body leave the options unread.

### Naming strategy (TO-K6)

`namingStrategy: new SnakeNamingStrategy()` with no arguments, the class
imported by name directly from `typeorm-naming-strategies` (not a project
re-export) and that package locked at 4.x (TO-V10), selects the snake
names (table below); any other `namingStrategy` value (a subclass, a
re-export, a variable, a call with arguments, another package, an
unproven pin) makes the group D (decision 2026-11-13 as amended by
Revision 2 O11). The strategy's names are applied by the claim pass
(TOB3).

### Dialect (TO-K1, TO-V4, TO-P1 (7))

The candidates' literal `type` values: `postgres`, `sqlite` and
`better-sqlite3` (`oracle`), `mysql`, `mariadb`, `mssql` and the other
SQLite-family drivers (`sqljs`, `capacitor`, `cordova`, `expo`,
`nativescript`, `react-native`: `mapped`), mapped to the dialects
`postgres`, `sqlite`, `mysql`, `mariadb`, `mssql`; a dialect's
verification is the weakest of the types that map to it. One dialect:
the group's `dialect`; several (seerr: `sqlite` x3, `postgres` x2): the
dialect set (Revision 1 O1, ADR 0065). A non-literal or missing `type`
leaves the options unread; another type (`cockroachdb`, `oracle`, `sap`,
`spanner`, `mongodb`, `aurora-*`, ...) makes the group D. Under **1.x** a
`type: "sqlite"` candidate is TO-P1 (7) (`MissingDriverError`,
`driver/DriverFactory.js:72` at 1.1.3): a
`schema_model_import_failure` diagnostic, and no table of the group is
claimed on that dialect.

### Entities (TO-K1)

- **Entity classes.** Module-level classes of the package with a class
  decorator named `Entity`, `ViewEntity` or `ChildEntity` (payload row
  S4's `decorator_list`); the decorator must resolve to the `typeorm`
  export (re-exports followed), else the class is D. `@ViewEntity` and
  `@ChildEntity` classes are D markers. A typeorm `@Entity` class no
  claimable group reaches is a marker ("not in the data source's
  entities").
- **Entities option.** An array literal, or an identifier bound to a
  module-level `const` array literal (imports followed), whose elements
  are class references (identifiers resolving to an `@Entity` class;
  seerr: 14 imports through the tsconfig `paths` alias `@server/*`) or
  string globs of the resolvable subset: a string literal, a template or
  `+` concatenation of `__dirname` with a string literal, or
  `path.join(__dirname, <string literals>)` / `path.resolve(...)` of the
  same (`path` as a namespace or default import, or `join`/`resolve`
  imported by name: aqualink, A40); pattern syntax `*`, `**`, `?` and one
  level of `{a,b}` only, names starting with a dot never matched by a
  wildcard (glob's default). `__dirname` patterns are resolved against
  the directory of the file the expression is written in, other relative
  patterns against the package directory (TO-V5), keeping the files
  TypeORM's loader imports (`.js`, `.mjs`, `.cjs`, `.ts`, `.mts`, `.cts`,
  not `.d.ts`; `util/DirectoryExportedClassesLoader.js:14-52` / `:16-39`,
  `tinyglobby` in 1.x); every exported `@Entity` class (`export class`,
  `export default class`, `export { X }`, `export default X`) of each
  matched file is reached.
- **Incomplete table list** (marker naming the element): a pattern that
  matches no source file (`dist/**/*.js`, build output: nest-admin, A25,
  open point O10), unless it differs from a pattern that matches files
  only by a final `.js`/`.ts` (Revision 3 O18: aqualink's `.js` sibling
  adds nothing); other glob syntax; an absolute or outside-the-repository
  path; an `EntitySchema`, a spread, a call or an unresolved identifier;
  an `@Entity` class whose decorator is not typeorm's; a matched entity
  file of another package.

### Compiler flags (TO-V3)

For every reached entity file the nearest `tsconfig.json` (else
`jsconfig.json`) and its relative `extends` chain (child settings
override parents, later array entries override earlier ones) give
`experimentalDecorators`, `emitDecoratorMetadata`, `strict` and
`strictNullChecks` (`typescript.CompilerFlags`). An `extends` that names
a package (`@tsconfig/node20/tsconfig.json`) or a config that does not
parse is not read: an option it could set is unknown unless a nearer
config sets it. A group with a reached entity file whose chain does not
prove `experimentalDecorators: true` is D (TypeORM 0.3 and 1.x need
legacy decorators); the other flags are kept on each entity for the claim
pass (design-type inference, TO-C2, TO-P1 (3)).

### Migrations (TO-U1)

The `migrations` option of every candidate (an array literal or a
conditional of arrays): each element read as an entities glob is
recorded with the directory before its first wildcard and the number of
script files it matches (a build-output pattern matches none and is
listed as named only); each element gives one `schema_model_migrations`
marker. seerr: `server/migration/sqlite` (55 files),
`server/migration/postgres` (20), `dist/migration/...` (named only). The
option's presence makes TO-X4's generated names unknown (Revision 1 O5).
The server lists each named folder that holds stored script files in
`unread_tools` (tool `typeorm-migrations`, the script files at any depth
under the folder; step TOB4, Revision 3 addendum 2 OB); a build-output
folder without stored scripts adds no entry.

## Assumptions

Copied from the task file's "Environment" (ids and text identical);
each group's records will list them (`assumptions`) with its `pins`:

- **TO-V1 Pins.** The `typeorm` version in the package's lockfile
  (`pins: {"typeorm": "<v>", "typescript": "<v>"}`) created the database
  and the `typescript` version compiled the entities.
- **TO-V2 Schema deployed.** The database was created by `synchronize`
  for the reached entities or by migrations that match them (TypeORM
  migration classes are not read, TO-U1). seerr: production runs its
  migrations (`synchronize: false`, `migrationsRun` in dev Postgres);
  the 55 SQLite and 20 Postgres migrations give the synchronize catalog
  except generated constraint names.
- **TO-V3 Decorator metadata.** The entities are compiled by `tsc` (the
  pinned `typescript`) with `experimentalDecorators` and
  `emitDecoratorMetadata` as the nearest tsconfig chain states them, and
  `strictNullChecks` as that chain states it (`strict` implies it).
  Without both decorator flags proven `true`, every design-type inferred
  type is unknown; with `experimentalDecorators` not `true`, the group is
  D (TypeORM 0.3 needs legacy decorators).
- **TO-V4 Dialect.** The database is of the DataSource's literal `type`;
  when the options are selected at run time among several literal option
  objects (seerr: five objects, `sqlite` and `postgres`), the dialect is
  one of that set (open point O1).
- **TO-V5 Modules load.** Entity module side effects do not change the
  metadata beyond what is read (no `getMetadataArgsStorage()` mutation,
  no decorator applied conditionally or in a loop), and when an
  `entities` glob is read, the process runs with the package directory as
  working directory (cwd-relative globs) and a compiled output mirrors
  the source tree (`__dirname` globs). NestJS (TO-K4): an import
  declaration loads its module (TypeScript elides an import used
  only as a type; such a module is still counted), and a module loaded
  only by a dynamic `import()` is not seen (it leaves a list incomplete
  only when no root reaches the `forFeature` file).
- **TO-V6 Mapped dialects.** MySQL, MariaDB and MSSQL facts follow
  TypeORM 0.3.31's driver source (`driver/mysql/MysqlDriver.js`,
  `driver/sqlserver/SqlServerDriver.js`) as cited (never `oracle`).
- **TO-V7 Schemas exist.** An entity `schema` on Postgres names a schema
  that exists: `synchronize` does not create it.
- **TO-V9 Verified versions (TA3).** The rules were verified at TypeORM
  0.3.22, 0.3.31, 1.1.2 and 1.1.3 (oracle) and TypeScript 5.4.5, 5.8.3,
  5.9.3 and 6.0.3; a pin of another 0.3.x or 1.x version is read with the
  per-major rules and this assumption shown (open point O14).
- **TO-V10 Naming-strategy package (TA3, only with TO-K6).** The
  `typeorm-naming-strategies` version in the lockfile is 4.x (verified at
  4.1.0, the latest; its peer range names TypeORM 0.2/0.3, and under 1.x
  it is installed past that range and behaves the same).
- **TO-V8 Config namespaces load (TA2, only with TO-K5).** A `registerAs`
  namespace read through `configService.get('<token>')` is in the
  `ConfigModule` load list at run time (nest-admin:
  `load: [...Object.values(config)]`, `src/app.module.ts:39`).

## Claim contract

Finalised from the inventory and the oracle (the investigation). Row kinds: **K**
scope and naming; **E** entity forms; **C** columns; **X** indexes and
constraints; **R** relations; **P** provable failures and refusals;
**U** unread tool folders; **V** assumptions; **D** the single default
branch. Every form the corpus uses has exactly one row (counts per row:
the T018g6 task record (internal)); forms only the fixture uses come from
the same oracle runs (scratch probes). The contract test keeps
`docs/extract/schema-typeorm.md`, the Go table and the code 1:1 (plan,
shared piece 4). **30 rows: 29 rows plus D** (21 from the investigation; TO-K3, K4,
K5, E3, C6 and X4 added for Revision 1 and the NestJS
corpus, TO-K5 accepted by Revision 2 O9; TO-K6, K7 and K8 added pending open points O15 and O16; TA3 also wrote the
TypeORM 1.x rules into the rows, marked **1.x**, and corrected TO-K2
and TO-R1 for both majors); TO-P1 and TO-P2 only
refuse, TO-R3 claims nothing (inverse sides are counted), TO-U1 only
marks, TO-V has no branch. Citations are to the npm package
`typeorm@0.3.31` (files under its root) and `typescript@5.4.5`
(`lib/typescript.js`); **1.x** citations are to `typeorm@1.1.3` (equal
to 1.1.2 in every cited function), written `0.3.31 line / 1.1.3 line`
where both are given (full table: "TypeORM 1.x comparison" in the TA3
section).

Dialect rule (developer decision 2026-11-13):
- **postgres:** `verification: "oracle"`: `synchronize` on a scratch
  Postgres 17, catalog read.
- **sqlite:** the same oracle on a scratch SQLite file through `sqlite3`
  (`sqlite_master`, `pragma table_xinfo`, `index_list`, `index_info`,
  `foreign_key_list`; constraint names from the `CREATE TABLE` text).
- **mysql, mariadb, mssql:** `verification: "mapped"`, TO-V6, from
  `driver/mysql/MysqlDriver.js` (`normalizeType` :578-640,
  `normalizeDefault` :642, `getColumnLength` :684-700, `createFullType`
  :707, date defaults `CURRENT_TIMESTAMP(6)` :178-182) and
  `driver/sqlserver/SqlServerDriver.js` (`normalizeType` :481-520,
  `normalizeDefault` :523, lengths :558-561, `createFullType` :567, date
  defaults `getdate()` :125-127). MariaDB's `uuid` type depends on the
  server version detected at connect (`uuidColumnTypeSuported`,
  `MysqlDriver.js:594`): unknown.
- **Several dialects (TO-V4, seerr):** provider-independent facts are
  claimed with the verification of the weakest dialect of the set;
  dialect-dependent facts (type, default text, enum representation,
  generation) are claimed per dialect through the kit's per-dialect
  fields (Revision 1 O1, ADR 0065), each with its own `verification`.
- **SQLite family (Revision 1 O8):** `better-sqlite3` is `sqlite` (the
  shared `driver/sqlite-abstract/AbstractSqliteDriver.js`; TA2 ran the
  oracle through both drivers with identical catalogs), so
  its compared forms are `oracle`; `sqljs`, `capacitor`, `cordova`,
  `expo`, `nativescript`, `react-native` follow the same rules as
  `mapped`.
- **TypeORM 1.x (TA3):** the same labels; SQLite means `better-sqlite3`
  and the other SQLite-family types (1.x has no `sqlite` type, A32), and
  its oracle runs through `better-sqlite3` (open point O20).
- **MySQL types (TA2):** the mapped list for `mysql` and
  `mariadb` is stated in TO-C2; a type outside it is unknown.
- **Unknown in every dialect:** generated constraint and index names,
  collations, comments, sequence options, column order.

Provider-independent facts (table and column names, column set,
nullability, primary-key columns, unique column sets, index column sets
and uniqueness, foreign-key columns, target table and columns, actions)
were equal on both oracle dialects for every seerr and probe table.

| id | form | claim | refusal / unknown | assumptions |
|---|---|---|---|---|
| TO-K1 | scope (Discovery above): package dependency on `typeorm` 0.3.x or 1.x, lockfile pins, the module-level `new DataSource(<options>)` with the candidate rule, ormconfig when no DataSource, the `entities` option (class references; the glob subset), the dialect set, tsconfig decorator flags | the group (`set: models/typeorm/<file>`), `pins`, `dialect` (one type) or the dialect set, the reached entity classes, `table_list` complete when every `entities` element is read | non-literal or unequal scope options, an unread `entities` element, an unreached `@Entity` class: `may_be_incomplete` / not claimed (markers); `namingStrategy` other than TO-K6, another major, other dialects: D; **1.x:** a candidate `type: "sqlite"` is TO-P1 (7) (the `sqlite3` driver is removed, `driver/DriverFactory.js:43` / `:39-72`); ormconfig files are D (1.x reads them nowhere: `globals.js` keeps only `getMetadataArgsStorage`); globs as in 0.3 (`tinyglobby` instead of `glob`, `util/DirectoryExportedClassesLoader.js:32` / `:39`; same files on the oracle's glob run); both majors: `join(...)`/`resolve(...)` imported by name from `path` count as `path.join`/`path.resolve` (aqualink, A40) | TO-V1, TO-V3, TO-V4, TO-V5, TO-V9 |
| TO-K2 | naming: table name: `@Entity` literal name, else `snakeCase(className)` (`naming-strategy/DefaultNamingStrategy.js:26-28`, `util/StringUtils.js:29-36`), prefixed by `entityPrefix` (every table, explicit names and junction tables too); schema: the entity's `schema`, else the options' `schema` (postgres); column name: the literal `name` option, else the property name (`DefaultNamingStrategy.js:37-42`); join column: the `@JoinColumn` `name`, else `camelCase(relationProperty + "_" + referencedColumnProperty)` (`:114-116`, `metadata-builder/RelationJoinColumnBuilder.js:137`: the referenced column's property name); junction: `@JoinTable` `name`, else `snakeCase(ownerTable + "_" + property + "_" + targetTable)` of the unprefixed names (`:117-123`), columns `camelCase(table + "_" + referencedColumnDatabaseName)` (`:127-129`: `joinTableColumnName(table, property, databaseName)` prefers the database name; TA3 corrected the investigation's text, which named the property), `_1`/`_2` appended when both are equal (`:124-126`, `metadata-builder/JunctionEntityMetadataBuilder.js:272-275`); Postgres enum type: `enumName`, else `<table>_<lower(column)>_enum` in the table's schema (`driver/postgres/PostgresQueryRunner.js:2597-2612`); **1.x:** the same rules (`DefaultNamingStrategy.js:13-15`, `:19-24`, `:96-115`, `PostgresQueryRunner.js:2961` at 1.1.3; oracle: equal names on every probe, seerr and nest-admin table); with TO-K6 the SnakeNamingStrategy names replace these | the names | a non-literal name, prefix or schema: that name unknown (the table or column unknown when its name is); an empty-string `@Entity`/`@JoinTable` name (0.3 falls back, 1.x keeps `""`): unknown; `database` option on an entity: D | TO-V1, TO-V9 |
| TO-K3 | NestJS root options (Revision 1 O2; `@nestjs/typeorm` at the lockfile pin, cited at 11.0.0 under `dist/`): `TypeOrmModule.forRoot(<options>)` with `<options>` read as TO-K1 reads DataSource options; `TypeOrmModule.forRootAsync({useFactory, inject?, imports?, name?, dataSourceFactory?})` whose `useFactory` is a non-async arrow or function expression whose every `return` is an object literal (Revision 3 addendum 2: its block may hold `try` statements, body, catch and finally kept as statements, read only when none of them returns, so every path reaches the same later `return`; nest-admin `src/shared/database/database.module.ts:22-38`); `dataSourceFactory` absent or `(o) => new DataSource(o)` (optionally `.initialize()`d; the default, `typeorm-core.module.js:160-165`), async or not, or the async block `async (o) => { const x = await new DataSource(o).initialize(); return x }` (nest-admin `:41-44`; Revision 3 addendum 2); one group per data source name (`name` literal, default `default`, `common/typeorm.utils.js:58-66`), group root the file of the call | the group, `pins`, `dialect` from the literal `type`, the scope options as TO-K1 | a spread or a non-literal value among the options: the keys it can supply are unknown, and since a spread can carry `namingStrategy`, `entityPrefix` or `type`, every name and dialect fact of the group is unknown (marker naming the spread) unless TO-K5 resolves it; `useClass`/`useExisting` (`createTypeOrmOptions()`, `typeorm-core.module.js:143-149`), `forRoot()` without options, any other `dataSourceFactory`: the group's options unknown (marker; open point O9); **`@nestjs/typeorm` 12.x** (1.x apps): `forRoot(options, name)` with a literal second argument names the group (`dist/typeorm.module.js:25-26` at 12.0.3) | TO-V1, TO-V3, TO-V4, TO-V5 |
| TO-K4 | NestJS entity reach: `TypeOrmModule.forFeature([<identifiers>], <data source name literal>?)` in package files other than tests (`*.spec.*`, `*.test.*`, `*.e2e-spec.*`, files under `test/` or `__tests__/`), each identifier resolving to an `@Entity` class; `autoLoadEntities: true` (literal) in the TO-K3 options | reached entities: the options' `entities` (TO-K1 element rules) when it is an array, plus every `forFeature` entity registered for that data source name in a file the root's application loads through static imports (from the root module file and from every entry file of the package, a non-test file no other package file imports, whose imports reach the root file: import declarations and `export ... from` clauses, resolved by the module resolver, recursively; package imports are not followed) (`typeorm.module.js:23-25`, `entities-metadata.storage.js:5-28`, `typeorm-core.module.js:166-183`); a non-array `entities` option is replaced by the `forFeature` list (`:176-179`; in 12.x an object-map `entities` keeps its values and the `forFeature` list is appended, `typeorm-core.module.js:182-186` at 12.0.3; a string `entities` with `autoLoadEntities`: `may_be_incomplete` on every version); without `autoLoadEntities`, `forFeature` reaches no table (repositories only) | a non-identifier element or a non-literal name: `may_be_incomplete`; an `@Entity` class registered only from test files: not claimed; a `forFeature` call in a file the root's application does not reach: not counted for that root, and the root's list `may_be_incomplete` when its import graph is open (a relative specifier that resolves to no file, a `require` call) or no root module of the package reaches that file (a dynamic import may load it) (Revision 4 A4) | TO-V5 |
| TO-K5 | (Revision 2: O9 accepted) `configService.get('<token>')` / `get<T>('<token>')` (`ConfigService` from `@nestjs/config`) spread into or returned as TO-K3 options, when the package has exactly one `registerAs(<the same token: a string literal or a module-level const bound to one>, () => <options TO-K1 can read>)` | the registered options, read as if written in place | no or several matching `registerAs`, a non-literal token, a factory with statements other than one `return`: TO-K3's unknown | TO-V8 |
| TO-K6 | (Revision 2 O11, TA3) `namingStrategy: new SnakeNamingStrategy()` in the scope options (TO-K1, TO-K3, TO-K5 readers), the class imported by name from `typeorm-naming-strategies` (4.x in the lockfile), no arguments | every TO-K2 name through the strategy (`snake-naming.strategy.js` at 4.1.0): table the literal name, else `snakeCase(className)` (`:7-9`); column the literal `name` verbatim, else `snakeCase(property)` (`:10-13`); join column the `@JoinColumn` `name`, else `snakeCase(relationProperty + "_" + referencedColumnProperty)` (`:17-19`); junction table `snakeCase(ownerTable + "_" + property + "_" + targetTable)` of the unprefixed names (`:20-26`); junction columns `snakeCase(table + "_" + referencedColumnDatabaseName)` (`:27-29`), `_1`/`_2` and `entityPrefix` inherited; Postgres enum types from the snake column name; generated names as TO-X4 over the snake names; `snakeCase` is TypeORM's own (`util/StringUtils.js:29-36` / `:32-39`) | another `namingStrategy` value (a subclass, a project re-export, an instance from a variable, a call with arguments, another package): D (decision 2026-11-13 as amended by Revision 2 O11) | TO-V1, TO-V9, TO-V10 |
| TO-K7 | (pending open point O15, TA3) `new DataSource(X)` or `TypeOrmModule.forRoot(X)` where `X` is an identifier imported (default or named) from a package module in which it is bound to a module-level `const` whose initializer is an object literal (a type annotation allowed) | the options, read as if written in place (Maintainerr `src/app/config/typeOrmConfig.ts:4`, used at `src/app/app.module.ts:49`) | a re-export chain the resolver cannot follow, a `let`, a mutated object, a non-literal initializer: TO-K1's unknown options (marker) | TO-V5 |
| TO-K8 | (pending open point O16, TA3) a spread `...Y` in the options object literal where `Y` resolves to a module-level `const` whose initializer is an object literal, a conditional of object literals, or an object literal with `&&`-guarded spreads of object literals, recursively, and none of those literals has a scope key (`type`, `entities`, `entityPrefix`, `schema`, `namingStrategy`, `name`, `autoLoadEntities`) or a computed key | the spread adds no scope option; the other keys read as TO-K1 (aqualink `ormconfig.ts:15-34`: `{url}` or `{host, port, database, username, password}`) | any other spread argument, a scope key or computed key inside it: TO-K1's unknown options (marker) | TO-V5 |
| TO-E1 | `@Entity()`, `@Entity("name")`, `@Entity({name, schema, synchronize})` (from `typeorm`) on a module-level class (plain, `export`, `export default`, or `class X` with `export default X`) reached by TO-K1; members with no table fact: `@RelationId`, `@RelationCount`, `@VirtualColumn`, entity listeners (`@AfterLoad`, `@BeforeInsert`, ... `@AfterRecover`), undecorated members, methods, constructors; option `orderBy` has no table fact | a model (table); `synchronize: false`: the table and its columns claimed with `verification: "mapped"` under the shown assumption "TypeORM does not create this table; this is the shape the code expects" (Revision 1 O4) | other entity options (`database`, `withoutRowid`, `engine`, `expression`): the table's facts they could change unknown (`partial`); `@Entity` from another module than `typeorm`: D; **1.x:** `@RelationCount` no longer exists (`index.js:55` at 0.3.31, absent at 1.1.3): an entity module using it fails to load, TO-P1 (8) | TO-V2, TO-V5 |
| TO-E2 | class inheritance: `extends` an identifier resolving to a class of a reached module (abstract or not, decorated or not), recursively, and `extends BaseEntity` (from `typeorm`: no columns) | inherited columns, relations, indexes and uniques, base-class members first | a base that does not resolve (a mixin call, an external package class other than `BaseEntity`): the column set unknown (`partial`) | TO-V5 |
| TO-E3 | member decorators from packages that never touch TypeORM metadata: `@nestjs/swagger`, `class-transformer`, `class-validator` (TA2: at nest-admin's lockfile versions 11.1.1, 0.5.1, 0.14.1 no file of these packages mentions `typeorm`, and none depends on it) | no table fact: the member is read as if the decorator were absent | any other package: TO-P2 | TO-V5 |
| TO-C1 | column decorators from `typeorm`: `@Column(type?, options?)`, `@PrimaryColumn(type?, options?)`, `@PrimaryGeneratedColumn(strategy?, options?)` with strategy `"increment"` (default), `"uuid"`, `"identity"` (postgres), `@Column({primary: true})`, `@Column({generated})` | the column; primary-key columns in member order (composite allowed); generation: postgres `increment` a `nextval` sequence default (serial), `identity` `GENERATED BY DEFAULT`, `uuid` default `uuid_generate_v4()`; sqlite `increment` on the single integer primary key `INTEGER PRIMARY KEY AUTOINCREMENT`, `uuid` no default | an entity without a primary column: TO-P1; strategy `rowid` or a non-literal strategy: generation unknown; `@ObjectIdColumn`: D | TO-V2 |
| TO-C2 | column type: the explicit literal type (positional first argument or `type` option) from the per-dialect list the oracle ran (the investigation: `int`, `int4`, `integer`, `varchar`, `character varying`, `text`, `bigint`, `int8`, `smallint`, `int2`, `boolean`, `bool`, `decimal`, `numeric`, `float`, `double precision`, `float8`, `real`, `float4`, `date`, `time`, `timestamp`, `timestamptz`, `uuid`, `json`, `jsonb`, `character`, `char`, `simple-array`, `simple-json` on postgres; `int`, `integer`, `varchar`, `text`, `bigint`, `smallint`, `boolean`, `decimal`, `numeric`, `float`, `double precision`, `real`, `date`, `time`, `datetime`, `character`, `simple-array`, `simple-json` on sqlite), rendered by the driver (`driver/postgres/PostgresDriver.js:732-789`, `driver/sqlite-abstract/AbstractSqliteDriver.js:433-461`); else the design type of the annotation (`Column.js:25-33`) when it is exactly `string`, `number`, `boolean` or `Date` (TypeScript's serializer, `typescript.js:95540-95683`): `String` varchar / character varying, `Number` integer, `Boolean` boolean, `Date` datetime / timestamp without time zone; `length` (literal) as `(n)`; `precision`/`scale` (literals) as `(p,s)`, a precision alone as `(p)` on sqlite and `(p,0)` with scale 0 on postgres, mysql and mariadb (their catalogs report NUMERIC(p) and DECIMAL(p) so; TOB5 oracle); `array: true` (postgres) as `[]`; `simple-array` and `simple-json` are `text`; `mysql`/`mariadb` (`mapped`, TA2, `MysqlDriver.normalizeType`, `getColumnLength`, `createFullType`): `int`, `integer` (`int`), `tinyint`, `smallint`, `bigint`, `varchar` (`varchar(255)` without `length`), `text`, `datetime`, `date`, `boolean`/`bool` (`tinyint`), `decimal`, `float`, `double`, `json`; design types `String` `varchar(255)`, `Number` `int`, `Boolean` `tinyint`, `Date` `datetime` | type per dialect | a non-literal type or length (a project call such as seerr's `resolveDbType('datetime')`, 10 sites), a type outside the list, `unsigned`, `zerofill`, `width`, `charset`, `collation`, `spatialFeatureType`, `srid`, `hstoreType`, `generatedType`/`asExpression`: type unknown; any other annotation (union with `null`/`undefined`, type references to aliases, enums, classes, interfaces, `Promise`, arrays, missing annotation): type unknown, never inferred (A1, A13); a type the dialect does not support: TO-P1 | TO-V3, TO-V6 |
| TO-C3 | special columns: `@CreateDateColumn`, `@UpdateDateColumn`, `@DeleteDateColumn`, `@VersionColumn` | type without an explicit one: postgres `timestamp without time zone` (`PostgresDriver.js:170-176`), sqlite `datetime` (`AbstractSqliteDriver.js:125-131`), version `integer`; create/update default `now()` (postgres) / `datetime('now')` (sqlite) unless the column states a `default`; delete-date columns nullable (`metadata/ColumnMetadata.js:269`); create/update/version not null | an explicit non-literal type: as TO-C2; `precision` on date columns: type unknown (mapped dialects excepted, cited); an explicit falsy literal default (`0`, `""`, `false`) on a create or update date column: default unknown (0.3 replaces it with the driver default, `metadata/ColumnMetadata.js:240-241`; 1.x keeps it, `:228`, `??=`; open point O19) | TO-V2 |
| TO-C4 | column facts of literal options: `nullable` (default `false`: not null), `default` (string, number, boolean, `null`, a `() => "<sql>"` arrow with a literal body, a member of a TypeScript enum captured by S8 and resolved by the resolver), `unique`, `name`; keys with no table fact: `select`, `insert`, `update`, `transformer`, `comment` (not compared), `cascade`, `eager`, `lazy`, `persistence` | nullability (primary keys not null); default per dialect and type family: postgres `normalizeDefault` (`PostgresDriver.js:793-830`), sqlite (`AbstractSqliteDriver.js:465-483`), mysql (`MysqlDriver.js:642-670`); a string `'<value>'` on a character, text (not MySQL's TEXT) or enum column only; on a boolean column a boolean string (true/false, t/f, yes/no, y/n, on/off, 1/0, any case) and a number 1/0 as postgres stores them (`true`/`false`) and on sqlite as written; a number as written on postgres integer, numeric and character columns (quoted on enum columns) and on sqlite, quoted (`toFixed`) on mysql number, boolean, character and enum columns; a boolean `true`/`false` on a postgres boolean column, `1`/`0` on sqlite and on mysql boolean and number columns; a function default as its text, except on postgres a text containing `CURRENT_TIMESTAMP`, `CURRENT_DATE`, `CURRENT_TIME`, `LOCALTIMESTAMP` or `LOCALTIME`, where exactly `CURRENT_TIMESTAMP` is `now()` and any other such text is unknown (`PostgresDriver.js:1270-1304`; **1.x** rewrites only a whole keyword with an optional precision, `:1455-1486` at 1.1.3, so the same claim holds and other texts stay unknown); `unique: true` a single-column unique constraint (`Column.js:54-58`); a default's `verification` is its dialect's only for a form the oracle fixture compares on that dialect (`oracleDefaultForms`), else `mapped` | a non-literal value: that fact unknown; a string default on a date or time, uuid, number, json or other column (the database stores its own rendering) or on a column whose type is not known, a boolean on a postgres column that is not boolean, a number on a postgres date or time, uuid or other column, a number other than 1/0 or another string on a boolean column: unknown (Revision 4 A1, A2; the texts the database rejects are TO-P1 (10) to (12)); a string default containing `'` (not escaped) or a fractional number default on an integer column on postgres: TO-P1 (both majors: `PostgresDriver.js:806` / `:931`); an object default (json, jsonb) containing `'`: unknown (1.x doubles the quote, `:937`; 0.3 does not, `:816`) | TO-V2 |
| TO-C5 | enum columns: `type: "enum"` (postgres, mysql, mariadb) or `"simple-enum"` with `enum:` an array literal of strings/numbers or an identifier resolving to an S8 enum (values: the non-numeric keys' values, `metadata/ColumnMetadata.js:184`), `enumName` literal | postgres: an enum type (TO-K2 name) with the labels in order (numbers as strings); sqlite: `varchar` with a `CHECK (col IN (...))` (labels claimed, constraint unnamed); mysql/mariadb `ENUM(...)` (mapped) | `enum` non-literal or an unread enum: type and labels unknown; `enum` on sqlite: TO-P1 | TO-V2 |
| TO-C6 | (Revision 1 O3) project wrapper decorators: a decorator imported from a package module whose declaration is a module-level `function F(options)` (or a `const` arrow) whose last statement is `return Column(options)` (or `PrimaryColumn`, `CreateDateColumn`, `UpdateDateColumn`, `DeleteDateColumn` from `typeorm`), all earlier statements only assigning `options.type` or `options.default` (seerr `DbAwareColumn`, `server/utils/DbColumnHelper.ts:15`, 15 sites) | the column as the inner decorator with the call's literal options: name, nullability, unique and primary claimed; type and default unknown when the wrapper assigns them | any other body (renames, loops, conditionals over other keys), a wrapper from another package, a non-literal options argument: TO-P2 | TO-V2, TO-V5 |
| TO-X1 | `@Index()` on a property (a column or a `@ManyToOne`/`@OneToOne` owner: its join columns), `@Index("name")`, `@Index({unique, where})`, class `@Index(["a", "b"], opts?)`, `@Index("name", ["a", "b"], opts?)` with property names (relations: their join columns) | an index: columns in order, `unique`, partial (`where` present), explicit name; junction tables: one index per side | function form `@Index((e) => [...])`, `synchronize: false` (TypeORM never creates the index; aqualink 2 sites), **1.x** `type` (`decorator/Index.js:44`, `metadata/IndexMetadata.js:49-52` at 1.1.3), `spatial`, `fulltext`, `parser`, `sparse`, `background`, `expireAfterSeconds`, a non-literal element: that index unknown (`partial`); predicate text and generated names: never claimed | TO-V2 |
| TO-X2 | class `@Unique(["a", "b"])`, `@Unique("name", ["a", "b"])` (relation properties: their join columns), column `unique: true` (TO-C4), the one-to-one owner's unique join columns (TO-R2) | a unique constraint: columns, explicit name | non-literal fields: that constraint unknown; `deferrable`: D | TO-V2 |
| TO-X3 | class `@Check("<sql>")`, `@Check("name", "<sql>")` | a check constraint; explicit name claimed; the expression text as written (not compared: the database rewrites it) | non-literal: unknown; a property-level `@Check` (aqualink 1 site) and `@Exclusion`: D | TO-V2 |
| TO-X4 | (Revision 1 O5) generated constraint and index names: `PK_`, `UQ_`, `REL_`, `FK_`, `IDX_`, `CHK_` + sha1 of the table and sorted column names (`naming-strategy/DefaultNamingStrategy.js:46-113`) for constraints and indexes without an explicit name; the hash is per major: 0.3.x SHA-1 of `encodeURIComponent(key)` (`util/RandomGenerator.js:17-43`), 1.x standard SHA-1 of the key (`util/StringUtils.js:98-99`, `DefaultNamingStrategy.js:124-126` at 1.1.3); they differ when the key holds a character `encodeURIComponent` escapes (a partial index `where`, a check expression) | the name, under the shown assumption "schema made by synchronize" | the data source has a `migrations` option or a migrations folder is found (seerr, nest-admin): unknown | TO-V2 |
| TO-R1 | `@ManyToOne(() => T, inverse?, options?)` with `T` a reached entity, optional `@JoinColumn({name, referencedColumnName, foreignKeyConstraintName})` or `@JoinColumn([{...}, ...])`; lazy (`Promise<T>`) and eager alike | join columns (TO-K2 names) referencing the target's primary columns or the named ones (`RelationJoinColumnBuilder.js:103-118`), each typed as the referenced column (integer for an `increment` key, `uuid`/`varchar` for a `uuid` key, length kept); nullable unless `nullable: false` or primary (`metadata/RelationMetadata.js:173-174`); a foreign key unless `createForeignKeyConstraints: false` (`:178-179`, `RelationJoinColumnBuilder.js:62-67`) with `onDelete`/`onUpdate` as written (upper case), else `NO ACTION` (`metadata/ForeignKeyMetadata.js:32-33`); explicit `foreignKeyConstraintName` claimed; a join column whose name equals the database name of an explicit column of the same entity reuses that column: its own nullability, default and uniqueness stay, its type becomes the referenced column's (`RelationJoinColumnBuilder.js:141`, `:186-190` / `:163`, `:205-209`; Maintainerr 9, aqualink 12 sites); explicit composite `@JoinColumn([...])` columns in array order (0.3.x) or **1.x** in the target's primary-key order (`:129-137` at 1.1.3); on a MySQL-only group, an index on the foreign key's columns named as the foreign key (`mapped`) when no declared index, unique constraint or the primary key leads with them (InnoDB creates it: MySQL reference manual, "FOREIGN KEY Constraints"; TypeORM names every constraint and reads an index named like a referential constraint as the foreign key's, `driver/mysql/MysqlQueryRunner.js:1507-1520`, `:1881`) | an unresolved or string target, a referenced column that is unknown or not found: the join column and foreign key unknown (`partial`); an explicit same-named column the rules cannot read (TO-P2): the join column unknown; `deferrable`, `orphanedRowAction` (no table fact), `primary` (removed in 0.3): D; on MariaDB, or with MySQL and another dialect in the set, such a foreign key's index leaves the constraint set `partial` (its name on MariaDB, or its absence on the other dialects, is not read), as does a foreign key on primary key columns the primary key may not lead with | TO-V2 |
| TO-R2 | `@OneToOne(() => T, inverse?, options?)` with `@JoinColumn(...)` (the owner side) | as TO-R1, plus a unique constraint on the join columns unless they are all primary or no foreign key is created (`RelationJoinColumnBuilder.js:81-95`) | as TO-R1 | TO-V2 |
| TO-R3 | inverse sides: `@OneToMany(() => T, inverse, options?)`, `@OneToOne` without `@JoinColumn`, `@ManyToMany` without `@JoinTable` | no table fact (counted; the owner side's row claims) | an inverse side whose owner is not read: nothing to claim | - |
| TO-R4 | `@ManyToMany(() => T, inverse?, options?)` with `@JoinTable()` or `@JoinTable({name, joinColumn: {name, referencedColumnName}, inverseJoinColumn: {...}})` | a junction table (TO-K2 names): one column per primary (or named referenced) column of each side, typed as referenced, not null, together the primary key; two foreign keys: the owner side's with the relation's `onDelete`/`onUpdate`, else `CASCADE`; the inverse side's with the inverse relation's `onDelete`/`onUpdate` when the owner names an inverse property (`NO ACTION` when that relation states none), else `CASCADE` (`JunctionEntityMetadataBuilder.js:162-198`); **1.x:** the inverse relation's `onDelete`/`onUpdate` each, `CASCADE` when it states none (`?? "CASCADE"`, `:177-190` at 1.1.3), composite sides in primary-key order (`:241`, `:273`); one index per side | `joinColumns`/`inverseJoinColumns` arrays, `synchronize: false`, `schema`/`database` on `@JoinTable`, an unresolved side: the junction unknown (marker) | TO-V2 |
| TO-P1 | failures `DataSource.initialize()` or `synchronize` would hit, provable from the source: (1) an entity without a primary column (`metadata-builder/EntityMetadataValidator.js:48`); (2) an explicit type the dialect does not support, e.g. `datetime` on postgres, `timestamp` or `char` on sqlite (`:89`; the driver's `supportedDataTypes`, `PostgresDriver.js:50-126`, `AbstractSqliteDriver.js:37-70`); (3) a design type proven `Object`: `@Column()` without an explicit type on a property without an annotation (`typescript.js:95541-95543`), or with a union containing `null`/`undefined` and a non-null member when the tsconfig chain proves `strictNullChecks` (`:95667-95677`), or `any`/`unknown`/object-literal types, except on a column that merges with a join column (TO-R1: its type is replaced before validation; aqualink 3 sites); (4) a string default containing `'`; (5) a fractional number default on an integer column on postgres; (6) `enum` on sqlite; **1.x:** (7) a candidate `type: "sqlite"` (`MissingDriverError` from the `DataSource` constructor, `driver/DriverFactory.js:72`), (8) an entity module using `@RelationCount` (`TypeError` at class evaluation), (9) an `@Index` with a non-empty string literal `type` option on any dialect but postgres (`Unsupported index type` from the `IndexMetadata` constructor, `metadata/IndexMetadata.js:49-52` at 1.1.2 and 1.1.3: only `PostgresDriver` has `supportedIndexTypes`; Revision 3 addendum 3 OP1; on postgres the index stays a TO-X1 refusal); **Revision 4:** (10) postgres: a string default `CURRENT_TIMESTAMP`, `CURRENT_DATE`, `CURRENT_TIME`, `LOCALTIMESTAMP` or `LOCALTIME` (any case, an optional precision) on a date or time column (PG17: invalid input syntax for type timestamp); (11) postgres: a string default containing `(` on a uuid column (PG17: invalid input syntax for type uuid); (12) mysql: a string default `CURRENT_TIMESTAMP`, `CURRENT_TIMESTAMP(n)` or `NOW()` on a date or time column (quoted, `MysqlDriver.js:646-649`: a string literal that is no date, ER_INVALID_DEFAULT; MySQL reference manual, "Data Type Default Values"; `mapped`); (13) a `length` on a type outside the driver's `withLengthColumnTypes` (postgres `PostgresDriver.js:138-148`, sqlite `AbstractSqliteDriver.js:78-88`, mysql and mariadb `MysqlDriver.js:124-131`): `EntityMetadataValidator.js:89-92` (1.1.3 `:92-94`) "does not support length property" (int on every dialect, text on postgres and mysql); (14) postgres: a `precision` without a `length` on `integer`, `smallint`, `bigint`, `text`, `boolean`, `uuid`, `date`, `double precision`, `real`, `json` or `jsonb` (`createFullType` writes `integer(5)`, `PostgresDriver.js:850-864` / 1.1.3 `:1011-1025`; PG17: syntax error, or type modifier is not allowed) | diagnostic `schema_model_import_failure` (row named); no claim for any table of the group on that dialect: validation runs for every entity before any DDL, and the schema build runs in one transaction that is rolled back (`schema-builder/RdbmsSchemaBuilder.js:52-78`) | other failures: assumed absent (TO-V2) | TO-V2, TO-V3 |
| TO-P2 | project decorators and helpers not covered by TO-C6 or TO-E3: a decorator on an entity member not imported from `typeorm` or a TO-E3 package, a project call as an option value | refused: the member's column (name, type and every fact) unknown and the table's column set `partial`, reason naming the wrapper; a helper value: that fact unknown | always refused | - |
| TO-U1 | the `migrations` option's globs and directories | `unread_tools` entry (found, not read yet) | always unread | - |
| TO-V | assumptions TO-V1..TO-V7 attached to every record of the group | stated, never claimed as facts | - | - |
| D | anything else: TypeORM majors other than 0.3 and 1, `EntitySchema`, a custom `namingStrategy` other than TO-K6 (decision 2026-11-13, Revision 2 O11), 1.x-only `deferrable` on relations and exclusions, `@TableInheritance`/`@ChildEntity`, `@ViewEntity`/`@ViewColumn`, `@Tree`/`@TreeChildren`/`@TreeParent`/`@TreeLevelColumn`, embedded `@Column(() => X)`, `@ObjectIdColumn`/mongodb, `@Generated`, `@ForeignKey`, `@Exclusion`, generated columns, dialects outside the five and the SQLite family, NestJS forms outside TO-K3-K5 (`useClass`/`useExisting` options, custom `dataSourceFactory`, `forRoot()` without options), metadata mutations, option objects not read | no claim; marker diagnostic `schema_model_unread` with `tool: "typeorm"` and the form | - | - |

Rows TO-R3, TO-U1 and TO-V exist so the inventory can count every form
and the contract test can name them. The claim-side rows are TO-K1, K2,
K3, K4, K5, K6, K7, K8, E1, E2, C1, C2, C3, C4, C5, C6, X1, X2, X3, X4,
R1, R2, R4 (twenty-three; TO-E3 only removes a refusal and claims
nothing itself; TO-K7 and TO-K8 only widen which options TO-K1 reads).

## TypeORM 1.x comparison

Copied from the task file's TA3 section: the 0.3.31 and 1.1.3
sources of every row.

"Same" means the cited 0.3.31 code and the 1.1.3 code compute the same
result for every form the row claims (compiled code compared after
removing comments; most differences are `||` -> `??`, `substr` ->
`slice`, `replace` -> `replaceAll` and `connection` -> `dataSource`).
Lines: `0.3.31 file:line` / `1.1.3 file:line`; unprefixed paths are
under the `typeorm` package root. The rows TA3 added (TO-K6, K7, K8) read the same
under both majors (TO-K6's oracle runs used 0.3.31 and 1.1.3).

| row | 1.x | 0.3.31 | 1.1.3 | notes |
|---|---|---|---|---|
| TO-K1 | differs (A32, A37) | `driver/DriverFactory.js:43` (`case "sqlite"`); `globals.js:4` (`getConnectionOptions`); `util/DirectoryExportedClassesLoader.js:14`, `:32` (`glob.sync`) | `driver/DriverFactory.js:39-72` (no `sqlite` case: `MissingDriverError`); `globals.js:3` (only `getMetadataArgsStorage`); `util/DirectoryExportedClassesLoader.js:16`, `:39` (`tinyglobby.globSync`) | `type: "sqlite"` (the `sqlite3` driver) is removed; `better-sqlite3` and the other SQLite-family types remain; `mysql` uses `mysql2` only (`MysqlDriver.js:917`, no DDL effect); ormconfig files are no longer read by the runtime (only the exported `ConnectionOptionsReader` class, used by nothing internal); the same loader formats (`.js`, `.mjs`, `.cjs`, `.ts`, `.mts`, `.cts`, not `.d.ts`); `DataSource` reads `entities`, `entityPrefix`, `namingStrategy` as before (`data-source/DataSource.js:59-113`, `:482` / `:60-112`, `:521`); the oracle's glob run (an absolute `{a,b}` pattern, a cwd-relative `**`/`?` pattern and a `.d.ts` pattern) loaded the same 20 tables under both versions; engines `node ^20.19 \|\| ^22.13 \|\| >=24.11` |
| TO-K2 | same | `naming-strategy/DefaultNamingStrategy.js:26-28`, `:37-42`, `:114-131`, `:139`; `util/StringUtils.js:16-36`; `metadata-builder/RelationJoinColumnBuilder.js:137`; `metadata-builder/JunctionEntityMetadataBuilder.js:30-31`, `:58`, `:272-275`; `driver/postgres/PostgresQueryRunner.js:2597-2612` | `naming-strategy/DefaultNamingStrategy.js:13-15`, `:19-24`, `:96-115`; `util/StringUtils.js:17-39`; `metadata-builder/RelationJoinColumnBuilder.js:159`; `metadata-builder/JunctionEntityMetadataBuilder.js:33-34`, `:60`, `:287-290`; `driver/postgres/PostgresQueryRunner.js:2961` | names equal on every probe, seerr and nest-admin table (oracle); only empty-string names differ (`@Entity("")`, `@JoinTable({name: ""})`: `??` keeps `""`, 0.3 falls back), unknown on both; the junction-column wording was corrected for both versions (A30) |
| TO-K3 | same reading, more versions | `@nestjs/typeorm` 11.0.0 `dist/typeorm-core.module.js:143-183` | 12.0.3 `dist/typeorm.module.js:25-26` (`forRoot(options, name)`), `dist/typeorm-core.module.js:172-196` | 12.x is ESM-only (`"type": "module"`); `forRoot` takes the data source name as a second argument (read as the group name when it is a literal); the default factory is still `new DataSource(options)` (`:174-175`) |
| TO-K4 | differs in 12.x (A41) | 11.0.0 `typeorm-core.module.js:166-183` | 12.0.3 `typeorm-core.module.js:178-190` | with `autoLoadEntities`, 12.x appends the `forFeature` list to an array or to an object map's values (`Object.values`), where 11.0.0 replaced a non-array `entities`; a string (glob) `entities` with `autoLoadEntities` gives TypeORM single characters as patterns in 12.x: `may_be_incomplete` |
| TO-K5 | same | `@nestjs/config` (no TypeORM code) | same | - |
| TO-E1 | differs (A36) | `index.js:55` (`RelationCount` exported), `decorator/entity/Entity.js:9-22` | `index.js` (no `RelationCount`, no `EntityRepository`, no `AbstractRepository`), `decorator/entity/Entity.js:9-22` | `@RelationCount` is gone: importing it gives `undefined` and the decorator call throws at class evaluation (seerr under 1.1.3: 5 entity modules fail to load); entity options read the same (`??` only) |
| TO-E2 | same | `metadata-builder/EntityMetadataBuilder.js` inheritance tree | same | `BaseEntity` still exported; inherited members first (oracle) |
| TO-E3 | same | - | - | the three packages never touch TypeORM |
| TO-C1 | same | `decorator/columns/PrimaryGeneratedColumn.js`, `decorator/columns/Column.js:25-33` | `decorator/columns/PrimaryGeneratedColumn.js`, `decorator/columns/Column.js:27-35` | still legacy decorators `(object, propertyName)` reading `design:type`: TO-V3 unchanged |
| TO-C2 | same for the lists | `driver/postgres/PostgresDriver.js:50-126`, `:732-789`, `:850`; `driver/sqlite-abstract/AbstractSqliteDriver.js:37-70`, `:433-461`; `driver/mysql/MysqlDriver.js:578-640`, `:682-700`, `:707-730` | `driver/postgres/PostgresDriver.js:76-152`, `:851-908`, `:1011`; `driver/sqlite-abstract/AbstractSqliteDriver.js:61-95`, `:474-506`; `driver/mysql/MysqlDriver.js:596-660`, `:707-725`, `:734-755` | Postgres and SQL Server type tables, `normalizeType` and `createFullType` identical; SQLite adds `jsonb` (outside the list: unknown); MySQL drops the `width` option and the integer display widths of `dataTypeDefaults` (`width`/`zerofill`/`readonly` column options are gone in 1.x; they were already refusals) and reads `Uint8Array` where 0.3 read `Buffer` (no listed type) |
| TO-C3 | same except falsy defaults | `metadata/ColumnMetadata.js:240-241`, `:252-253` (`if (!this.default)`) | `metadata/ColumnMetadata.js:228`, `:238` (`??=`) | an explicit `0`, `""` or `false` default on a create/update date column: 0.3 replaces it with the driver default, 1.x keeps it: unknown on both (O19); mapped types and defaults identical (`PostgresDriver.js:170-176` / `:196-202`, `AbstractSqliteDriver.js:125-131` / `:150-156`) |
| TO-C4 | same for the claimed forms | `driver/postgres/PostgresDriver.js:793-830`, `:1270-1304`; `driver/sqlite-abstract/AbstractSqliteDriver.js:465-483` | `driver/postgres/PostgresDriver.js:914-940`, `:1455-1486`; `driver/sqlite-abstract/AbstractSqliteDriver.js:508-535` | string defaults still unquoted inside `'...'` (`:806` / `:931`: TO-P1 (4) holds); Postgres rewrites only a whole keyword with an optional precision (`/^(CURRENT_TIMESTAMP\|...)(\(\d+\))?$/`), so exact `CURRENT_TIMESTAMP` is `now()` on both (seerr's 10 sites agree under 1.1.3) and other keyword texts stay unknown; JSON object defaults double `'` in 1.x (`:937`) but not in 0.3 (`:816`); SQLite 1.x renders object defaults as JSON (`jsonb(...)` for `jsonb`, `:529-534`) where 0.3 wrote `[object Object]`: object defaults stay outside the SQLite list |
| TO-C5 | same | `metadata/ColumnMetadata.js:176-184`; `PostgresQueryRunner.js:2597-2612` | `metadata/ColumnMetadata.js:163-171`; `PostgresQueryRunner.js:2959-2970` | enum labels and type names equal on the probe enums (oracle) |
| TO-C6 | same | - | - | the wrapped decorators are unchanged |
| TO-X1 | differs (new option) | `decorator/Index.js:17-42`, `metadata/IndexMetadata.js` | `decorator/Index.js:17-48` (`type`, `:44`), `metadata/IndexMetadata.js:49-52` (`Unsupported index type`), `driver/postgres/PostgresDriver.js:223` (`supportedIndexTypes`), `PostgresQueryRunner.js:2728` | `@Index({type: "gin"})` and the other index methods exist only in 1.x: that index unknown (refusal added); everything else equal (oracle) |
| TO-X2 | same | `decorator/Unique.js`, `metadata/UniqueMetadata.js` | same | - |
| TO-X3 | same | `decorator/Check.js:8` | `decorator/Check.js:8` | - |
| TO-X4 | differs (A35) | `naming-strategy/DefaultNamingStrategy.js:46-113` with `util/RandomGenerator.js:17-43` (SHA-1 of `encodeURIComponent(key)`) | `naming-strategy/DefaultNamingStrategy.js:28-94`, `:124-126`, `util/StringUtils.js:98-99` (`PlatformTools.sha1`, standard SHA-1) | keys of identifier characters hash the same (31 seerr, 11 nest-admin, 13+4+8 probe names agree under both); a key with a space, quote or other escaped character (a partial index `where`, a check expression) gives another name: the probe's partial index is `IDX_8cc03de...` under 0.3.31 and `IDX_6af7f1a...` under 1.1.3 on both dialects |
| TO-R1 | differs for composite keys (A34) | `metadata-builder/RelationJoinColumnBuilder.js:62-67`, `:103-118`, `:137`, `:141`, `:186-190`; `metadata/RelationMetadata.js:68-74`; `metadata/ForeignKeyMetadata.js:32-33` | `metadata-builder/RelationJoinColumnBuilder.js:64-69`, `:118-139`, `:159`, `:163`, `:205-209`; `metadata/RelationMetadata.js:68-74`; `metadata/ForeignKeyMetadata.js:32-33` | explicit composite `@JoinColumn([...])` columns are sorted by the target's primary-key order in 1.x (`:129-137`), so the foreign key lists `r_code, r_region` where 0.3 lists `r_region, r_code`; the relational column no longer copies `width`/`zerofill`; the merge with an explicit same-named column is the same in both (A31) |
| TO-R2 | same | `RelationJoinColumnBuilder.js:81-95` | `RelationJoinColumnBuilder.js:83-97` | - |
| TO-R3 | same | - | - | - |
| TO-R4 | differs (A33) | `metadata-builder/JunctionEntityMetadataBuilder.js:172-196` | `metadata-builder/JunctionEntityMetadataBuilder.js:156-191` | inverse foreign key: `relation.inverseRelation?.onDelete ?? "CASCADE"` in 1.x, where 0.3 took the inverse relation's value even when undefined (then `NO ACTION`, A19); composite junction columns sorted by primary-key order (`:241`, `:273`); `deferrable` now passed (D) |
| TO-P1 | more failures | `metadata-builder/EntityMetadataValidator.js:48`, `:89`; `schema-builder/RdbmsSchemaBuilder.js:52-78` | `metadata-builder/EntityMetadataValidator.js:55`, `:91`; `schema-builder/RdbmsSchemaBuilder.js:60-86` | the six failures of the investigation fail the same way under 1.1.3 (14 failure-probe runs, same messages); new: `type: "sqlite"` (7) and `@RelationCount` (8) |
| TO-P2 | same | - | - | - |
| TO-U1 | same | `connection/ConnectionMetadataBuilder.js:19-21` | `connection/ConnectionMetadataBuilder.js:18-20` | migration classes are instantiated with `new` (no container); not read either way |
| TO-V | extended | - | - | TO-V9 (verified versions) and TO-V10 (naming-strategy package) added; TO-V3 holds at TypeScript 5.4.5, 5.9.3, 6.0.3 |
| D | extended | - | - | 1.x-only forms (`@Index({type})`, `deferrable` on relations and exclusions) and removed forms (`@RelationCount` as P1 (8), `@EntityRepository`, `AbstractRepository`, `Connection`) join the existing D list; `@ViewEntity`, `@Tree`, `@ChildEntity`, embedded, `EntitySchema` unchanged |

## SnakeNamingStrategy names (TO-K6)

Port of `typeorm-naming-strategies@4.1.0` `snake-naming.strategy.js`
(the class extends TypeORM's `DefaultNamingStrategy` and imports
`snakeCase` from `typeorm/util/StringUtils`, so TypeORM's own `snakeCase`
of the installed version applies: `util/StringUtils.js:29-36` at
0.3.31, `:32-39` at 1.1.3, the same regexes):

| name | SnakeNamingStrategy | source | default naming (TO-K2), for contrast |
|---|---|---|---|
| table | the literal name, else `snakeCase(className)` | `:7-9` | same |
| prefixed table | inherited `prefixTableName` | `DefaultNamingStrategy.js:139` / `:115` | same |
| column | the literal `name` verbatim, else `snakeCase(propertyName)` (embedded prefixes snake-cased; embedded is D) | `:10-13` | the property name |
| join column | the `@JoinColumn` `name`, else `snakeCase(relationProperty + "_" + referencedColumnProperty)`; TypeORM passes the referenced column's property name (`RelationJoinColumnBuilder.js:137` / `:159`) | `:17-19` | `camelCase(...)` of the same |
| junction table | `@JoinTable` `name`, else `snakeCase(ownerTable + "_" + property + "_" + targetTable)` of the unprefixed names | `:20-26` | same |
| junction columns | `snakeCase(table + "_" + referencedColumnDatabaseName)` (the database name is already snake case or explicit), `_1`/`_2` inherited for a self relation | `:27-29`; `DefaultNamingStrategy.js:124-126` / `:106-108` | `camelCase(...)` of the same (A30) |
| Postgres enum type | `<table>_<lower(snake column name)>_enum` (the query runner uses the database column name) | `PostgresQueryRunner.js:2601` / `:2961` | same rule on the default column name |
| constraint and index names | inherited, over the snake database names (TO-X4 version rule) | `DefaultNamingStrategy.js:46-113` / `:28-94` | same |
| closure-table, tree and inheritance names | `classTableInheritanceParentColumnName` `:30-32`; others inherited | - | D either way |


## Claim pass

`Link` runs discovery, then reads every claimable group's reached `@Entity`
classes (typeorm's decorator, `Form` Entity) into kit tables, one kit
group per data source or NestJS root (`models/typeorm/<root>`); each
entity is one isolated step (a failure becomes a
`schema_model_internal_error` diagnostic and the other entities are still
read). Every entity's own columns are read first, then the
relations of every entity (they read their targets' columns), then
TO-P1 (3), then the constraints of each table.

- **Group and dialects.** The kit group states the group's single
  dialect, or the run-time dialect set (ADR 0065) of the dialects left
  after TO-P1: a provable failure on one dialect removes that dialect
  from the group (validation runs for every entity before any DDL and the
  schema build is one transaction), with a `schema_model_import_failure`
  diagnostic naming the row and the form; when no dialect is left the
  group claims no table. Assumptions: TO-V1 to TO-V5 and TO-V9 always;
  TO-V6 with a mapped dialect; TO-V7 when a table has a schema; TO-V8
  when a registerAs namespace supplied options (TO-K5); TO-V10 with
  TO-K6. Pins are the package's.
- **Verification.** A per-dialect fact carries its dialect's verification
  (oracle: postgres, sqlite, better-sqlite3; mapped: mysql, mariadb,
  mssql, the other SQLite-family drivers); a dialect-independent fact the
  weakest of the group's dialects; every fact and the table of a
  `synchronize: false` entity mapped (Revision 1 O4), with the table
  reason "TypeORM does not create this table; this is the shape the code
  expects" and the table attribute `synchronize: false`.
- **Tables (TO-E1, TO-K2, TO-K6).** Identity `table:<file>#<Class>`; an
  entity two claimable groups of the package reach (Maintainerr's CLI
  data source and NestJS root) gets `<Class>@<group root>` as its model
  name in both, so the identities stay distinct. The table name is the
  literal `@Entity` name (positional or `name`), else `snakeCase(class)`,
  prefixed by `entityPrefix`; the schema is the entity's or the options'
  (postgres only: a schema on another dialect leaves the table not
  claimed, marker). A non-literal or empty name, the `database` option
  (D) and an anonymous class leave the table not claimed (marker);
  `withoutRowid`, `engine`, `expression` and unknown entity options make
  the column set partial.
- **Members (TO-E2, TO-E3, TO-P2, D).** Base class members first (an
  `extends` chain through repository classes; typeorm's `BaseEntity`
  ends it; another base makes the column set partial). Per member: a
  typeorm column decorator is a column; `@RelationId`, `@RelationCount`
  (0.3), `@VirtualColumn`, the listeners and TO-E3's packages have no
  fact; the relation, `@JoinColumn`, `@JoinTable`, `@Index`, `@Unique`
  and `@Check` decorators are read below; any other typeorm
  decorator (`@Tree*`, `@ObjectIdColumn`, `@Generated`, ...) is D (marker;
  a column it sits on is unknown, the column set partial); an embedded
  `@Column(() => X)` is D; a project decorator is read as a TO-C6
  wrapper or refused (TO-P2: no column record, the column set partial);
  a decorated method or computed member is D.
- **Columns (TO-C1 to TO-C6).** The column name is the literal `name`,
  else the property (TO-K6: `snakeCase(property)`); a non-literal name
  leaves the column out and the set partial. Facts: `name`;
  `primary_key` (`@PrimaryColumn`, `@PrimaryGeneratedColumn`,
  `primary: true`); `nullable` (primary false, delete date true, else the
  literal, default false); `unique` true for `unique: true`; per dialect `type` (the catalog spelling on
  postgres, the declared spelling on sqlite, the DDL spelling on MySQL:
  `character varying(40)`, `varchar(40)`, `varchar(255)`), `length`,
  `precision`, `scale`, `autoincrement` (true for an increment or
  identity key; never claimed false), `server_default` and
  `server_default_text`; `default` (`server` or `absent`) when every
  dialect agrees; the enum labels as the attribute `enum.values`
  (TO-C5, covered by the type claim). A column whose type or default the
  rules do not reach has that fact unknown with a reason per dialect.
- **Types.** Explicit types from the TO-C2 lists only (`render.go`);
  length only on the length types (`character varying`, `character`;
  sqlite `varchar`, `character`, `text`; MySQL `varchar`, whose default
  length 255 is claimed), precision and scale only on postgres
  `numeric`, sqlite `decimal`/`numeric` and MySQL `decimal` (a MySQL
  `decimal` without precision is unknown: MySQL's own default applies);
  postgres `character` without a length is unknown (not compared); arrays
  on postgres only; mssql types are unknown (no mapped list). Design
  types from a bare `string`, `number`, `boolean` or `Date` annotation
  only, and only when `emitDecoratorMetadata` is proven true. Special
  columns: postgres `timestamp without time zone`, sqlite `datetime`,
  MySQL and MariaDB `datetime(6)` (`MysqlDriver.js` mappedDataTypes
  :176-185), version `integer` / `int`. Generated keys: increment
  `integer` / `int` (with an explicit integer type, that type), sqlite's
  autoincrement only on the single integer primary key; uuid `uuid`,
  sqlite `varchar`, MySQL `varchar(36)` (`getColumnLength`), MariaDB
  unknown; identity on postgres only; `rowid` and `@Column({generated})`
  unknown. Enums: postgres the TO-K2 enum type name (unknown with an
  entity prefix or a schema unless `enumName` is set), sqlite `varchar`
  (`simple-enum`), MySQL and MariaDB `enum`; an enum option without type
  `enum`/`simple-enum`, an enum array or a `const enum` is unknown.
- **Defaults.** Strings `'<value>'`; numbers as written on postgres and
  sqlite (quoted on a postgres enum column), `'<n>'` for integers on
  MySQL (`normalizeDefault` :650, `toFixed` without a scale; fractional
  or scaled numbers unknown there); booleans `true`/`false` (postgres),
  `1`/`0` (sqlite, MySQL); function texts as written, except the postgres
  datetime rule of TO-C4 and, on MySQL, datetime texts other than
  `CURRENT_TIMESTAMP` and `CURRENT_TIMESTAMP(<n>)` (unknown; every
  datetime text unknown on MariaDB, which writes
  `CURRENT_TIMESTAMP()`); an enum member's value; object and array
  defaults and mssql defaults unknown. Create and update dates without a
  default take the driver's (`now()`, `datetime('now')`,
  `CURRENT_TIMESTAMP(6)`); an increment key on postgres has a server
  default whose text (the sequence) is unknown, on sqlite and MySQL none;
  a postgres uuid key `uuid_generate_v4()` (unknown when the options set
  `uuidExtension`); an identity key's default is not claimed.
- **Failures (TO-P1).** (1) no primary column when the column set is
  complete; (2) an explicit type outside the driver's supportedDataTypes
  after normalizeType (postgres, sqlite, MySQL, MariaDB lists in
  `render.go`); (3) a design type proven `Object` (no annotation;
  `any`, `unknown`, `object`; a union with `null` or `undefined` under a
  proven strictNullChecks), not claimed on an entity with an owning
  relation (a merged join column's type is replaced before
  validation); (4) a string default with a quote (postgres, sqlite,
  MySQL, MariaDB); (5) a fractional default on an integer column on
  postgres; (6) `enum` on sqlite; (7) from discovery; (8) `@RelationCount`
  under 1.x; (9) a 1.x `@Index` with a literal `type` option on any
  dialect but postgres, read with the `@Index` before its other options
  (Revision 3 addendum 3 OP1). (3) is decided after the relation pass: a column a join
  column reuses (TO-R1) is no failure, and an entity with an
  owning relation that is not read proves none.
- **TO-C6.** A project decorator resolving to a module-level function or
  const arrow of the package with one parameter, whose last statement
  returns `Column`, `PrimaryColumn`, `CreateDateColumn`,
  `UpdateDateColumn` or `DeleteDateColumn` of typeorm called with that
  parameter, every earlier statement assigning only `<param>.type` or
  `<param>.default` (directly or inside if statements), and called with
  one object literal: the column as the inner decorator with those
  options; name, nullability, unique and primary claimed with row TO-C6;
  type and default unknown when the wrapper assigns them.
- **Relations (TO-R1, TO-R2, TO-R3).** A `@ManyToOne` or a `@OneToOne`
  with `@JoinColumn` whose target is `() => T` (or `(type) => T`) with
  `T` a reached entity of the group whose table is read: one join column
  per referenced column (the target's primary columns in column order,
  or the `referencedColumnName` properties; under 1.x several named ones
  sorted by the target's primary key). Names: the
  `@JoinColumn` `name`, else `camelCase(property + "_" + referenced
  property)` (TO-K6: `snakeCase`). A join column whose name equals an
  explicit column's reuses it: its own facts stay, its type
  per dialect is the referenced column's (unknown when either side has a
  length, precision or scale). Otherwise a new column typed as the
  referenced column on every dialect (length, precision and scale kept),
  nullable unless `nullable: false`, no default, never primary. A
  referenced enum, join or wrapper-typed column, an unresolved or string
  target, a target whose primary key may be hidden (an unread member
  that could be a primary column; a tree form or a typeorm `@Column`
  beside a refused project decorator cannot be), two relations sharing a
  join column or an unread `@JoinColumn` option leave the join columns
  out (column set partial) and the constraint set partial. The foreign key
  (all join columns, `local_columns` and `columns` in that order) unless
  `createForeignKeyConstraints: false` (then `foreign_keys: []`), with
  `onDelete`/`onUpdate` upper-cased from a literal of CASCADE, SET NULL,
  NO ACTION, RESTRICT, SET DEFAULT, else NO ACTION, and the explicit
  `foreignKeyConstraintName`; `deferrable` is D. A one-to-one owner adds
  a unique constraint on its join columns unless all are primary (TO-R2).
  `@OneToMany`, `@OneToOne` without `@JoinColumn` and `@ManyToMany`
  without `@JoinTable` are counted (TO-R3).
- **Junction tables (TO-R4).** A `@ManyToMany` with `@JoinTable()` or
  `@JoinTable({name, joinColumn, inverseJoinColumn})` (one object each;
  arrays and other options refused, marker): its own table record
  (identity `table:<owner file>#<junction name>`, attributes `junction:
  true` and `relation: "<Class>.<property>"`, the owner's schema), named
  by `@JoinTable` `name` else `snakeCase(ownerTable + "_" + property +
  "_" + targetTable)` of the unprefixed names, prefixed by `entityPrefix`;
  one column per referenced column of each side, named
  `camelCase(table + "_" + referencedDatabaseName)` (TO-K6: `snakeCase`),
  equal names suffixed `_1`/`_2`, typed as referenced, not
  null, together the primary key; the owner's foreign key with the
  relation's actions else CASCADE, the inverse one with the inverse
  relation's actions when the owner names an inverse property (0.3.x: NO
  ACTION when it states none; 1.x: CASCADE, A33) else
  CASCADE; one index per side.
- **Indexes (TO-X1).** `@Index()`, `@Index("name")`, `@Index({unique,
  where})` on a property (a column, or a relation with one join column),
  class `@Index(["a", "b"], opts?)` and `@Index("name", ["a", "b"],
  opts?)` with property names, on the entity and its base classes: each
  `{name, columns, unique, partial}` (partial: `where` present, read on
  postgres and sqlite only; the predicate is not claimed). The function
  form, `synchronize: false`, 1.x `type`, `spatial`, `fulltext`, `parser`,
  `sparse`, `background`, `expireAfterSeconds`, other options (D), a
  property that is not a read column or single-column relation: that
  index unknown, the constraint set partial (marker).
- **Unique and check constraints (TO-X2, TO-X3).** Class `@Unique([...])`
  and `@Unique("name", [...])` (relation properties: their join column),
  column `unique: true`, the one-to-one owners' join columns:
  `{name, columns}` in `unique_constraints` (on MySQL TypeORM writes each
  as a unique index, `EntityMetadataBuilder.js:549-570`, which MySQL
  reports as a unique constraint; its generated name is then IDX_);
  options (`deferrable`) and a property-level `@Unique` are D. Class
  `@Check("<sql>")` and `@Check("name", "<sql>")`: `{name, expression}` as
  written, except on MySQL and MariaDB, where TypeORM creates no check
  (`RdbmsSchemaBuilder.js:694-698`: not read, set partial); a
  property-level `@Check` is D. On a sqlite-only group a `simple-enum`
  column adds its unnamed inline CHECK (`AbstractSqliteQueryRunner.js:1306-1312`,
  TO-C5); with several dialects that CHECK exists on sqlite only, so the
  set is partial.
- **Names (TO-X4).** Explicit names are claimed. Generated names (`UQ_`,
  `REL_`, `FK_`, `IDX_`, `CHK_` with the per-major hash;
  MySQL unique indexes `IDX_`) only when the data source states no
  `migrations` option and the package holds no `migration` or
  `migrations` folder, and every live dialect generates the same name;
  else `null`. Primary key names are not written.
- **Column facts.** `foreign_keys` from the foreign keys above (claimed
  with the relation's row; `[]` claimed with TO-R1 when every relation is
  read), unknown on a column no read relation owns when the column set is
  partial, and on every column of an entity with an unread relation
  member. `index` true for a single-column non-unique index, `unique`
  true for a single-column unique constraint or non-partial unique index;
  both false when the constraint set is complete, else unknown.
- **Constraint set.** Complete when the column set is complete and every
  decorator, relation and constraint above is read; any refusal makes it
  partial with a reason. A group with an mssql dialect lists no
  constraints (the one-to-one unique is a filtered index there; not in
  the contract's mapped forms) and no foreign keys.

## Tests

- `TestTypeORMContract` (`contract_test.go`): the claim contract table
  above equals the task file's row for row; `contract.go` lists the same
  rows in the same order with the same assumptions and sides
  (twenty-three claim rows, TO-E3 and TO-R3 without a table fact, TO-P1
  and TO-P2 refusing only, TO-U1 marking only, TO-V without a branch, D
  last); every listed form is named in its row; the assumptions section
  equals the task file's. The tag half is `TestTypeORMFixtureTags`
: over the fixture every tag is a row or a listed form of
  its row, every row with a branch is reached and TO-V never, and every
  listed form of every row is reached. `TestTypeORMCatalogue`: the
  registered catalogue's rows, claim cells, branches and assumptions
  equal this document's (D is not served, `catalogue.go`).
- `discover_test.go` over inline sources: the candidate rule on seerr's
  selector shape (dialect set, class references, migrations, pins), the
  options left unread (unequal scope options, `let`, a mutated object, a
  scope spread, a call spread, a non-literal type, an unread selector
  statement, a computed key), TO-K7 imports (default and named), TO-K8
  spreads, TO-K6 and the naming forms that are D, major lines, 1.x
  `sqlite` (TO-P1 (7)), the SQLite family, mapped and D dialects, the
  entities elements and globs (braces, `__dirname` forms,
  `path.resolve`, build output, outside the subset, spreads,
  `EntitySchema`, unresolved names, D entity forms, unreached classes),
  the NestJS forms (forRootAsync through registerAs, forRoot with a name,
  12.x names, forFeature per name and in test files and functions, the
  unread forms), ormconfig (object, array, script, a data source winning,
  D forms) and the compiler flags (off, absent, a package `extends`, a
  child overriding its parent).
- `TestTypeORMCorpusDiscover` (`corpus_test.go`, set
  `GRAPHVEIN_TYPEORM_CORPUS` to a directory with the seerr, nest-admin,
  Maintainerr and aqualink-app checkouts of `scripts/cloud-corpus.sh`):
  discovery against the inventory's discovery rows both ways (every row
  found at its line; every group, options object, selector, entities
  array and forFeature call discovery reports has a row), and the groups
  and reached entities of the coverage targets: seerr one group, 14
  entities, dialect set postgres and sqlite; nest-admin the CLI data
  source (0 entities: dist glob, incomplete) and the forRootAsync root
  (read since Revision 3 addendum 2: the `try` statement and the async
  block `dataSourceFactory`; 15 entities through autoLoadEntities, the
  list incomplete through the dist glob); Maintainerr the CLI data source (17 entities by
  a cwd-relative glob, better-sqlite3, 1.x) and the forRoot root (17
  through TO-K7 and 10 forFeature calls); aqualink-app the data source
  (27 entities by `join(__dirname, ...)` globs, TO-K8 spread, TO-K6) and
  the forRoot root (a method call: not read).

- `TestTypeORMFixtureRows` (`fixture_test.go`, the `typeorm-models`
  fixture, part 1): cases per row of step TOB3 (names, the NestJS
  forRootAsync, synchronize false, TO-E3, generation, types per dialect
  and on MySQL, special columns, literal defaults, enums, the wrapper,
  the refusals, each TO-P1 form and the D markers), with
  `modeltest.CheckClaims` and `CheckDialectFacts`; `TestClaimIsolation`
  (a failing entity step); `TestSnakeCase`, `TestEnumMembers`.
- `TestTypeORMFixtureRelations` (`relations_test.go`): cases per row of
  step TOB4 (join columns, the reused column of A31, composite keys per
  major, the one-to-one unique, `createForeignKeyConstraints: false`,
  junction tables with default, named and self-relation columns and the
  inverse actions per major, indexes, unique and check constraints, the
  generated names per major and their absence with migrations, MySQL's
  unique index names and checks, the refusals and their markers);
  `TestGeneratedNames` (`names_test.go`: the name port against values
  TypeORM 0.3.31 and 1.1.3 returned for the same keys); the OP2 scope
  cases in `TestDiscoverNest` (a factory's or selector's parameter or
  local shadowing a module-level name is not read from the module; a
  const literal local is read as it).
- `TestTypeORMIndexedFixture` (`indexed_test.go`): the fixture indexed
  through `internal/index` against `testdata/fixtures/typeorm-models.golden.json`,
  then a warm replay that parses nothing.
- `TestTypeORMCorpusClaims` (`claims_corpus_test.go`, the same corpus
  directory): the claim pass over the four corpora with
  `modeltest.CheckClaims` and the counts of the coverage targets (tables,
  columns, complete column and constraint sets, types per dialect,
  foreign keys, unique constraints, indexes).
- Server: `TestUnreadToolsTypeORM` and `TestSchemaTypeORMMigrations`
  (`internal/server/schema_typeorm_test.go`, both backends).
- `TestTestScopeGroups` (`claims_test.go`): a data source and an
  ormconfig in test files are read, their tables `scope: test`.
- `TestSchemaAssumptionsPerTool` (`internal/server`, both backends):
  model rows are served per tool, each tool with its own D.
- **Oracle** (`TestTypeORMOracle`, `oracle_test.go`; skipped unless
  `GRAPHVEIN_TYPEORM_ORACLE` names the dump of
  `scripts/oracle/typeorm_catalog.sh` and `GRAPHVEIN_TYPEORM_ORACLE_ROOT`
  the fixture directory or the corpus checkouts' parent; never run by
  CI). The script installs each pin's kit in scratch (`npm install
  --ignore-scripts`; the SQLite bindings copied from a directory given
  with `--binaries`), loads the run's entity modules with every other
  module stubbed (TypeScript `transpileModule` with decorator metadata),
  and lets `DataSource.initialize()` with `synchronize` and `dropSchema`
  build a fresh scratch database; the test compares every claimed fact
  that applies to the run's dialect (table, column set, primary key,
  unique constraints, indexes, checks, and per column name, type with
  length, precision and scale, nullability, primary key, autoincrement,
  unique, index, default presence and text, enum labels, foreign keys
  with names), fails on any disagreement, and fails on any `oracle` label
  no run compared on its dialect. Default labels are per form
  (Revision 4 B1): `oracleDefaultForms` lists the default forms the
  fixture's runs compare per dialect; a default of another form is
  `mapped`, and the test fails when a listed form is not compared by a
  fixture run. A run on a dialect its group does not
  claim (nest-admin's MySQL group on SQLite and a Postgres copy,
  Maintainerr's Postgres copy) compares the provider-independent facts
  only. Each provable TO-P1 failure has a run that must fail with its
  error while the group claims nothing on that dialect; seerr's
  migrations are applied to an empty database and diffed with the
  synchronize catalog (TO-V2). Results: the T018g6 task record (internal),
  TOB5.

## Known gaps

- Generated names are claimed only without migrations; primary key
  names are never written; an index on a relation with several join
  columns, `@Unique` on a property and every 1.x index `type` are
  unknown.
- Decorators of a base class other than `@Index`, `@Unique` and `@Check`
  are not read.
- Column order is not claimed. MySQL, MariaDB and MSSQL facts are
  `mapped` (no oracle runs them; nest-admin's provider-independent facts
  are compared on SQLite and a Postgres copy).
- A 1.x `@Index` with a `type` TypeORM's Postgres driver does not list
  (anything but brin, btree, gin, gist, hash, spgist) also throws on
  postgres; the index stays a TO-X1 refusal there, not a TO-P1 failure.
- A `registerAs` module or an options module that imports neither
  `typeorm` nor `@nestjs/typeorm` has no payload (gate G10), so its
  options are not read; the four corpora have none.
- Entity classes in `.js` files are read like TypeScript (the payload
  parses both); decorators in JavaScript need a transpiler that TO-V3
  does not model.
- (Revision 4) A string default on a date or time, uuid, number, json or
  other column is unknown (only the texts the database rejects are TO-P1
  (10) to (12)); a postgres `'now()'` or `'now'` on a date or time column
  is frozen at `CREATE TABLE` and has no diagnostic of its own. A boolean
  string on a MySQL boolean column, and every literal default on MySQL's
  TEXT, are unknown.
- (Revision 4) `autoLoadEntities` reach follows static imports only: a
  module that only a dynamic `import()` loads is invisible to the
  link-time facts, so when another root of the package statically
  reaches its file it is counted for that root only (TO-V5); an import
  TypeScript elides (used only as a type) is still followed.
- (Revision 4) MySQL's foreign key index is claimed on MySQL-only groups;
  on MariaDB (its name is not proven) and in a dialect set with MySQL
  and another dialect the constraint set is partial.
