T022 MR-B6c fixture for build-time constant bases (docs/extract/clients.md
"Build-time constant bases", rows BC-1 to BC-4 and the unknown default BC-U).
No inert-to-live mappings or ignored directories are needed.

global.d.ts declares the build-time constants (a script declaration file);
types/module.d.ts is a module declaration file whose top-level
`declare const` is not global and whose `declare global` one is. legacy.js
(a top-level `var`) and setup.ts (a globalThis assignment) give two declared
names an in-repository value, so they stay unknown. frontend.ts uses the
constants as leading URL prefixes (BC-1, including a fetch wrapper and the
unresolved-base anchor guard); url-objects.ts covers URL-object origin bases
(BC-2, BC-3) and the mixed or rewritten sources that keep base none.
orval/ is an Orval 8 project whose mutator has react's URL composition
(`new URL(url, __API_URL__)` with protocol and host from
`new URL(__API_URL__)`, BC-4). server.ts holds the Express routes.
TestBuildConstantBases (internal/index) checks every call against its row;
build-constants.golden.json holds the client calls and calls_route edges.
