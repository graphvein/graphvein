# ADR 0067 - Java, C# and Dart best-effort tier

**Status:** Accepted (delegated review, 2026-11-16)
**Date:** 2026-11-16

Task: the T021 task record (internal) (claim contract, open points
O1-O14); counts: the T021 task record (internal). Written in the investigation; implementation steps add implementation notes. Where an open point is
still undecided, this ADR records its provisional default.

## Context

Milestone M2 (item 6) of the project plan moves the Java/C#/Dart best-effort tier into v1
(developer decision 2026-11-09): bundled grammars through the existing WASM
build, checksum and licence pipeline, and structural extraction of files,
types, functions and imports labelled `best_effort`, without calls,
cross-file symbol resolution, build files or frameworks. ADR 0002 chose
upstream tree-sitter in WASM through wasitter/wazero without cgo; ADR 0006
defines shared compilation, isolated workers and the compilation cache
namespace; ADR 0007 owns the CI build of non-upstream modules and recorded
that the C# and Dart modules (CI run C, `37428985231`) load and match their
goldens; ADR 0008 and ADR 0028 registered the current seven production
modules by copying audited spike assets. The scanner already labels
`.java` files `java`, `.cs` files `c_sharp` and `.dart` files `dart`, all
with `precision: "best_effort"`, and emits them as file nodes only.

## Options considered

**Grammar assets**

- **A. Copy the audited spike modules** (Java: wasitter v0.1.0 release,
  tree-sitter-java v0.23.5, Zig 0.15.2; C# and Dart: run C, WASI SDK 25.0
  clang 19.1.5, tree-sitter-c-sharp v0.23.5 and tree-sitter-dart
  `be07cf7`). No build, the same toolchains as the current set, existing
  loading and reproducibility evidence.
- **B. Rebuild at newer pins** (tree-sitter-c-sharp HEAD with C# 14,
  tree-sitter-dart `0038280` with Dart 3.12/3.13). Covers newer syntax;
  needs a CI run, a new review per ADR 0007 and a re-inventory. Java has
  no newer tag.
- **C. Another Dart grammar.** None more maintained than
  UserNobody14/tree-sitter-dart was found.

**Recovered trees**

- **A. File-level refusal**: no symbols or import edges from a tree with any
  ERROR or MISSING node.
- **B. Per-declaration refusal.** Fails: a member of a type whose header
  did not parse is re-parented outside any ERROR node (found in the investigation).
- **C. Emit everything** and rely on the `syntax_error` gap warning.

**Import resolution**

- **A. Provable cases only**: Java fully qualified type names declared by
  exactly one scanned file; Dart relative URIs and the own package's
  `package:` URIs (via the `name:` of the nearest `pubspec.yaml`); C#
  `using` never to a file.
- **B. Externals only**, no file edges.
- **C. Build-file aware** resolution (Maven/Gradle source sets, csproj
  references, pub dependencies): out of scope by decision.

## Decision

