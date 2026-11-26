# ADR 0028 — Production grammar set v3

**Status:** Accepted (developer review, 2026-10-24)
**Date:** 2026-10-21

## Context

T014a prepares Angular structure extraction without publishing template facts.
ADR 0006 defines shared compilation, independent worker modules and fact caching;
ADR 0008 registers six production grammars. ADR 0007 requires an explicit asset
namespace and extractor-version change when adopting another audited module.
The investigation must establish that recovery in the pinned Angular corpus preserves
element, attribute and control-flow structure before production registration.

## Options considered

- Register Angular only and parse every scanned HTML/HTM file: the developer's
  G1/G2 choice; ownership remains future link-time work.
- Register HTML as well, or parse only component-owned templates: outside the
  agreed grammar set and would prematurely introduce ownership semantics.
- Rebuild or patch the grammar: unnecessary for the structure-preserving corpus
  recoveries; outside this offline task and its audited artifact boundary.

## Decision

Amend ADR 0006's asset list and compilation-cache namespace and ADR 0008's
production set. Register JavaScript, TypeScript, TSX, Go, Python, Rust and
Angular, using `wasitter-0.1.0-assets-v3` and aggregate `extractors-18`.
Preserve shared compilation, isolated guests, deadlines, panic handling,
corruption recovery and ADR 0012's bounded result lifetime.

G1: register Angular only; no HTML grammar is loaded. G2: route every retained
`.html`/`.htm` file to Angular, preserving scanner language `html` and precision
`best_effort`. Cache only ERROR/MISSING spans, with contract v1's half-open,
1-based lines and 0-based UTF-8 byte columns over normalized source. Include
unnamed MISSING nodes, which normal named-child detachment omits. Emit no
template syntax diagnostics, nodes or edges; existing file records and file
nodes remain unchanged. Size/deadline/panic failures retain existing per-file
diagnostics and are not cached. Inline templates remain future work.

Use `internal/lang/template.Facts` with just `error_spans` for HTML cache entries
through the existing store interface. An empty array is a valid clean parse;
a missing/null array is a cache miss. Other languages' cache payloads are
unchanged. Contract v1, storage schema, store interface and dependencies do not
change.

G3: copy the audited spike Angular module byte for byte. The production loader
verifies SHA-256
`343253fe664ecd1c20d49ccfe19991fc5f75a6fbe2834d5642c90da20755d96a`
before compilation. Source: `dlvandenberg/tree-sitter-angular` **v0.9.2**, ABI 15;
source archive SHA-256
`5457e3f5d054287715c6acc6a46e24127976ea315da26c451dbccb0677d3fe20`.
Retain `angular-build.json`, `angular-LICENSES.txt`, `WASI-SDK-NOTICES.txt`,
`LLVM-LICENSE.txt` and the existing complete notices. Do not add Angular to the
upstream builder's `grammar-registry.json`.

The toolchains are deliberately mixed: six upstream wasitter v0.1.0 release
modules use **Zig 0.15.2**; Angular is ADR 0007 **run C (37428985231)**, built
with **WASI SDK 25.0 clang 19.1.5**. The runtime remains wasitter v0.1.0 /
wazero v1.10.1. No source regeneration, download, installation or cgo is needed.

## the investigation diagnosis

At Angular `dd99ed2cf39c805d719f943c5d7061a5683d98a8`, the scanner retains ten
component HTML templates plus `src/index.html`. Home has two nested ERROR nodes,
both inside `*rxLet="tags$; let tags"`: line 55 columns [41,51) covers
`; let tags`, and [47,49) covers `ta`. The smallest enclosing attribute is
`*rxLet`; its identifier and surrounding `div`/`@for` boundaries remain intact.
Removing `; let tags` from a minimal reproduction removes both errors.

Of eight unmodified inline backtick bodies, profile-articles and
profile-favorites each have one ERROR for postfix `!` in `[config]`, respectively
body line 2 columns [61,62) and [62,63). Removing the postfix assertion from
`<app-list [config]="value()!" />` removes the error. The attribute name,
self-closing component and surrounding `@if` boundaries are preserved.
All four errors are class (a); no class (b) occurs. Single-line and multiline
object literals with trailing commas parse cleanly, including the corpus
`[ngClass]` examples; trailing commas do not cause home's errors.

All 19 bodies (11 files and eight inline bodies) have identical error spans at
one worker, four workers, and a repeated one-worker run. The scratch and
production copies have the required digest. None of S1–S3 fires. The public
per-file census is in the T014a task record (internal); private-corpus rows were
kept in scratch only. Aggregates and exact reproductions are in the task summary.

