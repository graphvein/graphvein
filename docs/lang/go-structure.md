# Go structure: claim contract

Status: contract of the T020 implementation (2026-11-16, `extractors-39`),
revised by the review fixes (step T020-S1F: row GS-F3, forms
`lone_cr` and `malformed_import_path`, the shared package reader, union
semantics stated), ADR 0068. The rows are kept 1:1 with `structureRows` in
`internal/lang/golang/structure.go` by `TestGoStructureContract`, which
also checks that the fixture `testdata/fixtures/go-structure` reaches every
row and the default branch. `TestGoStructureOracle` compares every claimed
fact with an oracle built on the Go standard library, reading the raw bytes
(go/parser, go/ast, and go/build's `Context.ImportDir` per port, tag subset
and cgo setting), and `TestGoStructureGoList` (opt-in) compares the package
reader and the import edges with `go list` on the fixture. Rows change only
through a revision of task T020 (or a later task) with a version bump.
Inventory and the investigation results: the T020 task record (internal). Label
rule: row PL-L1 of `docs/extract/precision.md` (added
after the independent review and its fixes): every Go file, test files
included, is labelled `precise`, except a file whose structure claims
nothing (rows GS-F2 and GS-F3, a file Graphvein could not parse or
extract, or one dropped by validation), which the indexer stores as
`best_effort` and the server serves with reason `structure_refused`. The
label covers the rows below, never calls (`code_calls`, row D).

Scope (T020 decision O5 (a)): Go files, top-level declarations and import
specs, as emitted by `internal/lang/golang` (`Extract`, `Link`). Calls are
a separate scope with no contract and stay Best effort. GORM model facts in
Go files are row PL-F7's scope, not this one.

**Every form no row below names goes to the single default branch D: it
claims nothing (no node, no edge). Forms inside the scope that D receives
are counted (one `go_structure_unread` diagnostic per file); forms outside
the scope (listed under "Not modelled") are not counted.**

Input text: every source is read as the scanner reads it, with CRLF and
lone CR as LF (contract v1), except where a row says otherwise. Go reads a
lone CR (a CR not followed by LF) as white space, not as a line end, so a
file whose raw bytes hold one is not read at all beyond its file node (row
GS-F3); for every other file the scanner's text and the bytes Go reads
have the same lines.

Status column: **oracle** = `TestGoStructureOracle` reports 0
disagreements on every compared fact of the row on the fixture, gotify,
realworld-gin and the self-index (the Go implementation numbers in ADR 0068).

## Module discovery (input to the import rows)

| row | form | claim | refusal (to D) | status |
|---|---|---|---|---|
| GS-M1 | a scanned `go.mod` with a `module` directive | the importer's module is the nearest scanned go.mod at or above its directory; its path is the directive's argument | a go.mod outside the scanned inventory is never read; a module path that is not a valid import path (the block form `module (`) resolves no import, though its go.mod still bounds the module; `replace`, `require`, `exclude`, go.work and vendor directories claim nothing | oracle |

## Files

