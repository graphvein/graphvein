# ADR 0011 — TypeScript/JavaScript object-literal owners and repeated declarations

**Status:** Accepted (developer review, 2026-10-08)
**Date:** 2026-10-08

## Context

T010a revision 2 decides the producer identity policy after the completed the investigation
diagnosis found 26 colliding TypeScript/JavaScript identities in pinned Langflow.
Separate anonymous callbacks lose their scopes, and object-literal members lose
their declared object owners. The 65 warm node writes are intermediate writes:
last-valid-record-wins already holds and the warm store equals a fresh snapshot.
Store behavior is unchanged by this task.

## Options considered

- **Declaration-order suffixes (chosen):** retain all declarations, follow the
  existing Go init/Python/Rust exceptions, and survive body edits and preceding
  line movement. Inserting or reordering declarations can change their suffixes.
- **Scope names from call context (rejected):** test titles or hook names are
  unstable, framework-specific, and still require a suffix fallback.
- **Drop declarations inside anonymous scopes (rejected):** removes real
  closures, including the helpers inside `useSpanTree` callbacks.
- **Line-number or body-hash discriminators (forbidden by contract v1):**
  unrelated line movement or body edits would change node identity.

## Decision

For TypeScript, TSX and JavaScript, a direct object-literal initializer of a
declared variable (`const`, `let`, `var`, exported or not) adds the variable name
as a lexical owner. Nested literals under identifier/string-keyed properties
add their keys. Computed keys and unbound literals (arguments, returns, array
elements and spreads) add no segment. Existing member node kinds are preserved.
The nearest emitted lexical owner contains a member: the variable if emitted,
otherwise the enclosing emitted named declaration, and the file only when no
such declaration exists.
Currently exported object variables are emitted; local object variables are not.

Anonymous functions/callbacks still add no owner. After all semantic naming,
including overloads and object owners, count each unsuffixed identity per file
in source declaration order. Keep the first plain name and append `@2`, `@3`,
and so on for later declarations. Containment, calls and evidence reference the
resulting identities. Calls to repeated callable groups remain unresolved rather
than selecting a different body. Cache these ambiguity groups as internal
extraction metadata, so warm replay and imported bindings use the same policy.

Bump the aggregate extractor version to `extractors-6`. Go, Python and Rust
extraction semantics are unchanged. Contract v1, store interface, storage/schema,
dependencies and grammar assets are unchanged. This documents the explicit
TypeScript/JavaScript declaration-order exception alongside ADRs 0006 and 0008.

## Consequences

Every bound object-literal method's identity changes once when its owner is
added (unbound members retain their existing owners). Content-bound rationale,
dispositions and caches for changed identities will require reassociation.
Every language's extraction cache is invalidated by the aggregate version bump.
Distinct declarations are preserved, eliminating these producer collisions
without changing duplicate-key ingestion semantics.

## Known gaps

Inserting/reordering repeated declarations can change which body receives each
suffix. Anonymous lexical bindings are not modeled, so repeated helper calls
are conservatively unresolved. Resolution remains syntactic and best effort.
External producers can still emit duplicate keys and cause intermediate graph
writes on unchanged replay; last-valid-record-wins remains in force. Postgres
and cross-platform execution need CI evidence; this sandbox cannot reach
Postgres or the network and cannot write the user compilation cache.

## Review notes (developer, 2026-10-08)

- Accepted including the T010a revision 3 containment rule: the nearest
  emitted lexical owner contains object-literal members.
- Calls to repeated helpers stay unresolved. In Langflow these are almost
  all test helpers and closures. If M2 tracing loses links through
  production closures (for example the useSpanTree helpers), resolve calls
  through anonymous lexical bindings in a later task.
- Suffix reassignment on reorder can move rationale to another body. The
  identity + content-hash binding marks it "needs re-review" when the body
  differs, so a reorder cannot silently misattribute accepted rationale.

## Revision: call binding model (T046)

**Status:** Accepted (delegated review, 2026-11-25)

Context: the T044 review found that the "Anonymous lexical bindings are not
modeled" gap above produced wrong `calls` edges, stored as facts: a
parameter, destructured name, `catch` parameter or block binding of an
anonymous function, arrow or block did not shadow an outer function of the
same name, and a function declared inside an anonymous function was
reachable from outside it by qualified name.

Options considered:

- Extend the qualified-name shadow sets to anonymous scopes (rejected:
  qualified names merge scopes that are distinct lexically, so a block or
  callback binding would shadow its whole named function and drop correct
  edges, as T034 found on owncast).
