# ADR 0015 — Python module resolution and FastAPI routes

**Status:** Accepted (developer review, 2026-10-10)
**Date:** 2026-10-10

## Context

The T011c investigation found 23 literal generated SDK operations and 23 backend
registrations in the pinned full-stack-fastapi-template. Existing imports
reached its package files, but callable-only binding resolution could not follow
router/settings assignments or initializer re-exports. Route composition needs
the same current declarations as import and call resolution.

## Options considered

- Run Python/FastAPI and inspect live apps: requires dependencies and environment
  configuration, executes repository code, and cannot honor offline extraction.
- Infer objects from receiver names: creates false routes on unrelated objects.
- Cache resolved imports or composed paths: becomes stale after target-only edits.
- Cache syntax and resolve against the current scan: chosen, using existing
  contract v1 attributes and the existing fact cache.

## Decision

Use the shared resolver in `internal/lang/python` described in
[python-resolution.md](../lang/python-resolution.md) for imports, calls,
framework constructors, route endpoints and prefixes. Metadata is read from the
current scan; internal per-file caches retain scopes, imports, bindings, class
bodies and registration syntax only. Constructor bindings retain only syntax
used by resolution, rather than arbitrary dependency/model argument payloads.
Per-pass module and completed callable memoization do not persist, and per-name
cycle guards never memoize incomplete re-export traversals.
Aggregate extraction version becomes `extractors-9`.

FastAPI recognition requires an external import of exactly FastAPI/APIRouter
from fastapi, FastAPI from fastapi.applications, or APIRouter from fastapi.routing.
Scanned shadow modules win. Per-file Python syntax stays in the language package;
composition stays in `internal/extract/routes`, using the same path entry shape
as Express. All FastAPI apps are roots. Router constructor prefixes, nested
includes and mounts concatenate before normalization. Named handlers get fact,
confidence-1 handler-to-route `exposes_route` edges with registration evidence.
Decorators hash only their call expression, excluding `@` and handler bodies.
Source identities follow ADR 0013, including declaration-order suffixes.

Prefixes accept literals, stable module literal assignments and source-backed
settings defaults. `settings_default` is environment-overridable: the own class
literal comes from a direct imported BaseSettings subclass and an unoverridden
module instance. Arbitrary keyword unpacking is unresolved because it may
override the attribute. Class-body conditionals cannot prove an unconditional
default. Resolution strength is settings_default < constant < literal; a chain
uses the weakest and duplicate templates retain the strongest proof. Conditional
chains merge to unconditional if any identical path is unconditional.

The task's intended deviations are explicit:

1. Includes are order-insensitive live compositions, although FastAPI copies
   registrations at include time.
2. Normalization removes trailing slashes except root.
3. Starlette convertor suffixes are stripped; `{p:path}` becomes `{p}` without
   modeling multiple segments.
4. Resolution uses scanned project roots rather than interpreter sys.path.
5. Every FastAPI app is a composition root, including mounted sub-apps.
6. Settings defaults ignore environment variables, dotenv files, validators and
   Field defaults.

Real-semantics corrections: methods=None and empty method lists/tuples/sets
default to GET. Starlette treats colons/asterisks outside brace parameters as
literal path characters, so FastAPI does not apply Express substitutions.
Python's package finder prefers regular packages to source
modules, and source modules to namespace directories; intermediate source
modules cannot contain submodules. Unknown keyword unpacks do not prove omitted prefix/method defaults.
Bare function names skip an enclosing class
namespace. Explicit assignment aliases do not establish constant prefixes.

Contract v1, store interface, schema, dependencies and non-route source identity
policy remain unchanged. This extends ADRs 0006 and 0013 without editing them.

## Consequences

Settings and target-module changes relink cached importers and recompose routes.
Handler body edits preserve route hashes. The aggregate version invalidates all
language caches once. Source evidence and conservative diagnostics remain
available without evaluating Python or importing project code.

## Known gaps

No type inference, inheritance traversal, star-import expansion, arbitrary
assignment flow or evaluation of constructor/factory return values. Function
locals count only in their binding function. FastAPI dependencies are not
middleware links; websocket registrations are excluded from HTTP routes.
Unicode named string escapes require a Unicode name table and stay unresolved.
Postgres parity and execution on other operating systems belong to CI.

## Review notes (developer, 2026-10-10)

- Accepted. full-stack-fastapi-template: 23/23 SDK operations, 23
  exposes_route edges, settings_default on every path, conditional only
  on the private router include. Express unchanged (24/24, 19/19).
  Langflow: 318 routes with handler edges; Python import statements
  resolving to scanned files rise from 5,515 to 15,387 and unresolved
  call sites fall from 209,011 to 180,563. No unresolved count rises on
  the external corpora. Same-session interleaved Langflow index time:
  -3.3% against main (an earlier +11.2% run was fixed by trimming cached
  constructor syntax and memoizing completed lookups per pass).
- Regression from the task's root policy (the reviewer's specification,
  not the implementation): repositories without pyproject.toml,
  setup.py or setup.cfg lose absolute-import resolution through package
  ancestors and the old src probe (self-index: four call sites in
  testdata/repos/python). Common in legacy and Django projects. Follow-up
  task: implicit roots at the parent of each top-level regular package,
  plus the src probe, at the lowest ancestor-root precedence.
- Empty methods collections default to GET. Starlette appears to treat
  an empty method set as matching every method; verify against FastAPI's
  APIRoute and switch to ANY or a diagnostic if confirmed.
- Six Langflow api_route decorators pass methods through **config
  dictionaries and produce no route (route_dynamic_methods); T012 will
  report their client calls as unmatched.

## Revision 1 (T035: mounted FastAPI apps)

**Status:** Accepted (delegated review, 2026-11-24)

### Context

