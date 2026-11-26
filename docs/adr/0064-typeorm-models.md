# ADR 0064 - TypeORM models and model payload version 5

**Status:** Accepted (delegated review, 2026-11-16)

TypeORM 0.3.x and 1.x entities as model-defined tables
(`docs/contract/claims/typeorm-models.md`, plan
the T018 task record (internal) "ORMs in the other first-tier
languages"). Written with the change that widens the cached TypeScript model
payload of ADR 0059 and ADR 0061 to version 5. Discovery, node shapes, row
and assumption ids and the oracle are fixed by the task file's claim
contract, the investigation results and Revisions 1-3 (with the TA2 and TA3
sections); steps TOB2-TOB5 implement them and add their implementation
notes here.

## Context

TypeORM states tables as decorated TypeScript classes: `@Entity("x")` on a
class, `@Column({...})`, `@PrimaryGeneratedColumn()`, `@ManyToOne(() => T,
(t) => t.inverse, {...})`, `@JoinColumn`, `@JoinTable`, `@Index`, `@Unique`
and `@Check` on its instance properties and on the class. A column without
an explicit type takes the design type the TypeScript compiler emits for
the property's annotation (`emitDecoratorMetadata`), so the declared type
is part of the input. The entities a data source creates are
those its `entities` option reaches (class references or globs), and the
options themselves are often chosen at run time: seerr's
`new DataSource(getDataSource())`, whose `if`/`else if`/`else` returns one
of five option objects through conditional expressions; NestJS apps pass
them through `TypeOrmModule.forRoot`/`forRootAsync` factories,
`@nestjs/config` `registerAs` namespaces, imported option consts and
spreads of local objects (rows TO-K3 to TO-K8). Column defaults and enum
columns name members of TypeScript enums declared in modules that import
nothing from an ORM.

The cached model payload (version 4) keeps module-level declarations,
statements, class headers and decorator counts, never decorator arguments
or instance members, no declared types, no enums, no array initializers,
no `if` with an `else` and no conditional expressions; its gate opens only
for Drizzle and Sequelize files. As for ADR 0059 and 0061, the link pass
has no tree on a cache hit (`internal/index/index.go`), and a second parse
path outside the bounded workers is ruled out.

## Options considered

1. **Read retained sources at link time.** Rejected for the reasons of ADR
   0059 and 0061: no tree on a cache hit, a second unbounded parse path.
2. **Reuse `DeclarationSyntax` / `TemplateSyntax`** (the Angular class
   decorator caches, ungated). Rejected as the payload: they keep class
   decorators only, no members, no declared types, lossy values (property
   maps without order) and are not gated. (TemplateSyntax does keep
   NestJS `@Module` decorator arguments in every file, which the census
   uses for `forFeature` calls outside the gate; see Known gaps.)
3. **A separate TypeORM payload.** Rejected as for Sequelize: the reduced
   tree, its validity and wrappers would be duplicated.
4. **Widen the model payload, tool-neutral and additive (chosen).**
   Version 5 adds three gate rows, class decorator expressions, instance
   members with a reduced declared-type tree, enum declarations, array,
   string and conditional initializers, `else` branches and conditional
   expressions. Version 1-4 fields keep their meaning.

## Decision

### Payload version 5

`ModelSyntaxVersion` is 5 and `Valid()` rejects every other version, so
version 4 entries miss once. The aggregate extractor version moves to
`extractors-37` (`extractors-36` is the GORM Go model payload, ADR 0063;
only living documents name it). The payload table in `docs/lang/typescript-models.md` names every
capture branch; `TestModelPayloadContract` keeps code and table 1:1 over
the `drizzle-syntax`, `sequelize-syntax` and new `typeorm-syntax`
fixtures.

Gate rows, after G1-G9 (first matching row):

- **G10** the file imports `typeorm` or a subpath (value or type import;
  `typeorm-naming-strategies` is another package), or (decision OP1) imports `@nestjs/typeorm` or a subpath, or
  is named `ormconfig.ts` or `ormconfig.js`: every file a discovery row
  reads (NestJS root and feature modules, option consts typed with
  `TypeOrmModuleOptions`, ormconfig scripts) has a payload.
- **G11** otherwise, a module-level class has a class decorator whose
  callee is an identifier named `Entity`, `ViewEntity` or `ChildEntity`
  (any import source: barrels that re-export TypeORM's decorators).
- **G12** otherwise, the file declares a module-level `enum` or `const
  enum` (plain or `export`); its payload holds only enum statements.

Capture additions:

- **Classes** (S4, B-class): `decorator_list`, each class decorator as an
  expression (a call with its arguments, or an identifier or member),
  decorators before `export` included; `members`, every instance member
  in order: properties with `name`, `optional`, `definite`, `abstract`,
  `declare`, `readonly`, `has_initializer` (the value is not kept), `type`
  and `decorators`; methods, getters and setters with `name` and
  `decorators` (no body); `unknown` for computed and private names
  (decorators kept), the constructor (parameter properties not read) and
  index signatures. Static members stay in version 4's `statics`.
- **Declared types** (rows T-*): `keyword`, `reference` (dotted name,
  type-argument count, arguments kept only for `Promise<T>` and
  `Array<T>`), `union` and `intersection` (flattened), `array`, `literal`
  (string, number with its sign, boolean), `tuple`, `function`;
  parentheses transparent; everything else `unknown`; depth limit 8.
- **Enums** (S8): name, `exported`, `const`, members in order with `name`
  and `value` (string or signed number literal; any other initializer
  `unknown`; absent for an auto-increment member). `declare enum` is not
  kept.
- **S1** also keeps array literal (the `entities` array), string literal
  (a `registerAs` token const, TO-K5) and conditional (an options object
  chosen at run time, aqualink-app's `dataSourceInfo`, TO-K8)
  initializers. The task's payload decision listed the array initializer;
  the string and conditional initializers were added in TOB1 because rows
  TO-K5 and TO-K8 (TA2, TA3) read them.
- **B-if** gains `else_body` (an `else if` is a nested `if`), and
  expressions gain `conditional` (`test`, `then`, `else`): seerr's options
  selector.
- **E-async, E-await, B-try** (T018g6 Revision 3 addendum 2,
  inside `extractors-37`): async arrow and function expressions carry an
  `async` flag, `await x` is a unary expression, and a `try` statement in
  a captured body keeps its try, catch and finally blocks, so
  nest-admin's `forRootAsync` (a `try` before the `useFactory`'s return,
  an async block `dataSourceFactory`) is read. They are kept only in the
  TypeORM gate rows G10 and G11; every other gated file's payload holds
  those forms as unknown, exactly as before, so the Drizzle and Sequelize
  readers never see them.

### Discovery, node shapes and rows

As fixed by the task file: package `internal/extract/models/typeorm`;
packages depending on `typeorm` 0.3.x or 1.x (per-major rules, TO-V9;
other majors a marker), pins from the lockfile; groups per module-level
`new DataSource(<options>)` (candidate option objects through a selector
function), ormconfig (0.3 only) and NestJS root modules
(`forRoot`/`forRootAsync`, TO-K3, with TO-K5 `registerAs`, TO-K7 imported
option consts and TO-K8 scope-neutral spreads); entities through the
`entities` option (classes and the glob subset) and `forFeature` with
`autoLoadEntities` (TO-K4); the dialect or dialect set (per-dialect claims
through the model kit, Revision 1 O1); the tsconfig decorator and
strictness flags (TO-V3), read from the shared resolver's tsconfig chain
(`typescript.ModuleResolver.CompilerFlags`, added: the
nearest config and its relative `extends`, a package `extends` unread);
`SnakeNamingStrategy` as the only custom naming
read (TO-K6). Records are model-defined `table` and `column` nodes
(`source: "model"`, `tool: "typeorm"`, `set: "models/typeorm/<data source
file>"`) with `contains` and `foreign_key` edges in the T018c2 record shape
and the T018g0 kit (`claims` per fact with `verification`: `oracle` for
Postgres and SQLite forms the oracle compared, `mapped` for MySQL,
MariaDB, MSSQL and the other SQLite-family drivers), so T018f's Schema
view shows them without view changes. The claim contract (rows TO-K1 to
TO-K8, E1-E3, C1-C6, X1-X4, R1-R4, P1, P2, U1, V and D; assumptions TO-V1
to TO-V10) is kept 1:1 with `contract.go` and
`docs/extract/schema-typeorm.md` by the contract test.

### Oracle

TypeORM's own `DataSource.initialize()` with `synchronize: true` and
`dropSchema: true` on a scratch Postgres 17 and a scratch SQLite file (the
entity modules transpiled with the corpus's `typescript` pin, side effects
stubbed), each corpus with its own TypeORM pin, plus a migrations drift
check that applies the corpus's migration classes to an empty database;
`TestTypeORMOracle` compares every claimed fact with the catalog (zero
disagreements). MySQL, MariaDB and MSSQL facts are `mapped` from the cited
driver source.

## Consequences

- Gated files (TypeScript packages of the corpora): seerr 122 of 618 (G10
  104, G12 18), nest-admin 69 of 237 (G10 58, G12 11), Maintainerr
  `apps/server` 141 of 499 (G10 135, G12 6), aqualink-app `packages/api`
  240 of 394 (G10 233, G12 7); G11 matches no corpus file. Payload bytes
  (JSON, uncompressed): seerr about 0.39 MB, nest-admin 0.27 MB,
  Maintainerr 1.18 MB, aqualink-app 0.88 MB (step TOB2's G10 widening
  added 12, 11 and 21 `@nestjs/typeorm` files and about 0.05 MB each). On the corpora without
  TypeORM only G12's enum payloads are new (langflow 3 files).
- Timing and store size: see the TOB1 section of
  the T018g6 task record (internal) (fresh and warm index within the 10 %
  gate on seerr, langflow, kaneo and umami).
- The Drizzle and Sequelize payloads change where version 5 captures a
  form version 4 left `unknown` or uncaptured (a conditional, a string
  const, class members and decorator expressions, an `if` with an `else`);
  the `drizzle-syntax` and `sequelize-syntax` payload goldens show these
  additions. No Drizzle or Sequelize claim changes: the
  `drizzle-models` and `sequelize-models` goldens are unchanged, and their
  linkers dispatch on statement kinds, so enum statements are skipped.
- `TestTypeORMCensus` checks the capture against the T018g6 inventory on
  the four corpora.

## Known gaps

- G10 does not reach a `registerAs` or options module that imports
  neither `typeorm` nor `@nestjs/typeorm`; such options are not read (no
  corpus has one).
- Async arrows and functions and `try` statements are `unknown` in the
  payload, so a `dataSourceFactory` such as nest-admin's `async (o) => {
  ... await new DataSource(o).initialize() ... }` and a `useFactory`
  holding a `try` are not read (TOB2 open point).
- Parameter decorators, constructor parameter properties and decorators of
  static members are not captured; TypeORM reads none of them.
- Enum member initializers other than string and signed number literals
  (`A = Other.X`, `B = 1 << 2`) are `unknown`; TO-C5 leaves such enums
  unknown.
- A dynamic `import()` is not in the link-time facts: a `forFeature`
  module loaded only that way, while another root statically reaches it,
  is counted for that root only (TO-V5); an import used only as a type
  (elided by TypeScript) is still followed (Revision 4 A4).
- A string default on a date, time, uuid or number column is unknown
  (only the rejected texts are TO-P1); a frozen `'now()'` on postgres
  gets no diagnostic of its own yet.

## Implementation notes

### Payload version 5

- `internal/lang/typescript/models_typeorm.go` (gate rows G10-G12, class
  members, declared types, enums), `models.go` (version, gate dispatch,
  S1 initializers, conditional expressions, G12's enum-only capture) and
  `models_statements.go` (class decorator lists and members, `else`
  branches, the enum statement, validity).
- Tests: `models_typeorm_test.go` (every decorator form of the contract,
  the NestJS forms, `registerAs`, option objects, spreads and the
  selector, members, declared types, enums, gates, validity),
  `TestModelPayloadContract` with the new rows, the `typeorm-syntax`
  fixture, and `TestTypeORMCensus`
  (`models_typeorm_census_test.go`).
- The corpora seerr, nest-admin, Maintainerr and aqualink-app are in
  `scripts/cloud-corpus.sh`.

### Relations, constraints and wiring

- `internal/extract/models/typeorm/relations.go` (TO-R1 to TO-R4: join
  columns typed as the referenced column, an explicit column of the same
  name reused with its own facts and the referenced type;
  composite join columns in array order on 0.3.x and primary-key order on
  1.x, A34; junction tables as their own table records, identity
  `table:<owner file>#<junction name>`, with the inverse foreign key's
  actions per major, A19 and A33), `constraints.go` (TO-X1 to TO-X3 from
  class, base class and property decorators; the one-to-one owners'
  unique constraints; foreign keys through the kit's L4; TO-X4 generated
  names only when the data source states no migrations option and the
  package has no `migration`/`migrations` folder, Revision 1 O5) and
  `names.go` (camelCase, the naming strategies' join names, the 0.3.x and
  1.x name hashes, A35).