- A lexical scope model built during extraction, checked against what link
  time resolves (chosen).
- Resolve through the lexical model at link time (rejected for now: it
  changes which target a call gets, not only whether it gets one, and the
  cached facts would have to carry the scopes).

Decision: the extractor builds the file's lexical scopes by ECMAScript
rules (module, function, parameter, block, `for`, `catch`, class and static
block scopes; `var` hoisting to the function scope) and, for every call it
records, compares the name's nearest lexical binding at the site with the
declaration link time picks by qualified name. A mismatch, or a binding the
syntax does not fix (`with`, direct `eval`, `this` outside a class method,
two declarations of one name in one scope), marks the call shadowed: it stays
unresolved. The rows are TSX-1 to TSX-17 of
`docs/lang/typescript-resolution.md` ("Call binding model"). Link time,
contract v1, the store and the cached fact shape are unchanged; the
`Shadowed` flag of cached calls changes value, so the aggregate extractor
version must be bumped when this lands.

Consequences: no `calls` edge whose callee's lexical binding is another
declaration; edges whose binding is undecidable are dropped too (a missing
edge, never a wrong one). Block bindings no longer shadow their whole named
function, so some correct edges return.

Known gaps: link time still resolves by qualified name, so a call whose
lexical binding is a module function but whose qualified-name lookup would
pick a nearer method or nested declaration stays unresolved instead of
resolving to the module function. Sloppy-mode block function declarations
(Annex B) are treated as block scoped; calls outside the block stay
unresolved. Static class member calls (ADR 0026) and the bridge rows keep
their own shadow models.

## Revision: written bindings, member ownership and lexical retargeting (T046 review fixes)

**Status:** Accepted (delegated review, 2026-11-25)

Context: the review of the call binding model found three forms that still
stored wrong `calls` edges, all pre-existing: a `let`/`var` function binding
reassigned after its declaration resolved to its initial value; `this.m()`
and `C.m()` ignored whether `m` is static; and two object-literal owners of
one qualified name in sibling scopes shared their members. It also counted
508 corpus call sites whose lexical binding is a single, never-written
function declaration or import, left unresolved only because the
qualified-name lookup of link time picks a nearer class or object method of
the same name.

Options considered:

- Keep link time unchanged and only mark more calls shadowed (rejected for
  the 508 sites: the binding is exactly what the call evaluates, so the
  missing edge is avoidable).
- Carry the whole scope model in the cached facts and resolve every call
  lexically at link time (rejected: a larger cache change for the same
  result; the extractor already knows the binding).
- Record, per call, the declaration its lexical binding names when link
  time would pick another one, and let link time use it (chosen).

Decision: this revises the T046 revision's "never retargets" decision.

- A binding written after its declaration (assignment, compound or logical
  assignment, `++`/`--`, destructuring assignment, `for (x in/of ...)`
  target, or a direct `eval` it is visible from) no longer resolves; `const`
  declarators, imports and the unwritable own names of classes and named
  function expressions are unaffected (TSX-19).
