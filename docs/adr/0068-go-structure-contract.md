# ADR 0068 - Go structural claim contract: files, declarations, imports

**Status:** Accepted (delegated review, 2026-11-16)

## Context

T020 (ADR 0066) labels a language's structural scope (files, top-level
declarations, imports) `precise` only through a rule-table row whose
evidence includes a claim contract with a 1:1 test, goldens, corpus
checks, an oracle where cheap and an independent review. The T020 investigation
(the T020 task record (internal)) inventoried every Go form the
extractor (`internal/lang/golang`) emits and compared it with an oracle
built on the Go standard library (`scripts/oracle/go_structure/main.go`).

Declarations agreed on every compared fact (9330 of 9330 on gotify,
realworld-gin and the self-index). Imports did not: 4927 of 13136
in-module `imports` file edges were wrong (gotify 169, realworld-gin 5,
self 4753). `Link` used `PackageFiles` for import edges, which gives a test
importer every `_test.go` file of the imported directory (class I-1: test
files of another directory's package; I-2: an external test package's
sibling `x_test` files and itself) and ignores build constraints, `_`/`.`
file names and package clauses (I-3). The probe also showed edges into a
nested module (I-4), duplicate identities for `func _` and redeclarations
(D-1, D-3), receivers dropped silently (D-2) and declarations recovered by
tree-sitter in files go/parser rejects (D-4). Precision stops PS-S1-1 to
PS-S1-6 hold the PL-L1 flip until each is fixed and the oracle reports 0.

## Options considered

1. **Narrow the scope**: take imports out of the Go scope (O-S1-2 (b)).
   Keeps the code; loses the most useful structural fact.
2. **Fix the edges in Link with the go command's package rules**, using the
   build constraints captured at extraction time (O-S1-3 (a): in
   `facts.GoFacts`) or passing sources to `golang.Link` (O-S1-3 (b): an
   index call change).
3. **Use `go list`** at index time. Needs the module cache or the network
   for third-party imports, runs the go toolchain on user code's metadata
   and ties the result to the installed toolchain. Rejected (as in the investigation).

For files go/parser rejects (O-S1-1): (a) claim no declaration or import;
(b) keep the facts and drop the file label to `best_effort` (needs the file
label decided after parsing: `precision.FileLabel`, the scanner, the
indexer and the server's stored-label check change); (c) keep only
declarations without an error node (no oracle can check recovered trees).

## Decision

Option 2 with O-S1-3 (a), and every decision of the task
file's "Decisions (2026-11-16, before S1B)" (O-S1-1 fallback (a), O-S1-2
(a), O-S1-4 (a), O-S1-6 (a), O-S1-7 (a), O-S1-8 (a)). The contract is
`docs/lang/go-structure.md`, rows GS-M1, GS-F1, GS-F2, GS-D1..GS-D8,
GS-I1..GS-I5 and the default branch D, mirrored by `structureRows`
(`internal/lang/golang/structure.go`).

- **Import edges** use the new `golang.ImportFiles` (GS-I1, GS-I2, GS-I5).
  Calls and GORM keep `PackageFiles`, so call resolution and PL-F7 are
  untouched. The file selection is the go command's: build constraints
  read as go/build reads the header (stored as `facts.GoFacts.Build`), a
  file selected when some build configuration builds it (a fixed port list
  of Go 1.27.1, the gc compiler, release tags up to go1.27, `ignore` never
  set, any value of other tags), go/build's file name ports, `_`/`.` names,
  the package clause; another directory's test files never; an external
  test file of the same directory also reads the package's internal test
  files, never itself. A package that cannot be decided (unreadable
  constraint, several packages, a self import) goes to D.
- **Nested modules** (GS-I3): the import keeps its `external:.#go:<path>`
  edge; the in-repo directory goes to D. `replace`d in-repo modules stay
  external (under-resolution), as before.
- **Declarations**: `func _` gets `_@2`, `_@3` like `init` (GS-D3); every
  declaration of a redeclared kind and qualified name goes to D with its
  contains edge and calls (GS-D6); receivers other than `T`, `*T`, `T[...]`,
  `*T[...]` go to D, counted (GS-D4).
- **Files go/parser rejects** (GS-F2): the extractor runs go/parser on the
  source (standard library, no cgo, no network); a rejected file claims no
  declaration and no import edge, its calls are dropped, and its imports
  stay in the facts for GORM. O-S1-1 (b) was not practical in this unit:
  the file label is decided by the scanner before parsing, and the server
  checks a stored label against `precision.FileLabel(language)`, so a
  per-file downgrade needs the precision API, the indexer and the server
  (a task revision); it is moot until a PL-L1 row exists. Fallback (a) is
  precision-safe and checkable by the oracle now.
