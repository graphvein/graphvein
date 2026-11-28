# Graphvein — Architecture

Read `VISION.md` first. This document describes how Graphvein is built. Decisions that change anything here must be recorded as an ADR in `docs/adr/`.

## Overview

```
                ┌──────────────────────────────────────────────┐
  repo on disk  │                 graphvein (Go)               │
  ───────────►  │                                              │
                │  scanner ─► indexer ─► fact stream (NDJSON)  │
                │                │   (replaceable: external     │
                │                │    process, same contract)   │
                │                ▼                              │
                │             ingester ─► store (SQLite|Postgres)│
                │                              ▲    ▲           │
                │  linkers / extractors ───────┘    │           │
                │  (cross-boundary, API, schema)    │           │
                │                                   │           │
                │  analysis (LLM, cached) ──────────┤           │
                │  review (analyzers, queue,        │           │
                │          dispositions) ───────────┤           │
                │                                   │           │
                │  HTTP API ◄── web UI (React, embedded)        │
                │  MCP server ◄── coding agents                 │
                │  CLI                                          │
                └──────────────────────────────────────────────┘
```

Graphvein ships as a single Go binary. The web UI is built separately (TypeScript/React) and embedded into the binary with `embed`.

## Languages and key libraries

| Concern | Choice | Notes |
|---|---|---|
| Core | Go | Single binary, easy distribution, readable code. |
| Parsing | tree-sitter via WASM (wasitter/wazero) | Pure-Go runtime; ADR 0002. Shared compilation and isolated workers follow ADR 0006; ADR 0008 adds Go/Python/Rust; ADR 0028 adds Angular and the v3 cache namespace. ADR 0012 bounds detached-tree lifetime. JS/TS/TSX/Go/Python/Rust have best-effort structural extraction. ADR 0067 registers the Java, C# and Dart grammars (v4 cache namespace) and adds the best-effort tier: Java types, methods and constructors, and import directives resolved to a file only by a unique fully qualified type name (`internal/lang/besteffort`, `internal/lang/java`; recovered trees claim nothing; `docs/lang/best-effort.md`; extractors-38), and C# types, callables, properties and indexers, with usings that never become file edges (repository namespaces claim nothing, others are `dotnet:` externals; `internal/lang/csharp`); while a file of the language is refused or failed, no import it may declare becomes an external (row BE-8, narrowed to a refused file's lexically provable package or file-scoped namespace); and Dart types, functions, methods, accessors, operators and constructors (classic and Dart 3.13 concise forms), with `import`/`export`/`part` resolved to a file for relative URIs and the file's own package (named by the nearest `pubspec.yaml`'s top-level `name:` line), other repository packages claiming nothing and the rest `dart:`/`pub:` externals (`internal/lang/dart`). ADR 0030 adds HTML/HTM syntax and class-decorator/inline-template cache payloads; a second bounded Angular pass starts after host trees are released. Capture remains source-only; ADR 0031 adds declaration/resource graph facts at link time and diagnoses host capture failures. ADR 0053 adds a gated Python model payload (SQLModel/SQLAlchemy class headers, model bodies, Core table lines and mutation sites, syntax only) to files that import `sqlmodel` or `sqlalchemy` (extractors-29); aggregate extractors-64 (30 from T018f, ADR 0055: provenance attributes on schema records; 31 from T018g0, ADR 0058: model group keys, column claims and verification; 32 from T018g2, ADR 0059; 33 from T018h, ADR 0060: Python model payload version 2 with query sites and parameter annotations, gate G1 ∪ GQ, and the gated sqlx query payload `RustFacts.Queries`; 34 from T018h's binding change; 35 from T018g3, ADR 0061; 36 from T018g5, ADR 0063: the Go model payload; 37 from T018g6, ADR 0064; 38 from T021-B2, ADR 0067: the Java best-effort payload; 39 from T020 S1, ADR 0068: the Go structure contract, `facts.GoModelsVersion` 2; 40 from T020 S2, ADR 0069: the Python structure contract, `facts.PythonFacts` fields `Rejected` and `Unread`, `PythonBinding.Guarded`, declaration spans ending at the `ast` end; 41 from T020 S3, ADR 0070: the TypeScript structure contract, `facts.TypeScriptFacts`; 42 from T020 S4, ADR 0071: the Rust structure contract, `facts.RustFacts` fields `Unread`, `LoneCR`, `Rejected` and `ExternAliases`; 43 from T020 S2X, ADR 0072: workspace members installed as packages join the Python search path, a link-time change; 44 from GORM package selection: GORM reads the files `golang.Package` selects, ADR 0068 known gap closed, a link-time change; 45 from T020-TSG, ADR 0074: patched JavaScript/TypeScript/TSX grammars and the `await (…)` call-site rule; 46 from T023 GR1, ADR 0076: the Go router payload, `docs/lang/go-routes.md`; 47 from T022 (branch-internal 48 and 52), ADR 0075: MR-B3b's Express route attribute `equivalent_registrations` (ADR 0022's equivalence proof, stored) and per-alternative `base`/`base_expression` on client calls with several or conditional alternatives, link-time changes, and MR-B6c's `RouteFacts.AmbientStrings` and `GlobalDefinitions` and the URL-object authority assignment sources, for build-time constant client bases); 48 from T026 (ADR 0079): package manifests, deploy descriptors and auth markers (AG-B1 to AG-B3), the TypeScript bridges payload (renders, passed functions, react-router routes; AG-B5) and the cached JavaScript function parameter flags of B4/DA-E5; 49 from T024 (branch-internal 47 to 56), ADR 0077: the TypeScript `facts.NextFacts` payload (destructured export names, the default-export statement, literal `next.config` keys) for Next.js route handlers and Pages Router API routes, SWR template keys and inline fetchers, literal rewrites re-routed in Next.js order, the TypeScript `facts.ActionFacts` payload (directive prologues, `'use server'` module exports, inline `'use server'` functions with their route scope, JSX expression attributes) for server actions, and Supabase migration sets, table access and generated types, link-time changes; 50 from T028: Express route paths read per Express major (named wildcards, optional groups, refusals; `docs/contract/claims/express5-paths.md`), a link-time change; 51 from T029 SH-G: the Go route shape payload (Go payload v10: `GoFacts.Shapes`, `GoFacts.Marshalers`) and gin/chi/net/http route shapes built from it at link time, a parse-time cache change; `docs/contract/claims/route-shape-go.md`; 52 from T029 SH-F: the FastAPI shape payload (PythonFacts.Shape v2) and FastAPI/Starlette route shapes, a parse-time cache change; `docs/contract/claims/route-shape-fastapi.md`; 53 from T029 SH-X: the Rust shape payload (RustShape v2) and Axum route shapes, a parse-time cache change; `docs/contract/claims/route-shape-axum.md`; 54 from T029 SH-E: the TypeScript shape payload and Express / Pages Router API route shapes, a parse-time cache change; `docs/contract/claims/route-shape-express.md`; 55 from T029 FU-1: FastAPI keyword-unpacking scope, a link-time change; 56 from T029 FU-2/FU-3, SH-N, SH-O: unresolved mount hops and prefix-level path values (Go, Express, FastAPI, Axum), Axum trailing slashes and nest_service, Next.js App Router and server action shapes, committed OpenAPI spec shapes; 57 from T029 SH-S, NXH, W2, W4: security schemes and requirements, gin/chi group middleware, next.config headers rule names, route model-type edges (Go payload v11), request shape differences and Pydantic major reading; 58 from T034: TypeScript destructuring bindings shadow call names (the cached `Shadowed` flag), with link-time client matcher rules LF-1 to LF-3, `docs/extract/clients.md`; 59 from T033 UP-A1, UP-A2, UP-B, UP-C and UP-D, ADR 0084: Go, Express, FastAPI and Axum routes' `unresolved_prefix_paths`, the client matcher's unknown-prefix tier, runtime base URL helpers read as unresolved bases and declared unknown-prefix project links, link-time changes, and the unplugin-auto-import declaration payload `RouteFacts.AutoImports`, a parse-time cache change, and a Next.js route whose `basePath` is not read linked as a suffix match (UP-D, decision U6), a link-time change; and from T035 FM, EM and EM2: Python `Mount(...)` calls kept in the route syntax with their `routes=` list position, a FastAPI app a Starlette mount consumes and an Express app a `use` consumes are no roots of their own, and `routes=[Mount(...)]` and `app.router.mount` compose; at link time `APIRouter.mount` composes only for a proven FastAPI >= 0.137.0, a route a Starlette mount added before it takes loses that path (`route_shadowed`), and a mounted app served by a closed list of forms (`listen`, `createServer(...).listen`, `uvicorn.run`, `[tool.fastapi] entrypoint`) keeps its own root; and from T036: the CommonJS exports payload `RouteFacts.CommonJS` (top-level `module.exports`/`exports` writes) and `const { n } = require("m")` bindings, a parse-time cache change, so a relative `require` composes Express mounts as an ESM import does, `docs/extract/routes.md`; 60 from the `golang.org/x/text` v0.41.0 bump, ADR 0069 Revision 1: Python identifiers NFKC-normalised and `\N{...}` names read with Unicode 17 tables, and the PS-F2 rejection form `unicode_version` for characters whose properties differ from the reference Python's Unicode 16, with its review fixes: `\N{...}` names matched with ASCII case folding only and the form `constant_name` for identifiers whose NFKC form is `True`, `False` or `None`, a parse-time cache change; 61 from T046, ADR 0011 revisions: the TypeScript/JavaScript call binding model, lexical scopes with the cached `Lexical`, `LexicalImport` and `MemberWritten` call flags and the file's `MemberWrittenNames`, a parse-time cache change; 62 from the 2026-11-24 UI stack, ADRs 0088 and 0089: scan diagnostics for deliberately skipped binary, non-UTF-8 and non-regular files, the Go route forms outside the contract counted on the file node (`go_route_forms_outside_contract`), the withheld route path label and the index inputs each run records, stored-fact changes; 63 from T056 and T054, one unreleased bump: the call site of `await f<T>(x)`, which tree-sitter-typescript reads as a call of `await f`, is read through its awaited callee, a parse-time cache change; ADR 0092: Rust row RS-F4 reads a file whose only syntax errors are attributes on struct pattern fields from a verified second parse, and a `.rs` file holding a few raw NUL bytes is read as source, not skipped as binary; 64 from T069: TypeScript route and client expression trees are built once per file node and shared between an enclosing call and a nested call's own record, and `RouteFacts` are cached as one expression table per file with Text stored as source offsets (`internal/lang/facts/exprtable.go`), a parse-time cache change with no fact change). ADR 0059 adds a gated TypeScript model payload (module-level declarations and the default export as an ordered reduced expression tree, syntax only) to files that import a `drizzle-orm/<dialect>-core` module or `drizzle-kit`, or carry drizzle-kit's default config name (extractors-32; `docs/lang/typescript-models.md`); ADR 0061 widens it to version 2 for Sequelize (gate rows G4-G8: a `sequelize` import or `require`, a module-level `init` or a `define` call, a Sequelize options object, an environment selector; module-level expression statements, class headers and destructuring declarations, function bodies, and `new`, function, assignment, index, binary and unary expressions; extractors-35), to version 3 for attribute helper modules (gate row G9, module-level function declarations; T018g3 Revision 2, still extractors-35), and to version 4 for the sequelize-cli forms (module-level for-of loops, `if` statements in bodies, static class members, `<call>[<expr>]` declarations; T018g3 Revision 3, still extractors-35); ADR 0064 widens it to version 5 for TypeORM (gate rows G10-G12: a `typeorm` or `@nestjs/typeorm` import or an ormconfig script name, an `@Entity`-named class decorator, a module-level enum; class decorator expressions, instance members with declared types, enum declarations, array, string and conditional initializers, `else` branches and conditional expressions; extractors-37). ADR 0063 adds the Go model payload (`GoFacts`: struct, named-type, method and string-constant shapes of every non-test Go file; functions, locals, GORM calls with receiver chains and nesting marks, and package-level vars in files that import GORM, its drivers or inflection; syntax only; extractors-36; `docs/lang/go-models.md`). |
| SQLite | `modernc.org/sqlite` | Pure Go, no cgo. |
| Postgres | `pgx` | Team mode. |
| Logging | `log/slog` | Structured. |
| MCP | Official Go MCP SDK, `github.com/modelcontextprotocol/go-sdk` v1.8.0 | Stdio server API only (`NewServer`, `AddTool`, the I/O transport); its client, HTTP, SSE and OAuth parts are rejected in first-party code by the boundary test (ADR 0082). |
| UI | React + TypeScript + Vite + Tailwind CSS + shadcn/ui (Radix) | Embedded in the binary; the build is committed in `internal/server/ui/static`. Node 24 LTS is needed only to build the UI (ADR 0041). |
| 2D graphs | Decide in Tier 2 (React Flow or Cytoscape.js) | Must support nested/compound nodes and large graphs. |
| 3D | three.js (react-three-fiber) | Tier 6. |
| API UI | Swagger UI or Scalar, rendering Graphvein's OpenAPI output | "Try it" requests are proxied through the Go server so guardrails apply. |

