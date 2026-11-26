# ADR 0007 — Tree-sitter adapter ownership

**Status:** Accepted (developer review, 2026-10-06)
**Date:** 2026-10-05

## Context

T005a prepares CI builds for HTML, Angular, C# and Dart; T005b supplies the
loading/corpus/reproducibility evidence. ADR 0002 requires pure-Go consumers,
offline bundled assets and isolated worker state. ADR 0006 already implements
shared compilation and disk caching in `internal/parse/parse.go`.

Local inspection of **wasitter v0.1.0's module-cache source** established that
`internal/grammarbuild/compiler.go` links tree-sitter `lib.c`, generated parser,
external scanners and `wasitter_abi.c` into each WASI module. The bridge exports
`tsw_*`, allocation helpers and memory. Grammar-only emscripten assets cannot
satisfy this ABI. Runtime language support is ABI 13–15.

The restriction on non-official owners is in `Grammar.Validate` in
`internal/grammarbuild/registry.go`, alongside a release-tag-only check which
also excludes Dart's commit pin. `language.go:LoadLanguage` resolves guest
exports without a repository allowlist. `NewRuntimeFromModule` accepts a local
instantiated module; production already uses it. Therefore a build registry
patch is distinct from a runtime loading patch. T005a uses a CI-only helper
inside a disposable copy of the pinned module to call `CompileWASM` directly,
without changing upstream code or production dependencies.

## Upstream evidence