## Measurements

Measurements and sample tables are recorded in the T014a task record (internal).
They use Windows/amd64, Go 1.27.1, default GC, scratch compilation caches,
four-worker Langflow indexing, separate processes and stores, discarded
warm-ups, and interleaved main/branch samples. Startup separately primes every
grammar at one and four workers, with fresh and reused compilation caches.

Langflow medians of three interleaved pairs (seconds; private memory in MiB):

| Metric | Six grammars | Seven grammars | Change |
|---|---:|---:|---:|
| Fresh index s | 128.502 | 132.445 | +3.07% |
| Fresh ingest s | 49.193 | 49.535 | +0.69% |
| Warm index s | 52.423 | 53.143 | +1.37% |
| Warm ingest s | 29.586 | 29.975 | +1.32% |
| Fresh peak private MiB | 3,151.6 | 3,090.9 | -1.92% |
| Warm peak private MiB | 2,442.6 | 2,385.2 | -2.35% |

All four +10% timing gates pass. A branch fresh sample spent 103.763 s scanning,
versus about 6–7 s in other fresh samples; it remains in the results and required
the third pair. Parse+extract remained about 42–43 s throughout. Sample spreads
still exceed 10% for branch fresh index/ingest and main warm index; medians are
session evidence, not a guarantee about arbitrary runs.

Startup medians of two measured pairs, after discarded warm-ups:

| Workers | Compilation cache | Six grammars ms | Seven grammars ms |
|---:|---|---:|---:|
| 1 | cold | 859.891 | 992.775 |
| 1 | warm | 43.051 | 52.860 |
| 4 | cold | 833.161 | 1,003.350 |
| 4 | warm | 42.433 | 46.378 |

Angular fresh indexing parsed and cached all **11 HTML files**, with two cached
error spans and no HTML syntax diagnostics. All Langflow no-op runs parsed zero
files and wrote zero graph rows. Final Langflow snapshot bodies were
byte-identical; only header timestamp and aggregate version differed.

## Consequences

The production binary embeds another 374,053 WASM bytes plus provenance and
notices. The aggregate version invalidates all prior fact caches. Every HTML
file now incurs parsing on a cold index; unchanged files reuse their span cache.
Recovered expression syntax is recorded for later extraction without degrading
the current graph or silently presenting a clean syntax claim.

## Known gaps

- Angular microsyntax `; let name` and postfix non-null assertions have the
  bounded recovery gaps above. Future extractors must respect these spans.
- The investigation covers the current pinned corpus, not arbitrary Angular syntax.
- No template ownership, inline source mapping, components, services, routing
  or template references are introduced by this grammar prerequisite.
- Measurements and execution are Windows-only; macOS/Linux remain CI work.
- The scan-time outlier's external cause is unverified; timing variability
  remains visible in the retained samples.
- Local Postgres is unconfigured; the shared backend test includes the new
  fixture but its Postgres case requires the CI database.
- Aggregate cache versioning still invalidates unaffected languages.

## Review notes (developer, 2026-10-24)

- Accepted. The Angular corpus's 11 HTML files parse and cache; Langflow
  snapshot bodies are byte-identical to main (233,840 lines), with only the
  header version and timestamp differing. Existing goldens change only the
  extractor version.
- Grammar gaps, all confined to expressions and cached as error spans:
  structural microsyntax after a semicolon (`*rxLet="tags$; let tags"`) and
  a postfix non-null `!` inside bindings. Element names, attribute names and
  block boundaries survive. Template extraction (T014b onward) must treat
  pipes and expression references inside error spans as unknown, never as
  absent, so a missing uses_pipe edge is reported rather than silently dropped.
- angular-build.json is the spike build record copied unchanged; its
  "loading_evidence: pending T005b" field is historical. ADR 0007's T005b
  evidence and this task's parse tests verify loading.
- Timing against main, three interleaved pairs (medians): fresh index +3.07%,
  warm index +1.37%, ingest within +1.4%. One branch fresh sample spent
  103.8 s scanning (first-read pattern); it is retained. Seven-grammar cold
  startup is about 1.0 s against 0.85 s for six.
- Postgres CI: TestAngularTemplateCache/postgres passes; only the four known
  helper skips.
- Toolchains are mixed: six Zig 0.15.2 release modules and one WASI SDK 25.0
  clang 19.1.5 module.
