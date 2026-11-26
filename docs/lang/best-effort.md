# Best-effort languages (Java, C# and Dart)

Task: the T021 task record (internal); ADR 0067. Java,
C# and Dart get bundled grammars and best-effort structure: files, types,
callables with exact spans, and import directives resolved to a file only
where a row below proves it. No calls, no cross-file symbol resolution, no
build files, no framework extraction. Identities: `docs/lang/identity.md`.

**Governing rule.** A fact comes from exactly one row whose conditions
hold; every other form is the default branch D and claims nothing. The Go
tables `besteffort.Shared` (`internal/lang/besteffort`), `java.Rows`
(`internal/lang/java`), `csharp.Rows` (`internal/lang/csharp`) and
`dart.Rows` (`internal/lang/dart`) equal the tables below (row id and claim text, code
marks removed), and every branch of the extractor and linker names its row;
`TestContractTableMatchesDocument`, `TestBranchesMatchRows` and
`TestFixtureCoversEveryRow` keep code, tables and the fixture one to one.
A claim no row lists is a contract change, never a code fix.

Spans are half-open, 1-based lines, 0-based UTF-8 byte columns over
LF-normalized source (contract v1). Every file record keeps
`precision: "best_effort"`; nodes carry no precision field (contract v1 has
none; T021 open point O14 (b) until a node representation exists).

## Shared

| Row | Form | Claim | Refusal / note |
|---|---|---|---|
| BE-1 | Every scanned `.java`, `.cs`, `.dart` file | File record with scanner language `java`, `c_sharp`, `dart` and `precision: "best_effort"`; file node | none |
| BE-2 | Tree with any `ERROR` node or `MISSING` node | Nothing beyond BE-1; the existing `syntax_error` warning (a knowledge gap) | All symbol rows and all import rows are refused for the file. The extractor checks the detached tree (`ERROR` nodes, named zero-width nodes); the indexer also refuses on the parser's syntax flag, which sees unnamed `MISSING` tokens the detached tree drops (`besteffort.Refuse`) |
| BE-3 | Parse failure (deadline, size, panic) or extractor panic | Existing `parse_failed` / `extract_failed` error diagnostic for that file only | Other files continue (ADR 0012) |
| BE-4 | Every emitted symbol | `contains` edge from the nearest emitted lexical owner, else the file; evidence is the symbol's span | none |
| BE-5 | Repeated qualified names after naming | `@2`, `@3` in source order per file and kind | none |
| BE-6 | Java/C# overload sets | `@overload(<types>)` per member | Naming sections below; a member's own discriminator (C# `@IFoo`, `@static`, `` `N ``) is part of the name the set is formed by |
| BE-7 | Every emitted symbol | Span per its row; `content_hash` of exactly that span | none |
| BE-8 | An import or using that would otherwise be external (JV-I8, CS-I2) while a file of the same language that may declare its name is refused by BE-2 or failed by BE-3 | `unresolved_import` warning naming a hidden file; no external node, no edge | A hidden file could declare the name (a repository type would otherwise be labelled an external dependency). A failed file may declare any name. A refused file is scanned lexically (`RefusedScope`; every whole-word keyword counts, in comments and strings too, so the scan only over-matches): Java, every `package <name>` found: it hides names equal to or under one of them; none found: its types are in the unnamed package, which no import can name (JLS 7.5), so it hides nothing. C#, when every `namespace` keyword is followed by a name and `;` (file-scoped; at most one per file, before every type and never with a block namespace): it hides usings equal to, under or a dotted prefix of one of them; otherwise (block namespaces or a mix, which may sit next to global-namespace types): plain usings equal to, under or a dotted prefix of a name found (a nested block's composite name is under its outer name), since a plain using can name only a namespace (ECMA-334 §7.8.1, CS0138), and every static and alias using; no keyword (no namespace declared, ECMA-334 §14.2-14.3): every static and alias using, no plain one. A `\u` escape in the text, or a verbatim name after `namespace`, proves nothing. No allowlist: JDK and BCL names are included (`besteffort.Hidden`, `Hiding`, `HiddenImport`). Dart has no BE-8: no Dart file declares a name a directive resolves through |
| D | Every form no row lists | Nothing | no claim, no diagnostic; the walk still descends into it |