Read-only [GitHub metadata](https://api.github.com/repos/zema1/wasitter) on
2026-10-05 reported not archived, last push 2026-09-07T16:20:55Z, zero open
issues. [Recent commits](https://api.github.com/repos/zema1/wasitter/commits?per_page=3)
included documentation changes `d4ae75f` and `942b316`, and CI maintenance
`ef88c05`, all on 2026-09-07. The
[all-issues query](https://api.github.com/repos/zema1/wasitter/issues?state=all&per_page=10)
returned an empty list. There is activity, but no issue-response history from
which to infer responsiveness or acceptance of a proposed patch. No submission
or contact was made. Counts and dates are observations, not maintenance guarantees.

## Options considered

| Option | Effort estimate | Maintenance and risk | Gain / fit with existing code |
| --- | --- | --- | --- |
| Patch upstream builder | 1–3 days plus unbounded review wait | Small patch; acceptance and response unknown | Retain runtime/API. Add explicitly trusted repository/ref pins to the builder; preserve all checksum/path safeguards. Existing compile-once and disk cache stay intact. |
| Graphvein-owned fork | 2–5 days setup/tests, recurring upgrade review | Own releases, security fixes and drift; minimal patch can still grow | Control build allowlist and pins immediately. Retain `NewRuntimeFromModule`, worker isolation and wazero compilation cache; change adapter/cache namespace only when forked artifacts enter production. |
| Thin wazero adapter | ADR 0002 estimate: 2–4 engineer-weeks; 2–4k Go lines, 300–700 C bridge lines, 1–2k test lines | Highest initial ABI/lifetime/query/cancellation risk; owns all compatibility work | Narrow API and no wrapper dependency. Can reuse engine/cache/worker lifecycle design, but must replace wasitter handles, field access, parser/tree/query lifecycle and bridge validation. |

These are planning estimates, not measurements. T005b now demonstrates loading
of all four modules without an adapter patch. The current CI helper builds
non-official pins and avoids needing a fork merely to extend a registry.

### Draft upstream change (not submitted)

Add `LoadRegistryWithPolicy(reader, policy)` with a default policy matching the
current official owner and release-tag rules. An explicit `TrustedPins` policy
would match exact repository, release tag or full 40-character commit, and
archive digest. Reject unspecified pins. Keep path traversal checks, safe archive
extraction, required digest verification and compiler argv construction. The
CLI could accept a reviewed local policy file; it must not have an unrestricted
"trust any repository" switch. Tests should reject an unlisted owner, changed
digest/ref, abbreviated commit and unsafe paths, and allow exactly the reviewed
Angular/Dart pins. This is an API proposal, not implemented behavior.

## Decision

Retain **wasitter v0.1.0 and wazero v1.10.1**, owning the CI source
pins, checksums and build steps. Prefer a narrowly scoped upstream
builder patch if upstream is receptive; do not make approval of that patch a
prerequisite for CI local-asset builds. Keep the fork as contingency and defer
the thin adapter until demonstrated runtime incompatibility or maintenance need.
Run A's SDK-clang modules load, parse and match all four active goldens through
the unchanged pinned runtime. No fork, runtime patch or dependency change is
needed. T005b registers these languages only in the isolated spike, with stable
`wasitter-{html,angular,c_sharp,dart}.wasm` filenames. The spike now uses run C;
replacement required only asset and checksum/provenance updates, with no code change.
No production languages are registered by T005a or T005b.

Commit reviewed WASM assets with their digests, source pins and notices after
T005b validation. Do not download at application build/runtime: this preserves
offline builds and auditable distribution. Fetched runs are separate review
directories, not automatically copied into embedded production/spike assets.

### Relationship to ADR 0006

This draft **does not amend ADR 0006's parsing implementation or current pin**.
The compile-once `engine.compiled` map, serialized compilation, shared wazero
engine, independent guest modules per worker, disk cache and fallback behavior
already satisfy Part 3. Retain `wasitter-0.1.0-assets-v1` for the existing three
production assets. Wazero keys compiled entries by runtime version and digest.
Future adoption of new reviewed assets or an adapter/bridge change must assign
a new asset namespace and extraction version and explicitly amend ADR 0006 in
that later change. The current task changes neither cache nor extraction semantics.

## Consequences

Developer machines continue to need no C toolchain. CI installs pinned WASI SDK
25.0 and tree-sitter CLI 0.25.10 on Linux only and compiles checked-in generated
parsers without regeneration. It invokes the pinned builder's exact compiler
logic but uses SDK clang rather than upstream Zig. Source hashes are verified
before compilation; full licences and provenance accompany outputs. T005b pins
the SDK archive SHA-256 recorded by run A in the manifest and verifies it before
extraction: `52640dde13599bf127a95499e61d6d640256119456d1af8897ab6725bcf3d89c`.
This is a reviewed observed digest, not a claim of an upstream published digest.
CLI has its published digest. Both runs used `ubuntu-24.04`; the workflow keeps
that explicit runner image, though packages within the image can still evolve.

## T005b evidence (2026-10-06)

### Reproducibility

Verified bundles A (`37422818741`) and B (`37423415593`) have identical grammar
pins, source hashes, tool versions, SDK digest and wasitter module sums. Their
Graphvein commits differ (`0eabf7a` / `9b2b76a`), but the compiler inputs recorded
for every grammar match. The offline comparison verifies both bundles' checksum
files before examining bytes. Full hashes and counts are preserved in
`grammars/comparison-t005b.json`.

| Module | Bytes in each run | Differing bytes | Random source prefixes |
| --- | ---: | ---: | ---: |
| HTML | 258692 | 88 | 11 |
| Angular | 373941 | 88 | 11 |
| C# | 5572759 | 88 | 11 |
| Dart | 1398366 | 80 | 10 |

Replacing `/tmp/tmpXXXXXXXX/` prefixes in memory makes each pair byte-identical.
Ten embedded runtime paths occur in all modules; scanner paths add the eleventh
in HTML/Angular/C#. These correspond to assert-bearing sources, consistent with
`__FILE__` expansion rather than debug sections (the linker strips all). Dart
has ten paths and **80**, not 88, differing bytes. Run provenance is intentionally
different and is not the reproducibility target.

The Python build driver now builds all copied sources from the fixed Linux path
`/opt/graphvein-build/work`, refusing an existing work directory and cleaning only
the directory it created. Inspection of v0.1.0 `CompilerOptions`/`CompileWASM`
found no extra compiler flags option and no `CFLAGS` forwarding. Therefore use
the permitted **fixed path alone**; no prefix-map flag, compiler argv copy,
wrapper or wasitter fork. Internal randomized object/output names remain, but
they were not embedded in A/B. An offline byte check rejects `/tmp/`, home/runner,
workspace and Windows host paths, plus the actual `RUNNER_TEMP` and
`GITHUB_WORKSPACE`; it runs on every newly linked module before checksums.
Historical A assets intentionally retain their original paths for evaluation.

Runs C (`37428985231`) and D (`37434582439`) are verified byte-identical for all
four modules with `scripts/compare-grammars.py --require-identical` and path-clean
with `scripts/build-grammars.py --check-paths`. The spike now uses run C modules,
checksums and original build records; licence/notice text is unchanged. The A/B
analysis above and `grammars/comparison-t005b.json` remain historical evidence.
No workflow was dispatched, branch pushed or software installed during replacement.

### Loading, goldens and SDK clang

Run A assets instantiate through unchanged wasitter v0.1.0 / wazero v1.10.1:
HTML ABI 14; Angular, C# and Dart ABI 15. Each sample parses without syntax errors
and matches its active golden. C#/Dart hand-written drafts are promoted unchanged;
the walker reports C# name lines after attributes and Dart function signatures.
HTML goldens list elements; Angular goldens cover `*ngIf`, `*ngFor`, property,
event and two-way bindings, interpolation/pipe, `@if`, `@for`, `@empty`, `@else`.
All seven existing grammar goldens remain unchanged and pass.

This validates SDK clang 19.1.5-wasi-sdk for these samples and corpus, not general
equivalence to upstream's Zig output. No compatible upstream Angular/Dart asset
exists here for an apples-to-apples comparison, and no clang-specific runtime
failure was observed. A pinned Zig build is not needed for the current evidence;
if compiler-specific differences emerge, compare the same pinned sources/bridge
and fixed path using a reviewed Zig version and archive digest in Linux CI,
still invoking `CompileWASM`, before considering adapter changes. Do not fork
wasitter to work around a compiler mismatch.

### Inline templates and corpus

`spike/parsing/internal/parse/inline.go` locates literal `template:` values in
`@Component` object arguments using the TypeScript AST, parses the untrimmed body
with Angular, and adds `literal_start_line - 1` to guest lines. The standalone
inline golden checks multiple components, multiline backticks, a same-line
quoted literal and unrelated metadata; LF and CRLF map to identical TS lines.
Escapes, JS substitutions and computed values produce per-template diagnostics;
a broken Angular template does not prevent a later component from parsing.
This prototype does not resolve imported aliases or evaluate JavaScript.

The six clean pinned T002 public checkouts rerun offline on Windows/amd64,
Go 1.27.1, with 1/4/8 independent workers. All **695 files** parse in every run,
zero failures/panics, no skipped recognized files. All **17 previously skipped
HTML files** parse: 11 Angular, 1 Express, 4 FastAPI, 1 React. Angular templates
use the Angular grammar; other HTML uses HTML. One Angular home template has
two recovered syntax-error nodes at every worker count. This is reported as
syntax recovery, not a crash or a clean syntax claim. No evidence attributes it
to clang rather than the pinned grammar. Private corpus configuration is empty;
no private filenames/results are recorded. Raw cold starts and worker runs are
in `spike/parsing/results-t005b.windows-amd64.json`.

## Known gaps / pending evidence

- **Cross-platform execution:** T005b evidence is Windows/amd64 only. CI must
  exercise the new spike goldens; arbitrary repositories and query/cancellation
  behavior of these four new modules remain outside this evaluation.
- **Syntax coverage:** the pinned Angular corpus home template recovers two
  error nodes. Investigate grammar coverage separately; do not hide recovery.
- **Inline mapping:** raw unescaped literal lines only; alias resolution,
  escape-aware source maps, dynamic templates and columns remain later work.
- **Upstream responsiveness: unknown**; no issues or submitted proposal to judge.
- Dart source LICENSE is an MIT-style grant while package metadata says ISC;
  retain its actual permission text and all nested notices. Both permit binary
  redistribution with notice retention. Upstream metadata clarification remains
  separate from the permission granted by the pinned source.

## Review notes (developer, 2026-10-06)

- Reproducibility verified: runs C and D (fixed build path) are
  byte-identical and path-clean; the spike uses run C assets.
- `check_paths` scans whole modules for 3-byte patterns (`C:\`, `D:\`),
  which can match machine code by chance in multi-megabyte modules.
  Restrict it to string/data content or longer patterns in the next task
  that touches the grammar build.
- The pinned Angular grammar recovers two error nodes in the corpus home
  template. Investigate before Angular structure extraction in M2.
- Registering HTML/Angular/C#/Dart in production `internal/parse` is M2/M7
  work and needs a new asset namespace, an extractor-version bump and an
  explicit ADR 0006 amendment, as this ADR states.