- `this.m()` and `C.m()` resolve only to an own method of the bound class
  with the matching staticness; `x.m()` only to an own method of the bound
  class or object literal itself, identified by its declaration, not by its
  qualified name (accessors, overridden keys, computed keys and a
  function's nested declarations are not own methods) (TSX-20, TSX-21).
- When the lexical binding is a never-written function declaration or
  function-valued declarator and link time's qualified-name pick is another
  declaration, the cached call carries the binding's qualified name
  (`facts.Call.Lexical`) and link time resolves to it (TSX-22), also when
  the qualified name is repeated in another scope and the pick is a nearer
  method (the repeated rule above exists because a qualified name cannot
  tell the declarations apart; the binding does, and two declarations of
  one scope are TSX-16). When the pick is another function of a repeated
  name, the repeated rule still applies and the call stays unresolved
  (TSX-24); lifting it there is left to a later decision. When it is
  an import, the call is marked `LexicalImport` and link time resolves it
  through the module resolver only (TSX-23).
- A named function expression no declarator binds resolves calls of its own
  name to itself (TSX-18; the row text of TSX-10 now names only the
  declarator-bound case).

Consequences: the cached fact shape of TypeScript/JavaScript calls gains
two optional fields; contract v1 and the store are unchanged. Cached
`Shadowed` values change and the new fields appear, so the aggregate
extractor version must be bumped when this lands. Edges are retargeted
only to the declaration the scope model proves the call evaluates; every
other disagreement stays unresolved.

Known gaps: writes through `arguments`, another script sharing a sloppy
global or prototype patching are not seen; member writes (`o.m = f`,
`delete o.m`) do not block `o.m()` (ADR 0026 blocks them for static class
calls only); virtual dispatch through `this` still resolves to the class's
own method even when a subclass overrides it.

## Revision: member writes and the repeated rule (T046 review fixes, round 3)

**Status:** Accepted (delegated review, 2026-11-25)

Context: the previous revision's known gaps named member writes: a call
`this.m()`, `C.m()` or `o.m()` resolved to the own method `m` even when
code of the file assigned, deleted or redefined that member, so the call
could reach another value (one corpus site: a plugin class whose
constructor deletes the method's own-property copy after a mixin set it).
The same revision kept the repeated rule for lexically bound calls whose
link-time pick is another function of a repeated name (TSX-24) and left
lifting it open.

Options considered:

- Ignore member writes, as before (rejected: the edge is stored as fact
  although the syntax shows the member can hold another value).
- Track member values (rejected: what a write stores is generally
  unknown; a mixin or wrapper may keep or replace the method).
- Treat any write of the member through the owner as making the member's
  calls unresolved, with the one form whose value is known kept (chosen).

Decision:

- A write of member `m` of an object owner, or of a class on the side
  the call reads (its instances and prototype for an instance method, the
  class itself for a static method), makes `this.m()`, `C.m()` and
  `o.m()` calls that TSX-13, TSX-7 or TSX-9 would resolve unresolved
  (TSX-25). Writes are assignments, compound and logical assignments,
  `++`/`--`, destructuring and `for (x in/of ...)` targets, `delete`, and
  `Object.defineProperty`, `Reflect.defineProperty`, `Reflect.set` and
  `Reflect.deleteProperty` with that literal key. Receivers are `this` in
  the class's methods, static blocks and field initializers (a field
  initializer counts for both sides), the class or object binding,
  `C.prototype`, an instance's `constructor`, and plain declarators
  holding `this`, a binding or `new C(...)`. A computed or non-literal
  key, `Object.assign` and `Object.defineProperties` on such a receiver
  write every member of that side; a write on a member's key path
  (`o.nested = ...`) writes the members under it. An instance write does
  not touch a static method of the same name, nor a class write an
  instance method: the first corpus run without sides dropped 112 right
  static calls (class constructors that `Object.assign(this, init)` or
  set `this.from`, beside a static `from`).
- The static member resolver of ADR 0026 does not resolve a TSX-25 call
  either: the cached call carries `facts.Call.MemberWritten`, and link
  time skips that fallback for it (that resolver's own mutation scan does
  not see every receiver form above, for example `this.constructor.m`).
- `this.m = this.m.bind(this, ...)` and `C.m = C.m.bind(C, ...)` are not
  writes: the member keeps a function that calls the method.
- The repeated rule stays for TSX-24 calls. A repeated qualified name is
  one stored identity for several declarations, so an edge to it cannot
  say which declaration it means; lifting the rule needs distinct
  identities per declaration, an after-v1 identity item.

Consequences: fewer `this.m()`/`C.m()`/`o.m()` edges where a member is
written (each removed edge's target may not be what the call reaches).
Cached call facts change (`Shadowed` values and the optional
`MemberWritten` field), so the aggregate extractor version must be bumped
when this lands; contract v1 and the store are unchanged.

Known gaps: member writes through receivers the model does not follow are
not seen (a mixin that receives the object as an argument, `this` in a
plain function or object-literal method, another file, a subclass's
instances, `Object.setPrototypeOf`). TSX-24 keeps about 2,440 corpus call
sites unresolved (measured in the previous round, mostly owncast minified
bundles whose module functions repeat one qualified name across webpack
wrappers): a missing edge, not a wrong one, until declarations get
distinct identities.

## Revision: Object.assign with literal keys (T046 review fixes, round 4)

**Status:** Accepted (delegated review, 2026-11-25)

Context: the previous revision counts every `Object.assign(x, ...)` on a
TSX-25 receiver as a write of every member of that side. A constructor
that copies its arguments, `Object.assign(this, { root, base, head })`,
then leaves every `this.m()` of the class unresolved although only three
known keys are written (26 sites in one corpus).

Options considered:

- Keep "every member" for all `Object.assign` (simplest; loses those
  right edges).
- Evaluate the sources' own enumerable keys in general (needs values the
  model does not track: names, call results, spreads).
- Write exactly the keys of the sources when every source is an object
  literal whose entries all have a static key, and keep "every member"
  for every other form (chosen).

Decision: `Object.assign(x, s1, s2, ...)` on a TSX-25 receiver writes
exactly the keys of its sources when there is at least one source and
every source (after parentheses and type assertions) is an object literal
whose every entry is a shorthand property `a`, a pair keyed by an
identifier or a string literal (`b: v`, `"b": v`), or a pair under a
computed string literal (`["b"]: v`), and no key is `__proto__` (copying
an own `__proto__` property may replace the target's prototype, so every
member may change). Any other form writes every member of that side, as
before: no source, a source that is no object literal (a name, a call),
a spread argument, a spread entry, a method or accessor entry, a numeric
key, a computed key that is not a string literal. The keys extend the
receiver's key path (`Object.assign(this.opts, { a })` writes `opts.a`;
`Object.assign(C.prototype, { a })` writes the instances' `a`).

Consequences: a `this.m()`/`C.m()`/`o.m()` call keeps its edge when an
`Object.assign` of literal keys does not name `m`. Methods, accessors and
numeric keys in a source still count as unknown keys although their keys
are static: a deliberately narrow first step, so the model stays a
subset of the oracle's ECMAScript semantics. Cached call facts change for
such files, so the aggregate extractor version bump noted in the previous
revision covers this too.

Known gaps: sources held in a name (`const patch = { a }; Object.assign(
this, patch)`) still write every member. The static member resolver of
ADR 0026 keeps its own mutation scan, which still counts every
`Object.assign` as a write of every member (more conservative; it is only
a fallback when this model leaves a call to link time unresolved).

## Revision: receivers of unknown owner (T046 review fixes, round 5)

**Status:** Accepted (delegated review, 2026-11-25)

Context: the round-3 revision counts member writes only through receivers
it follows (`this` of a class, a class or object binding, a declarator
holding `this`, a name or `new C()`). A re-check found wrong edges through
receivers it does not follow, all already on main: a declarator holding a
class expression (`const CE = class Inner {...}; CE.m = f`), `this` in an
object-literal method, an alias of a member path (`const proto =
P.prototype; proto.m = f`), a destructured name, and a `let` assigned
`this` after its declaration. The earlier known gaps listed some of these
(a mixin parameter, `this` in a plain function or object method).

Options considered:

- Follow each form (member-path aliases, destructuring, later
  assignments, object-method `this`): every new form needs its own case,
  and parameters, imports and call results stay open.
- Treat a write whose receiver's owner is not determined as a write that
  may reach any owner, by property name (chosen): one class rule, a
  missing edge where the model cannot tell, never a wrong one.

Decision: a declarator whose value is a class expression is an alias of
that class (the class; through `prototype`, its instances), so writes
through it count under TSX-25. New row TSX-26: a member write (the TSX-25
write forms) whose written object is not a determined receiver (a
parameter, a destructured name, an import, a global, a written name, a
member-path or call alias, `this` outside a class, a call result,
`super`, or any value reached through a key, `this.cfg.m = f`) leaves
every TSX-7, TSX-9 or TSX-13 call of the file unresolved whose key path
has the written property's name; a non-static key there leaves every such
call of the file unresolved. A receiver known to hold no owner (a fresh
literal, `Object.assign({}, ...)`, a never-written function binding)
blocks nothing. A name written after its declaration is no longer an
alias (its writes are of unknown owner). TSX-26 calls are marked like
TSX-25 ones, so the ADR 0026 resolver does not resolve them either;
the file's written names are cached with its facts
(`MemberWrittenNames`), so every static call that resolver adds in the
file follows the same rule: module-level calls, calls the binding model
leaves to it (a class name repeated in sibling scopes, TSX-21), and calls
of an imported class (the receiver may hold it).

Consequences: precision over recall: files that write a computed key on a
parameter (`target[key] = value`) or on a value a field holds
(`this.cache[key] = value`) lose every member-call edge. The corpus
counts are in the T046 task record (internal). Cached call facts change,
covered by the pending aggregate extractor version bump.

Known gaps: dispatch is static (TSX-13): `this.render()` in `Base.run`
resolves to `Base.render` even when a subclass of the file overrides
`render`; the edge holds for `Base` instances and no edge to the override
is emitted (a missing edge for subclass instances). Writes in another
file, `Object.setPrototypeOf`, and calls through an import's members
(`api.m()` after `api.m = f`) are not covered. A class/namespace merge
inside the class bypasses TSX-16 (`tsc` rejects such programs).

## Revision: member calls through imports (T046 gate preparation)

**Status:** Accepted (delegated review, 2026-11-25)

Context: the previous revision left member calls through an import open:
`import * as api from "./api"; api.m = f; api.m()` resolved to the
export `m` through the module resolver (TSX-11), although the file
replaces it (in CommonJS output and with bundlers a namespace object is
writable; a default or named import may hold any object).

Options considered:

- Block only writes through the same import binding: misses an import
  reached through another name (a default import, a require binding, a
  parameter holding the namespace).
- Treat the import binding like every other receiver of unknown owner
  (chosen): a member call through an import whose key path has a name the
  file writes through any receiver of unknown owner stays unresolved; a
  non-static key there leaves every such call unresolved.

Decision: row TSX-26 also decides member calls through an import binding
(`api.m()`, `api.a.m()`, TSX-11 and TSX-23). Receivers of unknown owner
include imports (namespace, default, named), require bindings
(`const api = require(...)`, a declarator holding a call) and
destructured requires (`const { a } = require(...)`); the write forms are
those of TSX-25 (assignment, compound or logical assignment, `delete`,
`Object.defineProperty` with a literal key, ...). Such calls are marked
like the other TSX-26 calls, so the ADR 0026 resolver does not resolve
them either.

Consequences: more missing edges in files that patch a module's exports
or write a computed key on a parameter; never a wrong edge for the forms
listed. Cached call facts change, covered by the aggregate extractor
version bump at merge.

Known gaps: writes in another file are not seen: a module that patches
another module's export (`import * as api from "./api"; api.m = f` in
`patch.js`) leaves `api.m()` in every other file resolved to the export.
A plain call through a named import (`import { m } from "./api"; m()`)
is not blocked by a write of `m` through the namespace in the same file;
it reads the module's binding, which such a write only replaces where
the module is compiled to CommonJS with live member access.

