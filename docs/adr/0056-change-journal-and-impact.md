# ADR 0056 - Change journal, comparison bases and change impact

**Status:** Accepted (delegated review, 2026-11-14)

## Context

Change impact is the first user-facing M2 feature after the schema work
(project plan M2; VISION "Change impact"). Developer decisions of 2026-11-08 and
2026-11-09 fix its scope:

- It is an add-on to understanding the code, not a PR-review product: it
  shows what a change does to the codebase's verified facts and what
  depends on them. No PR bot, bundled linters, autofix, test generation or
  chat learnings; LLM summaries belong to M3 and findings to M4. A CI
  command that posts one PR comment through egress stays a later decision.
- Three comparison inputs: since the previous index run, against a pinned
  baseline, and branch versus base (two git refs).
- Storage: a bounded change journal written at re-index, holding the old
  payloads of changed and removed facts so pinned baselines work; no full
  snapshots per run.
- The unit of change is the fact at the levels people read (routes, client
  calls and their links, callers, migration- and model-defined tables and
  columns, migration verdicts, IPC commands, architecture edges), each
  linked to its line.
- Impact is a deterministic traversal outward from the changed facts with
  honest confidence, under its own claim contract (like T018a2): a fact
  that goes from known to unknown is "unknown now", never "removed";
  extractor upgrades are labelled apart from code changes; a partly failed
  run makes the comparison partial.
- Surfaces: a "Changes" section in the UI shell plus markers in existing
  views (after a mockup round), and the MCP tools (M3, read-only server) so coding agents can
  check their own changes; the query API is specified now so MCP reuses it.

What exists today:

- Contract v1 streams are complete snapshots (ADR 0005). Within one run
  transaction the SQL store loads every stored file, node and edge payload
  into `old` maps, upserts only payloads that differ, and prunes absent
  keys at `Commit` (ADR 0006, `internal/store/sqlstore`). The transaction
  therefore already knows, for every key, the payload before the run and
  whether the run changed or removed it. It forgets the old payload when it
  overwrites the map entry.
- Edges cascade on node deletion; `DeleteFile` (a changed duplicate file in
  one stream) removes nodes, incident edges and evidence edges directly.
- Diagnostics are republished every run and are not diffed.
- Node identity is `kind:path#qualified-name`; renames produce new
  identities by design (ADR 0005). L5 location nodes encode their range.
- `store.Dump` returns a deterministic projection of one repository, used
  today to prove that an incrementally updated store equals a fresh index.
- CLI `impact`/`callers` traverse incoming edges breadth-first in Go with
  cycle-safe shortest paths and report blind spots (unresolved call sites
  whose callee name matches) (ADR 0009, ADR 0039).
- Server endpoints read through the `store` interface with a runs-before/
  after consistency check (ADR 0042, ADR 0045, ADR 0048); the only
  state-changing endpoint is the re-index POST under the index lock
  (ADR 0052).
- The built-in indexer does not fill the optional `Header.Commit`; the
  scanner already runs `git rev-parse` for blob OIDs.
- The fact cache keeps the latest entry per repository and path, keyed by
  content hash and extractor version (ADR 0006).

## Options considered

### 1. Where comparisons come from

- **1A Snapshot diff on demand.** Keep nothing new; to compare, re-index
  the base and diff two `Dump`s. Works for git refs only (a past index run
  cannot be re-created when its tree is not a commit, for example a dirty
  working tree), costs a full re-index per comparison and makes "since the
  previous run" as expensive as a branch comparison.
- **1B Full snapshot per run.** Copy the graph at every run (or at pins).
  Simple reads; store size grows by a full graph per kept run (Langflow:
  about 200,000 records per copy), violating the "no full snapshots"
  decision.
- **1C Change journal written in the run transaction.** Record, per changed
  key, the payload before the run. The transaction already holds it; the
  extra writes are bounded by the rows the run writes anyway. Previous-run
  and recent-run comparisons compose journal entries with the current
  graph.

### 2. Pinned baselines

- **2A Pins block journal pruning.** Exact but unbounded: a pin made a year
  ago keeps a year of journal.
- **2B Pin overlay.** At every commit after the pin, store for each changed
  key the payload it had at the pin, once (insert if absent). The overlay
  is bounded by the number of distinct keys changed since the pin, at most
  the graph size plus the facts removed since, independent of the number
  of runs.
- **2C Pin as full copy.** Option 1B for pins only; bounded by pin count
  but every pin costs a full graph.

### 3. Branch versus base

- **3A Second store.** A separate SQLite file or Postgres schema per ref.
  Needs a second store configuration, Postgres schema-creation rights in
  team databases, and duplicates the fact cache.
- **3B Same store, separate repository namespace.** Index the ref into the
  same store under a reserved repository identifier; the graph tables
  already partition by repository. The fact cache can be read from the
  main repository's entries, so only files that differ from the indexed
  working tree are parsed.
- **3C Diff without storing the base.** Index the base into memory and diff
  against the store. The traversal of removed facts needs the base graph
  stored anyway (callers of a removed route exist only in the base).

How a ref's files reach the scanner:

- `git worktree add`: writes into the user's `.git` (worktree metadata)
  and runs checkout filters.
- `git archive`: honours `export-ignore`/`export-subst` attributes, so the
  tree can differ from the commit.
- **`git ls-tree -r -z` plus `git cat-file --batch`** into a temporary
  directory: exact blob bytes, no repository writes, no attribute effects.

### 4. Identity of facts across renames and moves

- **4A Keys only.** A renamed function is removed plus added. Exact, and
  rationale stays keyed by identity (ADR 0005).
- **4B Heuristic matching merged into one "renamed" fact.** Reads better
  but is a guess presented as a fact, and moves rationale silently.
- **4C Keys plus an inferred hint.** Items stay removed plus added; a
  separate, labelled hint pairs them when one removed and one added node
  share kind and content hash (or kind and qualified name in another
  file) uniquely within the comparison.