- **Default branch count** (O-S1-4 (a)): one `info` diagnostic
  `go_structure_unread` per file with the count per form. Extractor forms
  are stored in `facts.GoFacts.Unread` (cached); the linker adds its own.
- **Oracle**: `TestGoStructureOracle` runs scan, parse, `Extract` and
  `Link` in process and compares declarations (kind, identity, name, span,
  hash, contains edge), import evidence spans and target sets (file set or
  external name), `unresolved_import` warnings and default-branch counts
  per form with go/parser, go/ast and go/build's `MatchFile` (every port
  of its own port list, every subset of the header's custom tags). Default
  on the fixture; opt-in on gotify and realworld-gin
  (`GRAPHVEIN_GO_STRUCTURE_ORACLE_CORPUS`) and the self-index
  (`GRAPHVEIN_GO_STRUCTURE_ORACLE_SELF=1`).
- Extractor version `extractors-39` (T021 holds `extractors-38`; the
  number is re-checked when the branches merge; re-checked at the T021
  merge: one over main's `extractors-38`);
  `facts.GoModelsVersion` 2.

No rule-table row is added and no label changes here: PL-L1 follows the
independent review (added, below).

### Review fixes (S1R)

The independent review (the T020 task record (internal)) found four
classes of confident wrong facts the oracle could not see because it
shared the code's assumptions (A1..A4), packages the go command rejects
that still got edges (B2), wrong call edges from the package-name binding
(B3), no `code_calls` label (B4) and an oracle that was not independent
(B1). Each is fixed as a class rule, still at `extractors-39` (unreleased):

- **Raw bytes (A1, row GS-F3).** The scanner exposes `scan.File.LoneCR`
  (a CR not followed by LF in the raw bytes, which the normalised text and
  the content hash cannot show). The indexer applies
  `golang.RefuseLoneCR` to such a Go file: the file node only, every
  declaration and import to D (`lone_cr`), calls dropped, no import edge,
  and its directory is no decided package. Modelling Go's reading of a
  lone CR (white space, so a `//` comment runs past it) was rejected: the
  scanner, the spans and the hashes all use the normalised text.
  `facts.GoFacts.LoneCR` is cached, and the cache is reused only when it
  equals the scanned bit.
- **Import paths (A2).** `golang.CheckImportPath` transcribes
  `golang.org/x/mod/module.CheckImportPath` (Go 1.27.1's vendored copy);
  x/mod is not a dependency and is not added. A malformed path claims
  nothing (`malformed_import_path`), in-module or not, and
  `ModuleImportDir` refuses it, so `path.Join` never cleans `..`, `.` or
  `//` into a directory. A module path that fails the check (the block form
  `module (`, C4) resolves nothing; its go.mod still bounds the module.
- **One package reader (A3, A4, B2, B3).** `golang.Package(dir, files)` is
  go/build's `Context.Import` over the configuration union: `_`/`.` names
  skipped, `package documentation` files dropped before the package is
  decided, external test files recognised as go/build does, `cgo` ANDed
  into the constraint of a file importing `"C"`, and every go/build error
  (several packages, test files included; cgo in a test file; an
  unreadable constraint) a refusal, plus Graphvein's own (a matched file
  go/parser rejects, a lone-CR file, more than 12 custom tags).
  `ImportPackage` adds the go command's "is a program" rule (a `package
  main` directory imported from another directory). Import edges,
  `Link`'s import bindings and same-package call resolution
  (`OwnPackageFiles`) and the GORM front end's package-name binding all
  read it; a refused package binds nothing. This removes the confirmed
  wrong call edge (an ignored `package main` generator sorting first) and
  calls into nested modules (now external bindings, as their imports are).
- **`code_calls` label (B4, O-S1-5 (a)).** The server labels code `calls`
  edges with the family `code_calls` (row D) on `/flow`
  (`code_calls_precision`), on `/source` of a function or method and on
  change items of `calls` edges; the UI shows the chip there.
  `TestCodeCallsNeverPrecise` stands in a precise file row and checks that
  no call label follows it.
- **Oracle (B1).** `TestGoStructureOracle` now reads every scanned file's
  raw bytes from disk and decides packages with go/build's
  `Context.ImportDir` once per port, per subset of go/build's own
  `AllTags` and with cgo on and off, over the scanned inventory; it checks
  import paths with its own transcription of the x/mod rules and the "is a
  program" rule. `TestGoStructureGoList` (opt-in,
  `GRAPHVEIN_GO_STRUCTURE_GOLIST=1`) compares `golang.Package` and the
  import edges with `go list -e -deps -json` on the fixture, offline, for
  every port of `go tool dist list`, cgo on and off and every tag subset,
  and checks `goPorts` against `go tool dist list`.
- **Receivers (C1).** `func (a, b T) M()` goes to D (`receiver`).
- **Union semantics (C2)** are stated in the contract: an import edge
  claims "imports some file of the package in some configuration".

## Consequences

- Corpus and self-index snapshots lose exactly the wrong import edges
  (Review notes); nothing else changes except the version string.
- Go files are parsed twice (tree-sitter and go/parser); the cost is small
  next to tree-sitter's.
- The import edges depend on a fixed port list and release tag bound in the
  code, not on the toolchain that built Graphvein; a new Go release needs a
  code change and a version bump to follow it.
- A rejected file's declarations no longer appear in the graph, and its
  calls no longer count in `unresolved_calls`.

## Known gaps

- Calls stay Best effort and have no contract; every view showing code
  `calls` edges next to a file label carries the `code_calls` label (done
  in the S1R fixes). `/api/v0/precision` does not count `calls` edges as a
  default family yet (it would need a store query by edge kind).
- Before the S1R fixes, call bindings and GORM's import bindings took a
  package's name from the first non-test file of the directory, so a
  `//go:build ignore` `package main` generator sorting first gave a wrong
  `calls` edge with confidence 1 (`lib/a_gen.go` before
  `lib/lib.go`, `l.F()` bound to the generator). Fixed by `golang.Package`;
  calls still have no contract and stay Best effort.
- GORM's `packageFiles` ignored build selection (any read file of the
  directory with the package name). Closed (2026-11-17, extractors-44):
  GORM reads exactly `golang.Package`'s files and a refused directory is
  row D (`docs/extract/schema-gorm.md`, fixture `gorm-models/buildsel/`).
