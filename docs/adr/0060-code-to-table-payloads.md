# ADR 0060 - Code-to-table linking: query payloads and table access edges

**Status:** Accepted (delegated review, 2026-11-14)

Code-to-table linking (`docs/contract/claims/table-access.md`, project plan M2).
Number reserved by the T018h investigation; written with the change that fixes the
cached payload shapes, gates and versions for Python and Rust. The
TypeScript fidelity fields are a separate section, added when
that unit lands; nothing in the Python and Rust decisions depends on them.
The edge, unknown-record, SQL reader and oracle decisions are fixed by the
task file's claim contract; steps H-B3 to H-B6 implement them and add their
implementation notes here.

## Context

T018h emits `reads_table` and `writes_table` edges from the functions that
build queries to the `table` nodes the schema sources emit (sqlx migration
sets, SQLModel, Prisma and Drizzle groups), so change impact (T019, row I8)
can answer "what uses this table in code". The link pass sees cached facts
and raw sources, never a tree (ADR 0059's payload decision). The investigation checked
the caches field by field:

- Python `HelperCalls` reduce a nested call's arguments to their kind
  (`session.exec(select(User))` loses `User`), drop keyword values, carry no
  owner identity, and `PythonScope.Parameters` holds names without
  annotations (`session: AsyncSession` is lost).
- Rust returns at every `macro_invocation` and caches calls without
  arguments: no SQL text is cached.
- TypeScript caches every call with owner, scope and reduced arguments;
  object spreads and tagged-template substitutions are its only gaps.

## Options considered

1. **Link-time re-reading of retained sources** (a lexer per language over
   the raw bytes). Rejected: it needs its own tokenizer for Python strings
   and Rust raw strings, comments and macro token trees, and owners would
   come from span containment instead of the extractor.
2. **Widen the ungated caches** (`HelperCalls` arguments, Rust call
   arguments for every file). Rejected: the cost lands on every file and
   every consumer of those fields.
3. **Gated, versioned payloads next to the existing model payloads**, capture
   filtered by the cheapest syntactic test that keeps every site a contract
   row can claim or refuse. Chosen.

## Decision

### Python: model payload version 2

`facts.PythonModels` (ADR 0053) becomes version 2
(`facts.PythonModelsVersion`; version 1 entries miss once) with:

- **`Queries []PythonModelQuery`**: the outermost call of each call chain
  with a link named `select`, `insert`, `update`, `delete`, `text` (function
  form: a bare name, or a dotted name whose first segment an import of the
  file binds, or a bare import alias of one of these names such as `insert
  as pg_insert`), `get`, `get_one` (method form, first positional argument a
  dotted name) or `add`, `add_all`, `delete`, `merge`, `refresh` (method
  form, first positional argument not a literal). A site keeps `Span`,
  `Scope`, `Owner` (innermost emitted function, method or class; a
  module-level constant for its initialiser; else the file node) and
  `Expression`, the reduced chain: a link chained on a non-dotted receiver
  holds it in the new `PythonModelExpr.Object`. Arguments are reduced to a
  depth of 4; keyword values keep names only, because no contract row reads
  them. A captured chain's arguments are examined as chains of their own.
- **`Annotations []PythonModelAnnotations`**: per function scope holding or
  enclosing a site, the annotated parameters a site names (first segment of
  a dotted name or callee), reduced as member annotations without text.
- **Gate G1 ∪ GQ.** G1 is unchanged (an absolute import of `sqlmodel` or
  `sqlalchemy`). GQ admits a file with a captured session-method call
  (`get`, `get_one`, `add`, `add_all`, `delete`, `merge`, `refresh`) whose
  receiver is a dotted name with a last segment containing `session`
  (case-insensitive; 20 production sites in files importing
  neither package), or whose first segment is a parameter of an enclosing
  function annotated with a type whose text contains `session` (`db:
  DbSession`). This widens the task's GQ (five methods, receiver spelling
  only), which missed three langflow inventory sites; see Known gaps for the
  one site still outside. A GQ-only file gets `Queries` and
  `Annotations` only, so model linking (T018c2) reads exactly the files and
  records it read before.

Shape and rules: `docs/lang/python-models.md`, "Query sites".

### Rust: `RustFacts.Queries`