## Java

tree-sitter-java v0.23.5. Level 4 for every symbol node; `name` is the
declared name (constructors: the type name).

| Row | Node (grammar) | Claim | Span / name | Refusal / note |
|---|---|---|---|---|
| JV-T1 | `class_declaration` | `class` | node (modifiers and annotations included, Javadoc excluded); `name` field | |
| JV-T2 | `interface_declaration` (incl. `sealed ... permits`) | `interface` | as JV-T1 | |
| JV-T3 | `enum_declaration` | `enum` | as JV-T1 | Enum constants not emitted |
| JV-T4 | `record_declaration` | `record` | as JV-T1 | Components and implicit accessors not emitted |
| JV-T5 | `annotation_type_declaration` (`@interface`) | `annotation` | as JV-T1 | |
| JV-T6 | Any JV-T form nested in a type body, in a method/constructor body (local class) or in a lambda or initializer below a type | as its row, owned by the enclosing type or callable | qualified name through owners | Lambdas and initializers add no owner |
| JV-F1 | `method_declaration` in a class, interface, enum, record or local-class body | `method` | node; `name` field | |
| JV-F2 | `constructor_declaration`, `compact_constructor_declaration` | `constructor`, name = type name | node | A compact constructor's BE-6 types are the record header's |
| JV-F3 | `annotation_type_element_declaration` | `method` | node | |
| JV-F4 | `method_declaration` directly under `program` (Java 25 compact source file) | `method` with no owner | node | No implicit class node |
| JV-R1 | Declarations in an `object_creation_expression` class body (anonymous class) or an `enum_constant` body | nothing | | Subtree skipped; the creation's arguments are still walked |
| JV-R2 | Fields, initializer blocks, enum constants, record components, `module_declaration`, `package-info.java` | nothing | | D; the walk descends (a lambda in a field may hold a local class) |
| JV-I1 | `package_declaration` | cached package name (no node) | | Used by JV-I2-I7 |
| JV-I2 | `import a.b.C;` or nested `import a.b.C.D;` | `imports` file to the one scanned Java file declaring that fully qualified type (package + top-level type [+ member types]) | evidence: directive | Self-import: no edge |
| JV-I3 | `import static a.b.C.m;`, `import static a.b.C.*;` | `imports` to the one file declaring type `a.b.C` | evidence: directive | |
| JV-I4 | `import a.b.C.*;` where `a.b.C` is a type | `imports` to the one file declaring it | evidence: directive | |
| JV-I5 | JV-I2-I4 name declared by two or more files | `ambiguous_import` warning listing the files | | No edge (Python precedent) |
| JV-I6 | `import a.b.*;` where a scanned file declares package `a.b` | nothing | | No edge, no diagnostic (Python namespace-package precedent) |
| JV-I7 | Name not declared, but it or a dotted prefix of it is a scanned package | `unresolved_import` warning | | No edge (generated sources, for example QueryDSL or protobuf classes) |
| JV-I8 | Otherwise | external node `external:.#java:<name>` (`static` dropped, `.*` kept, whitespace removed) and an `imports` edge | evidence: directive | BE-8 takes precedence |

### Java details

- **Owners.** Types and callables own what they declare. Packages are not
  owners. Lambdas, anonymous class bodies, enum-constant bodies, field
  initializers and initializer blocks add no segment.
- **Overloads (BE-6).** When two or more methods (or constructors) of one
  owner share a name, each is named `<name>@overload(<types>)`: each
  parameter's type as written with whitespace, comments and annotations
  removed, C-style array dimensions after the name appended (`int a[]` is
  `int[]`), varargs `...` kept, names and modifiers dropped. A receiver
  parameter (`Foo this`) is not part of a Java signature and is left out.
  A compact canonical constructor takes the record header's types. A lone
  method keeps its plain name. Identical type lists then follow BE-5.