- Before the S1R fixes, a lone CR was read as a line end everywhere, so a
  `//` comment ending at a lone CR hid nothing from Graphvein while Go saw
  the code after it as comment (`// note\rfunc Hidden() {}` gave
  a `Hidden` declaration Go does not have), and a file Go rejects was read
  as valid. Fixed by row GS-F3 (the whole file to D).
- Free tags take any value, including impossible ones (`amd64.v5`, an
  unknown `goexperiment.*`, `cgo` on a port without cgo support), so such
  files are over-selected. Contrived.
- An import of `<module>/vendor/x` is unresolved (the scanner skips
  `vendor/`), as the go command reports; the oracle now reads the
  scanned inventory, so it agrees.
- Import cycles: the go command rejects every package on an import cycle
  and its importers; only a file importing its own package is refused
  (`self_import`). Edges into a package on a longer cycle remain.
- `internal/` visibility: an import of another tree's `internal` package
  is an error for the go command but still gets edges.
- Both are kept on purpose (decision, 2026-11-16, after the
  S1R fixes; consistent with the developer's 2026-11-09 decision that code
  is shown as written without claiming it compiles): each such edge names
  the files the import spec resolves to as written, so it is not a wrong
  fact; the edge does not claim the program builds. Recorded in
  `docs/lang/go-structure.md` (GS-I1 note). No code change.
- A go.mod without a `module` directive bounds no module (the outer module
  claims its directory); the go command refuses such a go.mod.
- A file outside the scanned inventory (ignored by `.gitignore`) is never
  read, though the go command builds it; it can hide a package error.
- Freshness compares normalised content hashes, so a change of line
  endings only (LF to lone CR) does not mark a file changed until the next
  full index (the fact cache itself checks the lone-CR bit).
- `replace` and go.work modules stay external (O-S1-7 (b) is a later
  contract change).
- A go.mod outside the scanned inventory is never read (GS-M1).
- gccgo-only files, files for ports newer than the list and release tags
  newer than go1.27 are never targets.

## Review notes

Corpus and oracle results (the Go implementation, 2026-11-16, `extractors-39`):

- `TestGoStructureOracle`: **0 disagreements** on the fixture (42 Go
  files), gotify (142 files, 1124 declarations, 734 import specs),
  realworld-gin (21 files, 241 declarations, 106 import specs) and the
  self-index of the Go implementation branch, fixture included (794 files, 8096
  declarations, 4631 import specs).
- Snapshot diff `extractors-37` → `extractors-39` (gotify, realworld-gin,
  and the self-index at `c225f0cc`): removed `imports` edges gotify 169,
  realworld-gin 5, self 4753 (4927 in all), each equal to the investigation
  oracle prototype's extra-edge list for the same snapshot; no other
  record changed except the header's version and timestamp and the
  footer's counts.

Review fixes (still `extractors-39`):

- `TestGoStructureOracle` (raw bytes, go/build `ImportDir` per port, tag
  subset and cgo setting): **0 disagreements** on the fixture (63 Go files
  with the written cases: 3 lone-CR, 2 rejected, 42 import specs of which
  5 malformed and 9 refused), gotify (142 files, 734 import specs),
  realworld-gin (21 files, 106 import specs) and the self-index (820
  files, 4700 import specs). `TestGoStructureGoList` (188 configurations:
  48 ports, cgo off and on, tag `integration` off and on; 21 package
  directories): 0 disagreements; `goPorts` equals `go tool dist list`.
