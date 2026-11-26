# Python module resolution

ADRs 0015 and 0017 specify an offline current-inventory resolver shared by imports,
calls, FastAPI objects, handlers and prefixes. Cache only per-file syntax;
rebuild resolver state every link pass. Aggregate version: extractors-49; implicit
roots and empty-method expansion change linking, not cached per-file syntax.

## Roots and module probing

Roots are the repository root and directories of scanned pyproject.toml,
setup.py or setup.cfg. Include each root's src child if it contains scanned
Python sources. Ancestor roots are searched deepest first. The first candidate
wins. If ancestors have none, exactly one sibling-root candidate resolves;
several candidates produce ambiguous_import with every candidate path.
Only after explicit ancestors and sibling fallback find no candidate or ambiguity,
try implicit ancestor roots deepest first. A directory containing scanned
__init__.py is a top-level regular package when its parent has no scanned
__init__.py; that parent is an implicit root. Namespace directories create none.
Implicit roots apply only to files under them, never enter sibling fallback and
get no src child. A root equal to an explicit root is explicit. Adding/removing
initializers rebuilds roots and relinks cached importers without reparsing them.
A flat sibling module of a file whose directory is neither a package nor a
root (a script or pytest module directory) is refused, not external
(`script_directory_module`, ADR 0069 review fixes). Existing explicit
targets win even if a real project would launch from the implicit root.

Workspace members installed as packages (row PS-M2, ADR 0072, Proposed;
review fixes S2XF): a file's environment is found as uv 0.13.9 finds it
(nearest project with a pyproject.toml; the workspace it declares, or the
workspace of the first pyproject.toml above it when that one lists the
project as a member; else the project alone). The environment is assumed
synced by default (`uv sync --all-packages`): every member uv packages,
and every editable uv path source an unconditional requirement names (a
member's `project.dependencies`, the root's default dependency groups).
Members follow uv's globs and exclusions (glob crate 0.3.4 semantics),
`managed = false` and nested-workspace rules. When a project builds with
hatchling, has no `hatch.toml` and its pyproject.toml names its import
packages exactly (`packages` in the wheel target or the build table,
`include` only inside them, or hatchling's default `<name>/__init__.py` /
`src/<name>/__init__.py`), those packages form one search path entry
right after the ancestor roots, before sibling fallback and implicit
roots, as site-packages comes after the script directory (and after the
standard library: a standard-library name gets no answer from it).
PathFinder rules apply over the ancestors plus that entry: a namespace
directory on an ancestor root no longer ends the search when an installed
project provides a concrete package of the name (langflow: `src/lfx`
before `src/lfx/src/lfx`), and the installed package binds the name, so
its rest is searched only inside it. A name two projects install is
ambiguous. A workspace or project the reader cannot read exactly adds
nothing and reports a `python_workspace_member_unread` info diagnostic.
pyproject.toml is read with a TOML 1.0 subset reader
(`internal/lang/python/toml.go`, checked against tomllib).

Every Python consumer (import edges, calls, FastAPI routes, SQLModel models,
table access) resolves modules through one resolution, the structural claim
contract's ([python-structure.md](python-structure.md), ADR 0069,
extractors-40, and ADR 0072, extractors-43; `ModuleResolver.structural`, which `Resolve` wraps): the
interpreter's own modules and the meta path first, the script directory, a
PathFinder-style search over these roots, then the standard library's
concrete packages; refusals for relative imports beyond the top-level
package, for files Graphvein does not read that Python would load first, for
undecided from-names and self edges, and no external node for a top-level
name that resolves in the repository. A refused import resolves to nothing
for every consumer. Other unavailable absolute modules become `python-stdlib`
externals or `pypi` externals. No interpreter sys.path, network lookup or
installed package inventory is used.

Probe regular package `a/b/__init__.py` before source module `a/b.py`, then
namespace directories (any directory the scanner walked). Intermediate source
modules block submodule traversal. Namespace packages do not need
initializers. Stubs are not used. Relative imports start at the importing
file's package directory; every extra leading dot moves up once. Going above
the repository root remains unresolved.

## Bindings and re-exports

For `from m import n` (and the member `m.n`), the structural from-list rule
decides: a module-level binding in m (definitions, assignments, `type`
statements and imports in initializers) when m has no same-named submodule; the
submodule when m does not bind n, has no star import and no module
`__getattr__`; nothing when both exist or m defines `__getattr__` (undecided).
Do not fall through an existing unavailable/cyclic binding to a same-named
submodule. Per-name guards terminate re-export cycles. Star imports are not
followed. `import a.b.c` binds a; an explicit alias binds the full named module.
Dotted members traverse packages and explicit class members through the same
resolver. Bare names inside functions skip enclosing class namespaces.

Existing structural symbols and their identities remain unchanged. Assignment
bindings are cached syntax, not new graph nodes. The target of `except E as X`,
`with f() as X` and `case P as X` binds X in the enclosing scope (unstable, no
value), so a later X there never resolves to a class or import of the same
name (T018h Revision 5, R5-B2). Import edges record scanned
module dependencies; externally imported names retain full module/export names
for exact framework recognition. Calls emit edges only to emitted callable
declarations; instances do not establish callable targets through type inference.

## Prefixes and limitations

Literal strings include implicit adjacent concatenation, raw/triple-quoted
strings and replacement-free f-strings. Bytes and interpolations are rejected.
Stable module literal assignments must occur exactly once outside conditionals,
with no global rebinding or augmented assignment. Imported names and namespace
members preserve this rule; assignment aliases are not constant proofs.

Settings defaults require one unconditional module instance, no positional
arguments, no override of the requested attribute and no unknown keyword unpack.
Its scanned class must directly inherit externally imported BaseSettings from
pydantic_settings or pydantic. Only one unconditional literal declaration in
that class's own body qualifies. Underscore constructor options are allowed.
Inherited fields, Field calls, validators and environment files are not
evaluated. Returned resolution is settings_default, an environment-overridable
source default. See [routes.md](../extract/routes.md) for composition.

No star imports, type inference, inheritance resolution, arbitrary expression
evaluation or Unicode named-escape table. Resolver memoization lives for one
link pass only; metadata/target edits never require reparsing cached importers.