**cgo policy:** no production cgo. ADR 0002 selects WASM and permits cgo dependencies only in the isolated T001 native comparison package (`spike/parsing/internal/parse`), behind its build tag.

## The graph model

Everything Graphvein knows is a graph of nodes and edges. Every view (architecture, functional, API, schema, 2D, 3D) is a query over this graph.

### Levels

Every node has an abstraction level. Drill-down moves from lower to higher numbers.

| Level | Meaning | Examples |
|---|---|---|
| L0 | System | The whole repository or product |
| L1 | Component | Server, desktop app, mobile app, database, external service |
| L2 | Area | Module, package, feature area, route family |
| L3 | File | A source file, a migration |
| L4 | Symbol | Function, method, class, route, table, column |
| L5 | Location | A specific line range (the IDE-link target) |

### Node identity

- Each node has a **stable identity**: `kind` + repository-relative path + qualified name (for example `function:server/src/main.rs#app_router`). Identity survives edits to the node's body.
- Each node has a **content hash**: SHA-256 of its source with line endings normalised to LF. Contract v1 preserves all other bytes, including trailing whitespace, to avoid hiding meaningful changes; language-aware normalisation requires a later contract version (Proposed ADR 0005).
- Files also record their **git blob OID** when the repository is a git repo.

Rationale, dispositions and cached LLM output attach to **identity + content hash**, never to a commit hash. The commit on which something was recorded is stored as metadata only.

