# ADR 0025 - hey-api generated clients

**Status:** Accepted (developer review, 2026-10-19)
**Date:** 2026-10-18

## Context

The pinned FastAPI template commits 23 client-axios SDK operations. The investigation's
read-only inventory and census establish transport, URL type pinning,
serialization and configuration semantics. Revision 1 corrects the original
initializer-first base rule: setConfig merges `{...old, ...next}`.
All operations contain the full server path and match at exact confidence 0.9
even when their deployment base is unresolved.

## Options considered

- Follow the bundled implementation through general interprocedural analysis:
  broadens the evaluator into transport execution beyond the required subset.
- Recognize header comments or receiver/file names: cannot prove the configured
  transport and risks confident wrong links.
- Prove the configured transport through the shared resolver, then interpret
  the inventoried request/configuration subset: chosen.

## Decision

Require a scanned openapi-ts.config.{ts,mts,cts,js,mjs,cjs} default export calling
external @hey-api/openapi-ts defineConfig with an object argument. Evaluate its
output string/output.path using the existing bounded evaluator. Only relative,
repository-contained output directories are eligible. Require a plugin
array containing literal @hey-api/client-axios (string or name property).
Evaluate relevant fields only; unrelated SDK callback settings are not executed.
A scanned output/client.gen.ts must export const client initialized by
createClient imported from a scanned module under that output directory.
Shared TypeScript import/export resolution supplies cross-file proof.

Within functions, methods and arrows, recognize proven-client verb requests and
an immediate parameter's client nullish fallback to that client. CONNECT, DELETE,
GET, HEAD, OPTIONS, PATCH, POST, PUT and TRACE follow the inventoried transport.
Type arguments are transparent. An object argument needs exactly one own URL
evaluating to a single string. Earlier spreads cannot replace it; a later
immediate parameter spread uses the investigation's proof that generated Options excludes
url and every Data type pins its URL. Other later spreads remain dynamic.
Only whole {name} path segments are parameters; embedded, explode, label and
matrix forms remain dynamic. Preserve slash flags. Unsupported requests and
own client overrides remain visible as dynamic client_call nodes with library
hey-api and a verb-derived method when available.

Per-call own baseURL wins. Otherwise merge the initializer configuration with
the repository's setConfig calls. Zero base-setting calls preserve the initial
base; exactly one unconditional production module-level object call with an
own baseURL and no opaque properties/spreads replaces it. A plain object with
no baseURL preserves it. Multiple base-setting calls, conditional/test/function
calls, opaque arguments/spreads, duplicate base keys and direct configuration
mutations make the base unresolved. Evaluate base strings with the existing
bounded evaluator; environment bases stay unresolved. Existing clients.base_paths
can repair unresolved bases. Joining follows getUrl's leading slash and direct
base-plus-path concatenation, treating absolute URL spellings as paths rather
than using axios's base joining or absolute-URL bypass.

App invocations produce no extra client_call nodes. Emit info
generated_client_override for literal options with client/baseURL/url keys,
naming the site and SDK method without adding general callable edges. Emit info
generated_client_absent when a proven transport configuration has no scanned
output files, with the scanner-supported ignore-negation remedy and its limits.
Emit info generated_client_unproven for generated-header files outside proven
output directories. Comments supply diagnostics only; client-fetch is unproven.

Reuse detached expression, scope, call and binding records. Extend cached syntax
for config arrays, URL object entry order/duplicates/spreads, setConfig
scope/conditions and mutations; invalidate with extractors-15. Route composition,
wrapper rules, matching tiers/confidences, identities, contract v1, persistence
interfaces/schema and dependencies remain unchanged.

## Consequences

Config, import, base and scope edits rebuild proof on cache replay. The corpus's
test-scope setConfig means its base is unresolved; exact matching still precedes
suffix matching. An explicit empty frontend base changes the label to configured
and preserves all 23 targets/confidences. Five existing repository golden headers
change only the extractor version. The heyapi fixture has its own graph golden.
SQLite runs locally; Postgres and other platforms remain CI checks.

## Known gaps

- Requests issued during module evaluation before the setConfig module runs
  are not modeled.
- Nonliteral app options cannot be inspected for overrides. SDK links describe
  the default rather than specializing each application invocation.
- No type checker enforces the inventoried generated Options URL exclusion;
  opaque later spreads are deliberately rejected.
- General app-to-SDK static method calls remain unresolved: Member follows
  namespaces/external exports, not class static members. Barrel exports work.
- Other hey-api plugins, arbitrary runtime serializers/interceptors, general
  local dataflow and existing client/route matching gaps remain out of scope.

## Review notes (developer, 2026-10-19)

- Accepted. FastAPI template: all 23 sdk.gen.ts request sites link exact at
  0.9 to the routes in fastapi-template-api.txt; with clients.base_paths
  {frontend: ""} the same targets and confidences, base configured. Zero
  wrong links. Other corpora unchanged apart from the extractor version;
  self-index differences come only from the heyapi fixture.
- Revision 1: the task's original base rule preferred the client.gen.ts
  initializer, but setConfig merges over it at runtime; the investigation stopped on a
  /old vs /new counterexample, now a fixture case.
- The task's gate expected suffix 0.6. The existing matcher tries exact
  whole-path matching before the suffix tier even with an unresolved base,
  and these URLs carry the full /api/v1 path.
- The corpus base is unresolved because a test-scope setConfig exists in
  addition to the production one. Ignoring test-scope setConfig calls for
  production sites is a refinement to make only with corpus evidence.
- Timing against main (three pairs): fresh +5.02%, warm +7.07%.
- Gaps: app-to-SDK static class member calls do not resolve (0/18 in the
  corpus), so route callers stop at sdk.gen.ts until a resolver task lands;
  nonliteral app options are not inspected; requests during module
  evaluation before setConfig are not modeled; client-fetch is unproven;
  recognized request sites are still counted in unresolved_calls; the
  generated_client_absent remedy text is shown even when output was never
  generated.