1. **Grammars: option A.** Register the audited Java and C# modules byte
   for byte from `spike/parsing/grammars/`, and the Dart module byte for
   byte from its reviewed `Grammar assets` artifact:

   | Language | Source pin | ABI | Module SHA-256 | Bytes | Toolchain |
   |---|---|---:|---|---:|---|
   | Java | tree-sitter/tree-sitter-java v0.23.5 | 14 | `bdafeee7af6963461de9498f22f9e0a1893a2196bb410f20941a55fab0e24e88` | 641,890 | wasitter v0.1.0 release, Zig 0.15.2 |
   | C# | tree-sitter/tree-sitter-c-sharp v0.23.5 | 15 | `9075c0442ff28ddd2d0c368994aad2b26de4d79f08df5a22383290ef46208bcf` | 5,572,871 | run C, WASI SDK 25.0 clang 19.1.5 (external scanner) |
   | Dart | UserNobody14/tree-sitter-dart `0038280b774b65c1b89ba7fec50624d8baed9fe1` (2026-10-18; developer decision O1 (b)) | 15 | `ff84ee67832448ba4187f05adb9c991c3bbd7e1fc4e8f5761f94c6daa22e302b` | 1,441,929 | `Grammar assets` run `43909359868`, WASI SDK 25.0 clang 19.1.5 (external scanner), as run C |

   All three are MIT-licensed (Dart: MIT source text, ISC package metadata,
   handled as in ADR 0007). The production loader verifies each digest
   before compilation. This amends ADR 0006's asset list and moves the
   compilation cache namespace to `wasitter-0.1.0-assets-v4`; the runtime
   stays wasitter v0.1.0 / wazero v1.10.1; no cgo, download or
   installation. The aggregate extractor version is bumped once the
   extractors land. Java's grant is already in `THIRD_PARTY_NOTICES.txt`;
   `c_sharp-LICENSES.txt`, `dart-LICENSES.txt` and the two build records
   are copied with the modules. Dart follows option B (developer decision
   O1, 2026-11-16): the superseded audited pin `be07cf7` (module
   `5812b5b4...`, 1,398,462 B) is not registered; the `0038280` module is
   built by the grammar CI workflow and reviewed per ADR 0007 before it is
   copied in (done in T021-B1, see the implementation notes).
2. **Recovered trees: option A** (row BE-2). The file keeps its file node
   and the existing `syntax_error` warning, which already marks it as a
   knowledge gap. Parse and extractor failures stay per-file diagnostics.
   Known clean misparses of the pinned Dart grammar on Dart 3.13 concise
   constructors are refused by guard rows DA-G1 and DA-G2. (With `0038280`
   the DA-G1 misparse no longer occurs and the DA-G2 shape is an ambiguity
   rather than a misparse; whether B4 keeps both guards is open, see the
   implementation notes.)
3. **Structure.** Level-4 symbol nodes, contained by their nearest emitted
   lexical owner:

   | Language | Types (kind) | Callables (kind) | Not emitted |
   |---|---|---|---|
   | Java | class, interface, enum, record, `@interface` (`annotation`); nested and local types | methods and annotation elements (`method`), constructors and compact constructors (`constructor`), Java 25 program-level methods | anonymous-class and enum-constant bodies, fields, initializers, enum constants, record components, modules |
   | C# | class, struct, interface, record (`record struct`: attribute `struct`), enum; `partial` attribute | methods, destructors, operators (`method`), constructors (`constructor`), local functions (`function`), properties and indexers (`property`) | delegates, events, fields, enum members, accessors, lambdas |
   | Dart | class (incl. `mixin class`, mixin applications), mixin, extension (anonymous: `extension on <T>`), extension type, enum | top-level functions, getters and setters (`function`), members (`method`, setters named `x=`), constructors (`constructor`, unnamed `new`), local functions (`function`) | typedefs, variables, enum values, function expressions |

   Spans are the declaration node (Java and C#: modifiers, annotations and
   attributes included, doc comments excluded); Dart members and top-level
   functions run from their preceding annotations to the end of the sibling
   `function_body`, or to the `;` of a bodyless declaration. Java packages and
   C# namespaces are not owners. Java/C# overload sets append
   `@overload(<parameter types>)`; C# explicit interface implementations
   append `@<interface>`; repeated names use the house `@2`/`@3` rule.
4. **Imports: option A.** Java JV-I2-I4 resolve to the one file declaring
   the fully qualified type name; ambiguous names give `ambiguous_import`;
   undeclared names under a repository package give `unresolved_import`;
   others are `java:` externals. C# usings never resolve to files: usings
   of repository namespaces produce nothing, others are `dotnet:`
   externals. Dart: `dart:` externals; relative URIs and own-package
   `package:` URIs resolve to scanned files (`unresolved_import` when
   missing); other repository packages produce nothing; unknown packages
   are `pub:` externals; every conditional alternative is followed without
   evaluation; `part` is an `imports` edge, `part of` none. The only
   build-file read is the top-level `name:` of scanned `pubspec.yaml` files.
5. **Precision labels.** Files keep `precision: "best_effort"`; nodes
   follow T020's representation if it adds one, otherwise carry none. T020
   must not label these languages `precise`.