## Review notes (delegated review, 2026-11-25, T046)

Accepted with the six T046 revisions above after gate 1 (CI run 45612588023: changes and postgres green; postgres log 0 FAIL, 254 `/postgres` PASS, only opt-in oracle, census, corpus and generator skips; Windows runs on the final CI gate). Aggregate extractor version extractors-61.

- **Results.** The oracle (acorn 8.14.0 + eslint-scope 8.2.0, @typescript-eslint 8.74.0 + typescript 5.9.3, the T046 oracle script (internal)) compares every resolved call with the language's own scope analysis on 12 corpora. Main emitted 605 wrong call edges before round 1. Round 1 removed them; round 2 (A1-A3, written bindings) removed 154 more (owncast 120, seerr 27, langflow 6, kaneo 1) and regained 41; round 3 (member writes, TSX-25) removed 1,423 more (owncast 1,329) and regained 989; round 4 (literal `Object.assign` keys) regained 26 kaneo sites with none lost; round 5 (receivers of unknown owner, TSX-26) removed 3,766 more (owncast 3,683, seerr 67, langflow 11, angular-ngmodule 3, dozzle 2) and 6 right seerr sites; the gate round (imports) changed no corpus site. Final: 0 wrong and 0 undecidable on all 12 corpora; fixture 61/61 right. Langflow indexing +1.2% to +2.9% across rounds (gate +10%).
- **Review.** One independent review and two focused re-checks (each fed the harness hand-made wrong edges, all flagged); every finding was fixed as a class rule (TSX-18..26), not a case patch, and the rounds converged (narrower findings each time, all pre-existing on main).
- **Contract.** `docs/lang/typescript-resolution.md` rows TSX-1..26 are kept 1:1 with the code by `TestCallBindingContract`, which checks each fixture call's outcome per row.
- **Known gaps (after v1).** Repeated qualified names stay unresolved (TSX-24; about 2,440 oracle-right sites, mostly minified bundles; needs per-declaration identity). Subclass overrides get no dispatch edges (`Base.run -> Base.render` holds for base instances). Writes from another file (cross-module monkey-patching) and a named-import call `m()` after `api.m = f` are not seen (developer decision 2026-11-25: recorded gaps, listed in `docs/known-gaps.md`). Writes through mixin arguments, `Object.setPrototypeOf` or `this` in plain functions are not seen. Files writing `target[key] = value` through an unknown receiver lose all member-call edges (recall cost of TSX-26). The harness is over-strict on overload signatures (safe direction).