- **Imported names.** The written name of a directive is its qualified
  name with whitespace and comments removed. A single static import names a
  member, so its type is the name without the last segment; every other
  form names a type (or, for `.*`, a type or package). Resolution reads
  only the current inventory: a file's package (JV-I1) and its importable
  type paths, which are its top-level types and their member types (never
  local or anonymous classes), from files that are not refused. Lookup
  order: one declaring file (JV-I2-I4), several (JV-I5), a package
  on-demand import of a scanned package (JV-I6), a scanned package as the
  name or a dotted prefix of it (JV-I7), a hidden Java file that may
  declare the name (BE-8: any failed file, a refused file whose package is
  the name or a dotted prefix of it), otherwise external (JV-I8).
- **Assumption V1.** The repository compiles: one fully qualified type
  name declared once in the repository names that declaration. A name
  declared by two files (several Maven modules, say) is JV-I5, never a
  guess. A type declared only in a refused (BE-2) or failed (BE-3) file is
  invisible to resolution, so while such a file exists an import that would
  be JV-I8 and that it may declare is reported by BE-8 instead (T021-B3;
  before, it became a `java:` external; T021-B4 narrowed it to the refused
  file's packages).

### Cached payload

`facts.BestEffortFacts` (version 1) on each Java file's cached facts:
`Refused` (BE-2), `Scoped` and `Scopes` (a refused file's lexically
provable packages, BE-8), `Package` (JV-I1), `Types` (importable type
paths) and `Imports` (name without `static` and `.*`, `Static`, `Star`, directive
span). Linking reads only this payload and the files' nodes and edges, so a
changed file re-resolves every importer without re-parsing it. Another
payload version is a cache miss.

### Oracle

`scripts/oracle/java_decls.sh --scratch DIR --root ROOT [--out FILE]`
(opt-in, never in CI; needs a JDK 21+) compiles
`scripts/oracle/JavaDecls.java`, which lists every type, method and
constructor from javac's parser (`JavacTask.parse()` only, `-proc:none`,
nothing runs), and compares with Graphvein's index of ROOT through
`TestJavaDeclsOracle`: kind, owner path, start and end, outside BE-2 files.
Overload and repeated-name suffixes and import resolution are not compared.
Results (T021-B2): the fixture 43 of 43, spring-modulith `a58dd45b` 4,279
of 4,279 (682 files), commons-lang `e2a0789b` 12,719 of 12,719 (645
scanned files), no disagreement.

## C#

tree-sitter-c-sharp v0.23.5. Level 4 for every symbol node; `name` is the
declared name (constructors: the type name; destructors `~T`; operators
`operator <op>`; indexers `this`). No compiler oracle exists for C# here
(T021 open point O13 (a)): the rows rest on the fixture goldens, the eShop
census, BE-2, the guard row CS-G1 and review.

