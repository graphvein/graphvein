# ADR 0072 - Python workspace members installed as packages on the search path

**Status:** Accepted (delegated review, 2026-11-18)

## Context

ADR 0015 fixed Graphvein's Python root policy (python-resolution.md):
the repository root, directories of scanned `pyproject.toml` / `setup.py`
/ `setup.cfg`, their `src` child, implicit roots; ancestor roots as one
search path, then sibling roots alone, then implicit roots. ADR 0069 made
the search a PathFinder (row PS-M1 of `docs/lang/python-structure.md`)
and listed in its known gaps that a workspace member installed as a
package is not a root: on langflow, `import lfx` from
`src/backend/base/langflow/...` finds the directory `src/lfx` (the
member's project directory, a namespace portion on the ancestor root
`src`) and stops, so about 23 sites get no edge, although the root
`pyproject.toml` declares `src/lfx` as a uv workspace member whose
regular package `src/lfx/src/lfx` is what an installed environment
imports. The same shape exists wherever a project's directory and its
package share a name (src layout under a sibling directory).

## Options considered

1. **Keep the gap** (counted as `namespace_hides_concrete`). Safe, loses
   the edges, and leaves a wrong fact: `from lfx.tests import x` claims
   `src/lfx/tests/x.py` through the namespace while the installed `lfx`
   has no `tests` subpackage.
2. **Treat every member's `src` as an ancestor root.** Changes answers for
   every name in those directories and ignores what the build exposes.
3. **Read the declared environment**: the workspace members (and editable
   path sources) a scanned `pyproject.toml` declares, the import packages
   each member's build backend exposes when that can be read exactly, and
   put exactly those packages on one search path entry after the ancestor
   roots, as site-packages comes after the script directory; let
   PathFinder's own rules decide (a namespace portion found earlier does
   not stop the search, a concrete package later wins). Chosen.
