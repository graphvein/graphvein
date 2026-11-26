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

No file is stored under an inert name.