| Row | Node (grammar) | Claim | Span / name | Refusal / note |
|---|---|---|---|---|
| CS-T1 | `class_declaration` | `class` | node (attribute lists and modifiers included, `///` comments excluded); `name` field | Attribute `partial: true` when the `partial` modifier is present (each part is its own node in its own file) |
| CS-T2 | `struct_declaration` (incl. `ref`, `readonly`) | `struct` | as CS-T1 | as CS-T1 |
| CS-T3 | `interface_declaration` | `interface` | as CS-T1 | as CS-T1 |
| CS-T4 | `record_declaration` (`record`, `record class`, `record struct`) | `record`; attribute `struct: true` for `record struct` | as CS-T1 | as CS-T1 |
| CS-T5 | `enum_declaration` | `enum` | as CS-T1 | Members not emitted |
| CS-T6 | Traversal | Declarations inside `namespace_declaration` bodies, after a `file_scoped_namespace_declaration` (a sibling node spanning only `namespace X;`), and inside every `preproc_if`/`preproc_elif`/`preproc_else` branch, without evaluating conditions | | Namespaces are not owners |
| CS-F1 | `method_declaration` | `method`; explicit interface implementation `M@IFoo` | node | |
| CS-F2 | `constructor_declaration` whose `name` is the enclosing type's name | `constructor`, name = type name; `static` modifier: `T@static` | node | Primary-constructor parameters not emitted |
| CS-F3 | `destructor_declaration` | `method` named `~T` | node | |
| CS-F4 | `local_function_statement` | `function`, owned by the enclosing callable or property; under a top-level statement (`global_statement`): no owner | node | Lambdas add no owner; in a field initializer's lambda the type is the nearest owner (BE-4) |
| CS-F5 | `operator_declaration`, `conversion_operator_declaration` | `method` named `operator <op>` / `implicit operator <T>` / `explicit operator <T>` | node | `<op>` is the tokens after `operator` (`operator checked +`); `<T>` as written without whitespace |
| CS-P1 | `property_declaration` | `property`; explicit interface `P@IFoo` | node (accessors included) | Accessors are not separate nodes |
| CS-P2 | `indexer_declaration` | `property` named `this` (overloads per BE-6; explicit `this@IList`) | node | |
| CS-G1 | `constructor_declaration` whose `name` differs from the enclosing type's name, or outside a type | nothing | | Subtree skipped. The pinned grammar reads a C# 14 extension block `extension(string s) { ... }` without an error as a constructor named `extension` holding its members as local functions (T021-B3, precision stop S2). Generic and receiver-only extension blocks parse with errors (BE-2) |
| CS-R1 | `delegate_declaration`, `event_declaration`, `event_field_declaration`, fields, enum members, accessors, lambdas, anonymous methods | nothing | | D; the walk descends (a lambda or an accessor may hold a local function); lambdas add no owner |
| CS-I1 | A `using` (plain, `global`, `static`, alias) that may name something the repository declares: the target (with `global::` removed), tried as written and, inside a namespace, relative to that namespace and each enclosing one, is a declared namespace or a prefix of one; or, for `static` and alias, a proper dotted prefix of it is a declared namespace, the using sits inside a namespace, or its first segment is a type the repository declares in the global namespace | nothing | | Never a file edge, no diagnostic |
| CS-I2 | Any other `using` at compilation-unit level, or written with `global::` | external node `external:.#dotnet:<name>` (alias target, `global::` and whitespace removed) and an `imports` edge | evidence: directive | CS-I4 and BE-8 take precedence. `extern alias` and alias targets that are not names (tuple, predefined, array or pointer types): D |
| CS-I3 | Any other `using` inside a namespace body or after a file-scoped namespace, not written with `global::` | nothing | | Its name may be relative to an enclosing namespace that a referenced assembly declares, so the written name is not provably the external one |
| CS-I4 | A `using` that CS-I1 and CS-I3 do not match, one of whose proper dotted prefixes of two or more segments is a declared namespace or a prefix of one | `unresolved_import` warning naming that prefix | | No external node, no edge; takes precedence over BE-8. The repository may declare the name outside `.cs` files, which the inventory does not read: Razor components (`.razor`, `.cshtml`) take their namespace from the project's root namespace and folder (eShop's `using eShop.WebApp.Components;`), and `.proto` files and source generators add namespaces at build time (T021-F1). A one-segment prefix (`Microsoft`, `System`) does not count |

### C# details

- **Owners.** Types, callables and properties own what they declare.
  Namespaces (block and file-scoped) are not owners (T021 open point O4
  (a)): `class:src/Order.cs#Order`. Lambdas, anonymous methods and field
  initializers add no segment. Local functions of top-level statements have
  no owner.
- **Discriminators.** Appended to the declared name before BE-6 and BE-5:
  explicit interface members `@` plus the interface's last name segment
  without generic arguments or alias (`CompareTo@IComparable`,
  `Descriptor@IMessage`, `this@ILookup`); the static constructor
  `T@static`; the generic arity suffix below.