4. **Run the build backend** (hatchling's editable build) at index time.
   Exact, but executes third-party code and needs a Python environment.
   Rejected; used once as a check (below).

## Decision

Option 3, as row PS-M2 of `docs/lang/python-structure.md`
(`internal/lang/python/workspace.go`, `ModuleResolver.findWithSite`):

- **Declarations** (as revised by the review fixes below): a file's
  environment and its installed projects follow uv's discovery; members
  are the root with `[project]` and the `members` glob matches minus
  `exclude`; editable path sources count when a requirement names them.
- **Packages**: only `build-backend = "hatchling.build"`, a static
  `project.name`, and as hatch build options only `packages` (each a
  directory with a scanned `__init__.py`) and `include` patterns inside
  those packages; without them hatchling 1.31.0's default file selection
  (`<name>/__init__.py`, then `src/<name>/__init__.py`, for the name as
  written and normalised; a module or namespace default is refused).
  Everything else (other backends, `sources`, `only-include`,
  `force-include`, `exclude`, `dev-mode-dirs`, `dev-mode-exact`, hooks,
  `tool.uv.package = false`, no `[build-system]`) refuses the member.
- **Search**: the installed packages form one entry after the ancestor
  roots, before sibling fallback and implicit roots; it holds only the
  declared packages, so the claim is the same for an editable install
  (hatchling's `.pth` puts the package's parent on `sys.path`) and a
  regular one (the package copied into site-packages). A name two members
  install is `ambiguous_import` (PS-I7): the order of their `.pth` files
  decides.
- **Refusal**: a member or declaration not read exactly adds nothing and
  reports one `info` diagnostic `python_workspace_member_unread` on its
  `pyproject.toml`; imports keep their PS-M1 answer.
- **Assumption** (stated in the contract, policy): the environment uv's
  discovery gives a file is synced by default (`uv sync --all-packages`:
  all members, default groups, no extras).
- **TOML**: `pyproject.toml` is read by a TOML 1.0 subset reader
  (`internal/lang/python/toml.go`): tables, arrays of tables, dotted and
  quoted keys, all string forms, arrays, inline tables, booleans; numbers
  and dates kept as text; what TOML forbids (a table or key defined twice,
  extending an inline table) is an error, and an unreadable file declares
  nothing. The repository has no TOML module (`go.mod`), and the Rust
  `Cargo.toml` line reader (`internal/lang/rust/manifest.go`) reads single
  string keys only (no multi-line arrays, inline tables or quoted keys),
  so it cannot read `members = [...]` or `{ path = ..., editable = true }`;
  no dependency is added. `TestTOMLReader` compares it with Python's
  `tomllib` on its cases, every fixture `pyproject.toml` and, opt in, the
  corpora's.
- **Oracle**: `scripts/oracle/python_structure/oracle.py` reads the
  declarations with `tomllib` from the raw files on disk, derives the
  packages from hatchling's rules on its own disk listing, and adds a
  site-packages entry after the ancestor roots; its PathFinder decides.
- Extractor version `extractors-43` (a link-time change; no cached
  payload changes).

## Review fixes (S2XR)

The independent review the T020 task record (internal) (at `4fd8620`,
with CPython 3.14.10, hatchling 1.31.0, uv 0.13.9's source and the glob
crate 0.3.4) found one class A finding and six class B. Decisions: model uv's exact rules where the langflow shape needs them,
refuse what cannot be modelled exactly. Premises, from uv 0.13.9
`crates/uv-workspace/src/workspace.rs` (read from the PyPI sdist,
MIT OR Apache-2.0) and glob 0.3.4 `src/lib.rs`:

- `Workspace::discover` (about lines 465-520): the project is the nearest
  ancestor with a `pyproject.toml`; `tool.uv.managed = false` there gives
  no workspace. `find_workspace` (about lines 1935-2025) reads the first
  `pyproject.toml` above the project only: a workspace there that does
  not include the project, or excludes it, or no workspace, makes the
  project standalone.
- `collect_members_only` (lines 1220-1478): the root is a member when it
  has `[project]`; each `members` glob is normalised and joined to the
  root escaped with `glob::Pattern::escape`, then expanded with `glob()`
  (glob 0.3.4 `glob_with`/`fill_todo`: one component at a time, a literal
  component kept when the path exists, a wildcard component matched
  against every entry of the directory, hidden ones too); an excluded
  match is skipped; a match without `pyproject.toml` is skipped when it is
  not a directory, hidden, or holds only gitignored files, otherwise an
  error; `managed = false` omits a member; a member without `[project]`,
  two members of one name, and a non-root member with its own
  `[tool.uv.workspace]` (`NestedWorkspace`) are errors.
- `WorkspaceExclusions::matches` (lines 2062-2140): `Pattern::matches_path`
  with the default `MatchOptions` on the path relative to the root, so `*`
  and `?` cross `/` (checked with a glob 0.3.4 probe program;
  `TestGlobMatch` holds its answers).
- `PyProjectToml::is_package` (`pyproject.rs` 158-166): a project is
  installed as a package when `tool.uv.package` says so, else when it has
  a `[build-system]` table. Default groups (`pyproject.rs` 104-131):
  `dev`, unless `tool.uv.default-groups` is set.
- hatchling merges `<project>/hatch.toml` over `[tool.hatch]`
  (`hatchling/metadata/core.py`, reviewer).

Fixes, each with a unit test (`TestWorkspaceMembers`,
`TestWorkspaceEscapedRoot`, `TestWorkspaceScope`, `TestWorkspaceStdlibName`,
`TestWorkspacePathSource`, `TestGlobMatch`, `tomlCases`) and checked by
mutation (each test fails with its fix removed; the C1 change is
equivalent to the empty-match rule and needs no separate mutation):

- **A1**: a project with a `hatch.toml` is refused
  (`python_workspace_member_unread`, its own reason).
- **B1, B2**: members are expanded as `glob()` walks them, starting at the
  root (whose own name is therefore literal); exclusions match with `*` and
  `?` crossing `/`. `[`, `]`, `\` and `**` still refuse the workspace;
  `{`, `}` and `!` are literal, as in the glob crate.
- **B3**: the importer's environment is uv's (above), not "under the
  declaring directory": a nested project that is not a member, or is
  excluded, gets no workspace packages. `managed = false` members are
  left out; a member declaring its own workspace refuses the workspace. A
  path source is installed only when an unconditional requirement names
  it (a member's `project.dependencies`, the root's default dependency
  groups; PEP 503 names; markers and direct references left out); the
  root's sources apply to members, with paths relative to the root. The
  fixture's `wsext` is now a requirement of the member `wscore` (same
  edge), and `wsundeclared` is a source no requirement names (no edge;
  its namespace still hides it).
- **B4**: an installed package named like a standard-library, built-in or
  frozen module (`stdlibSet`) gets no PS-M2 answer for that name; the
  oracle uses the running interpreter's own `sys.stdlib_module_names`,
  built-in, frozen and standard-library `PathFinder` answers.
- **B5**: the TOML reader validates numbers and dates against TOML 1.0
  (with tomllib's range checks) and refuses bare words, a lone CR and
  control characters in comments; the review's cases are in `tomlCases`
  (78 documents compared with tomllib with the corpora opt-in, 0 differences).
- **B6**: a test for every refusal and omission listed in row PS-M2.
  The oracle now implements the discovery (environment, glob walk,
  `fnmatch` exclusions, managed, nested workspaces, requirements) on its
  own; it still shares the reading of uv and hatchling (Known gaps).
- **C1**: a literal member path that does not exist is ignored, as uv
  does. **C3**: an exclusion matching a hidden directory no longer
  refuses. **C4**: a project uv does not package (no `[build-system]`,
  `tool.uv.package` unset or false) is not installed and gets no
  diagnostic. **C2, C6**: Known gaps.

## Consequences

- langflow gains the `lfx` edges the namespace directory hid, and calls
  through them; wrong edges through the namespace (`lfx.<name>` where
  `src/lfx/<name>` is beside the package) become `unresolved_import`
  warnings.
- Members found before by sibling fallback are now found on the ancestor
  path's site entry (same target); a name several sibling roots held is
  still ambiguous when two members install it.
- Every declared member the reader refuses gets an info diagnostic.

## Known gaps

- Other backends (setuptools, poetry-core, pdm-backend, flit-core,
  uv_build) and Poetry/PDM/hatch path dependencies are refused (no entry).
- Non-package names in an editable member's directory (`.pth` puts the
  whole directory on `sys.path`) are not modelled; they keep PS-M1's
  answer.
- `dev-mode-exact` (a meta-path finder ahead of `sys.path`) is refused;
  when used, an ancestor package of the same name would really lose.
- The member set and layout rules are a reading of uv and hatchling that
  the oracle shares (it implements the same reading independently in
  Python over its own disk listing, so it catches code that departs from
  the reading, not a wrong reading). The layouts were checked once
  against hatchling 1.31.0's own editable build (the `.pth` directories of
  every langflow member, fastapi's `backend` and the fixture's members
  agreed with PS-M2's package parents; the review found hatchling 1.18.0
  gives the same `.pth` for the default `src` layout), not on every run.
- A glob match without `pyproject.toml` whose files are all skipped by the
  scan refuses the workspace: uv skips it when every file is gitignored
  (`has_only_gitignored_files`, which also reads global and `.ignore`
  rules), and the scan's skipped list does not say why a file was skipped
. A match with no file at all is skipped, as uv does.
- A glob walk into a directory the scan skipped refuses the workspace
  (its entries are not known); the oracle walks the whole disk and does
  not apply this refusal.
- Members' own default dependency groups and optional dependencies are
  not read for path sources (a default sync installs no extras; whether
  members' groups are installed was not established): such a source adds
  no entry (an omission).
- `{ workspace = true, editable = false }` is not read: the
  standard wheel holds the same package under PS-M2's allowed options.
- An `exclude` or member glob with `..` leaving the workspace root
  refuses the workspace (uv joins it to the escaped root; not modelled).

## Review notes (acceptance, 2026-11-18)

Accepted after delegated review (2026-11-17). Steps: S2X implementation (row PS-M2), S2I (merge with the TypeScript and Rust structure stacks, extractors-43), one independent review (the T020 task record (internal): one class A, six class B, C items), S2XF fixes (A1 and B1-B6 fixed and mutation-checked, 11 of 11), a confirmation pass by the reviewer (every finding closed; one remaining lone-CR TOML form fixed afterwards).

- Evidence used: CPython 3.14.10 (`tomllib`, PathFinder), hatchling 1.31.0 editable and regular builds, uv 0.13.9 source (workspace discovery), the glob crate 0.3.4 (uv's pinned version). docs.astral.sh was refused by the proxy and was not needed.
- Results: the oracle (`TestPythonStructureOracle`, now reading uv discovery independently of the code) reports 0 gate records on the fixture, fastapi (43 files / 192 import statements) and langflow (4,143 / 31,557). langflow gains 17 import edges into the `lfx` member and `namespace_hides_concrete` drops from 23 to 0; nothing else changes. The TOML reader agrees with `tomllib` on 96 documents.
- Refused rather than modelled: other build backends, hatch options beyond `packages`/`include`, a member with a `hatch.toml`, glob syntax beyond `*`/`?` in exclusions, a nested workspace, glob matches whose files are all skipped by the scan; members' own dev groups are not read for path sources.
- Gates: local full check 0 FAIL, 172 `/postgres` PASS; CI run 44240876687 (attempt 2) on fa63d8a (`[ci windows]`, the stacked S2X + precision follow-ups + GORM tree) green, postgres raw log 0 FAIL, 176 `/postgres` PASS, Windows 0 FAIL.
