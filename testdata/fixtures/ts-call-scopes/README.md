# ts-call-scopes

Fixture of the TypeScript/JavaScript call binding model (rows TSX-1 to
TSX-26 of `docs/lang/typescript-resolution.md`, "Call binding model").
Every call carries a comment with the row that decides it and whether a
`calls` edge is expected. `TestCallBindingContract` indexes this
directory, checks that every call gets its row's outcome (resolved or
unresolved, the test's `bindingOutcomes`) and checks the `calls` edges
against `ts-call-scopes.golden.json`; the expected edges were checked
against the lexical scopes eslint-scope 8.2.0 (JavaScript) and
@typescript-eslint/scope-manager 8.74.0 (TypeScript) compute for each
call, and the member writes of `src/members.js` (TSX-25) and of
`src/receivers.js`, `src/receivers-any.js` and `src/patch/` (TSX-26)
with the same oracle's member-write check. `src/patch/` holds the
imports and requires used as receivers of unknown owner: `api.js` and
`lib.cjs` are the modules the other files import, call and patch.

`src/awaited.ts` holds the grammar correction of the same section
(T056): `await f<T>(x)`, which tree-sitter-typescript reads as a call of
`await f`, is bound through its real callee `f`, and a generic
`Object.assign<A, B>(o, ...)` under `await` is a member write (TSX-25);
`(await f)<T>(x)` is left unresolved. The base build emitted one
oracle-wrong edge here (`target.m()` after the awaited write) and missed
the three awaited calls.

No file is stored under an inert name.
