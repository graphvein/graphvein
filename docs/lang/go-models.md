# Go model syntax (GORM)

ADR 0063; task T018g5 (step GB1 captures the syntax; steps GB2-GB5 link
it). Aggregate extractor version: extractors-36 (36 from T018g5 GB1: cached
Go facts gained the `Go` payload). Contract version unchanged; the payload
adds no graph record.

The Go extractor captures the syntax that GORM model linking reads: the
type shapes of every non-test Go file (structs with their fields and tags, named
types, the methods GORM's schema parse calls, string constants) and, in
files that import GORM, its drivers or inflection (gate G1), the functions,
locals, calls and package-level variables of GORM discovery, as reduced
expression trees. The payload is syntax, never a claim (rule 31): which
struct is migrated, which receiver is a `*gorm.DB`, which call opens which
dialect and which constant a `TableName` returns are decided at link time
through the module's own packages under the claim contract in
`docs/contract/claims/gorm-models.md`.

## Why a cached payload

On a cache hit the indexer replays stored facts and never parses the file,
so the link pass has no tree. The Go facts cached before this payload keep
one node per declaration and the calls with their callee name: no struct
fields, tags, embedded fields, underlying types, method bodies, call
arguments, receiver chains or statement nesting (T018g5 task file,
"Payload decision"; ADR 0063).

Type shapes are captured in every non-test Go file, not only in files that
import GORM: a model's struct, an embedded struct, a named field type or a
table-name constant can live in a file that imports nothing from GORM
(gotify's five models are in files that import only `time`). GORM sites
are captured only in gated files; a migrate call in a file that passes no
gate (a receiver whose type is inferred from another package's function)
still has its callee name in `Facts.Calls` (`StaticName` `AutoMigrate`),
so the linker can mark the model set as possibly incomplete instead of
missing the site silently.

## Payload

`facts.Facts.Go` (JSON `Go`), type `facts.GoFacts`, version 9
(`facts.GoModelsVersion`; version 2 adds the structure fields below,
version 3 the router fields of `docs/lang/go-routes.md`, version 4 two
router filter forms of that payload, version 5 its net/http serving
syntax, version 6 its wrappers, request helpers and `x.Handler = v`
bindings, version 7 its generated-server record and entry calls, version 8 the
forwarding proof of a function literal, version 9 its escape records,
deciding statements and request reads, T023 GR8). Every Go file
carries it (a `_test.go` file only its version among the GORM fields);
other languages do not. Lines are 1-based.

- `Version`; `Structs`: package-level struct type declarations in source
  order (`Name`, `Line`, `EndLine`, `TypeParams`, `Fields`); each field
  declaration (`Names`, empty for an embedded field; `Embedded`; `Pointer`
  for an embedded `*T`; `Type`; `Tag`, the decoded tag literal as
  `reflect.StructTag` reads it; `Line`).
- `Types`: package-level named non-struct types and every alias (`Name`,
  `Alias`, `TypeParams`, `Type`: the underlying or aliased type, `Line`).
- `Methods`: methods named `TableName`, `Value`, `Scan`, `GormDataType` or
  `GormDBDataType` (`Receiver`: the base type name, `Pointer`, `Name`,
  `Params` and `Results`: declared counts, `Line`); a `TableName` whose
  body is exactly one `return e` keeps `Return` (T018g5 Revision 1, O7:
  literals, constants and `+` of those resolve at link time).
- `Consts`: package-level constants whose value is built from string
  literals, names (not `iota`), selectors and `+` (`Name`, `Type`, `Value`,
  `Line`), which a `TableName` body may name.
- `Gated` (gate G1), then only in gated files: `Vars` (package-level var
  specs: `Names`, `Type`, `Values`, `Line`) and `Funcs` (every function and
  method with a body: `Name`, `Receiver`, `Line`, `EndLine`, `Params`,
  `Results`, `Locals`, `Sites`, `Statements`, `Mutations`, and from T018g5
  Revision 2 `LocalTypes`, `Returns`, `Calls`, `Stores`, `Literals`).
- Structure fields (every Go file, test files included; not GORM syntax and
  not part of the payload table below: they belong to the Go structural
  claim contract, `docs/lang/go-structure.md`, ADR 0068): `Build`, the
  header's build constraint lines as go/build reads them; `Rejected`, set
  when go/parser rejects the file; `Unread`, the extractor's default-branch
  counts by form. They are set by `Extract` after the GORM capture and are
  absent when the capture failed (the payload is then nil).
- Parameters (`Name`, empty when unnamed; `Type`; `Variadic`).
- Locals (`Kind` `var`, `define` or `assign`; `Names`; `Type` for `var`;
  `Values`; `Line`; `Statement`, the index of the body's top-level
  statement that holds it; `Nesting`), kept only when a name they declare
  or assign is used by a site, mutation, call, store, literal or return
  of the function, directly or through the values of another kept local
  (a receiver, a configuration variable, an argument slice); other locals
  are row FD.
- Handle flows (T018g5 Revision 2, A4; the receiver proof reads where a
  `*gorm.DB` may come from): `LocalTypes` (names of type declarations in
  the body), `Returns` (`Value`, `Line`, `Nesting` of each `return` outside
  function literals, in a function whose first result is `*x.DB`), `Calls`
  (calls of a name or `name.name` with an argument of handle shape),
  `Stores` (`x.f = e` with a value of handle shape) and `Literals`
  (composite literals of a named type with an element value of handle
  shape). Handle shape (`facts.GoHandleShaped`): a name, selector, call,
  index, spread, or `&`/`*` of one.
- Sites (`Call`, the whole call with its receiver chain and arguments;
  `Line`; `Statement`; `Nesting`). A call inside a captured site's tree
  (the `Debug()` of `db.Debug().AutoMigrate(x)`, the `postgres.Open(dsn)`
  of `gorm.Open(postgres.Open(dsn), cfg)`) is not repeated, except inside
  a function literal.
- Statements (`Index`, `Line`, `Kind` `call`, `define`, `assign`, `return`
  or `if`, `Callee`): the order rule's top-level call statements.
- Mutations (`Target`, `Value` when the assignment's sides pair up, `Line`,
  `Statement`, `Nesting`).
- Nesting marks: `unconditional` (directly in a top-level statement),
  `if-header` (in a top-level `if`'s initializer or condition),
  `conditional` (in a nested block, an `if` branch, a loop, a `switch` or
  `select`, `defer` or `go`), `closure` (inside a function literal;
  overrides the others).
- Expressions (`Kind` plus the fields of the kind): `identifier` (`Name`),
  `selector` (`Object`, `Name`; also a qualified type `pkg.T`), `call`
  (`Callee`, `Args`; a type conversion `[]byte(x)` too), `composite`
  (`Type`, absent for an elided nested literal; `Elements` with `Key` and
  `Value`), `unary` (`Value` the operator, `Object`), `binary` (`Value`,
  `Left`, `Right`), `string` (decoded `Value`), `number` (source `Value`),
  `boolean` (`Value`), `nil`, `spread` (`Object`, a `x...` argument),
  `index` (`Object`, `Args`: `x[i]` and a generic instantiation `T[A, B]`),
  types `pointer`, `slice` (`Object`), `array` (`Value` the length text,
  `Object`), `map` (`Left` key, `Right` value), `chan`, `func`, `interface`,
  `struct` (an anonymous struct type, fields not kept), and `unknown`.
  Parentheses are transparent; comments are never captured. A tree deeper
  than 48 levels is cut to `unknown`.

Validity: `GoFacts.ModelsValid()` checks the version, that only gated
files carry `Vars` and `Funcs`, every record's required fields and lines,
every nesting and kind, and every expression's required fields; the
indexer checks it on every cache read, and a Go file without a payload is
a miss too, so a payload of another version, a damaged one or an entry
written before extractors-36 is re-extracted. A capture failure (a
recovered panic) keeps the file's other facts and every other file:
diagnostic `model_syntax_capture_failed` (warning), no payload, and the
next run tries again.

## Payload table

Every capture branch in `internal/lang/golang/models.go` reports one row
id below; `TestGoPayloadContract` checks that the Go table and this table
match 1:1, that a snippet per row fills exactly the row's fields, and that
the snippets together emit exactly the rows of the table.

| id | form | payload |
|---|---|---|
| G1 | the file imports `gorm.io/gorm` or a package under it, a `gorm.io/driver/...` package, `github.com/glebarez/sqlite`, `github.com/jinzhu/inflection`, or `github.com/jinzhu/gorm` or a package under it | payload: Version, Gated (and every row below) |
| GT | a `_test.go` file, whatever it imports: not part of the package GORM migrates from (contract row GO-K1, open point O10) | payload: Version |
| GD | every other Go file | payload: Version (type-shape rows T only) |
| T-struct | a package-level `type N struct {...}`, generic or not | struct: Name, Line, EndLine, TypeParams, Fields |
| T-field | a named field declaration (`A int`, `A, B string`), with or without a tag | field: Names, Type, Tag, Line |
| T-embedded | an embedded field (`T`, `*T`, `pkg.T`, `*pkg.T`), with or without a tag | field: Embedded, Pointer, Type, Tag, Line |
| T-type | a package-level `type N T` whose `T` is not a struct type, generic or not | type: Name, TypeParams, Type, Line |
| T-alias | a package-level `type N = T` | type: Name, Alias, Type, Line |
| T-interface | a package-level `type N interface {...}` (T023 GR9): its method names | type: Name, Type, Methods, Line |
| T-method | a method named `TableName` (other bodies), `Value`, `Scan`, `GormDataType` or `GormDBDataType` | method: Receiver, Pointer, Name, Params, Results, Line |
| T-tablename | a `TableName` method whose body is exactly one `return e` | method: Receiver, Pointer, Name, Results, Return, Line |
| T-const | a package-level constant whose value is string literals, names (not `iota`), selectors and `+` of those | const: Name, Type, Value, Line |
| TD | every other declaration: other methods, functions and package-level vars of an ungated file (type declarations inside their bodies included), constants with other values or none (`iota` lists) | nothing |
| V1 | a package-level var spec of a gated file | var: Names, Type, Values, Line |
| F1 | a function or method declaration with a body in a gated file | func: Name, Receiver, Line, EndLine, Params, Results, Locals, Sites, Statements, Mutations, LocalTypes, Returns, Calls, Stores, Literals |
| F-param | a parameter, receiver or result (one per name; unnamed once; `...T` variadic) | param: Name, Type, Variadic |
| L-var | a `var` spec in a function body, kept (see Locals) | local: Kind, Names, Type, Values, Line, Statement, Nesting |
| L-define | a `:=` declaration in a function body (not a `range` clause), kept | local: Kind, Names, Values, Line, Statement, Nesting |
| L-assign | an `=` assignment whose targets are all identifiers, kept | local: Kind, Names, Values, Line, Statement, Nesting |
| X-mutation | an `=` assignment with a target selector ending in `Config`, `NamingStrategy`, `DisableForeignKeyConstraintWhenMigrating`, `IgnoreRelationshipsWhenMigrating`, `TablePrefix`, `SingularTable`, `NoLowerCase`, `IdentifierMaxLength` or `NameReplacer` (one per such target) | mutation: Target, Value, Line, Statement, Nesting |
| X-site | a call whose callee is a selector ending in `AutoMigrate`, `CreateTable`, `Open`, `New`, `Migrator`, `Set`, `Session`, `Debug`, `WithContext`, `SetupJoinTable`, `Exec`, `Raw` or a Migrator DDL method (`DropTable`, `RenameTable`, `AddColumn`, `DropColumn`, `AlterColumn`, `MigrateColumn`, `MigrateColumnUnique`, `RenameColumn`, `CreateView`, `DropView`, `CreateConstraint`, `DropConstraint`, `CreateIndex`, `DropIndex`, `RenameIndex`; T018g5 Revision 2, A2 and A3) | site: Call, Line, Statement, Nesting |
| X-inflection | a call whose callee is a selector on the file's local name of the `github.com/jinzhu/inflection` import | site: Call, Line, Statement, Nesting |
| L-type | a type declaration (`type T ...`, `type T = ...`) anywhere in a function body of a gated file (Revision 2, A6) | local type: Name |
| X-return | a `return` statement outside function literals in a function whose first result is `*x.DB` (Revision 2, A4) | return: Value (first value, absent for a bare return), Line, Nesting |
| X-call | a call whose callee is a name or `name.name` with an argument of handle shape (a name, selector, call, index, spread, or `&`/`*` of one), anywhere in the body, nested calls each on their own (Revision 2, A4) | call: Call, Line, Statement, Nesting |
| X-store | an `=` assignment to a selector target (`x.f = e`) whose value has handle shape; Value absent when the sides do not pair up (Revision 2, A4) | store: Target, Value, Line, Statement, Nesting |
| X-literal | a composite literal of a named type (`T{...}`, `pkg.T{...}`) with an element value of handle shape, anywhere in the body, nested literals each on their own (Revision 2, A4) | literal: Call (the literal), Line, Statement, Nesting |
| X-statement | a top-level statement of a body that calls a name or `name.name`: an expression statement, a `:=` or `=` with that single value, `return` of it, or an `if` whose initializer is one of those | statement: Index, Line, Kind, Callee |
| N-unconditional | a local, site or mutation directly in a top-level statement | Nesting |
| N-if-header | a local, site or mutation in a top-level `if`'s initializer or condition | Nesting |
| N-conditional | a local, site or mutation in a nested block, `if` branch, loop, `switch`, `select`, `defer` or `go` | Nesting |
| N-closure | a local, site or mutation inside a function literal | Nesting |
| FD | in a gated file, everything else: locals no site or mutation uses, compound assignments (`+=`), assignments to index expressions, assignments to selectors of values without handle shape, `range` clauses, other calls (kept only inside captured trees), functions without a body | nothing |
| E-identifier | an identifier, type name, `iota` | Kind, Name |
| E-selector | `x.Name` or a qualified type `pkg.T` | Kind, Name, Object |
| E-call | a call `f(a, b)` or a type conversion `T(x)`; type arguments of a generic call dropped | Kind, Callee, Args |
| E-composite | a composite literal `T{...}`, `&T{...}` (with E-unary), `[]T{...}`, or an elided nested `{...}` (no Type) | Kind, Type, Elements |
| E-keyed | a keyed element `Key: value` | Key, Value |
| E-positional | a positional element | Value |
| E-unary | a unary expression (`&x`, `-1`, `!x`, `*p`, `<-c`) | Kind, Value, Object |
| E-binary | a binary expression (`a + b`, `x != nil`) | Kind, Value, Left, Right |
| E-string | an interpreted or raw string literal that decodes | Kind, Value |
| E-number | an integer, float or imaginary literal | Kind, Value |
| E-boolean | `true`, `false` | Kind, Value |
| E-nil | `nil` | Kind |
| E-spread | a variadic argument `x...` | Kind, Object |
| E-index | `x[i]` or a generic type `T[A, B]` | Kind, Object, Args |
| E-pointer | a pointer type `*T` | Kind, Object |
| E-slice | a slice type `[]T` | Kind, Object |
| E-array | an array type `[N]T` or `[...]T` | Kind, Value, Object |
| E-map | a map type `map[K]V` | Kind, Left, Right |
| E-chan | a channel type | Kind |
| E-func | a function type | Kind |
| E-interface | an interface type | Kind |
| E-struct | an anonymous struct type | Kind |
| E-paren | a parenthesized expression or type: the wrapped expression's fields | Kind, Name |
| E-unknown | every other expression (function literals, type assertions, slice expressions, rune literals, undecodable strings) and any node at depth 48 | Kind |

## Census (the T018g5 investigation corpora)

`TestGormCensus` (set `GRAPHVEIN_GORM_CENSUS_CORPUS` to the corpus
directory holding `gotify` and `realworld-gin`, cloned by
`scripts/cloud-corpus.sh gotify realworld-gin`) parses both corpora at
their pins and checks the capture against the investigation's inventory
(`docs/contract/claims/data/gorm-models-inventory.jsonl`, every `read` row): the gate counts
of the inventory's Gates table (non-test files, G1 files, structs, fields
and tagged fields), every reached struct at its line, every field row's
Go type form and `gorm` tag settings, every `gorm.Open` site with its
dialector and configuration form, every `gorm.Config` literal's keys,
every migrate site with its receiver form and each argument, and the
`users.AutoMigrate()` call statement of the order rule.

## Known gaps

- Struct types declared inside a function are not captured (row TD): GORM
  discovery claims only module struct types a migrate argument names by
  `T` or `pkg.T`.
- Methods with other names are not captured: GORM's schema parse calls
  only the listed ones for table and column facts.
- Calls with other selectors are kept only inside a captured tree (a
  receiver chain, an argument, a local's or var's value); a `Table(...)`
  or `Scopes(...)` link of a receiver chain is in the site's tree.