### 5. Where change semantics live

- **5A In the store (SQL classification).** Two dialects, hard to test
  row by row.
- **5B Raw journal in the store, classification in Go.** The store keeps
  exact raw payloads and composes raw change sets; a new package applies
  the claim contract to them. The raw layer can be checked exactly against
  a snapshot diff (oracle), the contract layer row by row.

## Decision

Options **1C, 2B, 3B with `ls-tree`/`cat-file` materialisation, 4C and
5B**. Contract v1 is unchanged (`contract_version` stays 1).

### Journal write path

- `sqlstore.transaction` keeps a `before` map: the first time a run
  changes, removes (prune or `DeleteFile`) or adds a key, it stores the
  key's pre-run payload (or "absent") before overwriting `old`. Memory is
  O(changed keys), on top of ADR 0006's O(snapshot) maps.
- At `Commit`, after pruning and before the run row, the transaction
  computes the net change per key in `before` (pre-run payload versus
  final payload; equal payloads, for example a duplicate file deleted and
  re-emitted unchanged, produce no entry), writes the entries, updates
  every pin overlay, writes the journal run row and applies retention, all
  in the run transaction. A rolled-back run writes nothing.
- Cascaded edge deletions are recorded: the transaction resolves them
  from `old["edges"]` (endpoints in a deleted node set) before deleting,
  so every removed edge has an entry.
- An unchanged re-index writes zero entries and one journal run row.
- The journal run row stores what the comparison needs about the run
  itself: extractor name and version (`Header.Indexer`), the commit and
  a dirty flag when known, counts per table and change, and the run's
  **knowledge gaps**: the files with an error-severity diagnostic and
  the file-less or directory diagnostics of severity error (scan level),
  capped at 10,000 paths with a `gaps_truncated` flag (then the whole run
  counts as partial).
- `graphvein index` and the server re-index record the HEAD commit and a
  dirty flag in the run metadata (`Header.Commit` stays optional in the
  contract; external indexers may set it). The commit is metadata only.

### Store schema (migration 0004, SQLite and Postgres)

Both dialects get `0004_graph.sql`; `migrations.Version` becomes 4. Keys use
`C` collation on Postgres like the graph tables.

```sql
CREATE TABLE change_runs (
  repository TEXT NOT NULL REFERENCES repositories(repository),
  seq INTEGER NOT NULL,             -- per repository, increasing
  run_id TEXT NOT NULL,             -- index_runs.id
  payload TEXT NOT NULL,            -- extractor, commit, dirty, counts, gaps
  PRIMARY KEY(repository, seq));
CREATE UNIQUE INDEX change_runs_run ON change_runs(repository, run_id);
CREATE TABLE change_entries (
  repository TEXT NOT NULL,
  seq INTEGER NOT NULL,             -- the run that made the change
  record TEXT NOT NULL,             -- 'file' | 'node' | 'edge'
  key TEXT NOT NULL,                -- path | identity | kind NUL source NUL target
  before TEXT,                      -- payload before the run; NULL = absent
  after_hash TEXT,                  -- sha256 of the payload after; NULL = absent
  PRIMARY KEY(repository, seq, record, key),
  FOREIGN KEY(repository, seq) REFERENCES change_runs(repository, seq) ON DELETE CASCADE);
CREATE INDEX change_entries_key ON change_entries(repository, record, key, seq);
CREATE TABLE change_pins (
  repository TEXT NOT NULL REFERENCES repositories(repository),
  id TEXT NOT NULL, name TEXT NOT NULL,
  seq INTEGER NOT NULL,             -- the pinned run
  payload TEXT NOT NULL,            -- copy of that run's change_runs payload, created_at
  PRIMARY KEY(repository, id));
CREATE UNIQUE INDEX change_pins_name ON change_pins(repository, name);
CREATE TABLE change_pin_entries (
  repository TEXT NOT NULL, pin TEXT NOT NULL,
  record TEXT NOT NULL, key TEXT NOT NULL,
  before TEXT,                      -- payload at the pin; NULL = absent at the pin
  PRIMARY KEY(repository, pin, record, key),
  FOREIGN KEY(repository, pin) REFERENCES change_pins(repository, id) ON DELETE CASCADE);
CREATE TABLE ref_snapshots (
  repository TEXT NOT NULL REFERENCES repositories(repository),
  namespace TEXT NOT NULL,          -- the reserved repository id holding the graph
  commit_id TEXT NOT NULL, extractor TEXT NOT NULL,
  payload TEXT NOT NULL,            -- run id, created_at, last_used_at, gaps, config source
  PRIMARY KEY(repository, namespace));
```

`before` alone is enough to compose any comparison: the state of a key at
run R is the `before` of its earliest entry after R, or the current row
when no later entry exists. `after_hash` lets the oracle and the
composition skip a no-op chain without decoding payloads.

### Retention

- Journal: keep the newest 30 runs (`changes.journal_runs`) and at most as
  many entry bytes as the current graph's payload bytes
  (`changes.journal_budget`, factor 1.0), whichever is smaller, but always
  the newest run. Oldest runs go first, at commit. A comparison whose
  base run is no longer covered is refused (`base_expired`), never
  answered partially.
- Pins: at most 5 per repository (`changes.max_pins`); no automatic
  expiry. Each overlay is bounded by the graph size plus the facts removed
  since the pin. Pinning the latest run creates an empty overlay; pinning
  an older run still in the journal seeds the overlay from the entries
  after it. Pinning and unpinning take the index lock briefly.
- Ref snapshots: at most 4 per repository, least recently used dropped
  first (`changes.max_ref_snapshots`); dropping one deletes its namespace's
  files, nodes, edges, diagnostics, runs and fact-cache entries.

### Branch versus base

