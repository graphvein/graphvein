# Module resolution fixture

Hand-written cases for T011b Revision 2 and ADR 0014. The golden projects
resolved import/call endpoints, route handler/path data and diagnostic codes;
it is authored from these sources, without generating expectations from output.

barrel covers star, named aliases, default and namespace exports; relay covers
local imported relays (including default). conflict and outer-conflict propagate
star ambiguity; override demonstrates explicit and local precedence. cycle-a/b
terminate without a declaration; star excludes default. esm.js coexists with
esm.ts so substitution cannot succeed merely by absence of the literal file.

JSONC tsconfig extends configs/child.json then base.json, preserving baseUrl's
origin and replacing inherited paths. Aliases override workspace names, failed
aliases skip baseUrl and continue to workspace or external resolution, and baseUrl wins over workspace.
Packages cover source conditions, declarations last, missing dist main fallback,
subpaths and duplicate names. route-barrel relays handlers, routers, constants,
apps and an allowlisted external factory through the shared resolver.

No inert-to-live filename mapping is needed: package.json and tsconfig.json are
ordinary metadata, not ignore files. dist appears only as an absent string
target, never as a committed directory. Tests copy this tree outside Git and
edit only config metadata for the cache-replay check.

Additional cases cover nested namespace access, identical declarations reached
through two star sources, repeated-callable ambiguity behind a barrel, namespace
route handlers, type-only exports, and TypeScript's additional .jsx/.js fallback
candidates verified with the offline compiler.

Since extractors-41 (ADR 0070) resolution is the TypeScript compiler's
(`docs/lang/typescript-structure.md`): the root `package.json` declares
`packages/*` as workspaces so the packages are linked; `source` fields, the
`src/index` fallback and a `paths` miss continuing to a workspace name are
no longer read, the duplicate `@dup` links neither package, and an import
whose declaration-file and implementation lookups disagree (`@condition`'s
`types` condition) claims nothing. The replay test switches `@condition`
to a `main` entry.