6. Contract v1, the storage schema, the store interface, dependencies and
   the server/web are unchanged.

## Consequences

- The binary grows by about 7.6 MB (+14 %): measured linux/amd64
  +7,618,560 B, windows/amd64 +7,615,488 B, darwin/arm64 +7,678,112 B;
  C# accounts for 5.57 MB. Modules compile lazily per language, so
  repositories without these languages pay only the size; none of the 23
  pinned corpora has such a file. The namespace and version bumps invalidate
  existing compilation and fact caches once.
- Java, C# and Dart repositories get a navigable symbol map with exact
  spans and file-level dependency edges where provable, and honest gaps
  elsewhere (refused files, unresolved and ambiguous imports).
- The investigation measured that the Java rows agree with javac's parser on every
  declaration of two corpora (4,279 and 12,719 declarations).
- Newer syntax than the pinned grammars understand (C# 14 extension
  blocks, Java 25 module imports and flexible constructor bodies) turns
  whole files into BE-2 gaps until the grammars are bumped. Dart 3.12/3.13
  primary and concise constructors parse cleanly with `0038280`.
- The binary growth with all three modules, measured in T021-B1 with
  `-buildvcs=false` against `b8af787`: linux/amd64 +7,664,384 B,
  windows/amd64 +7,659,520 B, darwin/arm64 +7,727,712 B.

## Known gaps

- No C# or Dart oracle (no SDK available; open point O13); their rows rest
  on goldens, the corpus census, BE-2 and review.
- Row BE-8 is coarse where a refused file's text proves nothing: a failed
  file, or a refused one with a `\u` escape (or, in C#, a verbatim name
  after `namespace`), turns every would-be external import of its language
  into a warning. A refused C# file with block namespaces or without the
  keyword still hides every static and alias using (a global-namespace
  type may be the target); eShop keeps 4 such warnings (its four alias
  usings) after the T021-B5 narrowing; of the former 499, 400 are `dotnet:`
  externals again and 95 CS-I4 warnings (T021-F1).
- C# 14 extension blocks are refused only in the shape CS-G1 guards; a new
  clean misparse of newer C# is a precision stop.
- Silent misparses beyond the guarded ones (CS-G1; DA-G1, an untyped Dart
  method read as a constructor) are not excluded for forms the corpora do
  not contain; a new one is a precision stop. localsend uses no Dart
  3.12/3.13 constructor form, so DA-C2, DA-C3 and the guards rest on the
  fixture and the grammar's probes only.
- Dart primary constructors (Dart 3.12/3.13) are not emitted; their class is.
- `#if` branches and Dart conditional imports are indexed without
  evaluation (all alternatives).
- Java import resolution assumes a compiling repository (one declaration
  per fully qualified name); build-time generated classes stay unresolved.
- Java imports of types declared in scanned Kotlin, Groovy or Scala files
  (mixed Android or Spring projects): the inventory reads only `.java`
  files, so such an import becomes an `external:.#java:` node unless its
  name or a dotted prefix of it is a package a `.java` file declares
  (JV-I7). Not corrected in T021-F1: the C# analogue of
  CS-I4 for Java (a two-segment prefix of a declared package) would turn
  1,455 of spring-modulith's 3,680 JV-I8 imports (339 of 714 names, all
  under `org.springframework`) into warnings, because a Java package's
  first two segments name the organisation rather than the project.
- C# usings of namespaces declared only outside `.cs` files (Razor
  components, `.proto` files, source generators) are warnings when a
  repository namespace of two or more segments encloses them (CS-I4,
  T021-F1); under a one-segment prefix, or when no repository namespace
  shares their first two segments, they are still `dotnet:` externals.
  CS-I4 also turns third-party names that share a two-segment prefix with
  a repository namespace (eShop's `Microsoft.Extensions.*` next to its own
  `Microsoft.Extensions.Hosting` extension namespace) into warnings: a
  recall loss, not a wrong fact.
- Dart imports of other packages of the same repository (pub workspaces,
  path dependencies) produce no edge (O11).
