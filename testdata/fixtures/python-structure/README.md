# python-structure fixture

Python structural claim contract (`docs/lang/python-structure.md`, ADR
0069, task T020, the implementation), rebuilt from the investigation probe
(section 8 of the T020-S2 inventory, an internal record) with one file per rejection
form of row PS-F2 (`rejected/`) and more import forms. Nothing here is
run. Several files are deliberately not valid Python 3.14 (`py2.py`,
`syntax_bad.py`, `rejected/`; `compile_bad.py` is valid for `ast` and
rejected only by `compile()`).

Tests:

- `TestPythonStructureFixture` (`internal/lang/python`) indexes this
  directory and checks `testdata/fixtures/python-structure.golden.json`.
- `TestPythonStructureContract` checks that the fixture reaches every
  contract row and the default branch.
- `TestPythonStructureOracle` compares every claimed fact with
  `scripts/oracle/python_structure/oracle.py` (CPython 3.14 `ast`,
  `tokenize` and the import system's rules over the raw bytes).

`meta/` (an explicit root, a later fix): `user.py`
imports meta-path names the root also holds as files (`sys`, `abc`,
`encodings`: python-stdlib), a platform name (`math`: refused),
`__main__` and `sitecustomize` (refused), a namespace `email/` named like
a standard package (refused), a data-only directory `assets/` (a
namespace, not external), dotted names with blanks, a line continuation
and a fullwidth letter (`ｍpkg`, NFKC), a PEP 695 `type` binding next to a
submodule, a binding only in an `except*` handler, and a module
`__getattr__` (all undecided), and a package binding a name from its same-named submodule (`rpkg`: decided, the binding); `scripts/run.py` imports its sibling
(`script_directory_module`); `tools/` is an implicit root (`tools/rpkg/`): `tools/run.py`'s `import rpkg` is refused because the directory alone answers differently from the policy (`meta/rpkg`), `import toolonly` is claimed (both agree); `nfkc.py` defines `ℌ` and `H` (`H`, `H@2`).
`ws/` (row PS-M2, ADR 0072) is a uv workspace:
`ws/src/app/main.py` imports `wscore`, which is a namespace directory
(`ws/src/wscore`, the member's project directory) on the ancestor root
`ws/src` and the member's regular package `ws/src/wscore/src/wscore`
(hatchling's default src layout): the installed package wins. `from
wscore.tests import helper` is unresolved (`tests` is beside the package,
not in it). `ws/src/odd` builds with setuptools (refused,
`python_workspace_member_unread`; `import odd` stays
`namespace_hides_concrete`); `ws/src/dupa` and `ws/src/dupb` both install
`dupe` (`ambiguous_import`); `libs/*` matches `ws/libs/wsglob` (flat
default layout) and `ws/libs/skipme`, which `exclude` removes; `wsext/`
is an editable uv path source of `ws` that the member `wscore` requires
(`packages` plus an `include` inside the package): installed;
`wsundeclared/` is an editable path source no requirement names: not
installed, so its namespace directory on the root still
hides it.

`rejected/` holds one file per rejection form.

Inert names: `_build/` is copied to `build/` (a directory the scanner
skips by default, so `import build` in `cond/user.py` is refused as
`shadowed_module`).

The repository stores every file as UTF-8 text with LF line endings, so
every test writes these cases into its temporary copy
(`structureExtraFiles` in `internal/lang/python/structure_harness_test.go`):
`crlf.py` (CRLF), `lonecr.py` (lone CR line ends), `bom.py` (byte order
mark), `latin1.py` (a coding cookie and a Latin-1 byte: the scanner does
not read it, so `import latin1` is refused as `shadowed_module`),
`fastext.cpython-314-x86_64-linux-gnu.so` (an extension module beside
`fastext.py`: `import fastext` is refused) and `onlypyc.pyc` (bytecode
without source: `import onlypyc` is refused, not external).
