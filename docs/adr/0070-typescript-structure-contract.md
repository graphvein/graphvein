# ADR 0070 - TypeScript, TSX and JavaScript structural claim contract: files, declarations, imports

**Status:** Accepted (delegated review, 2026-11-18)

## Context

T020 (ADR 0066) labels a language's structural scope (files,
declarations, imports) `precise` only through a rule-table row whose
evidence includes a claim contract with a 1:1 test, goldens, corpus
checks, an oracle where cheap and an independent review. The T020 investigation
(the T020 task record (internal)) inventoried every TypeScript, TSX and
JavaScript form the extractor (`internal/lang/typescript`) emits and
compared it with an oracle built on the TypeScript compiler API
(`scripts/oracle/ts_structure/oracle.cjs`, `typescript@6.0.3`) on sixteen
corpora and the self-index.

Declarations agreed on 99.0 % of the compared facts, but main stated wrong
facts in ten classes (precision stops PS-S3-1..PS-S3-10): `default.*`
nodes and `exported` flags propagated through `export default <expr>`
(D-1, 186 rows), methods read from type literals (D-2), the tree-sitter
misparse of `=> tag<T>`...`` (D-3), declarations recovered in files
tree-sitter rejects (D-4, 355 wrong and 775 missing), decorators lost
across a comment (D-6) and variables named by a binding pattern (D-7).
Imports were worse: main's resolver was a best-effort order of its own
(ADR 0014), so 812 `imports` edges named a file the compiler does not
resolve: `paths` of a configuration that does not govern the file (I-3,
152), a `src/index` guess for unbuilt workspace entries (I-2, 642), every
named `package.json` taken as a workspace package (I-4), implementations
where the compiler takes a declaration file (I-1, 14), ESM-mode relative
specifiers (I-5), and 262 externals whose name is no npm package (I-8).

## Options considered

1. **Patch main's resolver** at each stop: keep ADR 0014's order and add
   the governing configuration, members only, and refusals. Every class of
   the inventory is a place where main's order is not the compiler's; the
   next corpus would find the next one.
2. **Transcribe the compiler's resolution** (configuration reading,
   program membership, `moduleNameResolver.ts` for node10, node16, nodenext
   and bundler) in Go over the scanned inventory, refuse what it does not
   model, and let the compiler oracle check it. Chosen.
3. **Run the compiler** (Node and `typescript`) as an external indexer.
   Precise, but a runtime dependency the indexer does not have; it remains
   the oracle only.

## Decision

Option 2, with the developer's decisions on the open points (2026-11-17,
before S3B): O-S3-1 (a), O-S3-2 (c), O-S3-3 (refuse, except the
compiler's own output mapping proven by the member's tsconfig), O-S3-4
(a), O-S3-5 (a), O-S3-6 (a), O-S3-7 (a), O-S3-8 (a). The contract is
`docs/lang/typescript-structure.md`, rows TS-C1..TS-C3, TS-F1..TS-F3,
TS-D1..TS-D12, TS-I1..TS-I7 and the default branch D, mirrored by
`structureRows` (`internal/lang/typescript/structure.go`).

- **Configurations** (`tsconfig.go`): the compiler's reading (JSONC,
  `extends` chains with relative and package-name bases through workspace
  links, path options relative to their configuration, `${configDir}`,
  `jsconfig.json` defaults) and program membership (`files`, `include`,
  `exclude` with the compiler's wildcard semantics, extension priority,
  `allowJs`, `resolveJsonModule`, `references`); the governing
  configuration is the first, walking up, whose program contains the file;
  otherwise the inferred-project options. An unread configuration makes
  the file's options unknown: bare specifiers claim nothing, relative ones
  only what node10, bundler and nodenext agree on.
- **Resolution** (`resolve.go`): a transcription of
  `nodeModuleNameResolverWorker` and the functions it calls (paths,
  baseUrl, rootDirs, extension substitution, directory and package
  loading, exports/imports with conditions, patterns and arrays,
  self-names, node_modules through the links, @types, typeRoots), with the
  site's resolution mode (`getModeForUsageLocation`). Lookups it does not
  model (typesVersions, `types@` conditions, non-empty moduleSuffixes,
  `tryLoadInputFileForPath`, classic) refuse the site.
