# ADR 0017 — Python implicit roots and empty FastAPI methods

**Status:** Accepted (developer review, 2026-10-11)
**Date:** 2026-10-11

## Context

ADR 0015's metadata roots lose absolute imports in legacy and Django-like
projects without packaging metadata. Self-index lost four call sites in the
nested Python fixture. The documented repository src probe is missing without
repository metadata. Empty FastAPI methods collections were treated as GET.

## Options considered

- Restore the old package-ancestor climb before metadata: retargets imports
  already resolving through explicit roots and sibling fallback.
- Treat every source directory as a root: invents flat sibling/namespace roots
  and creates cross-project ambiguities.
- Execute Python or read installed sys.path: requires environments and executes
  repository code, contrary to offline deterministic extraction.
- Derive regular-package roots each link pass after explicit resolution: chosen.

## Decision

A directory with a scanned __init__.py is a top-level regular package when its
parent has no scanned __init__.py. That parent is an implicit root. Namespace
directories themselves create none. Only ancestors of the importing file apply;
implicit roots never enter sibling fallback and receive no src child. A root
equal to an explicit root is explicit.

Run explicit ancestor resolution deepest first, then existing sibling fallback
including ambiguous_import, unchanged. Only when no candidate or ambiguity
exists, try implicit ancestors deepest first, taking the first candidate. Keep
regular-package/source-module/namespace probe order. Restore repository src in
the documented explicit-root position when it contains scanned Python sources.
Derive roots from the current inventory every link pass. Imports, calls, FastAPI
objects, handlers and prefixes share one resolver. Initializer changes relink
cached importers without reparsing them.

FastAPI APIRoute defaults only methods=None to GET, otherwise uses the supplied
set; Starlette Route.matches restricts methods only when that set is nonempty.
Missing/None methods retain GET. Literal empty [] and () emit one ANY route.
set() remains a nonliteral call and produces route_dynamic_methods with no route.
An ANY registration from empty methods has no OpenAPI operation in FastAPI's
own specification; later OpenAPI generation must account for this difference.
Offline framework sources were unavailable; these semantics follow T011e's
stated library behavior.

Per-file syntax payloads do not change: roots and methods expansion are link-pass
decisions. Retain extractors-10, contract v1, dependencies, store and schema.

## Consequences

Legacy absolute imports gain scanned dependencies and callable edges. Explicit
targets and sibling ambiguities retain priority. Empty-method route identity
changes from GET to ANY intentionally, retaining the registration span/hash.
Current-inventory linking preserves incremental replay.

## Known gaps

Real sys.path depends on script directory, working directory and installed
packages. These rules approximate launch from a top-level package's parent.
Flat sibling modules outside regular packages remain external/unresolved even
though executing a script there could make them importable. When repository and
implicit roots both provide a candidate, the explicit repository root wins even
if a project launches from the implicit root. Explicit sibling resolution and
ambiguity also precede implicit ancestors to preserve the additive guarantee.
The package-parent approximation can select a same-named component package
instead of an installed SDK: Langflow's firecrawl component package under a
namespace parent is one example. A scanned module dependency does not prove
that its requested export exists; unavailable exports remain without callable
edges. Launching an installed bundle with another sys.path can select the SDK.

No namespace-created roots, installed inventory, runtime execution, type
inference or arbitrary method-expression evaluation is added. Postgres and
cross-platform execution require CI; storage code is unchanged.

## Review notes (developer, 2026-10-11)

- Accepted. Self-index call sites in testdata/repos/python/pkg/main.py
  (lines 40, 41, 43, 46) resolve to their pre-T011c targets. Strictly
  additive on every corpus: all existing import/call edges and evidence
  spans survive. Langflow gains 121 import and 211 call edges (133
  statements: 129 test-helper imports, 4 firecrawl); FastAPI template
  unchanged at 23/23. extractors-10 retained, verified by main-warmed
  replay equality on Langflow.
- Empty methods verified against current FastAPI and Starlette source
  (2026-10-11): APIRoute replaces only None with ["GET"]; Route.matches
  restricts methods only for a non-empty set. FastAPI's APIRoute does not
  call Starlette Route.__init__, so FastAPI GET routes do not answer HEAD
  (Starlette, Express and Axum GET routes do). T012 matching uses this.
- Timing: the first interleaved series failed (+26.7%) on one 79 s scan
  in a branch sample; scan code is unchanged. The full repeat series is
  +3.4%. Protocol from now on: when an outlier appears in a phase the
  change cannot affect, the complete series may be repeated once, and
  both series are reported.
- Firecrawl self-shadow: a local package named after a third-party SDK it
  imports resolves to itself through its implicit root (4 Langflow
  statements, no callable edges invented). If it recurs, prefer the
  external module when the implicit-root target is the importer's own
  top-level package and lacks the requested name.
- Explicit sibling fallback outranks implicit ancestors to keep the
  additive guarantee. The T011e fixture showed the cost (an unrelated
  sibling `pkg` hid the importer's own package). Real Python favours the
  importer's own tree. Revisit with an edge-target diff when a monorepo
  without packaging metadata enters the corpus.
