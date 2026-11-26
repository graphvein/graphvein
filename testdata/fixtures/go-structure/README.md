# go-structure fixture

Go structural claim contract (`docs/lang/go-structure.md`, ADR 0068, task
T020, the implementation), rebuilt from the investigation probe
(section 6 of the T020-S1 inventory, an internal record). Nothing here is built or
run: the root, `ext/` and `nested/` are Go modules only so the linker and
the oracle read their `go.mod` files. Several files are deliberately not
valid Go (`gen/`, `selfimp/`, `multi/`, `badbuild/`, `twobuild/`).

Tests:

- `TestGoStructureFixture` (`internal/lang/golang`) indexes this directory
  and checks `testdata/fixtures/go-structure.golden.json`.
- `TestGoStructureContract` checks that the fixture reaches every contract
  row and the default branch.
- `TestGoStructureOracle` compares every claimed fact with go/parser,
  go/ast and go/build.

- `TestGoStructureGoList` (opt-in, `GRAPHVEIN_GO_STRUCTURE_GOLIST=1`)
  compares the package reader and the import edges with `go list` on this
  module, offline.

The repository stores every file with LF line endings, so every test
(the golden included) writes seven more cases into its temporary copy
(`structureExtraFiles` in `internal/lang/golang/structure_harness_test.go`):
`util/crlf.go` (CRLF), `util/cr.go` and `util/hidden.go` (lone CR, GS-F3;
`hidden.go` is the comment case), `lonecr/hdr.go` (a lone CR
inside the header comment: Go builds the file as `package other`, so
`lonecr` is refused, GS-I5), `util/bom.go` (byte order mark),
`lib/bom_ignore.go` and `lib/crlf_ignore.go` (a `//go:build ignore`
header after a byte order mark and with CRLF line endings).

No file uses an ignored name, so no inert names are needed.

| path | rows | case |
|---|---|---|
| `go.mod` | GS-M1, GS-I4 | module `example.test/structure`; `replace example.test/ext => ./ext` (the replaced module stays external) |
| `ext/` | GS-I4 | the replaced module (own go.mod) |
| `nested/` | GS-I3 | a nested module under the root module's path |
| `lib/lib.go` | GS-D1..GS-D5, GS-D7, GS-D8, GS-I4, D | grouped, dot, blank, aliased and `import "C"` imports; struct, generic struct, type-set interface, alias, generic alias, named type; value, pointer, generic and `[_, _]` receivers; two parenthesised receivers (D, `receiver`); two `init`, two `func _` (`_`, `_@2`); `var A, B = 1, 2`; grouped values with unexported and blank names; iota constants; a body-less function; a local type; Unicode names |
| `lib/lib_test.go` | GS-I1 | an internal test file: a target of `lib/ext_test.go` and `lib/ext2_test.go`, never of another directory |
| `lib/ext_test.go`, `lib/ext2_test.go` | GS-I1 | external test files importing `lib`: each sees `lib_test.go`, never the other `lib_test` file nor itself |
| `lib/gen.go` | GS-I1 | `//go:build ignore`, `package main`: not a target |
| `lib/_skip.go` | GS-I1 | `_`-prefixed name: not a target |
| `lib/os_windows.go`, `lib/os_linux_386.go` | GS-I1 | one port each: targets (union over ports) |
| `lib/os_plan9_wasm.go` | GS-I1 | a port that does not exist: not a target |
| `lib/tagged.go`, `lib/cgo_only.go` | GS-I1 | custom tag `integration`; `cgo && (linux \|\| darwin)`: targets |
| `lib/never.go`, `lib/gccgo.go` | GS-I1 | `linux && !linux`; `gccgo` (the gc compiler only): not targets |
| `lib/legacy.go` | GS-I1 | `// +build linux` followed by a blank line: a target |
| `lib/legacy_off.go` | GS-I1 | `// +build ignore` followed by a blank line: not a target |
| `lib/legacy_noblank.go` | GS-I1 | `// +build ignore` directly before the package clause: go/build ignores it, so a target |
| `lib/late.go` | GS-I1 | `//go:build ignore` after a block comment: still the header, not a target |
| `lib/after.go` | GS-I1 | `//go:build ignore` after the package clause: not a constraint, a target |
| `multi/` | GS-I5 | two buildable files with different package clauses |
| `badbuild/` | GS-I5 | a `//go:build` line that does not parse |
| `twobuild/` | GS-I5 | two `//go:build` lines |
| `selfimp/` | GS-I5 | a file importing its own package (`self_import`) |
| `testonly/` | GS-I2 | a directory with only a test file: `unresolved_import` for `util/u_test.go` |
| `_hidden/`, `testdata/pkg/` | GS-I1 | `_`-named and `testdata` directories imported explicitly |
| `util/u.go` | GS-I1..GS-I5 | imports of every class, including `example.test/structure/missing` (GS-I2) |
| `util/u_test.go` | GS-I1, GS-I2 | a test file of another directory importing `lib`: no test file of `lib` is a target |
| `util/lined.go` | GS-D1 | a `//line` directive: spans stay in the file's own lines |
| `gen/dup.go` | GS-D6 | `func F` and `func (D) M` twice each: all four to D (`redeclaration`) |
| `gen/recv.go` | GS-D4, D | `**R` and `strings.Builder` receivers (D, `receiver`), a plain receiver |
| `gen/broken1.go`, `gen/broken2.go` | GS-F2, GS-I5 | files go/parser rejects: no declaration, counted as `syntax_error`; `gen` is refused as a package (go list accepts it: Graphvein's own rule) |
| `gen/recv.go` `Two` | D | `func (a, b R) Two()`: one receiver declaration with two names (`receiver`) |
| `imports/i.go` | GS-I1, GS-I2, GS-I5 | imports of the probe directories below |
| `doconly/` | GS-I2 | only a `package documentation` file: nothing to import, `unresolved_import` (A3) |
| `docmix/` | GS-I1 | a `package documentation` file next to `package docmix`: the edge reads `m.go` only (A3) |
| `nocgo/` | GS-I1, GS-I4 | `b.go` is `//go:build !cgo` and imports `"C"`: never built, so the edge reads `a.go` only (A4) |
| `badtest/` | GS-I5 | a `_test.go` file with a foreign package clause: go/build's MultiplePackageError (B2) |
| `cgotest/` | GS-I5 | a `_test.go` file importing `"C"`: refused for `imports/i.go` and for the external test file `x_test.go` (B2) |
| `prog/` | GS-I1, GS-I5 | `package main`: refused from `imports/i.go` ("is a program"), read by its own external test `x_test.go` (B2) |
| `lonecr/` | GS-I5 | refused: `hdr.go` (written by the tests) has a lone CR |
| `malformed/m.go` | D | `..`, `.`, `//`, trailing slash and an external `fmt/../os`: no edge, `malformed_import_path` (A2) |
| `bind/`, `binduse/u.go` | GS-I1 | `bind/a_gen.go` (`//go:build ignore`, `package main`, its own `F`) sorts before `bind/lib.go`: `bind.F()` and `l.F()` bind to `bind/lib.go#F`, never the generator (B3; `TestGoPackageCallBindings`) |
| `blockmod/` | GS-M1, GS-I4 | a nested go.mod in the block form `module (`: a module boundary that resolves nothing, so both imports are external (C4) |