Intended deviation 5 ("every FastAPI app is a composition root, including
mounted sub-apps") stored, for a mounted app, full paths the served app
answers 404 for: with `app.mount("/sub", sub)`, `sub`'s `@sub.get("/ping")`
was stored as `/ping` beside `/sub/ping`. FastAPI 0.121.1 (Starlette
0.49.3) and 0.165.0 (Starlette 1.10.0) serve a mounted app only below its
mount. The T035 task record (internal) has the diagnosis and the oracle.

### Options considered

- Keep deviation 5: rejected; it stores a confident wrong path per mounted
  route.
- Drop roots only for constant-prefix mounts: rejected; a mount at an
  unread prefix also moves the app, so its own-root path is still wrong.
- An app a mount consumes is no root (chosen), and compose the mount forms
  whose path is provable.

### Decision

Deviation 5 is superseded: every FastAPI app is a root unless a Starlette
mount consumes it. `x.mount(p, app)` consumes `app` whatever `p` and `x`
are; a constant `p` on a proven `x` composes the paths through the mount,
any other `p` is a hop (`unresolved_mounts`), an unread `x` gives no path
(`route_mount_unresolved`). `x.router.mount(p, app)` on a proven app `x` is
`x.mount(p, app)`. A Starlette `Mount(p, app)` (`starlette.routing.Mount`,
also through `fastapi.routing`) consumes `app`; written directly in the
`routes=` list literal of a proven `FastAPI(...)` constructor (outside a
helper context) it composes as `mount` on that app, anywhere else it gives
no path. A mount in a test-scope file consumes only a test-scope app. The
syntax keeps `Mount(...)` calls and whether each is an element of a
`routes=` list literal (a cached syntax change; extractors version).

Step EM2 adds three parts (link-time; no further syntax change):

- `router.mount(p, app)` on an `APIRouter` composes only where the
  project's manifest proves FastAPI >= 0.137.0 (the lowest version the
  nearest pyproject.toml allows, as the route shape's version-dependent
  forms read it): 0.137.0 introduced lazy included routers and serves the
  mount below the include's prefix without the mounting router's own
  prefix; 0.136.3 and earlier copy only API routes at `include_router` and
  drop the mount. Otherwise `app` is consumed and named in
  `route_mount_unresolved`, no path. A proven version with an unread `p`:
  `route_prefix_unresolved`, no path (no hop: its known part above would
  carry the router's own prefix).
- Route order: Starlette dispatches an app's entries in the order they were
  added and a `Mount` answers every request below its prefix (404
  included). A composed path of a route reached through an entry added
  after a mount on the same object (or in an order no rule proves: another
  file, function or helper context) whose prefix takes the path is not
  stored (info `route_shadowed`). The prefix itself is not taken, a
  trailing-slash path below it is; a template parameter under a literal
  prefix segment is taken only for some values and keeps its path. Every
  mount counts, whatever it mounts (a `StaticFiles` too). A mount whose
  prefix is not read may take a later path or not: the path is kept,
  conditional, with the shown assumption RO-V1 ("a mount at an unknown
  prefix may answer first") naming the mount (first decided as
  "may take any later path, not stored", which dropped every later path of
  an app with one static mount at a configured URL; FastAPI 0.165.0 /
  Starlette 1.10.0 serve those paths for a prefix elsewhere). Only a read
  prefix that takes the path withholds it; such a route stores the
  withheld paths in `shadowed_paths` and is shown as shadowed, not
  unmounted. A `Mount` in a constructor's `routes=` list literal comes
  before every call on the object.
- A consumed app stays a root when served at its own root by a form from a
  closed list: `uvicorn.run(x, ...)` (or `app=x`) with `x` a proven app
  (conditional under a branch, the `if __name__ == "__main__":` guard
  included, or inside a function), or a committed pyproject.toml's
  `[tool.fastapi] entrypoint = "pkg.mod:name"` naming a module-level
  binding of a proven app in `<manifest dir>/pkg/mod.py` (fastapi-cli
  0.0.34 imports it from the manifest's directory). A form in a test-scope
  file serves only a test-scope app.

### Consequences

A mounted app's routes have only the paths the served app answers; an
unmounted app keeps its own root (the app-root policy for apps served on
their own). Routes of apps mounted through a form not composed lose their
path (precision-safe). Golden outputs of mounted sub-apps lose the own-root
path; OpenAPI documents lose the matching operation.

### Known gaps

An app that is both mounted and served on its own through a form outside
the closed list (`uvicorn` or `gunicorn` on the command line, a Dockerfile
or Procfile, `uvicorn.run("pkg.mod:app")` as a string, a `Config`/`Server`
pair) loses its own-root paths. `Mount` in an `APIRouter(routes=...)` list,
a routes list bound to a name or built elsewhere, and `app.router`
reassigned to another router are not read; such a list is also not read for
shadowing. Route order between two routes (not a mount) of one app is not
modelled, nor shadowing below an unresolved-mount hop (`unresolved_mounts`
paths). FastAPI releases after 0.165.0 are assumed to keep 0.137.0's
included-router mounts.

Revision 1 review notes (2026-11-24): confirmed pre-existing wrong facts (mounted Express and FastAPI sub-apps stored at their own root; the real frameworks answer 404 there) fixed with oracles on Express 4.21.1/5.1.0 and FastAPI 0.121.1/0.136.3/0.137.0/0.165.0; one independent review and a confirmation pass (no open finding); gate run 45311511149 green on Linux, Postgres and Windows; no corpus route changed except conditional marks with shown assumptions (RealWorld 21, seerr 1, gladys 1). Known gaps: an app served through forms outside the closed list (Lambda wrappers, a string passed to `uvicorn.run`, a Dockerfile command) keeps only its mounted paths.