- TO-P1 (3) is decided after the relation pass: a column a join column
  reuses is no failure, and an entity with an unread owning relation
  proves none.
- An entity two groups reach is the model `<Class>~<group root>` with
  every byte outside the identity alphabet written `_` (TOB3 wrote
  `<Class>@<root>`, which the contract's node schema rejects at index
  time, dropping the file).
- The indexer calls `typeorm.Link` once after the Sequelize front end;
  the catalogue registers the contract's rows for
  `/api/v0/schema/assumptions` (TOB4 left D out while the API keyed
  model rows by id across tools; TOB5 keys them per tool); the server lists TypeORM migrations folders in `unread_tools`
  (`typeorm-migrations`).

### Oracle and TOB4 open points

- `scripts/oracle/typeorm_catalog.sh` and `typeorm_catalog.cjs` (scratch
  kits per pin: typeorm 0.3.22, 0.3.31, 1.1.2, 1.1.3 with pg 8.23.0,
  sqlite3 5.1.7, better-sqlite3 11.10.0, typeorm-naming-strategies
  4.1.0 and typescript 5.8.3, 5.4.5 or 6.0.3; installed with
  `--ignore-scripts`, native bindings copied in) and `TestTypeORMOracle`:
  zero disagreements on the fixture and the four corpora; every `oracle`
  label compared on its dialect.
