# ADR 0027 — Orval generated clients

**Status:** Accepted (developer review, 2026-10-19)
**Date:** 2026-10-19

## Context

The pinned React application imports generated Orval exports that are absent
from its checkout. T012e's source inventory proves 27 request intents against
19 scanned OpenAPI operations. Names alone cannot establish these links.

## Options considered

- Guess exports from import names: cannot prove the operation or transport.
- Execute Orval or application configuration: adds dependencies and execution
  beyond deterministic offline indexing.
- Model an explicitly bounded Orval 8 configuration and resolve invocation
  imports against predicted files: chosen by T012e D1–D4.

## Decision

Read scanned Orval defineConfig projects through the shared TypeScript resolver
and bounded evaluator. Require a local major-8 package declaration, supported
scanned OpenAPI input, tags-split/react-query/fetch output, approved settings
and a uniquely emitted mutator when configured. Scanned generated source takes
precedence. Rebuild models and invocation proofs every link pass.

Run OpenAPI declaration extraction before client recognition, preserving its
matching behavior. Count proven direct calls, generated hooks and query-options
calls passed directly to proven TanStack scheduling APIs. Exclude URL/key
functions and literal queryFn overrides. Wrong-file exports remain unmatched.
Emit source client_call nodes and inferred calls_operation edges at 0.9.
These edges are inferred because generated output is predicted from config and
spec rather than scanned, and may be stale.
The invocation's source span and hash establish provenance; generated file
locations are predictions, never invented source declarations.

The B2 URL-object subset admits module-level or function-local single-identifier
const bindings initialized by unshadowed new URL(input[, base]). Absolute http(s)
input supplies its own origin and path; absolute-path input takes its origin
from a literal base or leaves the host unknown. Other input stays dynamic.
Unconditional top-level protocol/host/hostname/port assignments in the binding's
own block, after its declaration and before the use, preserve the path. Only
host assignment clears an unknown base host; hostname or port alone cannot.
A host still unknown from the base makes the use dynamic. Serialization uses
.toString() or .href, directly or through one local const. Literal dot segments,
backslashes, whitespace, non-ASCII and invalid percent escapes, other prior
references, escapes, other writes and conditional/loop assignments make the use
dynamic.
If any authority component was assigned, retain the path with origin null and
origin_resolution unresolved. Such paths use only the exact tier at 0.7, never
suffix matching. Unassigned literal origins retain the existing rules.

Prove mutator transport only when its first URL and second options parameters
are plain identifiers and never reassigned. Require exactly one recognized
global fetch site, excluding nested functions, reached through top-level
statements consisting only of const declarations and the URL assignments above.
Bind the URL parameter to the predicted generated URL, preserving dynamic
parameter names and truncating query strings. Fetch init must be the options
parameter itself or an object whose only spread is that parameter, first, with
no own method key; this preserves the operation verb. Without a mutator, apply
the existing global-fetch rules to the predicted URL. Proven paths use the
unchanged client matcher. calls_route evidence includes the application call
and mutator fetch spans; the inner fetch receives status wrapper when at least
one Orval node uses it. Unproven transport retains the dynamic B1 path and
calls_operation only. Wrong-file mismatch nodes have wrapper null and make no
transport claim.

New-expression syntax for QueryClient proof, per-use URL-object guards and
compact mutator transport prefixes survive replay under
extractors-17. Existing clients, contracts, persistence and dependencies remain
unchanged. One stat per configured output directory distinguishes absent
output from excluded output. Existing ignore-source bytes supply diagnostic
provenance; no generated files are read outside the scanned inventory.

## Consequences

Metadata/spec edits relink unchanged callers. Ignored output diagnostics name
the responsible rule source and retain the existing negation remedy; nonexistent
directories have no ignore remedy. B1 alone retains dynamic paths while making
the declared operation available.

## Known gaps

Barrels and require do not establish predicted generated exports. Nonliteral
options and spread options are not inspected for queryFn overrides. Only the
explicit task subset of Orval configuration and version ranges is modeled.
No general type checker, runtime execution or arbitrary transport inference is
provided. Unknown base hosts, unsupported URL writes, escapes and dataflow
outside the bounded subset remain dynamic.

## Review notes (developer, 2026-10-19)

- Accepted. React alone: 27 Orval client_call nodes (18 direct, 9
  query_options, 0 hooks), each with one inferred calls_operation edge at 0.9;
  all 19 operations reached; templates equal realworld-react-calls.txt
  exactly; unmatched without a backend. The seven getGetCurrentUserQueryKey
  calls produce no nodes. React + Express, React + Axum and React + Axum DDD:
  27/27 calls_route exact at 0.7 (Express favorite/unfavorite to the first
  registrations); OpenAPI results unchanged (38/38, 19/19, 19/19).
- The URL-object subset changed no existing link in any corpus. The three
  Langflow memories url.toString() sites stay dynamic (searchParams use and
  unknown base host). React's apiFetch fetch site becomes wrapper.
- Revision 1: the task's G2 expected three getGetCurrentUserQueryKey uses;
  the checkout has seven across six files (assistant counting error).
- Revision 2: generated path parameters keep their names ({slug}, not {});
  unmodeled operations use generated_client_operation_unmodeled; mismatch
  nodes carry no wrapper; B2 moved under Decision. Codex had also rewritten
  extractors-16 to -17 in ADR 0026 and in T012f task records; those edits were
  reverted before the checkpoint.
- Timing against main (two pairs, spread 2.02%): fresh -0.17%, warm +1.19%.
- Gaps: barrels and require; nonliteral or spread options are not inspected
  for queryFn overrides; only Orval 8 tags-split/react-query/fetch is modeled
  (other clients, modes, axios httpClient, mutation hooks and suspense or
  infinite variants are not); operations with several request content types
  or without operationId are not modeled; the model assumes orval runs from
  the config directory (no --config from elsewhere); recognized invocation
  sites still count in unresolved_calls, and absent generated files still
  produce unresolved_import warnings; URL objects outside the bounded subset
  stay dynamic.
