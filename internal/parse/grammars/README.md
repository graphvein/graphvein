# Production grammar set v5

Ten audited wasitter v0.1.0 WASI modules are embedded. Six are byte-for-byte
copies of the audited spike assets under `spike/parsing/grammars/`; Dart,
JavaScript, TypeScript and TSX are byte-for-byte copies of reviewed
`Grammar assets` artifacts.
`SHA256SUMS` records the module digests verified by the production loader.
`grammar-registry.json` preserves the upstream registry, including exact source
pins and source archive SHA-256 digests. Angular, C#, Dart, JavaScript, TypeScript and TSX are built outside that
upstream registry; their provenance is retained in `angular-build.json`,
`c_sharp-build.json`, `dart-build.json`, `javascript-build.json`,
`typescript-build.json` and `tsx-build.json` (the registry's JavaScript and
TypeScript rows describe the replaced release modules). Other registry
entries are provenance only; they do not register production grammars.

| Language | Source | Pin |
|---|---|---|
| JavaScript | tree-sitter/tree-sitter-javascript | v0.25.0 |
| TypeScript / TSX | tree-sitter/tree-sitter-typescript | v0.23.2 |
| Go | tree-sitter/tree-sitter-go | v0.25.0 |
| Python | tree-sitter/tree-sitter-python | v0.25.0 |
| Rust | tree-sitter/tree-sitter-rust | v0.24.2 |
| Angular | dlvandenberg/tree-sitter-angular | v0.9.2 |
| Java | tree-sitter/tree-sitter-java | v0.23.5 |
| C# | tree-sitter/tree-sitter-c-sharp | v0.23.5 |
| Dart | UserNobody14/tree-sitter-dart | 0038280b774b65c1b89ba7fec50624d8baed9fe1 |

All ten grammars have MIT licences (Dart: MIT source text, ISC package
metadata; see `grammars/NOTICES.md`). `THIRD_PARTY_NOTICES.txt` retains the complete
audited release notices: grammar grants, wasitter bridge, tree-sitter runtime,
Unicode/ICU, endian header, wasi-libc and Zig runtime. Redistribute those notices
with the assets. No compilation, fetching, installation or cgo occurs here.
The seven upstream release modules (including Java) were built with Zig 0.15.2. Angular is the
reproducible ADR 0007 run C module (run 37428985231), built with WASI SDK 25.0
clang 19.1.5, using generated sources without regeneration. Its source archive
SHA-256 is `5457e3f5d054287715c6acc6a46e24127976ea315da26c451dbccb0677d3fe20`;
the module digest is `343253fe664ecd1c20d49ccfe19991fc5f75a6fbe2834d5642c90da20755d96a`.
`angular-LICENSES.txt`, `WASI-SDK-NOTICES.txt` and `LLVM-LICENSE.txt` retain its
grammar and toolchain grants alongside the existing bridge/runtime notices.
Redistribute all these notices. The runtime remains wasitter v0.1.0 /
wazero v1.10.1; the cache namespace is `wasitter-0.1.0-assets-v5`. See ADR 0028, ADR 0067 and ADR 0074.

Set v4 (ADR 0067, T021) adds Java, C# and Dart. Java is the wasitter v0.1.0 release
module (module SHA-256
`bdafeee7af6963461de9498f22f9e0a1893a2196bb410f20941a55fab0e24e88`); its grant
is already in `THIRD_PARTY_NOTICES.txt` (`licenses/java/LICENSE`). C# is the
ADR 0007 run C module (run 37428985231, WASI SDK 25.0 clang 19.1.5), source
archive SHA-256
`9628b164369071019368618bdefa446f0aab8acaac47b75d5dfb209e93b8903b`, module
SHA-256 `9075c0442ff28ddd2d0c368994aad2b26de4d79f08df5a22383290ef46208bcf`;
`c_sharp-build.json` and `c_sharp-LICENSES.txt` retain its provenance and grant.
Both are byte-for-byte copies of `spike/parsing/grammars/`. Dart
(tree-sitter-dart `0038280`, developer decision O1) is the `Grammar assets`
run 43909359868 module (WASI SDK 25.0 clang 19.1.5), source archive SHA-256
`7d9f51b4ff20756a21935ce69a2386e6db1e2fda325835d41790e745270e0db8`, module
SHA-256 `ff84ee67832448ba4187f05adb9c991c3bbd7e1fc4e8f5761f94c6daa22e302b`;
`dart-build.json` and `dart-LICENSES.txt` retain its provenance and grants
(the MIT `LICENSE` and the nested `tree_sitter/LICENSE`). Java, C# and Dart
are loadable but not yet routed to extraction (`internal/index`
`supported()` is unchanged).

Set v5 (ADR 0074, T020-TSG) replaces the JavaScript, TypeScript and TSX
release modules with `Grammar assets` run 44249201780 (commit `04d12e2`,
WASI SDK 25.0 clang 19.1.5, tree-sitter CLI 0.25.10), built from the pinned
sources plus the digest-checked patches in `grammars/patches/` and
regenerated (TypeScript and TSX now ABI 15). Module SHA-256: JavaScript
`3cdf36463561d9fc4d2719a9fd3a52b8b8a1ffd2fd61a1d23ec5acbbf97f3fc3`
(661,726 B), TypeScript
`520278cf9f41dbb6d591e99889f25ea1faa80da4063050548eda5803d7b459e2`
(1,672,849 B), TSX
`454fe6226f8c2c2a6ef0813d49e5c9471a6b419450ddb7f1d47e8f7dc79b01be`
(1,710,045 B). `javascript-LICENSES.txt`, `typescript-LICENSES.txt` and
`tsx-LICENSES.txt` retain the MIT grants; the sources are modified MIT works.
