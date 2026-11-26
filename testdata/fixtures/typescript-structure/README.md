# typescript-structure fixture

TypeScript, TSX and JavaScript structural claim contract
(`docs/lang/typescript-structure.md`, ADR 0070, task T020, the implementation),
rebuilt from the investigation probe (section 6 of the T020-S3 inventory, an internal record)
with the cases the implementation added. Nothing here is built or run; several files are
deliberately not valid for tree-sitter (`src/broken.ts`, `src/comp.js`,
`src/amp.tsx`, `src/typeofimport.ts`) and `src/decls.ts` holds forms the
compiler would report.

Tests:

- `TestTypeScriptStructureFixture` (`internal/lang/typescript`) indexes this
  directory and checks `testdata/fixtures/typescript-structure.golden.json`,
  then a warm replay.
- `TestTypeScriptStructureContract` checks that the fixture reaches every
  contract row, the default branch and every default-branch form.
- `TestTypeScriptStructureOracle` (opt-in, `GRAPHVEIN_TS_ORACLE_DIR`)
  compares every claimed fact with the TypeScript 6.0.3 compiler.

The repository stores every file with LF line endings, so every test writes
four more cases into its temporary copy (`structureExtraFiles` in
`internal/lang/typescript/structure_harness_test.go`): `src/crlf.ts`
(CRLF), `src/lonecr.ts` (a lone CR ending a `//` comment), `src/bom.ts` (a
byte order mark and multi-byte characters) and `src/large.js` (over the
2 MiB parsing limit, TS-F3). No file uses an ignored name (`dist` appears
only as a string in `package.json` and `tsconfig.json`), so no inert names
are needed. The investigation probe's symlink case is not stored (the scanner
skips symlinks and Windows checkouts cannot hold them), nor its two file
names differing only in case.

