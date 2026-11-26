# TypeScript/JavaScript module resolution

ADR 0014 specifies inventory-only, best-effort resolution. The
application still consumes contract v1. Every link pass rereads scanned
package.json and tsconfig/jsconfig content; cached source facts contain syntax,
never resolved cross-file links. Aggregate extraction version: extractors-62 (61 from T046: the call
binding model's cached call flags and written member names; 62 changes
no TypeScript/JavaScript fact).

## Exports and bindings

Capture export-star, namespace-star, named export-from (including default), and
local clauses relaying imported bindings. Local and explicit exports override
stars even if their target is unavailable. Star exports exclude default and
resolve only if every available source agrees on one terminal declaration.
Distinct declarations are ambiguous; ambiguity propagates through barrels.
Per-name cycle guards terminate recursive paths. Namespace members resolve
through the same export lookup. Repeated callable groups remain ambiguous under
ADR 0011. Re-export statements emit imports edges to their resolved modules.

## Files and bare specifiers

Since the T020 implementation (ADR 0070, extractors-41) import resolution is
the TypeScript 6.0 compiler's, transcribed in Go over the scanned
inventory; the claim contract `docs/lang/typescript-structure.md` (rows
TS-C1..TS-C3 and TS-I1..TS-I7) is normative and supersedes the earlier
best-effort order of ADR 0014:

- The governing configuration is the compiler's: from the file's directory
  to the root, `tsconfig.json` then `jsconfig.json`, the first whose
  program (`files`, `include`, `exclude`, `allowJs`, `references`) contains
  the file; `extends` chains include package-name bases reached through a
  workspace link. An unread configuration (parse error, unresolvable
  `extends`, unknown option value) refuses bare specifiers and keeps only
  relative results every resolution kind agrees on.
- `moduleResolution` node10, node16, nodenext and bundler, the file's
  resolution mode, `paths`, `baseUrl`, `rootDirs`, extension substitution
  and probing, directory indexes, `package.json` `typings`/`types`/`main`,
  `exports` and `imports` (conditions, patterns, arrays) and self-name
  references follow the compiler. Declaration files are the compiler's
  result, except that an import whose implementation-only resolution
  (`noDtsResolution`) finds another file claims nothing.
- Workspace packages are only the declared members (package.json
  `workspaces`, pnpm-workspace.yaml `packages`), linked as
  `<root>/node_modules/<name>`; duplicate names link neither and emit
  `duplicate_workspace_package`. A non-member package.json is never a
  resolution target, and an import of its name claims nothing.
- `dist` (or any `outDir`) is mapped to source only where the member's own
  `tsconfig.json` proves it: through a link, a file that configuration
  would emit into its `outDir` or `declarationDir` (rootDir as computed by
  TypeScript 6.0) stands for its source file. There is no `src/index`
  fallback; an entry the compiler cannot find claims nothing
  (`workspace_entry_missing`).
- A bare name resolved nowhere is an `external:.#npm:<package>` node only
  when the package name is a valid npm name; aliases and virtual modules
  (`~icons`, `@/x`, `virtual:*`) claim nothing (`not_a_package`).
- Refusals are counted per file in the `ts_structure_unread` diagnostic;
  an unresolvable relative import keeps its `unresolved_import` warning.

## Call binding model

Link time resolves a call's callee by qualified names: the enclosing named
declarations outward, then the file's imports (`symbols` and `bindings` in
`LinkWithResolver`). Qualified names do not see anonymous functions,
blocks, parameters or most local bindings, so since T046 the extractor
also builds the file's lexical scopes (ECMAScript semantics: module,
function, parameter, block, `for`, `catch`, class and static-block scopes)
and checks every call recorded inside a named function: the name's nearest
lexical binding at the call site, searched from the site outward through
every scope up to the module, must be the declaration link time picks.
When it is another binding, or the syntax does not fix the binding, the
call is marked shadowed and stays unresolved (`unresolved_calls`): a
missing edge, never a wrong one. When the binding is a never-written
function or an import and link time would pick another declaration of the
name, the extractor hands link time the binding instead (TSX-22, TSX-23).
Bindings hold throughout their scope, before their declaration too; a
binding is written when any code of the file assigns it after its
declaration (TSX-19). The rows below are the binding forms; each
row names what a call through such a binding resolves to. Code:
`internal/lang/typescript/scopes.go`; fixture
`testdata/fixtures/ts-call-scopes` (a tag-to-row test keeps the table, the
code and the fixture 1:1).

