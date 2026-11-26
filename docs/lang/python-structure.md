# Python structure: claim contract

Status: contract of the T020 implementation (2026-11-17,
`extractors-40`) with the fixes of the independent review
(the T020 task record (internal)), ADR 0069 (accepted 2026-11-18),
and row PS-M2 (workspace members installed as packages,
ADR 0072, Proposed, `extractors-43`). The
rows are kept 1:1 with `structureRows` in `internal/lang/python/structure.go`
by `TestPythonStructureContract`. What it checks: the document's row ids
equal `structureRows` in order; the document's default-branch list equals
`unreadForms`, each form with the one row `unreadRows` maps it to; the
rejection-form table equals `rejectForms`; and on the fixture
`testdata/fixtures/python-structure`, every row, every rejection form and
every default-branch form is reached, every counting site reports exactly
its form's row, and no branch reports an id outside the table.
`TestPythonRejectionForms` has a case for every rejection form. What it does
not check: that a form reaches only one claim branch (a refusal branch
reports both D and its row by design), and that D emits nothing (the oracle
and the goldens see that). `TestPythonStructureOracle` compares
every claimed fact with `scripts/oracle/python_structure/oracle.py`, which
reads each file's raw bytes with the reference CPython's own `ast` and
`tokenize` and re-derives imports from importlib's `PathFinder` /
`FileFinder` rules over a listing of the whole checkout. Rows change only
through a revision of task T020 (or a later task) with a version bump.
Inventory and the investigation results: the T020 task record (internal). Label
rule: row PL-L2 of `docs/extract/precision.md` (added
after the independent review and its fixes): every Python file, test
files included, is labelled `precise`, except a file whose structure
claims nothing (row PS-F2, a file Graphvein could not parse or extract, or
one dropped by validation), which the indexer stores as `best_effort` and
the server serves with reason `structure_refused`. The label covers the
rows below, never calls (`code_calls`, row D).

Scope (T020 decision O5 (a)): Python files, declarations and import
statements, as emitted by `internal/lang/python` (`Extract`,
`LinkWithResolver`). Calls are a separate scope with no contract and stay
Best effort (`code_calls`, row D). FastAPI routes (`docs/extract/routes.md`),
SQLModel / SQLAlchemy models (PL-F2) and table access (PL-F9) in Python
files are other rows.

**Every form no row below names goes to the single default branch D: it
claims nothing (no node, no edge). Forms inside the scope that D receives
are counted (one `python_structure_unread` diagnostic per file); forms
outside the scope (listed under "Not modelled") are not counted.**

Reference Python: CPython 3.14 (the oracle ran CPython 3.14.10, built from
source tag v3.14.10). The version is one constant (`referencePython`):
row PS-F2's version-dependent forms test it.

Input text: the scanner's text (CRLF and lone CR as LF, contract v1).
CPython's tokenizer also ends a line at LF, CRLF and a lone CR, so both
read the same lines. Columns are byte columns of the raw line; a UTF-8 BOM
counts on line 1. A file that is not valid UTF-8 (a coding cookie with a
Latin-1 byte, say) is not scanned and has no record. Names are the
parser's: identifiers NFKC-normalised as CPython does (PEP 3131;
`golang.org/x/text/unicode/norm`), and a dotted module name is
its NAME tokens joined by `.` (white space, comments and line
continuations between them are not part of it).

