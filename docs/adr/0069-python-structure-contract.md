# ADR 0069 - Python structural claim contract: files, declarations, imports

**Status:** Accepted (delegated review, 2026-11-18)

## Context

T020 (ADR 0066) labels a language's structural scope `precise` only through
a rule-table row whose evidence includes a claim contract with a 1:1 test,
goldens, corpus checks, an oracle where cheap and an independent review.
ADR 0068 did this for Go (row PL-L1). The T020 investigation
(the T020 task record (internal)) inventoried every Python file,
declaration and import form `internal/lang/python` emits at `extractors-39`
and compared it with an oracle built on CPython's own `ast`, `tokenize` and
importlib rules over the raw bytes (`scripts/oracle/python_structure/oracle.py`).

Kinds, qualified names, decorators and owners agreed everywhere. The investigation
found five precision stops: declaration spans and hashes that include the
comments tree-sitter keeps after a body (PS-S2-1: fastapi 6, langflow 869);
declarations and edges recovered from files CPython rejects (PS-S2-2);
edges for relative imports beyond the top-level package (PS-S2-3) and to a
`.py` file an extension module shadows (PS-S2-4), both on the probe only;
and `external:.#pypi:<name>` nodes for names that are in-repository
packages (PS-S2-5: langflow 34). It also left open points O-S2-1..O-S2-9,
decided with the developer before and during the implementation.

## Options considered

1. **Run CPython at index time** (parse with `ast`, resolve with importlib).
   Exact, but Graphvein is an offline Go binary with no Python dependency;
   it would tie results to whatever interpreter the user has. Rejected.
2. **Keep tree-sitter, make the claims follow CPython where tree-sitter
   reads the same text, refuse the rest, and check everything with an
   oracle that reads the raw bytes with CPython itself.** Chosen.
3. **Narrow the scope** (no import edges). Loses the most useful fact.