### Edges

Edge kinds include: `contains`, `imports`, `calls`, `references`, `implements`, `inherits`, `exposes_route`, `calls_route`, `calls_operation`, `reads_table`, `writes_table`, `uses_config`, `depends_on_external`. Each edge carries:

- `provenance`: `fact` | `inferred` (deterministic heuristics, for example URL matching) | `assessment` | `agent_proposed`
- `confidence`: 0–1, always 1 for facts
- `evidence`: source locations supporting the edge

### Annotations

Architecture edges between components can carry protocol, auth and encryption annotations (for example `HTTP/1.1`, `Bearer token`, `TLS: unknown`), each with its own provenance. "Unknown" is a valid, displayed value.

## The indexer contract

The indexer is a replaceable stage. Its output is a **versioned stream of facts** in NDJSON, defined in `docs/contract/`.

- Each record has a `type` (`file`, `node`, `edge`, `diagnostic`), and the stream starts with a header carrying `contract_version`, indexer name and version.
- The built-in Go indexer emits this stream in-process. An external indexer (for example a future Rust indexer for power users) is any executable that accepts the same arguments and writes the same stream to stdout.
- The ingester validates every record against the contract and rejects incompatible versions with a clear error.
- Changing the contract bumps `contract_version`, updates `docs/contract/`, and updates the golden tests.
- Contract v1 streams are complete repository snapshots with validated footer counts. The store diffs bounded write batches in one run transaction and publishes only after valid EOF; malformed framing rolls back the changes. Identity and hashing are specified in `docs/contract/v1.md` and ADR 0005; incremental caching and diffing are specified in ADR 0006.

This is why the Go application never depends on indexer internals: it only ever sees the contract.

## Indexing pipeline

