# ADR 0031 — Angular declaration metadata and scopes

**Status:** Accepted (developer review, 2026-10-25)
**Date:** 2026-10-25

## Context

ADR 0030 records syntax without Angular proof. T014c1 needs class roles,
template resources and NgModule scopes rebuilt after configuration/import edits.
Existing HTTP client facts and identities must survive. The ten-checkout
inventory is in the T014c1 task records (internal); a private corpus contributes
aggregates only.

## Options considered

- Assign roles during parsing: caches resolver/version decisions and goes stale.
- Broaden shared URL expression meanings: risks changing existing consumers.
- Add separate declaration syntax and a link-time Angular package: chosen.

## Decision

Add `internal/extract/angular`. Direct non-type-only imports whose specifier is
exactly `@angular/core` need ADR 0014 external-package proof. Aliases and
namespaces qualify. Barrels are indirect; scanned core packages block roles.
Conflicting primary decorators yield only null role and conflict markers.

Add a versioned `declarations` payload beside `template_syntax`: original
metadata wrapper kinds, ordered array elements/spans, function returns, module
const arrays and mutation receivers. Existing fields keep their meanings.
Capture a mutation receiver only when its root identifier, followed through
member/subscript access, is unshadowed and names an import binding or a
module-level single-declarator const in that file. Other receivers cannot
affect the const arrays or namespaces used by declaration evaluation.
Require complete payload arrays on replay; invalidate with extractors-21.
Contract v1, dependencies, identities, store interfaces and schemas are unchanged.

Expose only existing ADR 0020 evaluation and ignore-provenance lookups to the
new package. Read project/package/lock inputs at link time through the existing
JSONC parser. Use ADR 0022's deepest sourceRoot/root ownership rule and the
owning angular.json's nearest declaring package. Unowned files start at their
directory. Matching core/compiler-cli majors determine the implicit standalone
value at Angular 19. Disagreeing co-located lock versions are unknown.
Only an absent standalone key uses that default. An explicit unknown or
wrong-type value stays null with source unknown, without a version lookup or
version diagnostic. The optional sorted `unknown_keys` array names present
selector, exportAs, standalone, name, pure and templateUrl keys whose evaluated
values are unknown or have the wrong type; scalar diagnostics are unchanged.

Apply class attributes as snapshot overlays after source facts are cached.
New edges reuse class, file and package-level external identities. Resolve
imports/barrels, const arrays, forwardRef and external ModuleWithProviders;
reject mutations/cycles and preserve contributing source spans. Scope counts
partition flattened leaves into local linked, external, unresolved and invalid.
Edge deduplication does not reduce entry counts.
Invalid scope entries require a proven class declaration with a rejected role
or standalone value. Conflicted classes and non-class identities are unresolved;
the latter report "target is not a class declaration", including aliases and
unproven const arrays. A directive with an unknown selector is unresolved as a
declaration target; only an absent or empty selector proves it selectorless.

Suppress duplicate declarations among distinct modules only within the same
ADR 0021 production/test scope. Traversal behavior is unchanged. Host capture
panics are diagnosed every failing run, preserving graph facts and forcing retry.

## Consequences

Metadata and template edits relink unchanged TypeScript. Uncertain metadata
remains visible without guessed facts. Declaration and scanned-core-shadow
fixtures have goldens and independent assertions; the storage round-trip case
is registered for both SQLite and Postgres.

## Known gaps

Template matching, transitive exports, DI edges, router facts, providers,
bootstrap, schemas and host directives remain deferred. Non-call decorators
remain outside ADR 0030 capture. Arbitrary expressions, local ModuleWithProviders
and unsupported function bodies remain unresolved. Existing code has no yarn
or pnpm lock parser, so neither is added.

## Review notes (developer, 2026-10-25)

- Accepted. Results by checkout:
  - angular: roles Component 18, Directive 1, Pipe 2, Injectable 6,
    NgModule 0; all 21 component/directive/pipe classes standalone true by
    default (21.2.12, bun.lock); 10 uses_template, 8 inline templates parsed;
    27 local and 24 external scope_imports edges (31 external entries);
    0 Angular diagnostics.
  - angular-ngmodule: roles NgModule 16, Component 18, Directive 1, Pipe 1,
    Injectable 14; 20 classes standalone false by default (15.2.3,
    package-lock v2); 18 uses_template; 20 declares; scope_imports 17 local
    and 12 external (14 external entries; @angular/common/http shares the
    @angular/common package node); scope_exports 7 local and 10 external
    (11 external entries); 0 Angular diagnostics. Every edge matches the
    source binding.
  - The other eight corpora are byte-identical apart from the header. The
    Angular + Express composite keeps 24 clients: 21 linked, 3 wrappers,
    0 ambiguous.
  - Postgres CI: TestAngularDeclarationsRoundTrip passes for both fixtures.