For imports: (a) change the shared `ModuleResolver.Resolve` (calls, routes,
SQLModel and table access read it: other rows' outputs would move), or (b)
a separate structural resolution for import edges only. (b) chosen.

For files Python would load but Graphvein does not read (PS-S2-4): (a) let
the linker read the disk (breaks "the scan is the input" and the cache
model); (b) have the scanner report the entries it skipped and refuse an
import when one of them comes first in `FileFinder`'s order. (b) chosen.

## Decision

Option 2 with import option (b) and shadowing option (b). The contract is
`docs/lang/python-structure.md`: rows PS-M1, PS-F1..PS-F3, PS-D1..PS-D7,
PS-I1..PS-I7 and the default branch D, mirrored by `structureRows`
(`internal/lang/python/structure.go`), kept 1:1 by
`TestPythonStructureContract` (rows, rejection forms, default-branch forms,
the reference version, and a fixture case reaching each).

- **Reference Python** (O-S2-1, developer decision 2026-11-17): CPython
  3.14, one constant (`referencePython`). The oracle ran CPython 3.14.10
  built from source tag v3.14.10 (commit b703b2c5, PSF licence) in the
  session container.
- **Spans** (PS-S2-1, O-S2-2 (a)): a def or class span ends where `ast`
  ends it: the last statement of the body, a `;` after it on the same line
  included, trailing comments excluded (`declEnd`); the content hash
  follows the span. The route and scope owner lookup (`ownerFor`) uses the
  same end.
- **Files the reference Python rejects** (PS-S2-2, row PS-F2): a tree with
  a tree-sitter error (an `ERROR` node, or the indexer's
  `parse.Result.SyntaxError`, which also sees missing tokens), or a tree
  holding one of twelve forms the grammar reads cleanly but CPython 3.14
  rejects (`rejection`: Python 2 `print`/`exec` statements, `<>`,
  backticks, invalid string prefixes, legacy integers, `except A, B as e`,
  parameter and argument order, a bare walrus statement, `del f()`,
  augmented unpacking). Such a file claims its file node only: its
  declarations and imports go to D (`syntax_error`); its calls, routes,
  model syntax and bindings go with them; a `python_syntax_rejected`
  warning names the form. PEP 758 `except A, B:` and PEP 750 t-strings are
  accepted: the grammar reads both correctly (checked on the tree), and
  3.14 accepts them; below 3.14 the two forms would reject.
- **Files only `compile()` rejects** (O-S2-3 (a), row PS-F3): claimed as
  written.
- **Constants** (O-S2-4 (b)): `X: int` and starred names go to D, counted.
- **Imports** (`linkImports`, `ModuleResolver.structural` and `find`): a
  PathFinder over the documented root policy (ancestor roots as one path,
  sibling roots alone, implicit roots as one path; O-S2-9 (a): the policy
  is stated in the contract, not verified), where a concrete top-level
  package on the ancestor path ends the search as in Python. Refused, each
  counted in D: relative imports beyond the top-level package (PS-S2-3);
  an import an unread file would satisfy first (PS-S2-4: the scanner's new
  `scan.Result.Skipped` plus scanned non-Python files and dropped files;
  extension `__init__`, extension module before source, any candidate where
  Graphvein found nothing concrete, a skipped directory); a top-level name
  that resolves in the repository but whose rest does not
  (`unresolved_in_repository`, an `unresolved_import` warning, PS-S2-5);
  self edges (O-S2-6 (a)); undecided from-names (O-S2-5 (a): a binding
  next to a same-named submodule, a conditional, repeated or `try`-body
  binding (`PythonBinding.Guarded`), a star import in the package's
  `__init__`, a refused `__init__`). Counted, not claimed (O-S2-8 (a)):
  `from __future__`, a submodule bound only by `from . import n`
  (inventory I-5), parent packages of dotted imports, and a namespace
  package on the ancestor path hiding a concrete one on a sibling root.
  Calls, routes and models keep `Resolve` (superseded by the review fixes
  below: `Resolve` now wraps the structural resolution).
- **External ecosystems** (O-S2-7, developer decision 2026-11-17): an
  unresolved absolute import whose top-level name is in CPython 3.14's
  `sys.stdlib_module_names` is `external:.#python-stdlib:<name>` (row
  PS-I5); every other is `external:.#pypi:<name>` (PS-I4), documented as
  "not in the repository", no registry claim. The list is a committed copy
  (`internal/lang/python/stdlib.go`, generated once from the 3.14.10
  interpreter; the command is in the file) that `TestPythonStdlibNames`
  checks against the running interpreter when its version is 3.14. The
  ecosystem is chosen in one function (`externalEcosystem`).
- **Default branch**: one `info` diagnostic `python_structure_unread` per
  file with the count per form; the extractor's forms are cached in
  `facts.PythonFacts.Unread`.
- **Oracle**: `TestPythonStructureOracle` indexes through `index.Emit` and
  runs `oracle.py` with the interpreter `GRAPHVEIN_PYTHON_ORACLE` (else
  `python3`), skipping with a reason when it is older than 3.14. The
  oracle reads every file's raw bytes (encoding from `tokenize`, CPython's
  line ends, byte columns through the decoded line), parses with `ast`,
  lists the whole checkout (only `.git` skipped, not the scanner's
  inventory) and re-derives imports from `FileFinder`/`PathFinder` with
  the loader suffixes of `importlib.machinery`. It applies the contract's
  refusals on top of Python's answer and marks each record `gate` (a
  claimed fact that disagrees) or not (an omission the contract explains,
  or a note). Fixture by default; opt-in on fastapi and langflow
  (`GRAPHVEIN_PYTHON_STRUCTURE_ORACLE_CORPUS`) and the self-index
  (`GRAPHVEIN_PYTHON_STRUCTURE_ORACLE_SELF=1`).
- Extractor version `extractors-40` (cached payload fields `Rejected`,
  `Unread`, `Guarded`; spans).

No rule-table row is added and no label changes: PL-L2 follows the
independent review (added, see "Review notes").

## Results

`TestPythonStructureOracle` with CPython 3.14.10, 0 gate records on each:

| corpus | Python files (rejected) | declarations: oracle = agree | import statements | file edges = agree | external edges = agree |
|---|---|---|---|---|---|
| fixture (with the written cases) | 55 (13) | 80 | 65 | 45 | 10 |
| fastapi `1762adac` | 43 (0) | 174 | 192 | 90 | 117 |
| langflow `504c02fc` | 4,143 (0) | 50,033 | 31,557 | 16,570 | 14,659 |
| self-index (this branch's worktree) | 186 (14) | 530 | 387 | 88 | 298 |

Compared: per file, presence in the inventory, size and hash against the
raw bytes; per declaration (`ast` def, class and module constant), kind,
qualified name, name, all four span numbers, content hash, decorators and
the one `contains` edge (owner and evidence); per import statement
(matched by evidence span = the `ast` statement span), the set of file
targets and of external targets (ecosystem and name); every Graphvein
import edge's evidence matches a statement. A file Python rejects must
claim nothing. Not compared: identity strings (compared through kind,
file and qualified name), provenance and confidence, diagnostic texts,
the default-branch counts, calls, routes and models.

Omissions the contract explains (no claim, not a disagreement), langflow:
`from __future__` 1,243, undecided names 943, in-repository top-level
names 110, I-5 submodules 40, self edges 17. A first run found 12
langflow files refused although Python accepts them (`*args: T` /
`**kwargs: T` read as positional parameters by the `parameter_order`
check); fixed before these numbers, and the oracle now reports any such
refusal (`O-refused-but-ast-accepts`).

Snapshot diff, main (`extractors-39`) against this branch, fastapi,
langflow and a `git archive` of main for the self-index: the only
differences are the intended classes (counts in the unit report): spans
and hashes trimmed (fastapi 6, langflow 869, self 1), standard-library
externals moved to `python-stdlib` (edges 41 / 8,013 / 138), in-repository
externals replaced by `unresolved_import` warnings (langflow 43 edges, 88
warnings), self edges dropped (langflow 15), one undecided from-name edge
dropped (langflow, inventory I-4), the rejected `broken.py` node (self),
and `python_structure_unread` diagnostics.

## Review fixes (S2R)

The independent review (the T020 task record (internal)) found one class A
finding (A1), nine class B and eight class C. Each is fixed as a class rule
in the code, the contract table and `TestPythonStructureContract` together,
and the oracle now checks each class from CPython's own behaviour instead of
sharing the blind spot. Extractor version stays `extractors-40` (S2B is not
on main; S2B and S2F ship as one change).

- **A1 / OP-1 (decision, option (a))**: an absolute import in a
  file whose directory is neither a regular package nor a PS-M1 root, when
  that directory holds the top-level name, claims nothing: D, counted
  (`script_directory_module`, row PS-M1). `python dir/run.py` and pytest's
  default `prepend` mode put that directory first on `sys.path`. Oracle:
  CPython's `PathFinder` over the importer's directory (`sys.path[0]`).
  Implicit roots count as roots (their order against explicit ancestors is
  the PS-M1 policy).
- **B1 (meta path)**: built-in, frozen and start-up modules (`metaPathNames`
  in `internal/lang/python/metapath.go`, generated from CPython 3.14.10 by
  `scripts/oracle/python_structure/stdlib_lists.py`) are `python-stdlib`
  even when the repository holds a file of the name. Standard-library names
  that are extension modules of the reference build or modules of another
  platform, CPython's test extensions and `importlib` (frozen submodules)
  are platform names: when the repository would supply one, the import
  claims nothing (`platform_module`, PS-I5), since another build compiles it
  in. `TestPythonMetaPathNames` checks the lists exactly against 3.14.10 and
  as inclusions against any other 3.14 build (checked against
  python-build-standalone 3.14.5 too). Oracle: `BuiltinImporter`,
  `FrozenImporter` and a `-I -S` start-up of the running interpreter; a
  built-in that the repository would supply is accepted either way.
- **B2 (rejection forms)**: nine new PS-F2 forms, each a class: Python 2
  forms (`raise_comma`, `comprehension_tuple`, and tabs mixed with spaces
  through the tokenizer's indentation check `indentation`), literal rules
  (`string_literal`, `number_literal`), the target grammar
  (`invalid_target`, which replaces `delete_call` and adds `with ... as`
  and annotated targets), `except_star`, `import_from_list`, extended
  `parameter_order`, the tokenizer's limits (`nesting_limit`) and coding
  cookies (`encoding_cookie`, codec names from the reference Python's
  registry). All 29 probe forms of the review and one more
  (`'a' b'c'`) are cases of `TestPythonRejectionForms`; a probe of the 30
  files gives 32 oracle gates with the S2B binary and 0 now.
- **B3**: a symlink to a directory is a skipped directory
  (`scan.Result.Skipped` with `/`), so imports through it refuse
  (`shadowed_module`). Oracle: `os.walk(followlinks=True)` inside the
  checkout, as FileFinder follows links.
- **B4**: a top-level name found only as namespace portions that is a
  standard-library name claims nothing (`namespace_hides_concrete`).
  Oracle: CPython's `PathFinder` over the standard library's own entries.
- **B5**: every directory the scanner walked is a namespace portion
  (`scan.Result.Dirs`, `WithDirectories`; data-only and empty directories).
- **B6**: `__main__`, `sitecustomize`, `usercustomize` claim nothing
  (`interpreter_module`); CPython's `test` package is `python-stdlib`.
- **B7**: module names are built from the NAME tokens of `dotted_name`
  (no blanks or continuations).
- **B8**: PEP 695 `type` statements bind; a module `__getattr__` makes every
  from-name it does not bind undecided (`fromList`); `except_group_clause`
  is conditional (this grammar version reads `except*` as `except_clause`,
  already conditional). `sys.modules[__name__] = ...` is not modelled
  (follow-up).
- **B9**: one resolution for every consumer: `Resolve` (calls, FastAPI
  routes, SQLModel models, table access) wraps `structural`, and member
  access uses `fromList`, the same from-list decision as the import edges.
  The old per-root `probe` is gone. A refused import resolves to nothing
  for every consumer; a binding next to a same-named submodule is
  undecided for calls too. The sqlmodel, table-access and route goldens do
  not change.
- **C1**: `unreadRows` maps each default-branch form to one row; the
  document lists each form with its row and the test compares both lists
  and the rejection table exactly, and that every counting site reports its
  row. What it does not check is stated in the contract.
- **C2**: identifiers are NFKC-normalised (`golang.org/x/text/unicode/norm`;
  the module was already an indirect dependency at the same version, now
  direct; no new module, no ADR needed beyond this note). Characters newer
  than its Unicode 15 tables normalise to themselves (Known gaps).
- **C3, C6, C7, C8**: documented in the contract (C6 as the visible list of
  shared assumptions).
- **C4**: the scanner also lists files it fails to read, unsupported paths
  and non-regular files in `Skipped`.
- **C5**: the oracle reads `file_too_large` as "nothing claimed"
  (`TestPythonStructureOracle/fixture-oversize`, a 64-byte parse limit).
- **CI**: the postgres job sets up CPython 3.14 (`actions/setup-python@v6`)
  and passes it as `GRAPHVEIN_PYTHON_ORACLE`, so the fixture oracle and the
  list tests run in CI.

- **From-name refinement** (found by the B9 snapshot diff: langflow's
  `langflow.api` binds `log_router` by `from langflow.api.log_router import
  log_router` next to the submodule, and the route mount was lost): a
  single unconditional from-import out of the same-named submodule decides
  the name (the binding), since that import loads the submodule before the
  binding and a loaded module's attribute is never set again. Oracle: the
  same rule from its own `ast` census and PathFinder.

Oracle after the fixes (CPython 3.14.10, 0 gate records on each): fixture
81 Python files / 84 import statements (and a 64-byte parse-limit run),
fastapi 43 / 192, langflow 4,143 / 31,557 (omissions it explains, new:
`script_directory_module` 3, `undecided_name` 943 to 1,317), self-index
213 / 420. No file Python accepts is refused on fastapi or langflow
(`O-refused-but-ast-accepts` 0). With the S2B binary the new oracle reports
32 gates on a probe of the review's 30 rejection forms, 3 on a symlink and
data-directory probe, and 32 on this fixture; with this branch 0.

Snapshot diff against S2B (`21fd647`): fastapi none; langflow 3 `pypi`
edges removed (A1), 22 file edges removed (undecided from-names, mostly
packages with a module `__getattr__`), counts `script_directory_module`
+12 and `undecided_name` +387, calls -116/+37 (B9: calls through refused
or undecided imports go; calls through a `from . import loading` submodule
come); routes, models and table access unchanged.

## Review notes (S2R, pending acceptance)

Independent review (the T020 task record (internal), at `21fd647`):
The PL-L2 flip was blocked by one class A finding (A1), nine class B and
eight class C findings. Step S2F (section "Review fixes" above): Every
class A and B finding is fixed as a class rule in the code, the contract
table and `TestPythonStructureContract` together, and the oracle checks
each class from CPython's own behaviour. No class A finding is open. S2F
open point 2 (a script in a directory that is an implicit root) was
decided and fixed: the import is refused
(`script_directory_module`) only when that directory alone answers
differently from the PS-M1 search path (both candidates exist and differ);
the oracle applies the same rule with CPython's `PathFinder`, and with the
rule removed from the Go code it reports the fixture case as a gate
(`I-wrong-edge`).

Corpus and oracle (`extractors-40`, CPython 3.14.10 built from
tag v3.14.10): TestPythonStructureOracle (CPython 3.14.10): 0 gate records
on the fixture, fastapi, langflow and the self-index (fixture
84 Python files, 86 import statements, and a 64-byte parse-limit run; fastapi 43 / 192; langflow 4,143 / 31,557; self-index 216 / 422, and 217 / 426 after the merge of main). It runs in CI's
postgres job, which sets up CPython 3.14 with `actions/setup-python` and
passes it as `GRAPHVEIN_PYTHON_ORACLE` (the fixture; the corpora and the
self-index are opt-in).

Label flip (row PL-L2): snapshot diff of the S2F binary
(`c7efe76`) against S2L, fastapi `1762adac`, langflow `504c02fc` and a
`git archive` of the S2L branch for the self-index: the only differences
are `precision` values of Python file records and file nodes
(`best_effort` to `precise`: fastapi 43 of 43 Python files, langflow
4,143 of 4,143, self-index 193 of 216) and, on the self-index, the new
fixture case of the implicit-root rule (one `imports` edge removed, one
`python_structure_unread` diagnostic added). The 23 self-index files that
stay `best_effort` are all rejected under PS-F2 (21 listed rejection forms
and 2 tree-sitter errors: `syntax_bad.py` and `testdata/repos/python`'s
`broken.py`). Calls stay row D (`code_calls`).

## Consequences

- Declaration spans and hashes of defs and classes followed by comments
  change (rationale and caches attached to them see a new hash once).
- Files with a listed rejection form lose every structural, call, route
  and model claim; a `python_syntax_rejected` warning says why.
- Standard-library externals change identity (`pypi:os` becomes
  `python-stdlib:os`); stored snapshots differ in those nodes and edges.
- Every Python file with a counted form gains a `python_structure_unread`
  diagnostic (dotted imports into the repository are the most common).
- The scanner reports skipped entries; nothing is stored.

## Known gaps

- **Shared assumptions** (the oracle cannot verify them): the root set and
  order of PS-M1 (a different launch directory gives a different
  `sys.path`); that a namespace package on the ancestor path ends the
  search even when a sibling root holds a concrete package (PS-I6); that a
  relative import's `__path__` is the importer's directory; that `.pyi`
  stubs are never targets; the scanner's file list as the root set's
  input; the contract's own refusal rules (star rule, `try`-body
  bindings), which the oracle applies as policy.
- **Rejection forms are a finite list.** CPython 3.14 rejects more forms
  that tree-sitter reads without an error (a generator argument that is
  not the only argument, which the grammar even misreads; and others). A
  file holding only such a form is claimed as written; the oracle reports
  it as `D-claimed-in-rejected-file`. None occur on the fixture and the
  corpora. Conversely, newer syntax the grammar cannot read (a future
  version's) is refused through `tree_sitter_error`, which is
  precision-safe.
- **3.14 syntax**: PEP 758 and t-strings are read correctly by the grammar
  (checked); `except A, B as e` stays refused. A move of the reference to
  another version changes `referencePython`, the version-gated forms and
  the stdlib list.
- **Extension suffixes** are recognised by shape (`.so`, `.pyd` after the
  module name), not the running platform's `EXTENSION_SUFFIXES`, so a
  Windows `.pyd` refuses on every platform (precision-safe).
- **Calls, routes and models** resolve modules through the structural
  resolution since the review fixes (B9); their own name-binding rules
  (values, classes, call targets) are unchanged and best effort.
- **Unicode versions** (superseded by Revision 1 below): NFKC
  (`golang.org/x/text`, Unicode 15) and the `\N{...}` name check (its
  rune-name tables) are older than CPython 3.14's Unicode 16: an
  identifier holding a character added later is not normalised as Python
  does, and a newer name is refused (over-refusal).
- **Script directories under an implicit root**: a file in a directory
  that is an implicit root searches the explicit ancestors first (policy),
  while `python dir/run.py` would search the directory first. Since unit
  S2L the import is refused (`script_directory_module`) when the two
  orders answer differently; when they agree the edge stands. An
  own-directory namespace portion next to a concrete package elsewhere is
  refused although Python would take the concrete one (over-refusal).
- **Meta path per build**: `metaPathNames` and `platformModuleNames` are
  the reference Linux build's; a build that compiles in a pure-Python
  name does not exist in CPython, but `posix`/`pwd` are not built in on
  Windows (there a repository `posix.py` would load).
- **`sys.modules[__name__] = ...`** replacing a module is not modelled
  (from-names of such a module are decided as for a plain module).
- `dotted_parent_packages` is counted on every dotted in-repository
  import, which makes the diagnostic frequent.
- O-S2-7 cosmetic: `pypi` is kept as the name for "not in the repository,
  not standard library", which includes unavailable modules.
- Workspace members installed as packages (langflow `lfx`): addressed by
  row PS-M2, ADR 0072 (Proposed).

## Review notes (acceptance, 2026-11-18)

Accepted after delegated review (2026-11-17). Steps: the investigation (the T020 task record (internal)), S2B implementation, one independent review (the T020 task record (internal): one class A, nine class B, eight class C), S2F fixes (every A and B finding fixed as a class rule and mutation-checked; C1, C2, C4, C5 fixed; C3, C6-C8 documented), S2L (implicit-root script-directory refinement, row PL-L2, label flip).

- Results: the CPython 3.14.10 oracle (`TestPythonStructureOracle`) reports 0 disagreements on the fixture, fastapi (43 files / 192 import statements), langflow (4,143 / 31,557) and the self-index; an independent PathFinder check by the reviewer agreed on 14,754 + 6,776 langflow imports. The oracle and the stdlib/meta-path list tests run in CI's postgres job (setup-python 3.14).
- Coverage per corpus after the flip: fastapi 43/43 Python files precise, langflow 4,143/4,143, self-index 193/216 (23 deliberately rejected fixtures stay best effort, `structure_refused`).
- Wrong facts removed from main: trailing-comment spans and hashes (fastapi 6, langflow 869), in-repository packages shown as `pypi` externals (langflow 43 edges), self edges, script-directory imports shown as externals, declarations recovered from files CPython rejects; standard-library modules moved from `pypi` to `python-stdlib` (developer decision O-S2-7).
- Assumptions shown: the documented root set and order (python-resolution.md), relative-import `__path__` as the importer's directory, the reference interpreter 3.14 (developer decision O-S2-1).
- Known gaps and after-v1 items: as listed above; a workspace member installed as a package (langflow `lfx`, 23 sites) is not yet a root (follow-up unit); calls stay row D.
- Gates: local full check 0 FAIL, 170 `/postgres` PASS; CI run 44203680984 on 2a9b131 (`[ci windows]`) green, postgres raw log 0 FAIL, 171 `/postgres` PASS, skips only the corpus- and environment-gated opt-in tests.

## Revision 1 (2026-11-24): Unicode 17 tables

**Status:** Accepted (delegated review, 2026-11-25)

### Context

`golang.org/x/text` v0.29.0 has two known vulnerabilities (GO-2026-5970,
GO-2026-6629; ADR 0086), fixed in v0.41.0 (developer decision 2026-11-24:
bump now). Under the Go 1.27 toolchain, v0.41.0 selects its Unicode 17.0.0
tables, so the extractor's NFKC normalisation of identifiers (`identifier`)
and its `\N{...}` name check (`unicodeName`, rune names) move from Unicode
15.0.0 to 17.0.0; Go's own `unicode` package (the constant rule's
`IsLetter`/`IsUpper`/`IsDigit`, row PS-D7) is 17.0.0 since the go1.27.4
toolchain. The reference Python, CPython 3.14, has Unicode 16.0.0 (every
3.14 build: checked on 3.14.5 and 3.14.6). The contract's rows that depend
on Unicode data: PS-D1..PS-D7 and PS-I1..PS-I7 (names and module names are
NFKC-normalised identifiers), PS-D7 (letter and upper-case classes), and
PS-F2 (which files are rejected: identifier validity and `\N{...}` names).

### Options considered

1. **An oracle CPython whose Unicode version matches the tables.** No
   CPython 3.14 has Unicode 17; the first is 3.15 (3.15.0b4 is the newest
   build uv offers in the container, `unicodedata` 17.0.0). Moving the
   reference to 3.15 changes the developer decision O-S2-1 (rejection
   forms, standard-library and meta-path lists) and a beta is no
   reference. Rejected for this revision.
2. **Keep v0.29.0's Unicode 15 tables** (vendor or pin them). Leaves the
   vulnerable module or forks it. Rejected.
3. **Refuse what the versions disagree on.** List, from both versions'
   own data, every code point whose identifier-relevant properties or name
   differ between the reference's Unicode 16.0.0 and the tables' 17.0.0,
   and refuse a file whose claims would depend on one. Chosen.

### Decision

- `x/text` v0.41.0 (no other module changes: `go mod tidy` adds nothing;
  BSD-3-Clause as before, notices regenerated).
- `scripts/oracle/python_structure/unicode_tables.py` dumps, per code
  point, `isidentifier` as start and as continue, the general category,
  the combining class, the NFKC form and the name from the running
  interpreter, and writes `internal/lang/python/unicodeversion.go` from a
  CPython 3.14.6 dump (Unicode 16.0.0) and a CPython 3.15.0b4 dump
  (Unicode 17.0.0): `identifierChanged`, 4,699 code points (start,
  continue, the letter / upper-case / digit class, combining class or NFKC
  differ: all are characters new in 17.0 that can be part of an
  identifier there), and `nameChanged`, 10,948 (6,145 Tangut ideographs
  that 3.15's `unicodedata.name()` names algorithmically and 3.14's does
  not, and the new characters). 3.14's name lookup, and so `\N{...}`,
  already accepts the algorithmic Tangut names (`TANGUT IDEOGRAPH-17000`);
  Graphvein refuses them (`string_literal`: the tables hold no plain name
  for them), an over-refusal. Two existing characters change: U+0295 (Ll to Lo, the same
  letter class, so not listed for identifiers) and the Tangut names.
- New PS-F2 rejection form `unicode_version`: an identifier holding a
  character of `identifierChanged`, or a `\N{...}` name of a character of
  `nameChanged` that the tables hold (`unicodeName` no longer confirms
  those names). The whole file is refused, as for every PS-F2 form: the
  reference rejects such a file or reads it differently. Fixture
  `rejected/unicode_version.py` (`\N{ARABIC LETTER NOON WITH RING ABOVE}`,
  U+088F, new in 17.0).
- Identifier validity is decided by the grammar, not by `x/text`. Measured
  once (container probe, not committed): on every non-ASCII code point
  (1,111,936, surrogates excluded), the grammar reads `<c>=1` and `a<c>=1`
  as an assignment to the identifier exactly when CPython 3.14.6's
  `str.isidentifier` accepts `<c>` and `a<c>` (0 differences; 141,194
  start and 144,459 continue characters). So today every character of
  `identifierChanged` already gives a tree-sitter error first
  (`tree_sitter_error`); `TestPythonUnicodeVersion` checks that for each
  listed code point, so a grammar update that reads one turns into the
  `unicode_version` refusal, not a claim.
- `TestPythonUnicodeVersion`: the lists' version equals `norm.Version`,
  `runenames.UnicodeVersion` and `unicode.Version`; the ranges are sorted,
  disjoint and of the stated size; the identifier branch refuses each
  range's ends; and with an interpreter (`GRAPHVEIN_PYTHON_ORACLE`, CI's
  postgres job sets up 3.14) it compares Go's tables code point by code
  point with that interpreter's `unicodedata`: NFKC form, letter /
  upper-case / digit class and every name `runenames` holds, outside the
  lists for a Unicode 16.0.0 interpreter, everywhere for a 17.0.0 one.
  Results: CPython 3.14.6, 1,112,064 code points, 0 differences outside
  the lists; CPython 3.15.0b4, 1,112,064 code points, 0 differences;
  with `identifierChanged` emptied (mutation) the 3.14.6 comparison fails.