- The oracle found one wrong rendering: a precision without a scale on
  postgres is `numeric(p,0)` in the catalog, not `numeric(p)`; the claim
  now writes `(p,0)` with scale 0 on postgres, MySQL and MariaDB.
- TO-P1 (9): a 1.x `@Index` `type` option fails on every dialect but
  postgres (`IndexMetadata.js:49-52`).
- `/api/v0/schema/assumptions` keys model rows per tool
  (`rows["models/<tool>"]`), so every tool's D is served.
- A data source or ormconfig in a test file is read; its tables carry
  `scope: test`.

### Independent review fixes (Revision 4)

Decisions on the independent review
(the T018g6 task record (internal), "Revision 4"); the status stays
Proposed.

- Defaults are claimed per type family: a string only on character,
  text (not MySQL's TEXT) and enum columns, numbers and boolean strings
  on boolean columns as the database stores them (postgres
  `true`/`false`, sqlite as written); other combinations are unknown.
  The string texts postgres or MySQL reject at `CREATE TABLE` are TO-P1
  (10) to (12).
- TO-P1 (13) and (14): a length on a type the driver gives none (the
  metadata validator), a precision on a postgres type without a type
  modifier.
- `oracle` labels on defaults are per form (`oracleDefaultForms`): a
  form the oracle fixture does not compare on a dialect is `mapped`
  there, and `TestTypeORMOracle` fails when a listed form is not
  compared.
- MySQL: the foreign key index InnoDB adds is claimed on MySQL-only
  groups (named as the foreign key, `mapped`); MariaDB or a mixed
  dialect set leave the constraint set partial.
- NestJS `autoLoadEntities` reach follows the static imports of the
  root's application (the root module file and every package entry file
  whose imports reach it; contract change to TO-K4): two roots in one
  package each get their own `forFeature` calls; an unreached call leaves the list
  incomplete when the import graph is open or no root reaches it.
