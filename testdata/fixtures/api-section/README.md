# API section fixture

One repository with an Express backend, fetch/axios and Tauri frontend code,
an OpenAPI document and a Tauri app, indexed by `internal/server/api_test.go`
for the API section endpoints (T016d1, ADR 0042). Goldens:
`internal/server/testdata/api-*.golden.json`; the MCP session golden
`internal/mcp/testdata/session-api.golden.json` (`TestSessionGoldens`)
reads the same fixture.

Isolation in the self-index (T016d Revision 1): Graphvein indexes this
directory as part of its own repository, where route matching and Tauri app
scope are repository-wide, so the fixture must neither reach nor be reached
by other fixtures.

- Route paths and call URLs use names no other fixture uses
  (`/t016d-items`, `/t016d-tie`, ...). A path of one segment cannot be the
  target of another call's suffix match, which needs a route of two or more
  segments. Of the two-segment routes, `/t016d-api/t016d-users` ends in a
  segment no other call names; `/t016d-items/:id` ends in a parameter, which
  a one-segment call with an unresolved base elsewhere could reach only as
  a suffix match with no literal segment (the Revision 1 self-index diff
  found none). Command names start with `t016d_`.
- The unmatched call with an unresolved base (`remoteUnmatched`) has only a
  dynamic segment (`DELETE /{}`), so it gets no suffix match (which needs a
  literal segment and would reach the repository's `ANY /files/*`), and no
  stored route is a lone DELETE or ANY parameter or wildcard.
- The Tauri app files are inert (below), so the self-index gains no app
  registry and other fixtures' `tauri_app_ambiguous` counts stay unchanged.

Inert names: the test copies the fixture to a temporary directory and
restores, before indexing:

| stored as | restored to |
|---|---|
| `_graphveinignore` | `.graphveinignore` (ignores `tools/package.json`) |
| `app/Cargo.toml.inert` | `app/Cargo.toml` |
| `app/build.rs.inert` | `app/build.rs` |
| `app/tauri.conf.json.inert` | `app/tauri.conf.json` |
| `app/src/main.rs.inert` | `app/src/main.rs` |
| `app/src/commands.rs.inert` | `app/src/commands.rs` |

| file | covers |
|---|---|
| `server/app.ts` | Express routes: documented `GET /t016d-items`, routes called by several calls, two tied `GET /t016d-tie` (ambiguous), never-called `GET /t016d-never`, an inline handler, a never-mounted router (`paths: []`) |
| `server/tests/routes.ts` | a test-scope route that only a test_only call matches |
| `api/openapi.json` | one operation documenting `GET /t016d-items`; every other route is undocumented |
| `web/src/http.ts` | linked 0.9 (exact), 0.7 (literal origin), 0.6 (suffix through an unresolved axios base); a wrapper-derived linked call; unmatched; unmatched with an unresolved base (a dynamic segment only); dynamic method; dynamic URL; ambiguous; test_only; a conditional call with two calls_route edges; a dynamic call with one linked alternative (one edge from a non-linked call) |
| `web/src/client.ts` | a fetch wrapper; its inner site has status `wrapper` |
| `web/src/http.test.ts` | a test-scope call |
| `web/src/ipc.ts` | Tauri: direct, namespace and wrapper invocations, a conditional two-target call, an unmatched call with a linked alternative, a dynamic name, a `plugin:` call, a call to a cfg-gated command |
| `web/src/ipcwrap.ts` | the invoke wrapper; its inner site has status `wrapper` |
| `web/src/ipc.test.ts` | a test-scope invoke |
| `app/` | (inert, restored by the test) one proven Tauri app: four commands, one cfg-gated (`t016d_debug`), one never invoked (`t016d_never`) |
| `web/package.json` | source root `web` |
| `web/packages/ui/` | a nested package: source root `web/packages/ui` below `web` |
| `scripts/probe.ts` | a call without a manifest ancestor: source root `scripts` (first segment) |
| `root-call.ts` | a call at the repository root without a root manifest: source root `.` |
| `tools/` | `tools/package.json` is ignored, so it is not stored and `tools/check.ts` falls back to source root `tools` |
| `server/package.json` | a manifest of a directory without calls |
| `next/` | Next.js server actions (T024b): `t016dSave` used by `useActionState` and a direct call in a `'use client'` file, `t016dUnused` never called, `t016dPassed` passed as a prop only (flag `unread_use`, not never called) |

Cases one repository cannot hold (Tauri app scope is repository-wide, so
registry_unproven, acl_possible, ambiguous, shadow_possible, unknown_command,
test_only and global_unlinked invoke statuses need other registries) are in
the crafted snapshot `internal/server/testdata/api-statuses.ndjson`.