| row | form | facts claimed | refusal (to D) | status |
|---|---|---|---|---|
| GS-F1 | a scanned file with extension `.go` (any build constraint, test file, external test package, generated, cgo, inside `testdata`) | file record and level-3 `file` node, `language: go`; the file exists at this path with this content hash | - (the scanner's ignore rules decide the inventory) | oracle |
| GS-F2 | a file go/parser rejects | the file node only, labelled `best_effort` (plus the indexer's `syntax_error` warning when tree-sitter also reports one) | every declaration and import tree-sitter recovered goes to D and is counted (`syntax_error`); the file's calls are dropped with its declarations (open point O-S1-1, fallback (a)) | oracle |
| GS-F3 | a `.go` file whose raw bytes hold a CR not followed by LF | the file node only, labelled `best_effort` | every declaration and import tree-sitter read in the scanner's text goes to D and is counted (`lone_cr`, a GS-F2 count included); the file's calls are dropped; a directory holding such a file is no decided package (GS-I5) | oracle |

## Declarations (top level only)

Spans are contract v1 spans (1-based lines, 0-based byte columns, end
exclusive) of the go/ast node named in the row, read with `//line`
directives ignored; the content hash covers exactly the span. Every row
emits one `contains` edge from the file node (`provenance: fact`,
confidence 1, evidence = the span). Methods are contained by their file,
not by the receiver type.

| row | form | node kind, qualified name | span (go/ast) | refusal (to D) | status |
|---|---|---|---|---|---|
| GS-D1 | `func Name(...)` without receiver, `Name` not `init` or `_`, generic or not, with or without body | `function`, `Name` | `FuncDecl` (from `func`, doc comment excluded, to the closing brace or the signature end) | GS-D6 | oracle |
| GS-D2 | `func init()` | `function`, `init` for the first in the file, then `init@2`, `init@3` in source order | `FuncDecl` | - | oracle |
| GS-D3 | `func _()` | `function`, `_` for the first in the file, then `_@2`, `_@3` in source order (as GS-D2; open point O-S1-6 (a)) | `FuncDecl` | - | oracle |
| GS-D4 | method whose receiver list is one parameter declaration with at most one name, of type `T`, `*T`, `T[P, ...]` or `*T[P, ...]` (T an identifier) | `method`, `T.Name` (the receiver as written; an alias is not resolved) | `FuncDecl` | GS-D6; any other receiver (parenthesised `(T)`, `(*T)`, qualified `pkg.T`, `**T`, variadic, several declarations, one declaration with several names `(a, b T)`, which the type checker rejects) goes to D, counted (`receiver`) | oracle |
| GS-D5 | type spec, standalone or in a `type ( ... )` group, generic or not | `struct` when the type is a struct type, `interface` when an interface type, `type_alias` when written `Name = ...` (generic aliases included), otherwise `type`; qualified name `Name` | `TypeSpec` (name to end of type; the `type` keyword and group parentheses excluded) | GS-D6 | oracle |
| GS-D6 | two or more declarations in one file with the same kind and qualified name (other than GS-D2, GS-D3; invalid Go) | none: every one goes to D, counted (`redeclaration`), with its contains edge and its calls | - | - | oracle |
| GS-D7 | exported name (`token.IsExported`) in a package-level `var` spec | `variable`, the name; `attributes.exported: true` | `ValueSpec` (first name to end of last value or type), shared by every name of the spec | GS-D6 | oracle |
| GS-D8 | exported name in a package-level `const` spec (with or without value, `iota`) | `constant`, the name; `attributes.exported: true` | `ValueSpec`, as GS-D7 | GS-D6 | oracle |

Not modelled (outside the scope; claim nothing, not counted; open point
O-S1-8 (a)): unexported package-level `var`/`const` names, blank `_`
values, the package clause, declarations inside function bodies (types,
variables, constants, function literals), struct fields, interface
methods, type parameters, embedded fields, comments and directives
(`//go:build`, `//go:generate`, `//go:linkname`), the cgo preamble.

## Imports

Each import spec (plain, aliased, dot, blank; grouped or not) of a file
go/parser accepts (and without a lone CR) is one claim site; the evidence span of each edge is the
`ImportSpec` (alias and path). Several specs naming the same package merge
into one edge with several evidence entries.

**Import paths**: a path that is not a valid import path by the go
command's rules (`golang.org/x/mod/module.CheckImportPath`, which
`cmd/go` applies to every import before resolving it: non-empty
slash-separated elements of ASCII letters, digits and `- . _ ~ +`, no
leading dash, no element of dots only or ending in a dot, no double or
trailing slash, no reserved Windows name, no `~` and digits suffix) claims
nothing: no row reads it, it goes to D, counted (`malformed_import_path`). The rules are transcribed in
`internal/lang/golang/modules.go` (`CheckImportPath`); x/mod is not a
dependency. A module path that fails the same check (the block form
`module (`, for one) resolves no import: its go.mod still bounds the
module (GS-M1), and every import of its files is GS-I4.

**Packages** (GS-I1, GS-I2, GS-I5): the package in a directory is the one
go/build's `Context.Import` reads (`golang.Package`, shared by import
edges, the call bindings of `Link`, and the GORM front end's package-name
bindings and package files, extractors-44): names starting with `_` or `.` are never read; a file no
configuration matches is ignored; a matched file whose package clause is
`documentation` is ignored; a matched `_test.go` file
whose clause ends in `_test` (other than the package's own name so far)
is an external test file; a matched file importing `"C"` is a package
file only in configurations with `cgo` set, that is, `cgo` is ANDed into
its constraint. go/build's errors refuse the directory:
matched files naming different packages, test files included (a foreign
clause in a `_test.go` file breaks every importer); a matched `_test.go`
file importing `"C"`; a constraint that cannot be read. Graphvein also
refuses a directory with a matched file go/parser rejects, a file with a
lone CR (GS-F3) or more than 12 custom tags in one file.

**Build configurations** (GS-I1, GS-I5): a file is selected when some
configuration builds it: a port of `go tool dist list` (Go 1.27.1; the
list is in the code), the gc compiler (`gc` set, `gccgo` never), release
tags `go1.1` to `go1.27` (no newer tag, so the result does not depend on
the toolchain that built Graphvein), `unix` on the Unix ports, `ignore`
never set, and any value of every other tag (`cgo`, custom tags; at most
12 per file). The file name rules are go/build's: names starting with `_`
or `.` are never selected; `_GOOS`, `_GOARCH` and `_GOOS_GOARCH` suffixes
(before `_test`) restrict the ports (`linux` also matches android,
`solaris` illumos, `darwin` ios). The constraint is read as go/build reads
the file header (a byte order mark is skipped): the `//go:build` line of
the leading comments, or else the `// +build` lines followed by a blank
line. The extractor records the header lines in `facts.GoFacts.Build`.

**Union semantics**: the configurations are taken
separately for the importer and for the target. An import edge claims
"this file imports some file of the package in some configuration", not
that one build holds both: `a_linux.go` gets an edge to
`lib/os_windows.go` when `lib` holds one. A directory whose files name
different packages in different configurations is refused as one with
several packages.

**Code as written** (GS-I1 note; decision after the S1R
fixes, consistent with the developer's 2026-11-09 decision that code is
shown as written without claiming it compiles): an import edge states
which files the import spec resolves to, not that the program builds. Two
go command errors therefore keep their edges: an import into a package on
an import cycle (only a file importing its own package is refused,
`self_import`), and an import of another tree's `internal/` package. Each
edge names the files the path resolves to as written, so neither is a
wrong fact; both are listed in ADR 0068 "Known gaps".

| row | form | facts claimed | refusal (to D) | status |
|---|---|---|---|---|
| GS-I1 | a valid import path equal to or under the importer's module path (GS-M1), no go.mod between the module root and the directory, the package decided (see Packages) | `imports` edges file → file to each scanned file of the imported package (go/build's GoFiles and CgoFiles, joined over the configurations; union semantics above); when the importer is an external test file of the same directory (`package <name>_test` in a `_test.go` file), also the package's internal test files (TestGoFiles: never `<name>_test` files, never the importer) | GS-I2, GS-I3, GS-I5 | oracle |
| GS-I2 | as GS-I1 but the package has no non-test file in any configuration (only test files, only `package documentation` files, only cgo files whose constraint excludes cgo, no Go file) | no edge; `unresolved_import` warning | - | oracle |
| GS-I3 | path under the module path, but the directory lies inside a nested module (a scanned go.mod between the module root and the directory) | the GS-I4 edge to `external:.#go:<path>` only; the in-repo directory goes to D, counted (`nested_module_import`; open point O-S1-7 (a)) | - | oracle |
| GS-I4 | any other path (standard library, third-party module, another in-repo module reached through `replace` or go.work, `C`) | `imports` edge to `external:.#go:<path>` (`ecosystem: go`, `name`: the import path): the file imports a package with this path; nothing about where it lives | - | oracle (a `replace`d in-repo module stays external: under-resolution, not a wrong fact) |
| GS-I5 | as GS-I1 but the package is refused (see Packages: a go/build error such as several packages, test files included, or cgo in a test file; a constraint that cannot be read; a matched file go/parser rejects or without a package clause; a lone-CR file), or the directory holds `package main` and the importer is in another directory (the go command: "is a program, not an importable package"), or the importer is in the directory and is not an external test file | none: the import goes to D, counted (`undecided_package_import`, or `self_import` for the importer in the directory); the import binds no package name for calls | - | oracle |

