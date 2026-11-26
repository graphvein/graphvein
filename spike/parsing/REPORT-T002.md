# T002 — Grammar coverage and real-file benchmarks

Measured on 2026-10-05, Windows/amd64, Go 1.27.1, 16 logical CPUs
(AMD64 Family 25 Model 80, AuthenticAMD), GOMAXPROCS=16, CGO disabled.
No system software was installed. Dependencies remain wasitter v0.1.0 /
wazero v1.10.1; the embedded C runtime is upstream tree-sitter v0.25.0.
ADR 0002 remains **Proposed**, for the developer to accept.

Seven grammars load and match hand-written declaration/line goldens. Four are
**buildable**, with pinned generated sources, but have no validated compatible
prebuilt module. All 678 supported files (1,171,930 bytes) in the five public
repositories parse without syntax errors, runtime failures or panics at all
three worker counts. This is smoke/robustness evidence, not extraction accuracy.

## Grammar coverage

The pinned runtime supports language ABI **13–15** (wasitter `language.go`).
Grammar ABI and the Go/WASM adapter ABI are separate compatibility checks.
The seven successful ABIs below are read from the loaded guest, not inferred
from release numbers. Their modules include the C runtime and wasitter's
`tsw_*` bridge, and require WASI preview 1. The upstream browser modules use a
different linking/import model.

| Grammar | Status and prebuilt source | Pin | Language ABI | Licence |
|---|---|---|---|---|
| javascript (JSX included) | Available: wasitter release | v0.25.0 | 15, loaded | MIT |
| typescript | Available: wasitter release | v0.23.2 | 14, loaded | MIT |
| tsx | Available: wasitter release | v0.23.2 | 14, loaded | MIT |
| html | Buildable: own release has browser WASM, incompatible here | v0.23.2 | 14, generated source | MIT |
| angular | Buildable: no release WASM attachment | v0.9.2 | 15, generated source | MIT |
| rust | Available: wasitter release | v0.24.2 | 15, loaded | MIT |
| go | Available: wasitter release | v0.25.0 | 15, loaded | MIT |
| python | Available: wasitter release | v0.25.0 | 15, loaded | MIT |
| java | Available: wasitter release | v0.23.5 | 14, loaded | MIT |
| c_sharp | Buildable: own release has browser WASM, incompatible here | v0.23.5 | 15, generated source | MIT |
| dart | Buildable: no GitHub releases found | `be07cf7118d3dba06236a3f19541685a68209934` (package 1.0.0) | 15, generated source | LICENSE is MIT-style; package declares ISC |

**Evidence and reproduction:**