- Revision 1:
  - Present but unevaluable or wrong-type metadata never takes a default. The
    first implementation applied the version default to an unknown explicit
    standalone, which could create or drop declares edges.
  - unknown_keys separates unknown from absent values; T014c3 must read it.
  - Only proven class targets can be invalid scope entries.
  - Mutation capture is limited to import and module-const roots: the Langflow
    store grew +0.28% instead of +2.19%.
- Timing against main, three interleaved Langflow pairs (medians): fresh index
  -1.06%, fresh ingest +1.83%, warm index -3.63%, warm ingest -0.17%.
- Conservative deviations:
  - External entries in declarations are info unresolved; Angular rejects them
    as invalid declarations.
  - A standalone-true directive with an unknown selector in declarations is
    unresolved, although provably invalid.
- Gaps:
  - Edge evidence includes enclosing array and expansion spans, which sort
    before the entry span.
  - Mutation through local aliases, Object.assign or Array.prototype calls is
    not detected (entries can be missed; no wrong edge).
  - Co-located lockfiles are compared on full versions, not majors.
  - Dynamic namespace writes mark every export of the namespace.
  - Decorators written without a call are not captured.
  - yarn and pnpm lockfiles are not parsed.
  - Bootstrap, providers, schemas and host directives are not modeled.
- Rules written from memory, because their sources were not in the October 2026
  bundle, were made conservative:
  - reflectObjectLiteral (spreads, computed keys, duplicates);
  - getImportOfIdentifier (direct specifier text);
  - trait-compiler handling of multiple primary decorators;
  - the CLI resource loader for rooted templateUrl.
  Verify them in October 2026 from the fetched sources.

## Review notes (developer, verification, 2026-10-25)

Checked against v21.2.12 reflection/typescript.ts, transform/compilation.ts
and annotations/src/injectable.ts, plus the 15.2.3 diffs.

- **reflectObjectLiteral.** It reads own property assignments (identifier,
  string and numeric keys) and shorthands; the last key wins. It silently
  ignores spreads, computed keys, methods and accessors. "partial" (no
  evaluation) is conservative: it loses coverage but never gives a wrong fact.
  Align with Angular only with corpus evidence.
- **getImportOfIdentifier.** It uses import specifier text only, keeps the
  imported name through aliases and resolves ns.Name through a namespace
  import; barrels are not Angular. Graphvein's extra external-package proof
  is conservative.
- **Trait compiler.**
  - Two different primary decorators cause DECORATOR_COLLISION and no traits,
    as implemented.
  - A repeated identical primary decorator is detected once (each handler
    runs once and findAngularDecorator takes the first). Graphvein reports a
    conflict warning: no wrong edge, but the severity is overstated. Not
    changed.
- **@Injectable argument.** A single argument must be an object literal with
  no unwrapping. T014c1 accepted any single argument; T014c2 corrects this
  (C1).
- **Rooted templateUrl.** The resource loader source was not available. The
  unresolved rule gives no wrong fact either way; verify it with the T014c3
  sources.

## Review notes (developer, verification, 2026-10-26)

Checked against v21.2.12 compiler-cli resource/src/loader.ts and its 15.2.3
diff (resolution logic unchanged).

- Rooted templateUrl: AdapterResourceLoader.resolve uses the compiler host's
  resourceNameToFileName when the host provides one, and fallbackResolve
  otherwise. The fallback resolves a URL starting with '/' against each
  tsconfig rootDirs entry (first existing file wins), not against the
  component directory or the repository root. The host override lives outside
  compiler-cli. The T014c1 rule (rooted URLs unresolved, info diagnostic)
  gives no wrong fact under either behaviour; unchanged.
- Relative templateUrl: the fallback prefixes './' only when the URL does not
  start with '.', then uses TypeScript's failed lookup location beside the
  containing file, so 'x.html' and './x.html' resolve next to the component,
  as T014c1 joins them.
- Correction C2 (T014c3): a URL starting with '.' but not './' or '../'
  (for example '.templates/x.html') is not prefixed and becomes a
  non-relative module lookup in the fallback, while a host override may join
  it to the component directory. T014c1 joins it; T014c3 makes it unresolved.
  Neither Angular corpus contains such a URL.