- `ref:<base>[..<head>]` comparisons resolve both refs locally with
  `git rev-parse --verify <ref>^{commit}`; the base side defaults to the
  merge base of base and head. The head side defaults to the current index
  (the working tree as last indexed); `..<head>` names a ref instead.
- A ref is materialised with `git ls-tree -r -z --full-tree <commit>` and
  `git cat-file --batch` into `os.MkdirTemp`, removed afterwards. Regular
  files and executables only; symlinks and submodules become diagnostics.
  Every git call runs with `-c protocol.allow=never`,
  `GIT_NO_LAZY_FETCH=1` and `GIT_TERMINAL_PROMPT=0`, so a partial clone
  fails with a "objects missing" diagnostic instead of fetching. No network
  connection is opened (AGENTS.md: only `internal/egress` does).
- The ref is indexed with the built-in indexer into the namespace
  `<repository>@ref:<commit>`; configured repository identifiers
  containing `@ref:` are refused from this version on. The run reads the
  fact cache of the main repository first (same path, content hash and
  extractor version) and its own namespace second, writes cache entries
  only to its own namespace, writes no journal and holds the main
  repository's index lock. Its `graphvein.yaml` is the one committed in
  the ref; when the ref has none, the working tree's file is used, and the
  comparison reports which.
- Both sides of a ref comparison are indexed by the same binary, so their
  extractor versions are equal by construction. When the head side is the
  current index and its run used another extractor version than the
  binary, the comparison is refused with `reindex_needed` (re-index
  first) instead of mixing versions.
- Ref snapshots are comparison inputs named by commit, not caches attached
  to facts: nothing (rationale, dispositions, LLM output) attaches to
  them, and the reusable cache stays the content-hash fact cache.

### Raw change sets (store) and the claim contract (Go)

- `store.Store` gains (both backends, shared SQL, read transactions):
  - `ChangeRuns(ctx, repo) ([]ChangeRun, error)`;
  - `ChangeSet(ctx, repo string, base ChangeBase) (ChangeSet, error)`:
    for a run or pin base, every key whose state differs between the base
    and the current graph, with both payloads, composed in one read
    transaction together with the current run id; for a ref base, the
    same between two repository namespaces (anti-joins and a payload
    inequality join on the graph tables);
  - `Pins`, `Pin(ctx, repo, seq, name)`, `Unpin(ctx, repo, id)`,
    `RefSnapshots`, `DropRefSnapshot`;
  - transaction option `RunOptions.Namespace` (ref runs: no journal,
    cache fallback repository).
- New package `internal/changes` applies the claim contract
  (`docs/contract/claims/change-impact.md`) to a `ChangeSet`: projection
  (span-only and evidence-location-only differences are moves, not
  changes; L5 location nodes are counted, not listed), classification per
  row, cause labels, partiality from both sides' knowledge gaps, the
  inferred move hints, and the impact traversal. It is the query API that
  the server, the CLI and the M3 MCP tools call:
  `Compare(ctx, store, repo, Base) (Comparison, error)`,
  `(*Comparison).Items(Filter, Page)`, `Impact(ctx, store, *Comparison,
  ImpactRequest) (ImpactResult, error)`.
- Impact traversal: breadth-first in Go over `store.Edges` (ADR 0009) from
  the changed facts, following only edge kinds the contract's relation
  rows list, in the direction each row gives; removed facts take their
  first hop from the removed incoming edges in the change set and then
  continue in the graph that still has the dependents (current graph for
  run and pin bases, the base namespace for ref bases). Defaults: depth 3
  (maximum 6), 2,000 reached nodes, fan-out 200 per node; every cap that
  binds is reported. Path confidence is the minimum edge confidence;
  a path is `fact` only when every edge is a fact. Blind spots (ADR 0009
  unresolved call sites matching a reached name; ADR 0042 near misses for
  a changed route) are listed as unknowns, never as impact.

### HTTP API (additive, `api_version` 0)

All under `/api/v0/changes/`, read through `internal/changes`, with
freshness (as `/api/v0/status`) in every response and the runs-before/
after check of ADR 0042 (409 `index_changed`).

- `GET bases`: journal runs still covered (seq, run id, finished time,
  commit, dirty, extractor), pins, ref snapshots, retention settings.
- `GET summary?base=`: `base` is `previous`, `run:<id>`, `pin:<name>` or
  `ref:<base>[..<head>]`. Returns both sides (kind, run/commit, extractor),
  `extractor_changed`, `partial` with the gap files of each side,
  counts per group, change kind and cause, moves and location-node counts,
  and the contract version of the classification.
- `GET items?base=&group=&change=&cause=&kind=&q=&cursor=&limit=`: items
  in a stable order (group, change, identity); `limit` default 100,
  maximum 500; `cursor` is opaque and bound to the comparison (a cursor
  from another head run is a 409). Each item: `group`, `change`, `row`
  (contract row id), `identity`, `kind`, `name`, `before` and `after`
  (stored payload fields the row lists, span, file state, IDE link only
  when the side's file is the current file unchanged), `fields` (changed
  user-level fields, before and after), `cause`, `provenance`,
  `confidence`, `hints` (move hints) and `impact_count` (null until
  computed).
- `GET impact?base=&id=&depth=&cursor=&limit=`: the traversal from one
  item, or from every item of a group (`group=`), with paths (edge kind,
  relation row, provenance, confidence, evidence link), unknowns (blind
  spots, gap files reached, untraversed edge kinds with counts) and caps.
- `POST pins` (`{"run": "<id>", "name": "..."}`) and `POST pins/delete`
  (`{"name": "..."}`); `POST refs` (`{"base": "...", "head": "..."}`)
  starts a ref index job; `GET refs` reports it like `GET
  /api/v0/index/run`. The POST paths follow ADR 0052's rules
  (`http.CrossOriginProtection`, `Content-Type: application/json`, 409
  while the index lock is held). The server never accepts a path, only
  ref names it resolves locally.