| row | binding form | call through it |
|---|---|---|
| TSX-1 | `var` declarators (names and destructured names) and `for (var ... in/of ...)` heads, bound in the nearest function, static-block or module scope from any block | unresolved, unless it names a function (TSX-8) or object owner (TSX-9) |
| TSX-2 | `let` and `const` declarators, bound in the enclosing block, `for` head, `switch` body, function body or module | unresolved, unless it names a function (TSX-8) or object owner (TSX-9) |
| TSX-3 | a function or generator declaration, bound in the enclosing block, function body or module (also in sloppy-mode code: a call outside the block never resolves to it, see TSX-17), never written | the declaration, which link time picks by its qualified name (when it would pick another declaration: TSX-22) |
| TSX-4 | a name inside a destructuring pattern (object, array, shorthand, `key: name`, the left side of a default, rest) of a declarator, parameter or `catch` parameter | unresolved |
| TSX-5 | a parameter of any function, method or arrow, anonymous ones included, bound in the function's parameter scope; default values are read in that scope (they see earlier parameters, never the body) | unresolved |
| TSX-6 | a `catch` parameter, bound in the clause | unresolved |
| TSX-7 | a class declaration, bound in the enclosing scope (written: TSX-19) and, unwritable, in the class's own scope; a class expression's name, in its own scope only | `C.m()` resolves to the member link time picks when it is a static own method `m` of that class (an instance method: TSX-20; anything else: TSX-21; a written member: TSX-25, TSX-26); `C()` itself is unresolved |
| TSX-8 | a declarator whose value is a function or arrow expression (the function's semantic name, row TS-D4): `const`, or `let`/`var` never written | the function, which link time picks by its qualified name (when it would pick another declaration: TSX-22) |
| TSX-9 | a declarator whose value is an object literal (the object owner of ADR 0011): `const`, or `let`/`var` never written | `o.m()` resolves to the method link time picks when it is an own method `m` of that object literal (anything else: TSX-21; a written member: TSX-25, TSX-26); `o()` itself is unresolved |
| TSX-10 | the own name of a named function expression that a declarator binds (`const c = function f() {}`), inside the function only | unresolved (the expression's identity is the declarator's name, not its own) |
| TSX-11 | an import binding (default, named, namespace), bound in the module scope, when no declaration of the file has the callee's qualified name | through the module resolver (a member call whose key the file writes: TSX-26) |
| TSX-12 | `import x = require(...)`, a namespace or module declaration's name and an enum, bound in the enclosing scope | unresolved |
| TSX-13 | `this.m()` in a class method, accessor or constructor body, also through arrow functions, whose enclosing method is the call's scope | the own method `m` of that class with the method's staticness (`this` is the class in a static method, an instance otherwise), when link time picks it (other staticness: TSX-20; anything else: TSX-21; a written member: TSX-25, TSX-26). Dispatch is static: when a subclass overrides `m`, the edge holds for instances of the class itself and no edge to the override is emitted (known gap, ADR 0011) |
| TSX-14 | `this` anywhere else: a plain or anonymous function, an object-literal method, a class field initializer or static block, a function bound by a declarator, the module | unresolved (what `this` holds depends on the call) |
| TSX-15 | a call inside a `with` statement's body, or in a function, static block or module whose code calls `eval(...)` directly | unresolved (the object or the evaluated code may bind the name) |
| TSX-16 | a second declaration of one name in one scope by another declaration (repeated functions, a function and a variable, declaration merging) | unresolved |
| TSX-17 | a name no scope of the file binds: a global, a built-in or a name declared only in a scope the site cannot see | unresolved when link time would pick a declaration of the file (it is not visible); otherwise as before (built-ins and globals count as external or unknown) |
| TSX-18 | the own name of a named function expression no declarator binds (`list.map(function f() { f(); })`), inside the function only; unwritable | the function expression (its own identity) |
| TSX-19 | a function, class or object-owner binding of TSX-1, TSX-2, TSX-3, TSX-7, TSX-8 or TSX-9, except a `const` declarator, that the file writes after its declaration: an assignment, compound or logical assignment, `++`/`--`, a destructuring assignment, a `for (x in/of ...)` target, or a direct `eval` the binding is visible from | unresolved (the call may reach the written value; the static member resolver of ADR 0026 does not resolve it either) |
| TSX-20 | `this.m()` (TSX-13) or `C.m()` (TSX-7) where the own method link time picks has the other staticness: a static method from an instance method, an instance method from a static method or through the class name | unresolved (the member does not exist on that value) |
| TSX-21 | `x.m()` through a class, object-owner or function binding, or `this.m()`, where link time's pick is not an own method of the bound declaration: another object owner of the same qualified name (sibling scopes), a function's nested declaration, an accessor, a method a later entry of the same key, a spread or a field of the same name replaces, a method under a computed key | unresolved |
| TSX-22 | a call through a TSX-3, TSX-8 or TSX-18 binding when link time's qualified-name lookup would pick another declaration (a nearer class or object method of the name, a declaration in a sibling scope) | the binding's declaration (link time is given it; a qualified name repeated in another scope does not block it, ADR 0011, since the binding names one declaration), except TSX-24 |
| TSX-23 | an import binding (TSX-11) when a declaration of the file has the callee's qualified name (a nearer class or object method of the name) | through the module resolver only (link time skips the file's declarations) |
| TSX-24 | a call through a TSX-3, TSX-8 or TSX-18 binding whose qualified name another declaration of the file repeats (ADR 0011), when link time's pick is not a method (another function of the name, or nothing) | unresolved (the repeated rule of ADR 0011) |
| TSX-25 | `this.m()`, `C.m()` or `o.m()` that TSX-13, TSX-7 or TSX-9 would resolve, when code of the file writes member `m` (or a property on its key path) of that object, or of that class on the call's side: its instances for an instance method, the class itself for a static method. Writes: an assignment, compound or logical assignment, `++`/`--`, a destructuring or `for (x in/of ...)` target, `delete`, or `Object.defineProperty`, `Reflect.defineProperty`, `Reflect.set` or `Reflect.deleteProperty` with that literal key. Receivers: `this` in the class's methods (instances; the class in a static method), static blocks (the class) and field initializers (both sides), the class binding (the class; `C.prototype`: instances), `x.constructor` of an instance (the class), the object binding, and a plain declarator holding `this`, a binding, `new C(...)` (instances) or a class expression (the class; through `prototype`, its instances). A write through any other receiver, or of a value reached through a key, is TSX-26. `Object.assign(x, { a, b: v, "c": v, ["d"]: v }, ...)` writes exactly the keys of its sources when there is at least one source and every source is an object literal whose every entry is a shorthand property or a pair keyed by an identifier, a string literal or a computed string literal, none of them `__proto__`. A computed key, a non-literal key, `Object.defineProperties`, or `Object.assign` in any other form (no source, a source that is no object literal, a spread argument or entry, a method or accessor entry, a numeric or non-literal computed key, a `__proto__` key) there writes every member of that side. `this.m = this.m.bind(this, ...)` and `C.m = C.m.bind(C, ...)` are no writes | unresolved (the call may reach the written value; the static member resolver of ADR 0026 does not resolve it either) |
| TSX-26 | `this.m()`, `C.m()` or `o.m()` that TSX-13, TSX-7 or TSX-9 would resolve, or a member call through an import (`api.m()`, `api.a.m()`) that TSX-11 or TSX-23 would resolve, when code of the file writes a property named like a key of the call's path (`m`, or `a` of `o.a.m()`) through a receiver whose owner the model does not know: `this` in a plain function, an object-literal method or the module, a parameter, a destructured name, an import (namespace, default or named), a require binding (`const api = require(...)`) or a destructured require, a global, a name written after its declaration (a `let` assigned `this` later), a declarator holding a member path (`const proto = C.prototype`, `const u = api.users`) or a call, a call result or `super`, and any value reached through a key of any receiver (`this.cfg.m = ...`, `x[k].m = ...`). Writes: those of TSX-25. A computed non-literal key there (`target[k] = ...`, `Object.assign(target, ...)` with an unknown key) may write any name: every such call of the file. A receiver known to hold no owner blocks nothing: a fresh literal (`const bag = []`, `Object.assign({}, ...)`) or a function binding. Only writes in the call's file count: a module that patches another module's export (`api.m = f` in another file) is not seen (known gap, ADR 0011), nor a write of `m` for a plain call `m()` through a named import | unresolved (the receiver may hold the call's owner, the imported module or an object on its path; the static member resolver of ADR 0026 does not resolve it either, nor any other `C.m()` of the file it would resolve: module-level calls, calls of an imported class) |