- **Packages** (`workspace.go`): declared members only, linked as
  `<root>/node_modules/<name>`; duplicate names link neither. O-S3-3:
  through a link, an output the member's own `tsconfig.json` emits into
  its outDir or declarationDir (rootDir default as in TypeScript 6.0: the
  configuration's directory) exists and stands for its source; no
  `src/index` fallback.
- **Checks after resolution**: O-S3-2 (c), the implementation-only
  resolution (`noDtsResolution`) must find the same file or none; a bare
  name the compiler does not resolve is an external only when it is a
  valid npm name and no non-member `package.json` of the repository carries
  it (`unlinked_package`: it may be a `file:` dependency, and the Angular
  and Tauri shadow guards rely on it).
- **Declarations** (`extract.go`): flags and `default.*` names only for the
  declaration an `export` names (PS-S3-1); type positions skipped
  (PS-S3-2); the misparse to D (PS-S3-3); binding patterns to D (PS-S3-9);
  decorators across comments (PS-S3-10); namespace and module blocks to D
  (O-S3-6); `import = require()` not a site (O-S3-7, and I-9's misleading
  "empty import specifier" warning is gone).
- **Files tree-sitter rejects** (TS-F2, O-S3-1 (a)): the indexer replaces
  the facts of a TypeScript file with a syntax error by
  `typescript.RefuseSyntaxError`: the file node only, one `syntax_error`
  count; calls, export bindings and routes go with it.
- **Default branch count**: one `info` diagnostic `ts_structure_unread`
  per file with the count per form; extractor forms are cached in
  `facts.TypeScriptFacts.Unread`, the linker adds its refusals.
- **Oracle**: `TestTypeScriptStructureOracle` indexes the fixture through
  the indexer and runs `oracle.cjs` with the pinned compiler
  (`GRAPHVEIN_TS_ORACLE_DIR`, skipped when absent; corpora opt-in through
  `GRAPHVEIN_TS_ORACLE_CORPORA`); any claimed fact the compiler does not
  confirm fails it.
- Extractor version `extractors-41` (the Python implementation holds 40 on its
  branch; the number is re-checked when the branches merge), TypeScript
  extractor `typescript-2`.

No rule-table row is added and no label changes: PL-L3 follows the
independent review.

## What the oracle shares with the extractor

The oracle parses with the compiler, resolves with
`ts.resolveModuleName`, reads configurations with
`getParsedCommandLineOfConfigFile` and computes outputs with
`ts.getOutputFileNames`, over the raw bytes and the filesystem; it uses no
Graphvein code. It still shares, by definition of the contract:

- the naming rules of `identity.md` (owners, `default.*`, `@overload`
  signatures, `@n`), transcribed, so a shared naming mistake (like D-7
  before the fix) is visible only in the census;
- the span conventions of O-S3-5 (written into both);
- the decision which files are rejected (the snapshot's `syntax_error`
  diagnostics: the oracle expects nothing there, it does not judge
  tree-sitter);
- the asset rule (`<x>.d<ext>.ts` stands for `<x><ext>`, then an existing
  relative file), the npm package-name rule and the "link in reach means
  refuse" rule, transcribed as definitions (since S3F the rest of TS-I5 is
  not shared: the oracle reads a matched `paths` key from the compiler's
  resolution trace, built-ins from Node's `module.isBuiltin` and the
  dependency declarations with `JSON.parse` over the raw `package.json`
  files above the importing file);
- the workspace member selection (package-manager globs), written
  separately in JavaScript and Go from the package managers' documented
  behaviour, not from either's code;
- the choice of the member's root `tsconfig.json` for output mapping.

What it does not compare: calls, export bindings, routes, model facts;
`require()` and dynamic `import()` targets; the default-branch counts;
packages under a real `node_modules` (none on disk); symlinked directories
(the scanner skips them; the compiler would follow them).

## Consequences

- Import edges, calls, routes and model linkers read the new resolver:
  an import that is refused binds no name, so some `calls` edges and
  route handlers through refused imports disappear (corpus diff below).
- Many TypeScript files gain a `ts_structure_unread` info diagnostic
  (`require_call` alone in CommonJS code).
- Files tree-sitter rejects lose their declarations and imports; on
  gladys's Preact front end that is most of its structure (known gap).
- Workspace imports of packages built by a bundler (an unbuilt `dist`
  without a proving `tsconfig.json`) claim nothing instead of guessing
  `src/index`.

