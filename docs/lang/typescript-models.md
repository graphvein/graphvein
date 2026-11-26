# TypeScript model syntax (Drizzle, Sequelize, TypeORM)

ADR 0059 (version 1, task T018g2: step G2B1 captures the syntax; units
G2B2-G2B4 link it) and ADR 0061 (version 2, task T018g3: step SQB1 widens
the payload for Sequelize; steps SQB2-SQB4 link it; version 3:
attribute helper modules, gate row G9, and module-level function
declarations, row S6; version 4, T018g3 Revision 3:
module-level for-of loops, row S7, if statements in bodies, row B-if, and
classes' static members) and ADR 0064 (version 5, task T018g6: step TOB1
widens the payload for TypeORM: gate rows G10-G12, class decorator lists,
instance members with declared types, enum declarations, array and string
initializers, `else` branches and conditional expressions; steps TOB2-TOB5
link it). Aggregate extractor
version: extractors-49 (32 from T018g2: cached TypeScript facts gained the
`Models` payload; 33 and 34 from T018h, ADR 0060: Python and Rust payloads,
and the two TypeScript call cache fidelity fields below; 35 from T018g3 SQB1:
model payload version 2; SQB4b's version 3 and Revision 3's version 4
keep extractors-35, see "Known gaps"; 37 from T018g6 TOB1: model payload
version 5; 36 is T018g5's Go model payload). Contract version unchanged; the payload adds no
graph record.

The TypeScript extractor captures the syntax that ORM model linking reads
from Drizzle schema modules and drizzle-kit config files, and from Sequelize
model, instance and options modules (TypeScript and JavaScript, ESM and
CommonJS), and from TypeORM entity, data source, NestJS module and enum
modules: the module-level declarations, statements and the default export,
as a reduced expression tree that keeps source order; for classes also the
decorators and the instance members with their declared types. The payload is syntax,
never a claim (rule 31): which callee is `pgTable`, which receiver is the
Sequelize instance, which import or `require` reaches a schema file and which
config drizzle-kit loads are decided at link time through the TypeScript
resolver ([typescript-resolution.md](typescript-resolution.md)) under the
claim contracts in `docs/contract/claims/drizzle-models.md`,
`docs/contract/claims/sequelize-models.md` and
`docs/contract/claims/typeorm-models.md`.

## Why a cached payload

On a cache hit the indexer replays stored facts and never parses the file,
so the link pass has no tree. The existing TypeScript caches do not keep what
Drizzle needs: `RouteExpr` bindings keep object properties in a map (column
order lost), drop array elements and spreads, and keep arrow functions as
text. The model payload keeps order, arrays, spreads, arrow bodies and tagged
templates, only for the few files that pass the gate (task file, "Payload
decision"; ADR 0059). Sequelize needs more than version 1 kept: module-level
expression statements (`X.init(...)`, `A.belongsTo(B)`, `module.exports = ...`),
class headers (`class X extends Model`), `new Sequelize(...)`, destructured
requires and the bodies of CommonJS factory functions (T018g3 task file,
"Payload decision"; ADR 0061). TypeORM needs what version 4 lacks: the
arguments of class and member decorators, the instance members (columns and
relations are decorated instance properties) with their declared types
(TypeORM infers a column type from the emitted design type), enum
declarations (defaults and enum columns name enum members), the `entities`
array, the data source's options selector (`if`/`else` chains and
conditional expressions) and string consts (a `registerAs` token)
(T018g6 task file, "Payload decision"; ADR 0064). `DeclarationSyntax` (the
Angular decorator cache) is not reused: it keeps class decorators only, no
members or types, and is ungated.

## Payload

`typescript.CachedFacts.Models` (JSON `models`, omitted when the file does not
pass the gate), type `typescript.ModelSyntax`, version 5
(`typescript.ModelSyntaxVersion`; version 1 to 4 entries miss once).

- `version`; `declarations`: every captured module-level declarator in
  source order (`name`, `keyword` `const`/`let`/`var`, `exported` for the
  `export const` form, `span`, `value`); `default`: the `export default`
  statement's `span` and `value`, when present; `statements` (version 2,
  omitted when empty): module-level expression statements, class
  declarations, destructuring declarations, (version 3) function
  declarations, (version 4) for-of loops and (version 5) enum
  declarations in source order. The two
  lists are separate so version 1's `declarations` keep their meaning; spans
  order them against each other.
