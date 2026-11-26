# graphvein openapi

```text
graphvein openapi [path] [--format json|yaml] [--out file] [--repository ID]
graphvein openapi --project DIR [--repository MEMBER] [--format json|yaml] [--out file]
```

Writes an OpenAPI 3.1 document of the HTTP routes Graphvein stored for a
repository (T029, [ADR 0081](../adr/0081-openapi-explorer-try-it.md)).
It reads the index and `graphvein.yaml` only: it runs no index, reads no
source file, runs no code and opens no network connection. Run
`graphvein index` first; a repository that is not indexed is refused
(exit 1).

The same function writes the document of
[`GET /api/v0/openapi`](serve.md#get-apiv0openapi) and the explorer's
download, so the CLI, the server and the UI give the same bytes for one
store state.

## Options

| Option | Meaning |
|---|---|
| `path` | the repository directory (default `.`); the store is found as for `graphvein query` (`GRAPHVEIN_POSTGRES_URL`, else `GRAPHVEIN_SQLITE_PATH`, else `.graphvein/index.db`) |
| `--format json` | JSON, two-space indent, LF, final newline (the default) |
| `--format yaml` | YAML: the same document with the keys in the same order |
| `--out file` | write the document to `file` (atomically) instead of standard output |
| `--repository ID` | the repository ID, as for `graphvein index` |
| `--project DIR` | a project manifest or its directory: write the document of one member (`--repository MEMBER`, optional when the project has one member), read from the project store, with that member's `graphvein.yaml` (none for a member without a checkout) |

Exit codes: 0 written, 1 not indexed or a read or write error, 2 usage
or a `graphvein.yaml` that does not load.

## What the document holds

Every field comes from a row of the
[claim contract](../contract/claims/openapi-generation.md); a field no
row names is not written. In short:

- **One document per repository** (per member in project mode). Each
  operation names its route, file, line, framework and extraction rule in
  `x-graphvein`, so a reader can tell the servers of a monorepo apart;
  `servers` lists only the configured try-it environments
  ([`tryit`](../config.md#try-it) in `graphvein.yaml`, and in project mode
  the project's environments for the member), never a URL read from code,
  and never their auth settings.
- **Paths and methods** come from the stored path entries: router
  prefixes, mounts and trailing slashes as composed at index time.
  Conditional registrations and paths holding a settings default
  (FastAPI's `API_V1_STR`) are included and marked
  (`x-graphvein.conditional`, `x-graphvein.resolution:
  settings_default`).
- **Never picked silently.** Two routes with the same method and path
  (two apps, two registrations) are listed in `x-graphvein.conflicts` and
  get no operation; so are paths that differ only in parameter names.
  Multi-segment wildcards, optional parameters, host patterns and methods
  outside OpenAPI's eight are listed in `x-graphvein.unrepresentable`;
  `ANY` routes in the path item's `x-graphvein.any_method`.
- **Routes without a path** (an unproven mount or prefix, a Next.js
  `basePath` read from the environment) are listed in
  `x-graphvein.unresolved` with the unknown part named and the
  `explorer.path_values` keys that would place them.
- **Absent means not read.** Without a stored shape an operation has only
  its path parameters (named, required, untyped). With a stored
  [route shape](../extract/route-shape.md), only items of kind `fact`
  become `parameters`, `requestBody`, `responses` and `security`; names
  read in code, intended and returned types and mapped items stay in
  `x-graphvein.shape.not_standard`. Each operation lists what was not
  read in `x-graphvein.not_read`. `security: []` and `operationId` are
  never written.
- **First-match frameworks.** In Express and FastAPI the first matching
  registration answers; an operation lists in
  `x-graphvein.may_be_shadowed` the routes whose path matches all its
  requests (order not checked: registration order across routers is not
  stored).
- **Test-only routes** are left out and counted
  (`x-graphvein.excluded_test`).
- **Committed specs are never merged.** They are listed in
  `x-graphvein.imported_specs`, and an operation a committed spec
  documents names it in `x-graphvein.documented_by`.

## Values set by you

A route Graphvein could not place on a path can be placed by stating the
missing part in `graphvein.yaml` ([explorer](../config.md#explorer)), by
hand or from the explorer (a guarded edit,
[`POST /api/v0/explorer/path-values`](serve.md#get-and-post-apiv0explorerpath-values)):

```yaml
version: 1
explorer:
  path_values:
    - key: "route:src/workflows.py#POST%20%2Fworkflows"   # the whole path of one route
      value: /api/v2/workflows
    - key: nextjs_base_path:apps/web                       # a Next.js basePath
      value: /dashboard
```

The operation is then written with `x-graphvein.resolution: set_by_you`
and an assumption naming the key and value, never as read from code. A
value never replaces or displaces a path read from code (it is reported
`unused` with the reason), and it changes only this document, the
explorer and try-it. A route key also records the route's `registration` hash, so a value
never silently moves to another registration; see
[explorer](../config.md#explorer).

## Validation

The documents of the golden fixtures and of the corpus check validate
against the OpenAPI 3.1 JSON Schema (test-only oracle, see the claim
contract's "Validation").