- [wasitter v0.1.0 assets](https://github.com/zema1/wasitter/releases/tag/v0.1.0):
  registry, checksums and full notices are already retained under `grammars/`.
  Five additional WASM assets were downloaded from this exact release. The
  checksum test now checks all seven embedded modules against `SHA256SUMS`.
- Runtime support and build conventions:
  [language.go](https://github.com/zema1/wasitter/blob/v0.1.0/language.go),
  [DEVELOPMENT.md](https://github.com/zema1/wasitter/blob/v0.1.0/DEVELOPMENT.md),
  [WASM bridge notes](https://github.com/zema1/wasitter/blob/v0.1.0/internal/wasm/README.md).
  These were also inspected in the cached pinned Go module, independently of
  whether GitHub's web rendering was available.
- HTML [release](https://github.com/tree-sitter/tree-sitter-html/releases/tag/v0.23.2),
  [parser ABI](https://github.com/tree-sitter/tree-sitter-html/blob/v0.23.2/src/parser.c),
  [licence](https://github.com/tree-sitter/tree-sitter-html/blob/v0.23.2/LICENSE).
- C# [release](https://github.com/tree-sitter/tree-sitter-c-sharp/releases/tag/v0.23.5),
  [parser ABI](https://github.com/tree-sitter/tree-sitter-c-sharp/blob/v0.23.5/src/parser.c),
  [licence](https://github.com/tree-sitter/tree-sitter-c-sharp/blob/v0.23.5/LICENSE).
- Angular [release](https://github.com/dlvandenberg/tree-sitter-angular/releases/tag/v0.9.2),
  [parser ABI](https://github.com/dlvandenberg/tree-sitter-angular/blob/v0.9.2/src/parser.c),
  [grammar](https://github.com/dlvandenberg/tree-sitter-angular/blob/v0.9.2/grammar.js),
  [licence](https://github.com/dlvandenberg/tree-sitter-angular/blob/v0.9.2/LICENSE).
- Dart [parser ABI](https://github.com/UserNobody14/tree-sitter-dart/blob/be07cf7118d3dba06236a3f19541685a68209934/src/parser.c),
  [package metadata](https://github.com/UserNobody14/tree-sitter-dart/blob/be07cf7118d3dba06236a3f19541685a68209934/package.json),
  [LICENSE](https://github.com/UserNobody14/tree-sitter-dart/blob/be07cf7118d3dba06236a3f19541685a68209934/LICENSE).
  Clarify its inconsistent licence metadata before shipping an asset.

`cmd/coverage -probe <path>` attempted to instantiate both upstream HTML/C#
release modules with `wasitter.NewParserFromWASM`. Both returned
`instantiate WASM: module[env] not instantiated`. Their language ABIs fall
within range, but these binaries are not drop-in WASI/wasitter modules.
This does not prove that another adapter could not run them.

- Probed `tree-sitter-html.wasm` SHA-256: `c48fcd82c7ea8bf943180088ba7f28c48b2bb5287874179168bf9d31e394cf85`.
- Probed `tree-sitter-c_sharp.wasm` SHA-256: `6f69e1cae44e1c32c1eccc170dc5a9778fb94ff716f71113fe1f8c4299aa2f40`.

### Smoke fixtures

`go run ./cmd/coverage` checks samples, syntax errors, loaded ABI and the exact
ordered entries against their goldens. `go test ./...` includes this suite.
The original TSX/Rust goldens are unchanged. New goldens are hand-written for
JavaScript/JSX, TypeScript, Go, Python and Java. The throwaway walker now covers
their function/class/interface/type/method declarations and Angular metadata.
It does not claim complete symbol extraction. T001's 6,000-line synthetic test
now expects 5,000 entries because it also sees 1,000 interfaces and 1,000 methods;
no old golden was regenerated to accommodate a mismatch.

| Grammar | Sample | Hand-checked declarations / structure (one-based lines) |
|---|---|---|
| javascript | `samples/javascript.jsx` | greet 2, arrow component Card 3, Store/get 4; import 1 |
| typescript | `samples/typescript.ts` | ItemsComponent 3, ItemsService/list 5, InlineComponent 7, identity 8; decorators/metadata 2, 4, 6 |
| tsx | `samples/items.tsx` | Items 4, ItemCard 9, ItemStore 11; imports 1–2 |
| rust | `samples/items.rs` | Item 4, new 9, handler 14, app 18; use declarations 1–2 |
| go | `samples/go.go` | Item 2, NewItem 3, Label 4 |
| python | `samples/python.py` | Item 1, __init__ 2, label 4 |
| java | `samples/java.java` | Item 2, label 3, Repository/find 5 |
| html | `samples/html.html` | Prepared; no compatible grammar loaded, no golden |
| angular | `samples/angular.html` | Prepared legacy/new-flow case; no compatible grammar loaded, no golden |
| c_sharp | `samples/c_sharp.cs` | Prepared; no compatible grammar loaded, no golden |
| dart | `samples/dart.dart` | Prepared; no compatible grammar loaded, no golden |

### Angular findings

The TypeScript golden reaches `@Component` and `@Injectable`, the selector and
`templateUrl` object pairs, and the inline `template` template-string node.
These decorator arguments are reachable without text-search heuristics.

The Angular template sample includes `*ngIf`, `*ngFor`, property/event/two-way
bindings, interpolation with a pipe, and `@if`/`@for`/`@empty`/`@else`. Angular
v0.9.2's generated grammar includes structural directives, bindings, pipes and
new control-flow nodes, but runtime parsing of this fixture remains **unverified**.

HTML alone is a plausible structural fallback, **inferred from its grammar.js**:
it exposes element/tag/attribute/value structure. Its attribute-name rule can
retain `*ngIf`, `[title]`, `(click)` and `[(ngModel)]` as attribute text, but it
does not parse their Angular expressions. Interpolations, pipes and `@if`/`@for`
are text rather than Angular expression/control-flow trees. This fallback has
not been smoke-tested here because the HTML module does not load.

Parsing TypeScript does not automatically parse embedded templates. Graphvein
would need host-driven injection or a second parse of the template string with
an Angular/HTML parser, with escape handling and original source-line mapping.
`${...}` substitutions would require an explicit policy; the sample is a static
string. No template injection/extractor is implemented by this spike.

JSON (`package.json`, `angular.json`, `tsconfig.json`) needs Go's standard JSON
library; YAML needs a YAML package selected later. Neither needs tree-sitter,
and neither is part of the grammar/benchmark suite.

### Building the four missing compatible assets (not done)

Builds can run entirely in CI and ship checked assets to Go-only consumers.
The shortest path reuses wasitter v0.1.0's `wasitter_abi.c` and tree-sitter
v0.25.0 C runtime, linking each pinned `src/parser.c` plus `src/scanner.c` into
a standalone wasm32-WASI module. Preserve adjacent headers (HTML/Angular
`tag.h`, tree-sitter parser/alloc/array headers), exports and licence notices.
Record archive hashes and artifact checksums, then load the result and add its
hand-reviewed golden before declaring it available.

- HTML/C#: extend wasitter's official-grammar allowlist with the pinned source
  records; use its Go build helper and pinned Docker/Zig builder. Estimated
  0.5–1 engineer-day each including smoke tests, not a measured build result.
- Angular/Dart: their non-official repositories are rejected by the current
  helper allowlist. A reviewed fork/CI-specific build configuration must support
  those pinned sources and scanners with the same bridge. Estimated 1–2 days
  each, with extra risk from scanner compatibility and Dart licence metadata.
- Generated C is checked in for these pins, so regenerating it with a
  tree-sitter CLI is not inherently required. If regeneration is needed, pin a
  compatible CLI in CI. A direct custom build needs a WASI C compiler (Zig or
  WASI SDK/clang). Emscripten/`tree-sitter build --wasm` produces the browser
  linking model and would still need adapter/host work; it is not a drop-in fix.

On this host Docker is present in PATH, but its daemon was not exercised.
`tree-sitter`, `mise`, GCC, Clang and `emcc` are absent from Git Bash PATH.
No compiler, CLI or SDK was installed and no missing grammar was built.

## Corpus and method

The [RealWorld project](https://github.com/realworld-apps/realworld) links to
[CodebaseShow](https://codebase.show/projects/realworld). Its public directory
was queried read-only on the measurement date using the same `Implementation.find`
request as its frontend: `isPubliclyListed=true`, repository URL, libraries and
`markedAsUnmaintainedOn`. React, Express, Angular and the selected Axum entry
are listed with no unmaintained marker. This is directory status, not a guarantee
of future maintenance. Express's latest source commit is from November 2024;
Axum's is April 2026. Older/archived candidates were not used in the final run.

`fetch-corpus.sh` performs the shallow-clone equivalent (git init, depth-1 fetch
of the exact object, detached checkout with LF bytes). It refuses dirty
checkouts. The runner independently verifies pins and clean working trees.
It never runs corpus code or installs their dependencies. `.corpus/` is ignored.

| Corpus | Repository | Pinned commit |
|---|---|---|
| react | [yurisldk/realworld-react-fsd](https://github.com/yurisldk/realworld-react-fsd) | `969709a379b13935b4e1caae0ad8cad548e5879a` |
| express | [kenyipp/realworld-nodejs-example-app](https://github.com/kenyipp/realworld-nodejs-example-app) | `94a864e4e458a5e0ae2b07d3cf762791769e8690` |
| axum-ddd | [CaptainCollie/realworld-rust-axum-sqlx-ddd](https://github.com/CaptainCollie/realworld-rust-axum-sqlx-ddd) | `a3a5bc307f24cdf29eca50ee0ea0c7eeb4d50cb2` |
| angular | [realworld-apps/angular-realworld-example-app](https://github.com/realworld-apps/angular-realworld-example-app) | `dd99ed2cf39c805d719f943c5d7061a5683d98a8` |
| fastapi | [fastapi/full-stack-fastapi-template](https://github.com/fastapi/full-stack-fastapi-template) | `1762adac607a1b29cfc4da129557780beea71616` |

Optional local corpus is supported via the fetch script's argument or `-local`.
It is scanned in place and identified only as `private` in JSON; filenames,
paths and exception details are withheld. It was **not included** in the
committed measurement. No private file names or contents are retained here.

The source walk includes .js/.jsx/.mjs/.cjs, .ts/.mts/.cts, .tsx, .rs, .go, .py
and .java. It does not follow symlinks. It skips .git, node_modules, target,
dist, build, vendor, .venv, venv, __pycache__ and the named agent folders.
It is a spike walk, not the future .gitignore-aware scanner. Tests/generated
source outside those ignores remain included. Paths are relative to each public
checkout. Unavailable HTML/Angular/C#/Dart extensions are counted separately.

Each worker lazily creates its own runtime/parser for each encountered language,
then reuses it sequentially; no parser/runtime/tree is shared across workers.
Every tree is closed before the next file. Initialization/read/parse/traversal
panics are recorded per file and damaged parsers are retired; cleanup panics are
also recorded against the last file using that parser. Any runtime/read/panic
failure makes the benchmark exit nonzero. Syntax-recovery trees remain measurable.

Each file is parsed once per run with no previous tree. Parse timings exclude
initialization, reading, error traversal and tree cleanup. Windows timing uses
QueryPerformanceCounter/QueryPerformanceFrequency; the end-clock boundary adds
a small syscall cost. Initial Go clock measurements rounded many small files
to zero on this host and were discarded. Wall time includes worker creation,
all lazy WASM compilation, source reading, parsing, error inspection and cleanup;
manifest verification/scanning and final aggregation are outside that interval.

Error counts include actual ERROR nodes and inserted missing nodes, including
unnamed punctuation, rather than counting every ancestor with HasError.
Aggregate medians are calculated from individual files, not from corpus medians.
Sum timings under concurrency represent cumulative work, not elapsed wall time.

## Results

[Raw Windows JSON](results-t002.windows-amd64.json) contains counts, times,
the five slowest/worst files per language and per corpus at every worker count,
and any diagnostics. No syntax-error worst-five entries exist in this run.

### Files and parse times, one worker

| Language | Files | Bytes | Sum parse ms | Median/file ms | Syntax-error files |
|---|---:|---:|---:|---:|---:|
| javascript | 29 | 25,666 | 9.557 | 0.088 | 0 |
| python | 43 | 94,064 | 38.749 | 0.436 | 0 |
| rust | 46 | 168,028 | 69.447 | 1.029 | 0 |
| tsx | 85 | 242,930 | 75.860 | 0.644 | 0 |
| typescript | 475 | 641,242 | 253.415 | 0.257 | 0 |

Go and Java have zero real files in this corpus, so their throughput and
real-file syntax-error rates are unmeasured. Both have golden and cold-start
coverage. Seventeen HTML files (41,339 bytes) are skipped: React 1, Express 1,
Angular 11, FastAPI 4. No C# or Dart files occur in these public checkouts.

### Worker scaling and peak process memory

Every row is a fresh benchmark child process, with a new compilation/runtime
state. Windows GetProcessMemoryInfo's **PeakWorkingSetSize** is the lifetime
peak resident working set, not a sampled end-of-run working set or Go heap.
**PeakPagefileUsage** is also shown as the peak commit counter; it is not a
claim that this much data was written to a paging file. Measurements include
the Go process, embedded assets, compilation code/data and guest memories.

| Workers | Wall ms | Peak working set MiB | Peak commit MiB |
|---:|---:|---:|---:|
| 1 | 1200.359 | 97.30 | 282.03 |
| 4 | 955.172 | 350.41 | 999.84 |
| 8 | 1281.260 | 596.57 | 1868.73 |

For this mix of five active languages, incremental resident peak cost is roughly
84.4 MiB per added worker
from 1 to 4 and 61.5 MiB from 4 to 8.
These are whole-process slopes, not isolated parser allocations. Compilation
overlap, guest memory growth and OS residency make them workload-dependent.
Four workers beat one here; eight are slower than four while consuming more
memory. This small ~1.1 MiB corpus does not justify a production worker default.
No compilation cache is configured/shared across the separate worker runtimes.

### Cold start

Each grammar runs in its own fresh process, reads its small smoke sample,
compiles and instantiates its WASM runtime/parser, and parses successfully before
emitting a signal. The parent measures launch-to-signal receipt, including process
creation, Go startup and a small JSON/pipe overhead. Cleanup is after the signal.
No `go run` compilation or corpus fetch is part of these times. Source/OS caches
may be warm; this is process-cold, not a disk-cold test.

| Grammar | Launch to first successful parse ms |
|---|---:|
| javascript | 153.326 |
| typescript | 147.019 |
| tsx | 159.169 |
| rust | 151.884 |
| go | 136.238 |
| python | 133.862 |
| java | 136.728 |

### Five slowest files per language, one worker

These public paths and timings are a snapshot; the raw file contains independent
lists for the 4/8-worker runs as well. Every listed file has zero error nodes.

| Language | Corpus-relative path | Parse ms |
|---|---|---:|
| javascript | `react/public/mockServiceWorker.js` | 3.124 |
| javascript | `react/webpack.config.js` | 2.170 |
| javascript | `react/.dependency-cruiser.js` | 1.278 |
| javascript | `react/.eslintrc.js` | 0.624 |
| javascript | `react/jest.config.js` | 0.400 |
| python | `fastapi/backend/tests/api/routes/test_users.py` | 7.236 |
| python | `fastapi/backend/app/api/routes/users.py` | 2.629 |
| python | `fastapi/scripts/prepare_release.py` | 2.598 |
| python | `fastapi/backend/tests/api/routes/test_login.py` | 2.419 |
| python | `fastapi/backend/tests/api/routes/test_items.py` | 2.273 |
| rust | `axum-ddd/tests/articles.rs` | 7.947 |
| rust | `axum-ddd/tests/auth.rs` | 7.833 |
| rust | `axum-ddd/src/infrastructure/db/repositories/article_repository.rs` | 3.859 |
| rust | `axum-ddd/tests/errors_auth.rs` | 3.844 |
| rust | `axum-ddd/tests/common.rs` | 3.688 |
| tsx | `fastapi/frontend/src/components/ui/sidebar.tsx` | 6.197 |
| tsx | `react/src/pages/article/article.ui.tsx` | 4.046 |
| tsx | `react/src/pages/profile/profile.ui.tsx` | 3.194 |
| tsx | `react/src/pages/home/home.ui.tsx` | 2.804 |
| tsx | `fastapi/frontend/src/components/Admin/EditUser.tsx` | 2.264 |
| typescript | `angular/src/app/features/article/services/comments.service.spec.ts` | 7.245 |
| typescript | `angular/src/app/features/article/services/tags.service.spec.ts` | 6.696 |
| typescript | `angular/src/app/features/profile/services/profile.service.spec.ts` | 6.184 |
| typescript | `angular/src/app/core/auth/services/user.service.spec.ts` | 5.655 |
| typescript | `express/packages/core/database/DbArticle/DbArticle.ts` | 5.350 |

Zero crashes/panics, runtime failures and syntax-error files were observed in
each of the three runs. This is not proof that all future repositories will be
error-free. Malformed inputs, missing files, a seeded parser panic, parser
replacement, cleanup-panic reporting and private-path redaction are tested
separately; seeded failures are not part of the corpus numbers.

## Fallback if wasitter is abandoned

Retain the pinned offline artifacts as an immediate continuity measure. For a
maintainable replacement, compile upstream tree-sitter's C runtime and generated
grammars to standalone WASI modules, link a Graphvein-owned C bridge, and call
them directly through wazero. Merely loading upstream browser grammar modules
does not provide the runtime or the required import/bridge functions.

The owned glue would need:

- Versioned exports and language ABI checks; WASI imports; guest allocation,
  freeing, source-buffer transfer, ownership and error handling.
- Parser creation/deletion, language selection, fresh/incremental parses,
  cancellation and safe retirement after faults; tree copy/edit/delete and
  explicit worker/resource lifetimes.
- Tree walking/cursors; null/named/error/missing nodes; kind/text/byte ranges,
  row/column locations, children, field names/IDs, node identity and UTF-8.
- Query compile/delete, detailed query errors, cursor iteration, captures,
  ranges, limits and host evaluation of text predicates. Template injection
  remains host orchestration rather than an automatic runtime feature.
- Native-vs-WASM semantic fixtures where a CI toolchain exists, lifecycle and
  malformed-input tests, release checksums/licences and cross-platform tests.

Planning estimate for the narrow API Graphvein needs: 2–4k lines of Go, 300–700
lines of C bridge, 1–2k lines of tests, roughly 2–4 engineer-weeks. Full wasitter
compatibility, all query predicates/incremental editing and untrusted-input
hardening could substantially exceed this estimate. ABI/scanner behaviour and
build reproducibility are the main unknowns. No fallback code was implemented.

## Verification and limits

Run from `spike/parsing/` in Git Bash (see README for optional private input):

```bash
mkdir -p out
export GOCACHE="$(pwd)/out/go-cache"
CGO_ENABLED=0 go test ./...
CGO_ENABLED=0 go vet ./...
go run ./cmd/coverage
bash fetch-corpus.sh
CGO_ENABLED=0 go build -buildvcs=false -o out/bench.exe ./cmd/bench
./out/bench.exe
```

The complete Go suite, seven grammar goldens, original T001 fixtures and checksum
checks pass. The wrong-golden test still verifies readable rejection. `go vet`
and `bash -n fetch-corpus.sh` pass. CGO-disabled builds of all spike packages pass
for Linux/amd64 and macOS/arm64 using `-buildvcs=false`; only Windows was executed.
The local Git ownership mismatch requires command-scoped safe-directory settings
for inspections or `-buildvcs=false` for builds; no global Git config was changed.

This is one recorded final run per worker count and one cold parse per grammar,
not a statistical confidence interval. Absolute times/peaks vary with scheduling
and host load. OS file caches were not flushed. There is no native comparison,
long-running worker memory/leak test, incremental/cancellation/query benchmark,
large-file stress test, or foreign-OS execution. Declaration/line goldens do not
establish framework extraction accuracy. Four compatible grammar assets and the
HTML fallback remain untested, with build paths documented above. No production
parser, indexer contract, storage, VISION or PLAN changes were made.