## Known gaps

- Tree-sitter rejects TypeScript-valid forms (inventory section 1: JSX
  attributes named by keywords, `&` in JSX text, `typeof import()` in type
  arguments, `export type * from`, unnamed function-type parameters, a
  property `in`); those files claim nothing (cost per corpus below). Fixing
  the grammar is a separate task (O-S3-1 (c)).
- Case-insensitive file systems: paths compare case-sensitively; on
  Windows or macOS the compiler would also match a differently cased
  specifier, where Graphvein reports it unresolved (no wrong edge).
- Files in directories the scanner ignores (`dist`, `build`, `vendor`,
  `node_modules`) are invisible: an import into them is unresolved, and a
  lookup the compiler would satisfy there first (for example a
  `vendor/index.ts` before a sibling `vendor.js` under node10) could pick
  another file; not observed on the corpora.
- Symlinked directories are not followed (the scanner skips symlinks).
- `typesVersions`, `types@` conditions, non-empty `moduleSuffixes`,
  classic resolution, `preserveSymlinks` through links, `rootDirs` lookups
  for failed relative imports beyond the transcription: refused or
  unresolved, never claimed.
- Output mapping reads only the member's root `tsconfig.json` (not a
  `tsconfig.build.json` or a referenced project); a package built by a
  bundler stays `workspace_entry_missing`.
- A non-member `package.json` carrying an imported package name refuses
  the import even when it is only an example or a test fixture.
- A real install can put another package of a member's name in a nearer
  `node_modules`; Graphvein assumes the workspace link.
- Review class C (S3R, not fixed in S3F; none states a wrong fact):
  C1 under `config_unread` a relative import claims what node10, bundler
  and nodenext agree on, though an unread base could set `moduleSuffixes`;
  C2 `disableSolutionSearching` is not read (the walk goes on past it);
  C3 every consumer resolves with the file's import-declaration mode, so a
  `require()` (CommonJS) or dynamic `import()` (ESM under node16/nodenext)
  in calls, Angular lazy routes and Sequelize `require` reading can resolve
  with the wrong mode; C4 `pnpm-workspace.yaml` flow sequences are not read
  and npm's order-sensitive negation is read as a global exclusion (both
  refuse); C5 without `node_modules`, a `typeRoots` declaration for an
  installed JavaScript-only package can win under bundler, and a `/x`
  web-root import a `paths` `*` or `/*` key would map stays TS-I6; C6 a
  file in two programs takes the first program's options; C7
  `config_unread` also removes every framework proof of the file (kaneo
  `apps/mobile`: 690 sites); C8 the inferred-project options and
  TypeScript 6.0's `moduleResolution` default are O-S3-8 (a); C9 the
  `--ts 59` differential found no claim depending on 6.0 semantics.
- TS-I5 refuses packages a project uses without declaring them (hoisted
  from a sibling workspace member, installed globally, provided by a
  bundler), and every bare name a matched `paths` key does not resolve,
  even when the compiler would then find the package in `node_modules`.
- A family fact re-anchored to the file node of a TS-F2 file loses the
  declaration it belonged to (a table access of `getTasks` becomes the
  file's); a family edge that points at a withheld declaration (a route
  handler, an Angular class) is not emitted.

## Review fixes (S3R)

The independent review (the T020 task record (internal), head `5fc1fb3`)
found one class A and five class B findings. Each is fixed as a class rule;
the contract table, `structureRows` and `TestTypeScriptStructureContract`
stay 1:1, and every fix has a fixture case or test that fails when the fix
is reverted (mutation-checked). The extractor version stays `extractors-41`
(2026-11-17 assigns 41 to TypeScript, 40 to Python, 42 to Rust).