- **Generic arity.** When type declarations of one owner (or of the file's
  top level) share a name but differ in generic arity, each generic one
  gets the CLR metadata suffix `` `N ``: `Result`, `` Result`1 ``,
  `` Result`2 ``. Same name and arity (two namespaces of one file, `#if`
  alternatives) follow BE-5 (`` Result`1@2 ``).
- **Overloads (BE-6).** Methods, constructors, operators, conversions and
  indexers. Each parameter is its modifiers (`ref`, `out`, `in`, `params`,
  `this`, `scoped`) and its type as written with whitespace, comments and
  attributes removed, modifiers separated by one space, names dropped:
  `Touch@overload(this Invoice,int)`,
  `Order@overload(int,ref string,params int[])`. The grammar leaves a
  `params` parameter's type and name directly in the parameter list; the
  extractor reads them as the last parameter. Identical type lists (`#if`
  alternatives) then follow BE-5.
- **Using inventory.** Linking reads only the payload: each readable C#
  file's declared namespaces (nested block names joined with `.`), the
  names of its types in the global namespace (`Program` for top-level
  statements) and its usings. A refused or failed file contributes nothing
  and triggers BE-8 for every using that would be CS-I2 and that it may
  declare: any using for a failed file or a refused one with a `\u`
  escape; for a refused file whose namespaces are all file-scoped, usings
  of those namespaces, of names under them and of their dotted prefixes;
  for a refused file with block namespaces (eShop's committed gRPC file),
  plain usings related to a namespace name in its text in the same way and
  every static and alias using; for a refused file without the keyword,
  every static and alias using only (T021-B5). The inventory does not see
  namespaces declared outside `.cs` files (Razor components, whose
  namespace comes from the project's root namespace and folder; `.proto`
  files and source generators), so a using under a repository namespace of
  two or more segments that no `.cs` file declares is a warning (CS-I4),
  never an external: on eShop this turns 33 of 146 `dotnet:` nodes
  (95 usings) into warnings, among them `eShop.WebApp.Components`, and,
  because eShop declares extension namespaces such as
  `Microsoft.Extensions.Hosting`, most of its `Microsoft.Extensions.*` and
  `Microsoft.AspNetCore.*` usings (T021-F1).

### C# cached payload

`facts.BestEffortFacts` (version 1): `Refused` (BE-2), `Scoped` and
`Scopes` (the namespace names in a refused file's text, BE-8), `Global`
(set when those names bound only namespaces: the file may also declare
global-namespace types), `Namespaces`,
`GlobalTypes` and `Imports` (target `Name` without `global::`, `Static`,
`Alias`, `Scope` (the enclosing namespace, empty at compilation-unit level
or for a `global::` name), `Prefixes` (the target's proper dotted prefixes
from the syntax) and the directive span).

## Dart

UserNobody14/tree-sitter-dart `0038280`. Level 4 for every symbol node;
`name` is the declared name (unnamed constructors `new`, setters `x=`,
operators `operator <op>`, anonymous extensions `extension on <T>`). No
Dart oracle exists here (T021 open point O13 (a)): the rows rest on the
fixture golden, the localsend census, BE-2, the guard rows and review.

| Row | Node (grammar) | Claim | Span / name | Refusal / note |
|---|---|---|---|---|
| DA-T1 | `class_definition` (modifiers `abstract`, `sealed`, `base`, `final`, `interface`, `mixin class`; mixin application `class C = B with M;`; Dart 3.12/3.13 bodiless `class C;` and primary constructors) | `class`; attribute `mixin: true` for `mixin class` | node (its annotations are inside it); name from the `name` field or the `mixin_application_class` identifier | A primary constructor is not emitted (DA-R1) |
| DA-T2 | `mixin_declaration` (incl. `base mixin`) | `mixin` | node; first `identifier` child | |
| DA-T3 | `extension_declaration` | `extension`; anonymous: `extension on <T>` | node; `<T>` is the on-type as written, comments removed, whitespace removed except one space between two word characters (`extension on Map<String,int>?`, `extension on void Function()`) | Two anonymous extensions on one type: BE-5 |
| DA-T4 | `extension_type_declaration` | `extension_type` | node | Representation field not emitted |
| DA-T5 | `enum_declaration` | `enum` | node | Enum values and a primary constructor not emitted |
| DA-F1 | `function_signature` directly under `program` | `function` | DA-S | |
| DA-F2 | `method_signature` or `declaration` wrapping a `function_signature` in a type body | `method` | DA-S | `static`, `external`, abstract (bodyless) included |
| DA-F3 | `getter_signature` directly under `program`, or wrapped in a type body | `function` at top level, `method` in a type; attribute `accessor: "get"` | DA-S | |
| DA-F4 | `setter_signature` (as DA-F3) | as DA-F3, name `x=`, `accessor: "set"` | DA-S | A getter and a setter never collide |
| DA-F5 | `operator_signature` wrapped in a type body | `method` named `operator <op>` | DA-S | `<op>` is the text between `operator` and the parameters without whitespace (`operator []=`); unary and binary `-` collide: BE-5 |
| DA-F6 | `local_function_declaration` | `function`, owned by the nearest emitted owner | node | The enclosing callable; in a field initializer's lambda the type; at top level no owner. Lambdas add no owner |
| DA-C1 | `constructor_signature` or `constant_constructor_signature` without a `new` token or whose `new` token follows a `.`, `factory_constructor_signature` or `redirecting_factory_constructor_signature`, in a type body, whose first identifier (before the parameters) is the enclosing type's name | `constructor`; one identifier: unnamed `new`; two: named `<second identifier>` | DA-S | `Shape()`, `Shape.named()`, `const Shape.c()`, `factory Shape()`, `factory Shape.f()`, `factory Shape.r() = Other;`. `Shape.new()` and `const Shape.new()` (Dart 2.15) declare the unnamed constructor: one identifier and a `.` before `new`, so unnamed `new` (T021-F1) |
| DA-C2 | `constructor_signature` or `constant_constructor_signature` with a `new` token that no `.` precedes (Dart 3.13 concise constructor) | `constructor`; without a `name` field: unnamed `new`; with one: named `<name>` | DA-S | `new(...)`, `const new()`, `new named()`, `const new c()`. Without the token check `new named();` would read as the unnamed `named` of the classic form |
| DA-C3 | `factory_constructor_signature` or `redirecting_factory_constructor_signature` in a named type body with no identifier, or with one identifier that differs from the enclosing type's name (Dart 3.13 concise factory) | `constructor`; without an identifier: unnamed `new`; with one: named `<identifier>` | DA-S | `factory() => ...`, `factory make() => ...`, `factory red() = C.named;`. The name comparison is the only difference from the classic unnamed `factory C()` |
| DA-S | Span rule for DA-F1-F5 and DA-C1-C3 | Span from the first annotation of the contiguous run of annotation siblings directly before the declaration (comments between them included; at top level also a directly preceding `external` keyword) to the end of the following `function_body`; without a body, to the end of the wrapping `declaration` node (at top level the signature) extended over a directly following `;` (spaces and tabs only between) | | Member and top-level annotations are preceding siblings in this grammar, and a top-level `external` is a token outside the signature; doc comments are outside the span |
| DA-G1 | `constructor_signature` or `constant_constructor_signature` that DA-C1 and DA-C2 do not match: no `new` token, or a `.` before it, and a first identifier that differs from the enclosing type's name (`Other.new()`), or no named enclosing type (anonymous extension) | nothing | | Subtree skipped. `0038280` reads an untyped method (`foo();`, `foo() {}`) as a constructor signature without an error (T021-B4, precision stop S2) |
| DA-G2 | `factory_constructor_signature` or `redirecting_factory_constructor_signature` that DA-C1 and DA-C3 do not match: two identifiers whose first differs from the type's name, a `new` token, or no named enclosing type | nothing | | Subtree skipped; not valid Dart, refused rather than guessed |
| DA-R1 | `type_alias`, variables and fields, enum values, primary constructors and their bodies (`this : ...;`), `function_expression` (lambdas) | nothing | | D; the walk descends (a lambda may hold a local function); lambdas add no owner |
| DA-I1 | `dart:<lib>` URI (import or export), `<lib>` a plain name | external `external:.#dart:<lib>` and `imports` | evidence: the directive | |
| DA-I2 | Relative URI (no scheme) of `import`, `export` or `part` | `imports` to the scanned file at the URI resolved against the importing file's directory | evidence: the directive | Percent-decoded. Missing, absolute (`/x.dart`) or above the repository root: `unresolved_import` warning, no edge. Self-import: no edge. `part` gives the edge (O12) |
| DA-I3 | `package:<own>/<p>`, `<own>` = `name:` of the nearest enclosing scanned `pubspec.yaml` | `imports` to `<pubspec dir>/lib/<p>` | evidence: the directive | Missing, or outside `lib/` after `..`: `unresolved_import` warning, no edge |
| DA-I4 | `package:<n>/<p>`, `<n>` the name of another scanned `pubspec.yaml`; or the file's nearest `pubspec.yaml`, or any scanned one, has no readable name | nothing | | Workspace or path dependency not provable without reading dependencies (O11); an unknown name could be `<n>` |
| DA-I5 | `package:<n>/<p>`, `<n>` no scanned pubspec's name (every scanned name readable) | external `external:.#pub:<n>` and `imports` | evidence: the directive | |
| DA-I6 | Conditional import/export `'a.dart' if (dart.library.io) 'b.dart' ...` | Each URI (default and every alternative) handled by DA-I1-I5 and DA-I8 independently, conditions not evaluated | | Rust cfg / Go build-tag precedent |
| DA-I7 | `part of` (URI or library name), `library` | nothing | | `part` gives the edge (O12) |
| DA-I8 | Any other scheme (`http:`, `file:`, ...), a malformed `package:` or `dart:` URI, a query or fragment, a non-literal URI (interpolation, escapes, adjacent or triple-quoted strings) | nothing | | D |

### Dart details

- **Owners.** Types own their members; callables own their local
  functions. Libraries and parts are not owners: a declaration in a `part`
  file belongs to that file. Lambdas and field initializers add no segment.
  Dart has no overloading, so BE-6 never applies; repeated names follow
  BE-5 (`Shape.operator -@2`, `extension on List<int>@2`).
- **Constructors.** Unnamed constructors are named `new`, Dart's own name
  for the unnamed constructor (`Shape.new`; `Shape(...)`, `Shape.new(...)`,
  `factory Shape()`, `new(...)` and `factory()` agree), named ones by their
  name (`Shape.named`). The `new` token with no `.` before it (DA-C2) and
  the comparison with the enclosing type's name (DA-C1, DA-C3) are the only
  things that tell the
  Dart 3.13 forms from the classic ones in `0038280` trees; anything they
  do not match is refused (DA-G1, DA-G2).
- **Packages.** `pubspec.yaml` is read for one top-level scalar only
  (`dart.PackageNames`, T021 open point O10 (a)): the first unindented
  `name:` line whose value, plain or quoted, matches
  `[A-Za-z_][A-Za-z0-9_]*` (a trailing comment is allowed). A pubspec
  without one has an unknown name. A file belongs to the package of its
  nearest enclosing `pubspec.yaml` (assumption V2). Generated files
  (`*.g.dart`, `*.freezed.dart`, `*.mapper.dart`) are indexed like any
  other file (O9); the repository's ignore files decide.

### Dart cached payload

`facts.BestEffortFacts` (version 1): `Refused` (BE-2) and `Imports`, one
entry per URI with `Directive` (`import`, `export` or `part`), `Name` (the
URI as written, without quotes) and the directive span. Linking reads only
this payload, the scanned Dart paths and the pubspec names, which are read
anew on every link.
