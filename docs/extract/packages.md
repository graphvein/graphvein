# Package manifests

T026, ADR 0079; extractors-48 (renumbered at merge); contract v1
unchanged (`package` is an extensible node kind). The claim contract is the
components table of `docs/contract/claims/architecture-graph.md`: rows CM-M1 to
CM-M5, CM-W1 and CM-U1 are the indexer's (`internal/extract/packages`,
`Contract`), the other rows are the server's (`internal/server/architecture.go`).
`TestPackagesContractRows` and `TestComponentContract` keep code and table
1:1; `TestComponentsFixture` reaches every indexer row on
`testdata/fixtures/components` (golden `components.golden.json`).

The pass runs at link time over the retained sources and the cached Python
and Go facts; it parses nothing and runs no manifest code. It states what a
manifest says and nothing else: components, kinds and visibility are server
computations over these nodes.

## Node

One `package` node per confirmed manifest: level 2, identity
`package:<manifest path>#<name>` (the directory when a `setup.py` names no
literal name), `file` the manifest, a span over the whole manifest and its
content hash. Attributes: `ecosystem` (`npm`, `cargo`, `python`, `go`),
`name` (null when none), `directory`, and per ecosystem:

| ecosystem | attributes |
|---|---|
| npm | `dependencies` (names of `dependencies`, `devDependencies`, `peerDependencies`, `optionalDependencies`), `workspaces` (as written, when a string array or `{packages: [...]}`), `bins` (the `bin` object's keys, or the package name for a string `bin`) |
| cargo | `dependencies` (every dependency table, top level and `[target.*]`, renamed entries by their `package`), `bins` (`[[bin]]` names), `lib` (a `[lib]` table), `autobins` (false only for a literal `autobins = false`) |
| python (`pyproject.toml`) | `dependencies` (PEP 508 names of `[project].dependencies`, keys of `[tool.poetry.dependencies]` but `python`), `scripts` (`[project.scripts]` and `[tool.poetry.scripts]` names) |
| python (`setup.py`, `setup.cfg`) | none |
| go | `main_files` (the module's Go files in package `main` that declare `func main`, nearest `go.mod` wins) |

## Rows

- **CM-M1** `package.json` with a nonempty string `name`. No `name` (or an
  empty one): a `workspaces` root is CM-W1, anything else is not a package.
- **CM-M2** `Cargo.toml` with `[package]` and a string `name`, read with
  `internal/extract/cargotoml` (accepts multi-line inline tables). Only
  `[workspace]`: CM-W1.
- **CM-M3** `pyproject.toml` with a string `[project].name`, else
  `[tool.poetry].name`, read with the same TOML reader. Tool configuration
  only: not a package; `[tool.uv.workspace]` without a project: CM-W1.
- **CM-M4** `go.mod` with a `module` directive.
- **CM-M5** `setup.py` with a module-level call of `setup` imported from
  `setuptools` or `distutils.core` (`from ... import setup`, or
  `import setuptools` / `import distutils.core` with or without an alias),
  found in the cached Python facts; its name only when `name=` is one plain
  string literal. `setup.cfg` with `[metadata] name`. Read only in a
  directory that no CM-M1..M4 package and no CM-U1 manifest encloses (same
  directory or an ancestor); enclosed, or without such a call: not a
  package, no diagnostic.
- **CM-W1** workspace roots without a package of their own: no node.
- **CM-U1** (the unknown branch) every other form of these file names:
  invalid JSON, a JSON value that is not an object, a non-string `name`,
  TOML the reader refuses, `[package]` or `[project]` without a string
  name, a `Cargo.toml` with neither `[package]` nor `[workspace]`, a
  `go.mod` without a module directive, a `setup.py` that was not read as
  Python (too large, parse failure, rejected syntax), a name that cannot
  form an identity. One `component_manifest_unread` info diagnostic on the
  manifest; the server shows the directory as an unread component of
  unknown kind, and its files never fall to an ancestor.

A failure in one manifest never affects another.