- **A1 (TS-I5).** A bare specifier the compiler does not resolve in the
  repository is an npm external only when (i) the whole specifier is a
  Node built-in (Node 24's `module.builtinModules`, with or without
  `node:`; `node:sea`, `node:sqlite`, `node:test`, `node:test/reporters`
  only with it), or (ii) a scanned `package.json` in the importing file's
  directory or an ancestor declares the package name in `dependencies`,
  `devDependencies`, `peerDependencies` or `optionalDependencies`; and
  (iii) no `paths` key of the file's options matched it. Otherwise D: a
  matched `paths` key is `paths_target_missing` (the compiler would go on
  to `node_modules`, but the targets are typically generated or ignored
  files), a name nothing declares is `undeclared_package`. The rule
  applies to `ModuleResolution.External` itself, so every consumer (calls,
  routes, clients, Angular, Tauri, models) sees a refused bare import as
  unproven, not as the package. Fixtures that imported packages without a
  `package.json` now declare them (every real project in the corpora
  does). The oracle no longer shares the rule (see "What the oracle
  shares"). Fixture: `typescript-structure/src/externals.ts`,
  `esm/package.json`, `legacy/`; test `TestDeclaredPackages`.
- **B1.** `${configDir}` is substituted in `paths` targets as the
  compiler's `getSubstitutedPathWithConfigDirTemplate` does (prefix
  compared ignoring case, the first exact occurrence replaced, the target
  then absolute, so the paths base does not apply); the same helper now
  serves path options and `files`/`include`/`exclude`. Fixture `cfgdir/`.
- **B2.** TypeORM (`refs.go`) and Sequelize (`cjs.go`, imports, re-exports
  and bare `require`) read a bare specifier as the package only when the
  resolver says `External`; a refusal is "not proven" with its form in the
  reason. Sequelize keeps the `paths`-to-declaration carve-out (a bare
  specifier `paths` maps to a `.d.ts`). Tests `TestDiscoverPackageShadow`
  (both packages, a non-member `package.json` named `sequelize` /
  `typeorm`: no instance, no group).
- **B3 (decision).** TS-F2 refuses the structural claims only:
  no declaration node, `contains`, `imports` or `calls` edge (and no
  `calls` edge from another file into one of its declarations), the
  `syntax_error` count, and (once PL-L3 exists) a best-effort label
  (`refusedStructure`). The extracted facts stay in the payload, marked
  `Rejected`, so the family extractors (models, table access, client
  calls, routes, Angular, Tauri) read such files exactly as on main under
  their own contracts and oracles. A family fact owned by a withheld
  declaration is owned by the file node instead
  (`typescript.RejectedOwners`: table access re-anchors its sites before
  grouping, `tableaccess.Input.Owners`; the indexer re-anchors other
  owner edges and drops family edges that point at a withheld
  declaration, `reanchorWithheld`). Import resolution from a rejected
  file uses the same resolver as every other file (the compiler port): it
  differs from main only in the classes it differs for accepted files
  (ADR 0070's import classes and A1), never because the file is rejected.
  Fixture `src/rejected-client.tsx`, `src/calls-rejected.ts`.
- **B4.** A matched `paths` key whose targets are all missing keeps the
  compiler's candidate path per target (the first `*` replaced, relative
  to `baseUrl` or the paths base) in `ModuleResolution.Candidates`, as the
  relative TS-I7 branch does; Orval predicts the generated client from the
  first. Fixture `orval/alias/` (a `~api/*` alias).
- **B5.** TS-F3 is tagged by production code: `typescript.NotParsed`,
  which the indexer applies to a file over the size limit, a parse failure
  and an extraction failure; `TestTypeScriptStructureContract` now runs
  `index.Emit` on the fixture (external test package), so every row's tag
  comes from the indexer.
