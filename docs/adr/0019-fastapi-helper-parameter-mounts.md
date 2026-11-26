# ADR 0019 - FastAPI helper parameter mounts and slash preservation

**Status:** Accepted (developer review, 2026-10-11)
**Date:** 2026-10-11

## Context

T011f's source inventory finds an undecorated deployment include helper and two
test-app helpers in Langflow. Receiver or included-router parameters prevent
composition despite explicit resolved call sites. Normalization also hides
distinct FastAPI empty-pattern and slash-pattern registrations.

## Options considered

- Infer routers from parameter names or annotations: lacks object evidence.
- Execute Python or arbitrary helper evaluation: executes repository code and
  exceeds offline deterministic extraction.
- Bind one level of explicit uniquely resolved calls using cached syntax:
  chosen; keeps object evidence and bounded scope.

## Decision

Cache eligible module-function signatures and trimmed static call arguments in
Python facts. Bind explicit positional and keyword arguments with Python's
positional-only, keyword-only, defaults and variadic rules. Reject duplicate,
missing or otherwise invalid bindings and every actual argument unpack. Omitted
defaults, variadic containers and rebound parameters establish no object.
An explicitly awaited async helper can execute its body; an ordinary async call
cannot. Decorated functions, methods and nested functions are excluded.

Resolve supplied arguments with the shared current-inventory Python resolver;
annotations never prove FastAPI objects. Substitute proven objects only for
include_router/mount receivers and included router/app arguments. Never forward
an enclosing parameter or evaluate a parameter prefix. Function-local imports
already resolve through the shared resolver; no resolver behavior changes.

Instantiate each call separately; local helper objects get call-specific keys
to prevent mixing receiver/child pairs between calls. Existing source route
identities and registrations stay unchanged. Conditions combine body and call
site contexts using the existing list; ordinary helper bodies add no condition.
Existing deduplication and per-chain cycle guards remain.

FastAPI output entries add trailing_slash=true when the full concatenated raw
path ends with a slash and normalizes to something other than root. Omit false.
Deduplicate by template plus slash flag, independently merging conditions and
resolution strength; sort template, then absent flag before true. Express and
Axum stay unchanged. Client matching is unchanged pending the later task.

Increment aggregate extractor version from extractors-11 to extractors-12,
because cached Python syntax changes. Contract v1 attributes remain extensible;
no transport, store schema, dependency or source identity change is required.

## Consequences

Langflow's 18 deployment registrations gain feature-conditional paths; six
policy registrations also gain the actual test-app paths identified in the investigation.
Target-only edits relink unchanged helper bodies. Slash twins remain distinct
source identities and distinct composed entries. Baseline flags intentionally
change without rewriting route hashes or other source facts.

Real-semantics corrections to the task: positional-only names can be captured
by **kwargs if supplied separately positionally; declared variadics accept
extra explicit arguments. Bare async calls do not execute bodies. Current
FastAPI uses lazy includes (older versions copy registrations); all inventoried
affected routes are registered before inclusion, so both yield the same paths.
See the source references in the T011f task record (internal), the investigation.

## Known gaps

No default evaluation, argument unpack evaluation, type inference, arbitrary
control-flow execution, interprocedural parameter forwarding or registration on
a parameter receiver. Conditions represent syntactic contexts rather than flag
evaluation. Non-FastAPI ASGI mounts remain ignored. General include-order and
runtime launch configuration analysis are outside this bounded task.

## Review notes (developer, 2026-10-11)

- Accepted. Langflow: all 18 deployment routes compose under
  /api/v1/deployments, conditional on the feature flag, matching source;
  route_unmounted 21 -> 3, route_mount_unresolved 9 -> 7. No other route
  template, condition or resolution changed. FastAPI template unchanged apart
  from 8 trailing_slash flags; Express, Axum, axum-ddd, React and Angular are
  byte-identical. 23 Langflow slash twins are now distinguishable.
- One new Langflow client link (tests deployments-page.spec.ts:344 to
  GET /api/v1/deployments, exact, 0.9) is correct; the task's expectation of
  an unchanged distribution was wrong and was reported rather than hidden.
- Test-app roots: six policy routes gain unconditional /model-provider-policy
  paths from FastAPI apps created in test files. This follows the existing
  every-app-is-a-root rule (as do the existing /registration and /api/mcp
  entries), but clients could link to paths production never serves. Label or
  exclude roots defined in test files before the architecture view (M2).
- Timing: +9.14% median on warm cached runs (zero parsed files), close to the
  gate and measured warm rather than fresh. Likely cause: cached Python call
  arguments. T012d measures fresh and warm runs against post-T011f main.
- Open: defaults, forwarding, unpacking, registrations on parameter receivers;
  the Axum equivalent (ADR 0016 note); six **config api_route decorators
  (nine runtime method twins) still have no identities (ADR 0015 note).