- Extractor version `extractors-60` (cached Python facts hold normalised
  names and the rejection).

### Review fixes

The review of this revision found three pre-existing gaps that read
Unicode data the lists do not cover, and one oracle difference; each is
fixed as a class rule (CPython 3.14.6 as the oracle):

- `\N{...}` names are matched as CPython's `_getcode` matches them:
  ASCII letters in any case, every other byte as written. Go's
  `strings.ToUpper` (full Unicode case mapping) accepted
  `\N{LATIN ſMALL LETTER A}`, `\N{latın small letter a}` and a name with
  U+212A KELVIN SIGN for `K`, which CPython rejects; a name with a
  non-ASCII byte is now `string_literal` (TestPythonRejectionForms).
- New PS-F2 form `constant_name`: an identifier whose NFKC form is `True`,
  `False` or `None` (`def 𝐓𝐫𝐮𝐞(): pass`); CPython rejects it in every
  identifier position (`ValueError` from `ast.parse`; probed as a
  definition, attribute, keyword argument, import, base class, `global`,
  pattern, f-string interpolation, parameter, full-width and modifier
  letter spellings). Fixture `rejected/constant_name.py`. A binding of
  `__debug__` (any spelling) is rejected by `compile()` only (`ast.parse`
  accepts it), so it stays a PS-F3 form, claimed as written like
  `return` at module level (open point O-S2-3 (a)); the row now names it.