- The server keeps the last 4 computed comparisons in memory, keyed by
  base and head run, so paging does not recompute; memory is O(changes).

### CLI

`graphvein changes [--base previous|run:ID|pin:NAME|ref:BASE[..HEAD]]
[--group G] [--json]`, `graphvein changes impact ID`, `graphvein changes
pin NAME [--run ID]`, `graphvein changes unpin NAME`, opening the store
like the other query commands (ADR 0009) except for pin, unpin and ref
indexing, which take the index lock. JSON shapes equal the HTTP items.

## Consequences

- "Since the previous run" and pinned comparisons cost no re-indexing; a
  branch comparison costs one index of each ref with the content-hash
  cache (only files that differ from the indexed working tree are parsed).
- Every changed row is written twice per run (graph row and journal
  entry); an unchanged re-index writes one small row. Gated in the task
  (re-index time, store growth).
- The store holds, per repository, up to about one graph's worth of
  journal bytes, up to 5 pin overlays and up to 4 ref namespaces. Users
  see these limits in `GET bases`.
- The raw layer is exact and checkable against a snapshot diff; all
  interpretation is in one Go package under a claim contract table.
- The configured repository identifier loses the `@ref:` substring.
- Renames remain removed plus added, with a labelled hint; rationale does
  not move with a hint.
- The store interface grows (journal, pins, refs); both backends share the
  SQL and are tested on both (AGENTS.md).

## Known gaps

- Architecture components and edges (L1) are not extracted yet; their
  relation rows answer `not_extracted` until the extractors exist. Table
  users in code (`reads_table`/`writes_table`, ADR 0060) are followed
  since Revision 4 (below) as a lower bound; I8 answers `not_extracted`
  only for tables of a source no code-to-table row covers.
- An extractor upgrade between two journal runs cannot be separated from
  relinking effects of other files' changes in unchanged files
  (`extractor_or_relink`); a ref comparison of the same commits with the
  current extractor separates them, when the commit is known.
- A journal run made from a dirty working tree has no ref to re-index; it
  stays comparable through the journal only.
- Team mode: pins are shared per repository and carry no author (there is
  no user identity yet, M4). A teammate's run on another branch appears in
  the shared journal like any run; branch-aware journals wait for the
  multi-repository identity ADR (M4).
- Ref materialisation writes the whole tree to a temporary directory
  (Langflow: hundreds of MB); scanning from git objects directly needs a
  filesystem abstraction in the scanner.
- `export-ignore` and other attributes are deliberately not applied, so a
  ref snapshot is the commit's tree, not its release archive.
- Very large change sets (an extractor bump rewriting every record) make
  a comparison as large as the graph in memory, the same O(snapshot)
  trade-off ADR 0006 accepted for ingestion.
- Cross-request consistency: impact traversal reads several snapshots; it
  is guarded by the runs-before/after check, not by one transaction
  (ADR 0009 review note for M5).

## Implementation notes (T019a1)

Recorded by the implementation; the decision above is unchanged unless a
note says otherwise. Open points O1 to O10 of the task use their
provisional defaults (developer 2026-11-09).

- **Edge keys.** The schema sketch's `kind NUL source NUL target` cannot be
  stored: Postgres `text` refuses NUL. Journal and change-set keys of edges
  are the JSON array `["kind","source","target"]` (`store.EdgeKey`),
  unambiguous on both backends. Files and nodes keep path and identity.
- **First run.** A repository's first run (no earlier index run) writes its
  journal row with `initial: true` and counts but no entries: no base
  precedes it, so the entries could never be read. Every journal row names
  `previous_run`, the run its entries start from. A run base is covered when
  it is a kept journal run or the `previous_run` of the oldest kept run
  (that run's entries hold its state); the latter includes the last run
  before migration 0004, whose gaps were never recorded
  (`gaps_unknown: true`, the whole side partial). Otherwise the base is
  `base_expired` (the run exists) or `base_unknown`.
- **Retention.** A run's entry bytes are the bytes of record name, key,
  before payload and after hash. Walking from the newest run, the first run
  beyond `journal_runs` or whose cumulative bytes exceed `journal_budget`
  times the graph's payload bytes (summed from the transaction's maps, no
  extra scan) is dropped with every older run. The newest run is always
  kept. On small graphs one run's entries can exceed the graph's bytes, so
  only the newest run is kept there; "since the previous run" still works.
- **DeleteFile** removes from the transaction's maps exactly what the
  database cascades: the file's nodes, the edges incident to those nodes
  and the edges with evidence in the file (before, edge endpoints were
  matched by identity path). Each removal goes through the journal.
- **Pins.** `Pin(ctx, repo, seq, name, limit)` takes the limit
  (`changes.max_pins`) because the store does not read configuration; names
  are 1 to 200 bytes without NUL; errors `ErrPinExists`, `ErrPinLimit`,
  `ErrPinUnknown`, `ErrBaseExpired`, `ErrBaseUnknown`.
- **Run metadata.** `indexrun.Run` (graphvein index and the server re-index)
  records HEAD's commit and a dirty flag from `scan.ReadGitState`: clean
  only when every indexed file's blob id equals HEAD's blob at that path and
  every regular file of HEAD that was not indexed still exists in the
  working tree. Untracked files, files excluded only by `.git/info/exclude`
  and checkout conversions (autocrlf, eol attributes) are therefore dirty.
  A subdirectory of a checkout records no commit. `Header.Commit`, when an
  external indexer sets it, takes precedence; external runs record no dirty
  flag.
- **Ref materialisation.** The temporary directory is also initialised as an
  empty git repository of the same object format (`git init --template=`),
  so the unchanged scanner computes `git_blob_oid` there exactly as in the
  working tree; without it every file record of a ref would differ.
  Symlinks get the scanner's own `symlink` warning, submodules
  `ref_submodule` (warning), paths that cannot be written on the platform
  `ref_unwritable` (error, so a knowledge gap). Missing objects fail the
  materialisation (`gitref.ErrObjectsMissing`); nothing is fetched.
