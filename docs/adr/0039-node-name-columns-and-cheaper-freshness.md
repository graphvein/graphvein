# ADR 0039 - Node name columns and cheaper freshness

**Status:** Accepted (delegated review, 2026-11-03)
**Date:** 2026-11-03

## Context

ADR 0009's review note (2026-10-08) measured 10-11 s per CLI graph read on
Langflow regardless of result size. T016 onwards adds an HTTP API and web UI
that issue these reads per click. T016a re-measured on Langflow (504c02fc,
10,233 files, 68,647 nodes, 150,032 edges; SQLite; see
the T016a task record (internal)): every `query`, `callers` and `impact` call
took 8.4-10.6 s and `status` about as long. About 70% was freshness: the scan
walk (dominated by the ignore matcher's regular expressions, not hashing) and
`Store.Dump`, which decodes every file, node, edge and diagnostic although
freshness needs only file hashes. The rest was JSON decoding of whole tables:
names live only in the node payload, so a name lookup decoded all nodes, and
callers/impact decoded every node to build an identity map.

## Options considered

- **Where names live for lookup.**
  - Name columns on `nodes` (chosen): one row per node, written in the same
    statement as the payload; existing filters combine in one query.
  - A separate name table: same information, plus a join and a second write
    path to keep consistent with the payload.
  - SQL JSON expressions over the payload (`json_extract`, `::jsonb`):
    no schema change for the data, but a second JSON decoder in the trust
    path whose edge cases (`\u0000`, lone surrogates) differ from Go's, and
    divergent syntax per backend.
- **Indexes.** Plain `(repository, name)` and `(repository, qualified_name)`
  indexes serve exact matches but not the qualified-name suffix, which no
  b-tree prefix can serve. A covering index
  `nodes_names (repository, qualified_name, name, identity)` lets the whole
  prefilter (exact name, exact qualified name, suffix) run as one scan of a
  narrow index, then payloads are read by primary key. The investigation showed no
  query shape that needs `(repository, name)` in addition, so it is not
  added.
- **Backfill.** SQL `UPDATE ... json_extract(...)` is fast but is the second
  decoder rejected above. Go backfill inside the migration transaction reads
  payloads in identity-ordered batches and decodes them with the same
  decoder settings as graph reads (`UseNumber`).
- **Freshness.** A stat cache (mtime/size) would skip reading and hashing,
  but on Langflow read+hash costs about 0.4 s of the walk while the ignore
  matcher cost about 3.7 s, and stat caches have racy-timestamp failure modes
  that can report stale files as fresh: a confident wrong fact. Chosen:
  keep reading and hashing every file, make the ignore matcher cheaper
  without changing any decision, and read stored hashes from the `files`
  table's `content_hash` column instead of `Dump`.

## Decision

Schema version 3 (`migrations/{sqlite,postgres}/0003_graph.sql`) adds
nullable `name` and `qualified_name` to `nodes` (Postgres `COLLATE "C"`) and
the covering index `nodes_names`. `UpsertNode`, the only statement that
writes `nodes.payload`, writes the columns from the payload it writes. The
migration backfills existing rows in Go inside the migration transaction.

NULL rule, identical on both backends: both columns are NULL when either
value contains U+0000 (Postgres text cannot hold it) or when
`len(repository)+len(identity)+len(name)+len(qualified_name)` exceeds 1536
bytes. The task suggested identity, name and qualified name; repository is
added because it is part of every `nodes_names` entry and has no length
bound of its own, and Postgres b-tree entries are limited to about 2.7 kB. A
payload whose name fields cannot be decoded also leaves NULL columns, so a
term read decodes it and reports the error. A NULL row is always a term
candidate and never excluded. A payload whose name fields decode but whose
full node does not (for example a wrongly typed `level`) gets columns and can
be excluded; see Known gaps.

`Nodes` with a term selects candidate identities with the other filters plus
`(qualified_name IS NULL OR name = t OR qualified_name = t OR qualified_name
LIKE '%' || escaped(t) ESCAPE '!')` in a subquery, reads their payloads in
identity order, then applies the unchanged Go predicate and the limit. The
SQL is a prefilter that must be a superset of the Go matches. SQLite's LIKE
folds ASCII case, which only widens the set; Postgres LIKE under `COLLATE "C"`
is exact. `!` is the escape character so the SQL needs no backslash, whose
meaning depends on Postgres's `standard_conforming_strings`. Terms that
contain U+0000, are not valid UTF-8 or are longer than 1536 bytes (no
non-NULL row can match them; SQLite also limits LIKE pattern length) use the
full read every term used before. Reads filtered by kind, file or level use
the same identity subquery shape, because with `ORDER BY identity` both
SQLite and the old flat query walked the primary key instead of
`nodes_kind_level`/`nodes_file`. `Edges` drops its SQL `ORDER BY
kind,source,target` for the same reason (SQLite walked the edges primary key
for the repository instead of `edges_incoming`/`edges_outgoing`); the Go sort
it already applied over the primary-key columns defines the same total order.

The store interface gains `FileHashes(repository)`, path to stored content
hash in one read transaction, and `NodesByIdentity(repository, identities)`,
identity-ordered, deduplicated, missing identities omitted, chunked inside
one read transaction like `Edges`. Freshness (`status` and every query's
`freshness`) uses `FileHashes`; IDE presentation reads and hashes source
lazily for presented nodes' files with the same normalization and
`contract.ContentHash`; callers/impact fetch each level's new endpoints and
result files' file nodes with `NodesByIdentity`; impact alone reads all file
nodes for blind spots, after its target is found.

The scan's ignore matcher precompiles each rule: a pattern without `/` whose
body cannot match `/` (no `**`, no bracket expression) is matched against the
last path segment only, as a string comparison when it has no other
metacharacter; anchored literal patterns are string comparisons; other
anchored patterns check their leading literal before the regular expression.
Byte comparisons agree with the regular expression only for valid UTF-8
(the expression reads each invalid byte as U+FFFD), so a path that is not
valid UTF-8 always takes the full regular expression. Rules are searched from the last one, and the first match decides, which is
the same as "last matching rule wins". The previous matcher is kept as a
test-only reference with a differential test.

## Consequences

- Store schema version 3. `index` and `status` migrate older stores; graph
  query commands keep refusing other versions (`OpenRead`). Binaries built
  before this change refuse a version 3 store ("newer than supported").
- Store size grows by the two columns and the covering index; the gates unit
  records the Langflow figure (the task's estimate was about +1.7%).
- Ingest decodes the name fields of every written node payload once more.
- Every CLI output for a store whose payloads all decode is unchanged; only
  latency, the schema version and store size are meant to differ. A store
  with a corrupt payload behaves differently; see Known gaps.

## Known gaps

- The qualified-name suffix remains a scan of `nodes_names` for the
  repository; it is narrow but linear in node count.
- Impact still reads every file node for repository-wide blind spots.
- Traversal levels, per-level node fetches and freshness are separate read
  snapshots, as ADR 0009 already accepted for traversal and freshness.
- No stat cache: every freshness call still reads and hashes every file.
  Revisit only with its own ADR if hashing becomes the bottleneck.
- Rows with NULL name columns are candidates for every term; there are none
  on the measured corpus, but a repository with many would read more
  payloads per lookup.
- Corrupt stored payloads no longer fail every read. Before schema 3 a name
  lookup, callers, impact, `status` and every query's freshness decoded all
  nodes (and freshness all files), so one undecodable payload anywhere in the
  repository failed them with "decoding stored graph". Now freshness reads
  only file hashes and traversal decodes only the rows it reaches, and a name
  lookup can exclude a row whose name fields decode while the full node does
  not. Such a command can succeed where it failed before. A row that cannot
  be decoded is still never returned, so no wrong fact is reported, and a
  read that reaches it still fails with the same error; the change is in
  which commands fail, not in what they report.
- A Postgres database whose server encoding is not UTF-8 may fail a term
  query with an encoding error when the term holds a character that encoding
  cannot represent: the term now reaches SQL as a parameter, where before it
  was compared in Go and returned no match. Such a database could not store
  node names with those characters either.

## Review notes (delegated review, 2026-11-03)

- Delegated review in place of developer review. Results:
  - The investigation: no stop. A2 plans use the covering index `nodes_names` and
    `nodes_kind_level`/`nodes_file`; A3 superset check 0 misses on Langflow
    (93,219 terms), the self-index and every fixture store.
  - B5: 25,727 main-vs-branch CLI invocations byte-identical (self-index, CLI
    fixtures, all ten public checkouts, stale scenarios); store tables
    row-identical.
  - Review 1 (independent): no blocker; 3 minor findings fixed in f74f830:
    M1 ignore-matcher byte fast paths differed from the regex on non-UTF-8
    paths (now fall back to the regex; differential test extended to
    1,739,444 decisions); M2 this ADR's decode-error wording corrected and the
    gaps listed below; M3 multi-batch backfill test on both backends. The
    `Edges` read for one incoming identity dropped its SQL ORDER BY (Go
    already sorts by the primary key), so SQLite uses `edges_incoming`
    (28-31 ms to about 1 ms); re-comparison 3,992 invocations, 0 diffs.
  - Gates (29c75d5): Langflow medians query 0.91-0.94 s (main 8.2-8.6 s),
    callers 0.92 s, impact 1.76 s, status 0.86 s, all within target; indexing
    fresh -0.8%, warm -2.5% (Langflow), self-index fresh -2.8%, warm +4.1%;
    v2 to v3 migration about 3.6 s on Langflow; CI green including postgres
    (only the four helper skips; new /postgres subtests pass).
  - Gate mismatch (rule 17): fresh store size +2.56% on Langflow (+23.3 MB:
    6.7 MB columns, 16.7 MB `nodes_names`) against the task's +2%
    expectation, which compared a vacuumed copy with an unvacuumed store.
    Accepted: the index is what makes name lookups sub-second.
- Accepted known gaps: the gaps listed above, plus the T016a task record (internal)
  is a task record outside the task's allowed list (kept).