- PS-M2: a member whose `project.name` fails hatchling 1.31.0's
  `is_valid_project_name`, read over ASCII, installs nothing
  (`python_workspace_member_unread`); hatchling refuses to build it, and
  `fileNameComponent` read Go's letter / digit classes, which differ from
  Python's `\w` (`½`, `Ⅷ`, `፩`). It is now ASCII-only. hatchling's
  case-insensitive pattern also accepts `ſ`, `ı`, `İ`, U+212A and a
  trailing newline; those are refused too (over-refusal).
- The oracle's PS-D7 rule reads digits as category Nd, as Go's
  `unicode.IsDigit` does (`str.isdigit` also took No digits such as
  U+1369), and refuses the same project names as PS-M2.

So the Python extractor's Unicode reads are: NFKC of identifiers, the
character-name table, and the letter / upper-case / Nd classes of PS-D7,
all covered by the lists and `TestPythonUnicodeVersion`.

### Consequences

- Identifiers holding characters new in Unicode 16 (outside the lists) are
  now normalised as the reference does (the old Unicode 15 tables left
  them unchanged): ADR 0069's Unicode known gap closes.
- A `\N{...}` name new in Unicode 17 is reported as `unicode_version`
  instead of `string_literal` (refused either way).
- ASCII code is unaffected (corpus results below).

