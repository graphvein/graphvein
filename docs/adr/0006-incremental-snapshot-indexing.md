# ADR 0006 — Cached extraction and snapshot diffs

**Status:** Accepted (developer review, 2026-10-05)
**Date:** 2026-10-05

## Context

T006 makes the complete v1 snapshots from ADR 0005 practical for a built-in
indexer. Its developer review requires unchanged sources to avoid parsing and
unchanged graph records to avoid database writes. Cross-file links must still
respond to deleted or renamed targets. T002 also found repeated WASM compilation
and unconstrained worker counts expensive.

## Options considered

- Delta transport: changes contract semantics and requires a separate contract
  version, external-producer support and failure/publication design.
- Replace the whole graph after replaying extraction: saves parsing but rewrites
  unchanged rows and invalidates incremental storage benefits.
- Cache final cross-file edges: makes callers stale after target-only edits.
- Cache local extraction and unresolved candidates, relink against each complete
  inventory, and compare payloads inside the existing atomic transaction.
- Filesystem fact-cache sidecar: duplicates persistence/repository isolation
  logic; the existing store is already the supported persistence boundary.

## Decision

Keep contract v1 unchanged. Add `fact_cache` in schema migration 0002 for both
SQLite and Postgres, accessed only through `store.Store`. Keep the most recent
entry per logical repository/path; require matching content hash and extractor
version (including the file-size policy) for reuse. Cache source nodes,
containment, export/import bindings, call candidates and syntax diagnostics.
Do not cache transient parser/extractor failures. Corrupt JSON is a cache miss.
Changing grammar/extraction/naming semantics requires an extractor-version bump.

Cache entries are independent source-derived artifacts and may be saved before
ingestion. A failed stream can leave useful extraction cached, but cannot publish
an incomplete graph. Successful ingestion prunes cache entries for absent files.
Rebuild import/call edges on every run against the current inventory, including
for cached callers. Do not emit dangling edges; diagnose unavailable imports and
count unresolved calls per file. Resolution is intentionally best effort, without
TypeScript type checking, path aliases, inheritance or re-export traversal.

Within the serialized repository transaction, retain previous payloads and the
keys emitted this run. Upsert only changed payloads; delete absent edges, nodes
and files after valid footer/EOF. References must point to keys emitted in the
new snapshot, not merely rows left from the preceding run. A changed duplicate
file within one stream still deletes its previously emitted graph as v1 requires.
Diagnostics and run metadata are republished every run. `Transaction.Writes`
and `ingest.StreamWithWrites` expose graph-row counts for verification and CLI
reporting. The contract remains the sole application fact boundary.

Pin production wasitter v0.1.0 / wazero v1.10.1 and the existing audited JS/TS/TSX
assets. Compile each grammar once per process/cache configuration. A shared,
thread-safe wazero engine owns compiled code and WASI imports; each worker owns
independent guest module memory, wasitter runtime wrapper and parser, reused
sequentially. Trees are detached and closed before another parse; modules and
parsers close when the batch ends. Compiled engines remain until process exit.
This shares compilation, not mutable parser state, as required by ADR 0002.

Use `os.UserCacheDir()/graphvein/wazero/wasitter-0.1.0-assets-v1` for compilation.
Wazero additionally keys entries by its runtime version and module digest.
Unavailable disk caching falls back to memory. On corrupt-cache errors, discard
only that owned namespace and compile with a fresh runtime/cache; fall back to
memory if rebuilding disk storage fails. Close-on-context-done interrupts WASM;
the per-file deadline also bounds tree traversal. Default workers are
`min(4, CPUs)`, timeout five seconds, and parsing size limit 2 MiB.

CLI `index` and `status` accept a repository path and `--repository` logical ID
(directory basename by default); `index` also accepts `--workers`, `--timeout`,
`--max-file-size` and `--emit`. The emitted stream is also ingested atomically.
Use a temporary stream under the ignored `.graphvein/` directory rather than
keeping another complete serialized snapshot in CLI memory.

## Measurements

Windows/amd64, existing pinned RealWorld React checkout. Both cold and warm
measurements use a fresh child process and fresh SQLite store. Only compilation
files survive between them; all 73 supported source files are parsed in each.
The harness separately primes all three grammars to measure parser startup, then
measures full scanning, extraction, linking and ingestion. Process peaks include
everything, measured by `GetProcessMemoryInfo`.

| Workers | Cache | Parser startup ms | Index ms | Ingest ms | Peak resident MiB |
|---:|---|---:|---:|---:|---:|
| 1 | cold | 447.85 | 721.33 | 121.85 | 192.02 |
| 1 | warm | 50.54 | 740.72 | 133.07 | 190.51 |
| 4 | cold | 436.58 | 541.15 | 134.19 | 226.41 |
| 4 | warm | 51.22 | 553.52 | 133.92 | 234.03 |

The fixture body edit reparses one file and writes one file row/two node rows,
with no changed edge rows; its dump matches a fresh full index. The corpus's
second CLI run reparses zero files, reuses 73, writes zero graph rows, and takes
134.17 ms to index plus 91.12 ms to ingest (first: 917.89 + 129.45 ms).
Diffing is sufficient for this corpus; it does not justify a delta contract.

## Consequences