- The source archive digests were not re-verified in the investigation or B1
  (`codeload.github.com` refused by the container proxy with HTTP 403,
  "GitHub access to this repository is not enabled"); they
  were verified in T005a, and the grammar CI workflow verifies each
  CI-built row's digest before compiling. The Dart `0038280` digest was
  computed by the developer outside the container and checked by the
  `Grammar assets` run that built the module.
- Cross-platform loading of the C# module has Windows (T005b) and
  linux/amd64 (the investigation) evidence; the `0038280` Dart module has linux/amd64
  evidence (B1). Windows loading runs in CI on main and `[ci windows]`
  pushes, macOS only in manually dispatched CI runs.
- `scripts/build-grammars.py`'s `check_paths` 3-byte patterns (ADR 0007
  review note) remain; this ADR builds nothing.

## Implementation notes

**T021-B1 (grammar registration, partial).** Java and C# are registered:
`wasitter-java.wasm`, `wasitter-c_sharp.wasm`, `c_sharp-LICENSES.txt` and
`c_sharp-build.json` copied byte for byte from `spike/parsing/grammars/`
into `internal/parse/grammars/`, their digests added to `SHA256SUMS` and
the loader's `checksums` map, and the compilation cache namespace moved to
`wasitter-0.1.0-assets-v4`. This amends ADR 0006's asset list and cache
namespace (as ADR 0028 did for v3). `internal/index` `supported()` is
unchanged, so neither language is parsed during indexing yet.

Binary size with Java and C# only (`CGO_ENABLED=0 go build ./cmd/graphvein`,
Go 1.27.1, against `b8af787`): linux/amd64 53,764,627 to 59,981,395 B
(+6,216,768); windows/amd64 54,091,776 to 60,307,968 B (+6,216,192);
darwin/arm64 51,699,970 to 57,974,594 B (+6,274,624). The two modules
total 6,214,761 B.