- **Ref runs.** `indexrun.RunRef` indexes one commit into
  `<repository>@ref:<commit>` with `index.Options.CacheFallback` (the main
  repository's cache first, the namespace's second; reused entries are not
  copied) and `index.Options.ConfigRoot` (the working tree's
  `graphvein.yaml` when the commit has none, without adding it to the ref's
  files). `last_used_at` is set when a ref run commits; a comparison that
  only reads a snapshot does not update it (a2 may add that).
- **Store boundaries.** A run into a repository identifier containing
  `@ref:` is refused unless it is a ref run whose header repository equals
  its namespace. `internal/gitref` is listed in the egress subprocess
  allow-lists (local git with `protocol.allow=never`).
- **Raw layer checks.** `store.DiffSnapshots` is the reference diff; the
  store tests compare every change set with it on both backends, and
  `TestChangeJournalOracle` (env-gated) on real commit pairs.

Known gap found during implementation: on a checkout with CRLF conversion
(Windows `core.autocrlf=true`, `eol=crlf` attributes) the working-tree
index records the converted bytes' size and blob id, a ref snapshot the
committed blob's. A ref comparison against the current index then shows
those file records as changed (content hashes are equal, since the scanner
normalises line endings). The classification (a2) has to treat such a
difference as a move or the contract has to say how it is reported.

## Implementation notes (T019a2)

Recorded by the implementation of `internal/changes`, the API and the CLI;
the decision above is unchanged unless a note says otherwise.

- **Line endings (resolves the a1 known gap).** A file record is compared
  under projection: path, language, precision and the LF-normalised
  content hash. Its size and git blob id differ between a CRLF checkout
  and the committed blob (or between a scan inside and outside a git
  repository) while the content hash is equal; such a record is no change
  (no C1 item, no cause `code`) and is counted as `file_metadata_only` in
  the summary. `TestLineEndingConversion` covers it on both backends.
- **syntax_error is a knowledge gap.** The built-in indexer reports
  tree-sitter recovery as a `syntax_error` warning, not an error; facts
  missing from a recovered tree would otherwise read as removed. The
  journal (and C15) count it as a gap (`store.GapWarningCodes`). Contract
  change to the Terms ("knowledge gaps": error-severity diagnostics) for the
  developer's review.
- **Schema rules come from the server.** C8, C9 and C11 depend on a group's
  `table_list` and a table's completeness, which `/api/v0/schema/sets`
  derives from nodes and diagnostics. `changes.Schema` is implemented by
  `server.ChangeSchema` with the same code, so an item never disagrees with
  the Schema view; the CLI passes it too. Run and pin bases have no stored
  diagnostics, so their table lists are unknown: a table added since such a
  base is `known_now` (`base_table_list_unknown`), never `added`. A member
  is added or removed only when its table's completeness is `complete` on
  both sides (`complete_in_source` is not enough).
- **Rows and forms.** The code holds the row table as data (`Rows`,
  `Relations`, `rowCodes`, `follows`); `TestChangeClaimContract` compares
  it with the task file and checks code and golden coverage of every row
  and relation. Forms no row lists take D; `file` nodes and the
  `exposes_route` and `alters` edges took D until Revision 1 (see the
  Revision 1 notes below).
  C10's verdict is `applied`; may-fail reasons are diagnostics, which the
  journal does not hold, so they are not compared.
- **Links.** C5's head-call condition reads the call's `status` (the summary
  over its paths) and requires a known method (C6: method and path; C7:
  the command name). A lost link whose call no longer exists was
  `link_unknown_now` (`call_absent`) until Revision 1 (below).
- **Impact.** `untraversed` counts incoming edges no relation follows and
  unfollowed outgoing kinds that a relation follows outgoing (`exposes_*`);
  other outgoing edges point at what a node depends on. I10 is followed one
  hop: a file reached through an import does not follow imports again. A
  table's foreign keys are read through its columns (I7, `via`). I8 is
  answered in one function (`notExtracted`), so the `reads_table` and
  `writes_table` edges (T018h) change only it and `follows`. (Revision 4
  replaced it with `follows` entries and `tableUsers`.)
- **API.** Change endpoints answer 409 `index_changed` (G6), not 503 like the
  other endpoints; `base_expired` is 410, unknown bases 404. Cursors are
  opaque and bound to the comparison (base, both runs, filter). The server
  caches 4 comparisons keyed by base, latest run and, for ref bases, the
  resolved commits and their snapshot runs. `POST refs` indexes only commits
  without a snapshot. Freshness was computed per request, as in
  `/api/v0/status`, until the Revision 1 notes below.
- **Invariants.** `Comparison.Violations` and `ImpactViolations` re-check
  oracle step 5 independently of the classifier; every golden and the
  corpus oracle (`TestChangeClassificationOracle`, on the stores
  `TestChangeJournalOracle` leaves) require zero.

## Implementation notes (T019a, Revision 1)

Task revision 1 (2026-11-13) decided the A0 findings; the
classification follows it (`ContractVersion` `T019-r1`).

- **File nodes are context.** A changed `file` node is never an item: its
  file record is the C1 item and the G4 cause context of the items in that
  file. The node is counted in the `files` group (`moves` when equal under
  projection, else the new `context` count).
- **Handler and alters edges go with their owner.** A changed
  `exposes_route` edge is reported on the route it exposes (C3, label
  `handler_changed`, field `handler_edges` with the handlers lost and
  gained); a changed `alters` edge on the migration it starts from (C10,
  label `alters_changed`, field `alters` with the tables), and a listed
  table it connects gets the label too. When the owner itself did not
  change, a C3 or C10 item `changed` is created for it; when the owner is
  added or removed, the edge is part of that item and counted as
  `context`. Equal under projection the edge is a move. G3 applies per
  field: an edge gained on a base that does not know its footprint is
  `known_now`, any other gap `unknown_now`. The edges' payloads are in the
  item's raw view (`raw.related`). C4 has no such edge: `exposes_route`
  runs from a handler to a route, so the revision's "C3/C4" applies as C3.
  These edges are processed before the link rows, so a lost link sees its
  route with `handler_changed` (a changed target, so never `call_changed`).