Complete snapshots preserve interchangeability and rollback guarantees while
avoiding expensive unchanged work. Link resolution still traverses cached call
candidates, and ingestion still reads every wire record. Old-payload and seen-key
maps require O(snapshot size) memory; the 128-record write buffer remains bounded,
but total ingestion memory is no longer claimed bounded independently of graph
size. This is a deliberate measurable tradeoff, not a streaming-delta solution.

## Known gaps

Postgres tests are wired to the same production golden and incremental cases,
but local execution is unavailable: no configured test URL or running Docker
daemon. Linux CI provides the configured Postgres service. Cross-platform runs
need CI execution after push; local cross-builds cannot prove those runs.
Measurements are individual samples, not statistical guarantees. They cannot
be compared as speed ratios with T002's five-language, 678-file parsing-only
corpus. Large repositories and long-running engine memory need further profiling.
Working-tree overlays, semantic disposition hashes, other languages, external
indexers and IDE coordinate conversion remain in their later tasks.

## Review notes (developer, 2026-10-05)

- Memory: about 190 MiB peak at one worker for 73 files, unchanged by the warm
  cache. Break peak memory down (compiled code, retained text, comparison maps)
  and profile a ~5k-file repository before M2.
- Postgres parity: CI must fail, not skip, when the Postgres URL is missing
  (T007). Local Postgres runs remain unverified.
- Repository ID: the basename default breaks team mode when checkouts are named
  differently. Move it into `graphvein.yaml` (T008).
- Scanner: record the language of every recognised file even without an
  extractor (Go files currently report `unknown`) (T007).
- Overload identities include parameter text; renaming a parameter changes
  identity. Accepted as semantic; document it alongside the T007 extractors.
- The parsing section (wasitter pin, shared compilation, cache namespace) is
  subject to the adapter-ownership ADR from T005 (ADR 0007), which must state
  whether it amends this section.
- Disk compilation cache verified on the developer machine (Windows) with fresh
  stores: self-index parse/extract 462 ms cold, 72 ms warm, cache created under
  the user cache directory. A reused fact cache parses nothing and therefore
  never compiles grammars. macOS runs on main after merge only, to save CI
  minutes.

## Review notes (developer, 2026-10-07, after T008c)

- The built-in indexer now validates every fresh and replayed record, including
  linker-created synthetic nodes, before streaming. A file with an invalid
  record keeps its file record, gains one extract_invalid_record diagnostic and
  contributes no nodes or edges; fresh cache entries are saved only after
  validation. Root cause fixed: web-root imports became an empty npm name.
  Postgres parity verified in CI.
- Rejection is per file: an invalid shared synthetic node rejects every file
  referencing it. Narrow to dropping the record and its edges if it ever fires
  on real code (zero occurrences in the T008c corpus sweep).
- Full validation of replayed cache entries is O(snapshot) work on every run.
  Measure the warm run in the ~5k-file profile before considering skipping
  validation for entries already validated under the same extractor version.
- Parser deadlines were hit when tests ran concurrently with corpus indexing.
  If CI fails with deadline diagnostics, investigate deadline-dependent tests.

## Review notes (developer, 2026-10-08, after T009)

- Langflow full root (7,341 files, ~200k records): cold peak 4.8-5.2 GiB at
  1 and 4 workers; Go heap 4.4-5.0 GiB, dominated by retained detached parse
  trees (parse.Batch returns every tree before extraction). Peak memory grows
  with total files, not workers (~0.7 MB per file). Before M2 route work:
  extract each file as its parse completes and release the tree (new ADR).
- Unchanged warm runs rewrite 65 nodes every time; the stream carries 67,674
  node records for 67,635 identities. Suspected duplicate-key diffing bug: if
  duplicates compare only against the previous run, the stored row alternates
  between runs, violating last-record-wins. Fix first; also find why the
  built-in indexer emits duplicate identities.
- Warm no-op cost: ~35 s index + ~24 s ingest. Replay validation 5.9 s and
  2.2 GiB allocated; ingest comparison 13.7 s; wire read/validate 10 s.
  Re-profile after the memory fix before optimising.
- Comparison maps: >= 142 MiB of live strings at ingest; not the peak driver.
- Private bytes minus Go runtime total is 48-52 MiB in every sample: native
  memory (compiled code, mappings) is not the driver.
- Q3 cold runs on fresh copies include ~60 s of first-read scan cost (170 s
  vs 109 s at 4 workers on the pinned checkout; warm scan 5 s), most likely
  antivirus first-touch. Use 109 s as the cold baseline.

## Review notes (developer, 2026-10-08, after T010a)

- The 65 warm node writes were intermediate writes caused by 26
  TypeScript/JavaScript identity collisions (ADR 0011), not a diffing bug.
  Last-record-wins holds, and a warm store equals a fresh index. The
  built-in indexer now emits unique identities (enforced for every language
  fixture); an unchanged Langflow rerun writes zero graph rows. External
  producers that emit duplicate keys still cause intermediate writes;
  accepted.
- First-touch scan cost confirmed: the pinned Langflow checkout scanned in
  103 s on its first read after cloning and in about 6 s afterwards. The
  real Langflow cold baseline is about 110 s index plus 40 s ingest.

## Review notes (developer, 2026-10-08, after T010b)

- The retained-tree memory issue noted after T009 is resolved by ADR 0012
  (bounded parse/extract pipeline), which amends this ADR's parsing section
  for result lifetime only.