**T021-B1 (Dart).** The manifest's Dart row moved to `0038280` with the
developer-supplied source archive digest
`7d9f51b4ff20756a21935ce69a2386e6db1e2fda325835d41790e745270e0db8`
(`LICENSE` at `0038280` is identical to `grammars/dart-LICENSE.txt`;
`grammars/NOTICES.md` follows the pin). `Grammar assets` run `43909359868`
(commit `062c52c`) built the four CI grammars. Artifact review per ADR
0007: `sha256sum -c SHA256SUMS` passes for every file; `--check-paths`
finds no host path in any module; the HTML, Angular and C# modules are
byte-identical to the run C copies in `spike/parsing/grammars/` (Angular
and C# also to the production copies), so the pipeline is unchanged;
`manifest.yaml`, `NOTICES.md`, `UPSTREAM_NOTICES.txt`, `LLVM-LICENSE.txt`
and `WASI-SDK-NOTICES.txt` equal the committed files; `dart-build.json`
records the new source digest, ABI 15 and the same toolchain fields as
run C. `dart-LICENSES.txt` (the MIT `LICENSE` and the nested
`tree_sitter/LICENSE`) is byte-identical to the `be07cf7` run C copy.
`wasitter-dart.wasm` (1,441,929 B, SHA-256 `ff84ee67...e302b`),
`dart-LICENSES.txt` and `dart-build.json` are copied byte for byte into
`internal/parse/grammars/` and registered in `SHA256SUMS` and the loader's
`checksums` map, in set v4.

Probes against the built module (`TestDartModernConstructorShapes`): every
Dart 3.12/3.13 form parses without ERROR or MISSING. Primary constructors
are a `primary_constructor` field on `class_definition` /
`enum_declaration` (named ones carry a `name` field, `class const P(...)`
a `const_builtin`); `class Empty;` is a bodiless `class_definition`; the
primary constructor body `this : ...;` is a
`primary_constructor_body_signature`. `new named();` is a
`constructor_signature` with an anonymous `new` token and one `name`
field, so the DA-G1 shape (`function_signature` returning `new`) no longer
occurs; it differs from the classic `Classic();` only by that token.
`new();` / `const new();` have no name at all. `factory make()` is still a
`factory_constructor_signature` with one identifier, the same shape as the
unnamed `factory Classic()`: DA-G2's form is now valid Dart 3.13 that only
a comparison with the enclosing type's name can tell apart. Neither is a
silent misparse any more, but both are silent shape ambiguities for an
extractor written against `be07cf7`. Classic `const C.c()` now exposes its
identifiers as `name` fields (they were plain children). The localsend
re-census is identical to the `be07cf7` census (see
the T021 task record (internal)).

Binary size with Java, C# and Dart (`CGO_ENABLED=0 go build -buildvcs=false
./cmd/graphvein`, Go 1.27.1, both sides built the same way, against
`b8af787`): linux/amd64 53,764,243 to 61,428,627 B (+7,664,384, within the
7.7 MB gate); windows/amd64 54,093,824 to 61,753,344 B (+7,659,520);
darwin/arm64 51,699,970 to 59,427,682 B (+7,727,712). The three modules
total 7,656,690 B.


**T021-B2 (shared kit and Java).** `internal/lang/besteffort` holds the
shared rules: BE-2 over the detached tree (`Recovered`: an ERROR node or a
named zero-width node) plus `Refuse`, which the indexer also applies when
only the parser's syntax flag is set (an unnamed MISSING token is dropped
from the detached tree); the declaration forest with BE-4 containment, BE-6
overload and BE-5 repeated-name naming; the type-text and span helpers; and
the claim-contract table type. `internal/lang/java` implements rows JV-*;
its per-file payload is the new optional `facts.BestEffortFacts` (version
1: refused flag, package, importable type paths, import directives as
written), so linking re-resolves every importer from the cache. The
aggregate extractor version is extractors-38 (main held extractors-37; the
number is re-checked at merge). Java joins `supported()`; C# and Dart do
not yet. Choices made where the contract was silent, all recorded in
`docs/lang/best-effort.md`: a receiver parameter is not part of the BE-6
type list (it is not part of a Java signature); a compact canonical
constructor takes the record header's types; JV-T6 also covers local
classes in lambdas and initializers below a type (javac agrees); JV-I7
applies when the imported type name or any dotted prefix of it is a
scanned package; a type declared only in a refused file is invisible to
resolution (T021-B3 closes the consequence with row BE-8, below). The javac oracle (`scripts/oracle/java_decls.sh`) agrees on
kind, owner path, start and end for 43 of 43 fixture declarations, 4,279 of
4,279 on spring-modulith and 12,719 of 12,719 on commons-lang; the row
counts on both corpora equal the investigation's inventory.

**T021-B3 (C#, and the shared row BE-8).** Precision fix first: B2 let a
type declared only in a refused (BE-2) file fall through to a `java:`
external, labelling a repository type as an external dependency. New shared
row BE-8 (`besteffort.Hidden`, `besteffort.HiddenImport`): while the
repository has a file of the language that is refused (BE-2) or has no
valid payload because parsing or extraction failed (BE-3), an import or
using that would otherwise be external (JV-I8, CS-I2) is an
`unresolved_import` warning naming a hidden file, with no external node and
no edge. There is no JDK/BCL allowlist, since a hidden file could declare
anything. The Java fixture (two BE-2 files) loses its four `java:`
externals and five import edges to five warnings; a second root,
`testdata/fixtures/java-best-effort-external/`, keeps JV-I8 covered.

`internal/lang/csharp` implements rows CS-*. The payload gains C# fields
(`Namespaces`, `GlobalTypes`; per using `Alias`, `Scope`, `Prefixes`) in the
same `facts.BestEffortFacts` version 1; `besteffort.Decl` gains `Suffix`
for C# discriminators (`@IFoo`, `@static`, `` `N ``), which are part of the
segment that BE-6 groups by. C# joins `supported()`; extractors-38 is
unchanged on this branch (one bump per merge). Contract changes, all
refusals (no new claim), recorded in `docs/lang/best-effort.md` and for the
developer's review:

- **CS-G1** (precision stop S2): the pinned grammar parses a C# 14
  extension block `extension(string s) { public bool IsBlank() => ...; }`
  without an error as a `constructor_declaration` named `extension` whose
  members are local functions (the investigation had seen only error-producing
  variants). A constructor whose name differs from the enclosing type's
  name claims nothing and its subtree is skipped. Generic and
  receiver-only extension blocks and C# 14 compound assignment operators
  (`operator +=`) parse with errors (BE-2); C# 14 instance `operator ++()`,
  `partial` constructors and events and the `field` keyword parse
  correctly.
- **CS-I3**: a using inside a namespace (block, or after a file-scoped
  declaration) not written with `global::` may name a namespace relative to
  an enclosing one that a referenced assembly declares, so it claims
  nothing instead of `dotnet:<name as written>`.
- **CS-I1** widened: relative lookup inside namespaces; for `static` and
  alias usings any proper prefix (not only the parent) being a declared
  namespace, so nested types count (the investigation labelled eShop's
  `eShop.Basket.API.Grpc.Basket.BasketClient` external); a using inside a
  namespace; and global-namespace types (`GlobalTypes`, `Program` for
  top-level statements).
- **CS-I2** narrowed: alias targets that are not names (tuple, predefined,
  array or pointer types, C# 12) are D.

eShop census (real extractor and linker, scratch; not an oracle): every
symbol row equals the investigation's inventory (2,458 symbols); CS-I1 402 (+1, the
nested-type alias above), CS-I3 10 (from the investigation's CS-I2), BE-8 499 (all
remaining would-be CS-I2, since eShop has two BE-2 files), CS-I2 0, CS-G1 0.
No C# oracle (O13 (a)).

**T021-B4 (Dart, and the BE-8 refinement).** BE-8 first (decision, precision-safe): a refused file now hides only the names its raw
text proves it can declare (`besteffort.Scope`, `Occurrences`, `Hider`,
`Hiding`; `java.RefusedScope`, `csharp.RefusedScope`; the payload's
`Scoped`/`Scopes`, absent in older payloads, which keeps them hiding
everything). The scan counts every whole-word keyword, in comments and
strings too, so it only over-matches. Java: every `package <name>` found
hides names equal to or under it; no occurrence means the unnamed package,
whose types no import can name (JLS 7.5; javac 21 rejects `import C;`,
`import C.D;`, `import C.*;` and `import static C.m;` for a class `C` in the
unnamed package, checked in scratch), so such a file hides nothing. C#:
only when every `namespace` keyword is a file-scoped declaration
(`namespace X;`: at most one per file, before every type, never with a
block namespace; CS8954-CS8956, not compiler-checked here) does it hide
just names equal to, under or a dotted prefix of X; otherwise any name, as
before. A `\u` escape in the text, or a failed file, proves nothing. The
Java fixture's refused files now hide only `com.acme.secret.Vault`, so its
JV-I8 externals return; the C# fixture's refused file-scoped `Broken.cs`
hides only `Shop.Hidden` names, so its CS-I2 externals return; a new root
`testdata/fixtures/csharp-best-effort-hidden/` keeps the block-namespace
case. eShop: 0 of the 499 BE-8 warnings return (its gRPC `Basket.cs` has a
block namespace; `MauiProgram.cs`'s file-scoped `eShop.ClientApp` hides
none of them on its own).

