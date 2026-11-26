# ADR 0014 — TypeScript/JavaScript module resolution

**Status:** Accepted (developer review, 2026-10-09)
**Date:** 2026-10-09

## Context

The T011b investigation found 154 Express export-star statements hiding all 24 handlers,
three workspace mounts, and frontend aliases in React, FastAPI and Langflow.
Every consumer needs the same source declaration, including cached callers.
Revision 1 stopped on literal .js priority; Revision 2 corrects that rule.
The completed inventory and baseline evidence are in the T011b task record (internal).

## Options considered

- A TypeScript compiler dependency: adds a runtime and dependency outside scope.
- Independent import, call and route resolvers: diverging precedence and ambiguity.
- Cache resolved exports or aliases: stale after metadata-only edits.
- Shared inventory-only resolution rebuilt at link time: chosen, offline and
  compatible with the existing source cache and contract.

## Decision

Implement the rules in [typescript-resolution.md](../lang/typescript-resolution.md).
One internal TypeScript resolver consumes current scanned source and metadata.
Cache only import/export syntax and local declarations, including relays,
namespace exports and export-star clauses. Bump the aggregate extractor version
to extractors-8. Relink import edges, callable bindings, ADR 0011 ambiguity
groups, route handlers and router mounts through the same resolver every run.

Explicit/local exports win over stars; stars exclude default. Distinct terminal
declarations make a star name ambiguous, including ambiguity propagated through
another barrel. Reaching the same declaration through multiple sources is valid.
Per-name cycle guards terminate resolution; do not memoize partial traversals.

Use TypeScript extension substitution even when the literal file exists.
Bare precedence is paths, baseUrl, workspace, external. A matched paths pattern
whose targets all fail skips baseUrl and continues package lookup, as TypeScript
5.3.3 does in Node10, Node16, NodeNext and Bundler. This corrects Revision 2's
fall-through-to-baseUrl assumption under the user's instruction to implement
other TypeScript contradictions and record the decision. Choose exact paths patterns first, then longest prefix/suffix.
Inherited relative options retain their declaring directory. JSONC parsing is
internal; invalid metadata is diagnosed and ignored without stopping other files.

Duplicate package names are unresolvable and diagnosed with every declaring path.
Try package exports, source/module/main/types, src/index, index; collect declaration
fallbacks after implementation candidates. Subpaths use exact string exports or
relative probing. Never rewrite dist/build/lib targets into source targets.

The following deliberate deviations from TypeScript are required by Revision 2:

- Implementation before declaration: .d.ts (and .d.mts/.d.cts) are last resorts;
  the types condition and field come last.
- The nonstandard source condition and field are honored first.
- Directory index resolution applies regardless of moduleResolution; the resolver
  accepts a permissive union of modes and supported package conditions.
- Only relative extends is followed; project references and include/files are
  ignored. Relative extends arrays follow TypeScript's left-to-right inheritance.
- Workspace packages come from scanned package.json names, without node_modules
  symlinks or any network access.

Other semantic corrections authorized by the user: exhaust file probes before
directory probes; preserve eligible condition property order (after source and
before types), rather than sorting import/default/require keys; a named default
declaration does not additionally export its declared name; retain TypeScript's
additional .jsx/.js and explicit source-extension fallbacks that the replacement
rule's abbreviated lists omit. Type-only export clauses never establish runtime
callable bindings. See Decisions in the
task summary for offline compiler evidence.

## Consequences

Metadata edits change links without reparsing unchanged sources. Resolver memo
tables live only for one link pass. Old per-file caches are invalidated for every
language by the existing aggregate version policy. Re-export statements gain
source-backed import edges. Ambiguous names never gain speculative call/handler
edges. Identities, contract v1, dependencies, store interface and storage stay
unchanged. This amends ADR 0006's lack of alias/re-export resolution and extends
ADR 0013's import resolution, without editing those accepted ADRs.

## Known gaps

No type checker, inheritance or arbitrary assignment inference. External star
exports have no scanned name inventory. Package export arrays/patterns and
conditional subpath exports are outside the task rules. Package-name extends,
project references and include/files selection remain unsupported deliberately.
CommonJS relative exports remain outside scope. Postgres and other OS execution
require CI; this task does not change persistence.

## Review notes (developer, 2026-10-09)

- Accepted. Express corpus: 24/24 handlers resolve through export * barrels
  (24 exposes_route edges); 0 route_mount_unresolved; 19/19 operations.
  Unresolved call sites fall in React (-34), FastAPI (-137) and Langflow
  (-1,404) with no regressions. Offline TypeScript 5.3.3 oracle: 40/40
  fixture specifiers agree.
- Codex corrected the developer's revision 2: a matched paths pattern
  whose targets all fail skips baseUrl (TypeScript behaviour in Node10,
  Node16, NodeNext and Bundler). Verified by the oracle and guarded by the
  fixture.
- Same-session interleaved Langflow timing: +3.1% against main.
- Remaining gaps (package-name extends, export arrays/patterns,
  conditional subpath exports, CommonJS relative exports) stay unresolved
  rather than guessed; revisit if T012 client-call linking loses links
  through them.