### Results

`TestPythonStructureOracle` with CPython 3.14.6 (uv's python-build-standalone
build, `unicodedata` 16.0.0), 0 gate records on each: fixture 96 Python
files / 94 import statements (and the 64-byte parse-limit run), fastapi
`1762adac` 43 / 192, langflow `504c02fc` 4,143 / 31,557 (the omissions the
contract explains are unchanged), self-index 336 / 681. Compared as in
"Results" above (declarations, spans, hashes, decorators, owners, import
targets; a rejected file claims nothing). `TestPythonUnicodeVersion`,
`TestPythonStdlibNames` and `TestPythonMetaPathNames` pass with the same
interpreter.

Snapshot diff, `x/text` v0.29.0 binary (`extractors-59`) against this
revision, every record of the emitted stream with the version string
masked, on the corpora with Python files: fastapi (43 Python files),
langflow (4,143), kftray (5), crates.io (3), dozzle (1), miniflux (1) and
RealWorld (1): no record added or removed. The fixture golden gains only
`rejected/unicode_version.py` (its file node, `python_syntax_rejected`
naming `unicode_version` and the `python_structure_unread` count); the
other goldens change only in the version string.

### Known gaps

- A move of the toolchain or `x/text` to another Unicode version needs a
  regenerated list (the test fails until then), and a move of the
  reference Python another.
- The list compares per-character data; whole-string NFKC also depends on
  composition pairs, which Unicode's stability policy adds only with new
  characters (in the list).
- Identifier validity rests on the grammar's tables and the one-off probe
  above; the probe uses `str.isidentifier` per character in two contexts,
  not the tokenizer on every identifier.
- The 3.15 side of the list comes from a beta build (3.15.0b4); its
  `unicodedata` is the final 17.0.0 data, and the Go tables agree with it
  on every code point.

Revision 1 review notes (2026-11-25): one independent review (no A; B1 `\N{}` case folding, B2 identifiers normalising to True/False/None, B3 non-ASCII hatch project names, C1/C2 wording and oracle; all fixed); oracle CPython 3.14.6 0 gate records on the fixture, fastapi and langflow; Unicode 17 lists generated from CPython 3.15.0b4 and checked against unicodedata2 17.0.1; no corpus change; gate run 45481900427 green. Known gap: the lists are regenerated when CPython 3.15 is final; `__debug__` bindings stay PS-F3 (decision O-S2-3 (a)).