`internal/lang/dart` implements rows DA-* for the `0038280` grammar,
replacing the investigation's DA-G1/DA-G2 guards (written for `be07cf7`) as decided in
T021-B1's open point, option (b): DA-C2 claims `constructor_signature` /
`constant_constructor_signature` with the anonymous `new` token (unnamed
`new` without a `name` field, else named), DA-C3 claims a factory with no
identifier (`new`) or with one identifier that differs from the enclosing
type's name (named); DA-C1 keeps the classic forms but requires the first
identifier to equal the type's name. The fixture covers the token check
(`new named()`) and the name comparison (`factory make()` against
`factory Classic()`). Precision stop S2 found while probing: `0038280`
parses an untyped method (`foo();`, `foo() {}`) without an error as a
`constructor_signature` named `foo`; the rewritten DA-G1 refuses every
constructor signature whose name does not match (subtree skipped), and
DA-G2 does the same for factories (not valid Dart). Other choices recorded
in `docs/lang/best-effort.md`: DA-S includes a top-level `external` keyword
(a program token outside the signature) and extends a bodyless top-level
signature over its `;`; anonymous extensions keep one space between two
word characters of the on-type (`extension on void Function()`); DA-I4 also
covers an unknown pubspec name (a `package:` URI that could name it claims
nothing and no `pub:` external is emitted); self-imports give no edge;
query, fragment, adjacent-string and triple-quoted URIs are DA-I8. BE-8
does not apply to Dart. The payload gains `BestEffortImport.Directive`;
`dart.PackageNames` reads each scanned `pubspec.yaml`'s first unindented
`name:` line at link time. Dart joins `bestEffort()` (so `supported()`, the
cache check and BE-2 apply); extractors-38 is unchanged on this branch.
localsend census (real extractor and linker, scratch; not an oracle): every
row equals the investigation's inventory, and the 34,221 declarations equal the investigation's
prototype after its naming (constructors `new`, setters `=`), except 6,346
bodyless spans that now include their `;` (DA-S), 7 anonymous-extension
names (the investigation kept only the first word of the on-type) and 5 repeated
local functions that BE-5 numbers. No Dart oracle (O13 (a)).