1. **Scan.** Walk the repo, respecting `.gitignore`, `.graphveinignore`, and built-in default ignores (agent working folders such as `.kilo`, `.opencode`, `.claude`, `.codex`, `.agents`, `.cursor`, worktree copies; `node_modules`; `target`; `dist`; `build`; vendored directories).
2. **Diff.** Compare normalized content hashes and extractor version against the store's per-file fact cache. Parse only changed/added supported files; replay cached extraction for unchanged files and omit deleted files.
3. **Parse.** Each file is parsed in isolation. Workers detach and close each guest tree before parsing another file, sending results through a channel with capacity equal to the worker count. One consumer extracts sequentially in arrival order and releases each detached tree before receiving the next; at most workers + channel capacity + 1 detached trees are alive (ADR 0012). Facts remain keyed by path, preserving downstream ordering. Parser or extractor failures and panics become per-file diagnostics; cancellation joins all workers and returns the context error. Each file's parse has a wall-clock deadline scaled with its size (ADR 0090); a file that runs past it in the parallel pass (machine load, not the file) is held back and, once every parallel worker has stopped, parsed again alone on one worker with the capped deadline (12 base timeouts, 60 s by default); only a file that times out again keeps its `parse_failed` diagnostic (T074). Parsing does not depend on timing, so a file the retry reads has exactly the facts of an unloaded run. `graphvein index` prints the retry and the files still not read, and `GET /api/v0/status` (`index.parse_failed`) and `GET /api/v0/architecture` (`parse_failed`) count them, so the UI can say a run's counts are incomplete.
4. **Emit and ingest.** Relink cached and new candidates against the current inventory and scanned module configuration on every run; TypeScript/JavaScript imports, calls and routes share ADR 0014's resolver, Python consumers share ADR 0015's project-root resolver with ADR 0017's implicit roots, and Rust consumers share ADR 0016's Cargo/module resolver. Then emit a complete v1 snapshot. A linker diagnostic the contract rejects (a file of `.`, absolute or with `..`) never aborts the run: it is emitted as `extract_invalid_record`, on its own file when that path is valid, otherwise as a run diagnostic with an empty file, and logged. The store compares payloads, writes only changed graph records and prunes absent keys atomically after footer/EOF validation. Write batches are bounded, while comparison maps scale with snapshot size (ADR 0006).
5. **Link.** Linkers and extractors run over the updated graph (see below). In a multi-repository project (`graphvein index --project`, ADR 0075) each member is indexed as above, into the project store under its own name and index lock, and then a **project link pass** (`internal/projectlink`) runs once under the project lock over every member's stored index in one read transaction: it links client calls and OpenAPI operations of one member to routes of another (`calls_route`, `documents_route`) under the claim contract in [project links](extract/project-links.md), honours `consumes` declarations, selectors and link scopes from the manifest, and stores the links, every source's outcome (linked, ambiguous with candidates, unmatched, incomplete, ...) and a journal entry in one transaction. It never changes a member's own snapshot or identities; a link is fresh only while both members' latest runs and the manifest's declarations are those the pass read, and is never `precise` (it rests on the assumption it shows, plus the row assumptions it lists in `assumes`, such as NX-V1 for a link to another member's Next.js route; [precision](extract/precision.md#cross-repository-links)).
6. **Invalidate.** Cached LLM output and assessments whose subject's content hash changed are marked stale. Dispositions on changed subjects are marked "needs re-review".

Per-language precision is recorded on every file record and file node (`precise` or `best_effort`). The scanner takes it from `internal/precision` (ADR 0066): a central rule table whose rows name the scopes that meet the v1 verification bar, with everything else falling to the single default branch `best_effort`. A file label is its language's structural scope only; fact families (migration schema, ORM models, code-to-table links) get scope labels the server computes from the same table, never stored. Go, Python, TypeScript/TSX/JavaScript and Rust have rows (PL-L1, ADR 0068; PL-L2, ADR 0069; PL-L3, ADR 0070; PL-L4, ADR 0071), so their files are `precise`, except a file whose structure the indexer refused (one go/parser, CPython 3.14 or tree-sitter rejects, for example), which it stores as `best_effort`, and a Rust file no crate root reaches (stored `best_effort` with a `structure_unrooted` diagnostic); every other language is `best_effort` until its own row exists. `TestPrecisionRules` checks each row's evidence, keeps the table 1:1 with `docs/extract/precision.md`, and pins its digest to `index.Version`.

## Linkers and extractors

These run after parsing and add higher-level nodes and edges. Each is a separate package with its own golden fixtures.

- **Angular declarations:** `internal/extract/angular` proves direct external decorators, overlays class roles and metadata, resolves standalone defaults from current project/package/lock inputs, and emits template/resource and module-scope edges with source evidence (ADR 0031). Duplicate declarations respect production/test scope. Requested-token DI facts and counts use the separate injection payload (ADR 0032). Compilation scopes, conservative selector/pipe matching and recovery handling add template fact edges at link time (ADR 0033); ordered import units also prove local pipe precedence in a subset of open scopes (ADR 0034). See [Angular declarations](extract/angular-declarations.md) and [template matching](extract/angular-matching.md). Router facts are linked from the router payload (ADR 0036, [Angular routes](extract/angular-routes.md)).

- **Route extractors** (per framework): Express, FastAPI and Axum are implemented (ADRs 0013–0016). Cache per-file syntax in `internal/lang/typescript`, `internal/lang/python` and `internal/lang/rust`; compose current paths in `internal/extract/routes` every run. Recognize resolved external framework factories, never receiver names. FastAPI prefixes include environment-overridable settings defaults; explicit one-level helper calls bind proven object parameters and preserve trailing-slash path flags (ADR 0019); Axum follows named router factories, constant prefixes, nests and merges. Go (net/http `ServeMux`, chi, gin; T023, ADR 0076): `internal/lang/golang` caches the router syntax of gated files (`GoFacts.Routes`, `Signatures`, `Debug`; `docs/lang/go-routes.md`); `internal/extract/routes` (`gohttp*.go`, `routes.LinkGo`) emits Go route nodes, handler edges and diagnostics from proven router objects (T023) and composes their paths through chi mounts, gin groups and net/http `StripPrefix` compositions from every root (the client matcher's HEAD-for-GET rule covers net/http). Produce source `route` nodes with unprefixed identities, composed path attributes and handler-to-route `exposes_route` edges; see `docs/extract/routes.md`.
- **Client call extractors** (per library): fetch and axios emit source `client_call` nodes from cached syntax, with bounded wrappers and conservative environment-base recognition (ADR 0018). Fetch wrappers also accept the leading environment-base shape, retaining its source spelling; fetch init methods follow source-ordered spreads and invocation options, with unknown methods staying dynamic (ADR 0029). Bounded evaluation follows current constants, tables and eligible helpers without executing code, preserving conditional path alternatives and helper/property evidence (ADR 0020). Angular HttpClient uses source-proven DI, injectable wrappers and application-owned interceptor prefixes (ADR 0022); dynamic authorities remain unresolved across libraries. Config-proven hey-api client-axios SDK calls follow the inventoried transport and configuration merge semantics (ADR 0025). Orval 8 config/spec models identify absent generated exports and application invocation sites, emitting inferred calls_operation edges; bounded URL-object and mutator proofs add route links (ADR 0027). Other generated clients and Dart http/Dio remain planned; Tauri invoke calls are linked separately as IPC (see the Tauri paragraph below).
- **Cross-boundary linker:** after route composition, match each client path alternative by method and normalized template; equal-template slash twins of one FastAPI or Go (net/http, chi, gin) router use the preserved slash flag. Emit one inferred calls_route edge per linked target at the strongest tier confidence. Calls expose linked/ambiguous/unmatched/dynamic/test_only/wrapper status and per-path capped candidates with a full count. Uncalled routes are counted without diagnostics. Impact/callers cross through route, call and owner.
- **Test scope:** a pure path classifier in `internal/testscope` applies built-ins and strict `tests.include/exclude` configuration at link time (ADR 0021). Composition labels paths whose every chain is test-scope; registrations and invocation sites label route/client nodes. Production calls prefer production paths only within the existing winning tier, or expose `test_only` without an edge or weaker fallback. Test calls retain baseline matching. Impact/callers still traverse tests. Scope rules stay unchanged under aggregate extractors-36; see `docs/extract/test-scope.md`.
- **Schema extractors:** `internal/extract/schema` implements sqlx migration sets with PostgreSQL semantics (ADR 0047): convention discovery from Cargo manifests (`migrations/` of a package depending on sqlx, dialect from declared features), sqlx file order, a hand-written Postgres lexer and recognizer, and a schema state that marks anything it cannot apply exactly as partial or opaque. It runs at link time over retained sources, adds no cached payload and emits `migration`, `table`, `column` and `db_index` nodes with `contains`, `alters` and `foreign_key` fact edges; see `docs/extract/schema.md`. Its claims follow a written claim contract (ADR 0051: `contract.go` and the "Claim contract" section, every statement form one row or the default branch that claims nothing), checked by a differential oracle that applies fixture, real and generated migration histories to a real Postgres 17 (test files only; CI's postgres job runs the fast subset). The embedded UI's Schema section (T018b2) reads them through the schema endpoints (ADR 0048). T018c1 caches the SQLModel/SQLAlchemy model syntax that model tables need (ADR 0053, `docs/lang/python-models.md`, from extractors-29). `internal/extract/models/sqlmodel` (T018c2, ADR 0054) resolves it through the Python resolver after the migration pass and emits, through the tool-neutral model kit `internal/extract/models` (T018g0, ADR 0058: records, per-fact `claims` with `verification`, foreign-key resolution, migration comparison, step isolation), model-defined `table`/`column` nodes (`source: "model"`, group `models/<tool>/<root>`, SQLModel's `models/sqlmodel/.`) with `contains` and `foreign_key` edges under the claim contract of `docs/extract/schema-models.md`; it never merges them into migration sets (equal names get a diagnostic). The schema endpoints serve them as model groups with the name overlap both ways, per-fact provenance (`claims`, reason rows, opaque sites), the unread tools found among stored paths and an assumption catalogue (T018f, ADR 0055). `internal/extract/models/prisma` (T018g1, ADR 0057, in progress) finds each package's Prisma schema as the locked Prisma 5, 6 or 7 CLI does, reads it with a hand-written lexer and parser that follows Prisma's grammar and, called once from the indexer after the SQLModel linker, emits its tables, foreign keys and implicit many-to-many join tables through the same kit under the claim contract of `docs/extract/schema-prisma.md`. `internal/extract/models/drizzle` (T018g2, ADR 0059, in progress) finds each package's drizzle-kit configurations (default config name or a literal `--config` in the package's scripts, schema globs against the package directory) and the modules their exports reach through the TypeScript resolver, from the cached TypeScript model payload; its claim contract is `docs/extract/schema-drizzle.md`; the indexer calls it once after the Prisma front end (T018g2), and its relations() entries are table attributes (`orm_relations`), never edges. `internal/extract/tableaccess` (T018h, ADR 0060, in progress) emits `reads_table`/`writes_table` edges from the functions that build queries to the table nodes the schema front ends emit, under the claim contract of `docs/extract/table-access.md`: one plain v1 edge per (kind, owner, table) with every site as evidence, the per-edge data (`table_access`) and the sites it cannot bind (`table_access_unknown`) as owner-node attributes; the indexer calls it once after the Drizzle front end. sqlx query macros and functions are read so far, through a SQL subset reader checked against the Postgres analyzer. `internal/extract/models/sequelize` (T018g3, ADR 0061, in progress) finds each package's Sequelize v6 instance (a module-level `new Sequelize(...)`, its literal or environment-selected options, the dialect from a literal or the single installed driver), the models that reach it (`init` with the instance as `sequelize` option, `define` on it, CommonJS factories called with it) and the association calls it runs (module level, the recognised `associate(models)` loop and the sequelize-cli loops), from the cached TypeScript model payload version 4 with a narrow CommonJS reach for gated files; its claim contract is `docs/extract/schema-sequelize.md` (step SQB2: discovery and contract table; step SQB3: the claim pass for models, attributes and options, with Sequelize's naming rules ported from inflection 1.13.4; step SQB4: references, associations and join tables merged in Sequelize's execution order, their failures and the cyclic-sync rule; step SQB4b: attribute helpers; Revision 3: the review's findings; step SQB5: an oracle that compares every claimed fact with the catalog after Sequelize's own `sync` on scratch Postgres 17 and SQLite, test files and `scripts/oracle/` only), emitted through the kit by `Link`, which the indexer calls once after the Drizzle front end. T018g5 caches the Go syntax GORM models need (ADR 0063, `docs/lang/go-models.md`, from extractors-36); the GORM front end `internal/extract/models/gorm` has discovery (modules and pins from `go.mod`, `gorm.Open` dialect and configuration, migrate sites with the receiver proof and order, through module-own import resolution shared with the Go linker) and its contract table (`docs/extract/schema-gorm.md`), the schema port with per-dialect rendering, and relations, join tables, the migrate-call simulation (`ReorderModels`, parse order) and the call failures, emitted through the kit by `Link`, which the indexer calls once after the Sequelize front end; step GB5 adds an oracle that compares every claimed fact with the catalog after GORM's own `AutoMigrate` on scratch Postgres 17 and SQLite (test files and `scripts/oracle/` only). `internal/extract/models/typeorm` (T018g6, ADR 0064, in progress) finds each package's TypeORM 0.3.x or 1.x data sources, ormconfig files and NestJS `TypeOrmModule` roots and the entity classes they reach, from the cached TypeScript model payload version 5 and the resolver's tsconfig chain, and emits their tables, join columns, junction tables, indexes, constraints and foreign keys through the kit under the claim contract of `docs/extract/schema-typeorm.md`; the indexer calls it once after the GORM front end. Other migration tools and Alembic remain planned.
- **Live schema introspection (optional):** read-only connection supplied by the user; diffs live schema against extracted schema.
- **OpenAPI import:** `internal/extract/openapi` discovers and parses retained JSON/YAML sources on every link pass, resolves scanner-visible references and unique `$self` claims offline, and emits declared api_spec/api_operation nodes with server path alternatives (ADR 0023). After route composition, an exact-only adapter over the existing client matcher supplies operation coverage and inferred documents_route edges, preserving client behavior (ADR 0024). Optional openapi.base_paths replaces server alternatives at link time. OpenAPI declarations precede client recognition so Orval can consume api_operation nodes. OpenAPI adds no cached syntax; the aggregate extractor version is extractors-64 for client and TypeScript syntax. Linked operations also add mapped items to the route shapes (T029 SH-O, `docs/contract/claims/route-shape-spec.md`), never facts. See `docs/extract/openapi.md`; imported specs are never merged into a generated document (below); export and query traversal through documents_route remain planned.

Angular router source syntax is cached separately (ADR 0035,
[router syntax](extract/angular-router.md)). The router payload (version 2, ADR
0036 Revision 1) retains registration calls, application bootstrap calls with
their options, static loader forms, previously uncached arrays, application
config objects and method keys of route-shaped objects. At link time
`internal/extract/angular` proves registrations (provideRouter in
bootstrapApplication/createApplication providers or NgModule decorators,
NgModule forRoot/forChild, platformBrowserDynamic bootstraps), composes full
paths from roots, bootstrap closures (imports and exports) and lazy closures,
and emits
level-4 `ui_route` source nodes with `routes_to`, `loads_children` and
`contains` fact edges, test-scope paths and validation diagnostics (ADR 0036,
[Angular routes](extract/angular-routes.md)). Object hashes come from the
normalized link-time sources; unproven context stays unresolved.

Tauri command syntax is cached in the Rust facts (ADR 0037,
[Tauri commands](extract/tauri-commands.md)). The additive version-3 payload,
absent from files without Tauri syntax, retains command-like function
attributes, generate_handler! entries with their function's attributes,
builder chains, the let bindings (with statement ends) that carry a builder to
invoke_handler, the macros of those flow bodies, cfg(test)-only marks, and app
sites (references to a Builder-like name, typed constructors, aliases, re-exports and macro token trees naming one, other invoke_handler calls, generate_context!). At link time `internal/extract/tauri` proves commands and
registries with the Rust resolver (a registry is final when its builder is
consumed by build or run), recognizes `@tauri-apps/api/core` invoke calls and
their wrappers through the TypeScript module resolver and the client
evaluator, and emits level-4 `ipc_command` and `ipc_call` source nodes
(transport `tauri_ipc`) with `contains`, `exposes_command` and
`invokes_command` fact edges. An invoke links only to the one proven app
registry of the repository's production scope, and only when no other Tauri
app evidence exists there; everything unproven keeps its node with a status
or reason and a tauri_* diagnostic (ADR 0038). Plugin command linking is
T015d.

Next.js server actions (ADR 0077, T024b, [server actions](extract/server-actions.md))
are a third API transport. The TypeScript extractor caches directive
prologues, `'use server'` module exports, inline `'use server'` functions
with their route scope and JSX expression attributes (`facts.ActionFacts`);
at link time `internal/extract/actions` resolves `action` / `formAction`
attributes, `useActionState` / `useFormState` and direct calls through the
route scopes and the module resolver, and emits level-4 `server_action` and
`action_call` nodes with `contains`, `exposes_action` and `calls_action`
fact edges. A direct call's `boundary` is `client` only in a `'use client'`
file or a module reached from one through value imports, else `unknown`.

## Storage

All persistence goes through a `store` interface with two implementations: SQLite (default, in WAL journal mode with `-wal`/`-shm` sidecar files, local disks only; ADR 0049) and Postgres (team mode, configured by URL and credentials). Migrations exist for both and are tested on both.

Where a folder's store is comes from one resolver, `internal/storeloc` (ADR 0093), which every command, the server and project runs ask: `GRAPHVEIN_POSTGRES_URL` (Postgres); else `GRAPHVEIN_SQLITE_PATH` for the folder named on the command line only; else an existing `<folder>/.graphvein/index.db` (a v0.1.1 store, used where it is and never moved implicitly); else the folder's entry in `stores.yaml` (beside `workspace.yaml`); else `<stores>/<name>-<h8>/index.db`, keyed by the first eight hex digits of the SHA-256 of the resolved folder path, in the per-user data folder (`%LocalAppData%\graphvein\stores`, `~/Library/Application Support/graphvein/stores`, `$XDG_DATA_HOME/graphvein/stores` or `~/.local/share/graphvein/stores`; `GRAPHVEIN_STORE_DIR` or `stores.yaml` `default_dir` replace it), created owner-only with a `folder.txt` naming the folder. A folder with more than one candidate store is refused, nothing read or written. Nothing is written into the indexed folder by default. `graphvein index` prints `Index: <path> (...)` first; `graphvein store where|move|list` show, move (copy with `VACUUM INTO`, verified, recorded, then the old files deleted) and list stores.

Graph reads filter nodes and retrieve incoming/outgoing edge sets through the
shared SQL layer in read transactions. CLI query/callers/impact open existing
stores without writes or migrations, calculate status freshness, and traverse
in Go with cycle-safe shortest paths (ADR 0009). Contract v1 is unchanged.

Schema version 3 keeps each node's `name` and `qualified_name` in columns
beside the payload, written from the payload by the same upsert and covered by
the `nodes_names` index. Name lookup uses them only as a SQL prefilter that is
a superset of the matches; the exact byte-sensitive predicate still runs in Go
on the decoded payloads. Freshness reads stored hashes from the `files` table
column (`FileHashes`) and still reads and hashes every scanned file; traversals
fetch only the nodes they reach (`NodesByIdentity`) (ADR 0039).
Views that explain one file's facts read its diagnostics by file
(`Diagnostics`, through the existing `diagnostics_file` index) instead of
decoding the repository with `Dump` (ADR 0048); the schema summary reads
the diagnostics whose file is not stored (`DiagnosticsWithoutFile`, where
the scanner reports what it did not read) to prove that no manifest was
missed. The schema model rules read the few diagnostic codes they need
across all Python files by code (`DiagnosticsWithCodes`, the code read
from the payload in SQL and checked again in Go) (ADR 0055), and the
unread-tools rule reads the GORM v1 markers among the
`schema_model_unread` diagnostics by code and message text
(`DiagnosticsWithMessage`, filtered in SQL and checked again in Go;
T018g5 Revision 2, ADR 0063). The schema
set list reads columns without model claims (`NodesWithoutModelClaims`,
the claims removed in SQL before decoding) and takes each model table's
claim counts from its record (ADR 0057).

Schema version 4 adds the change journal (ADR 0056). The run transaction remembers, the first time it changes, removes or adds a key, the key's payload before the run, and at commit writes the net change per key (`change_entries`, the before payload and a hash of the after payload), one journal run row (`change_runs`: extractor, commit and dirty flag, counts, knowledge gaps), the pin overlays (`change_pin_entries`, first write wins) and retention, all in the run transaction; an unchanged re-index writes one row. `ChangeSet` composes the raw difference between a journal run, a pin or a ref snapshot and the head in one read transaction; it equals a key-wise diff of two `Dump`s, which the store tests and the env-gated oracle check. Branch comparisons index a commit, materialised by `internal/gitref` with local git only, into the same store under the reserved repository identifier `<repository>@ref:<commit>` (`RunOptions.Namespace`: no journal, the main repository's fact cache read first); the claim contract is applied to the raw change sets in Go (`internal/changes`, T019a2), not in SQL.

Schema version 5 adds the project tables (ADR 0075 "Storage"): `projects` (name and the manifest hash of the last link pass), `project_members` (each member with the run the pass read, or the error that stopped it indexing), `project_links` (keyed by kind and the two (repository, identity) endpoints, with no foreign key to `nodes`, so a member re-index keeps links visible as stale until the next pass), `project_outcomes` (every source's outcome of the last pass) and `project_link_runs` (the link journal, 30 passes). `ReplaceProjectLinks` writes a pass in one transaction and refuses one whose member runs are no longer the latest or whose endpoints are not stored with the recorded content hash; former members' rows go with it. `RetireProjectMember` removes a member renamed from the UI in the same request as the manifest write: its project rows and its whole stored index under the old name, in one transaction, refused when another project names it (T022 B11). Readers compute freshness from stored runs and the current manifest (`projectlink.View`); nothing marks links stale in the store. The project lock is a Postgres advisory lock in its own class keyed by schema and project, or the SQLite store's index lock.

Index runs are serialized per store by an index lock behind the `store` interface (`IndexLocker`/`IndexLease`): an OS lock on the empty SQLite file `<store>-lock`, or a session-level Postgres advisory lock keyed by schema and repository, held by `graphvein index` and by `serve`'s re-index from before the scan until the writer store closed, released by the OS or database when the holder exits, and checked before commit; both callers run the same code (`internal/indexrun`) (ADR 0052).

### Team mode

- **Shared:** the committed-code index, rationale, dispositions, assessments and LLM cache, keyed by identity + content hash. Two teammates on different commits see the same rationale wherever their code is identical; where it differs, rationale is shown as "recorded on a different version" with a diff.
- **Local only:** an overlay for uncommitted working-tree changes. It never writes to the shared database.
- **Concurrent rationale:** entries are threads with author and timestamp; nothing is overwritten.

### Source of truth for rationale

Per project, configured explicitly:

- `rationale.source: database` — the database is authoritative; export/import to files is available.
- `rationale.source: files` — rationale lives as small text files under `.graphvein/rationale/`, committed to the repo; the database caches them.

Never both at once.

## Analysis (LLM layer)

- **Provider mode:** OpenAI-compatible HTTP (covers OpenRouter, Ollama, LM Studio) and Anthropic, behind one interface. Required for unattended runs.
- **Agent-driven mode:** the user's own Codex, Claude Code or OpenCode session performs tasks via skill packages calling `graphvein task list|evidence|submit|validate`. Graphvein never handles agent credentials. See ADR 0003.
- **Tasks** (page generation, feature naming, assessment, community naming) declare their inputs. The cache key is the task, prompt version, model and the content hashes of all inputs.
- **Lazy:** analysis runs when a user opens something or explicitly requests a batch, never automatically on every index.
- **Provenance:** all output is stored as `assessment` (or as generated prose that cites node identities). Claims must reference nodes; uncited claims are rejected at validation.

## Review layer

- **Analyzers:** external deterministic tools (for example Semgrep) run optionally; findings are ingested as nodes linked to code locations.
- **Assessments:** rubric-based. Each rubric criterion is pass/fail with a reason; the score is derived from the criteria. Security and quality are separate queues.
- **Dispositions:** `accepted` (with rationale), `fix`, `false_positive`, `deferred`. Bound to identity + content hash; when the content hash changes, the disposition becomes "needs re-review" and shows its old rationale.
- Accepted rationale becomes evidence-backed rationale and is included in future assessment prompts for that node.

## Interfaces

- **CLI:** `graphvein index`, `serve`, `query`, `impact`, `export`, `mcp` (stdio MCP server, [mcp](cli/mcp.md)), `config`, and so on.
- **HTTP API:** consumed by the embedded UI; local by default (binds to 127.0.0.1). `graphvein serve` (`internal/server`) serves HTTP API v0 (`GET /api/v0/status`: extractor version, last run and freshness computed per request like `graphvein status`) and the embedded UI on a loopback IP literal only, through `egress.ListenLoopback`, with a Host allow-list against DNS rebinding, GET/HEAD only (except the re-index POST below), no CORS, a `Sec-Fetch-Site` check on `/api/` and a strict CSP (ADR 0041, [serve](cli/serve.md)).
- **API section endpoints:** `GET /api/v0/api/endpoints`, `/api/v0/api/endpoint` and `/api/v0/api/calls` serve HTTP routes, Tauri IPC commands and Next.js server actions with their stored callers, coverage counts per transport and source root, near misses and gap lists with reasons derived from stored attributes, read through the `store` interface per request with a runs-before/after consistency check (ADR 0042, [serve](cli/serve.md)). The embedded UI's API section (`#/api`) consumes only these endpoints and `/api/v0/status`, maps raw statuses to categories client-side (only `linked` is linked) and uses a native `<dialog>` so the CSP stays unchanged.
- **Flow endpoint:** `GET /api/v0/api/flow?id=&scope=&root=` returns one endpoint's stored neighbourhood, two hops each way (callers, owners, the enclosing function by its stored `contains` edge, handler callees, the induced `calls` subgraph), every edge as stored, near misses as a separate candidates list, with fan-out and size caps; callers and near misses come from the same code as `/api/v0/api/endpoint` (ADR 0045, [serve](cli/serve.md)).
- **Schema endpoints:** `GET /api/v0/schema/summary`, `/sets`, `/table?id=` and `/migration?id=` serve the schema extracted from sqlx migrations (sets, ordered migrations, tables with columns, indexes and stored foreign-key pairs, history, one-hop neighbours, per-migration diagnostics) with stored attributes verbatim and a few derived fields (`completeness`, `default_state`, `objects`, `table_list`, `found`) whose fallback is always unknown; links come only from stored edges (ADR 0048, [serve](cli/serve.md)).
- **Source endpoint:** `GET /api/v0/source?id=&context=` returns the current lines around one stored node's span for the code peek panel. The request names a node, never a path; the node's stored file is read through `os.Root` with an `Lstat` of every component (no symlink followed, regular UTF-8 files up to 4 MiB), and the span is highlighted only when the file is unchanged or the span text still hashes to the node's content hash and occurs once (ADR 0044, [serve](cli/serve.md)). IDE links come from named editor presets or a template (`ide` in [configuration](config.md)).
- **Editor choice:** `GET /api/v0/ide` lists the editor presets with their verification status and the repository's default editor; the four node-presenting endpoints take `editor=<preset>`, which builds links from that preset alone over the repository's `ide` (unknown or empty: 400), and report `ide.editor` and `ide.source`. The UI's per-browser picker (localStorage `graphvein.editor.v1`) sends it; links stay server-built (ADR 0050, [serve](cli/serve.md#editor-for-links)).
- **Re-index:** `POST /api/v0/index/run` (the only non-GET path; `http.CrossOriginProtection`, `Content-Type: application/json`, body `{}`) starts a built-in index run inside `serve` with its own writer store under the index lock; `GET /api/v0/index/run` reports the lock state and the latest job's phase, run-counted progress (never estimated), committed run or error (ADR 0052, [serve](cli/serve.md#re-index-post-and-get-apiv0indexrun)).
- **Change impact:** `internal/changes` applies the T019 claim contract to the store's raw change sets (journal runs, pins, ref snapshots; ADR 0056): every changed record is classified by exactly one contract row (unknown_now and known_now instead of removed and added where a side's knowledge gap or an incomplete schema list does not prove it), causes and partiality are labelled, and the impact traversal follows only the relation rows. `graphvein changes` and `GET /api/v0/changes/{bases,summary,items,impact,refs}` with `POST /api/v0/changes/{pins,pins/delete,refs}` call it; the schema rules it depends on come from the server's schema code (`server.ChangeSchema`) ([serve](cli/serve.md#change-endpoints-adr-0056), [queries](cli/queries.md#graphvein-changes)).
- **Projects:** a project manifest `graphvein-project.yaml` (members by repository name and path, `consumes` declarations with selectors, link scopes; a machine-local `graphvein-project.local.yaml` for paths) is written by `graphvein project init|add`, the UI's manifest editor and `project suggest --accept`, always validated in full and replaced atomically ([project](cli/project.md), ADR 0075). `graphvein index|status --project` index and link it; `callers|impact|changes --project` query across members. `graphvein serve --project` serves every single-repository endpoint per member (`repository=` names it; ambiguous without it: 400) and the project endpoints `GET /api/v0/project` (members, freshness, link pass state and stale reasons, declarations, ambiguous sources, conflicts), `/api/v0/project/callers`, `/impact`, `/links/changes`, `/changes/impact`, `/suggestions` and `GET`/`POST /api/v0/project/manifest` (typed declaration edits, preview diff then apply with the base and result hashes, ADR 0052 checks plus a per-process session token and a loopback peer). The server reloads the manifest after a UI edit and when the files' content hash changes on disk (checked on project requests); links are stale until the next link pass ([serve](cli/serve.md)).
- **Workspace (multi-index server):** one `graphvein serve` serves every index of the workspace registry (`internal/workspace`: `workspace.yaml` in the user configuration directory, written by `serve <path>`, `serve --project` and the UI's add/remove, atomic replace, entries are folder pointers only). Every per-index endpoint takes `ws=<entry id>` (without it, the current entry answers, so single-index URLs are unchanged) and is answered by that index's own route set; at most 8 indexes stay open (least recently used idle one closed), a per-index answer cache covers a fixed list of store-only GET views keyed by the index's fingerprint, and background freshness checks run one at a time. `GET /api/v0/workspace` lists entries (state, freshness once checked, project members, job); `POST /api/v0/workspace/check|add|remove` take typed folder paths behind the ADR 0052 checks plus a session token, a loopback peer and a one-time confirm value (ADR 0087, ADR 0041 revision, [serve](cli/serve.md#workspace-multi-index-server), [claim contract](contract/claims/workspace.md)).
- **OpenAPI generation:** `internal/openapi` builds an OpenAPI 3.1.1 document per repository (per member in project mode) from stored route nodes, their auth and `shape` attributes, imported specs and `documents_route` edges, read through the `store` interface, plus `explorer.path_values` from `graphvein.yaml` (marked "set by you"); one function feeds `graphvein openapi`, `GET /api/v0/openapi` and the UI download. Only `fact` shape items become standard fields; unknowns, refusals, conflicts and unresolved routes are named in `x-graphvein` (claim contract [openapi-generation](contract/claims/openapi-generation.md), ADR 0081, [openapi](cli/openapi.md)). `GET`/`POST /api/v0/explorer/path-values` edits those values with the manifest edit's guards.
- **Search and couldn't read:** `GET /api/v0/search` (jump-to: routes, Explorer operations, tables, symbols and files matched by name or path with a documented tier ranking, bounded per kind, every member of a project in one call, every answer naming what was searched and never stating an absence) and `GET /api/v0/unread` (the "What Graphvein couldn't read" page: files not read, read with syntax errors or holding no fact, scanner gaps, records dropped as invalid, TypeScript structure outside its contract, routes without a path, mount hops, mounts that claim no path, Go route recognition failures, run-time patterns, dynamic and wrapper calls, shape sections not read, depth caps and unresolved in-code call totals from structured file attributes; each part computed by the code that already serves it), read through the `store` interface (`Query.Contains`, `FactFiles`); claim contracts [jump-search](contract/claims/jump-search.md) and [couldnt-read](contract/claims/couldnt-read.md), ADR 0089, [serve](cli/serve.md). The wrong-fact report link is a build-time setting served by `GET /api/v0/ui` (`report_url`, null until the repository is public).
- **MCP server:** `graphvein mcp` (`internal/mcp`, ADR 0082, [mcp](cli/mcp.md)) is read-only over deterministic facts (M3), on stdio only: no listener, no connection. Its fourteen tools (`status`, `find`, `endpoints`, `endpoint`, `source`, `callers`, `impact`, `chain`, `table`, `changes`, `change_impact`, `unresolved`, `overview`, `link_changes`) reuse the query package, the HTTP API v0 views through `server.Reader` (the serve handlers in process, without a socket, GET views only) and `internal/changes`, never a second implementation of a rule; the change tools read only stored bases and never index. Answers are compact by default, and every field left out is named with how to get it (a `detail` parameter or another tool). Every response is one envelope: index freshness checked per call, the request after defaults, unknowns, assumptions, every cap that bound, paging that says what was not shown, and the labels in use ([claim contract](contract/claims/mcp-server.md)). Later tools in M5: `rationale`, `dispositions`, `findings_for`, `record_decision` (agent write-back, stored as `agent_proposed`).
- **IDE links:** a configurable template, default `vscode://file/{abs_path}:{line}:{column}`.

## Network egress

All outbound network access goes through a single `egress` package. It enforces the configured mode:

- `offline`: no outbound connections except explicitly configured infrastructure (database host, optional live API base URL for "try it").
- `llm`: additionally allows configured LLM providers.

The package gates three things: dials, DNS lookups of names, and listening sockets. A `Policy` holds an allow-list of destinations (host, port and purpose); a name is resolved only if it is an allowed destination, and the resolved addresses are dialable only for that destination's port. HTTP clients are built on the same dialer and ignore proxy environment variables. Listeners are allowed only on loopback IP literals. Blocked attempts fail closed with an error wrapping `egress.ErrBlocked`. Postgres connections are opened only through `egress.OpenPostgres`, which allows exactly the hosts of the parsed connection configuration (ADR 0040).

Three tests enforce the boundary: a static scan of first-party Go source rejects dialing, listening, resolving, default HTTP clients, pgx imports and non-SQLite `sql.Open` outside the package; a reviewed list bounds the dependencies that import networking or subprocess packages on Linux, macOS and Windows; and a runtime test asserts that an offline index, status and query run makes no egress attempt and does not use the default resolver or HTTP transport; `graphvein serve` has its own runtime test with the same tripwires, in which the only network operation is one allowed loopback listen (ADR 0041); a `graphvein mcp` session (initialize, tool list, every tool) has one in which there is none at all, and the static scan allows only the MCP SDK's stdio server API in first-party code (ADR 0082). Limits: subprocesses (external indexers, `git`) are outside in-process control; the operating system may fetch certificate data during TLS verification; a dependency that dials an IP literal with its own dialer is visible only through the dependency review, and one that opens sockets through raw system calls not even there; the runtime test does not see names answered from the hosts file.

No other package may open network connections directly. This guarantee covers Graphvein's own connections; user-supplied external indexers are outside its network controls (ADR 0010).

## Configuration

The current schema supports `version: 1`, optional `repository`, optional
`ide` keys `link_template`, `column_unit` and `column_base`, and optional
`clients.base_paths` for explicit unresolved call bases, and optional
`tests.include/exclude` for test-scope classification, and optional
`openapi.include/exclude` for spec discovery using the same pattern syntax,
and optional `openapi.base_paths` for explicit operation deployment paths,
and optional `egress.mode` (`offline` or `llm`, default `offline`).
See [project configuration](config.md) for strict decoding and
flag/config/basename precedence. Other settings below describe the target scope.

`graphvein.yaml` in the project root, with environment variable overrides for secrets. Covers: storage backend and URL, ignores, IDE link template, rationale source, egress mode, LLM providers and per-task model routing, analyzer paths, API "try it" environments (base URL, label, auth header from an environment variable; ADR 0081).

## Repository layout (target)

```
cmd/graphvein/          CLI entry point
internal/scan/          file walking, ignores, hashing
internal/parse/         tree-sitter integration (per T001 / ADR 0002)
internal/lang/<lang>/   per-language fact extraction
internal/contract/      NDJSON contract types, validation
internal/ingest/        stream -> store
internal/store/         interface, sqlite/, postgres/, migrations/
internal/link/          cross-boundary linker
internal/extract/       routes/, routeshape/, clients/, schema/, models/, openapi/
internal/analysis/      providers/, tasks/, cache
internal/review/        analyzers/, rubric/, dispositions
internal/mcp/           MCP server (stdio, read-only; ADR 0082)
internal/openapi/       OpenAPI document generation from stored routes
internal/server/        HTTP API, embedded UI, try-it proxy
internal/server/ui/static/  built UI assets, committed so `go build` needs no Node (ADR 0041)
internal/egress/        the only package allowed to dial out
web/                    React + TypeScript UI sources (own go.mod fence; build output goes to internal/server/ui/static)
testdata/fixtures/      small synthetic repos with golden outputs
testdata/corpus/        pinned references to external test repos
docs/                   VISION, ARCHITECTURE, contract/ (incl. claims/), adr/
```