Default branch: a callee that is neither an identifier nor a chain of
named members of an identifier or `this` (`f()()`, `a[b]()`, `super.m()`,
`(0, f)()`) is not resolved, as before. A member write whose receiver is
none of the TSX-25 forms is TSX-26; a write in another file is not
seen. Static class member calls (ADR
0026) keep their own resolver and their own shadow guards.

## Shared consumers and limits

ADR 0026 adds own static class method calls after existing call resolution fails.
Resolve C.m() and ns.C.m() through lexical class bindings, imports, aliases,
barrels and namespace exports. Optional access/calls, async/generator methods and
overload implementations qualify. Require one emitted class declaration and an
own ordinary static method; ambient declarations, runtime declaration merges,
repeated identities, instance members, accessors, fields, private names and class
expressions provide no new targets. Type-only imports supply no runtime binding.
Scanned assignment/delete/update or Object.defineProperty/defineProperties/assign
mutations through resolved bindings block the affected member (all members for
unknown keys and bulk writes), including cross-file mutations on cached replay.
Existing edges are preserved. File-scope static calls use the nearest emitted
owner or file, without adding unresolved diagnostics for other file-scope calls.
Client wrappers consume these same edges. Inherited statics, this/super, element
access, instances and member value references retain their existing handling.

Angular DI proves a scanned Injectable service through a constructor parameter
property or inject(C). Injection factories and decorators use the shared resolver.
Any production provider overriding C with useClass/useFactory/useValue/useExisting
blocks proof. Proven receivers target the class's uniquely emitted methods for
the existing three-level client wrapper rules, with URL parameters last after
literal/constant prefixes. This supplies client wrapper targets, without adding
general-purpose calls edges or inferring inheritance/constructor assignments.

Import edges, calls, namespace members, route handlers and mounts share the same
module/export resolver. External exports without a scanned declaration do not
invent call targets. Factory recognition still uses the ADR 0013 allowlist.
Type declarations and type-only export clauses are not callable. No type checking or CommonJS export traversal
is performed. The deliberate TypeScript deviations and consequences are listed
in ADR 0014; unsupported forms remain unresolved.
