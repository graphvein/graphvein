# TypeScript, TSX and JavaScript structure: claim contract

Status: contract of the T020 implementation (2026-11-17,
`extractors-41`), revised by the review fixes (still
`extractors-41`: TS-I5's declared-package rule, `${configDir}` in `paths`,
TS-F2 scoped to structural records), ADR 0070. The rows are kept 1:1 with `structureRows` in
`internal/lang/typescript/structure.go` by
`TestTypeScriptStructureContract`, which also checks that the fixture
`testdata/fixtures/typescript-structure` reaches every row and the default
branch, and that the section "Default branch D" names every form.
Grammar patches (ADR 0074, Proposed, T020-TSG, `extractors-45`): the
JavaScript, TypeScript and TSX grammars are built from patched sources that
accept keyword JSX attribute names (`class=`, `for=`, `in=`, `default`), a
bare `&` in JSX text and attribute strings, and `export type *`. On the
pinned corpora this takes 656 files out of TS-F2 (gladys 640, dokploy 9,
langflow 3, aqualink-app 2, kaneo 1, maintainerr 1); TS-F2
keeps 85 corpus files (`f<typeof import("x")>()` 75, tagged templates with
type arguments 4, newline-separated `in`/`instanceof` members 2, unnamed
function-type parameters 2, a newline-separated generic call signature 1,
and 1 file the compiler rejects too). Per-file census:
the T020 task record (internal).
Export bindings (calls scope): a name that a file's `export type *` and
`export *` of different modules both supply binds nothing (ambiguous: the
compiler reports TS2308 and takes the type-only one, the emitted
JavaScript keeps the value one), so no `calls` edge goes through it
(T020 review).
`TestTypeScriptStructureOracle` (opt-in: it needs Node and the pinned
compiler, `GRAPHVEIN_TS_ORACLE_DIR`) compares every claimed fact with the
TypeScript compiler (`scripts/oracle/ts_structure/oracle.cjs`). Rows change
only through a revision of task T020 (or a later task) with a version bump.
Inventory and the investigation results: the T020 task record (internal); the
developer decisions on its open points O-S3-1..O-S3-8 are written into the
rows below. Label rule: a future row PL-L3 of `docs/extract/precision.md`,
added only after the independent review; until then TypeScript and
JavaScript files stay `best_effort`.

Scope (T020 decision O5 (a)): files with extension `.ts .tsx .mts .cts
.js .jsx .mjs .cjs` (declaration files `.d.ts .d.mts .d.cts` included),
their declarations, and their import and re-export statements, as emitted
by `internal/lang/typescript` (`Extract`, `ModuleResolver`,
`LinkWithResolver`). Calls, export bindings (`Exports`, barrels),
routes, client calls, Angular and model facts are other scopes (calls stay
row D, `code_calls`); they read the same resolver, so a refused import
binds no name for them either.

**Every form no row below names goes to the single default branch D: it
claims nothing (no node, no edge). Forms inside the scope that D receives
are counted (one `ts_structure_unread` diagnostic per file); forms outside
the scope ("Not modelled") are not counted.**

Input text: the scanner's text (CRLF and lone CR as LF; contract v1).
JavaScript also ends a line at U+2028 and U+2029, and a `//` comment at a
lone CR, U+2028 or U+2029; tree-sitter and the compiler agree on all four
(inventory section 1), so no file is refused for them. Spans are contract
v1 spans (UTF-8 byte columns; the compiler's UTF-16 offsets are converted
by the oracle).

Compiler semantics (O-S3-8 (a)): "the compiler" below is the TypeScript
6.0 compiler (`typescript@6.0.3`; 7.0 keeps its defaults and has no
JavaScript API). Graphvein transcribes its configuration reading and its
module resolution (`moduleNameResolver.ts`) in Go over the scanned
inventory (`tsconfig.go`, `resolve.go`, `workspace.go`); it never reads a
`node_modules` directory. Paths are compared case-sensitively.

Status column: **oracle** = `TestTypeScriptStructureOracle` reports 0
disagreements on every claimed fact of the row on the fixture and on the
corpora of ADR 0070; **census** = counted, not compared.

## Configurations and packages (inputs to the import rows)

