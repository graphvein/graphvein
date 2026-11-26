# Grammar asset notices

Retain this file, `UPSTREAM_NOTICES.txt`, `LLVM-LICENSE.txt`, `WASI-SDK-NOTICES.txt`, each grammar's
`*-LICENSES.txt`, `manifest.yaml` and build record with redistributed assets.
T005a ships no newly built assets. CI builds the four pending modules; T005b
must verify loading and reproducibility before review and redistribution.

| Asset | Source pin | Licence | Notice |
| --- | --- | --- | --- |
| JavaScript | tree-sitter/tree-sitter-javascript v0.25.0 | MIT | UPSTREAM_NOTICES.txt |
| TypeScript, TSX | tree-sitter/tree-sitter-typescript v0.23.2 | MIT | UPSTREAM_NOTICES.txt |
| Rust | tree-sitter/tree-sitter-rust v0.24.2 | MIT | UPSTREAM_NOTICES.txt |
| Go | tree-sitter/tree-sitter-go v0.25.0 | MIT | UPSTREAM_NOTICES.txt |
| Python | tree-sitter/tree-sitter-python v0.25.0 | MIT | UPSTREAM_NOTICES.txt |
| Java | tree-sitter/tree-sitter-java v0.23.5 | MIT | UPSTREAM_NOTICES.txt |
| HTML | tree-sitter/tree-sitter-html v0.23.2 | MIT | html-LICENSE.txt; CI html-LICENSES.txt |
| Angular | dlvandenberg/tree-sitter-angular v0.9.2 | MIT | angular-LICENSE.txt; CI angular-LICENSES.txt |
| C# | tree-sitter/tree-sitter-c-sharp v0.23.5 | MIT | c_sharp-LICENSE.txt; CI c_sharp-LICENSES.txt |
| Dart | UserNobody14/tree-sitter-dart be07cf7118d3dba06236a3f19541685a68209934 | MIT text; ISC metadata | dart-LICENSE.txt; CI dart-LICENSES.txt |

The full upstream release notices are preserved verbatim, including wasitter's
MIT bridge, tree-sitter's MIT runtime, Unicode/ICU notices, public-domain endian
header, grammar copyright holders, wasi-libc notices and Zig runtime licence.
The seven existing upstream assets use Zig 0.15.2. Our new modules use WASI SDK
25.0 instead: its exact wasi-libc submodule licences and allocator notices are
retained in `WASI-SDK-NOTICES.txt`; compiler runtime is covered by
`LLVM-LICENSE.txt` (Apache-2.0 with LLVM exceptions), read at the SDK's exact
LLVM submodule pin. Both submodule pins are recorded in the manifest. Zig's licence is retained
for existing assets, not claimed as the new build toolchain's licence.
CI also retains every nested grammar licence, including Dart's bundled
`tree_sitter/LICENSE`. The generated parsers are used without regeneration;
no Node package dependencies or bindings are linked.

## Dart discrepancy

The [pinned LICENSE](https://github.com/UserNobody14/tree-sitter-dart/blob/be07cf7118d3dba06236a3f19541685a68209934/LICENSE)
has copyright 2020–2023 UserNobody14 and others and the MIT permission grant,
notice-retention condition and warranty disclaimer. The pinned `package.json`
says `"license": "ISC"`. We preserve the actual source grant and record MIT
for these source-derived assets; we do not relabel or edit upstream metadata.
Both MIT and ISC permit redistribution, including binaries, with their
copyright/permission notices retained. Neither requires source disclosure.
This resolves the redistribution condition for this pin, without claiming
upstream has corrected its metadata. Any upstream clarification is a follow-up.

Source archive checksums and licence text were checked in T005a. Build output,
linked toolchain details and loading evidence remain pending CI/T005b.
