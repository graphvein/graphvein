# CI grammar builds

`manifest.yaml` uses JSON syntax (valid YAML 1.2) so CI can read it with Python's
standard library. It records all eleven languages. Only the seven `built in CI`
entries compile; the four remaining upstream assets (Go, Python, Rust, Java)
are unchanged in the spike and production packages. JavaScript, TypeScript
and TSX carry patches (ADR 0074): `patches` lists each `grammars/patches/*.diff`
with its SHA-256, `regenerate` names the `src/grammar.json` the patch edits,
and `parser_sha256` is the digest the regenerated `parser.c` must have. The
builder verifies the patch digests, applies them with `git apply`, runs the
pinned `tree-sitter generate` on that `grammar.json` (no Node runtime) and
refuses a parser with another digest. Source pins and archive hashes must be reviewed together.

The asset format was determined from **wasitter v0.1.0 in the Go module cache**:
`internal/grammarbuild/compiler.go` compiles the bundled tree-sitter `lib.c`,
generated parser, external scanner and `internal/wasm/src/wasitter_abi.c`.
It uses WASI preview1, C11/O2, `WASITTER_LANGUAGE_FN` and
`WASITTER_LANGUAGE_NAME`, exported memory (32 MiB initial, 256 MiB maximum),
no entry point and the complete `tsw_*` plus allocation export list.
The builder's `__EMSCRIPTEN__` define is part of its compatibility flags;
it does not make the output an emscripten/browser module.

`builder.go.txt` is copied into a disposable, verified Go module-cache copy of
wasitter in CI. It calls `CompileWASM` directly, retaining its exact export list,
compiler flags and ABI bridge, while avoiding the official-repository/tag-only
registry validation. No upstream runtime patch or fork is required by this
build path. All four upstream bindings compile `src/scanner.c`; Angular's
`src/html.scanner.c` is empty and is not compiled. Generated parsers are already
present, so the pinned CLI regenerates only the three patched rows above
(TypeScript and TSX move from ABI 14 to 15; ADR 0074 shows the trees are
unchanged on every corpus).

The workflow installs WASI SDK 25.0 and tree-sitter CLI 0.25.10 on Linux only.
CLI download has its published SHA-256 pin. The SDK archive digest observed in
verified run A is now pinned in the manifest and verified before extraction.
Toolchain provenance/runner-image drift remains a limitation; CI uses
`ubuntu-24.04`. The helper compiles with SDK clang, not wasitter's default Zig;
T005b initially loaded run A assets against the unchanged v0.1.0 adapter; the
spike now uses path-clean run C (`37428985231`), byte-identical to D (`37434582439`).

Sources build from `/opt/graphvein-build/work`, a fixed, exclusively created
directory. `CompileWASM` accepts no extra flags, so this uses the fixed path
alone, without copying argv or forking wasitter. Every newly built module is
checked for `/tmp/` and host/runner-specific paths. The offline entry point is:

```bash
python scripts/build-grammars.py --check-paths grammars/runs/run-C/wasitter-*.wasm
python scripts/compare-grammars.py grammars/runs/run-C grammars/runs/run-D --require-identical
```

Replace C/D with fetched run IDs. A/B deliberately fail these strict checks;
their random source-path-only differences are in `comparison-t005b.json`
(Dart differs by 80 bytes, the other three by 88). C/D must be byte-identical.
Checksums cover run metadata too, so compare modules rather than whole bundles.

`SHA256SUMS` covers modules, licences, notices, manifest and build records.
Records include source digest, ABI, module sums, tool versions, workflow commit
and run. Run metadata differs intentionally between runs; T005b compares WASM
hashes, not the combined checksum file. Do not add timestamps to compiled input.

Fetch successful runs with `bash scripts/fetch-grammars.sh RUN_ID`. Each lands
in `grammars/runs/run-RUN_ID/`, refuses overwrite and verifies the exact artifact
inventory and hashes before copying. It needs existing Git Bash and `gh`;
there is no tool installation. For releases, attach the individual artifact
files and use `--release TAG`. Do not commit fetched modules until review.