**T021-B5 (corpora, gates, and BE-8 for block-namespace C# files).** BE-8
first (decision, B4's open point option (b), precision-safe):
a refused C# file with block namespaces (or a mix), or without any
`namespace` keyword, now gets a `Global` scope (`besteffort.Scope.Global`,
payload `BestEffortFacts.Global`, omitted when false so older payloads stay
hide-all): it hides plain usings equal to, under or a dotted prefix of a
namespace name found lexically (a nested block's composite name is under
its outer name) and every static and alias using, which may name a
global-namespace type. The rule rests on ECMA-334 §7.8.1 (a plain using's
name shall refer to a namespace; CS0138) and §14.2-14.3 (only namespace
declarations declare namespaces; other members are in the global
namespace); no compiler was run. A `\u` escape, a verbatim name after
`namespace` and failed files still hide everything. eShop: CS-I2 0 to 495,
BE-8 499 to 4 (its four alias usings). Corpora: `scripts/cloud-corpus.sh`
pins spring-modulith, eShop, localsend and commons-lang (O13);
`GRAPHVEIN_BESTEFFORT_CENSUS_CORPUS`-gated census tests per language equal
the inventory; `TestJavaDeclsOracleCorpora` (`GRAPHVEIN_JAVA_ORACLE_CORPUS`)
runs the javac oracle on both Java corpora: 4,279 and 12,719 declarations,
0 disagreements. Gates against the merge base `a2979584`: the 23 existing
corpora's snapshots and SQLite sizes are identical apart from the version
string; Langflow, kaneo, umami and seerr fresh and warm medians are within
10 %; the self-index adds only the Java, C# and Dart files' records. Binary
growth on linux/amd64 is +7,876,270 B (the three modules plus about 0.2 MB
of extractor code), above the 7.7 MB gate read as 7,700,000 B and below it
read as 7.7 MiB (open point in the T021 task record (internal)). localsend's
generated Dart is 78 % of its nodes and 73 % of its store (O9 (a) kept).

**T021-F1 (review fixes).** Two contract changes chosen after
the T021 review (the T021 task record (internal)). C#: new
row CS-I4 (option (b)). A using that CS-I1 and CS-I3 do not
match, and that has a proper dotted prefix of two or more segments which is
a declared namespace or a prefix of one, is an `unresolved_import` warning
naming that prefix. It is no longer a `dotnet:` external. The repository may
declare that namespace outside `.cs` files: Razor components get theirs from
the project's root namespace and folder (eShop's
`using eShop.WebApp.Components;`), and `.proto` files and source generators
add theirs at build time. CS-I4 takes precedence over BE-8. On eShop, 33 of
the 146 `dotnet:` nodes become warnings (95 usings; 3,644 to 3,611 nodes,
3,193 to 3,098 edges, 18 to 113 diagnostics). Java's JV-I7 already covers
the case where a declared package encloses the name (Known gaps). Dart:
DA-C1, DA-C2 and DA-G1 now look at the token before `new`. With
a `.` before it, `C.new()` and `const C.new()` are the unnamed constructor
`C.new` (DA-C1), the identity `C()` gets; `Other.new()` is refused (DA-G1).
Only a `new` without a `.` before it is the concise form (DA-C2). Before
this change `C.new()` was named `C.C`. The binary size gate was raised to
8.0 MB (8,000,000 B): the investigation's 7.7 MB estimate covered
the grammar modules only and left out about 212 KB of extractor code, and
no part of the contract could be dropped. Growth at the F1 head is in
the T021 task record (internal).

## Review notes (2026-11-16)

Accepted after delegated review on 2026-11-16.

- **Developer decisions:** O2-O14 took their recommended defaults (2026-11-16); O1 moved the Dart grammar to `0038280` (Dart 3.12/3.13 syntax). The developer supplied the codeload archive digests (the session cannot download third-party archives) and allowed the artifact host; the grammar workflow re-verified the digest and built the module.
- **Steps:** B1 grammar registration (Java and C# copied byte for byte from the audited spike modules, Dart built by the grammar workflow and verified against SHA256SUMS and its build record), B2 shared kit and Java, B3 C#, B4 Dart, B5 corpora and gates, R1 independent review (the T021 task record (internal)), F1 review fixes.
- **Precision corrections during the task:** BE-8 (a refused or failed file hides the imports it may declare instead of letting them become externals), narrowed by the refused file's lexically provable package or namespace (JLS 7.5; ECMA-334 §7.8.1, §14.2-14.3 and Microsoft's CS0138 page: a plain using names a namespace); CS-G1 (a C# 14 extension block parses silently as a constructor); DA-G1/DA-G2 and the untyped-method guard on the 0038280 grammar; CS-I4 (usings under a declared namespace prefix that no `.cs` file declares, such as Razor components, become `unresolved_import`, never `dotnet:`); Dart `C.new()` named `new`.
- **Review (R1):** class A findings (Razor namespaces as externals, `C.new()` naming) fixed; class B: types declared only in Kotlin, Groovy or Scala recorded as a known gap, a row test strengthened, a binary number re-measured. Java 21-25 probes agree with javac (39/39, 20/20); C# 12-14 and Dart 3.x probes found no other wrong claim.
- **Results:** oracle (javac 21 parse-only, kind, owner path, start and end): fixture 43/43, spring-modulith 4,279/4,279, commons-lang 12,719/12,719, 0 disagreements (overload suffixes and import resolution not oracle-checked). No C# or Dart oracle (O13). Census tests equal the inventory on all four corpora (eShop after the CS-I4 correction: CS-I2 400, CS-I4 95, BE-8 4). The 23 existing corpora's snapshots and store sizes are unchanged apart from the version; timing within 10%; binary +7,884,206 B on linux/amd64 (gate raised from 7.7 MB to 8.0 MB: the investigation estimate covered the grammar modules only and omitted about 212 KB of extractor code). CI run 43950133496 on 03a528a: Windows and postgres green; postgres raw log 0 FAIL, 168 `/postgres` PASS, only environment-gated skips (the five new opt-in census/oracle tests among them).
- **Coverage per corpus:** spring-modulith 6,026 nodes, eShop 3,611, localsend 36,666 (generated `*.g.dart` 73% of its store, O9), commons-lang 13,789; every file and node `best_effort`.
- **Known gaps and after-v1 items:** precise Java/C#/Dart (calls, cross-file symbols, build files) is after v1; Dart primary constructors not emitted; types declared in other JVM languages; Razor namespaces under a one-segment root stay externals; re-probe modern forms at each grammar bump.