- **D keeps only the forms no row names.** Golden D items are checked
  against the row kinds (`TestChangeClaimContract`), the invariants
  (`Violations`) reject a D item of a named kind unless its payload does
  not decode.
- **A lost link of a removed call.** When a link's call is absent on the
  head and its C4 item is `removed`, the link is `link_lost` with reason
  `call_absent`, worded `call_removed`: the removal proves the absence. A
  call absent for another reason (a gap) keeps `link_unknown_now`
  (`call_not_proven_removed`).
- **Freshness cost.** The change endpoints keep, per index state (the
  latest run's id and finish time), the stored file hashes (one
  `FileHashes` read through the store per run, used for IDE links) and
  the last working-tree freshness check, reused for at most 2 s and
  reported with its own `checked_at`. Concurrent requests wait for one
  check. A new run drops both. `/api/v0/status` still checks per request.
  Known gap: the check itself is still a full walk and hash of the working
  tree; a request after the window pays it once (stat-based revalidation
  would need a scanner option, outside this task's allowed changes).

## Implementation notes (T019a, Revision 3)

Task revision 3 (2026-11-14) fixes the independent review's
findings as class rules (`ContractVersion` `T019-r3`). Status stays
Proposed.

- **Comparison cache keys on the resolved base.** A pin base adds the
  pin's id and run seq to the key (a deleted pin is 404 `base_unknown`, a
  re-pointed one recomputes); a ref base adds the resolved commits and each
  snapshot's run and extractor.
- **Blind spots scan every file node** of the graph, as `graphvein impact`
  does, not only the files of seeds and reached nodes.
- **Structural edges go with classified items.** A `contains` or `imports`
  edge gained or lost with an endpoint that has a C3-C12 item is that
  item's `context` (`raw.related`), never a C16 claim of its own;
  `Violations` rejects such a C16 item.
- **G8 reads the head tree.** A removed file is `deleted` only when the
  head commit's tree (ref head, `git ls-tree`) or the working tree (current
  index with a root) lacks the path; a path still present is
  `no_longer_in_index` with reason `excluded` (ignore rules, size limit or
  language; the specific rule is not named: that would need a scanner
  report). Without a tree to read, `no_longer_in_index` with no reason.
- **`ref:BASE` uses the indexed commit.** Without a head ref the merge base
  is taken with the commit the newest journal run recorded
  (`IndexedCommit`); `summary.head_commit_differs` says when the live HEAD
  is another commit; no recorded commit is 409 `reindex_needed`.
- **Snapshots of another extractor version are not used**
  (`SnapshotUsable`): POST refs and the CLI index them again, so both sides
  of a ref pair have this binary's version by construction.
- **G3 is symmetric.** An added or removed item stands only when both sides
  know its footprint: a head gap gives `unknown_now`, a base gap (head
  knows) `known_now`, both with reason `gap`; `Violations` checks both
  sides.
- **C4 resolution fields follow their link target.** A changed `paths` or
  `status` of a client call whose `calls_route` target lies in a gap file
  (either side) is `unknown_now`, like its C5 link.
- **Partial clones.** When `git cat-file --batch` exits with an error, its
  stderr decides (`ref_objects_missing` for missing or promisor objects)
  over the reader's EOF.
- **Smaller rules.** `changes.max_ref_snapshots` is at least 2; a cursor
  that does not decode is 400; POST bodies with a duplicate member are 400;
  `ref:a...b` is 400; 405 messages name the allowed methods; a path through
  a node in a gap file is `inferred` ("may depend on", checked by
  `ImpactViolations`); a path's wording is
  its last edge's I-table wording (I3, I7, I9 per edge); a C9 fact (such as
  a default) claimed on the head only is `known_now`, on the base only
  `unknown_now`; `followOf` returns an error.
- **Known gaps.** On small graphs retention may keep only the newest
  journal run, so `previous` has `gaps_unknown` and, with symmetric G3,
  both additions and removals read `known_now` there; keeping run metadata
  beyond entries is after v1. Pinning the `previous_run` of the oldest kept
  run (accepted as a run base) is not supported (`PinRun` looks only at
  kept runs: `base_expired`).


## Implementation notes (T019a gates)

Decision under the developer's delegation of 2026-11-13
(the gate was tried first, then raised with a reason). Status stays
Proposed.

- **Ref comparison cost target restated.** Indexing a ref with the warm
  cache costs no more than a warm no-op re-index plus the changed files'
  extraction. Measured on Langflow P3: 104.9 s, 57 % of a cold index; the
  warm no-op re-index alone is 43 %, so the earlier target (below 30 % of
  a cold index) is out of reach while a ref snapshot re-ingests the whole
  graph into its own namespace.
- **The 30 % target needs a copy-on-write ref namespace** (a snapshot that
  shares the main namespace's unchanged rows instead of re-ingesting
  them): a storage and indexer-pipeline change that needs its own ADR. It
  moves after v1, together with the snapshot size (415 MB per ref
  namespace on Langflow).

## Implementation notes (T019a, Revision 4)

Task revision 4 (2026-11-14): row I8 follows T018h's
code-to-table edges (ADR 0060, `docs/extract/table-access.md`)
(`ContractVersion` `T019-r4`). Status stays Proposed.