- Statements (`kind`, `span`, plus the fields of the kind): `expression`
  (`value`), `class` (`name`, `exported`, `default`, `abstract`, `extends`,
  `decorators`: the count of class and member decorators, never their
  arguments; version 4 `statics`: the static members in order, a plain
  static method as a `function` statement with `name` and `value`, any
  other static member (field, accessor, async or generator method,
  computed name, static block) as `unknown` with its plain `name`, if
  any; version 5 `decorator_list`: the class decorators' expressions in
  source order, decorators before `export` included, and `members`: the
  instance members, below), `enum` (version 5, module level only: `name`,
  `exported`, `const`, `enum_members`: `name`, `span` and `value` per
  member in order; `value` is absent for a member without an initializer,
  a `string` or `number` expression for a string literal or a (signed)
  number literal, else `unknown`), `function` (version 3: `name`, `exported`, `default`,
  `value`: the function as a `function` expression), `destructure` (`keyword`, `exported`, `bindings`: `key` and
  `name` per object-pattern entry, `name` `""` for a defaulted or nested
  entry, `key` `""` for a rest entry; `value`), `for-of` (version 4,
  module level only: `keyword`, `name` the loop binding, `value` the
  iterable, `body` the loop's statements read like a function body), and
  in function bodies only `declaration` (`keyword`, `name`, `value` when
  initialised), `return` (`value` unless bare), `if` (version 4: `value`
  the condition, `body` the consequence's statements; version 5
  `else_body`: the alternative's statements, an `else if` a nested `if`)
  and `unknown`.
- Expressions (`kind`, `span`, plus the fields of the kind): `call`
  (`callee`, `args`), `member` (`object`, `name`), `identifier` (`name`),
  `string` (decoded `value`), `number` (source `value`, with a leading `-` or
  `+`), `boolean` (`value`), `null`, `template` (cooked `strings`,
  `substitutions`; `strings` has one more element), `tagged` (`tag`,
  `strings`, `substitutions`), `object` (`entries`), `array` (`elements`),
  `spread` (`object`), `arrow` (`params`, `body`, `block`), `unknown`.
- Object entries (`kind`, `key`, `span`, `value`): `property`, `shorthand`,
  `spread`, `method`, `unknown`.
- Version 2 expressions: `new` (`callee`, `args`), `function` (`name` when
  named, `params`, `statements`), `assign` (`left`, `right`; `=` only),
  `index` (`object`, `index`), `binary` (`value` the operator, `left`,
  `right`), `unary` (`value` the operator, `object`); an arrow with a block
  body also lists its `statements` (its `body` keeps the version 1 meaning).
- Version 5 expressions: `conditional` (`test`, `then`, `else`).
- Class members (version 5; `kind`, `span`, plus the fields of the kind):
  `property` (`name`, `optional` for `?`, `definite` for `!`, `abstract`,
  `declare`, `readonly`, `has_initializer`, `type` the declared type when
  annotated, `decorators`), `method`, `get`, `set` (`name`, `decorators`;
  no body), `unknown` (a computed or private name: `decorators`; a
  constructor, whose parameter properties are not read, or an index
  signature: nothing). Static members stay in `statics`. Names are decoded
  identifier, string or number keys. The initializer and method bodies are
  not kept.
- Declared types (version 5; `kind` plus the fields of the kind, no span):
  `keyword` (`name`: `string`, `number`, `boolean`, `bigint`, `any`,
  `unknown`, `object`, `never`, `void`, `undefined`, `null`, `symbol`),
  `reference` (`name` dotted, `arguments` the type-argument count, `types`
  the arguments only for `Promise<T>` and `Array<T>`), `union` and
  `intersection` (`types`, nested unions flattened in source order), `array`
  (`types`: the element), `literal` (`literal` `string`, `number` or
  `boolean`, `value` decoded, a negative number with its `-`), `tuple`
  (`types`), `function`, `unknown` (every other form: object type literals,
  `typeof`, `keyof`, conditional, indexed, mapped and template types,
  `readonly T[]`, constructor types; and depth over 8). Parentheses are
  transparent.
- TypeScript wrappers (`as`, `satisfies`, `!`, `<T>x`) and parentheses are
  transparent: the payload holds the wrapped expression. Type arguments
  (`pgTable<...>(...)`, `$type<T>()`) are not kept.
- Comments are never captured. A tree deeper than 48 levels is cut to
  `unknown`.

Validity: `ModelSyntax.Valid()` checks the version, a non-nil declaration
list, every declaration's name and keyword, and every expression's kind and
required fields; the indexer checks it on every cache read, so a payload of
another version or a damaged one is a cache miss. A capture failure (a
recovered panic) leaves the file's other facts and every other file intact:
diagnostic `model_syntax_capture_failed` (warning), and the file is not
cached, so the next run tries again.

## Payload table

Every capture branch in `internal/lang/typescript/models.go` reports one row
id below; `TestModelPayloadContract` checks that the Go table and this table
match 1:1, that a snippet per row fills exactly the row's fields, and that
the fixtures `testdata/fixtures/drizzle-syntax/` (version 1 rows),
`testdata/fixtures/sequelize-syntax/` (version 2 to 4 rows) and
`testdata/fixtures/typeorm-syntax/` (version 5 rows) together emit exactly
the rows of the table.

| id | form | payload |
|---|---|---|
| G1 | the file imports a `drizzle-orm/<dialect>-core` module or a subpath of one (`<dialect>` lowercase letters and digits), with a value or a type import | `Models` present: version, declarations |
| G2 | otherwise, the file imports `drizzle-kit` | `Models` present: version, declarations |
| G3 | otherwise, the file is named `drizzle.config.ts` or `drizzle.config.js` (drizzle-kit's default names; a config need not import `drizzle-kit`) | `Models` present: version, declarations |
| G4 | otherwise, the file imports `sequelize` or a subpath (`sequelize/...`), with a value or a type import; `sequelize-typescript` is not `sequelize` | `Models` present: version, declarations |
| G5 | otherwise, the file calls `require("sequelize")` (or a subpath) anywhere | `Models` present: version, declarations |
| G6 | otherwise, the file has a module-level statement `<e>.init(<object literal>, <object literal>)`, or a call `<e>.define(<string literal>, <object literal>, ...)` anywhere (CommonJS factories import nothing from Sequelize) | `Models` present: version, declarations |
| G7 | otherwise, the file has an object property `dialect: "<d>"` with `<d>` a Sequelize dialect (`postgres`, `sqlite`, `mysql`, `mariadb`, `mssql`, `db2`, `snowflake`, `oracle`): an options module such as Gladys's `config/config.js` (open point O2) | `Models` present: version, declarations |
| G8 | otherwise, the file's module-level `module.exports = F` or `export default F` is a non-async arrow or function expression that returns `<id>[<expression>]`, `<id>` bound by a module-level `require("./...")`/`require("../...")` or an import from a relative module: an environment selector such as Gladys's `utils/getConfig.js` (open point O2) | `Models` present: version, declarations |
| G9 | otherwise, the file has a module-level function declaration (plain, `export` or `export default`), `export default F`, `module.exports = F` or `exports.<name> = F` (F a non-async, non-generator function or arrow) whose body is a single `return <object literal>` (or an arrow's object literal body), where that object or one of its property values is an object literal with a `type` property `<p>.<X>` or `<p>.<X>(...)`, `<p>` a parameter of the function: an attribute helper such as opencollective-api's `server/models/DataTypes.ts` (T018g3 Revision 2, open point O4; version 3) | `Models` present: version, declarations |
| G10 | otherwise, the file imports `typeorm` or a subpath (`typeorm/...`), or (T018g6) `@nestjs/typeorm` or a subpath, with a value or a type import (a re-export `export { X } from "typeorm"` is not an import; `typeorm-naming-strategies` is another package), or its base name is `ormconfig.ts` or `ormconfig.js` (version 5) | `Models` present: version, declarations |
| G11 | otherwise, the file has a module-level class (plain, `abstract`, `export`, `export default`) with a class decorator `@E` or `@E(...)` whose callee is an identifier named `Entity`, `ViewEntity` or `ChildEntity`, from any import source (a barrel that re-exports TypeORM's decorators; version 5) | `Models` present: version, declarations |
| G12 | otherwise, the file has a module-level `enum` or `const enum` declaration, plain or `export` (`declare enum` does not count): its payload holds only S8 statements (version 5) | `Models` present: version, declarations |
| GD | every other file, including files that import only the root `drizzle-orm` module, only `sequelize-typescript` or only `@nestjs/typeorm` | nothing |
| S1 | a module-level `const`/`let`/`var` declarator (plain or `export`) with an identifier name whose initializer, after wrappers, is a call, a tagged template, an object literal, a `new` expression, (version 4) an index into a call's result, `<call>[<expression>]` (the sequelize-cli `require(<config>)[env]`), or (version 5) an array literal (a TypeORM `entities` array), a string literal (a `registerAs` token const) or a conditional expression (an options object chosen at run time, aqualink-app's `dataSourceInfo`; the string and conditional initializers accepted by T018g6 Revision 3 addendum OP2) | declaration: name, keyword, exported, span, value |
| S2 | `export default <expression>` (a default-exported function or class is `unknown`; a class is also S4) | default: span, value |
| S3 | a module-level expression statement whose expression, after wrappers, is a call or an `=` assignment (`X.init(...)`, `A.belongsTo(B)`, `module.exports = e`, `x.associate = e`) | statement: kind, span, value |
| S4 | a module-level class declaration, plain, `abstract`, `export` or `export default` (decorators before `export` counted; static members from version 4; decorator expressions and instance members from version 5) | statement: kind, span, name, exported, default, abstract, extends, decorators, decorator_list (each class decorator as an expression: a call with its arguments, or an identifier or member), members (M rows), statics |
| S5 | a module-level declarator (plain or `export`) whose name is an object pattern, with its initializer | statement: kind, span, keyword, exported, bindings, value |
| S6 | a module-level function declaration, plain, `export` or `export default` (not async, not a generator; version 3) | statement: kind, span, name, exported, default, value (the function as E-function) |
| S7 | a module-level `for (const\|let\|var <identifier> of <expression>) <statement>` (not `for await`; version 4): the sequelize-cli `associate(models)` loop over `Object.keys` or `Object.values` | statement: kind, span, keyword, name, value (the iterable), body (a block's statements or the single statement, as B rows) |
| S8 | a module-level `enum` or `const enum` declaration, plain or `export` (version 5) | statement: kind, span, name, exported, const, enum_members (name, span, value: a string or number literal, `-`/`+` number included, as E-string or E-number; any other initializer E-unknown; absent without an initializer) |
| SD | every other module-level statement or declarator (other initializers, array patterns, async and generator functions, ambient `declare` (`declare enum` included), `export { }`, `export =` and `export * from`, other expression statements, nested scopes outside captured function bodies; in a G12 file, every statement but S8) | nothing |
| B-declaration | in a captured function body: a declarator with an identifier name, any initializer or none | statement: kind, span, keyword, name, value |
| B-destructure | in a captured function body: a declarator with an object pattern | statement: kind, span, keyword, bindings, value |
| B-expression | in a captured function body: any expression statement | statement: kind, span, value |
| B-return | in a captured function body: `return` with or without a value | statement: kind, span, value |
| B-class | in a captured function body: a class declaration (static members from version 4, decorator expressions and instance members from version 5, as S4) | statement: kind, span, name, abstract, extends, decorators, decorator_list, members, statics |
| B-if | in a captured function body or a for-of body: `if (<condition>) <statement>` (version 4: the sequelize-cli loop's `if (db[name].associate)`), with an `else <statement>` from version 5 (seerr's `getDataSource()` options selector) | statement: kind, span, value (the condition), body (the consequence's statements, as B rows), else_body (the alternative's statements, as B rows; an `else if` is one nested B-if) |
| B-try | in a captured function body of a G10 or G11 file (T018g6 Revision 3 addendum 2: nest-admin's `useFactory` parses a logger option in a `try` block before its `return`): `try { ... } catch { ... } finally { ... }`, the catch parameter not kept; in every other gated file a `try` is B-unknown, as before | statement: kind, span, body (the try block's statements, as B rows), catch_body, finally_body (as B rows) |
| B-unknown | in a captured function body: every other statement (`switch`, `try` outside G10 and G11, loops, blocks, function declarations, array-pattern declarators), or any statement deeper than 48 levels | statement: kind, span |
| M-property | in a captured class body: an instance field declaration (not `static`) with an identifier, string or number name (version 5) | member: kind `property`, span, name, optional, definite, abstract, declare, readonly, has_initializer, type (T rows, absent without an annotation), decorators (the member's decorator expressions in order) |
| M-method | in a captured class body: an instance method, getter or setter (not `static`, not the constructor) with an identifier, string or number name, including method signatures (version 5) | member: kind (`method`, `get` or `set`), span, name, decorators (the decorators written before it); no body |
| M-unknown | in a captured class body: an instance member with a computed or private (`#x`) name (decorators kept), the constructor (its parameter properties and parameter decorators are not read) or an index signature (version 5) | member: kind `unknown`, span, decorators (computed and private names only) |
| T-keyword | a predefined type `string`, `number`, `boolean`, `bigint`, `any`, `unknown`, `object`, `never`, `void`, `symbol`, or the literal types `null` and `undefined` (version 5) | type: kind, name |
| T-reference | a type name, dotted name or generic type (`User`, `geo.Point`, `Relation<User>`, `Promise<User>`) (version 5) | type: kind, name, arguments (the type-argument count), types (the arguments, only for `Promise` and `Array`) |
| T-union | `A \| B \| ...` (version 5) | type: kind, types (flattened, source order) |
| T-intersection | `A & B & ...` (version 5) | type: kind, types (flattened, source order) |
| T-array | `T[]` (version 5) | type: kind, types (the element) |
| T-literal | a string, number (with a leading `-`) or boolean literal type (version 5) | type: kind, literal, value |
| T-tuple | `[A, B]` (version 5) | type: kind, types (each element; optional, rest and labelled elements T-unknown) |
| T-function | a function type `(a: A) => B` (version 5) | type: kind |
| T-wrap | a parenthesized type `(T)` (version 5) | the wrapped type's fields |
| T-unknown | every other type (object type literals, `typeof`, `keyof`, `readonly T[]`, conditional, indexed, mapped, template and constructor types, `this`, other predefined types), or a type deeper than 8 levels (version 5) | type: kind |
| E-call | a call `f(a, b)` (not optional `f?.()`), type arguments dropped | kind, span, callee, args (spreads as `spread`) |
| E-member | `o.p` with an identifier property (not optional `o?.p`) | kind, span, object, name |
| E-identifier | an identifier, or `undefined` | kind, span, name |
| E-string | a string literal whose escapes decode | kind, span, value |
| E-number | a number literal, or `-`/`+` applied to one | kind, span, value |
| E-boolean | `true`, `false` | kind, span, value |
| E-null | `null` | kind, span |
| E-template | a template literal whose escapes decode | kind, span, strings, substitutions |
| E-tagged | a tagged template ``tag`...` `` such as ``sql`...` `` | kind, span, tag, strings, substitutions |
| E-object | an object literal | kind, span, entries |
| E-property | an entry `key: value` with an identifier, string or number key | kind, key, span, value |
| E-shorthand | an entry `name` | kind, key, span, value |
| E-spread-entry | an entry `...value` | kind, span, value |
| E-method | a plain method `key() { ... }`: value is its single `return <literal>` (string, number, boolean, null, template without substitutions), else `unknown` | kind, key, span, value |
| E-entry-unknown | any other entry: computed key, getter, setter, async or generator method | kind, span, value |
| E-array | an array literal | kind, span, elements (spreads as `spread`) |
| E-spread | `...value` in an array or call arguments | kind, span, object |
| E-arrow | a non-async arrow function: parameter names (`""` for a destructured, defaulted or rest parameter), an expression body, or a block body read as its single `return <expression>` (else `unknown`) plus, from version 2, its statement list | kind, span, params, body, block, statements |
| E-wrap | `as`, `satisfies`, `!`, `<T>x` and parentheses | the wrapped expression's fields |
| E-new | `new C(args)` or `new C`, type arguments dropped | kind, span, callee, args |
| E-function | a non-async, non-generator function expression: its name when named, parameter names as E-arrow, the body's statement list | kind, span, name, params, statements |
| E-assign | `left = right` (compound assignments are `unknown`) | kind, span, left, right |
| E-index | `object[index]` (not optional `object?.[index]`) | kind, span, object, index |
| E-binary | `left <operator> right`, every binary and logical operator (`===`, `\|\|`, `+`, ...) | kind, span, value (the operator), left, right |
| E-unary | `typeof`, `!`, `void`, `delete`, `~`, and `-`/`+` on anything but a number literal | kind, span, value (the operator), object |
| E-conditional | `test ? then : else` (version 5) | kind, span, test, then, else |
| E-async | in a G10 or G11 file (T018g6 Revision 3 addendum 2: NestJS's `dataSourceFactory: async (options) => ...`): an async arrow or async function expression, captured as E-arrow or E-function with `async` true; in every other gated file it is E-unknown, as before (only the TypeORM reader accepts it) | kind, span, async, params, body, block, statements (as E-arrow), or name, params, statements (as E-function) |
| E-await | in a G10 or G11 file (T018g6 Revision 3 addendum 2): `await <expression>`, as a unary expression with the operator `await`; E-unknown in every other gated file | kind, span, value (`await`), object |
| E-unknown | every other expression (class expressions, `this`, regular expressions, `await` and async arrows and functions outside G10 and G11, compound assignments, optional chains, generators, undecodable strings, depth over 48) | kind, span |

## Call cache fidelity fields (T018h)

Code-to-table linking (ADR 0060) reads TypeScript ORM query calls
from the ungated call cache (`facts.ClientCall`, every call with its reduced
callee chain, arguments, owner and scope), not from the model payload above.
Two additive fields keep what that reduction used to lose:

- `RouteExpr.Spread` (JSON `Spread`, omitted when false) is true on an object
  literal with at least one spread element, at any depth: in
  `prisma.user.findMany({ ...criteria, include: { posts: true } })` the
  argument object has `Spread`, its `include` object does not. `Properties`
  still holds only the named entries, so an object with `Spread` may be
  incomplete (T018h row CT-R2 records it as a `partial` unknown).
- A tagged template's arguments are its `string_fragment` and
  `template_substitution` children. Each `template_substitution` now holds
  its reduced inner expression in `Object` (``sql`select ${users.id} from
  ${users}` `` gives a member chain `users.id` and an identifier `users`;
  comments inside `${}` are skipped). Untagged templates are unchanged
  (literal and substitution `Parts`).

Both fields are set by the module-binding and route reduction (`routeExpr`)
and by the client argument reduction (`urlExpr`). They are syntax only,
pinned by tests over inline sources
(`internal/lang/typescript/routes_fidelity_test.go`). The code-to-table
pass reads them (`docs/extract/table-access.md`): `Spread` in CT-R2 and
CT-Z3 (a `site_partial` entry), the substitutions in CT-R3 (bound
parameters, `Prisma.sql` fragments) and CT-Z4 (table consts, columns,
values and structural substitutions).

Chained calls (payload correction, T018h Revision 4, ADR 0060): every
call of a chain (`db.select().from(users).leftJoin(posts, on)`) starts at
the same byte. The client reduction rebuilt each cached call's top-level
expression, and set its `URLScope`, from a map keyed by start byte only,
so every call of a chain got the innermost call's text and arguments
(`from` and `leftJoin` cached `Args=[]`). Both maps are keyed by the
call's whole span (start byte and end position); the `Object` callee
chain was always right. `TestClientCallChains`
(`routes_chains_test.go`) pins Drizzle chains, `axios.get(u).then(...)`
and a Prisma fluent call. No consumer read the wrong arguments (the full
suite's goldens are unchanged), and the branch's `extractors-33` bump
already invalidates caches built before it.

## Census (the investigation corpora of T018g2, T018g3 and T018g6)

`TestDrizzleCensus` (set `GRAPHVEIN_DRIZZLE_CENSUS_CORPUS` to the `.corpus`
directory) parses kaneo and dokploy at their pins and checks the capture
against the investigation's inventory (`docs/contract/claims/data/drizzle-models-inventory.jsonl`): the gated
files (kaneo 8 G1 + 1 G2, dokploy 54 G1 + 1 G2: the investigation's text search counted 55, including `packages/server/auth-schema.ts`, whose import is commented out like the rest of the file), every table, enum,
`customType`, `relations()` and `sql` const declaration at its line with its
exported flag, every table's columns as ordered object properties, and every
extra-config element.

`TestSequelizeCensus` (set `GRAPHVEIN_SEQUELIZE_CENSUS_CORPUS` to the
`.corpus` directory) parses opencollective-api and Gladys at their pins and
checks the capture against the T018g3 investigation's inventory
(`docs/contract/claims/data/sequelize-models-inventory.jsonl`, every `read` row): the gate counts
(opencollective-api G4 281, 224 outside `migrations/`, and G9 1, the
attribute helper `server/models/DataTypes.ts`; Gladys G5 52, 48
outside `migrations/`, G6 32, G7 1, G8 1), the instance declaration, every
module-level `init` statement with its class header and `extends`, every
`define` declaration in a factory body, every attribute with each attribute
key and its value form, every option key, `indexes` element and options
object, every association call at its line (module level, or in an
`associate` arrow body) with its option keys, `module.exports` assignments
and the D forms the linker must see (`Temporal(...)`, `removeAttribute`);
that no model definition or association call in the models directories
is captured that the inventory lacks; and the payload side of the
`discovery` rows: both instance declarations (`new Sequelize` with four
arguments), Gladys's options module (three environment entries, each with
the literal `dialect` and the `define` object) and environment selector
(`return config[...]`), the 32 factory calls of the `models` object, the
`associate` loop with its `typeof` filter, opencollective-api's 197
module-level association statements and the `sequelize` option of all 52
`init` calls. Discovery rows about `package.json`, lockfiles, JSON
configuration, migrations directories and sync sites are not TypeScript
syntax and belong to the linker (SQB2).

`TestTypeORMCensus` (set `GRAPHVEIN_TYPEORM_CENSUS_CORPUS` to a directory
holding the checkouts `seerr`, `nest-admin`, `maintainerr` and
`aqualink-app`, from `scripts/cloud-corpus.sh`) parses the four T018g6
corpora at their pins and checks the version 5 capture against T018g6's
inventory (`docs/contract/claims/data/typeorm-models-inventory.jsonl`): the gate counts of each
TypeScript package (seerr G10 104 of 618 files, G12 18 files with 23
enums; nest-admin G10 58 of 237, G12 11 with 12; Maintainerr
`apps/server` G10 135 of 499, G12 6 with 7, and `packages/contracts` G12
9 with 13; aqualink-app `packages/api` G10 233 of 394, G12 7 with 7; G11
0 everywhere; G10 counts the `@nestjs/typeorm` importers since step TOB2:
nest-admin 12, Maintainerr 11, aqualink-app 21 more), that a G12 payload holds only enums, every decorator site
(seerr 228, nest-admin 193, Maintainerr 228, aqualink-app 626) at its line
on its class or member with its argument shapes, option keys in order, an
explicit positional type and the member's declared type, that no
inventoried file has a captured decorator the inventory lacks, the
discovery rows' payload sites (seerr's five option objects with their
literal `type` and shorthand `entities`, the 14-element `entities` array
and the `getDataSource()` `if`/`else if`/`else` selector with its
conditional returns; nest-admin's `registerAs(dbRegToken, ...)` with the
token const and the `forRootAsync` factory's returned spread; Maintainerr's
CLI `new DataSource({...})`; aqualink-app's conditional `dataSourceInfo`,
its `dataSourceOptions` with the spread, `SnakeNamingStrategy` and `join`
globs, and its default-exported data source), and the
`TypeOrmModule.forFeature` calls of non-test files (nest-admin 12,
Maintainerr 10, aqualink-app 19), counted in the ungated class-decorator
cache (`TemplateSyntax.Decorators`) and, since G10 reaches
`@nestjs/typeorm` importers, each also found in the payload.

## Known gaps

- Version 3 (G9, S6) and version 4 (S7, B-if, static members) did not
  bump the aggregate extractor version: this branch's extractors-35 is not
  merged yet; version 4 adds no gate row, so its files miss once through
  the payload version like every other gated file. A fact cache written by the
  branch before version 3 keeps an attribute helper module (newly gated by
  G9) without a payload; the helper then does not resolve and its columns
  stay unknown (never a wrong fact) until the file changes or the cache is
  cleared. Gated files miss once through the payload version.

- Drizzle: CommonJS `require("drizzle-orm/pg-core")` and `module.exports =
  {...}` configs are not read: version 2 captures CommonJS statements only
  in files that pass a gate row, and no Drizzle row admits CommonJS, so
  linking reports such a file as not read rather than guessing.
- Sequelize association calls in a file that neither imports nor requires
  `sequelize` and defines no model have no payload; their call sites are in
  `Facts.Calls`, and the linker treats the target tables as not complete
  (ADR 0061).
- Statements nested in module-level blocks (`if (x) { X.init(...) }`) and
  other module-level loops are not captured (version 4 captures
  module-level for-of loops and static members, the sequelize-cli `static
  associate(models)` form; version 5 instance members and decorators).
- TypeORM (version 5): gate row G10 reads `typeorm` and `@nestjs/typeorm`
  imports and the ormconfig names, so a `registerAs` or options module
  that imports neither package has no payload (the four corpora have
  none). Static members keep version 4's
  `statics` without decorators; parameter decorators and constructor
  parameter properties are not read; a module-level enum declared with
  `declare` is not kept; enum member initializers other than string and
  (signed) number literals are `unknown`.
- Options modules other than G7's literal `dialect` form and environment
  selectors other than G8's shape have no payload; facts that need them are
  unknown.
- A config named by `--config` with another file name and no `drizzle-kit`
  import has no payload (G2B2 marks the config unproven).
- Values bound outside the captured declarations (an `sql` const in a file
  outside the gate, a non-call initializer such as `const n = 5`) are not in
  the payload; a claim that needs them is unknown.