| path | rows | case |
|---|---|---|
| `package.json` | TS-C3, TS-I5 | `workspaces: ["packages/*", "!packages/nonmember"]`; `dependencies` and `devDependencies` declaring `left-pad` and `@scope/dev-tool` |
| `src/externals.ts` | TS-I5, D | Declared packages (a scoped subpath included), Node built-ins (`fs/promises`, the `node:`-only `node:test`) are externals; `test` (no built-in without `node:`), `esm-peer` (declared only in `esm/package.json`, not above this file), a `baseUrl` path `src/generated/client` with no file (`undeclared_package`) and a failed `@/` alias (`paths_target_missing`) are not |
| `esm/package.json` | TS-I5 | `peerDependencies` declaring `esm-peer` for `esm/src/main.ts` |
| `cfgdir/`, `base/tsconfig.configdir.json` | TS-C1, TS-I3 | A base configuration's `paths` target `${configDir}/lib/*` resolves against the extending configuration's directory (`cfgdir/lib/y.ts`) |
| `src/rejected-client.tsx`, `src/calls-rejected.ts` | TS-F2 | A file tree-sitter rejects keeps its family facts (the `fetch` client call, owned by the file node) and claims no declaration, import or calls edge; a call into its declarations from another file is not an edge |
| `tsconfig.json`, `base/` | TS-C1, TS-I3 | relative `extends`, `bundler`, `baseUrl`, `paths` (`@/*`, a two-target `multi/*`), `allowJs`, `resolveJsonModule`, `include` |
| `packages/pkg-a` | TS-I4 | `exports` (`.`, `./sub`, a `./internal/*` pattern), `imports` `#util`, a self-name import (`src/self.ts`); `@probe/a/src/hidden` is not exported (`workspace_entry_missing`) |
| `packages/pkg-b` | TS-I4 | `main`/`types` in an absent `dist`, `index.ts` at the root (the compiler's `index`) |
| `packages/pkg-c` | D | `main: lib/main.js` beside `lib/main.d.ts` (`declaration_or_implementation`) |
| `packages/pkg-d` | TS-C3, TS-I4 | an unbuilt `dist` its `tsconfig.json` proves (`outDir`, `rootDir`, `declaration`): the import is `src/index.ts` |
| `packages/pkg-e` | D | `outDir` without `rootDir`: TypeScript 6.0 emits `dist/src/index.js`, not `main` (`workspace_entry_missing`) |
| `packages/pkg-t` | D | `typesVersions` (`unsupported_package`) |
| `packages/pkg-x`, `pkg-y` | TS-C3, D | two members named `@probe/dup`: neither linked (`duplicate_workspace_package`, `unlinked_package`) |
| `packages/nonmember` | D | a non-member named `lodash` (`unlinked_package`) |
| `esm/` | TS-I1, TS-I7 | `"type": "module"`, `nodenext`: extensionless and directory imports refused by the compiler |
| `solution/` | TS-C1 | `files: []` and `references` to a configuration with `paths` |
| `unread/` | TS-C1, D | `extends` a package not in the repository: bare imports and a relative import the resolution kinds disagree on go to D (`config_unread`); one relative import they agree on keeps its edge |
| `legacy/` | TS-C1, D | a `jsconfig.json` (defaults `allowJs`) with `baseUrl` and `paths`; `legacy-pkg` is declared nowhere (`undeclared_package`) |
| `scripts/build.ts`, `cjs/` | TS-C2 | files in no program: the inferred-project options; `require` calls (`require_call`) |
| `src/imports.ts` | TS-I1..I7, D | every site form: type-only, side-effect, JSON, SVG, namespace, `paths`, workspace entries, `node:fs`, `import = require` (`import_equals`), a declaration file beside a directory and beside `.js`/`.mjs` implementations, `export *` forms, dynamic `import()`, `require`, an import in `declare module` (`namespace_member`), a type-only import with `resolution-mode` (`import_attributes`), `~icons`, `virtual:`, a failed `@/` alias (`not_a_package`), a web-root import, a missing relative import |
| `src/decls.ts` | TS-D1..TS-D12, D | every declaration form: overloads, ambient, default function and class, bound arrows and function expressions, destructured exports (`binding_pattern`), decorators with a comment between, accessors, `#private`, computed and quoted names, constructor overloads, object owners, type literals, merged interfaces, namespaces (`namespace_member`), the `sql<T>` misparse (`misparse`), duplicates |
| `src/spans.ts` | TS-D2, D6, D7, D10, D12 | the span conventions: `declare`, body-less class members, a type alias ending in a line comment, a merged interface |
| `src/defaults.ts`, `src/defarrow.ts`, `src/assign.ts` | TS-D3 | `export default defineConfig({...})` (nothing exported), `export default <arrow>`, `export =` |
| `src/broken.ts`, `src/comp.js`, `src/amp.tsx`, `src/typeofimport.ts` | TS-F2 | files tree-sitter rejects (the last three the compiler accepts): the file node only (`syntax_error`). Since ADR 0074 the patched grammars accept keyword JSX attribute names and a bare `&`, so `comp.js` uses `import=` (a name the patch does not cover) and `amp.tsx` an unnamed function-type parameter `(string) => void` (form G5 of the T020-TSG inventory, an internal record) |
| `src/recovered/keyword-attrs.jsx`, `src/recovered/ampersand.tsx`, `src/recovered/star-hub.ts` | TS-F1, TS-D1, TS-I1 | ADR 0074 patches P1-P3: reserved-word JSX attribute names (`class`, `for`, `in`, `default`, `new`), a bare `&` in JSX text and an attribute string, `export type *` and `export type * as ns`; each file parses and claims its declarations and imports |
| `src/recovered/use-hub.ts`, `src/recovered/types-a.ts`, `src/recovered/types-e.ts` | calls (T020) | `dup` reaches `star-hub.ts` through a type-only star (`types-a`) and a value star (`types-e`): ambiguous, no `calls` edge; `onlyValue` binds through the value star (`useHub` → `types-e.ts#onlyValue`) |
| `src/awaitparen.js` | call-site misparse | tree-sitter-javascript reads `await (x)` as a call of a function named `await`: no call site (no unknown target) for it, counted `misparse`; the call inside the operand (`inner` → `helper`) is still an edge |
