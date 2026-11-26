# ADR 0012 — Bounded parse/extract pipeline

**Status:** Accepted (developer review, 2026-10-08)
**Date:** 2026-10-08

## Context

T009 measured Langflow cold working sets of 4.8–5.2 GiB for 7,341 parsed
files. Collecting every detached tree before extracting retained 1.6–1.7 GB
at the detachment allocation site alone. Worker count did not bound this cost.
T010b specifies a bounded pipeline with sequential extraction.

## Options considered

- **Single bounded consumer (chosen):** limits detached-tree lifetime while
  preserving sequential extractors and their existing detached-node input.
  Backpressure can leave parsers waiting while extraction runs.
- **Extraction inside each worker (rejected for now):** removes the result
  queue but introduces concurrent extractors and requires race verification.
  Revisit if extraction becomes the bottleneck.
- **Extraction against live trees without detaching (rejected for now):**
  avoids detached allocations but changes every extractor's input type.
- **Keep the batch and free trees incrementally (rejected):** the batch still
  holds all results before extraction begins, retaining the original peak.

## Decision

Amend ADR 0006's parsing section **for result lifetime only**. Parse workers
send indexed detached results through a channel with capacity equal to the
configured worker count. The calling goroutine is the single consumer; it
extracts in arrival order and drops each root before receiving another.
There are at most workers + channel capacity + 1 detached trees alive.

Keep independent guest modules, shared compiled engines, sequential parser
reuse, deadlines, parser panic recovery and guest-tree close before reuse.
Worker modules close before their last result is sent so cleanup errors retain
the existing per-file parse diagnostic. Cancellation stops parsing, drains
queued roots without extracting and waits for worker cleanup before returning
the context error. The consumer recovers extraction panics per file.

Facts are keyed by file path and downstream ordering is unchanged. The legacy
collecting Batch helper remains for existing parser tests and the startup
measurement; the indexer uses Consume. Neither contract nor store, schema,
dependencies, identities, extraction semantics or `extractors-6` changes.
Fact caching and replay remain unchanged. Profiling reports the interleaved
work as one `parse+extract` phase.

## Measurements

Windows/amd64, Go 1.27.1, pinned clean Langflow
`504c02fc47e76087b82b0e7cbe4186e9cdd916d4`. Discarded a scan before timing;
all runs were sequential, separate from tests, with default GC, five-second
deadlines and 2 MiB limits. User compilation-cache writes were denied, so the
existing memory fallback applies. Cold samples use fresh processes/stores.

Cold full root, median [minimum, maximum], three samples per worker count:

| Workers | Peak WS MiB | Peak private MiB | Go heap high-water MiB | Index s | Parse+extract s |
|---:|---:|---:|---:|---:|---:|
| 1 | 875.9 [872.7, 885.4] | 906.6 [905.6, 908.2] | 708.2 [705.5, 712.2] | 215.7 [167.1, 268.9] | 102.8 [99.1, 130.6] |
| 4 | 1306.0 [1305.1, 1318.8] | 1619.9 [1612.1, 1628.2] | 1461.8 [1460.0, 1477.4] | 135.1 [123.9, 137.7] | 41.9 [40.4, 42.7] |

Both working-set medians pass the 1536 MiB limit. **Timing acceptance fails:**
135.1 s exceeds the allowed 119.9 s (T009's 109.0 s + 10%). No parameters
were tuned. One one-worker scan took 96.7 s despite the discarded warm-up;
four-worker scans were 6.0–6.5 s. No samples were discarded or replaced.

One four-worker warm sample: 666.4 MiB WS, 39.7 s index, zero parsed files,
7,338 reused entries and zero graph writes. T009 warm medians were 689.9 MiB
and 35.4 s; functional reuse is preserved, but timing non-regression is not
established by this slower single sample.

Four-worker cold scaling, one sample each:

| Root | Parsed files | Peak WS MiB | T009 parsed files | T009 peak WS MiB (median) |
|---|---:|---:|---:|---:|
| src/backend/base | 743 | 280.4 | 743 | 734.9 |
| src/frontend | 2753 | 707.0 | 2753 | 2021.5 |
| src | 6399 | 1235.8 | 6399 | 4922.5 |
| full root | 7338 | 1304.4 | 7341 | 4859.4 |

Ordinary least-squares WS slope with an intercept, using these four points:
**154.7 MiB/1,000 parsed files**, versus **671.0** for T009's four medians.
Different language mixes, one-sample noise and earlier T010a changes limit
interpretation; this is not a complexity proof. The three-file count difference
predates this change; current main has the same application sources as the
pre-T010b HEAD. See the task summary for snapshot equivalence verification.

All six cold snapshots, the warm snapshot and the full-root scaling snapshot
were byte-identical to main on the pinned checkout, allowing only the header's
`generated_at` value to differ. Every language fixture also matched bytes at
one, two and four workers; all existing goldens passed unchanged.

Raw samples, comparisons, methodology and verification are in the T010b
task record (internal).

## Consequences

Detached-tree retention scales with workers rather than repository file count.
Scanned sources, extracted facts, linking structures and ingestion comparison
maps still scale with repository size. A bounded queue does not bound total
process memory. The tagged lifetime observer retains no trees and is absent
from normal builds. Backpressure leaves extraction single-threaded.

## Known gaps

Windows measurements do not prove cross-platform performance. Postgres is
unreachable in this sandbox; storage is unchanged and its conditional tests
remain for CI. Default user compilation-cache writes are denied here; memory
fallback measurements cannot establish disk-cache performance.
The cold timing requirement is unmet; performance follow-up is required before
this task can be accepted. No extraction, storage or GC-policy tuning is part
of this decision.

## Review notes (developer, 2026-10-08)

- Accepted. Langflow cold peak working set dropped from 4.86 to 1.31 GiB at
  4 workers (0.88 GiB at 1); growth fell from 671 to 155 MiB per 1,000
  parsed files. Snapshots are byte-identical to main; the lifetime bound
  (2 x workers + 1 trees) is verified by test.
- The failed timing gate is superseded, and the timing gap in Known gaps
  above is resolved by this review. The only changed phase, parse+extract,
  matches T009 (41.9 s vs 40.6 s). The total-index difference lies in
  unchanged phases and also appears in the same-session main control
  (130.1 s vs 135.1 s). Whole-run timings on the developer laptop vary by
  about +/-20% between sessions (1-worker samples 167-269 s), so historical
  baselines are unsound gates. Future timing gates use interleaved
  same-session runs of main and the branch.
- Fixed lanes (worker n takes jobs n, n+w, ...) remove dynamic load
  balancing. Not visible on Langflow; switch to a shared job queue if lane
  imbalance appears, delivering cleanup errors through a per-worker final
  message.
- Worker cleanup errors still mark the worker's final file parse_failed even
  when it parsed. Existing behaviour is preserved; consider a run-level
  diagnostic instead.
- Batch duplicates Consume's defaults and engine setup. Re-implement Batch on
  Consume or remove it when its remaining callers move.
- Remaining 4-worker heap (about 1.46 GiB) includes about 512 MiB of WASM
  guest memory (4 x 128 MiB), retained scanned sources and later-stage
  structures. Not blocking M2; re-profile before M5.