## Default branch D

Claims nothing. Counted per file when the form is in the scope, in one
`info` diagnostic `go_structure_unread` (open point O-S1-4 (a)) whose
message ends with the count per form, sorted: `(form=n, form=n)`. Forms:

- `syntax_error`: declarations and imports tree-sitter recovered in a file
  go/parser rejects (GS-F2); a rejected file counts no other form.
- `lone_cr`: declarations and imports of a file whose raw bytes hold a
  lone CR (GS-F3); such a file counts no other form.
- `malformed_import_path`: imports whose path is not a valid import path
  (see Import paths).
- `receiver`: methods whose receiver GS-D4 does not read.
- `redeclaration`: every declaration of a GS-D6 group.
- `nested_module_import`: imports into a nested module's directory (GS-I3).
- `undecided_package_import`, `self_import`: imports GS-I5 refuses.
- `payload_missing`: imports of a file whose Go payload capture failed
  (`model_syntax_capture_failed`); they claim no edge, and the next run
  retries the file.

The counts of the extractor's forms are stored in `facts.GoFacts.Unread`
(cached with the file's facts); the linker adds its own and emits the
diagnostic.

## Precision stops of the investigation (all closed in the implementation)

| stop | row | observed in the investigation | The implementation change | oracle after |
|---|---|---|---|---|
| PS-S1-1 | GS-I1 | I-1 on gotify (164), realworld-gin (5), self (4112) | `ImportFiles`: another directory's test files are never targets | 0 |
| PS-S1-2 | GS-I1 | I-2 on gotify (5, 3 self edges), self (641, 33 self edges) | an external test importer sees only the package's own test files, never itself | 0 |
| PS-S1-3 | GS-I1 | I-3 on the probe | build constraints, file name ports, `_`/`.` names and package clauses filter targets | 0 |
| PS-S1-4 | GS-I3 | I-4 on the probe | nested-module directories go to D (external edge kept) | 0 |
| PS-S1-5 | GS-D3, GS-D6 | D-1, D-3 on the probe | `_@2` suffixes; redeclarations go to D | 0 |
| PS-S1-6 | GS-F2 | D-4 on the probe | a file go/parser rejects claims no declaration or import | 0 |