- **Class C.** Not fixed (C3 is not trivial: the mode of a `require()` or
  dynamic `import()` site needs the compiler's
  `getEmitSyntaxForUsageLocation` per call site and a mode parameter on
  every consumer's `Resolve`); all nine are listed under "Known gaps".

### Results

Oracle (`oracle.cjs` with the S3F expectation for TS-I5, TypeScript
6.0.3) on the branch binary, what was compared as in S3B; "undeclared /
paths" are the sites the new TS-I5 refusals take:

| corpus | TS/JS files | declarations agree / claimed | import sites | claims agree | no claim | undeclared / paths | claimed disagreements |
|---|---|---|---|---|---|---|---|
| fixture | 71 | 111 / 111 | 63 | 39 | 24 | 4 / 2 | 0 |
| hoppscotch | 1179 | 6550 / 6550 | 5849 | 5244 | 605 | 43 / 129 | 0 |
| aqualink-app | 787 | 2639 / 2639 | 4498 | 4452 | 46 | 2 / 0 | 0 |
| kaneo | 4562 | 11770 / 11770 | 20508 | 19649 | 859 | 162 / 0 | 0 |
| opencollective-api | 2187 | 7082 / 7082 | 11513 | 11485 | 28 | 4 / 0 | 0 |
| react | 73 | 184 / 184 | 240 | 198 | 42 | 0 / 42 | 0 |
| umami | 1393 | 3713 / 3713 | 6078 | 6034 | 44 | 0 / 31 | 0 |
| dokploy | 1124 | 4800 / 4800 | 8347 | 8333 | 14 | 0 / 0 | 0 |
| self | 869 | 3657 / 3657 | 2175 | 2083 | 92 | 37 / 15 | 0 |

The review's A1 cases: hoppscotch's 39 `npm:src` edges (`baseUrl` into
the git-ignored `src/generated/`), 4 `npm:@app/api`, 2 `npm:@helpers/backend`
and 1 `npm:unfonts.css`, and aqualink-app's `npm:sites` and `npm:regions`
are gone (refused); the oracle now expects the refusal from its own reading.
The other 9 corpora of the review were not re-run.

Snapshot diff against S3B (what the fixes changed): A1 removes externals
(hoppscotch 47 `imports` edges, kaneo 159, aqualink-app 2,
opencollective-api 4); B3 restores the family facts S3B lost:
opencollective-api the `PaymentMethod` table (27 columns), its 131
`foreign_key` edges and the `complete` state and claims of the 47 other
tables (column `foreign_keys` 697, `nullable` 245): table, column and
edge records identical to main; kaneo 100 `reads_table`/`writes_table`
edges (identical to main except 39 that main sourced from declarations of
rejected files, now 22 file-owned edges, merged per table); hoppscotch 3
`ipc_call` nodes (file-owned). B4 restores react's 27 `client_call` nodes
and 27 `calls_operation` edges (identical to main).

Snapshot diff against main (`extractors-39`), per class, beyond S3B's
table above: no model, table, column, `foreign_key`, `calls_operation`,
`client_call` or Angular record differs on these corpora except the
re-anchoring of B3 (hoppscotch 12 `client_call` and 3 `ipc_call`
`contains` edges from the file node instead of a withheld function; kaneo
the 39 table edges above) and on self the Angular proofs of files that
import `@angular/core` without a declaring `package.json`
(`spike/parsing/samples`, the shadow fixture: `angular_decorator_unproven`
1 to 8, one `uses_template` edge less). Externals removed by A1 against
main: hoppscotch 116 `imports` edges (10 nodes), kaneo 559 (26; the
`config_unread` sites of S3B included), umami 37, dokploy 60, react 27,
aqualink-app 7, opencollective-api 13, self 45.

## Corpus results

Oracle (`oracle.cjs`, TypeScript 6.0.3, branch binary `extractors-41`):
what was compared is file nodes (span, hash), every declaration Graphvein
emits (kind, identity, name, qualified name, span, hash, `exported`,
`default_export`, `decorators`, contains edge) against the compiler AST
under the contract's naming and span rules, and every import site
(statement span; edge target or external against the contract's expected
outcome from `ts.resolveModuleName`). "No claim" sites are those where
Graphvein claims nothing (refused or unresolved).

| corpus | TS/JS files | declarations agree / claimed | compiler-only declarations | import sites | claims agree | no claim | claimed disagreements |
|---|---|---|---|---|---|---|---|
| fixture | 66 | 108 / 108 | 1 (misparse) | 52 | 33 | 19 | 0 |
| hoppscotch | 1179 | 6550 / 6550 | 0 | 5849 | 5291 | 558 | 0 |
| maintainerr | 900 | 5261 / 5261 | 0 | 4510 | 4510 | 0 | 0 |
| kaneo | 4562 | 11770 / 11770 | 0 | 20508 | 19811 | 697 | 0 |
| dokploy | 1124 | 4800 / 4800 | 0 | 8347 | 8333 | 14 | 0 |
| umami | 1393 | 3708 / 3708 | 0 | 6078 | 6034 | 44 | 0 |
| seerr | 637 | 2304 / 2304 | 0 | 4392 | 4385 | 7 | 0 |
| opencollective-api | 2187 | 7082 / 7082 | 19 (misparse) | 11513 | 11489 | 24 | 0 |
| gladys | 3671 | 5561 / 5561 | 0 | 1238 | 1238 | 0 | 0 |

(Declarations and sites of files tree-sitter rejects are not counted
above: the oracle expects nothing there.)

Cost of TS-F2 (files tree-sitter rejects, declarations the compiler reads
in them): hoppscotch 4 files / 113, maintainerr 4 / 12, kaneo 55 / 165,
dokploy 23 / 71, umami 3 / 0, seerr 0, opencollective-api 4 / 16, gladys
640 / 1964 (its Preact front end: `class=` attributes).
Known gap pointer: ADR 0074 (Proposed, T020-TSG) patches the JavaScript,
TypeScript and TSX grammars for keyword JSX attribute names, a bare `&` in
JSX and `export type *`, recovering 659 of these files; the forms it does
not cover stay TS-F2 (the T020 task record (internal)).

Under-claims worth a follow-up: kaneo's `apps/mobile` extends
`expo/tsconfig.base` (not in the repository), so 690 sites go to
`config_unread`; hoppscotch's `@hoppscotch/data` and `@hoppscotch/kernel`
are built by vite with declarations from a `tsconfig.decl.json`, so 289
sites are `workspace_entry_missing`; aliases such as `~icons` and
`@/generated` (hoppscotch 227, umami 31) are `not_a_package`.

Snapshot diff against main (`extractors-39`), per class:

| corpus | declarations removed (TS-F2 / other classes) | declaration facts changed (flags, span, decorators) | imports edges removed (TS-F2 / refused / other) | edges added or retargeted |
|---|---|---|---|---|
| hoppscotch | 127 / 48 (PS-S3-1 17, PS-S3-2 12, namespace members 19) | 72 (71 `exported`/`default_export` PS-S3-1, 1 decorators PS-S3-10) + 1 span (D-5 convention unchanged, ambient) | 18 / 516 (`workspace_entry_missing` 289, `not_a_package` 227, of which 101 were I-3 edges) / 4 (`declare module` sites) | 0 |
| maintainerr | 12 / 7 (PS-S3-2 5, PS-S3-1 1, namespace 1) | 0 | 35 / 0 / 0 | 0 |
| kaneo | 165 / 26 (PS-S3-1 15, namespace 8, PS-S3-2 3) | 0 | 369 / 690 (`config_unread`: `apps/mobile` extends `expo/tsconfig.base`) / 1 | 12 (I-3: the compiler's governing configuration) |
| dokploy | 71 / 5 (PS-S3-9 2, namespace 2, PS-S3-1 1) | 0 | 131 / 1 / 0 | 0 |
| umami | 0 / 12 (namespace 9, PS-S3-1 2, PS-S3-2 1) | 4 (PS-S3-1) | 15 / 41 (`not_a_package` 31, `workspace_entry_missing` 10) / 0 | 0 |
| seerr | 0 / 7 (namespace) | 1 (PS-S3-1) | 0 / 6 (`config_unread`) / 0 | 0 |
| opencollective-api | 16 / 43 (PS-S3-1 36, misparse 7) | 23 (PS-S3-1) + 9 renamed owners (PS-S3-1) | 36 / 0 / 0 | 1 (`config` now the npm external the compiler takes) |
| gladys | 1510 / 1 (PS-S3-1) | 0 | 3888 / 0 / 0 | 0 |
| self | 39 / 13 (namespace 8, PS-S3-2 2, PS-S3-9 2, PS-S3-1 1) | 3 (PS-S3-1 2, PS-S3-10 1) | 24 / 33 (fixture refusals: `not_a_package`, `declaration_or_implementation`, `workspace_entry_missing`, `unlinked_package`, ESM-mode) / 1 | 5 (fixture cases the compiler resolves: `paths` through `references`, an `exports` pattern, pkg-b's root `index.ts`) |

Every changed class is an intended one: no claim remains that the oracle
does not confirm. `calls` edges and route handlers that bound through a
refused import, and the new `ts_structure_unread` diagnostics, change with
them (not tabulated).

## Review notes

Corpus and oracle results. What `oracle.cjs` (TypeScript 6.0.3 compiler
API over the raw bytes) compares is described under "Corpus results
": file nodes, every declaration Graphvein emits and every
import site against the contract's expected outcome; sites where
Graphvein claims nothing are counted, not compared. It does not compare
`calls` edges (row D) or the family facts (models, table access, clients,
routes, Angular, Tauri), which have their own contracts and oracles.

- S3B (`extractors-41`): 0 claimed disagreements on the fixture and eight
  corpora (table "Corpus results").
- S3R, the independent review: 52,547 / 52,547 declarations and 70,716
  import sites on 17 corpora with 0 claimed disagreements, but the oracle
  shared the npm-name rule of TS-I5 (a class A finding).
- S3F, after the fixes, with the oracle's own TS-I5 expectation: 0
  claimed disagreements on the fixture and eight corpora (table "Results
").
- S3L (head of step T020-S3L, `extractors-41`, with the unread-options
  fix below): **0 claimed disagreements on the fixture, the 17 corpora of
  the S3 review and the self-index**. Fixture (`TestTypeScriptStructureOracle`):
  71 files, 111 / 111 declarations, 63 import sites, 39 claims agree. The
  17 corpora: 17,435 TS/JS files, 52,547 / 52,547 declarations, 70,716
  import sites of which 68,987 claims agree (the rest claim nothing);
  self-index: 869 files, 3,657 / 3,657 declarations, 2,175 sites, 2,083
  agree.

| corpus | TS/JS files | declarations agree / claimed | import sites | claims agree | files `precise` / `best_effort` (TS-F2, TS-F3) |
|---|---|---|---|---|---|
| seerr | 637 | 2304 / 2304 | 4392 | 4385 | 637 / 0 |
| nest-admin | 242 | 990 / 990 | 1210 | 1192 | 242 / 0 |
| opencollective-api | 2187 | 7082 / 7082 | 11513 | 11485 | 2183 / 4 |
| gladys | 3671 | 5561 / 5561 | 1238 | 1236 | 3031 / 640 |
| umami | 1393 | 3713 / 3713 | 6078 | 6034 | 1390 / 3 |
| dokploy | 1124 | 4800 / 4800 | 8347 | 8333 | 1101 / 23 |
| kaneo | 4562 | 11770 / 11770 | 20508 | 19649 | 4507 / 55 |
| react | 73 | 184 / 184 | 240 | 198 | 73 / 0 |
| hoppscotch | 1179 | 6550 / 6550 | 5849 | 5244 | 1175 / 4 |
| angular | 50 | 126 / 126 | 270 | 255 | 50 / 0 |
| angular-ngmodule | 67 | 161 / 161 | 244 | 243 | 67 / 0 |
| kftray | 180 | 601 / 601 | 855 | 854 | 179 / 1 |
| express | 357 | 742 / 742 | 888 | 886 | 357 / 0 |
| realworld | 26 | 63 / 63 | 76 | 50 | 26 / 0 |
| maintainerr | 900 | 5261 / 5261 | 4510 | 4491 | 895 / 5 |
| aqualink-app | 787 | 2639 / 2639 | 4498 | 4452 | 785 / 2 |
| localsend | 0 | 0 / 0 | 0 | 0 | 0 / 0 |
| self | 869 | 3657 / 3657 | 2175 | 2083 | 861 / 8 |

- Snapshot diff S3L against `c98a29b` (S3F) on the same 18 inputs:
  identical apart from the `precision` value of file records and file
  nodes (the flip counts above) and, from the unread-options fix, the
  `unresolved_calls` split of 5 kaneo `apps/mobile` files (calls through a
  declared package under unread options now count as external).
- Family facts against main (`extractors-39`) on the 16 corpora with
  family facts: model tables, columns and `foreign_key` edges,
  `reads_table` / `writes_table` edges, `client_call` nodes,
  `calls_operation` / `calls_route` edges, Angular and Tauri records are
  equal, except the re-anchoring of class B3 (kaneo 39 table edges of
  rejected test files now 22 file-owned edges, and 3 `table_access_unknown`
  rows of `default.*` owners now on the file node, PS-S3-1; hoppscotch 12
  `client_call` nodes renamed from `default.arrow*` owners and 15
  `client_call` / `ipc_call` `contains` edges from the file node) and one
  hoppscotch `client_call` whose `dynamic` status is unchanged but whose
  helper chain through `@hoppscotch/data` (TS-I4 `workspace_entry_missing`)
  is no longer listed. Nothing is lost or gained.

Unread options (S3F open point 1, decided (b) in S3L): a file whose
governing `tsconfig.json` extends a package outside the checkout keeps
its bare imports refused (`config_unread`, no `imports` edge); family
consumers (TypeORM, Sequelize, Drizzle, and every consumer of the export
bindings: clients, routes, Angular, Tauri, table access) read such a
specifier as the package exactly when rule (ii) of TS-I5 holds for its
package name and no workspace link or scanned `package.json` name carries
it (`ModuleResolution.DeclaredPackage`, `Package`). Node built-ins are not
included. Tests `TestUnreadDeclaredPackages`, TypeORM
`TestDiscoverCompilerFlags` ("package extends" restored),
Sequelize `TestDiscoverUnreadConfig`.

## Review notes (S3R, pending acceptance)

Independent review (the T020 task record (internal), on `5fc1fb3`).
Verdict, verbatim: "**The PL-L3 flip is blocked by one class A finding
(A1).** Row TS-I5 claims "the file imports an npm package of this name
that is not in the repository" for every bare specifier the compiler does
not resolve over the scanned inventory whose first segment looks like an
npm name." The review found every other claim confirmed by its own probes
and corpus runs (60 generated projects, 17 corpora).

Fixes (`c98a29b`), verbatim from "Review fixes" above: "The
independent review [...] found one class A and five class B findings. Each is
fixed as a class rule; the contract table, `structureRows` and
`TestTypeScriptStructureContract` stay 1:1, and every fix has a fixture
case or test that fails when the fix is reverted (mutation-checked)."
A1 (TS-I5's declared-package rule, applied to the resolution itself, with
the oracle no longer sharing it), B1-B5 as listed there. No class A
finding is open. The class C items are known gaps above.

Flip (`extractors-41`): row PL-L3 of
`docs/extract/precision.md` labels TypeScript, TSX and JavaScript files
`precise` (declaration and test files included); calls stay row D
(`code_calls`). A file whose structure claims nothing (tree-sitter rejects
it, TS-F2; not parsed or not extracted, TS-F3; or dropped by validation)
is stored `best_effort`, served with reason `structure_refused`. The
oracle `TestTypeScriptStructureOracle` is opt-in (it skips without
`GRAPHVEIN_TS_ORACLE_DIR`), so it does not run in CI. This section stays
pending until the developer accepts the review record; Status stays
Proposed.

## Review notes (acceptance, 2026-11-18)

Accepted after delegated review (2026-11-17). Steps: the investigation (the T020 task record (internal)), S3B implementation, one independent review (the T020 task record (internal): one class A, five class B, class C items), S3F fixes (A1 and B1-B5 fixed as class rules and mutation-checked; class C items are known gaps above), S3L (`config_unread` declared-package rule for family consumers, row PL-L3, label flip), S3I (merge with the Python structure stack).

- Results: the TypeScript 6.0.3 compiler-API oracle (`TestTypeScriptStructureOracle`, opt-in, not in CI) reports 0 claimed disagreements on the fixture and 17 corpora (52,547 declarations, 68,987 import claims); after the merge with main it was re-run on the fixture, kaneo, hoppscotch and the self-index with 0 disagreements and 0 hash mismatches.
- Coverage after the flip (precise / best effort files): seerr 637/0, nest-admin 242/0, opencollective-api 2,183/4, gladys 3,031/640 (files tree-sitter rejects, TS-F2; grammar follow-up), umami 1,390/3, dokploy 1,101/23, kaneo 4,507/55, hoppscotch 1,175/4, maintainerr 895/5, self-index 861/8.
- Wrong facts removed from main: 812 wrong import edges found by the investigation, unresolved bare specifiers shown as npm externals (A1: kaneo 559 edges, hoppscotch 116, dokploy 60), the `src/index` fallback, declarations recovered from rejected files.
- Refusals kept deliberately (they lose import edges only, no route or link): root `tests/` directories declared only by a sibling member, Vite-built `dist` entries, configs extending a base package that is not in the checkout.
- Known gaps and after-v1 items: as listed above; the six grammar forms behind TS-F2 are a follow-up task; calls stay row D (`code_calls`).
- Gates: local full check 0 FAIL, 171 `/postgres` PASS, check-web 1,124/1,124; CI run 44221172958 on afd261c (`[ci windows]`) green, postgres raw log 0 FAIL, 172 `/postgres` PASS, Windows 0 FAIL.