| row | form | claim | refusal (to D) | status |
|---|---|---|---|---|
| TS-C1 | a scanned `tsconfig.json` or `jsconfig.json` | a file's options are those of the configuration found by walking from the file's directory to the root, in each directory `tsconfig.json` then `jsconfig.json`, whose program contains the file directly or through `references` (transitively). Read as the compiler reads it: JSONC; `extends` (string or array; a relative path, with `.json` appended when needed, or a package name resolved as the compiler's configuration lookup through a TS-C3 link), later options overriding earlier ones, relative path options resolved against the configuration that sets them, `${configDir}`; the program is `files`, plus the `include` matches (default `**/*`) not matched by `exclude` (default: `outDir`, `declarationDir`) with the supported extensions (`.js .jsx .mjs .cjs` only with `allowJs`, `.json` only from an `include` entry ending in `.json` with `resolveJsonModule`), keeping per base name the extension the compiler prefers; `jsconfig.json` defaults `allowJs` and `noEmit` | a configuration Graphvein cannot read (a parse error, an `extends` it cannot resolve in the repository, a circular `extends`, an option it reads with a value of the wrong type or an unknown enum value) is unread; when the walk meets an unread configuration before a containing one, the file's options are unknown: its bare specifiers go to D, counted (`config_unread`; the family extractors still read one as the package when rule (ii) of TS-I5 holds for its package name and no TS-C3 link or scanned `package.json` name carries it; never an `imports` edge), and a relative specifier claims only the file that `node10`, `bundler` and `nodenext` resolution all agree on (else D, `config_unread`); `moduleResolution: classic` (computed for `module` `none`, `amd`, `umd`, `system`) is not modelled: D, `config_unread` | oracle |
| TS-C2 | a file the walk finds in no program | the editor's inferred-project options: `allowJs`, `resolveJsonModule`, `jsx: preserve`, latest target (so `module` computes to `esnext` and `moduleResolution` to `bundler`) | - | oracle |
| TS-C3 | a `package.json` with `workspaces` (array or `{ packages }`), or a `pnpm-workspace.yaml` with a block `packages:` list | each directory below the root that its globs select (`*`, `?`, `**`, `!` exclusions; wildcards never match a dot directory or `node_modules`) and whose scanned `package.json` has a string `name` is linked as `<root>/node_modules/<name>`, as the package managers link it; through a link, a file the member's own `tsconfig.json` would emit into its `outDir` or `declarationDir` (the compiler's output names: `rootDir`, else the configuration's directory, as in TypeScript 6.0; `.js`/`.jsx`/`.mjs`/`.cjs` and, with `declaration` or `composite`, `.d.ts`/`.d.mts`/`.d.cts`) exists when its source file does and stands for it (O-S3-3) | two members of one root with one name: neither is linked (`duplicate_workspace_package` warning on each); a `package.json` that is not a declared member is never a resolution target; no virtual output from an unread configuration, `noEmit`, `outFile`, a declaration or JSON input | oracle |

## Files

| row | form | facts claimed | refusal (to D) | status |
|---|---|---|---|---|
| TS-F1 | a scanned file with one of the extensions above | file record and level-3 `file` node, `language` `typescript` (`.ts .mts .cts`, declaration files), `tsx`, or `javascript` (`.js .jsx .mjs .cjs`, JSX included); the file exists at this path with this content hash | - (the scanner's ignore rules decide the inventory) | oracle |
| TS-F2 | a file tree-sitter reports an error in (an `ERROR` or `MISSING` node), whether or not the compiler accepts it (O-S3-1 (a)) | the file node only (plus the indexer's `syntax_error` warning); the file's structure label is not precise | every declaration and import site tree-sitter recovered goes to D as one count (`syntax_error`); no declaration node, `contains`, `imports` or `calls` edge, and no `calls` edge from another file into one of its declarations. The family extractors (models, table access, client calls, routes, Angular, Tauri) still read the file as before under their own contracts: a family fact owned by one of its declarations is owned by the file node, and a family edge pointing at one is not emitted | oracle |
| TS-F3 | a file not parsed (over the size limit, parse timeout or failure, an extraction failure) | the file node only | - | oracle |

## Declarations (every level)

Owners: a declaration's qualified name is its owner path joined with `.`
and its name. Owners are the emitted function, method and class
declarations that enclose it, plus object owners (TS-D9); anonymous
functions, callbacks and blocks add no segment. Repeats (TS-D12) are
counted after naming.

Spans (O-S3-5 (a), Graphvein's conventions written down): from the first
decorator or modifier of the declaration (`export`, `default`, `async`,
`abstract`, `static`, accessibility) to its end, except: `declare` (and an
`export` before it) is outside the span of an ambient declaration; a
body-less class member ends before its `;` (a body-less top-level function
includes it); a type alias without `;` ends after a line comment that
follows it on its last line; a variable-bound function (TS-D4) and an
exported variable (TS-D11) span their declarator, from `export` when
exported. The content hash covers exactly the span. Every row emits one
`contains` edge from the nearest emitted owner (the file when none) with
`provenance: fact`, confidence 1 and the span as evidence.

| row | form | node kind, qualified name | refusal (to D) | status |
|---|---|---|---|---|
| TS-D1 | function declaration with a name (`async`, generator, `export`, `export default function f`) | `function`, owner + name | - | oracle |
| TS-D2 | body-less function declaration (overload signature, `declare function`) | `function`, owner + `name@overload` + the parameter list and return type annotation as written, whitespace and comments outside literals removed | - | oracle |
| TS-D3 | `export default function () {}`, `export default function* () {}`, `export default class {}`, `export default <arrow function>`: the anonymous function or class that **is** the default-exported value | `function` `default.function`, `class` `default.class`, `function` `default.arrow`; `exported`, `default_export` | anything else default-exported (a call, an object, a parenthesised function) is an expression: what is nested in it is read by the other rows, with no flag and no `default.*` name | oracle |
| TS-D4 | `const`/`let`/`var` declarator with an identifier binding whose initializer is directly an arrow function, function expression or generator function | `function`, owner + binding name (a named function expression's own name is ignored) | tree-sitter reads the initializer as a call whose callee is an unparenthesised arrow function (the `=> tag<T>`...`` misparse; valid TypeScript calls an arrow only in parentheses): the declarator goes to D, counted (`misparse`), and so does any such call elsewhere | oracle |
| TS-D5 | named function expression anywhere else (`cb(function f() {})`, `module.exports = function main() {}`) | `function`, owner + its name | - | oracle |
| TS-D6 | class declaration or named class expression (`abstract` included) | `class`, owner + its name; `decorators`: the decorators' text | an anonymous class expression that is not TS-D3 claims nothing, members included (not counted) | oracle |
| TS-D7 | class member method, getter, setter, constructor (`static`, `async`, generator, private `#name`, computed or quoted names as written) | `method`, class + name as written (`constructor` for constructors); a body-less member (overload, `abstract`) appends `@overload` + signature as TS-D2; `decorators`: every decorator before the member, comments between them and the member included | - | oracle |
| TS-D8 | object literal method, getter or setter | `method`, owner + name as written | - | oracle |
| TS-D9 | object owners: an object literal that initializes an identifier-bound `const`/`let`/`var` adds the binding name as owner segment; a nested object literal under an identifier- or string-keyed property adds the key | (no node; naming only) | computed and numeric keys, and literals not directly bound (arguments, returns, array elements, spreads, `export default { }`), add no segment (not counted) | oracle |
| TS-D10 | interface, type alias, enum (`const enum`, `declare enum`), at any level | `interface`, `type_alias`, `enum`, owner + name; members not read | - | oracle |
| TS-D11 | exported `const`/`let`/`var` declarator with an identifier binding whose initializer is not TS-D4 | `variable`, name; `exported: true` | a binding pattern (`export const { a, b } = x`, `export const [a] = x`) goes to D, counted (`binding_pattern`); its initializer is still read by the other rows | oracle |
| TS-D12 | two or more declarations in one file with the same kind and qualified name (declaration merging, getter and setter, redeclarations, `export default` twice) | the first keeps the name, later ones append `@2`, `@3` in source order (identity.md) | - | oracle |

**Attributes**: `exported: true` only on a declaration an `export`
modifier names (TS-D1..D3, D6, D10, D11, and the declaration of an `export
declare` statement); `default_export: true` only on TS-D3 and `export
default function f`/`class C`. Nothing nested in an exported or
default-exported declaration or expression is exported (PS-S3-1).

**Type positions are never read for declarations** (PS-S3-2): type
annotations, type arguments and parameters, type literals, `as`,
`satisfies` and `<T>` assertion types claim nothing (not counted).

**Namespaces** (`namespace N {}`, `module N {}`, `declare module "x" {}`,
`declare global {}`, `declare namespace N {}`; O-S3-6 (a)): the block and
everything in it go to D, counted once per block (`namespace_member`):
member declarations, import sites, calls.

Not modelled (claim nothing, not counted): class fields and index
signatures (their initializers are read for TS-D5, TS-D8 and nested
declarations), static blocks as nodes, parameters (their default values
are not read), unexported non-function variables, type members, enum
members, `export =`, `export { x }` clauses (export bindings are the calls
scope), CommonJS assignments other than TS-D5 (`exports.x = () => {}`),
JSX, comments and directives.

## Imports

Sites: each `import ... from 's'` (default, named, namespace, type-only,
`import { type x }`), side-effect `import 's'`, `export * from 's'`,
`export * as ns from 's'` and `export { ... } from 's'` (type-only
included) of a file TS-F2 does not refuse is one claim site, except inside
a namespace block. The evidence span of each edge is the statement.
Several sites naming the same target merge into one edge with several
evidence entries.

Resolution is the compiler's `resolveModuleName` with the file's TS-C1 or
TS-C2 options and the site's resolution mode
(`getModeForUsageLocation`: the file's format from its extension and,
under `node16`/`nodenext`, the nearest `package.json` `type`; ESM for an
import declaration in an ESM-format file), over the scanned inventory plus
the TS-C3 links, for `moduleResolution` `node10`, `node16`, `nodenext` and
`bundler`: `paths` (exact key, else the longest prefix, the first on a
tie; targets in order; a matched pattern whose targets fail goes on to
packages, never to `baseUrl`), `baseUrl`, `rootDirs`, `moduleSuffixes`
(only the empty suffix), extension substitution and probing in the
compiler's order (in one pass for `bundler`/`node16`/`nodenext`; TypeScript
and declaration files before JavaScript for `node10`), directory indexes
and `package.json` `typings`/`types`/`main` (not in ESM mode), `exports`
and `imports` with the mode's conditions (`import` or `require`, `types`,
`node` except under `bundler`, `customConditions`), patterns and arrays,
self-name references, `@types` and `typeRoots`.

Then the result must hold:

- **implementation agreement (O-S3-2 (c))**: the same resolution with
  `noDtsResolution` (the compiler's implementation-only lookup) finds the
  same file or none; when it finds another file (`x.mjs` for `x.d.mts`, a
  `main` `.js` for its `.d.ts`, a directory index for a sibling `.d.ts`)
  the site goes to D, counted (`declaration_or_implementation`);
- **modelled lookups only**: a lookup that reaches a `package.json` with
  `typesVersions`, a versioned `types@` condition, a non-empty
  `moduleSuffixes` entry, an `exports`/`imports` target inside the
  importing project's own `outDir` or `declarationDir` (the compiler's
  `tryLoadInputFileForPath` guess), a `package.json` Graphvein cannot
  parse, or `preserveSymlinks` through a link: D, counted
  (`unsupported_package`).

| row | form | facts claimed | refusal (to D) | status |
|---|---|---|---|---|
| TS-I1 | relative specifier (`./`, `../`) the compiler resolves to a scanned TypeScript or JavaScript file | `imports` edge file → that file | the two checks above; ESM mode of `node16`/`nodenext`, where the compiler refuses an extensionless or directory specifier, is TS-I7 | oracle |
| TS-I2 | relative specifier of an asset: an extension the compiler does not load as a module (`.json`, `.svg`, `.css`, ...), resolved by the compiler's own path logic with `<x>.d<ext>.ts` standing for an existing `<x><ext>` (the oracle's `allowArbitraryExtensions` host), else naming an existing scanned file | `imports` edge file → that file | the two checks above | oracle |
| TS-I3 | bare specifier the compiler resolves without a workspace link (`paths`, `baseUrl`, self-name reference, `typeRoots`) | `imports` edge file → that file | the two checks above | oracle |
| TS-I4 | bare specifier the compiler resolves through a TS-C3 link (`exports` with the mode's conditions, else `typings`/`types`/`main`, else `index`; an unbuilt entry through TS-C3's virtual outputs), or a `#` specifier through the `imports` of the file's package | `imports` edge file → that file | the package name is linked in reach of the file but the compiler finds no entry (an unbuilt `dist` its `tsconfig.json` does not prove, a subpath the `exports` map does not export): D, counted (`workspace_entry_missing`; O-S3-3) | oracle |
| TS-I5 | any other bare specifier the compiler does not resolve in the repository, whose package name (first segment, two for a scope) is a valid npm package name (lower case letters, digits, `-`, `.`, `_`, not starting with `.` or `_`, optionally `@scope/`), that no `paths` key of the file's options matches, and that is either a Node built-in (the whole specifier is in Node 24's `module.builtinModules`, with or without `node:`; `node:sea`, `node:sqlite`, `node:test`, `node:test/reporters` only with it) or a name a scanned `package.json` in the file's directory or an ancestor declares in `dependencies`, `devDependencies`, `peerDependencies` or `optionalDependencies` | `imports` edge to `external:.#npm:<package name>` (`ecosystem: npm`): the file imports a package of this name that is not in the repository; nothing about where it lives | a matched `paths` key whose targets are not in the inventory (generated or ignored files, the compiler would go on to `node_modules`): D, counted (`paths_target_missing`); a name that is not a valid npm package name (`~`, `~icons`, `@/x`, `#x`, `virtual:x`, `astro:x`, upper case): no external, D, counted (`not_a_package`; O-S3-4 (a)); a name a scanned `package.json` outside the declared workspaces carries (it may be a `file:` or `link:` dependency): D, counted (`unlinked_package`); any other name (no declaration in reach: bundler aliases, virtual modules, `baseUrl` or test-runner paths of files outside the inventory, packages installed but not declared): D, counted (`undeclared_package`) | oracle |
| TS-I6 | web-root specifier (`/x`) | the existing `external:.#web:/x` node with `unresolved: true` and an `unresolved_import` warning | - | oracle |
| TS-I7 | relative specifier the compiler cannot resolve in the scanned inventory | no edge; `unresolved_import` warning | - | oracle |

Not sites (D, counted per file; O-S3-7 (a)): `import x = require('s')`
(`import_equals`), `require(...)` calls (`require_call`), dynamic
`import(...)` (`dynamic_import`), `import('s')` types, triple-slash
references, JSDoc `@import`. A type-only import or re-export with import
attributes (`with { "resolution-mode": ... }`) changes the site's mode,
which Graphvein does not model: D, counted (`import_attributes`).

## Default branch D

Claims nothing. Counted per file when the form is in the scope, in one
`info` diagnostic `ts_structure_unread` whose message ends with the count
per form, sorted: `(form=n, form=n)`. Forms: `syntax_error` (TS-F2; such a
file counts no other form), `misparse` (TS-D4, and a call whose callee is
the bare identifier `await`: tree-sitter-javascript reads `await (x).y()`
as a call of a function named `await`, so that call records no call site,
not even an unknown one, while the calls inside its operand are read; a
bare `await(x)` is already an `await_expression` and is not affected; in a
script that declares `function await`, a real call written `await (1).y()`
is dropped the same way, an under-claim counted `misparse`; ADR 0074), `binding_pattern`
(TS-D11), `namespace_member` (one per namespace or module block),
`config_unread` (TS-C1), `declaration_or_implementation` (import checks),
`unsupported_package` (import checks), `workspace_entry_missing` (TS-I4),
`paths_target_missing`, `not_a_package`, `unlinked_package` and
`undeclared_package` (TS-I5), `import_attributes`,
`require_call`, `dynamic_import`, `import_equals` (not sites).

## Precision stops of the investigation (all closed in the implementation)

| stop | rows | observed in the investigation (16 corpora + self) | closed by |
|---|---|---|---|
| PS-S3-1 | TS-D3, attributes | 186 rows in 11 corpora: `default.*` nodes for callbacks inside `export default <expr>`, renamed owners, `exported`/`default_export` on nested methods | flags only on the declaration an `export` names; `default.*` only for the default-exported value itself |
| PS-S3-2 | type positions | 20 `method` nodes from type literals | type positions not visited |
| PS-S3-3 | TS-D4 | 5 wrong kinds, 19 omissions (opencollective-api) | misparse to D |
| PS-S3-4 | TS-F2 | 355 wrong, 775 missing declarations in 744 files tree-sitter rejects | whole file to D |
| PS-S3-5 | TS-C1, TS-I3 | 152 edges from a non-governing configuration | the compiler's governing configuration (`files`/`include`/`exclude`, `references`) |
| PS-S3-6 | TS-C3, TS-I4 | 642 edges to `src/index` fallbacks, 4 to non-member packages (+ probe: `src/index` before `index`, `exports` encapsulation) | declared members only; the compiler's package algorithm; an unbuilt entry only through the member's own output mapping |
| PS-S3-7 | TS-I1 | 14 edges to an implementation where the compiler resolves a declaration file | implementation agreement check |
| PS-S3-8 | TS-I1 | probe only: ESM-mode extensionless and directory specifiers | resolution mode and `moduleResolution` from the options |
| PS-S3-9 | TS-D11 | 11 variables named by a binding pattern | binding patterns to D |
| PS-S3-10 | TS-D7 | 2 members losing decorators across a comment | decorators bind across comments |