Unicode versions (ADR 0069, revision of 2026-11-24): the reference Python's
`unicodedata` is Unicode 16.0.0; the tables Graphvein uses
(`golang.org/x/text` NFKC and character names, Go's `unicode` categories
under the toolchain in `go.mod`) are Unicode 17.0.0. No claim depends on a
character whose identifier properties or name differ between the two:
`internal/lang/python/unicodeversion.go` lists them (generated from
CPython 3.14's and 3.15's `unicodedata` by
`scripts/oracle/python_structure/unicode_tables.py`: 4,699 code points
whose identifier start or continue property, letter, upper-case or digit
class, combining class or NFKC form differ, all characters new in 17.0;
10,948 whose name differs), and a file using one is refused (PS-F2,
`unicode_version`). `TestPythonUnicodeVersion` checks that the lists
match the tables' version and, with an interpreter of either version,
compares Go's tables with its `unicodedata` on every code point (outside
the lists for the reference). Identifier validity is the grammar's: it
agrees with CPython 3.14's `str.isidentifier` on every non-ASCII code point
(measured once, ADR 0069) and reads none of the listed identifier
characters as part of a name (checked by the test). No other Unicode
property is read for Python: a `\N{...}` name is upper-cased over ASCII
only, as CPython's lookup does (a non-ASCII character in it is
`string_literal`), and a PS-M2 project name must be ASCII (hatchling's
`is_valid_project_name`), so neither reads case mapping or letter
classes.

**Assumptions the oracle shares and cannot verify** (ADR
0069, Known gaps): the root set and order of PS-M1 (one launch directory
does not exist for a repository); the declared environment of PS-M2 (ADR
0072): the environment uv's discovery gives a file is synced by default,
and its installed packages come after the ancestor roots (and after the
standard library), as site-packages comes after the script directory and
`PYTHONPATH`; a relative import's package is the
importer's directory chain under the repository root, so `src/a.py` with
`from . import b` claims `src/b.py` (true when `src.a` is imported from the
repository root); the roots come before the standard library, which holds
for the script directory, the current directory and `PYTHONPATH`, not for
`.pth` files or editable installs; a namespace package on the ancestor
path ends the search (PS-I6); `.pyi` stubs are never targets; the meta-path
and platform lists are the reference build's (Linux, CPython 3.14.10):
another build's built-in set may differ (a platform name is refused for
that reason).

Status column: **oracle** = `TestPythonStructureOracle` reports 0
disagreements on every claimed fact of the row on the fixture, fastapi,
langflow and the self-index (numbers in ADR 0069); **policy** = shared
with the oracle, not verified by it.

## Module roots (input to the import rows)

