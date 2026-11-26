# Parsing spikes (T001 and T002)

This isolated Go module compares upstream tree-sitter in WASM (wasitter/wazero)
with official cgo bindings. It is throwaway code, not the indexer contract or a
production parser API. Requires Go 1.24+; only the native candidate needs a C
compiler. No tools are installed by the runner.

From the repository root in Git Bash:

```bash
cd spike/parsing
go test ./...
go run ./cmd/parse
go run ./cmd/compare
```

`cmd/parse` prints an ordered flat list of names, kinds and one-based lines per
sample in JSON, with correctness checked against hand-written goldens.
`cmd/compare` generates `samples/large.tsx` (6,000 lines), builds both candidates
for the host, Linux/amd64 and macOS/arm64, then measures every candidate that
builds on the host. Raw results and binaries go in ignored `out/`. Missing native
toolchains are recorded as build failures; they are not replaced with WASM.
WASM build failures or correctness failures make the runner exit nonzero.

The native candidate, on a machine that already has a suitable C compiler:

```bash
CGO_ENABLED=1 go test -tags native ./...
CGO_ENABLED=1 go build -buildvcs=false -tags native -o out/native.exe ./cmd/parse
./out/native.exe -backend native -large samples/large.tsx
```

Run the comparison first to generate the large input. The native build excludes
the WASM backend and embedded assets so binary sizes can be compared fairly.
The runner uses `CGO_ENABLED=0` for WASM and `CGO_ENABLED=1 -tags native` for native.
It retains unstripped, default Go build settings and disables VCS stamping for
both candidates. Cross-built binaries are built, not executed here.

When the sandbox cannot write the user's Go build cache, use a local cache:

```bash
export GOCACHE="$(pwd)/out/go-cache"
```

After dependencies are cached, parsing and comparison need no network access.
The main repository intentionally remains without a Go module for this task.

## T002 coverage and real-file benchmarks

From `spike/parsing/` in Git Bash:

```bash
mkdir -p out
export GOCACHE="$(pwd)/out/go-cache"
CGO_ENABLED=0 go test ./...
go run ./cmd/coverage
bash fetch-corpus.sh
CGO_ENABLED=0 go build -buildvcs=false -o out/bench.exe ./cmd/bench
./out/bench.exe
```

The benchmark starts fresh child processes for each grammar's cold start and
each worker count (1, 4, 8). It writes `out/results-t002.json`. The saved Windows
measurement is [results-t002.windows-amd64.json](results-t002.windows-amd64.json);
methodology, coverage gaps and build paths are in [REPORT-T002.md](REPORT-T002.md).
The corpus is shallow-fetched at the immutable commits in `corpus.tsv`, never
vendored. No repository dependencies or system software are installed. Fetching
requires existing Git and network access; running afterwards is offline.

Optional local corpus, scanned in place with filenames withheld from JSON:

```bash
bash fetch-corpus.sh /path/to/local/repository
./out/bench.exe
```

The private path goes in ignored `.corpus/local-path.txt`. Running the fetch
script without an argument clears that configuration. Alternatively pass
`./out/bench.exe -local /path/to/local/repository`. Do not commit private
source, paths, filenames, or unreviewed local results. Non-Windows builds use Go's
monotonic clock and explicitly report process-memory measurement as unavailable.
On Linux/macOS, the `.exe` output name is harmless; use `out/bench` if preferred.

The public manifest must match clean checkouts before a benchmark runs.
T005b enables HTML/Angular, C# and Dart in this spike only. Angular corpus HTML
uses the Angular grammar; other HTML uses HTML. JSON/YAML configuration is
outside this parsing benchmark. The declaration walker is a
small syntax smoke check, not a production language extractor. T001's native
comparison remains restricted to TSX/Rust and requires an existing C compiler.

## Pinned assets

The seven WASM files, `SHA256SUMS`, `grammar-registry.json` and
`THIRD_PARTY_NOTICES.txt` come from the
[wasitter v0.1.0 release](https://github.com/zema1/wasitter/releases/tag/v0.1.0).
They are committed so building the WASM candidate needs no separate grammar
download or compiler. Tests verify embedded bytes against the release checksums.
The registry records JavaScript/Go/Python v0.25.0, TypeScript/TSX v0.23.2,
Rust v0.24.2 and Java v0.23.5. The native candidate uses the same TSX/Rust versions.
The dependency versions are pinned in `go.mod`/`go.sum`.
Keep the release notices with redistributed WASM assets.

The spike uses the four verified run C (`37428985231`) CI assets in this module
under stable `wasitter-{html,angular,c_sharp,dart}.wasm` filenames. Their original
build records, source licences and SDK notices accompany them. Historical build
records retain `loading_evidence: pending T005b`; the subsequent loading results
are in ADR 0007 and the T005b task record (internal). All eleven grammar goldens
and the inline Angular TS golden run with `go test ./...`.

To replace these with future reviewed assets, copy the four identically named modules,
replace their four checksum rows in `grammars/SHA256SUMS` and refresh the build
records/notices. No Go code or dependency change is needed. Run C assets are
path-clean and byte-identical to run D (`37434582439`).
The offline A/B analysis is `../../grammars/comparison-t005b.json`; full T002
corpus rerun evidence is `results-t005b.windows-amd64.json` (695 files, including
the 17 HTML files, at 1/4/8 workers with zero failures/panics).

See [REPORT.md](REPORT.md) and [the raw Windows results](results.windows-amd64.json).