`[]facts.RustQuery`, gated on a `use` of `sqlx` or a site path starting
with `sqlx::`: the sqlx query macros (`query!`, `query_as!`,
`query_scalar!`, their `_unchecked` forms) and query functions (`query`,
`query_as`, `query_scalar`, `query_with`, `query_as_with`) spelt with one
segment or a first segment `sqlx`. A site keeps `Path` segments, `Macro`,
the SQL argument (the first; the second for `query_as!`) decoded when it is
a string literal, normal or raw, or for a macro a `+` concatenation of
literals (`Literal`, `SQL`, `SQLSpan`), `Span`, `Owner` (the call capture's
`Source`) and `Scope` (innermost lexical scope, for resolving `use
sqlx::query` through `RustScope.Imports`). Macro arguments are split on the
commas between token-tree children, so commas in strings and comments never
split. `RustFacts.QueriesValid()` is checked on every cache read. The
payload is versioned by the aggregate extractor version, like the rest of
`RustFacts`. Shape and rules: `docs/lang/rust-sqlx.md`.

### Versions

One aggregate extractor version bump, extractors-33, covers both payloads
(and the TypeScript fields if H-B2 lands in the same release; otherwise H-B2
takes the next free number).

### TypeScript fidelity fields

No new TypeScript payload: every call is already cached ungated as a
`facts.ClientCall` (callee chain, reduced arguments, owner, scope, span).
Two additive fields close its two gaps, in both reductions the cache holds
(`routeExpr` for module-level binding values and `RouteCall` arguments;
`urlExpr` for `ClientCall` arguments, which replace the `routeExpr`
arguments while the callee chain stays from `routeExpr`):

- **`RouteExpr.Spread bool`** (JSON `Spread`, omitted when false): an object
  literal with at least one spread element, at any depth
  (`{...criteria, include: {...}}`). The `routeExpr` object map dropped
  spreads silently, so a spread object looked complete; the `urlExpr` object
  kept the spread as an unnamed `Keywords` entry, which a reader had to scan
  for. The spread operand itself is not added to `Properties`.
- **`template_substitution` reduced in `Object`**: a tagged template
  (``sql`... ${t} ...` ``, ``prisma.$queryRaw`...` ``) is a call whose
  arguments are the template's children (`string_fragment` and
  `template_substitution`); a substitution kept only its `Text`. It now
  holds the reduced inner expression (first non-comment child) in `Object`,
  by the same reduction as its call (`users.id` as a member chain,
  `eq(users.id, id)` as a call with arguments). Untagged templates are
  unchanged: their substitutions were already reduced into `Parts`.

No consumer reads either field (the full suite and the fixture goldens are
unchanged); the version bump is extractors-33, shared with the Python and
Rust payloads. Receivers stay unproven (parameters are bound without type
annotations; a callback parameter such as `tx` is not linked to the call
receiving the callback), so TypeScript ORM rows are `inferred` (O1). Shape:
`docs/lang/typescript-models.md`, "Call cache fidelity fields".

### Edges, unknown records, SQL reader, oracle

Fixed by the task file and recorded here as those units land: the first
emission of `reads_table`/`writes_table` (owner rule W1, aggregation W2,
provenance `fact` or `inferred` at confidence 0.9), the CT-Q1/CT-Q2 SQL
subset reader, the Postgres analyzer oracle (`debug_print_parse` permission
bits), contract row ids `CT-` and assumption ids `TA-V`.

Where the per-edge data lives (task Revision 2). Contract v1 edges have no
`attributes` field (type, kind, source, target, provenance, confidence,
evidence; an unknown edge field is dropped with `unknown_field`), so the
edges stay plain v1 edges whose evidence lists every site, and the owner
node carries two open attributes, the linker-attributes-on-existing-nodes
pattern of the Angular linker:

- `table_access`: one entry per edge, sorted by (kind, target), `{kind,
  target, tool, rows, operations, assumes, role, verification}` (W2,
  Revision 1's `role` and `verification`); consumers join an edge with its
  owner's entry by (kind, target);
- `table_access_unknown`: one entry per site a row recognises but cannot
  bind (W5), `{row, line, span, tool, reason, partial, candidates}`,
  including the `statement_not_traced` records of executions (row D).

No `contract_version` bump and no contract change: the edge kinds exist in
contract v1 and node attributes are open.

Step H-B3 notes (sqlx, the reader, the oracle; `docs/extract/table-access.md`):

- A one-segment sqlx path is sqlx's only through a `use` of its module's
  scopes (a nested module does not see its parent's `use`).
- The reader is the package's own small Postgres lexer and a clause
  scanner; it refuses a whole statement it cannot read, never part of it.
- `role` for sqlx: `executes` when a call of a sqlx execution method of the
  same owner holds the site; executions holding no captured site are
  `statement_not_traced` records (method names only: the caches keep no
  receiver types or bindings).
- A migration table with `schema: null` and a search_path reason (`O3.P13`)
  is not resolved: it may live outside `public` (Revision 2).
- Oracle: `scripts/oracle/sql_tables.sh` and `TestSQLTablesOracle`; the
  `EXCLUDED` entry is identified exactly (range table entry of relkind `c`
  and alias `excluded`), so A2 never drops a real read of the target.

Step H-B5 notes (Prisma rows CT-R1..CT-R3, `site_partial`):

- The Prisma table records carry no relation fields; the Prisma front end
  exports an accessor, `prisma.Relations(sources)`, that returns what its
  claim pass already parses (per claimed schema: its set, the packages
  that load it, each model's relation fields with the related model,
  list, implicit many-to-many side and unread relation attributes). No
  record changes. The link pass reads join tables from the existing
  `join_table` attribute.
- Prisma sites are read from the ungated TypeScript call cache
  (`facts.ClientCall`); every one is `inferred` (TA-V3a) and `executes`.
- `site_partial` (Revision 3) is a new reason: a site that keeps its claims
  while other tables it may touch are not named by its row (an implicit
  many-to-many join table, a relation in `orderBy` or `_count`, a spread,
  an argument that is not an object literal; SQLAlchemy's implicit FROM
  through a call entity's arguments). The site record holds further
  unknown entries next to its refusal for it.
- Oracle: `sql_tables.sh` builds a Prisma group's schema with the pinned
  CLI's `prisma migrate diff --from-empty` (no connection) for CT-R3.

Step H-B6 notes (Drizzle rows CT-Z1..CT-Z4; Revision 4):

- Payload correction (TypeScript call cache): the client reduction
  rebuilt each cached call's top-level expression, and set its
  `URLScope`, from maps keyed by start byte only. Every call of a chain
  starts at the same byte, so each one got the innermost call's text and
  arguments (`db.select().from(users)` cached `from` with `Args=[]`). Both
  maps are now keyed by the call's whole span. It rides on the branch's
  unmerged `extractors-33` bump; no golden changed (no consumer read the
  wrong arguments: the clients, Angular and route consumers read the
  inner calls, which were right). Chained-call tests pin it.
- The link pass reads the Drizzle table and column records, the cached
  model payloads and the shared module resolver (`Input.Drizzle`,
  `DrizzleModels`, `Modules`), and calls `drizzle.Discover` for the
  configs' schema modules (relational query keys, W6). No record change.
- W7 for Drizzle (Revision 4): a table const the resolver proves to be a
  group's table is claimed in any package; CT-Z3 needs a group of the
  site's package (its schema binding).
- Oracle: `sql_tables.sh` builds a Drizzle group's schema with the pinned
  `drizzle-kit export`; CT-Z4 SQL and the `toSQL()` SQL of CT-Z1/CT-Z2
  builders (`sql_tables_drizzle.mjs`, drizzle-orm's `pg-proxy` database,
  no connection) go through the analyzer. CT-Z1/CT-Z2 are `oracle` for
  postgresql groups only.

Step HF notes (Revision 5, the independent review's findings):

- Python bindings (shared resolver, R5-B2): the syntax walk binds the
  target of every `as_pattern` (`except E as X`, `with f() as X`, `case P
  as X`) in the enclosing scope as an unstable binding with no value,
  like an augmented assignment, so a model name rebound there no longer
  resolves to the class. Unlike a `for` target it does not mark the
  module-level name unstable: the local binding shadows it by scope
  lookup. This changes cached Python facts, so the aggregate extractor
  version moves to extractors-34 (every cached fact misses once).
- Python execution through a local (R5-A1) reads the execution call's
  argument from the retained source (the cached `Calls` keep no
  arguments) and the binding's value offset; no payload change.
- Per-file isolation (R5-B3): `tableaccess.Output` gains `Diagnostics`
  (`table_access_internal_error`); the index call runs under a recover
  guard. No record change.
- SQL reader (R5-B1): the Postgres keyword functions in FROM position
  are no table; no payload change.
- Prisma fluent API (R5-C1) and write predicates (R5-C3) add
  `site_partial` entries on rows CT-R2 and CT-P2; no new reason code.

## Consequences

- Python files in G1 ∪ GQ and Rust files using sqlx carry a larger cache
  entry; other files' entries are byte-identical apart from the version.
  The T018h H-B1 report records the census (files gated, sites captured)
  and the langflow store size delta.
- Every cached Python, Rust and TypeScript fact misses once (extractors-33);
  Python version 1 model payloads also fail `Valid()`.
  **(HF)** And once more with extractors-34 (the R5-B2 binding change).
- The payloads are syntax: the linker still resolves every name (select to
  sqlalchemy, `query` to sqlx, a model class through imports) and claims
  only what a contract row allows.

## Known gaps

- Python: a session call whose receiver is neither session-named nor
  annotated so (langflow `api/v1/memories.py:109`, `db: Any`) in a file
  outside G1 is not captured (1 inventory site of 2,647 on fastapi and
  langflow).
- Python: a statement extended after assignment (`stmt = stmt.join(N)`) is
  not captured; receivers reached through `await` or a call keep `Object`
  `other`; SQL-shaped literals passed to unlisted calls (row D's
  `sql_carrier_unmodelled` for driver-level `cursor.execute("...")`) are
  not in `Queries`. H-B4 must read them from another field (the cached
  `HelperCalls` keep top-level literal arguments and a scope index, but no
  owner) or report a payload change. **(H-B4)** Driver-level executions
  are read from the cached `Calls` (owner and span, no arguments): every
  `exec`/`execute`/`scalar(s)`/`stream*` call holding no statement site is
  a `statement_not_traced` record on its owner. A `sql_carrier_unmodelled`
  record needs the literal argument with an owner, which no Python field
  keeps: that is a payload change, reported, not made.
- TypeScript: `Spread` says only that a spread exists; its operand and the
  order of entries around it are not in the `routeExpr` map (the `urlExpr`
  `Keywords` keep both for `ClientCall` arguments). A tagged template is
  reduced only where the existing reductions reach it: inside an array or a
  function body that `routeExpr` keeps as text, it stays text.
- Rust: sites inside another macro's token tree are not parsed and not
  captured; a record type with a comma splits wrongly (the site is then
  refused, never misread); `query_scalar_with`, `query_file*` and
  `#[macro_use]` macros are outside the gate or the list; SQL-shaped
  literals passed to other calls in a sqlx file are not captured.

## Review notes (delegated review, 2026-11-14)

Accepted after the green `[ci windows]` run 43573327834 on 76f2c11
(Windows and postgres green; postgres job: 0 FAIL, only the env-gated
skips; CI's Postgres is 17.12).

- Results: 17 contract rows (CT-P1..P5, CT-R1..R3, CT-Z1..Z4, CT-S1/S2,
  CT-Q1/Q2, D) 1:1 in code, docs and task; edges stay plain contract v1
  edges, edge data on the owner node (`table_access`,
  `table_access_unknown`); extractor version extractors-34.
- Oracles (0 disagreements on every claimed site they compare): Postgres
  analyzer on SQL text (fixtures; corpora axum 24, axum-ddd 29,
  langflow 68, kaneo 19, umami 1), SQLAlchemy statement compile on
  CT-P1/P2 fixture sites (25), drizzle `toSQL()` on CT-Z1/Z2 fixture
  sites of postgresql groups (32); `oracle` labels only on compared forms
  (`TestTableAccessClaims`); P3, P4, R1, R2, Z3 are `mapped`.
- Revisions: 1 (O1-O6: inferred receivers, builder ownership,
  verification labels), 2 (owner-node data, `statement_not_traced`),
  3 (code users are a lower bound; `site_partial`), 4 (chained-call
  cache fix), 5 (review: build-then-execute role, FROM-position function
  keywords, except/with `as` shadowing, per-file guards, Prisma fluent
  and write-predicate partials).
- Coverage per corpus and the site-by-site reconciliation with the investigation inventory: the T018h task record (internal).
- Assumptions shown with the facts: TA-V1..V4 (named receivers for
  inferred edges, schema sources, search_path).
- Known gaps (labelled, no wrong fact): `with ... as db` sessions,
  destructured bindings, comparison operands in predicates, Python and
  TypeScript `sql_carrier_unmodelled`, generic tagged templates, stored
  Drizzle builders read as executing, Rust executions through locals,
  langflow's CT-P5 oracle schema taken from Graphvein's own records.
- After v1: helper substitution for generic repositories (umami
  `rawQuery`), a TypeScript receiver-type payload, column-level access,
  the remaining ORMs' query forms with their ORM tasks.