- Robustness: each package's discovery is an isolated step, the
  indexer's call is wrapped in a recover-to-diagnostic wrapper, and the
  oracle script drops its scratch databases from an `EXIT` trap.
- ADR 0055 gains an amendment for the per-tool model rows; old
  `row:models:<id>` deep links resolve to SQLModel's row in the web.

## Review notes (delegated review, 2026-11-16)

- **Steps:** the investigation inventory, oracle harness and payload decision (TA),
  addenda TA2 (NestJS corpus nest-admin) and TA3 (TypeORM 1.x, ported
  `SnakeNamingStrategy`, Maintainerr and aqualink-app), TOB1 TypeScript
  model payload version 5 with the class/property decorator capture, TOB2
  discovery and contract table, TOB3 entities, columns, types and
  defaults, TOB4 indexes, constraints, relations and wiring, TOB5 oracle
  and per-tool assumption rows, TF review fixes (Revision 4), TG gate.
- **Developer decisions (2026-11-15):** Revisions 1-3, TypeORM 1.x in
  scope (TA3).
- **Independent review (TR):** class A findings all fixed: string
  defaults only on character, text and enum columns, rejected forms are
  TO-P1 (A1); number and boolean-string defaults on boolean columns as
  stored (A2); MySQL's foreign-key index (A3); `autoLoadEntities` reach
  follows static imports from the application's root and entry files,
  else the table list may be incomplete (A4); options the database or the
  metadata validator rejects are TO-P1 (A5); oracle labels per compared
  form (B1); ADR 0055 amendment and legacy deep links (B2); guards and
  the oracle script's trap (C).
- **Results:** oracle 0 disagreements on claimed facts (fixture 1,549
  oracle labels; seerr 2,806, Maintainerr 3,322, aqualink-app 2,425 on
  postgres and sqlite; nest-admin MySQL, `mapped`); the oracle test fails
  on any uncompared `oracle` label. Corpus snapshot: 18 shared corpora
  identical to main; seerr 14, nest-admin 17, Maintainerr 36, aqualink-app
  29 tables. Timing within 10 % (seerr +5.3 % fresh / +4.5 % warm,
  self-index +1.1 % / +0.9 %, kaneo, umami and langflow within 2 %);
  store seerr +2.5 %, self-index +0.6 %. CI run 43786178126
  (`ci/s30-t018g6`): Windows and postgres green, postgres raw log
  0 FAIL, only env-gated skips.
- **Assumptions shown:** TO-V1..TO-V10.
- **Known gaps / after v1:** MySQL and MariaDB not compared on a server;
  dynamic `import()` invisible to the reach (TO-V5); provable failures
  still unknown (a postgres boolean number other than 1/0, literal
  defaults on MySQL TEXT); no info diagnostic for a frozen `'now()'`
  string default; two guard styles in `internal/index`.
