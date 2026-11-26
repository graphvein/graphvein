# TypeScript/JavaScript structure oracle (T020 S3)

`oracle.cjs` compares what Graphvein emits for TypeScript, TSX and
JavaScript files (`.ts .tsx .mts .cts .js .jsx .mjs .cjs`, declaration
files included) with the TypeScript compiler's reading of the same raw
bytes. The investigation built it (the T020 task record (internal)); the implementation (ADR 0070)
made it the oracle of the claim contract `docs/lang/typescript-structure.md`:
`TestTypeScriptStructureOracle` (`internal/lang/typescript`, opt-in with
`GRAPHVEIN_TS_ORACLE_DIR` pointing at the scratch install below, Node from
`PATH` or `GRAPHVEIN_NODE`, corpora through `GRAPHVEIN_TS_ORACLE_CORPORA`)
runs it on the fixture and fails on any claimed fact the compiler does not
confirm (`stats.claimed_disagreements`).

## Install (scratch only)

The compiler comes from npm at the versions pinned in `package.json` and
`package-lock.json` (both Apache-2.0):

- `typescript@6.0.3`: the primary oracle. The last release of the
  JavaScript compiler, with the `ts.*` API (`createSourceFile`,
  `resolveModuleName`, `getParsedCommandLineOfConfigFile`). The corpora pin
  TypeScript 5.4 to 7.0; 6.0 is the bridge release whose defaults 7.0
  keeps (an unset `moduleResolution` computes to `bundler` unless `module`
  says otherwise) while it still reads `moduleResolution: node10` and
  `baseUrl`, which 7.0 removed. Graphvein's own `web/` pins 6.0.3.
- `typescript59` (`npm:typescript@5.9.3`): a differential run only
  (`--ts 59`). 5.9 computes an unset `moduleResolution` differently
  (`node10` for `module: commonjs`, `classic` for ES modules), so
  disagreements between the two runs show configurations whose meaning
  depends on the TypeScript version.
- TypeScript 7 (`typescript@7.x`) is the native (Go) port: it ships a
  binary and no JavaScript `ts.*` API, so it is not used.

Install outside the repository, without lifecycle scripts:

```bash
mkdir -p "$SCRATCH/oracle" && cp scripts/oracle/ts_structure/package*.json scripts/oracle/ts_structure/oracle.cjs "$SCRATCH/oracle/"
(cd "$SCRATCH/oracle" && npm ci --ignore-scripts --no-audit --no-fund)
```

## Run

```bash
graphvein index <checkout> --repository <name> --emit "$SCRATCH/<name>.ndjson"   # GRAPHVEIN_SQLITE_PATH in scratch
node scripts/oracle/ts_structure/oracle.cjs --ts-dir "$SCRATCH/oracle" --snapshot "$SCRATCH/<name>.ndjson" --root <checkout> --out "$SCRATCH/out" --name <name> [--ts 59]
```

It writes `<name>.summary.json` (counts, form census, disagreement classes
with examples) and `<name>.rows.jsonl` (one row per disagreement). It
reads the checkout and never writes to it, never runs package scripts or
corpus code, and needs no network. `TRACE_SPEC=<specifier>` prints the
compiler's resolution trace for that specifier.

## What it compares

- File nodes: span and content hash of every TS/JS file of the snapshot,
  from the raw bytes (CRLF and lone CR as one line end each; contract v1).
- Declarations: the identity rules of `docs/lang/identity.md` applied to
  the compiler's AST (`ts.createSourceFile` over the raw bytes, UTF-16
  offsets mapped to UTF-8 byte offsets, BOM kept); per node kind, identity,
  name, qualified name, span, content hash, `exported`, `default_export`,
  `decorators`, and the source and evidence of its `contains` edge.
- Imports: every `import` and `export ... from` statement (and
  `import x = require()`); the statement span against the `imports` edge
  evidence, and the target against `ts.resolveModuleName` with the options
  of the file's governing configuration (below), the import's resolution
  mode (`ts.getModeForUsageLocation`) and a filesystem host over the
  checkout plus virtual `node_modules/<name>` links for declared workspace
  members (package.json `workspaces`, pnpm-workspace.yaml `packages`).
  Assets (`.svg`, `.css`, `.json` without `resolveJsonModule`) go through
  the same call with `allowArbitraryExtensions`, a `x.d.svg.ts` lookup
  standing for an existing `x.svg`; a relative specifier naming an existing
  file is that file.
- Governing configuration: the nearest ancestor `tsconfig.json` or
  `jsconfig.json` whose program (`getParsedCommandLineOfConfigFile`:
  `files`, `include`, `exclude`, `allowJs`, `extends` chains, package-name
  `extends` through the workspace links) contains the file, directly or
  through its `references`; else the defaults of the editor's inferred
  project in TypeScript 6.0 (`allowJs`, `jsx: preserve`, latest target,
  `resolveJsonModule`; `moduleResolution` computes to `bundler`).

## The contract's expected outcome (the implementation)

- A file tree-sitter rejects (`syntax_error` in the snapshot) is expected
  to claim no declaration and no import (row TS-F2), whatever the compiler
  reads.
- Declarations follow the contract's span conventions (`declare` outside
  the span, body-less class members without `;`, a type alias's trailing
  line comment), give no node to an exported binding pattern, and read
  nothing inside a namespace or module block.
- `import x = require()` and sites inside namespace or module blocks are
  not claim sites.
- An import site expects: the compiler's file, unless the same resolution
  with `noDtsResolution` finds another file (then nothing); for a bare
  name the compiler does not resolve, nothing when a workspace link of the
  package is in reach, an external of the package name when it is a valid
  npm name, else nothing. An edge or external Graphvein emits that differs
  from the expectation, an edge where nothing is expected, or an edge
  without a site is a claimed-fact disagreement; a site where Graphvein
  claims nothing is counted (`no_claim:*`), not a disagreement.
- Workspace links are the package managers' (declared members only,
  duplicate names linked to neither); through a link, an output that the
  member's `tsconfig.json` emits into its `outDir` or `declarationDir`
  (`ts.getOutputFileNames`) exists when its source does and stands for it.

## What it does not compare

Calls, export bindings and namespace members (`Exports`), routes, client
calls, model facts, `require()` and dynamic `import()` targets (counted
only), triple-slash references and `import()` types (counted only), the
external node's attributes beyond its package name, and packages under
`node_modules` (the checkouts have none: a bare specifier the compiler
cannot resolve in the repository is compared as external or unresolved).
The naming rules themselves are transcribed from `identity.md`; a rule that
both sides share (for example the name of a destructured export) is only
checked by the census, not by the comparison.