- **I8 is a followed relation.** From a table, the incoming `reads_table`
  and `writes_table` edges lead to their owner (any node kind); from a
  column, its table's edges (the table found through the column's
  incoming `contains`), with `access.column_use: "not_extracted"` (edges
  are table-level). The path's provenance and confidence are the edge's;
  from the owner the traversal goes on (I1 to its callers). The I8 step
  carries the owner's `table_access` entry joined by (kind, target) as
  `access`: tool, role, operations, rows, `verification` with its wording
  (`mapped`: "mapped, not verified"), `assumes`. Wording: "reads this
  table" / "writes this table" on fact edges, "may read / may write this
  table" on inferred ones, "builds a query on this table" for `role:
  builds`. `ImpactViolations` checks the wording and that only I8 steps
  carry `access`.
- **Users are a lower bound.** Each traversal lists `table_uses`, one entry
  per table whose users it read: `found` (every owner with an edge to the
  table, worded "uses found") and `may_also_be_used_by` (inferred): the
  `table_access_unknown` entries of other owners whose `candidates` name
  the table. There is no "no users" answer. Entries with `candidates`
  null (any table of the tool's sources in the package: for example
  `statement_not_traced`) are not attached to a table: resolving "the
  same package" would repeat the linker's W7 rule outside it (known gap,
  open point).
- **`not_extracted` per source group.** Covered: tables of a Postgres
  migration set (sqlx, the only migration tool read: CT-S1, CT-S2) and
  model tables of the tools `sqlmodel`, `prisma` and `drizzle`. Not
  covered, `not_extracted` with the node: SQLite or MySQL migration sets
  (`no_schema_source`; the schema extractor emits no tables for them
  today) and model groups of other tools (none emitted today). So no
  table answers I8 `not_extracted` on today's extractors.
- **Changed edges are classified by existing rows** (A0 mapping). A
  `reads_table` / `writes_table` edge whose owner is a code symbol is
  reported on the owner's C2 item (label `table_access_changed`, the
  gained and lost tables in a field named after the edge kind), like
  `exposes_route` on C3; when the owner is added or removed the edge is
  that item's `context`. A file node's edge is a C16 edge item, `context`
  of a C8 or C11 table item it connects (R3-3's rule). The owner's
  `table_access*` attributes are not C2 fields (C2 compares the content
  hash): a change in them alone is counted in `other_changes`. D keeps only
  unnamed forms.
- **Cost.** The candidate index reads the graph's code symbol nodes once
  per comparison and graph and takes the file nodes' entries from the
  file index the blind spots use (one file-node read for both). The
  server builds it in the background preparation after the summary when
  an item is a table or column (only those seeds reach a table), else on
  the first traversal that reaches a table (Langflow: a table
  seed 1 s after the summary 1,925 to 256 ms median, the summary
  unchanged).

## Review notes (delegated review, 2026-11-14)

Accepted for T019a (backend: journal, comparisons, impact, API, CLI)
after the green `[ci windows]` run 43585029421 on 681aaac (Windows and
postgres green; postgres job 0 FAIL, only env-gated skips). T019b (the
Changes view, design V3 with V1's density) follows under this ADR.

- Results: migration 0004 (five change tables), journal written inside
  the run transaction, pins, ref namespaces (`internal/gitref`, no fetch,
  no hooks), `internal/changes` with the C/D/G rows and I1-I11/ID
  relations 1:1 with the task (`TestChangeClaimContract`, contract
  `T019-r4`), HTTP endpoints and `graphvein changes`.
- Oracle (journal against snapshot diff): P1 axum, P2 fastapi, P3
  Langflow, 0 raw disagreements (P3 9,351 changes) and 0 classification
  violations; the review's own pair 0 disagreements.
- Revisions: 1 (A0: file nodes as context, `exposes_route`/`alters`
  with their rows, syntax errors as gaps), 2 (T019b design pick), 3
  (independent review: resolved-base cache keys, repository-wide blind
  spots, structural edges as context, G8 from the head tree, merge base
  from the indexed commit, ref snapshots per extractor version,
  symmetric G3, gap-target C4 fields, partial-clone errors), 4 (I8
  follows T018h's `reads_table`/`writes_table`; users are a lower bound).
- Gates: Langflow store +1.29 % over main; unchanged re-index writes 0
  entries; warm no-op 0.0 %, incremental +2.7 %, cold +2.0 %; first
  summary about 1.2 s; cached summary and items pages under 6 ms; impact
  of a function or table seed clicked after the summary 2-256 ms; peak
  RSS at main's level.
- Gates raised with reasons (developer delegation 2026-11-13): ref
  comparison cost restated as "no more than a warm no-op re-index plus
  the changed files" (57 % of a cold index vs the 30 % target, which
  needs a copy-on-write ref namespace); an impact sent within the
  background build window right after the first summary takes 0.5-1.2 s
  on Langflow (needs a store read of only the needed attributes).
- Assumptions shown: the git tree of a ref is what the indexer would
  see in a checkout of it (ignore rules from the ref).
- Known gaps (labelled, no wrong fact): retention on small graphs keeps
  only the newest run; pins of the oldest kept run's previous run;
  C10 compares `applied` only; in-file renames get no move hint;
  null-candidate code-to-table unknowns are not tied to a table.
- After v1: copy-on-write ref namespaces (415 MB per Langflow ref
  snapshot), attribute-only store reads, stat-based freshness
  revalidation, hey-api `__request` client calls.

## Implementation notes (git subprocess rule, 2026-11-14)

A Windows CI run failed the check that materialising a ref writes nothing
into the repository's `.git` (intermittent; it passed in the run before).
The git rule is now one class rule in one place:

- Every git subprocess Graphvein starts goes through `scan.GitCommand`
  (ref resolution, merge base, materialisation, `ReadGitState`, the
  scanner's object-format probe, the temporary repository's `git init`).
  A test parses the program's sources and refuses any other
  `exec.Command`/`exec.CommandContext` of `git`.
- Options: `-c protocol.allow=never`, `-c maintenance.auto=false`,
  `-c gc.auto=0`, `-c core.fsmonitor=false`, plus `safe.directory` and
  `-C` for the repository. Environment: `GIT_OPTIONAL_LOCKS=0`,
  `GIT_NO_LAZY_FETCH=1`, `GIT_TERMINAL_PROMPT=0`. New: the maintenance,
  gc and fsmonitor options, and the scanner's object-format probe, which
  had only `safe.directory`. The temporary repository is still created
  with `--template=` (no hooks).
- The commands used (`rev-parse`, `merge-base`, `ls-tree`,
  `cat-file --batch`, `init` in the temporary directory) take no index
  lock, run no hooks and start no maintenance, so no Graphvein invocation
  was found that writes into `.git`; the options make that hold for any
  future command. The likely source of the failure is the test itself: its
  `.git` state compared directory-walk modification times, which NTFS
  reports from the parent directory's entry and may update after the
  writing process (the test's own setup commits) has exited. The check now
  compares paths, kinds, modes, sizes and content hashes (every real
  write changes one of them), is taken right after `Materialise` returns,
  lists each differing entry, and the test's own git calls run without
  automatic maintenance.

## Review notes, T019b (delegated review, 2026-11-14)

The Changes view merged after the green `[ci windows]` run 43598267568
on 4721fb2 (Windows and postgres green; postgres job 0 FAIL, only
env-gated skips; check-web ok).

- Design: mockup round 1 variant V3 (global compare chip, three panes)
  with V1's density, picked by the developer; desktop-first gates
  (1280 px and wider, about 900 px usable; developer decision
  2026-11-14).
- Results: compare chip (base per browser, one-click off, refusals),
  Changes section (summary, groups, list/item panes, Change/Impact/Raw,
  I8 wording with verification and assumption texts, users as a lower
  bound), "changed since" markers in the API, Flow, Schema and migration
  views; counts in the UI equal the API's on the fixture, fastapi and
  axum.
- Review: one independent review (21 scenarios); Revision 5 fixed
  markers shown for comparisons whose head is not the current index,
  answers mixed across comparisons after an outside re-index, stale keys
  after re-running a branch or re-pointing a pin, an overclaiming
  verification tooltip, and spec and layout gaps.
- CI: a Windows-only `.git` check flake led to one git subprocess rule
  (`scan.GitCommand`: no optional locks, no auto maintenance or gc, no
  fsmonitor, no lazy fetch, no prompts) and a content-based check.
- Known gaps: a pin re-pointed elsewhere is noticed at the next status
  or bases answer, not per answer (needs the API to return the resolved
  base); no markers on the Overview diagram or mini map; no cancel for a
  ref job; change endpoints take no `editor=`.

## Implementation notes (T026 AG-F, T019 Revision 7)

Pending the developer's review. React bridges (ADR 0079 AG-B5) enter the claim
contract as one class rule each (`ContractVersion` `T019-r7`):

- C17 "React UI": a `ui_route` with `framework: "react-router"` compares
  `path`, `index`, `lazy`, `open`, `component`, `element`, `loader`,
  `action`; `routes_to` and `passes_function` from such a route and every
  `renders` edge that is not the Angular linker's (a source that is not a
  class, or no endpoint carrying the `angular` attribute on either side)
  compare presence. Other `passes_function` edges are C16.
- I14 follows incoming `renders` and `routes_to` (from a `ui_route`) into
  a function; I15 follows incoming `passes_function` into a function or
  method, and its paths are always inferred (`inferredRelations`): a
  passed function proves no call.
- Known gaps: a React class component reached through `renders` is I9
  ("renders this", wording shared); memo/forwardRef wrappers are D in the
  bridges contract, so impact does not pass through them.

## Revision (T042): traversal depth maximum 10

Status of this revision: Accepted (delegated review, 2026-11-28; the
decisions above keep their status).

- Context: a project walk already allowed depth 10 while one repository
  stopped at 6, and a route-to-frontend chain through three helper
  functions needs 7 steps, so single-repository change impact and the
  architecture chain cut real paths.
- Options: (a) keep 6 and send people to project mode; (b) raise the
  shared `traverse.MaxDepth` to 10 with the defaults unchanged; (c) remove
  the depth cap and rely on the member and fan-out caps. Chosen: (b). (c)
  would make every default walk as deep as the graph and change answers
  nobody asked to change.
- Decision: `traverse.MaxDepth` is 10 (was 6). The change impact default
  stays 3 (`changes.DefaultDepth`); the 2,000 reached-node and 200 fan-out
  caps are unchanged, so a deeper walk still stops at the same size and
  reports the cap that binds. `query.MaxDepth` follows the shared cap and
  now equals `query.ProjectMaxDepth`. A request above 10 is refused as
  before (400 on HTTP, a usage error on the CLI, `bad_request` in MCP).
- Consequences: members and paths at the default depth are unchanged;
  the reported caps change exactly as follows. HTTP chain
  (`GET /api/v0/architecture/chain`): `caps.depth` reads 10 (the largest
  depth a request may name, was 6); the walked `depth` is unchanged. MCP
  `chain`: a depth bound's `limit` is the walked depth (unchanged at the
  default 6; a request for depth 1 now reads 1, where it read 6) and its
  text names "at most 10" (ADR 0079, revision T042). MCP `callers`,
  `impact` and `change_impact`: a depth bound's text names 10 instead of
  6, and at depth 10 none of the walk tools proposes a larger depth (each
  points to a node at the last step instead). HTTP change impact reports
  cap names only and is unchanged. MCP tool descriptions and the refusal
  texts name 10. The web UI offers depths 1
  to 10 and remembers a chosen depth (ADR 0078, revision T042).
- Known gaps: the depth-10 timings and sizes are measured on the largest
  corpora in the T042 task record (internal); a walk from a hub at depth
  10 can reach the 2,000-node cap where depth 6 did not, which is
  reported as a cap, never as "nothing beyond".
