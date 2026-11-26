# sequelize-models fixture

Own code (no third-party text) for T018g3. Part 1 covers the
models, attributes and options rows of the claim contract
(`docs/extract/schema-sequelize.md`: SQ-K2, SQ-M1, SQ-M2, SQ-A1, SQ-A2,
SQ-O1, SQ-O2, D, and the SQ-P1 failures those rows read); part 2
adds references, associations, join tables, the remaining failures,
the cyclic rule and the remaining scope forms of SQ-K1 and SQ-U1.
`TestSequelizeFixture`, `TestSequelizeClaims`, `TestSequelizeContractTags`
and `TestDDLRendering` (internal/extract/models/sequelize) read it; the
tests skip this README and any `*.golden.json` when reading the sources.
`TestSequelizeIndexedFixture` indexes the whole directory and checks
`../sequelize-models.golden.json`. No
file needs an inert name (no ignore file, no `node_modules`). Each package
has its own `package-lock.json` (the inflection pin is read only from
one).

| path | what it covers |
|---|---|
| `pg/` | postgres, TypeScript ESM, class + `init` models (SQ-M2) importing the instance from `src/db.ts`. `models/Person.ts`: every type of the SQ-A1 list on postgres (arrays, a range, ENUM in both forms, JSON/JSONB), the column keys of SQ-A2 (allowNull, unique true / group / `{name}`, every default kind: string, number, boolean, null, JSON, `literal`, `fn`, NOW, UUIDV4, a function), `field`, `comment`, `validate`, `get`, a VIRTUAL attribute, hooks/scopes options. `Base.ts` + `Category.ts`: a project base class, a `schema` option and an ENUM in it (unknown). `Mouse.ts`: `paranoid`, `version`, renamed and disabled timestamps, `indexes` (generated and literal names, unique, `using`, a partial index, field objects) and a JSONB `index: true`. `Archived.ts`: `modelName` (Archived -> Archiveds), a UUID primary key, a user `createdAt`. `Settlement.ts`: `removeAttribute("id")`. `Unread.ts`: unknown columns (a `new` VIRTUAL, a helper whose body is not one return, a project type, a raw string type, an unread argument and key, an enum-member default). `Wallet.ts` + `DataTypes.ts` + `helpers.ts` (task Revision 2): attribute helpers (row SQ-A1, form "helper member"): opencollective-api's `CustomDataTypes(DataTypes).currency` shape (a default-exported function returning one object literal, no Sequelize import, payload gate G9; its `validate` and `set` have no table fact), a named helper's member, a whole helper result with a literal argument, a default naming a constant of the helper's module (default unknown), and refused helpers (the parameter used as a value, a non-literal argument, a body with two statements). `Spread.ts`: a spread in the attributes. `helpers.ts`: also a define inside a function (marker). `index.ts`: `Mouse.belongsTo(Person)` (a new nullable `PersonId`, SET NULL / CASCADE) |
| `pgdefine/` | postgres, CommonJS, module-level `sequelize.define` (SQ-M1) under a global `define` with `underscored` and `paranoid`: generated index name, `freezeTableName` with underscored, a `schema` option, a BIGSERIAL key, the remaining default forms (JSON and JSONB literals, TEXT, SMALLINT) |
| `sqlite/` | sqlite, CommonJS factories called with the instance from `models/index.js` (Gladys's form, its global `define`) and the recognised `associate(models)` loop: UUID, INTEGER autoIncrement, BIGINT and composite primary keys, boolean, DOUBLE, ENUM and JSON defaults, a `literal` default, a `references` attribute merged with a hasMany and a belongsTo (CASCADE / CASCADE, first association first), and a join table (`area.belongsToMany(Counter, {through: 't_area_counters'})`: UUID and BIGINT keys, underscored) |
| `pgassoc/` | postgres, CommonJS, one module: references to a define binding, a string with a key and lower-case actions, a string without a key; belongsTo into an existing attribute without allowNull (NO ACTION) and into one with its own references (kept), a new belongsTo key, `as` + `targetKey` (name from the primary key, type from the target key), hasOne with `as`, hasMany with a not-null foreignKey object (CASCADE), `constraints: false`, a composite-key target (attribute, no foreign key); two join tables (source underscored with a renamed createdAt; a literal foreignKey) |
| `pgcycle/` | postgres, a cyclic graph: mutual belongsTo, a foreign key on a primary-key column (unknown), a model whose default + unique stops the alter pass (its foreign keys unknown), a model whose foreign key is claimed |
| `pgfail/` | provable failures (SQ-P1): references naming a missing column, a cross-schema belongsTo, a belongsTo targetKey that is not unique (`Racks` is not created) and a table whose references name it (`Bins`, not created either), a belongsTo targetKey the target lacks (Sequelize throws there while the models load), a definition after that throw (`After`) and one in `late.js`, which `src/index.js` requires after `models.js` (not proven before the throw); neither is claimed |
| `pgrefuse/` | refused forms: a references model that is not a literal, association options that are not an object literal (every holder column unknown), `constraints: false` with an onDelete, a self belongsToMany over a string (join table not claimed, marker), a through model (its keys and implicit id unknown); `plugin/` is another package (its own `package.json`) with a model on the pgrefuse instance (a marker on the group) |
| `pgconfig/` | postgres from the single-driver rule, TypeScript ESM, options spread from the node-config object (open point O1, `config/default.json`), an instance `schema` (every table in `app`), references to an init class and to a table name (both resolved in `app`), a hasMany in the schema, and a sequelize-cli `migrations/` folder (SQ-U1 marker) |
| `mapped/` | mysql and mssql instances (two groups, facts mapped, SQ-V6): types, unique, defaults (TEXT has none on mysql), mssql's nullability with a default, an ENUM that is not mapped on mssql, and mssql's foreign-key actions (a reproduction; `TestDDLRendering` compares the same forms with Sequelize's own generator) |
| `pghooks/` | instance hooks (row D): a `hooks` key in the constructor options, `sequelize.addHook('beforeDefine', ...)` and `sequelize.beforeDefine(...)`, one instance each; no group is claimed |
| `cli/` | the sequelize-cli forms, postgres: `require(__dirname + '/../config/config.json')[env]` options (the entries agree), the models object filled by `db.X = require('./x')(sequelize, Sequelize.DataTypes)`, the template's `Object.keys(db).forEach` loop with its `if` guard; `models/legacy.js` is the model template (class + init inside the factory: a marker), which `models/index.js` does not load |
| `cliclass/` | the other sequelize-cli forms, postgres: module-level classes with `static associate(models)`, a models object with required classes and an inline `require('./track')(sequelize, DataTypes)` factory call, and the `for (const model of Object.values(models))` loop with an `&&` guard |
| `decorators/` | a package depending on sequelize-typescript (marker) and a decorated class (row D) |
| `legacy/` | sequelize 5.22.5 in the lockfile (refused: another major) |

Oracle set (`scripts/oracle/sequelize_catalog.sh --fixture`,
then `TestSequelizeOracle`; a later part added `cli/` and `cliclass/` and
loads `pgfail/` through `src/index.js`): every package except `decorators/`
(decorators need sequelize-typescript, which the harness does not
install; it claims nothing), `legacy/` (sequelize 5; nothing claimed) and
`mapped/` (mysql and mssql facts are mapped, SQ-V6: no driver is
installed and no database runs them) and `pghooks/` (no group is
claimed). `pg/src/models/Unread.ts` is
loadable and syncs (its project type `Cents` sets the instance key
`INTEGER`, which Sequelize renders, as opencollective-api's
ExpenseTypesEnum does; until SQB5 it set a static key and rendered
`ABSTRACT`, which Postgres rejects; its raw string type is `VARCHAR(12)`;
both columns stay unknown). `pgconfig/` reads `config` as its
`config/default.json` (node-config is not installed) and gets its dialect
from the run (the single-driver rule, SQ-V4). `pgfail/src/models.js`
throws by design (the entry is `src/index.js`): the harness catches the
load error and syncs the models defined before the throw. `pgcycle/` must be synced per model (create without foreign keys,
then alter, recording each failure), as the investigation replayed the cyclic path.