| row | form | claim | refusal (to D) | status |
|---|---|---|---|---|
| PS-M1 | the root set of `docs/lang/python-resolution.md`: repository root, directories of scanned `pyproject.toml` / `setup.py` / `setup.cfg`, their `src` child holding Python sources, implicit roots (parent of a top-level regular package) | the search path for an importer: the ancestor explicit roots deepest first as one path, then each other explicit root alone (exactly one hit resolves), then the implicit ancestor roots as one path | several other roots resolve the name: `ambiguous_import` (PS-I7); an absolute import in a file whose directory is neither a regular package (no `__init__.py`) nor a root, when that directory holds the top-level name (a module, package, namespace directory or unread candidate): D, counted (`script_directory_module`): `python dir/run.py` and pytest's default `prepend` mode put that directory first on `sys.path` (OP-1 (a)); an absolute import in a file whose directory is an implicit root (and neither an explicit root nor a regular package), when that directory holds the top-level name and `find` over that directory alone answers differently from the search path (another module or package, other namespace portions, nothing, or an unread candidate first): D, counted (`script_directory_module`); the same answer is claimed (S2F open point 2, decision) | policy: an edge states the module the name resolves to under this policy, not under some interpreter's `sys.path` (open point O-S2-9 (a)) |
| PS-M2 | a project installed in the importer's environment (ADR 0072, review fixes S2XF), read as uv 0.13.9 discovers it (`uv-workspace/src/workspace.rs`): the importer's project is the nearest directory above it with a scanned `pyproject.toml`; its environment is the workspace that project declares, or the workspace of the first `pyproject.toml` above it when that one declares `[tool.uv.workspace]` with the project as a member, else the project alone. Members: the root when it has `[project]`, plus every directory the `members` globs match (glob crate 0.3.4: `*` and `?` inside one component, the root's own name literal) that no `exclude` glob matches (`*` and `?` cross `/`), a match without `pyproject.toml` skipped when hidden or holding no file; `tool.uv.managed = false` members left out. Installed: every member uv packages (`tool.uv.package`, else a `[build-system]` table), and every editable uv path source (`{ path = "...", editable = true }`, the member's own or the root's) that an unconditional requirement names (a member's `project.dependencies`, the root's default dependency groups; PEP 503 names). Its packages: `build-backend = "hatchling.build"`, no `hatch.toml`, a static `project.name`, hatch `packages` (each a directory with a scanned `__init__.py`; `include` only inside them) or hatchling 1.31.0's default `<name>/__init__.py`, then `src/<name>/__init__.py` | one search path entry right after the PS-M1 ancestor roots (before the other roots) holding exactly the installed packages, as site-packages does; `find` applies PathFinder over the ancestor roots plus it: a concrete package found earlier wins, namespace portions found earlier do not stop the search, so the installed package wins over them (langflow's `src/lfx` namespace before `src/lfx/src/lfx`) | a workspace uv rejects or PS-M2 cannot read exactly (a match without `pyproject.toml` holding files, which uv rejects or skips when every file is gitignored; a member without `[project]`, declaring its own workspace, or sharing a name; another `[tool.uv.workspace]` key; `[`, `]`, `\` or `**` in a glob; a walk into a directory the scan skipped; an unreadable `pyproject.toml`; a root with `managed = false`): no entry; a project whose layout cannot be read exactly (another backend, a `hatch.toml`, a dynamic name, a name hatchling 1.31.0's `is_valid_project_name` refuses, read over ASCII (ASCII letters and digits, with `.`, `_`, `-` inside; its case-insensitive match also takes `ſ`, `ı`, `İ`, U+212A KELVIN SIGN and a trailing newline, refused here: an over-refusal), any other hatch build option such as `sources`, `only-include`, `force-include`, `exclude`, `dev-mode-dirs`, `dev-mode-exact` or hooks, a package without a scanned `__init__.py`, a module or namespace default layout) or a path source that is not editable: no entry for it; each with one `info` diagnostic `python_workspace_member_unread` on that `pyproject.toml` (imports keep their PS-M1 answer). Not installed, no diagnostic: a project uv does not package, an undeclared or conditional path source. A package named like a standard-library, built-in or frozen module: no PS-M2 answer for that name (the standard library comes first). A name two installed projects provide: `ambiguous_import` (PS-I7) | policy: the environment is assumed synced by default (`uv sync --all-packages`: all members, no extras, default groups); the member set, scope and packages are read as stated; the oracle reads them on its own (tomllib over the raw files, its own glob and discovery code) but from the same reading of uv and hatchling (ADR 0072) |

## Files

| row | form | facts claimed | refusal (to D) | status |
|---|---|---|---|---|
| PS-F1 | a scanned file with language `python` (`.py`, `.pyi`, extension-less file with a python shebang) | file record and level-3 `file` node, `language: python`; the file exists at this path with this content hash | - (the scanner's inventory decides) | oracle |
| PS-F2 | a file the reference Python rejects, as Graphvein detects it (see Rejection forms) | the file node only, plus a `python_syntax_rejected` warning naming the form (a tree-sitter error keeps the indexer's `syntax_error` warning instead) | every declaration and import goes to D, counted (`syntax_error`); the file's calls, routes, model syntax and module bindings go with them, so no other file resolves a name through it | oracle |
| PS-F3 | a file `ast` accepts and `compile()` rejects (`return` at module level, `nonlocal` at module level, duplicate parameters, a binding of `__debug__` in any spelling: `__debug__ = 1`, `def __debug__()`, `import a as __debug__`, a parameter or keyword argument named so) | as PS-F1 plus its declarations and imports (code as written, open point O-S2-3 (a)) | - | oracle |

### Rejection forms

A file goes to PS-F2 when tree-sitter reports an error in its tree, or its
coding cookie, its tree, or its indentation and nesting as CPython's
tokenizer reads them hold one of these forms, which the grammar reads
without an error but CPython 3.14 rejects (or, for `encoding_cookie`,
reads differently from Graphvein). The classes behind the review's
probes: Python 2 forms (`print`, `exec`, `raise E, V`, `<>`, backticks,
`ur''`, `0777`, `[x for x in a, b]`, tabs mixed with spaces), the
tokenizer's literal and limit rules, and the parser's target and
parameter rules.

| form | example |
|---|---|
| `tree_sitter_error` | an `ERROR` node, or a missing token (the indexer's parse result) |
| `print_statement` | `print "x"` (Python 2) |
| `exec_statement` | `exec "code"` (Python 2) |
| `raise_comma` | `raise E, "msg"`, `raise E, V, T` (Python 2) |
| `diamond_operator` | `a <> b` |
| `backtick_repr` | a backtick-quoted expression (Python 2 `repr`) |
| `string_prefix` | `ur"x"`, any prefix other than `r`, `u`, `b`, `br`, `rb`, `f`, `fr`, `rf`, `t`, `tr`, `rt` (any case; `t` from 3.14) |
| `string_literal` | `b"é"` (a non-ASCII character in bytes), `"a" b"c"` and `t"a" "b"` (bytes or a template concatenated with another kind), `f"{a!x}"` (a conversion other than `!s`, `!r`, `!a`), `"\N{NO SUCH NAME}"` (a name the Unicode tables of `golang.org/x/text` do not hold: aliases and algorithmic names are refused too, an over-refusal; a name only the tables' newer Unicode version holds is `unicode_version`); the name is matched as CPython does, with ASCII letters in any case and every other byte as written, so a name holding a non-ASCII character is refused here even when its Unicode upper case is a name (`"\N{LATIN ſMALL LETTER A}"`, `"\N{latın small letter a}"`, `"\N{KELVIN SIGN}"` with its first `K` written as U+212A KELVIN SIGN) |
| `legacy_integer` | `0777`, `12L` |
| `number_literal` | `1_`, `1_.5`: an underscore not between two digits (or after `0x`, `0o`, `0b`) |
| `except_comma` | `except A, B as e:` (PEP 758, 3.14, allows `except A, B:` without `as`; below 3.14 every comma form) |
| `except_star` | `except*` and `except` in one `try`; `except*:` without a type |
| `parameter_order` | `def f(a=1, b)`, `def f(*)`, `def f(*, **k)`, `def f(**k, a)`, `def f(*a, *b)`, `def f(/, a)`, `def f(a, /, /)`, `def f(*, a, /)`, also in `lambda` |
| `argument_order` | `f(a=1, b)`, `f(**k, b)`, `f(**k, *a)` |
| `comprehension_tuple` | `[x for x in a, b]` (Python 2), and `f(x for x in y, 1)`, which the grammar reads so: an unparenthesised tuple after `in` |
| `unparenthesized_walrus` | `x := 1` as a statement |
| `invalid_target` | a target outside Python's target grammar: `del f()`, `del 1`, `del x + 1`, `del (a, f())`, `del *a`; `with a as f():`; an annotated target that is not a single name, attribute or subscript (`(x, y): int = 1, 2`, `[a]: int`) |
| `constant_name` | an identifier whose NFKC form is `True`, `False` or `None` (`def 𝐓𝐫𝐮𝐞(): pass` in mathematical bold, `x.Ｎone`, `f(𝐅alse=1)`): the grammar reads a name, the reference Python normalises it to the constant and rejects it in every identifier position (`ValueError` from `ast.parse`) |
| `augmented_unpacking` | `a, b += 1` |
| `import_from_list` | `from a import b.c`, `from x import a,` (a trailing comma without parentheses) |
| `indentation` | the tokenizer's indentation check: at each logical line, the column with tabs to the next multiple of 8 and the column with tabs as one column order differently against the indentation stack (`TabError`, Python 2 files mixing tabs and spaces), or a dedent matches no outer level |
| `nesting_limit` | more than 200 nested brackets, 100 indentation levels (the tokenizer's limits) |
| `encoding_cookie` | a coding cookie Graphvein cannot follow: a BOM with a cookie that is not UTF-8, a codec the reference Python does not have or that does not read ASCII as ASCII (`foobar`, `unicode_escape`), or a codec other than UTF-8 over a file with non-ASCII bytes (`# coding: latin-1` over UTF-8 text: Python reads other characters; an over-refusal when they are only in strings or comments) |
| `unicode_version` | a character whose identifier properties or name differ between the reference Python's Unicode version (16.0) and Graphvein's tables (17.0) (`unicodeversion.go`): an identifier holding one (normalised or sorted differently; today the grammar reports an error first for each), or a `\N{...}` name only the newer version holds (`"\N{ARABIC LETTER NOON WITH RING ABOVE}"`, U+088F, new in 17.0) |

CPython rejects more forms the grammar reads without an error (deep
expression nesting past the parser's stack, and others). A file holding
only such a form is claimed as written; the oracle reports it
(`D-claimed-in-rejected-file`, a gate). Over-refusal: `print >> f` and
`print >>f, x` are valid Python 3 expressions the grammar reads as a
`print_statement`: the file is refused, which is
precision-safe. ADR 0069, Known gaps.

## Declarations

Spans: from the `def`, `async` or `class` keyword, or from the `@` of the
first decorator, to the end of the `ast` node: the end of the last
statement of the body, a `;` after it on the same line included, comments
after it excluded (stop PS-S2-1, closed). The content hash covers exactly
the span. Every row emits one `contains` edge from the lexical owner (the
enclosing def or class node, else the file node), `provenance: fact`,
confidence 1, evidence = the span. Compound statements (`if`, `try`,
`with`, `for`, `while`, `match`) open no scope.

Identity rules: `docs/lang/identity.md` "Python". A qualified name is the
lexical owner path joined by `.`; a repeated qualified name in one file
(any kind, overload signature included) gets `@2`, `@3` in source order.

| row | form | node kind, qualified name | refusal (to D) | status |
|---|---|---|---|---|
| PS-D1 | `def` / `async def` not directly in a class body (module level, nested in a def, in compound statements) | `function`, owner path + name | - | oracle |
| PS-D2 | `def` / `async def` directly in a class body (through compound statements) | `method`, `Class.name` (nested classes: `Outer.Inner.name`) | - | oracle |
| PS-D3 | `class` at any depth | `class`, owner path + name | - | oracle |
| PS-D4 | a def decorated with `overload` or `typing.overload` (written so) | as PS-D1/PS-D2 with `@overload` + the parameter text without trivia outside literals | - | oracle |
| PS-D5 | a repeated qualified name in one file (redefinition, property setter, conditional definitions) | the first keeps the name; later ones `name@2`, `name@3` | - | oracle |
| PS-D6 | decorators of PS-D1..PS-D3 | `attributes.decorators`: each decorator as written after `@` (a call: its callee) | - | oracle |
| PS-D7 | a module-level assignment with a value whose target names (plain, tuple or list unpacking, chained targets) include an UPPER_CASE name (a letter, every letter upper case, otherwise `_` and decimal digits, category Nd) | `constant`, the name; span from the target (the statement for the first target) to the end of the statement | an annotation without a value (`X: int`) to D, counted (`annotation_only`, O-S2-4 (b)); a starred name (`*E, F = ...`) to D, counted (`starred_target`) | oracle |

Not modelled (outside the scope; claim nothing, not counted): lower-case
module variables, class and instance attributes, augmented assignments,
`for`/`with`/`except`/`match` targets, walrus targets, lambdas, PEP 695
`type` statements, `global`/`nonlocal` names, `__all__`, docstrings,
comments.

## Imports

Each `import` / `from ... import` statement of a file PS-F2 does not
refuse is a claim site, at any nesting depth (inside functions, `try`, `if
TYPE_CHECKING`): the edge states that the statement, as written, names the
target, not that it runs. Each imported name is one claim unit (the
default branch counts per name). The evidence span of each edge is the
whole statement; several statements naming the same target merge into one
edge with several evidence entries.

**Finding a module** (`ModuleResolver.structural`), in CPython's order for
an absolute import:

1. Modules the interpreter provides: `__main__` (the running program) and
   `sitecustomize`, `usercustomize` (site imports them at start-up, before
   the script directory is on `sys.path`): D, counted
   (`interpreter_module`).
2. The meta path: a top-level name the reference Python serves before any
   `sys.path` entry (built in, frozen, or loaded at start-up:
   `metaPathNames` in `internal/lang/python/metapath.go`, generated from
   CPython 3.14.10's `sys.builtin_module_names`, frozen modules and a
   `-I -S` start-up's `sys.modules` on Linux by
   `scripts/oracle/python_structure/stdlib_lists.py`; these lists, unlike
   `sys.stdlib_module_names`, differ between platforms, so
   `TestPythonMetaPathNames` compares them exactly only with 3.14.10 on
   Linux and as inclusions elsewhere): `python-stdlib`
   (PS-I5), even when the repository holds a file of the name (`sys.py`,
   `os.py`).
3. The importer's script directory (PS-M1 refusal, `script_directory_module`).
   For an importer in an implicit root the refusal applies after step 4:
   only when that directory alone answers differently from the search
   path.
4. importlib's `PathFinder` over the PS-M1 search path (`find`), one dotted
   component at a time in the parent's `__path__`: per component, the
   first path entry holding a regular package (`name/__init__.py`) or a
   source module (`name.py`) wins and namespace directories accumulate as
   portions; a source module parent ends the search ("is not a package").
   Every directory the scanner walked is a directory here, also one
   holding no Python file or no file at all (`scan.Result.Dirs`), as for
   Python. `.pyi` stubs are never targets.
5. The standard library after the roots: a top-level name found in the
   repository only as namespace portions that is a standard-library name
   (`sys.stdlib_module_names`, or a package CPython ships beside them such
   as `test`) claims nothing, counted (`namespace_hides_concrete`):
   `PathFinder` takes the concrete standard package from its later entry.
   A top-level name the repository supplies that is a platform name (a
   standard-library extension module of the reference build, another
   platform's module, a test extension of `lib-dynload`, or `importlib`,
   whose submodules are frozen: `platformModuleNames`) claims nothing,
   counted (`platform_module`): another build compiles it in and never
   loads the repository file.

**Files Graphvein does not read** (stop PS-S2-4, closed): `FileFinder`
tries extension modules (`.so`, `.pyd`), then `.py`, then `.pyc`, and an
`__init__` with any of these makes a directory a regular package. When a
path entry the search reaches holds such a candidate that the scanner did
not read as Python source (a skipped file: binary, not UTF-8, ignored, in a
default-skipped directory such as `build` or `venv`; a skipped directory;
a scanned file of another kind) and Python would try it before what
Graphvein found there (or Graphvein found nothing concrete there), the
import claims nothing: D, counted (`shadowed_module`). The scanner's
skipped entries (`scan.Result.Skipped`: also files it failed to read,
unsupported paths, non-regular files, and symlinks, a symlink to a
directory as a directory, which Python follows and Graphvein does not) and
the scanned non-Python files are the input; nothing else on disk is read.

**Relative imports**: `__package__` is the importer's directory chain under
the repository root (an `__init__.py` names its own directory); `level`
dots need at least `level` components, unless the repository root is
itself a regular package (`__init__.py` at the root).

**From-names** (`fromList`, shared with calls, routes and models): for
`from M import n`, a submodule `M.n` is the binding when `M` is a package
whose `__init__` does not bind `n`, holds no star import and defines no
module `__getattr__` (CPython's `_handle_fromlist`); a name `M` binds once,
unconditionally (not in a conditional branch, a loop, an `except` or
`except*` handler or a `try` body), by an import is that import's module
(a re-export, followed through further re-exports); any other binding is
a value (no edge). Bindings are the parser's: definitions, imports,
assignments and PEP 695 `type` statements. A name bound in an `__init__`
that also has the submodule `M.n` is decided only when the binding is a
single unconditional `from <M.n> import ...` (relative or absolute): that
import loads the submodule before binding, and a loaded module's
attribute is never set again, so the binding stands (then as a
re-export). Undecided: any other name bound next to a same-named
submodule, a name whose binding is conditional or
repeated, a name `M` does not bind when `M` defines a module `__getattr__`
(PEP 562: `hasattr` calls it before any submodule import), a submodule of
a package whose `__init__` has a star import, and any name of a package
whose `__init__` PS-F2 refuses are undecided: no edge, counted
(`undecided_name`, O-S2-5 (a)). A submodule the `__init__` binds only by
`from . import n` is counted (`submodule_after_self_binding`, inventory
class I-5, O-S2-8 (a)), not claimed.

**External ecosystems** (developer decision O-S2-7, 2026-11-17): an
absolute import that no root resolves becomes an edge to an `external`
node named after the top-level name. The ecosystem is `python-stdlib`
when the name is in CPython 3.14's `sys.stdlib_module_names` (a committed
copy, `internal/lang/python/stdlib.go`, checked against the running
interpreter by `TestPythonStdlibNames` when it is 3.14), a meta-path name,
or a package CPython ships beside them (`test`; `metapath.go`, checked by
`TestPythonMetaPathNames`), otherwise `pypi`: the module is not in the
repository (an installed distribution, or unavailable, also a generated
extension whose sources are in the repository); `pypi` names
no claim that the registry holds it. A repository module named like a
pure-Python standard-library module (`json.py`) resolves to the
repository file first (PS-I1); a meta-path name never does (step 2). A
name found only as a namespace directory that lacks the rest of the
dotted name (`import tools.y`, namespace `tools/` without `y`) is `pypi`
(CPython takes a concrete `tools` from a later entry or fails;
both fit the meaning).

| row | form | facts claimed | refusal (to D) | status |
|---|---|---|---|---|
| PS-I1 | `import a.b.c` [`as x`], the module found as a source module or regular package | `imports` edge file → the leaf module's file (the parent packages it also loads are counted, `dotted_parent_packages`, not claimed) | a file Graphvein does not read is tried first: D, counted (`shadowed_module`); the importer itself: as PS-I2 (`self_import`) | oracle |
| PS-I2 | `from M import n, ...`, `M` absolute or relative, found as in PS-I1 | an edge to `M`'s file (its `__init__.py` for a package); per name, an edge to the submodule or re-exported module the name binds (From-names) | an undecided name (`undecided_name`) or a self-bound submodule (`submodule_after_self_binding`): no edge for that name; a target equal to the importer (`from . import x` in `__init__.py`, O-S2-6 (a)): no self edge, counted (`self_import`); shadowing as PS-I1 | oracle |
| PS-I3 | a relative import beyond the top-level package (fewer than `level` components, root not a package) | none | D, counted (`relative_beyond_top`) | oracle |
| PS-I4 | an absolute import that no root resolves, top-level name not a standard-library name | `imports` edge to `external:.#pypi:<top-level name>` (`ecosystem: pypi`, `name`) | the top-level name is a source module or regular package under some root (`import app.missing`; or a concrete top-level package on the ancestor path that lacks the rest, which ends Python's search): no edge, an `unresolved_import` warning, counted (`unresolved_in_repository`); `__main__`, `sitecustomize`, `usercustomize`: D, counted (`interpreter_module`) | oracle |
| PS-I5 | as PS-I4, the top-level name a standard-library name (`sys.stdlib_module_names`, `test`); also a meta-path name wherever the repository holds it | `imports` edge to `external:.#python-stdlib:<top-level name>` (`ecosystem: python-stdlib`, `name`) | as PS-I4; a platform name the repository supplies: D, counted (`platform_module`) | oracle |
| PS-I6 | a module found only as a namespace package | an edge per from-name that binds a submodule file (PS-I2); none for the package itself | a namespace on the ancestor path ends the search even when another root holds a concrete package of the name (policy), or a namespace-only top-level name that is a standard-library name (CPython takes the standard package): counted (`namespace_hides_concrete`) | oracle |
| PS-I7 | a relative import whose module is not found (or that goes above the repository), or an absolute import several other roots resolve | none; `unresolved_import` / `ambiguous_import` warning | - | oracle |

`from __future__ import ...` statements claim no edge (O-S2-8 (a)): D,
counted (`future_import`).

## Default branch D

Claims nothing. Counted per file when the form is in the scope, in one
`info` diagnostic `python_structure_unread` whose message ends with the
count per form, sorted: `(form=n, form=n)`. Forms, each with the one row
that sends it to D (`unreadRows`):

- `syntax_error` (PS-F2): declarations and imports of a file PS-F2
  refuses (it counts no other form).
- `annotation_only` (PS-D7): `X: int` without a value.
- `starred_target` (PS-D7): a starred UPPER_CASE name.
- `future_import` (PS-I2): names of `from __future__` statements.
- `shadowed_module` (PS-I1): an unread file comes first (also for a
  from-name's submodule).
- `self_import` (PS-I2): a module or name target equal to the importer.
- `undecided_name` (PS-I2): a from-name whose binding CPython decides at
  run time.
- `submodule_after_self_binding` (PS-I2): a submodule the `__init__` binds
  only by `from . import n`.
- `relative_beyond_top` (PS-I3): a relative import beyond the top-level
  package.
- `unresolved_in_repository` (PS-I4): a top-level name that resolves in
  the repository while the rest does not (also for standard-library names).
- `dotted_parent_packages` (PS-I1): dotted imports that resolve in the
  repository (their parent packages also load; only the leaf is claimed).
- `namespace_hides_concrete` (PS-I6): a namespace on the ancestor path
  while a sibling root or the standard library holds a concrete package.
- `script_directory_module` (PS-M1): the importer's script directory holds
  the top-level name (in an implicit root: and answers differently from the
  search path).
- `interpreter_module` (PS-I4): `__main__`, `sitecustomize`,
  `usercustomize`.
- `platform_module` (PS-I5): a platform name the repository supplies.

The counts of the extractor's forms are stored in
`facts.PythonFacts.Unread` (cached with the file's facts); the linker adds
its own and emits the diagnostic.

## Precision stops of the investigation (all closed in the implementation)

| stop | row | observed in the investigation (main at `5b48ad4e`) | The implementation change | oracle after |
|---|---|---|---|---|
| PS-S2-1 | PS-D1..PS-D3 | span and content hash include trailing comments: fastapi 6, langflow 869, probe 4 | spans end at the `ast` end | 0 |
| PS-S2-2 | PS-F2 | declarations and edges in files `ast` rejects: fastapi 1 file (PEP 758, accepted by the reference 3.14), self 1, probe 3 | rejected files claim nothing beyond the file node | 0 |
| PS-S2-3 | PS-I3 | edges for relative imports beyond the top-level package: probe 3 | refused | 0 |
| PS-S2-4 | PS-I1, PS-I2 | edge to a `.py` shadowed by an extension module: probe 2 | refused when an unread candidate comes first | 0 |
| PS-S2-5 | PS-I4 | `external:.#pypi:<name>` for an in-repository package: langflow 34, probe 3 | `unresolved_import` warning, no edge | 0 |