- Snapshot diff against the Go implementation binary (`893af975`): gotify and
  realworld-gin identical (GORM output included); the self-index differs
  only in the new fixture cases and in 7 `calls` edges between the files
  of `scripts/oracle/gorm_oracle/`, which are all `//go:build ignore`
  (`package main`): no configuration builds them into a package, so each
  file now sees only its own declarations (the oracle script lists them
  explicitly to `go run`; a miss, not a wrong fact). Against main
  (`c225f0cc`): the Go implementation removals (gotify 169, realworld-gin 5
  `imports` edges) and nothing else on the corpora.

## Review notes (S1R, pending acceptance)

Independent review (the T020 task record (internal), on `893af975`).
Verdict, verbatim: "**The PL-L1 flip is blocked.** Four class A findings:
in each, Graphvein states a structural fact with provenance `fact`,
confidence 1, that the Go toolchain contradicts, and
`TestGoStructureOracle` cannot see it because the oracle shares the blind
spot. None of them occurs in gotify, realworld-gin or the self-index
(checked by grep), so the corpus numbers stay valid; all four are
reachable in ordinary repositories and each has a small class-level fix."

Fixes (`4f04be01`), verbatim from the task file's "S1
review fixes": "Every class A and B finding is fixed as a class rule,
still at `extractors-39`; PL-L1 is not added and no label changes." Per
finding: A1 lone CR (row GS-F3), A2 malformed import paths
(`CheckImportPath`), A3 `package documentation`, A4 cgo, B1 an oracle on
go/build `ImportDir` over raw bytes plus the `go list` differential, B2
go/build package errors refused, B3 one package reader for imports, calls
and GORM, B4 the `code_calls` label (O-S1-5 (a)); each with a fixture
case, and mutation checks in which the oracle or `go list` caught every
reverted fix. No class A finding is open. The class C items and the two
open points of the fixes (import cycles, `internal/` visibility) are
known gaps above, kept by decision.

Flip (`extractors-39`): row PL-L1 of
`docs/extract/precision.md` labels Go files `precise` (test files
included); calls stay row D (`code_calls`). A Go file whose structure
claims nothing (go/parser rejects it, a lone CR, not parsed, not extracted
or dropped by validation) is stored `best_effort`, served with reason
`structure_refused`. This section stays pending until the developer
accepts the review record; Status stays Proposed.

## Review notes (acceptance, 2026-11-16)

Accepted after delegated review on 2026-11-16 (T020 structure contract, Go).

- **Steps:** S1A inventory and oracle prototype (found 4,927 wrong in-module import edges on main: test files linked to other packages' `_test.go` files and `x_test` files linked to siblings and themselves); S1B contract, `ImportFiles`, fixture and oracle (the snapshot diff removed exactly the oracle's list); S1R independent review (the T020 task record (internal): class A lone CR, malformed import paths, documentation files, cgo; class B oracle independence, package errors, a confirmed wrong `calls` edge and GORM binding from a build-ignored first file, the missing `code_calls` label); S1F fixes (every A and B as class rules, the oracle rebuilt on `go/build` `Context.ImportDir` over raw bytes, the opt-in `go list` differential, mutation checks); S1L rule row PL-L1 and the label flip; S1I integration with T021.
- **Decisions:** the open points took their precision-safe defaults (O-S1-1 fallback (a) in S1B, then (b) in S1L: a Go file whose structure claims nothing stores `best_effort`); import cycles and cross-tree `internal/` imports keep their edges (they resolve to the right files as written; code is shown as written, not claimed to compile).
- **Results:** `TestGoStructureOracle` 0 disagreements on the fixture (63 Go files), gotify (142), realworld-gin (21) and the self-index (850); `go list` differential 188 configurations, 0 disagreements; snapshots unchanged apart from the removed wrong edges and the `precision` values. CI run 43955207160 on 9a7bcd8: Windows and postgres green; postgres raw log 0 FAIL, 170 `/postgres` PASS, only environment-gated skips. Aggregate extractors-39.
- **Coverage:** every Go file whose structure is claimed is `precise` (gotify 142, realworld-gin 21, self 817 at the flip); calls stay Best effort (row D, `code_calls`).
- **Known gaps and after-v1 items:** as listed under Known gaps (C3 impossible tag values, configuration-union import edges stated in GS-I1, modules without a `module` directive, GORM's `packageFiles` build selection (closed in extractors-44), line-ending-only changes and freshness, no `code_calls` column in the coverage table).
