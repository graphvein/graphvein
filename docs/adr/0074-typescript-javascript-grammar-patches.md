# ADR 0074 - Patched JavaScript, TypeScript and TSX grammars (TS-F2 recovery)

**Status:** Accepted (delegated review, 2026-11-18)
**Date:** 2026-11-18

Task: T020 (grammar patches). Evidence: the T020 task record (internal)
and its per-file census the T020 task record (internal). Amends ADR 0006
(asset list and compilation-cache namespace), ADR 0007 ("compiles checked-in
generated parsers without regeneration") and ADR 0028 (toolchain note), and
narrows the cost of row TS-F2 of ADR 0070.

## Context

ADR 0070 row TS-F2 refuses every TypeScript, TSX or JavaScript file in which
tree-sitter reports an `ERROR` or `MISSING` node: the file claims nothing
beyond its file node. The developer chose (O-S3-1 (c)) to fix the grammar
gaps as their own task. The investigation found 749 rejected files of 22,122 on the 27
pinned corpora plus Graphvein itself; the TypeScript 6.0.3 compiler parses
745 of them without a syntax error. Nine forms cause the 745; three of
them (G1 keyword JSX attribute names, 644 files, all JavaScript; G2 a bare
`&` in JSX text or attribute strings, 14; G4 `export type *`, 1) account for
659.

The pinned grammars are the newest releases: tree-sitter-javascript v0.25.0
and tree-sitter-typescript v0.23.2 (git tags, npm, crates.io and PyPI agree).
G1 is a regression of JavaScript 0.25.0, which adopted tree-sitter 0.25's
reserved-word sets.

## Options considered

- **(a) Newer upstream release.** None exists. The unreleased `master`
  commits could not be inspected (codeload refused the repositories in the
  investigation session) and are not adopted (decision 4).
  Downgrading JavaScript to v0.23.1 fixes G1 but changes the trees of four
  accepted files and drops everything else 0.25.0 changed.
- **(b) Patches carried in the repository** (chosen for G1, G2, G4).
- **(c) Keep refusing** (chosen for G3 and G5-G9 and for every form no patch
  covers): always precision-safe.

## Decision

Carry three reviewed patches of the generated `src/grammar.json` files
(decisions 1 and 2):

| Patch file | Grammar | Change |
|---|---|---|
| `grammars/patches/javascript-0001-jsx.diff` | JavaScript v0.25.0 `src/grammar.json` | P1: `_jsx_attribute_name` also accepts the reserved-word tokens `break case catch class const continue debugger default delete do else export extends finally for function if in instanceof new return switch throw try typeof var void while with`, aliased to `property_identifier`, and wraps the identifier branch in `RESERVED properties`. P2: `_jsx_bare_ampersand` (`token(prec(-1, /&/))`) aliased to `jsx_text` in `_jsx_child` and to `string_fragment` in both `_jsx_string` quote forms |
| `grammars/patches/typescript-0001-jsx-export-type.diff` | TypeScript v0.23.2 `typescript/src/grammar.json` | P2 as above; P3: the `export type` branch of `export_statement` also takes `* from` and `namespace_export from` |
| `grammars/patches/tsx-0001-jsx-export-type.diff` | TSX v0.23.2 `tsx/src/grammar.json` | P2 and P3 |

Pipeline (`scripts/build-grammars.py`, `grammars/manifest.yaml`):

- The JavaScript, TypeScript and TSX rows become `built in CI` (seven CI
  grammars). Each patched row lists `patches` (path under
  `grammars/patches/` and SHA-256), `regenerate` (the `grammar.json` the
  patch edits) and `parser_sha256` (the digest the regenerated `parser.c`
  must have).
- After the verified archive extraction, the builder checks each patch
  digest, applies it with `git apply -p1`, runs the pinned
  `tree-sitter generate` (CLI 0.25.10, already digest-checked by
  `grammars.yml`) on the patched `grammar.json`, which needs no Node and no
  checkout of the JavaScript grammar TypeScript was composed from, and
  refuses a parser whose digest differs. The ABI check then reads the
  regenerated parser: TypeScript and TSX move from ABI 14 to ABI 15, which
  the wasitter v0.1.0 runtime already loads (JavaScript, Angular, C#, Dart).
- The three modules are compiled with WASI SDK 25.0 clang 19.1.5 like
  ADR 0007's modules, replacing the wasitter v0.1.0 release modules built
  with Zig 0.15.2. `grammar-registry.json` (the upstream builder's registry,
  provenance only) is left unchanged, as for Angular, C# and Dart; the
  build records (`*-build.json`) carry the new provenance.
- When the modules are registered: new digests in `internal/parse`
  `checksums`, `SHA256SUMS`, cache namespace `wasitter-0.1.0-assets-v5`,
  aggregate extractor version `extractors-45`.

Same change (decision 6): tree-sitter-javascript (patched or
not) reads `await (x).y()` as a call of a function named `await`; the
TypeScript extractor now records no call site for a call whose callee is
the bare identifier `await` (counted `misparse`), and still reads the calls
in its operand. A bare `await(x)` is already read as an `await_expression`
and is not affected, and the TypeScript and TSX grammars read every form
correctly. In a sloppy script that declares `function await`, a real call
written `await (1).y()` is dropped the same way: an under-claim counted
`misparse`, never a wrong fact. Such calls occur 30 times in accepted files (maintainerr 26,
all in `.yarn/releases/yarn-4.17.1.cjs`, which is not extracted; gladys 3;
kaneo 1). Snapshot diff main against the branch with the unchanged grammar
modules: main emitted 3 invented unknown call sites (gladys 2 in
`server/lib/gateway/gateway.disconnect.js`, kaneo 1 in
`scripts/ui-review-bot/worker/index.mjs`) and no edge; the branch removes
those 3 and adds `misparse` counts (gladys 3, kaneo 1); nothing else changes
on maintainerr, gladys or kaneo.

## Consequences

- Expected recovery (the investigation, native build of the same sources): 659 files
  (gladys 640, dokploy 9, langflow 3, self 3, aqualink-app 2, kaneo 1,
  maintainerr 1); still refused: 90 (86 compiler-valid, 4 invalid). On the
  same native build, every one of the 21,373 accepted files keeps a
  byte-identical tree, and regeneration without patches changes no tree.
- The patched trees of the 659 recovered files equal the TypeScript 6.0.3
  compiler on a projection of 28,782 facts (top-level declarations, import
  and export-from specifiers, JSX attribute names).
- Regeneration is neutral on the production path too: production
  regenerates from `src/grammar.json` (not `grammar.js`), which yields the
  same JavaScript `parser.c` as `grammar.js` and, for TypeScript/TSX,
  identical trees on every corpus file. When comparing grammar
  variants with the tree-sitter CLI, give each variant its own
  `TREE_SITTER_LIBDIR`, or the shared library cache reuses another
  variant's library.
- Export bindings: P3 makes `export type *` next to `export *` reachable. A
  name both supply from different modules is now ambiguous and binds
  nothing (option (a)); fixture `src/recovered/star-hub.ts`.
- Fixture cases for each recovered form: `src/recovered/` in
  `typescript-structure`, covered by the TypeScript 6.0.3
  oracle on the fixture.
- Patches must be re-applied and re-reviewed on every upstream bump; a patch
  that no longer applies or yields another parser fails the CI build.
- Fixtures: `typescript-structure`'s TS-F2 cases `src/comp.js`, `src/amp.tsx`
  and `src/rejected-client.tsx` used forms the patches now accept; they use
  forms that stay rejected (`import=`, `(string) => void`, `typeof import`).

## Known gaps

- Still TS-F2: G3 `f<typeof import("x")>()` (76 files), G5 unnamed
  function-type parameters (2), G6 `in`/`instanceof` members after a newline
  (3), G7 tagged templates with type arguments (4), G8 generic call
  signatures after a newline (1), and JSX attribute names `true`, `false`,
  `null`, `this`, `super`, `import` (none in the corpora). G9 (`using` as an
  identifier) occurred only in Graphvein's own UI, which no longer uses it.
- Also still rejected, all compiler-valid and precision-safe refusals: reserved-word parts of namespaced attribute names (`<a class:list={…}/>`),
  reserved-word element or member names (`<class/>`, `<a.class/>`), and
  `export type * from "x" with {…}`.
- The investigation counts came from a native build of the npm tarballs of the
  same tags; the CI modules reproduced them on the production path (see
  Implementation), except the three self fixture files, rewritten to stay
  rejected.
- Untagged upstream fixes are not evaluated.

## Implementation

- `Grammar assets` run 44249201780 on `wt/t020-tsg` (commit 04d12e2):
  success; the three patches applied and the regenerated parsers matched
  their reviewed digests in CI. Artifact `grammar-assets` id 14402644061,
  1,470,695 bytes, digest
  `sha256:aaed23904fcd7582c7547a1f78b7d061ef71a43c0bf210b9380ecbac0ce2e40b`
  (as GitHub reports; `gh run download` unpacks the zip, so the zip digest
  itself was not recomputed). Fetched with `scripts/fetch-grammars.sh`:
  `sha256sum -c SHA256SUMS` OK for all 26 files; `--check-paths` finds no
  host path; Angular, C# and Dart modules byte-identical to production;
  manifest and notices equal the committed files.
- Registered: `wasitter-javascript.wasm` (661,726 B,
  `3cdf3646…3fc3`), `wasitter-typescript.wasm` (1,672,849 B,
  `520278cf…59e2`), `wasitter-tsx.wasm` (1,710,045 B, `454fe622…01be`),
  their `*-build.json` and `*-LICENSES.txt` copied byte for byte;
  `internal/parse` `checksums`, `SHA256SUMS`, cache namespace
  `wasitter-0.1.0-assets-v5`, aggregate `extractors-45`.
- Production-path census (`scan.Walk` + `parse.Consume`, scratch overlay
  hashing every detached tree: kinds, fields, byte ranges, unnamed tokens)
  over the 27 corpora and Graphvein: **656 corpus files recovered** (gladys
  640, dokploy 9, langflow 3, aqualink-app 2, kaneo 1, maintainerr 1),
  exactly the investigation prediction; the three self fixture files now use forms
  that stay rejected; **all 21,375 accepted files have identical trees**;
  no file newly rejected; 92 still rejected (85 corpus files, 7 self
  fixture files).
- Oracle: `TestTypeScriptStructureOracle` (TypeScript 6.0.3) on gladys,
  dokploy, kaneo, langflow, aqualink-app, maintainerr, Graphvein and the
  fixture: **0 claimed disagreements, 0 file-hash mismatches**; declarations
  agree 7,525/7,525 (gladys), 4,841/4,841 (dokploy), 11,775/11,775 (kaneo),
  6,861/6,861 (langflow), 2,645/2,645 (aqualink-app), 5,273/5,273
  (maintainerr), 3,701/3,701 (Graphvein). It compares file nodes,
  declarations and import sites, not `calls` edges or family facts.
- Snapshot diff main (0bc25c1) against the branch on all 21 TS/JS corpora:
  every changed record belongs to a recovered file, to a file with an
  `await (…)` call site, or is a direct consequence of a recovered file (npm
  external nodes it imports: gladys 11, maintainerr 1; one kaneo call site
  in `apps/api/src/import/source/open-import-source.ts` that now resolves
  into the recovered `packages/importers/src/index.ts`). Nothing else
  changes. gladys gains 640 file records, 1,964 declarations, 1,964
  `contains`, 3,848 `imports` and 493 `calls` edges.
- Binary size: `graphvein` 63,857,151 → 63,938,655 bytes (+81,504; the
  three modules +75,306).
- Timing spot check (one interleaved run per corpus, main then branch,
  same machine): gladys 39.8 s / 39.4 s, kaneo 48.2 s / 46.9 s, langflow
  178.4 s / 169.6 s; no corpus slower by more than run-to-run noise.

## Review notes (acceptance, 2026-11-18)

Accepted after delegated review (2026-11-17; the developer asked for the grammar task, O-S3-1 (c), to be finished before acceptance). Steps: the investigation (the T020 task record (internal), census of 749 rejected files and fix options), the implementation (patch pipeline, CI build, module registration, the `await` call-site rule), one independent review (the T020 task record (internal): no class A; B1 a name reachable through a type-only and a value star export of different modules, B2 missing fixtures for the recovered forms, C1-C3 docs), TSGF fixes (B1 binds nothing for such a name, mutation-checked; B2 fixture files per recovered form; C1-C3).

- Supply chain: patch digests checked by `scripts/build-grammars.py`; the regenerated `parser.c` digests were reproduced independently by the reviewer from `grammar.json`; the committed modules equal Grammar assets run 44249201780 byte for byte (checked by the author and, independently, by the reviewer), and the JavaScript, TypeScript, TSX, Angular, C# and Dart modules of a second run (44261666892, on the gate commit) are byte-identical to the committed ones (checked during the review).
- Results: 656 corpus files recovered (gladys 640, dokploy 9, langflow 3, aqualink-app 2, kaneo 1, maintainerr 1); all 21,375 previously accepted files have identical trees (reviewer re-check on five corpora, 7,099 files); no file newly rejected; 85 corpus files still rejected (forms listed under Known gaps). The TypeScript 6.0.3 compiler oracle reports 0 claimed disagreements on eight corpora (gladys 7,525 / 7,525 declarations) and on the fixture (124 / 124). The `await (…)` rule removes 3 invented call sites main emitted (gladys 2, kaneo 1).
- Cost: binary +81,504 bytes; no corpus slower in a timing spot check.
- Gates: local full check 0 FAIL, 175 `/postgres` PASS, check-web ok; CI run 44261667074 on 3153ac9 (`[ci windows]`, stacked with the CI merge-skip rule change) green, postgres raw log 0 FAIL, 176 `/postgres` PASS, Windows 0 FAIL; Grammar assets run 44261666892 green.
